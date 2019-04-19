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
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"
)

// Test data
const (
	serviceRole = `
apiVersion: "rbac.istio.io/v1alpha1"
kind: ServiceRole
metadata:
  name: service-viewer
  namespace: default
spec:
  rules:
  - services: ["productpage.svc.cluster.local"]
`
	serviceRole2 = `
apiVersion: "rbac.istio.io/v1alpha1"
kind: ServiceRole
metadata:
  name: service-viewer-2
  namespace: default
spec:
  rules:
  - services: ["productpage.svc.cluster.local"]
    methods: ["GET"]
`
	serviceRoleBinding = `
apiVersion: "rbac.istio.io/v1alpha1"
kind: ServiceRoleBinding
metadata:
  name: bind-service-viewer
  namespace: default
spec:
  subjects:
  - properties:
      source.namespace: "default"
  roleRef:
    kind: ServiceRole
    name:    'service-viewer'
`
)

const (
	testFailedWithError   = "test failed with error %v"
	testFailedExpectedGot = "test failed, expected %s, got %s"
)

type testCases struct {
	input     string
	fieldType string
	expected  string
}

func TestGetRoleName(t *testing.T) {
	cases := []testCases{
		{
			input:    serviceRole,
			expected: "service-viewer",
		},
	}
	for _, tc := range cases {
		got := getRoleName(tc.input)
		checkResult(t, got, tc.expected)
	}
}

func TestGetNamespace(t *testing.T) {
	cases := []testCases{
		{
			input:    serviceRole,
			expected: "default",
		},
	}
	for _, tc := range cases {
		got := getNamespace(tc.input)
		checkResult(t, got, tc.expected)
	}
}

func TestReplaceUserOrGroupField(t *testing.T) {
	cases := []testCases{
		{
			input:     `- user: "foo"`,
			fieldType: userField,
			expected:  fmt.Sprintf("%s%s", oneLevelIndentation, `- names: ["foo"]`),
		},
		{
			input:     `group: "my_group"`,
			fieldType: groupField,
			expected:  fmt.Sprintf("%s%s", twoLevelsIndentation, `groups: ["my_group"]`),
		},
	}
	for _, tc := range cases {
		got := replaceUserOrGroupField(tc.fieldType, tc.input)
		checkResult(t, got, tc.expected)
	}
}

func TestGetAnotherFirstClassField(t *testing.T) {
	cases := []testCases{
		{
			input:    serviceRole,
			expected: "",
		},
		{
			input:    serviceRole2,
			expected: fmt.Sprintf("%s%s", twoLevelsIndentation, `methods: ["GET"]`),
		},
	}
	for _, tc := range cases {
		got := getAnotherFirstClassField(tc.input)
		checkResult(t, got, tc.expected)
	}
}

func TestGetServiceRoleFromBinding(t *testing.T) {
	cases := []testCases{
		{
			input:    serviceRoleBinding,
			expected: "service-viewer",
		},
	}
	for _, tc := range cases {
		got := getServiceRoleFromBinding(tc.input)
		checkResult(t, got, tc.expected)
	}
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
		RoleToWorkloadLabels: make(map[string]WorkloadLabels),
	}
	// Data from the BookExample. productpage.svc.cluster.local is the service with pod label
	// app: productpage.
	upgrader.RoleToWorkloadLabels["service-viewer"] = map[string]string{
		"app": "productpage",
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
