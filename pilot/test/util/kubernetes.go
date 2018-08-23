// Copyright 2017 Istio Authors
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

package util

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
)

// Test utilities for kubernetes

const (
	// PodCheckBudget is the maximum number of retries with 1s delays
	PodCheckBudget = 200
)

// CreateNamespace creates a fresh namespace
func CreateNamespace(cl kubernetes.Interface) (string, error) {
	return CreateNamespaceWithPrefix(cl, "istio-test-", false)
}

// CreateNamespaceWithPrefix creates a fresh namespace with the given prefix
func CreateNamespaceWithPrefix(cl kubernetes.Interface, prefix string, inject bool) (string, error) {
	injectionValue := "disabled"
	if inject {
		injectionValue = "enabled"
	}
	ns, err := cl.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: meta_v1.ObjectMeta{
			GenerateName: prefix,
			Labels: map[string]string{
				"istio-injection": injectionValue,
			},
		},
	})
	if err != nil {
		return "", err
	}
	log.Infof("Created namespace %s", ns.Name)
	return ns.Name, nil
}

// DeleteNamespace removes a namespace
func DeleteNamespace(cl kubernetes.Interface, ns string) {
	if ns != "" && ns != "default" {
		if err := cl.CoreV1().Namespaces().Delete(ns, &meta_v1.DeleteOptions{}); err != nil {
			log.Warnf("Error deleting namespace: %v", err)
		}
		log.Infof("Deleted namespace %s", ns)
	}
}

// GetPods gets pod names in a namespace
func GetPods(cl kubernetes.Interface, ns string) []string {
	out := make([]string, 0)
	list, err := cl.CoreV1().Pods(ns).List(meta_v1.ListOptions{})
	if err != nil {
		return out
	}
	for _, pod := range list.Items {
		out = append(out, pod.Name)
	}
	return out
}

func describeNotReadyPods(items []v1.Pod, kubeconfig, ns string) {
	for _, pod := range items {
		if pod.Status.Phase != "Pending" && pod.Status.Phase != "Running" {
			continue
		}
		for _, container := range pod.Status.ContainerStatuses {
			if container.Ready {
				continue
			}
			cmd := fmt.Sprintf("kubectl describe pods %s --kubeconfig %s -n %s",
				pod.Name, kubeconfig, ns)
			output, _ := Shell(cmd)
			log.Errorf("%s\n%s", cmd, output)

			cmd = fmt.Sprintf("kubectl logs %s -c istio-proxy --kubeconfig %s -n %s",
				pod.Name, kubeconfig, ns)
			output, _ = Shell(cmd)
			log.Errorf("%s\n%s", cmd, output)
		}
	}
}

// CopyPodFiles copies files from a pod to the machine
func CopyPodFiles(container, pod, ns, source, dest string) {
	// kubectl cp <some-namespace>/<some-pod>:/tmp/foo /tmp/bar -c container
	var cmd string
	if container == "" {
		cmd = fmt.Sprintf("kubectl cp %s/%s:%s %s",
			ns, pod, source, dest)
	} else {
		cmd = fmt.Sprintf("kubectl cp %s/%s:%s %s  -c %s",
			ns, pod, source, dest, container)
	}
	output, _ := Shell(cmd)
	log.Errorf("%s\n%s", cmd, output)
}

// GetAppPods awaits till all pods are running in a namespace, and returns a map
// from "app" label value to the pod names.
func GetAppPods(cl kubernetes.Interface, kubeconfig string, nslist []string) (map[string][]string, error) {
	// TODO: clean and move this method to top level, 'AwaitPods' or something similar,
	// merged with the similar method used by the other tests. Eventually make it part of
	// istioctl or a similar helper.
	pods := make(map[string][]string)
	var items []v1.Pod

	for _, ns := range nslist {
		log.Infof("Checking all pods are running in namespace %s ...", ns)

		for n := 0; ; n++ {
			list, err := cl.CoreV1().Pods(ns).List(meta_v1.ListOptions{})
			if err != nil {
				return pods, err
			}
			items = list.Items
			ready := true

			for _, pod := range items {
				// Exclude pods that may be in non-running state when helm is used to
				// initialize.
				if strings.HasPrefix(pod.Name, "istio-mixer-create") {
					continue
				}
				if strings.HasPrefix(pod.Name, "istio-sidecar-injector") {
					continue
				}
				if pod.Status.Phase != "Running" {
					log.Infof("Pod %s.%s has status %s", pod.Name, ns, pod.Status.Phase)
					ready = false
					break
				} else {
					for _, container := range pod.Status.ContainerStatuses {
						if !container.Ready {
							log.Infof("Container %s in Pod %s in namespace % s is not ready", container.Name, pod.Name, ns)
							ready = false
							break
						}
					}
					if !ready {
						break
					}
				}
			}

			if ready {
				for _, pod := range items {
					if app, exists := pod.Labels["app"]; exists {
						pods[app] = append(pods[app], pod.Name)
					}
					if app, exists := pod.Labels["istio"]; exists {
						pods[app] = append(pods[app], pod.Name)
					}
				}

				break
			}
			if n > PodCheckBudget {
				describeNotReadyPods(items, kubeconfig, ns)
				return pods, fmt.Errorf("exceeded budget %d for checking pod status", n)
			}

			time.Sleep(time.Second)
		}
	}

	log.Infof("Found pods: %v", pods)
	return pods, nil
}

// FetchLogs for a container in a a pod
func FetchLogs(cl kubernetes.Interface, name, namespace string, container string) string {
	log.Infof("Fetching log for container %s in %s.%s", container, name, namespace)
	raw, err := cl.CoreV1().Pods(namespace).
		GetLogs(name, &v1.PodLogOptions{Container: container}).
		Do().Raw()
	if err != nil {
		log.Infof("Request error %v", err)

		raw, err = cl.CoreV1().Pods(namespace).
			GetLogs(name, &v1.PodLogOptions{Container: container, Previous: true}).
			Do().Raw()
		if err != nil {
			return ""
		}
	}
	return string(raw)
}

// WaitForValidatingWebhookConfigurationDeletion waits until given validatingwebhookconfiguration is deleted.
// TODO: move this to framework level when more tests are in need of it.
func WaitForValidatingWebhookConfigurationDeletion(cl kubernetes.Interface, config string) error {
	err := wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		_, err2 := cl.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(config, meta_v1.GetOptions{})
		if err2 == nil {
			return false, nil
		}

		if errors.IsNotFound(err2) {
			return true, nil
		}

		return true, err2
	})

	return err
}
