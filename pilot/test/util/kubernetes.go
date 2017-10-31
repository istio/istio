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
	"testing"
	"time"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Test utilities for kubernetes

const (
	// PodCheckBudget is the maximum number of retries with 1s delays
	PodCheckBudget = 200
)

// CreateNamespace creates a fresh namespace
func CreateNamespace(cl kubernetes.Interface) (string, error) {
	ns, err := cl.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: meta_v1.ObjectMeta{
			GenerateName: "istio-test-",
		},
	})
	if err != nil {
		return "", err
	}
	glog.Infof("Created namespace %s", ns.Name)
	return ns.Name, nil
}

// DeleteNamespace removes a namespace
func DeleteNamespace(cl kubernetes.Interface, ns string) {
	if ns != "" && ns != "default" {
		if err := cl.CoreV1().Namespaces().Delete(ns, &meta_v1.DeleteOptions{}); err != nil {
			glog.Warningf("Error deleting namespace: %v", err)
		}
		glog.Infof("Deleted namespace %s", ns)
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

// GetAppPods awaits till all pods are running in a namespace, and returns a map
// from "app" label value to the pod names.
func GetAppPods(cl kubernetes.Interface, nslist []string) (map[string][]string, error) {
	pods := make(map[string][]string)
	var items []v1.Pod

	for _, ns := range nslist {
		glog.Infof("Checking all pods are running in namespace %s ...", ns)

		for n := 0; ; n++ {
			list, err := cl.CoreV1().Pods(ns).List(meta_v1.ListOptions{})
			if err != nil {
				return pods, err
			}
			items = list.Items
			ready := true

			for _, pod := range items {
				if pod.Status.Phase != "Running" {
					glog.Infof("Pod %s.%s has status %s", pod.Name, ns, pod.Status.Phase)
					ready = false
					break
				} else {
					for _, container := range pod.Status.ContainerStatuses {
						if !container.Ready {
							glog.Infof("Container %s in Pod %s in namespace % s is not ready", container.Name, pod.Name, ns)
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
				}

				break
			}
			if n > PodCheckBudget {
				return pods, fmt.Errorf("exceeded budget for checking pod status")
			}

			time.Sleep(time.Second)
		}
	}

	return pods, nil
}

// FetchLogs for a container in a a pod
func FetchLogs(cl kubernetes.Interface, name, namespace string, container string) string {
	glog.V(2).Infof("Fetching log for container %s in %s.%s", container, name, namespace)
	raw, err := cl.CoreV1().Pods(namespace).
		GetLogs(name, &v1.PodLogOptions{Container: container}).
		Do().Raw()
	if err != nil {
		glog.Infof("Request error %v", err)
		return ""
	}
	return string(raw)
}

// Eventually does retrees to check a predicate
func Eventually(f func() bool, t *testing.T) {
	interval := 64 * time.Millisecond
	for i := 0; i < 10; i++ {
		if f() {
			return
		}
		glog.Infof("Sleeping %v", interval)
		time.Sleep(interval)
		interval = 2 * interval
	}
	t.Errorf("Failed to satisfy function")
}
