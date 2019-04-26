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
	"os"
	"reflect"
	"testing"
)

const (
	testFailedWithError   = "test failed with error %v"
	testFailedExpectedGot = "test failed, expected %s, got %s"
)

type testCases struct {
	input    string
	expected string
}

func TestCheckAndCreateNewFile(t *testing.T) {
	// Remove ./testdata/rbac-policies-v2.yaml if it exist
	newFilePath := "./testdata/rbac-policies-v2.yaml"
	if _, err := os.Stat(newFilePath); !os.IsNotExist(err) {
		// File already exist, return with error
		err := os.Remove(newFilePath)
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
	}

	cases := []testCases{
		{
			input:    "./testdata/rbac-policies.yaml",
			expected: newFilePath,
		},
		{
			input:    "rbac-policies.html",
			expected: "rbac-policies.html is not a valid YAML file",
		},
		{
			input:    "rbac-policiesyaml",
			expected: "rbac-policiesyaml is not a valid YAML file",
		},
	}
	for _, tc := range cases {
		_, got, err := checkAndCreateNewFile(tc.input)
		if err != nil {
			got = err.Error()
		}
		checkResult(t, got, tc.expected)
	}
	err := os.Remove(newFilePath)
	if err != nil {
		t.Errorf(testFailedWithError, err)
	}
}

func TestUpgradeLocalFile(t *testing.T) {
	upgrader := Upgrader{
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
			input:    "./testdata/rbac-policies.yaml",
			expected: "./testdata/rbac-policies-v2-expected.yaml",
		},
	}
	for _, tc := range cases {
		newFile, err := upgrader.upgradeLocalFile(tc.input)
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		gotFileContent, err := ioutil.ReadFile(newFile)
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		expectedFileContent, err := ioutil.ReadFile(tc.expected)
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
		if !reflect.DeepEqual(expectedFileContent, gotFileContent) {
			t.Errorf(testFailedExpectedGot, expectedFileContent, gotFileContent)
		}
		err = os.Remove(newFile)
		if err != nil {
			t.Errorf(testFailedWithError, err)
		}
	}
}

func checkResult(t *testing.T, got, expected string) {
	if got != expected {
		t.Errorf(testFailedExpectedGot, expected, got)
	}
}
