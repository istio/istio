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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/kube"
)

// DumpPodState logs the current pod state.
func DumpPodState(kubeConfig, namespace string) {
	if s, err := kube.GetPods(kubeConfig, namespace); err != nil {
		scopes.CI.Errorf("Error getting pods list via kubectl: %v", err)
		// continue on
	} else {
		scopes.CI.Infof("Pods (from Kubectl):\n%s", s)
	}
}

// CopyPodLogs copies pod logs from Kubernetes to the specified workDir.
func CopyPodLogs(kubeConfig, workDir, namespace string, accessor *kube.Accessor) {
	pods, err := accessor.GetPods(namespace)

	if err != nil {
		scopes.CI.Errorf("Error getting pods for error dump: %v", err)
		return
	}

	for i, pod := range pods {
		scopes.CI.Infof("[Pod %d] %s: phase:%q", i, pod.Name, pod.Status.Phase)
		for j, cs := range pod.Status.ContainerStatuses {
			scopes.CI.Infof("[Container %d/%d] %s: ready:%v", i, j, cs.Name, cs.Ready)
		}

		by, err := json.MarshalIndent(pod, "", "  ")
		if err != nil {
			scopes.CI.Errorf("Error marshaling pod status: %v", err)
		}
		scopes.CI.Infof("Pod Detail: \n%s\n", string(by))
	}

	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			logs, err := kube.Logs(kubeConfig, namespace, pod.Name, cs.Name)
			if err != nil {
				scopes.CI.Errorf("Error getting logs from pods: %v", err)
				continue
			}

			outFile, err := ioutil.TempFile(workDir, fmt.Sprintf("log_pod_%s_%s", pod.Name, cs.Name))
			if err != nil {
				scopes.CI.Errorf("Error creating temporary file for storing pod log: %v", err)
				continue
			}

			if err := ioutil.WriteFile(outFile.Name(), []byte(logs), os.ModePerm); err != nil {
				scopes.CI.Errorf("Error writing out pod lod to file: %v", err)
			}
		}
	}
}
