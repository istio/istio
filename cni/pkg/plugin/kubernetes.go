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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// newKubeClient is a unit test override variable for interface create.
var newKubeClient = newK8sClient

// getKubePodInfo is a unit test override variable for interface create.
var getKubePodInfo = getK8sPodInfo

type PodInfo struct {
	Containers        sets.String
	Labels            map[string]string
	Annotations       map[string]string
	ProxyType         string
	ProxyEnvironments map[string]string
	ProxyUID          *int64
	ProxyGID          *int64
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
		Containers:        sets.New[string](),
		Labels:            pod.Labels,
		Annotations:       pod.Annotations,
		ProxyEnvironments: make(map[string]string),
	}
	for _, c := range containers(pod) {
		pi.Containers.Insert(c.Name)
		if c.Name == ISTIOPROXY {
			// don't include ports from istio-proxy in the redirect ports
			// Get proxy container env variable, and extract out ProxyConfig from it.
			for _, e := range c.Env {
				pi.ProxyEnvironments[e.Name] = e.Value
			}
			if len(c.Args) >= 2 && c.Args[0] == "proxy" {
				pi.ProxyType = c.Args[1]
			}
			if c.SecurityContext != nil {
				pi.ProxyUID = c.SecurityContext.RunAsUser
				pi.ProxyGID = c.SecurityContext.RunAsGroup
			}
		}
	}
	log.Debugf("Pod %v/%v info: \n%+v", podNamespace, podName, pi)

	return pi, nil
}

// containers fetches all containers in the pod.
// This is used to extract init containers (istio-init and istio-validation), and the sidecar.
// The sidecar can be a normal container or init in Kubernetes 1.28+
func containers(pod *v1.Pod) []v1.Container {
	res := make([]v1.Container, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
	res = append(res, pod.Spec.InitContainers...)
	res = append(res, pod.Spec.Containers...)
	return res
}

func (pi PodInfo) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  Containers: %v\n", sets.SortedList(pi.Containers)))
	b.WriteString(fmt.Sprintf("  Labels: %+v\n", pi.Labels))
	b.WriteString(fmt.Sprintf("  Annotations: %+v\n", pi.Annotations))
	b.WriteString(fmt.Sprintf("  Envs: %+v\n", pi.ProxyEnvironments))
	b.WriteString(fmt.Sprintf("  ProxyConfig: %+v\n", pi.ProxyEnvironments))
	return b.String()
}
