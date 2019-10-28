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

	"istio.io/istio/istioctl/pkg/auth"
	"istio.io/istio/pilot/test/util"
)

func runCommandAndCheckGoldenFile(name, command, golden string, t *testing.T) {
	out, err := runCommand(name, command, t)
	if err != nil {
		t.Errorf("%s: unexpected error: %s", name, err)
	}
	util.CompareContent(out.Bytes(), golden, t)
}

func runCommandAndCheckExpectedString(name, command, expected string, t *testing.T) {
	out, err := runCommand(name, command, t)
	if err != nil {
		t.Fatalf("test %q failed: %v", name, expected)
	}
	if out.String() != expected {
		t.Errorf("test %q failed. \nExpected\n%s\nGot%s\n", name, expected, out.String())
	}
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

func TestAuthCheck(t *testing.T) {
	testCases := []struct {
		name   string
		in     string
		golden string
	}{
		{
			name:   "listeners and clusters",
			in:     "testdata/auth/productpage_config_dump.json",
			golden: "testdata/auth/productpage.golden",
		},
	}

	for _, c := range testCases {
		command := fmt.Sprintf("experimental auth check -f %s", c.in)
		runCommandAndCheckGoldenFile(c.name, command, c.golden, t)
	}
}

func TestAuthValidator(t *testing.T) {
	testCases := []struct {
		name     string
		in       []string
		expected string
	}{
		{
			name:     "good policy",
			in:       []string{"testdata/auth/authz-policy.yaml"},
			expected: auth.GetPolicyValidReport(),
		},
		{
			name: "bad policy",
			in:   []string{"testdata/auth/unused-role.yaml", "testdata/auth/notfound-role-in-binding.yaml"},
			expected: fmt.Sprintf("%s%s",
				auth.GetRoleNotFoundReport("some-role", "bind-service-viewer", "default"),
				auth.GetRoleNotUsedReport("unused-role", "default")),
		},
	}
	for _, c := range testCases {
		command := fmt.Sprintf("experimental auth validate -f %s", strings.Join(c.in, ","))
		runCommandAndCheckExpectedString(c.name, command, c.expected, t)
	}
}

func TestAuthUpgrade(t *testing.T) {
	testCases := []struct {
		name              string
		rbacV1alpha1Files []string
		servicesFiles     []string
		configMapFile     string
		expectedError     string
		golden            string
	}{
		{
			name: "RBAC policy with (unsupported) group field",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/one-rule-one-service.yaml",
				"testdata/auth/upgrade/group-in-subject.yaml", "testdata/auth/upgrade/rbac-global-on.yaml"},
			configMapFile: "testdata/auth/upgrade/istio-configmap.yaml",
			expectedError: "Error: failed to convert policies: cannot convert binding to sources: serviceRoleBinding with group is not supported\n",
		},
		{
			name:              "Missing ClusterRbacConfig",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/one-rule-one-service.yaml"},
			golden:            "testdata/auth/upgrade/empty.yaml",
		},
		{
			name: "One access rule with one service",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/one-rule-one-service.yaml",
				"testdata/auth/upgrade/one-subject.yaml", "testdata/auth/upgrade/rbac-global-on.yaml"},
			servicesFiles: []string{"testdata/auth/upgrade/svc-bookinfo.yaml"},
			configMapFile: "testdata/auth/upgrade/istio-configmap.yaml",
			golden:        "testdata/auth/upgrade/one-rule-one-service.golden.yaml",
		},
		{
			name: "One access rule with all services",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/one-rule-all-services.yaml",
				"testdata/auth/upgrade/two-subjects.yaml", "testdata/auth/upgrade/rbac-global-on.yaml"},
			servicesFiles: []string{"testdata/auth/upgrade/svc-bookinfo.yaml"},
			configMapFile: "testdata/auth/upgrade/istio-configmap.yaml",
			golden:        "testdata/auth/upgrade/one-rule-all-services.golden.yaml",
		},
		{
			name: "Multiple access rules with one subject",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/multiple-access-rules.yaml",
				"testdata/auth/upgrade/one-subject.yaml"},
			servicesFiles: []string{"testdata/auth/upgrade/svc-bookinfo.yaml"},
			golden:        "testdata/auth/upgrade/multiple-access-rules-one-subject.golden.yaml",
		},
		{
			name: "Multiple access rules with two subjects",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/multiple-access-rules.yaml",
				"testdata/auth/upgrade/two-subjects.yaml"},
			servicesFiles: []string{"testdata/auth/upgrade/svc-bookinfo.yaml"},
			golden:        "testdata/auth/upgrade/multiple-access-rules-two-subjects.golden.yaml",
		},
		{
			name: "One access rule with all services with inclusion",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/one-rule-all-services.yaml",
				"testdata/auth/upgrade/two-subjects.yaml", "testdata/auth/upgrade/cluster-rbac-config-on-with-inclusion.yaml"},
			servicesFiles: []string{"testdata/auth/upgrade/svc-bookinfo.yaml"},
			golden:        "testdata/auth/upgrade/one-rule-all-services-with-inclusion.golden.yaml",
		},
		{
			name: "One access rule with all services with exclusion",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/one-rule-all-services.yaml",
				"testdata/auth/upgrade/two-subjects.yaml", "testdata/auth/upgrade/cluster-rbac-config-on-with-exclusion.yaml"},
			servicesFiles: []string{"testdata/auth/upgrade/svc-bookinfo.yaml"},
			configMapFile: "testdata/auth/upgrade/istio-configmap.yaml",
			golden:        "testdata/auth/upgrade/one-rule-all-services-with-exclusion.golden.yaml",
		},
		{
			name: "One access rule with multiple services",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/one-rule-multiple-services.yaml",
				"testdata/auth/upgrade/two-subjects.yaml", "testdata/auth/upgrade/rbac-global-on.yaml"},
			servicesFiles: []string{"testdata/auth/upgrade/svc-bookinfo.yaml"},
			configMapFile: "testdata/auth/upgrade/istio-configmap.yaml",
			golden:        "testdata/auth/upgrade/one-rule-multiple-services.golden.yaml",
		},
		{
			name:              "ClusterRbacConfig only",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/rbac-global-on.yaml"},
			configMapFile:     "testdata/auth/upgrade/istio-configmap.yaml",
			golden:            "testdata/auth/upgrade/rbac-global-on.golden.yaml",
		},
		{
			name:              "RbacConfig_ON_WITH_INCLUSION only",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/cluster-rbac-config-on-with-inclusion.yaml"},
			golden:            "testdata/auth/upgrade/cluster-rbac-config-on-with-inclusion.golden.yaml",
		},
		{
			name:              "RbacConfig_ON_WITH_EXCLUSION only",
			rbacV1alpha1Files: []string{"testdata/auth/upgrade/cluster-rbac-config-on-with-exclusion.yaml"},
			configMapFile:     "testdata/auth/upgrade/istio-configmap.yaml",
			golden:            "testdata/auth/upgrade/cluster-rbac-config-on-with-exclusion.golden.yaml",
		},
	}
	for _, c := range testCases {
		command := fmt.Sprintf("experimental auth upgrade -f %s -s %s -m %s",
			strings.Join(c.rbacV1alpha1Files, ","), strings.Join(c.servicesFiles, ","), c.configMapFile)
		if c.expectedError != "" {
			runCommandAndCheckExpectedCmdError(c.name, command, c.expectedError, t)
		} else {
			runCommandAndCheckGoldenFile(c.name, command, c.golden, t)
		}
	}
}
