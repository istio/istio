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

// Package labels provides utility methods for retrieving Istio-specific labels
// from Kubernetes resources.
package labels

import "istio.io/istio/pilot/pkg/model"

// CanonicalService returns the values of the following labels from the supplied map:
// - service.istio.io/canonical-name
// - service.istio.io/canonical-revision
//
// If the labels are not in the map, a set of fallbacks are checked. For canonical name,
// `app.kubernetes.io/name` is checked, then `app`, evenutually falling back to the
// supplied `workloadName`. For canonical revision, `app.kubernetes.io/version` is checked,
// followed by `version` and finally defaulting to the literal value of `"latest"`.
func CanonicalService(labels map[string]string, workloadName string) (string, string) {
	return canonicalServiceName(labels, workloadName), canonicalServiceRevision(labels)
}

func canonicalServiceRevision(labels map[string]string) string {
	if rev, ok := labels[model.IstioCanonicalServiceRevisionLabelName]; ok {
		return rev
	}

	if rev, ok := labels["app.kubernetes.io/version"]; ok {
		return rev
	}

	if rev, ok := labels["version"]; ok {
		return rev
	}

	return "latest"
}

func canonicalServiceName(labels map[string]string, workloadName string) string {
	if svc, ok := labels[model.IstioCanonicalServiceLabelName]; ok {
		return svc
	}

	if svc, ok := labels["app.kubernetes.io/name"]; ok {
		return svc
	}

	if svc, ok := labels["app"]; ok {
		return svc
	}

	return workloadName
}
