// Copyright 2019 Istio Authors
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

package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/ghodss/yaml"
	openshiftv1 "github.com/openshift/api/apps/v1"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/batch/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/pkg/log"
)

const (
	proxyContainerName              = "istio-proxy"
	initContainerName               = "istio-init"
	envoyVolumeName                 = "istio-envoy"
	certVolumeName                  = "istio-certs"
	annotationPolicy                = "sidecar.istio.io/inject"
	annotationStatus                = "sidecar.istio.io/status"
	annotationRewriteAppHTTPProbers = "sidecar.istio.io/rewriteAppHTTPProbers"
)

var (
	uninjectKinds = []struct {
		groupVersion schema.GroupVersion
		obj          runtime.Object
		resource     string
		apiPath      string
	}{
		{corev1.SchemeGroupVersion, &corev1.ReplicationController{}, "replicationcontrollers", "/api"},
		{corev1.SchemeGroupVersion, &corev1.Pod{}, "pods", "/api"},

		{appsv1.SchemeGroupVersion, &appsv1.Deployment{}, "deployments", "/apis"},
		{appsv1.SchemeGroupVersion, &appsv1.DaemonSet{}, "daemonsets", "/apis"},
		{appsv1.SchemeGroupVersion, &appsv1.ReplicaSet{}, "replicasets", "/apis"},

		{batchv1.SchemeGroupVersion, &batchv1.Job{}, "jobs", "/apis"},
		{v2alpha1.SchemeGroupVersion, &v2alpha1.CronJob{}, "cronjobs", "/apis"},

		{appsv1.SchemeGroupVersion, &appsv1.StatefulSet{}, "statefulsets", "/apis"},

		{corev1.SchemeGroupVersion, &corev1.List{}, "lists", "/apis"},

		{openshiftv1.GroupVersion, &openshiftv1.DeploymentConfig{}, "deploymentconfigs", "/apis"},
	}
	uninjectScheme = runtime.NewScheme()
)

func init() {
	for _, kind := range uninjectKinds {
		uninjectScheme.AddKnownTypes(kind.groupVersion, kind.obj)
		uninjectScheme.AddUnversionedTypes(kind.groupVersion, kind.obj)
	}
}

func validateUninjectFlags() error {
	var err error

	if uninjectInFilename == "" {
		err = multierr.Append(err, errors.New("filename not specified (see --filename or -f)"))
	}
	return err
}

func fromRawToObject(raw []byte) (runtime.Object, error) {
	var typeMeta metav1.TypeMeta
	if err := yaml.Unmarshal(raw, &typeMeta); err != nil {
		return nil, err
	}

	gvk := schema.FromAPIVersionAndKind(typeMeta.APIVersion, typeMeta.Kind)
	obj, err := uninjectScheme.New(gvk)
	if err != nil {
		return nil, err
	}
	if err = yaml.Unmarshal(raw, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

// extractResourceFile uninjects the istio proxy from the specified
// kubernetes YAML file.
func extractResourceFile(in io.Reader, out io.Writer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		obj, err := fromRawToObject(raw)
		if err != nil && !runtime.IsNotRegisteredError(err) {
			return err
		}

		var updated []byte
		if err == nil {
			outObject, err := extractObject(obj)
			if err != nil {
				return err
			}
			if updated, err = yaml.Marshal(outObject); err != nil {
				return err
			}
		} else {
			updated = raw // unchanged
		}

		if _, err = out.Write(updated); err != nil {
			return err
		}
		if _, err = fmt.Fprint(out, "---\n"); err != nil {
			return err
		}
	}
	return nil
}

// removeInjectedContainers removes the injected container name - istio-proxy and istio-init
func removeInjectedContainers(containers []corev1.Container, injectedContainerName string) []corev1.Container {
	for index, c := range containers {
		if c.Name == injectedContainerName {
			if index < len(containers)-1 {
				containers = append(containers[:index], containers[index+1:]...)
			} else {
				containers = append(containers[:index])
			}
			break
		}
	}
	return containers
}

// removeInjectedVolumes removes the injected volumes - istio-envoy and istio-certs
func removeInjectedVolumes(volumes []corev1.Volume) []corev1.Volume {

	l := len(volumes)
	index := 0
	for index < l {
		v := volumes[index]
		if v.Name == envoyVolumeName || v.Name == certVolumeName {
			if index < len(volumes)-1 {
				volumes = append(volumes[:index], volumes[index+1:]...)
			} else {
				volumes = append(volumes[:index])
			}
			//reset to 0
			index = 0
			l = len(volumes)
		}
		index++
	}
	return volumes
}

// handleAnnotations removes the injected sidecar.istio.io/status and sidecar.istio.io/rewriteAppHTTPProbers
// it adds sidecar.istio.io/inject: false
func handleAnnotations(annotations map[string]string) map[string]string {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	for key := range annotations {
		if key == annotationStatus {
			delete(annotations, key)
		}
		if key == annotationRewriteAppHTTPProbers {
			delete(annotations, key)
		}
	}
	// sidecar.istio.io/inject: false to default the auto-injector in case it is present.
	annotations[annotationPolicy] = "false"
	return annotations
}

// extractObject extras the sidecar injection and return the uninjected object.
func extractObject(in runtime.Object) (interface{}, error) {
	out := in.DeepCopyObject()

	var metadata *metav1.ObjectMeta
	var podSpec *corev1.PodSpec

	// Handle Lists
	if list, ok := out.(*corev1.List); ok {
		result := list

		for i, item := range list.Items {
			obj, err := fromRawToObject(item.Raw)
			if runtime.IsNotRegisteredError(err) {
				continue
			}
			if err != nil {
				return nil, err
			}

			r, err := extractObject(obj)
			if err != nil {
				return nil, err
			}

			re := runtime.RawExtension{}
			re.Object = r.(runtime.Object)
			result.Items[i] = re
		}
		return result, nil
	}

	// CronJobs have JobTemplates in them, instead of Templates, so we
	// special case them.
	switch v := out.(type) {
	case *v2alpha1.CronJob:
		job := v
		metadata = &job.Spec.JobTemplate.ObjectMeta
		podSpec = &job.Spec.JobTemplate.Spec.Template.Spec
	case *corev1.Pod:
		pod := v
		metadata = &pod.ObjectMeta
		podSpec = &pod.Spec
	default:
		// `in` is a pointer to an Object. Dereference it.
		outValue := reflect.ValueOf(out).Elem()

		templateValue := outValue.FieldByName("Spec").FieldByName("Template")
		// `Template` is defined as a pointer in some older API
		// definitions, e.g. ReplicationController
		if templateValue.Kind() == reflect.Ptr {
			templateValue = templateValue.Elem()
		}
		metadata = templateValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)
		podSpec = templateValue.FieldByName("Spec").Addr().Interface().(*corev1.PodSpec)
	}

	//skip uninjection for pods
	sidecarInjected := false
	for _, c := range podSpec.Containers {
		if c.Name == proxyContainerName {
			sidecarInjected = true
		}
	}
	if !sidecarInjected {
		log.Info("Skipping uninjection because there is no sidecar injected.")
		return in, nil
	}

	podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, initContainerName)
	podSpec.Containers = removeInjectedContainers(podSpec.Containers, proxyContainerName)

	podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes)

	metadata.Annotations = handleAnnotations(metadata.Annotations)

	return out, nil
}

var (
	uninjectInFilename  string
	uninjectOutFilename string
)

func uninjectCommand() *cobra.Command {
	uninjectCmd := &cobra.Command{
		Use:   "kube-uninject",
		Short: "Uninject Envoy sidecar from Kubernetes pod resources",
		Long: `

kube-uninject is used to prevent Istio from adding a sidecar and
also provides the inverse of "istioctl kube-inject -f".

`,
		Example: `
# Update resources before applying.
kubectl apply -f <(istioctl experimental kube-uninject -f <resource.yaml>)

# Create a persistent version of the deployment by removing Envoy sidecar.
istioctl experimental kube-uninject -f deployment.yaml -o deployment-uninjected.yaml

# Update an existing deployment.
kubectl get deployment -o yaml | istioctl experimental kube-uninject -f - | kubectl apply -f -
`,
		RunE: func(c *cobra.Command, _ []string) (err error) {

			if err = validateUninjectFlags(); err != nil {
				return err
			}
			//get the resource content
			var reader io.Reader
			if uninjectInFilename == "-" {
				reader = os.Stdin
			} else {
				var in *os.File
				if in, err = os.Open(uninjectInFilename); err != nil {
					log.Errorf("Error: close file from %s, %s", uninjectInFilename, err)
					return err
				}
				reader = in
				defer func() {
					if errClose := in.Close(); errClose != nil {
						log.Errorf("Error: close file from %s, %s", uninjectInFilename, errClose)

						// don't overwrite the previous error
						if err == nil {
							err = errClose
						}
					}
				}()
			}

			var writer io.Writer
			if uninjectOutFilename == "" {
				writer = c.OutOrStdout()
			} else {
				var out *os.File
				if out, err = os.Create(uninjectOutFilename); err != nil {
					return err
				}
				writer = out
				defer func() {
					if errClose := out.Close(); errClose != nil {
						log.Errorf("Error: close file from %s, %s", uninjectOutFilename, errClose)

						// don't overwrite the previous error
						if err == nil {
							err = errClose
						}
					}
				}()
			}

			return extractResourceFile(reader, writer)
		},
	}

	uninjectCmd.PersistentFlags().StringVarP(&uninjectInFilename, "filename", "f",
		"", "Input Kubernetes resource filename")
	uninjectCmd.PersistentFlags().StringVarP(&uninjectOutFilename, "output", "o",
		"", "Modified output Kubernetes resource filename")

	return uninjectCmd
}
