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
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/gogo/protobuf/jsonpb"
	kubeApiCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/scopes"
)

// podDumper will dump information from all the pods into the given workDir.
// If no pods are provided, accessor will be used to fetch all the pods in a namespace.
type podDumper func(workDir string, namespace string, pods ...kubeApiCore.Pod)

// DumpPods runs each dumper with all the pods in the given namespace.
// If no dumpers are provided, their resource state, events, container logs and Envoy information will be dumped.
func (a *Accessor) DumpPods(workDir, namespace string, dumpers ...podDumper) {
	if len(dumpers) == 0 {
		dumpers = []podDumper{
			a.DumpPodState,
			a.DumpPodEvents,
			a.DumpPodLogs,
			a.DumpPodProxies,
		}

	}

	pods, err := a.GetPods(namespace)
	if err != nil {
		scopes.CI.Errorf("Error getting pods list via kubectl: %v", err)
		return
	}

	for _, dump := range dumpers {
		dump(workDir, namespace, pods...)
	}
}

func (a *Accessor) podsOrFetch(pods []kubeApiCore.Pod, namespace string) []kubeApiCore.Pod {
	if len(pods) == 0 {
		var err error
		pods, err = a.GetPods(namespace)
		if err != nil {
			scopes.CI.Errorf("Error getting pods list via kubectl: %v", err)
			return nil
		}
	}
	return pods
}

// DumpPodState dumps the pod state for either the provided pods or all pods in the namespace if none are provided.
func (a *Accessor) DumpPodState(workDir string, namespace string, pods ...kubeApiCore.Pod) {
	pods = a.podsOrFetch(pods, namespace)

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}

	for _, pod := range pods {
		str, err := marshaler.MarshalToString(&pod)
		if err != nil {
			scopes.CI.Errorf("Error marshaling pod state for output: %v", err)
			continue
		}

		outPath := path.Join(workDir, fmt.Sprintf("pod_%s_%s.yaml", namespace, pod.Name))

		if err := ioutil.WriteFile(outPath, []byte(str), os.ModePerm); err != nil {
			scopes.CI.Infof("Error writing out pod state to file: %v", err)
		}
	}
}

// DumpPodEvents dumps the pod events for either the provided pods or all pods in the namespace if none are provided.
func (a *Accessor) DumpPodEvents(workDir, namespace string, pods ...kubeApiCore.Pod) {
	pods = a.podsOrFetch(pods, namespace)

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}

	for _, pod := range pods {
		events, err := a.GetEvents(namespace, pod.Name)
		if err != nil {
			scopes.CI.Errorf("Error getting events list for pod %s/%s via kubectl: %v", namespace, pod.Name, err)
			return
		}

		outPath := path.Join(workDir, fmt.Sprintf("pod_events_%s_%s.yaml", namespace, pod.Name))

		eventsStr := ""
		for _, event := range events {
			eventStr, err := marshaler.MarshalToString(&event)
			if err != nil {
				scopes.CI.Errorf("Error marshaling pod event for output: %v", err)
				continue
			}

			eventsStr += eventStr
			eventsStr += "\n"
		}

		if err := ioutil.WriteFile(outPath, []byte(eventsStr), os.ModePerm); err != nil {
			scopes.CI.Infof("Error writing out pod events to file: %v", err)
		}
	}
}

// DumpPodLogs will dump logs from each container in each of the provided pods
// or all pods in the namespace if none are provided.
func (a *Accessor) DumpPodLogs(workDir, namespace string, pods ...kubeApiCore.Pod) {
	pods = a.podsOrFetch(pods, namespace)

	for _, pod := range pods {
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			l, err := a.Logs(pod.Namespace, pod.Name, container.Name, false /* previousLog */)
			if err != nil {
				scopes.CI.Errorf("Unable to get logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				continue
			}

			fname := path.Join(workDir, fmt.Sprintf("%s-%s.log", pod.Name, container.Name))
			if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.CI.Errorf("Unable to write logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			}
		}
	}
}

// DumpPodProxies will dump Envoy proxy config and clusters in each of the provided pods
// or all pods in the namespace if none are provided.
func (a *Accessor) DumpPodProxies(workDir, namespace string, pods ...kubeApiCore.Pod) {
	pods = a.podsOrFetch(pods, namespace)

	for _, pod := range pods {
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			if container.Name != "istio-proxy" {
				continue
			}

			if cfgDump, err := a.Exec(pod.Namespace, pod.Name, container.Name, "pilot-agent request GET config_dump"); err == nil {
				fname := path.Join(workDir, fmt.Sprintf("%s-%s.config.json", pod.Name, container.Name))
				if err = ioutil.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
					scopes.CI.Errorf("Unable to write config dump for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			} else {
				scopes.CI.Errorf("Unable to get istio-proxy config dump for pod: %s/%s", pod.Namespace, pod.Name)
			}

			if cfgDump, err := a.Exec(pod.Namespace, pod.Name, container.Name, "pilot-agent request GET clusters"); err == nil {
				fname := path.Join(workDir, fmt.Sprintf("%s-%s.clusters.txt", pod.Name, container.Name))
				if err = ioutil.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
					scopes.CI.Errorf("Unable to write clusters for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			} else {
				scopes.CI.Errorf("Unable to get istio-proxy clusters for pod: %s/%s", pod.Namespace, pod.Name)
			}
		}
	}
}
