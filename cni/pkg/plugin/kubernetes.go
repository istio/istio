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

package plugin

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/options"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

// newKubeClient is a unit test override variable for interface create.
var newKubeClient = newK8sClient

// getKubePodInfo is a unit test override variable for interface create.
var getKubePodInfo = getK8sPodInfo

type PodInfo struct {
	Containers        []string
	InitContainers    map[string]struct{}
	Labels            map[string]string
	Annotations       map[string]string
	ProxyEnvironments map[string]string
	ProxyConfig       *meshconfig.ProxyConfig
}

// newK8sClient returns a Kubernetes client
func newK8sClient(conf Config) (*kubernetes.Clientset, error) {
	// Some config can be passed in a kubeconfig file
	kubeconfig := conf.Kubernetes.Kubeconfig

	config, err := kube.DefaultRestConfig(kubeconfig, "")
	if err != nil {
		log.Errorf("Failed setting up kubernetes client with kubeconfig %s", kubeconfig)
		return nil, err
	}

	log.Debugf("istio-cni set up kubernetes client with kubeconfig %s", kubeconfig)

	// Create the clientset
	return kubernetes.NewForConfig(config)
}

// getK8sPodInfo returns information of a POD
func getK8sPodInfo(client *kubernetes.Clientset, podName, podNamespace string) (*PodInfo, error) {
	pod, err := client.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	pi := &PodInfo{
		InitContainers:    make(map[string]struct{}),
		Containers:        make([]string, len(pod.Spec.Containers)),
		Labels:            pod.Labels,
		Annotations:       pod.Annotations,
		ProxyEnvironments: make(map[string]string),
	}
	for _, initContainer := range pod.Spec.InitContainers {
		pi.InitContainers[initContainer.Name] = struct{}{}
	}
	for containerIdx, container := range pod.Spec.Containers {
		log.Debugf("Inspecting pod %v/%v container %v", podNamespace, podName, container.Name)
		pi.Containers[containerIdx] = container.Name

		if container.Name == "istio-proxy" {
			// don't include ports from istio-proxy in the redirect ports
			// Get proxy container env variable, and extract out ProxyConfig from it.
			for _, e := range container.Env {
				pi.ProxyEnvironments[e.Name] = e.Value
				if e.Name == options.ProxyConfigEnv {
					mc := &meshconfig.MeshConfig{
						DefaultConfig: mesh.DefaultProxyConfig(),
					}
					mc, err := mesh.ApplyProxyConfig(e.Value, mc)
					if err != nil {
						log.Warnf("Failed to apply proxy config for %v/%v: %+v", pod.Namespace, pod.Name, err)
					} else {
						pi.ProxyConfig = mc.DefaultConfig
					}
					break
				}
			}
			continue
		}
	}
	log.Debugf("Pod %v/%v info: \n%+v", podNamespace, podName, pi)

	return pi, nil
}

func (pi PodInfo) String() string {
	var b strings.Builder
	icn := make([]string, 0, len(pi.InitContainers))
	for n := range pi.InitContainers {
		icn = append(icn, n)
	}
	b.WriteString(fmt.Sprintf("  Init Containers: %v\n", icn))
	b.WriteString(fmt.Sprintf("  Containers: %v\n", pi.Containers))
	b.WriteString(fmt.Sprintf("  Labels: %+v\n", pi.Labels))
	b.WriteString(fmt.Sprintf("  Annotations: %+v\n", pi.Annotations))
	b.WriteString(fmt.Sprintf("  Envs: %+v\n", pi.ProxyEnvironments))
	b.WriteString(fmt.Sprintf("  ProxyConfig: %+v\n", pi.ProxyEnvironments))
	return b.String()
}
