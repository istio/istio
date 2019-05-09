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

package auth

import (
	"io/ioutil"
	"reflect"
	"testing"
)

const (
	testFailedWithError   = "test failed with error %v"
	testFailedExpectedGot = "test [%s] failed. Expected\n%s\nGot\n%s"
)

var (
	serviceViewerProductpage = map[string]ServiceToWorkloadLabels{
		"service-viewer": {
			"productpage": map[string]string{
				"app": "productpage",
			},
		},
	}
	serviceViewerRatings = map[string]ServiceToWorkloadLabels{
		"service-viewer": {
			"ratings": map[string]string{
				"app": "ratings",
			},
		},
	}
)

type testCases struct {
	testName             string
	input                string
	workloadLabelMapping map[string]RoleNameToWorkloadLabels
	expected             string
}

func TestUpgradeLocalFile(t *testing.T) {
	cases := []testCases{
		{
			testName: "ServiceRole with only services",
			input:    "./testdata/rbac-policies.yaml",
			workloadLabelMapping: map[string]RoleNameToWorkloadLabels{
				"default": serviceViewerProductpage,
			},
			expected: "./testdata/rbac-policies-v2.golden.yaml",
		},
		{
			testName: "ServiceRole with methods and paths",
			input:    "./testdata/rbac-policies-with-methods-and-paths.yaml",
			workloadLabelMapping: map[string]RoleNameToWorkloadLabels{
				"default": serviceViewerRatings,
			},
			expected: "./testdata/rbac-policies-with-methods-and-paths-v2.golden.yaml",
		},
		{
			testName: "Same ServiceRole name in multiple namespaces",
			input:    "./testdata/rbac-policies-multiple-namespaces.yaml",
			workloadLabelMapping: map[string]RoleNameToWorkloadLabels{
				"foo": serviceViewerProductpage,
				"bar": serviceViewerRatings,
			},
			expected: "./testdata/rbac-policies-multiple-namespaces-v2.golden.yaml",
		},
	}

	for _, tc := range cases {
		upgrader := Upgrader{
			V1PolicyFile:                        tc.input,
			NamespaceToRoleNameToWorkloadLabels: tc.workloadLabelMapping,
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
			t.Errorf(testFailedExpectedGot, tc.testName, string(expectedContent), gotContent)
		}
	}
}
