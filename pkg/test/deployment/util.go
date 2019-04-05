//  Copyright 2018 Istio Authors
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

package deployment

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/gogo/protobuf/jsonpb"

	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

// DumpPodState logs the current pod state.
func DumpPodState(workDir string, namespace string, accessor *kube.Accessor) {
	pods, err := accessor.GetPods(namespace)
	if err != nil {
		scopes.CI.Errorf("Error getting pods list via kubectl: %v", err)
		return
	}

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

// DumpPodEvents logs the current pod event.
func DumpPodEvents(workDir, namespace string, accessor *kube.Accessor) {
	pods, err := accessor.GetPods(namespace)
	if err != nil {
		scopes.CI.Errorf("Error getting pods list via kubectl: %v", err)
		return
	}

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}

	for _, pod := range pods {
		events, err := accessor.GetEvents(namespace, pod.Name)
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

/*
// DumpPodData copies pod logs from Kubernetes to the specified workDir.
func DumpPodData(workDir, namespace string, accessor *kube.Accessor) {
	pods, err := accessor.GetPods(namespace)

	if err != nil {
		scopes.CI.Infof("Error getting pods for log copying: %v", err)
		return
	}

	scopes.CI.Infof("Dumping pod data for namespace: %v", namespace)

	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			logs, err := accessor.Logs(namespace, pod.Name, cs.Name)
			if err != nil {
				scopes.CI.Infof("Error getting logs from pod/container %s/%s: %v", pod.Name, cs.Name, err)
				continue
			}

			outPath := path.Join(workDir, fmt.Sprintf("%s_%s_%s.log", namespace, pod.Name, cs.Name))

			if err := ioutil.WriteFile(outPath, []byte(logs), os.ModePerm); err != nil {
				scopes.CI.Infof("Error writing out pod log to file: %v", err)
			}
		}
	}

	for _, pod := range pods {
		by, err := yaml.Marshal(pod)
		if err != nil {
			scopes.CI.Infof("Error marshaling pod state: %s: %v", pod.Name, err)
			continue
		}

		outPath := path.Join(workDir, fmt.Sprintf("%s_%s.yaml", namespace, pod.Name))

		if err := ioutil.WriteFile(outPath, by, os.ModePerm); err != nil {
			scopes.CI.Infof("Error writing out pod yaml to file: %v", err)
		}
	}
}
*/
