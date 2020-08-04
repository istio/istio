// Copyright 2018 Istio Authors
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

package main

import (
	"context"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/pkg/log"
)

// newKubeClient is a unit test override variable for interface create.
var newKubeClient = newK8sClient

// getKubePodInfo is a unit test override variable for interface create.
var getKubePodInfo = getK8sPodInfo

// newK8sClient returns a Kubernetes client
func newK8sClient(conf PluginConf) (*kubernetes.Clientset, error) {
	// Some config can be passed in a kubeconfig file
	kubeconfig := conf.Kubernetes.Kubeconfig

	// Config can be overridden by config passed in explicitly in the network config.
	configOverrides := &clientcmd.ConfigOverrides{}

	// Use the kubernetes client code to load the kubeconfig file and combine it with the overrides.
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		configOverrides).ClientConfig()
	if err != nil {
		log.Infof("Failed setting up kubernetes client with kubeconfig %s", kubeconfig)
		return nil, err
	}

	log.Infof("Set up kubernetes client with kubeconfig %s", kubeconfig)
	log.Infof("Kubernetes config %v", config)

	// Create the clientset
	return kubernetes.NewForConfig(config)
}

// getK8sPodInfo returns information of a POD
func getK8sPodInfo(client *kubernetes.Clientset, podName, podNamespace string) (containers []string,
	initContainers map[string]struct{}, labels map[string]string, annotations map[string]string, err error) {
	pod, err := client.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	log.Infof("pod info %+v", pod)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	initContainers = map[string]struct{}{}
	for _, initContainer := range pod.Spec.InitContainers {
		initContainers[initContainer.Name] = struct{}{}
	}
	containers = make([]string, len(pod.Spec.Containers))
	for containerIdx, container := range pod.Spec.Containers {
		log.Debug("Inspecting container",
			zap.String("pod", podName),
			zap.String("container", container.Name))
		containers[containerIdx] = container.Name

		if container.Name == "istio-proxy" {
			// don't include ports from istio-proxy in the redirect ports
			continue
		}
	}

	return containers, initContainers, pod.Labels, pod.Annotations, nil
}
