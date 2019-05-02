// Copyright 2016 The prometheus-operator Authors
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

package framework

import (
	"fmt"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pkg/errors"
)

// PrintPodLogs prints the logs of a specified Pod
func (f *Framework) PrintPodLogs(ns, p string) error {
	pod, err := f.KubeClient.CoreV1().Pods(ns).Get(p, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to print logs of pod '%v': failed to get pod", p)
	}

	for _, c := range pod.Spec.Containers {
		req := f.KubeClient.CoreV1().Pods(ns).GetLogs(p, &v1.PodLogOptions{Container: c.Name})
		resp, err := req.DoRaw()
		if err != nil {
			return errors.Wrapf(err, "failed to retrieve logs of pod '%v'", p)
		}

		fmt.Printf("=== Logs of %v/%v/%v:", ns, p, c.Name)
		fmt.Println(string(resp))
	}

	return nil
}

// GetPodRestartCount returns a map of container names and their restart counts for
// a given pod.
func (f *Framework) GetPodRestartCount(ns, podName string) (map[string]int32, error) {
	pod, err := f.KubeClient.CoreV1().Pods(ns).Get(podName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to retrieve pod to get restart count")
	}

	restarts := map[string]int32{}

	for _, status := range pod.Status.ContainerStatuses {
		restarts[status.Name] = status.RestartCount
	}

	return restarts, nil
}
