/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package k8s

import (
	"fmt"
	"os"
	"strings"

	api "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// IsValidService checks if exists a service with the specified name
func IsValidService(kubeClient clientset.Interface, name string) (*api.Service, error) {
	ns, name, err := ParseNameNS(name)
	if err != nil {
		return nil, err
	}
	return kubeClient.Core().Services(ns).Get(name, meta_v1.GetOptions{})
}

// IsValidConfigMap check if exists a configmap with the specified name
func IsValidConfigMap(kubeClient clientset.Interface, fullName string) (*api.ConfigMap, error) {

	ns, name, err := ParseNameNS(fullName)

	if err != nil {
		return nil, err
	}

	configMap, err := kubeClient.Core().ConfigMaps(ns).Get(name, meta_v1.GetOptions{})

	if err != nil {
		return nil, fmt.Errorf("configmap not found: %v", err)
	}

	return configMap, nil

}

// IsValidNamespace chck if exists a namespace with the specified name
func IsValidNamespace(kubeClient clientset.Interface, name string) (*api.Namespace, error) {
	return kubeClient.Core().Namespaces().Get(name, meta_v1.GetOptions{})
}

// IsValidSecret checks if exists a secret with the specified name
func IsValidSecret(kubeClient clientset.Interface, name string) (*api.Secret, error) {
	ns, name, err := ParseNameNS(name)
	if err != nil {
		return nil, err
	}
	return kubeClient.Core().Secrets(ns).Get(name, meta_v1.GetOptions{})
}

// ParseNameNS parses a string searching a namespace and name
func ParseNameNS(input string) (string, string, error) {
	nsName := strings.Split(input, "/")
	if len(nsName) != 2 {
		return "", "", fmt.Errorf("invalid format (namespace/name) found in '%v'", input)
	}

	return nsName[0], nsName[1], nil
}

// GetNodeIP returns the IP address of a node in the cluster
func GetNodeIP(kubeClient clientset.Interface, name string) string {
	var externalIP string
	node, err := kubeClient.Core().Nodes().Get(name, meta_v1.GetOptions{})
	if err != nil {
		return externalIP
	}

	for _, address := range node.Status.Addresses {
		if address.Type == api.NodeExternalIP {
			if address.Address != "" {
				externalIP = address.Address
				break
			}
		}

		if externalIP == "" && address.Type == api.NodeInternalIP {
			externalIP = address.Address
		}
	}
	return externalIP
}

// PodInfo contains runtime information about the pod running the Ingres controller
type PodInfo struct {
	Name      string
	Namespace string
	NodeIP    string
	// Labels selectors of the running pod
	// This is used to search for other Ingress controller pods
	Labels map[string]string
}

// GetPodDetails returns runtime information about the pod:
// name, namespace and IP of the node where it is running
func GetPodDetails(kubeClient clientset.Interface) (*PodInfo, error) {
	podName := os.Getenv("POD_NAME")
	podNs := os.Getenv("POD_NAMESPACE")

	if podName == "" || podNs == "" {
		return nil, fmt.Errorf("unable to get POD information (missing POD_NAME or POD_NAMESPACE environment variable")
	}

	pod, _ := kubeClient.Core().Pods(podNs).Get(podName, meta_v1.GetOptions{})
	if pod == nil {
		return nil, fmt.Errorf("unable to get POD information")
	}

	return &PodInfo{
		Name:      podName,
		Namespace: podNs,
		NodeIP:    GetNodeIP(kubeClient, pod.Spec.NodeName),
		Labels:    pod.GetLabels(),
	}, nil
}
