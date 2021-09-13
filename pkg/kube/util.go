// Copyright Istio Authors
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

package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	kubeApiCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	//  allow out of cluster authentication
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	istioversion "istio.io/pkg/version"
)

// BuildClientConfig builds a client rest config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientConfig(kubeconfig, context string) (*rest.Config, error) {
	c, err := BuildClientCmd(kubeconfig, context).ClientConfig()
	if err != nil {
		return nil, err
	}
	return SetRestDefaults(c), nil
}

// BuildClientCmd builds a client cmd config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientCmd(kubeconfig, context string, overrides ...func(*clientcmd.ConfigOverrides)) clientcmd.ClientConfig {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			// If the specified kubeconfig doesn't exists / empty file / any other error
			// from file stat, fall back to default
			kubeconfig = ""
		}
	}

	// Config loading rules:
	// 1. kubeconfig if it not empty string
	// 2. Config(s) in KUBECONFIG environment variable
	// 3. In cluster config if running in-cluster
	// 4. Use $HOME/.kube/config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  context,
	}

	for _, fn := range overrides {
		fn(configOverrides)
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
}

// CreateClientset is a helper function that builds a kubernetes Clienset from a kubeconfig
// filepath. See `BuildClientConfig` for kubeconfig loading rules.
func CreateClientset(kubeconfig, context string, fns ...func(*rest.Config)) (*kubernetes.Clientset, error) {
	c, err := BuildClientConfig(kubeconfig, context)
	if err != nil {
		return nil, fmt.Errorf("build client config: %v", err)
	}
	for _, fn := range fns {
		fn(c)
	}
	return kubernetes.NewForConfig(c)
}

// DefaultRestConfig returns the rest.Config for the given kube config file and context.
func DefaultRestConfig(kubeconfig, configContext string, fns ...func(*rest.Config)) (*rest.Config, error) {
	config, err := BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	config = SetRestDefaults(config)

	for _, fn := range fns {
		fn(config)
	}

	return config, nil
}

// adjustCommand returns the last component of the
// OS-specific command path for use in User-Agent.
func adjustCommand(p string) string {
	// Unlikely, but better than returning "".
	if len(p) == 0 {
		return "unknown"
	}
	return filepath.Base(p)
}

// IstioUserAgent returns the user agent string based on the command being used.
// example: pilot-discovery/1.9.5 or istioctl/1.10.0
// This is a specialized version of rest.DefaultKubernetesUserAgent()
func IstioUserAgent() string {
	return adjustCommand(os.Args[0]) + "/" + istioversion.Info.Version
}

// SetRestDefaults is a helper function that sets default values for the given rest.Config.
// This function is idempotent.
func SetRestDefaults(config *rest.Config) *rest.Config {
	if config.GroupVersion == nil || config.GroupVersion.Empty() {
		config.GroupVersion = &kubeApiCore.SchemeGroupVersion
	}
	if len(config.APIPath) == 0 {
		if len(config.GroupVersion.Group) == 0 {
			config.APIPath = "/api"
		} else {
			config.APIPath = "/apis"
		}
	}
	if len(config.ContentType) == 0 {
		config.ContentType = runtime.ContentTypeJSON
	}
	if config.NegotiatedSerializer == nil {
		// This codec factory ensures the resources are not converted. Therefore, resources
		// will not be round-tripped through internal versions. Defaulting does not happen
		// on the client.
		config.NegotiatedSerializer = serializer.NewCodecFactory(IstioScheme).WithoutConversion()
	}
	if len(config.UserAgent) == 0 {
		config.UserAgent = IstioUserAgent()
	}

	return config
}

// CheckPodReadyOrComplete returns nil if the given pod and all of its containers are ready or terminated
// successfully.
func CheckPodReadyOrComplete(pod *kubeApiCore.Pod) error {
	switch pod.Status.Phase {
	case kubeApiCore.PodSucceeded:
		return nil
	case kubeApiCore.PodRunning:
		return CheckPodReady(pod)
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

// CheckPodReady returns nil if the given pod and all of its containers are ready.
func CheckPodReady(pod *kubeApiCore.Pod) error {
	switch pod.Status.Phase {
	case kubeApiCore.PodRunning:
		// Wait until all containers are ready.
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				return fmt.Errorf("container not ready: '%s'", containerStatus.Name)
			}
		}
		if len(pod.Status.Conditions) > 0 {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == kubeApiCore.PodReady && condition.Status != kubeApiCore.ConditionTrue {
					return fmt.Errorf("pod not ready, condition message: %v", condition.Message)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

// GetDeployMetaFromPod heuristically derives deployment metadata from the pod spec.
func GetDeployMetaFromPod(pod *kubeApiCore.Pod) (metav1.ObjectMeta, metav1.TypeMeta) {
	if pod == nil {
		return metav1.ObjectMeta{}, metav1.TypeMeta{}
	}
	// try to capture more useful namespace/name info for deployments, etc.
	// TODO(dougreid): expand to enable lookup of OWNERs recursively a la kubernetesenv
	deployMeta := pod.ObjectMeta
	deployMeta.ManagedFields = nil
	deployMeta.OwnerReferences = nil

	typeMetadata := metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	}
	if len(pod.GenerateName) > 0 {
		// if the pod name was generated (or is scheduled for generation), we can begin an investigation into the controlling reference for the pod.
		var controllerRef metav1.OwnerReference
		controllerFound := false
		for _, ref := range pod.GetOwnerReferences() {
			if ref.Controller != nil && *ref.Controller {
				controllerRef = ref
				controllerFound = true
				break
			}
		}
		if controllerFound {
			typeMetadata.APIVersion = controllerRef.APIVersion
			typeMetadata.Kind = controllerRef.Kind

			// heuristic for deployment detection
			deployMeta.Name = controllerRef.Name
			if typeMetadata.Kind == "ReplicaSet" && pod.Labels["pod-template-hash"] != "" && strings.HasSuffix(controllerRef.Name, pod.Labels["pod-template-hash"]) {
				name := strings.TrimSuffix(controllerRef.Name, "-"+pod.Labels["pod-template-hash"])
				deployMeta.Name = name
				typeMetadata.Kind = "Deployment"
			} else if typeMetadata.Kind == "Job" && len(controllerRef.Name) > 11 {
				// If job name suffixed with `-<ten-digit-timestamp>`, trim the suffix and set kind to cron job.
				l := len(controllerRef.Name)
				if _, err := strconv.Atoi(controllerRef.Name[l-10:]); err == nil && string(controllerRef.Name[l-11]) == "-" {
					deployMeta.Name = controllerRef.Name[:l-11]
					typeMetadata.Kind = "CronJob"
					// heuristically set cron job api version to v1beta1 as it cannot be derived from pod metadata.
					// Cronjob is not GA yet and latest version is v1beta1: https://github.com/kubernetes/enhancements/pull/978
					typeMetadata.APIVersion = "batch/v1beta1"
				}
			}
		}
	}

	if deployMeta.Name == "" {
		// if we haven't been able to extract a deployment name, then just give it the pod name
		deployMeta.Name = pod.Name
	}

	return deployMeta, typeMetadata
}
