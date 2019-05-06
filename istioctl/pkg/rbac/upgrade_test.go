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
	"io/ioutil"
	"reflect"
	"testing"
)

const (
	testFailedWithError   = "test failed with error %v"
	testFailedExpectedGot = "test failed. Expected\n%sGot\n%s"
)

type testCases struct {
	input                string
	workloadLabelMapping map[string]ServiceToWorkloadLabels
	expected             string
}

func TestUpgradeLocalFile(t *testing.T) {
	cases := []testCases{
		{
			input: "./testdata/rbac-policies.yaml",
			// Data from the BookExample. productpage.svc.cluster.local is the service with pod label
			// app: productpage.
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"service-viewer": {
					"productpage": map[string]string{
						"app": "productpage",
					},
				},
			},
			expected: "./testdata/rbac-policies-v2.golden.yaml",
		},
		{
			input: "./testdata/rbac-policies-with-methods-and-paths.yaml",
			// Data from the BookExample. ratings.svc.cluster.local is the service with pod label
			// app: ratings.
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"service-viewer": {
					"ratings": map[string]string{
						"app": "ratings",
					},
				},
			},
			expected: "./testdata/rbac-policies-with-methods-and-paths-v2.golden.yaml",
		},
	}

	for _, tc := range cases {
		upgrader := Upgrader{
			RbacFile:                 tc.input,
			RoleNameToWorkloadLabels: tc.workloadLabelMapping,
		}
		gotContent, err := upgrader.UpgradeCRDs()
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		expectedContent, err := ioutil.ReadFile(tc.expected)
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		if !reflect.DeepEqual(string(expectedContent), gotContent) {
			t.Errorf(testFailedExpectedGot, string(expectedContent), gotContent)
		}
	}
}
