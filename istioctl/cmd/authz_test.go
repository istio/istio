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

package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func runCommandAndCheckGoldenFile(name, command, golden string, t *testing.T) {
	out, err := runCommand(name, command, t)
	if err != nil {
		t.Errorf("%s: unexpected error: %s", name, err)
	}
	util.CompareContent(out.Bytes(), golden, t)
}

func runCommandAndCheckExpectedCmdError(name, command, expected string, t *testing.T) {
	out, err := runCommand(name, command, t)
	if err == nil {
		t.Fatalf("test %q failed. Expected error: %v", name, expected)
	}
	if out.Len() != 0 {
		if out.String() != expected {
			t.Fatalf("test %q failed. \nExpected\n%s\nGot\n%s\n", name, expected, out.String())
		}
	} else {
		t.Fatalf("test %q failed. Expected error: %v", name, expected)
	}
}

func runCommand(name, command string, t *testing.T) (bytes.Buffer, error) {
	t.Helper()
	var out bytes.Buffer
	rootCmd := GetRootCmd(strings.Split(command, " "))
	rootCmd.SetOutput(&out)

	err := rootCmd.Execute()
	if err != nil {
		return out, fmt.Errorf("%s: unexpected error: %s", name, err)
	}
	return out, nil
}

func TestAuthZCheck(t *testing.T) {
	testCases := []struct {
		name   string
		in     string
		golden string
	}{
		{
			name:   "listeners and clusters",
			in:     "testdata/authz/productpage_config_dump.json",
			golden: "testdata/authz/productpage.golden",
		},
	}

	for _, c := range testCases {
		command := fmt.Sprintf("experimental authz check -f %s", c.in)
		runCommandAndCheckGoldenFile(c.name, command, c.golden, t)
	}
}

func TestAuthZConvert(t *testing.T) {
	testCases := []struct {
		name              string
		rbacV1alpha1Files []string
		servicesFiles     []string
		configMapFile     string
		expectedError     string
		golden            string
	}{
		{
			name: "One access rule with multiple services",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-multiple-services.yaml",
				"testdata/authz/converter/two-subjects.yaml",
				"testdata/authz/converter/rbac-global-on.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/one-rule-multiple-services.golden.yaml",
		},
		{
			name: "RBAC policy with (unsupported) group field",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-one-service.yaml",
				"testdata/authz/converter/group-in-subject.yaml",
				"testdata/authz/converter/rbac-global-on.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			expectedError: "Error: failed to convert policies: cannot convert binding to sources: serviceRoleBinding with group is not supported\n",
		},
		{
			name: "Missing ClusterRbacConfig",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-one-service.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/empty.yaml",
		},
		{
			name: "One access rule with one service",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-one-service.yaml",
				"testdata/authz/converter/one-subject.yaml",
				"testdata/authz/converter/rbac-global-on.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/one-rule-one-service.golden.yaml",
		},
		{
			name: "One access rule with two services of prefix and suffix",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-two-services-prefix-suffix.yaml",
				"testdata/authz/converter/one-subject.yaml",
				"testdata/authz/converter/rbac-global-on.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-prefix-suffix.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/one-rule-two-services-prefix-suffix.golden.yaml",
		},
		{
			name: "One access rule with all services",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-all-services.yaml",
				"testdata/authz/converter/two-subjects.yaml",
				"testdata/authz/converter/rbac-global-on.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/one-rule-all-services.golden.yaml",
		},
		{
			name: "One access rule with all services with inclusion",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-all-services.yaml",
				"testdata/authz/converter/two-subjects.yaml",
				"testdata/authz/converter/cluster-rbac-config-on-with-inclusion.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/one-rule-all-services-with-inclusion.golden.yaml",
		},
		{
			name: "One access rule with all services with exclusion",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/one-rule-all-services.yaml",
				"testdata/authz/converter/two-subjects.yaml",
				"testdata/authz/converter/cluster-rbac-config-on-with-exclusion.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/one-rule-all-services-with-exclusion.golden.yaml",
		},

		{
			name: "ClusterRbacConfig only",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/rbac-global-on.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/rbac-global-on.golden.yaml",
		},
		{
			name: "RbacConfig_ON_WITH_INCLUSION only",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/cluster-rbac-config-on-with-inclusion.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/cluster-rbac-config-on-with-inclusion.golden.yaml",
		},
		{
			name: "RbacConfig_ON_WITH_EXCLUSION only",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/cluster-rbac-config-on-with-exclusion.yaml",
			},
			configMapFile: "testdata/authz/converter/istio-configmap.yaml",
			golden:        "testdata/authz/converter/cluster-rbac-config-on-with-exclusion.golden.yaml",
		},
		{
			name: "Multiple access rules with one subject",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/multiple-access-rules.yaml",
				"testdata/authz/converter/one-subject.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			golden: "testdata/authz/converter/multiple-access-rules-one-subject.golden.yaml",
		},
		{
			name: "Multiple access rules with two subjects",
			rbacV1alpha1Files: []string{
				"testdata/authz/converter/multiple-access-rules.yaml",
				"testdata/authz/converter/two-subjects.yaml",
			},
			servicesFiles: []string{
				"testdata/authz/converter/svc-bookinfo.yaml",
			},
			golden: "testdata/authz/converter/multiple-access-rules-two-subjects.golden.yaml",
		},
	}
	for _, c := range testCases {
		// cleanupForTest clean the values of policyFiles and serviceFiles. Otherwise, the variables will be
		// appended with new values
		policyFiles = nil
		serviceFiles = nil

		command := fmt.Sprintf("experimental authz convert -f %s -s %s -m %s",
			strings.Join(c.rbacV1alpha1Files, ","), strings.Join(c.servicesFiles, ","), c.configMapFile)
		t.Run(c.name, func(t *testing.T) {
			if c.expectedError != "" {
				runCommandAndCheckExpectedCmdError(c.name, command, c.expectedError, t)
			} else {
				runCommandAndCheckGoldenFile(c.name, command, c.golden, t)
			}
		})
	}
}
