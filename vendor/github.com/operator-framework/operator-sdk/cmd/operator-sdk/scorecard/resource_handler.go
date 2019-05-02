// Copyright 2019 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scorecard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/operator-framework/operator-sdk/internal/util/yamlutil"
	proxyConf "github.com/operator-framework/operator-sdk/pkg/ansible/proxy/kubeconfig"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"github.com/spf13/viper"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

type cleanupFn func() error

// waitUntilCRStatusExists waits until the status block of the CR currently being tested exists. If the timeout
// is reached, it simply continues and assumes there is no status block
func waitUntilCRStatusExists(cr *unstructured.Unstructured) error {
	err := wait.Poll(time.Second*1, time.Second*time.Duration(viper.GetInt(InitTimeoutOpt)), func() (bool, error) {
		err := runtimeClient.Get(context.TODO(), types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()}, cr)
		if err != nil {
			return false, fmt.Errorf("error getting custom resource: %v", err)
		}
		if cr.Object["status"] != nil {
			return true, nil
		}
		return false, nil
	})
	if err != nil && err != wait.ErrWaitTimeout {
		return err
	}
	return nil
}

// yamlToUnstructured decodes a yaml file into an unstructured object
func yamlToUnstructured(yamlPath string) (*unstructured.Unstructured, error) {
	yamlFile, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %v", yamlPath, err)
	}
	if bytes.Contains(yamlFile, []byte("\n---\n")) {
		return nil, fmt.Errorf("custom resource manifest cannot have more than 1 resource")
	}
	obj := &unstructured.Unstructured{}
	jsonSpec, err := yaml.YAMLToJSON(yamlFile)
	if err != nil {
		return nil, fmt.Errorf("could not convert yaml file to json: %v", err)
	}
	if err := obj.UnmarshalJSON(jsonSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal custom resource manifest to unstructured: %s", err)
	}
	// set the namespace
	obj.SetNamespace(viper.GetString(NamespaceOpt))
	return obj, nil
}

// createFromYAMLFile will take a path to a YAML file and create the resource. If it finds a
// deployment, it will add the scorecard proxy as a container in the deployments podspec.
func createFromYAMLFile(yamlPath string) error {
	yamlSpecs, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %v", yamlPath, err)
	}
	scanner := yamlutil.NewYAMLScanner(yamlSpecs)
	for scanner.Scan() {
		obj := &unstructured.Unstructured{}
		jsonSpec, err := yaml.YAMLToJSON(scanner.Bytes())
		if err != nil {
			return fmt.Errorf("could not convert yaml file to json: %v", err)
		}
		if err := obj.UnmarshalJSON(jsonSpec); err != nil {
			return fmt.Errorf("could not unmarshal resource spec: %v", err)
		}
		obj.SetNamespace(viper.GetString(NamespaceOpt))

		// dirty hack to merge scorecard proxy into operator deployment; lots of serialization and deserialization
		if obj.GetKind() == "Deployment" {
			// TODO: support multiple deployments
			if deploymentName != "" {
				return fmt.Errorf("scorecard currently does not support multiple deployments in the manifests")
			}
			dep, err := unstructuredToDeployment(obj)
			if err != nil {
				return fmt.Errorf("failed to convert object to deployment: %v", err)
			}
			deploymentName = dep.GetName()
			err = createKubeconfigSecret()
			if err != nil {
				return fmt.Errorf("failed to create kubeconfig secret for scorecard-proxy: %v", err)
			}
			addMountKubeconfigSecret(dep)
			addProxyContainer(dep)
			// go back to unstructured to create
			obj, err = deploymentToUnstructured(dep)
			if err != nil {
				return fmt.Errorf("failed to convert deployment to unstructured: %v", err)
			}
		}
		err = runtimeClient.Create(context.TODO(), obj)
		if err != nil {
			_, restErr := restMapper.RESTMappings(obj.GetObjectKind().GroupVersionKind().GroupKind())
			if restErr == nil {
				return err
			}
			// don't store error, as only error will be timeout. Error from runtime client will be easier for
			// the user to understand than the timeout error, so just use that if we fail
			_ = wait.PollImmediate(time.Second*1, time.Second*10, func() (bool, error) {
				restMapper.Reset()
				_, err := restMapper.RESTMappings(obj.GetObjectKind().GroupVersionKind().GroupKind())
				if err != nil {
					return false, nil
				}
				return true, nil
			})
			err = runtimeClient.Create(context.TODO(), obj)
			if err != nil {
				return err
			}
		}
		addResourceCleanup(obj, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()})
		if obj.GetKind() == "Deployment" {
			proxyPodGlobal, err = getPodFromDeployment(deploymentName, viper.GetString(NamespaceOpt))
			if err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan %s: (%v)", yamlPath, err)
	}

	return nil
}

// getPodFromDeployment returns a deployment depName's pod in namespace.
func getPodFromDeployment(depName, namespace string) (pod *v1.Pod, err error) {
	dep := &appsv1.Deployment{}
	err = runtimeClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: depName}, dep)
	if err != nil {
		return nil, fmt.Errorf("failed to get newly created deployment: %v", err)
	}
	set := labels.Set(dep.Spec.Selector.MatchLabels)
	// In some cases, the pod from the old deployment will be picked up
	// instead of the new one.
	err = wait.PollImmediate(time.Second*1, time.Second*60, func() (bool, error) {
		pods := &v1.PodList{}
		err = runtimeClient.List(context.TODO(), &client.ListOptions{LabelSelector: set.AsSelector()}, pods)
		if err != nil {
			return false, fmt.Errorf("failed to get list of pods in deployment: %v", err)
		}
		// Make sure the pods exist. There should only be 1 pod per deployment.
		if len(pods.Items) == 1 {
			// If the pod has a deletion timestamp, it is the old pod; wait for
			// pod with no deletion timestamp
			if pods.Items[0].GetDeletionTimestamp() == nil {
				pod = &pods.Items[0]
				return true, nil
			}
		} else {
			log.Debug("Operator deployment has more than 1 pod")
		}
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get proxyPod: %s", err)
	}
	return pod, nil
}

// createKubeconfigSecret creates the secret that will be mounted in the operator's container and contains
// the kubeconfig for communicating with the proxy
func createKubeconfigSecret() error {
	kubeconfigMap := make(map[string][]byte)
	kc, err := proxyConf.Create(metav1.OwnerReference{Name: "scorecard"}, "http://localhost:8889", viper.GetString(NamespaceOpt))
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(kc.Name()); err != nil {
			log.Errorf("Failed to delete generated kubeconfig file: (%v)", err)
		}
	}()
	kc, err = os.Open(kc.Name())
	if err != nil {
		return err
	}
	kcBytes, err := ioutil.ReadAll(kc)
	if err != nil {
		return err
	}
	kubeconfigMap["kubeconfig"] = kcBytes
	kubeconfigSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scorecard-kubeconfig",
			Namespace: viper.GetString(NamespaceOpt),
		},
		Data: kubeconfigMap,
	}
	err = runtimeClient.Create(context.TODO(), kubeconfigSecret)
	if err != nil {
		return err
	}
	addResourceCleanup(kubeconfigSecret, types.NamespacedName{Namespace: kubeconfigSecret.GetNamespace(), Name: kubeconfigSecret.GetName()})
	return nil
}

// addMountKubeconfigSecret creates the volume mount for the kubeconfig secret
func addMountKubeconfigSecret(dep *appsv1.Deployment) {
	// create mount for secret
	dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, v1.Volume{
		Name: "scorecard-kubeconfig",
		VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{
			SecretName: "scorecard-kubeconfig",
			Items: []v1.KeyToPath{{
				Key:  "kubeconfig",
				Path: "config",
			}},
		},
		},
	})
	for index := range dep.Spec.Template.Spec.Containers {
		// mount the volume
		dep.Spec.Template.Spec.Containers[index].VolumeMounts = append(dep.Spec.Template.Spec.Containers[index].VolumeMounts, v1.VolumeMount{
			Name:      "scorecard-kubeconfig",
			MountPath: "/scorecard-secret",
		})
		// specify the path via KUBECONFIG env var
		dep.Spec.Template.Spec.Containers[index].Env = append(dep.Spec.Template.Spec.Containers[index].Env, v1.EnvVar{
			Name:  "KUBECONFIG",
			Value: "/scorecard-secret/config",
		})
	}
}

// addProxyContainer adds the container spec for the scorecard-proxy to the deployment's podspec
func addProxyContainer(dep *appsv1.Deployment) {
	pullPolicyString := viper.GetString(ProxyPullPolicyOpt)
	var pullPolicy v1.PullPolicy
	switch pullPolicyString {
	case "Always":
		pullPolicy = v1.PullAlways
	case "Never":
		pullPolicy = v1.PullNever
	case "PullIfNotPresent":
		pullPolicy = v1.PullIfNotPresent
	default:
		// this case shouldn't happen since we check the values in scorecard.go, but just in case, we'll default to always to prevent errors
		pullPolicy = v1.PullAlways
	}
	dep.Spec.Template.Spec.Containers = append(dep.Spec.Template.Spec.Containers, v1.Container{
		Name:            scorecardContainerName,
		Image:           viper.GetString(ProxyImageOpt),
		ImagePullPolicy: pullPolicy,
		Command:         []string{"scorecard-proxy"},
		Env: []v1.EnvVar{{
			Name:      k8sutil.WatchNamespaceEnvVar,
			ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
		}},
	})
}

// unstructuredToDeployment converts an unstructured object to a deployment
func unstructuredToDeployment(obj *unstructured.Unstructured) (*appsv1.Deployment, error) {
	jsonByte, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to convert deployment to json: %v", err)
	}
	depObj, _, err := dynamicDecoder.Decode(jsonByte, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode deployment object: %v", err)
	}
	switch o := depObj.(type) {
	case *appsv1.Deployment:
		return o, nil
	default:
		return nil, fmt.Errorf("conversion of runtime object to deployment failed (resulting runtime object not deployment type)")
	}
}

// deploymentToUnstructured converts a deployment to an unstructured object
func deploymentToUnstructured(dep *appsv1.Deployment) (*unstructured.Unstructured, error) {
	jsonByte, err := json.Marshal(dep)
	if err != nil {
		return nil, fmt.Errorf("failed to remarshal deployment: %v", err)
	}
	obj := &unstructured.Unstructured{}
	err = obj.UnmarshalJSON(jsonByte)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal updated deployment: %v", err)
	}
	return obj, nil
}

// cleanupScorecard runs all cleanup functions in reverse order
func cleanupScorecard() error {
	failed := false
	for i := len(cleanupFns) - 1; i >= 0; i-- {
		err := cleanupFns[i]()
		if err != nil {
			failed = true
			log.Printf("a cleanup function failed with error: %v\n", err)
		}
	}
	if failed {
		return fmt.Errorf("a cleanup function failed; see stdout for more details")
	}
	return nil
}

// addResourceCleanup adds a cleanup function for the specified runtime object
func addResourceCleanup(obj runtime.Object, key types.NamespacedName) {
	cleanupFns = append(cleanupFns, func() error {
		// make a copy of the object because the client changes it
		objCopy := obj.DeepCopyObject()
		err := runtimeClient.Delete(context.TODO(), obj)
		if err != nil {
			return err
		}
		err = wait.PollImmediate(time.Second*1, time.Second*10, func() (bool, error) {
			err = runtimeClient.Get(context.TODO(), key, objCopy)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, fmt.Errorf("error encountered during deletion of resource type %v with namespace/name (%+v): %v", objCopy.GetObjectKind().GroupVersionKind().Kind, key, err)
			}
			return false, nil
		})
		if err != nil {
			return fmt.Errorf("cleanup function failed: %v", err)
		}
		return nil
	})
}

func getProxyLogs(proxyPod *v1.Pod) (string, error) {
	// need a standard kubeclient for pod logs
	kubeclient, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return "", fmt.Errorf("failed to create kubeclient: %v", err)
	}
	logOpts := &v1.PodLogOptions{Container: scorecardContainerName}
	req := kubeclient.CoreV1().Pods(proxyPod.GetNamespace()).GetLogs(proxyPod.GetName(), logOpts)
	readCloser, err := req.Stream()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %v", err)
	}
	defer readCloser.Close()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(readCloser)
	if err != nil {
		return "", fmt.Errorf("test failed and failed to read pod logs: %v", err)
	}
	return buf.String(), nil
}
