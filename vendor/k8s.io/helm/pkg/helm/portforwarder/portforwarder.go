/*
Copyright The Helm Authors.

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

package portforwarder

import (
	"fmt"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	"k8s.io/helm/pkg/kube"
)

var (
	tillerPodLabels = labels.Set{"app": "helm", "name": "tiller"}
)

// New creates a new and initialized tunnel.
func New(namespace string, client kubernetes.Interface, config *rest.Config) (*kube.Tunnel, error) {
	podName, err := GetTillerPodName(client.CoreV1(), namespace)
	if err != nil {
		return nil, err
	}
	const tillerPort = 44134
	t := kube.NewTunnel(client.CoreV1().RESTClient(), config, namespace, podName, tillerPort)
	return t, t.ForwardPort()
}

// GetTillerPodName fetches the name of tiller pod running in the given namespace.
func GetTillerPodName(client corev1.PodsGetter, namespace string) (string, error) {
	selector := tillerPodLabels.AsSelector()
	pod, err := getFirstRunningPod(client, namespace, selector)
	if err != nil {
		return "", err
	}
	return pod.ObjectMeta.GetName(), nil
}

// GetTillerPodImage fetches the image of tiller pod running in the given namespace.
func GetTillerPodImage(client corev1.PodsGetter, namespace string) (string, error) {
	selector := tillerPodLabels.AsSelector()
	pod, err := getFirstRunningPod(client, namespace, selector)
	if err != nil {
		return "", err
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == "tiller" {
			return c.Image, nil
		}
	}
	return "", fmt.Errorf("could not find a tiller pod")
}

func getFirstRunningPod(client corev1.PodsGetter, namespace string, selector labels.Selector) (*v1.Pod, error) {
	options := metav1.ListOptions{LabelSelector: selector.String()}
	pods, err := client.Pods(namespace).List(options)
	if err != nil {
		return nil, err
	}
	if len(pods.Items) < 1 {
		return nil, fmt.Errorf("could not find tiller")
	}
	for _, p := range pods.Items {
		if isPodReady(&p) {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("could not find a ready tiller pod")
}
