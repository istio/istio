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

	"gopkg.in/yaml.v2"

	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/kube"
)

// DumpPodState logs the current pod state.
func DumpPodState(namespace string, accessor *kube.Accessor) {
	s, err := accessor.GetPods(namespace)
	if err != nil {
		scopes.CI.Errorf("Error getting pods list via kubectl: %v", err)
		return
	}
	scopes.CI.Infof("Pods (from Kubectl):\n%v", s)
}

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
			scopes.CI.Infof("Error marshalling pod state: %s: %v", pod.Name, err)
			continue
		}

		outPath := path.Join(workDir, fmt.Sprintf("%s_%s.yaml", namespace, pod.Name))

		if err := ioutil.WriteFile(outPath, []byte(by), os.ModePerm); err != nil {
			scopes.CI.Infof("Error writing out pod yaml to file: %v", err)
		}
	}
}
