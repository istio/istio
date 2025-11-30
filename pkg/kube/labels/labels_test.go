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

package labels

import (
	"testing"

	"istio.io/istio/pkg/model"
)

func TestCanonicalService(t *testing.T) {
	tests := []struct {
		name             string
		labels           map[string]string
		workloadName     string
		expectedName     string
		expectedRevision string
	}{
		{
			name:             "empty labels, fallback to workload name and latest",
			labels:           map[string]string{},
			workloadName:     "my-workload",
			expectedName:     "my-workload",
			expectedRevision: "latest",
		},
		{
			name: "canonical service labels take priority",
			labels: map[string]string{
				model.IstioCanonicalServiceLabelName:         "canonical-name",
				model.IstioCanonicalServiceRevisionLabelName: "canonical-revision",
				"app.kubernetes.io/name":                     "k8s-name",
				"app.kubernetes.io/version":                  "k8s-version",
				"app":                                        "legacy-app",
				"version":                                    "legacy-version",
			},
			workloadName:     "my-workload",
			expectedName:     "canonical-name",
			expectedRevision: "canonical-revision",
		},
		{
			name: "app.kubernetes.io/name takes priority over app",
			labels: map[string]string{
				"app.kubernetes.io/name":    "k8s-name",
				"app.kubernetes.io/version": "k8s-version",
				"app":                       "legacy-app",
				"version":                   "legacy-version",
			},
			workloadName:     "my-workload",
			expectedName:     "k8s-name",
			expectedRevision: "k8s-version",
		},
		{
			name: "legacy app and version labels work as fallback",
			labels: map[string]string{
				"app":     "legacy-app",
				"version": "legacy-version",
			},
			workloadName:     "my-workload",
			expectedName:     "legacy-app",
			expectedRevision: "legacy-version",
		},
		{
			name: "mixed labels - k8s name with legacy version",
			labels: map[string]string{
				"app.kubernetes.io/name": "k8s-name",
				"version":                "legacy-version",
			},
			workloadName:     "my-workload",
			expectedName:     "k8s-name",
			expectedRevision: "legacy-version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, revision := CanonicalService(tt.labels, tt.workloadName)
			if name != tt.expectedName {
				t.Errorf("CanonicalService() name = %v, want %v", name, tt.expectedName)
			}
			if revision != tt.expectedRevision {
				t.Errorf("CanonicalService() revision = %v, want %v", revision, tt.expectedRevision)
			}
		})
	}
}

func TestGetAppName(t *testing.T) {
	tests := []struct {
		name          string
		labels        map[string]string
		expectedValue string
		expectedOk    bool
	}{
		{
			name:          "empty labels",
			labels:        map[string]string{},
			expectedValue: "",
			expectedOk:    false,
		},
		{
			name: "canonical service label takes priority",
			labels: map[string]string{
				model.IstioCanonicalServiceLabelName: "canonical-name",
				"app.kubernetes.io/name":             "k8s-name",
				"app":                                "legacy-app",
			},
			expectedValue: "canonical-name",
			expectedOk:    true,
		},
		{
			name: "app.kubernetes.io/name takes priority over app",
			labels: map[string]string{
				"app.kubernetes.io/name": "k8s-name",
				"app":                    "legacy-app",
			},
			expectedValue: "k8s-name",
			expectedOk:    true,
		},
		{
			name: "legacy app label works as fallback",
			labels: map[string]string{
				"app": "legacy-app",
			},
			expectedValue: "legacy-app",
			expectedOk:    true,
		},
		{
			name: "only app.kubernetes.io/name set",
			labels: map[string]string{
				"app.kubernetes.io/name": "k8s-name",
			},
			expectedValue: "k8s-name",
			expectedOk:    true,
		},
		{
			name: "unrelated labels only",
			labels: map[string]string{
				"foo": "bar",
			},
			expectedValue: "",
			expectedOk:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, ok := GetAppName(tt.labels)
			if value != tt.expectedValue {
				t.Errorf("GetAppName() value = %v, want %v", value, tt.expectedValue)
			}
			if ok != tt.expectedOk {
				t.Errorf("GetAppName() ok = %v, want %v", ok, tt.expectedOk)
			}
		})
	}
}

func TestHasCanonicalServiceName(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: false,
		},
		{
			name: "has canonical service label",
			labels: map[string]string{
				model.IstioCanonicalServiceLabelName: "name",
			},
			expected: true,
		},
		{
			name: "has app.kubernetes.io/name",
			labels: map[string]string{
				"app.kubernetes.io/name": "name",
			},
			expected: true,
		},
		{
			name: "has app label",
			labels: map[string]string{
				"app": "name",
			},
			expected: true,
		},
		{
			name: "has unrelated labels",
			labels: map[string]string{
				"foo": "bar",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCanonicalServiceName(tt.labels)
			if got != tt.expected {
				t.Errorf("HasCanonicalServiceName() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHasCanonicalServiceRevision(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: false,
		},
		{
			name: "has canonical service revision label",
			labels: map[string]string{
				model.IstioCanonicalServiceRevisionLabelName: "revision",
			},
			expected: true,
		},
		{
			name: "has app.kubernetes.io/version",
			labels: map[string]string{
				"app.kubernetes.io/version": "version",
			},
			expected: true,
		},
		{
			name: "has version label",
			labels: map[string]string{
				"version": "v1",
			},
			expected: true,
		},
		{
			name: "has unrelated labels",
			labels: map[string]string{
				"foo": "bar",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCanonicalServiceRevision(tt.labels)
			if got != tt.expected {
				t.Errorf("HasCanonicalServiceRevision() = %v, want %v", got, tt.expected)
			}
		})
	}
}
