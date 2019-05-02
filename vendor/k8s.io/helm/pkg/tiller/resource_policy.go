/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tiller

import (
	"bytes"
	"strings"

	"k8s.io/helm/pkg/kube"
	"k8s.io/helm/pkg/tiller/environment"
)

func filterManifestsToKeep(manifests []Manifest) ([]Manifest, []Manifest) {
	remaining := []Manifest{}
	keep := []Manifest{}

	for _, m := range manifests {
		if m.Head.Metadata == nil || m.Head.Metadata.Annotations == nil || len(m.Head.Metadata.Annotations) == 0 {
			remaining = append(remaining, m)
			continue
		}

		resourcePolicyType, ok := m.Head.Metadata.Annotations[kube.ResourcePolicyAnno]
		if !ok {
			remaining = append(remaining, m)
			continue
		}

		resourcePolicyType = strings.ToLower(strings.TrimSpace(resourcePolicyType))
		if resourcePolicyType == kube.KeepPolicy {
			keep = append(keep, m)
		}

	}
	return keep, remaining
}

func summarizeKeptManifests(manifests []Manifest, kubeClient environment.KubeClient, namespace string) string {
	var message string
	for _, m := range manifests {
		// check if m is in fact present from k8s client's POV.
		output, err := kubeClient.Get(namespace, bytes.NewBufferString(m.Content))
		if err != nil || strings.Contains(output, kube.MissingGetHeader) {
			continue
		}

		details := "[" + m.Head.Kind + "] " + m.Head.Metadata.Name + "\n"
		if message == "" {
			message = "These resources were kept due to the resource policy:\n"
		}
		message = message + details
	}
	return message
}
