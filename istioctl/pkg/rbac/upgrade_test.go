// Copyright 2019 Istio Authors
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

package rbac

import (
	"reflect"
	"testing"
)

const (
	testFailedWithError   = "test failed with error %v"
	testFailedExpectedGot = "test failed, expected %s, got %s"
)

type testCases struct {
	input    Upgrader
	expected string
}

func TestUpgradeLocalFile(t *testing.T) {
	upgrader := Upgrader{
		RbacFile:             "./testdata/rbac-policies.yaml",
		RoleToWorkloadLabels: map[string]ServiceToWorkloadLabels{},
	}
	// Data from the BookExample. productpage.svc.cluster.local is the service with pod label
	// app: productpage.
	upgrader.RoleToWorkloadLabels["service-viewer"] = ServiceToWorkloadLabels{
		"productpage": map[string]string{
			"app": "productpage",
		},
	}
	cases := []testCases{
		{
			input: upgrader,
			expected: `apiVersion: rbac.istio.io/v1alpha1
kind: ServiceRole
metadata:
  name: service-viewer
  namespace: default
spec:
  rules:
  - constraints:
    - key: destination.labels[version]
      values:
      - v3
    methods:
    - '*'
---
apiVersion: rbac.istio.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: bind-service-viewer
  namespace: default
spec:
  allow:
  - role: service-viewer
    subjects:
    - properties:
        source.namespace: istio-system
    - groups:
      - bar
      names:
      - foo
  workloadSelector:
    labels:
      app: productpage
---
`,
		},
	}
	for _, tc := range cases {
		gotContent, err := tc.input.UpgradeCRDs()
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		if !reflect.DeepEqual(tc.expected, gotContent) {
			t.Errorf(testFailedExpectedGot, tc.expected, gotContent)
		}
	}
}
