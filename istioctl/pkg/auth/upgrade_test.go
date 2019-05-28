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
	testFailedExpectedGot = "test [%s] failed.\nExpected\n%s\nGot\n%s"
)

var (
	appProductPage = map[string]string{
		"app": "productpage",
	}
	appRatings = map[string]string{
		"app": "ratings",
	}
	appReviews = map[string]string{
		"app": "reviews",
	}

	productPageMapping = map[string]WorkloadLabels{
		"productpage": appProductPage,
	}
	ratingsMapping = map[string]WorkloadLabels{
		"ratings": appRatings,
	}
)

type testCases struct {
	testName             string
	input                string
	workloadLabelMapping map[string]ServiceToWorkloadLabels
	expected             string
}

func TestUpgradeLocalFile(t *testing.T) {
	cases := []testCases{
		{
			testName: "ServiceRole with only services",
			input:    "./testdata/rbac-policies-services-only.yaml",
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"default": productPageMapping,
			},
			expected: "./testdata/rbac-policies-services-only-v2.golden.yaml",
		},
		{
			testName: "ServiceRole with methods and paths",
			input:    "./testdata/rbac-policies-with-methods-and-paths.yaml",
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"default": ratingsMapping,
			},
			expected: "./testdata/rbac-policies-with-methods-and-paths-v2.golden.yaml",
		},
		{
			testName: "Same ServiceRole name in multiple namespaces",
			input:    "./testdata/rbac-policies-multiple-namespaces.yaml",
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"foo": productPageMapping,
				"bar": ratingsMapping,
			},
			expected: "./testdata/rbac-policies-multiple-namespaces-v2.golden.yaml",
		},
		{
			input: "./testdata/rbac-policies-multiple-rules.yaml",
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"default": {
					"productpage": appProductPage,
					"ratings":     appRatings,
				},
			},
			expected: "./testdata/rbac-policies-multiple-rules-v2.golden.yaml",
		},
		{
			input: "./testdata/rbac-policies-multiple-services.yaml",
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"default": {
					"productpage": appProductPage,
					"ratings":     appRatings,
				},
			},
			expected: "./testdata/rbac-policies-multiple-services-v2.golden.yaml",
		},
		{
			input: "./testdata/rbac-policies-multiple-rules-multiple-services.yaml",
			workloadLabelMapping: map[string]ServiceToWorkloadLabels{
				"default": {
					"productpage": appProductPage,
					"ratings":     appRatings,
					"reviews":     appReviews,
				},
			},
			expected: "./testdata/rbac-policies-multiple-rules-multiple-services-v2.golden.yaml",
		},
	}

	for _, tc := range cases {
		upgrader := Upgrader{
			V1PolicyFile:                       tc.input,
			NamespaceToServiceToWorkloadLabels: tc.workloadLabelMapping,
		}
		err := upgrader.UpgradeCRDs()
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		gotContent := upgrader.ConvertedPolicies.String()
		expectedContent, err := ioutil.ReadFile(tc.expected)
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		if !reflect.DeepEqual(string(expectedContent), gotContent) {
			t.Errorf(testFailedExpectedGot, tc.testName, string(expectedContent), gotContent)
		}
	}
}
