//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// podDumper will dump information from all the pods into the given workDir.
// If no pods are provided, client will be used to fetch all the pods in a namespace.
type podDumper func(a resource.Cluster, workDir string, namespace string, pods ...kubeApiCore.Pod)

func outputPath(workDir string, cluster resource.Cluster, pod kubeApiCore.Pod, name string) string {
	dir := path.Join(workDir, cluster.Name())
	if err := os.MkdirAll(dir, os.ModeDir|0700); err != nil {
		scopes.Framework.Warnf("failed creating directory: %s", dir)
	}
	return path.Join(dir, fmt.Sprintf("%s_%s", pod.Name, name))
}

// DumpPods runs each dumper with all the pods in the given namespace.
// If no dumpers are provided, their resource state, events, container logs and Envoy information will be dumped.
func DumpPods(a resource.Cluster, workDir, namespace string, dumpers ...podDumper) {
	if len(dumpers) == 0 {
		dumpers = []podDumper{
			DumpPodState,
			DumpPodEvents,
			DumpPodLogs,
			DumpPodProxies,
		}
	}

	pods, err := a.PodsForSelector(context.TODO(), namespace)
	if err != nil {
		scopes.Framework.Errorf("Error getting pods list via kubectl: %v", err)
		return
	}

	for _, dump := range dumpers {
		dump(a, workDir, namespace, pods.Items...)
	}
}

func podsOrFetch(a resource.Cluster, pods []kubeApiCore.Pod, namespace string) []kubeApiCore.Pod {
	if len(pods) == 0 {
		podList, err := a.CoreV1().Pods(namespace).List(context.TODO(), kubeApiMeta.ListOptions{})
		if err != nil {
			scopes.Framework.Errorf("Error getting pods list via kubectl: %v", err)
			return nil
		}
		pods = podList.Items
	}
	return pods
}

// DumpPodState dumps the pod state for either the provided pods or all pods in the namespace if none are provided.
func DumpPodState(c resource.Cluster, workDir string, namespace string, pods ...kubeApiCore.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}

	for _, pod := range pods {
		str, err := marshaler.MarshalToString(&pod)
		if err != nil {
			scopes.Framework.Errorf("Error marshaling pod state for output: %v", err)
			continue
		}

		outPath := outputPath(workDir, c, pod, "pod-state.yaml")
		if err := ioutil.WriteFile(outPath, []byte(str), os.ModePerm); err != nil {
			scopes.Framework.Infof("Error writing out pod state to file: %v", err)
		}
	}
}

// DumpPodEvents dumps the pod events for either the provided pods or all pods in the namespace if none are provided.
func DumpPodEvents(c resource.Cluster, workDir, namespace string, pods ...kubeApiCore.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}

	for _, pod := range pods {
		list, err := c.CoreV1().Events(namespace).List(context.TODO(),
			kubeApiMeta.ListOptions{
				FieldSelector: "involvedObject.name=" + pod.Name,
			})
		if err != nil {
			scopes.Framework.Errorf("Error getting events list for pod %s/%s via kubectl: %v", namespace, pod.Name, err)
			return
		}

		eventsStr := ""
		for _, event := range list.Items {
			eventStr, err := marshaler.MarshalToString(&event)
			if err != nil {
				scopes.Framework.Errorf("Error marshaling pod event for output: %v", err)
				continue
			}

			eventsStr += eventStr
			eventsStr += "\n"
		}

		outPath := outputPath(workDir, c, pod, "pod-events.yaml")
		if err := ioutil.WriteFile(outPath, []byte(eventsStr), os.ModePerm); err != nil {
			scopes.Framework.Infof("Error writing out pod events to file: %v", err)
		}
	}
}

// DumpPodLogs will dump logs from each container in each of the provided pods
// or all pods in the namespace if none are provided.
func DumpPodLogs(c resource.Cluster, workDir, namespace string, pods ...kubeApiCore.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	for _, pod := range pods {
		isVM := checkIfVM(pod)
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			l, err := c.PodLogs(context.TODO(), pod.Name, pod.Namespace, container.Name, false /* previousLog */)
			if err != nil {
				scopes.Framework.Errorf("Unable to get logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				continue
			}

			fname := outputPath(workDir, c, pod, fmt.Sprintf("%s.log", container.Name))
			if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.Framework.Errorf("Unable to write logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			}

			// Get envoy logs if the pod is a VM, since kubectl logs only shows the logs from iptables for VMs
			if isVM && container.Name == "istio-proxy" {
				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.err.log"); err == nil {
					fname := outputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.err.log", container.Name))
					if err = ioutil.WriteFile(fname, []byte(stdout+stderr), os.ModePerm); err != nil {
						scopes.Framework.Errorf("Unable to write envoy err log for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
					}
				} else {
					scopes.Framework.Errorf("Unable to get envoy err log for pod: %s/%s", pod.Namespace, pod.Name)
				}

				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.log"); err == nil {
					fname := outputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.log", container.Name))
					if err = ioutil.WriteFile(fname, []byte(stdout+stderr), os.ModePerm); err != nil {
						scopes.Framework.Errorf("Unable to write envoy log for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
					}
				} else {
					scopes.Framework.Errorf("Unable to get envoy log for pod: %s/%s", pod.Namespace, pod.Name)
				}
			}
		}
	}
}

// DumpPodProxies will dump Envoy proxy config and clusters in each of the provided pods
// or all pods in the namespace if none are provided.
func DumpPodProxies(c resource.Cluster, workDir, namespace string, pods ...kubeApiCore.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	for _, pod := range pods {
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			if container.Name != "istio-proxy" {
				continue
			}

			if cfgDump, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "pilot-agent request GET config_dump"); err == nil {
				fname := outputPath(workDir, c, pod, "proxy-config.json")
				if err = ioutil.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
					scopes.Framework.Errorf("Unable to write config dump for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			} else {
				scopes.Framework.Errorf("Unable to get istio-proxy config dump for pod: %s/%s", pod.Namespace, pod.Name)
			}

			if cfgDump, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "pilot-agent request GET clusters"); err == nil {
				fname := outputPath(workDir, c, pod, "proxy-clusters.txt")
				if err = ioutil.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
					scopes.Framework.Errorf("Unable to write clusters for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			} else {
				scopes.Framework.Errorf("Unable to get istio-proxy clusters for pod: %s/%s", pod.Namespace, pod.Name)
			}
		}
	}
}

func checkIfVM(pod kubeApiCore.Pod) bool {
	for k := range pod.ObjectMeta.Labels {
		if strings.Contains(k, "test-vm") {
			return true
		}
	}
	return false
}
