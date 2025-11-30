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

import (
	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/model"
)

var (
	// These are the labels that are checked for canonical service name and revision.
	// Note: the order of these labels is important.
	nameLabels = []string{
		model.IstioCanonicalServiceLabelName,
		"app.kubernetes.io/name",
		"app",
	}
	revisionLabels = []string{
		model.IstioCanonicalServiceRevisionLabelName,
		"app.kubernetes.io/version",
		"version",
	}
)

// WorkloadNameFromWorkloadEntry derives the workload name from a WorkloadEntry
func WorkloadNameFromWorkloadEntry(name string, annos map[string]string, labels map[string]string) string {
	if arg, f := annos[annotation.IoIstioAutoRegistrationGroup.Name]; f {
		return arg
	}
	if wn, f := labels[label.ServiceWorkloadName.Name]; f {
		return wn
	}
	return name
}

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

// lookupLabelValue returns the value of the first label in the supplied map that matches
// one of the supplied keys.
func lookupLabelValue(labels map[string]string, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := labels[key]; ok {
			return value, true
		}
	}
	return "", false
}

func HasCanonicalServiceName(labels map[string]string) bool {
	_, ok := lookupLabelValue(labels, nameLabels...)
	return ok
}

func HasCanonicalServiceRevision(labels map[string]string) bool {
	_, ok := lookupLabelValue(labels, revisionLabels...)
	return ok
}

func canonicalServiceRevision(labels map[string]string) string {
	value, ok := lookupLabelValue(labels, revisionLabels...)
	if !ok {
		return "latest"
	}
	return value
}

func canonicalServiceName(labels map[string]string, workloadName string) string {
	value, ok := lookupLabelValue(labels, nameLabels...)
	if !ok {
		return workloadName
	}
	return value
}

// GetAppName returns the app name from labels, checking the following labels in order:
// 1. service.istio.io/canonical-name
// 2. app.kubernetes.io/name
// 3. app
// Returns the value and true if found, or empty string and false if none of the labels are present.
func GetAppName(labels map[string]string) (string, bool) {
	return lookupLabelValue(labels, nameLabels...)
}
