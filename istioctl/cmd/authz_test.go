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
	"io/ioutil"
	"reflect"
	"strings"
	"testing"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/test/util"
)

func runCommandWantPolicy(command, want string, t *testing.T) {
	t.Helper()
	out, err := runCommand(command, t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	data, err := ioutil.ReadFile(want)
	if err != nil {
		t.Fatalf("failed to read input yaml file: %v", err)
	}
	wantPolicies := getPolicies(string(data), t)
	gotPolicies := getPolicies(out.String(), t)
	if !reflect.DeepEqual(gotPolicies.NamespaceToV1beta1Policies, wantPolicies.NamespaceToV1beta1Policies) {
		t.Logf("generated policy:\n%s\n", out.String())
		t.Errorf("want policies:\n%s\n got policies:\n%s\n",
			wantPolicies.NamespaceToV1beta1Policies, gotPolicies.NamespaceToV1beta1Policies)
	}
}

func getPolicies(data string, t *testing.T) *model.AuthorizationPolicies {
	t.Helper()
	c, _, err := crd.ParseInputs(data)
	if err != nil {
		t.Fatalf("failde to parse CRD: %v", err)
	}
	var configs []*model.Config
	for i := range c {
		configs = append(configs, &c[i])
	}
	return policy.NewAuthzPolicies(configs, t)
}

func runCommandWantOutput(command, golden string, t *testing.T) {
	t.Helper()
	out, err := runCommand(command, t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	util.CompareContent(out.Bytes(), golden, t)
}

func runCommandWantError(command, wantError string, t *testing.T) {
	t.Helper()
	_, err := runCommand(command, t)
	if err == nil {
		t.Errorf("want error: %v but got nil", wantError)
	} else if err.Error() != wantError {
		t.Errorf("want error: %v\n got error: %s", wantError, err)
	}
}

func runCommand(command string, t *testing.T) (bytes.Buffer, error) {
	t.Helper()
	out := bytes.Buffer{}
	rootCmd := GetRootCmd(strings.Split(command, " "))
	rootCmd.SetOut(&out)
	return out, rootCmd.Execute()
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
		runCommandWantOutput(command, c.golden, t)
	}
}

func TestAuthZConvert(t *testing.T) {
	testCases := []struct {
		name      string
		policies  []string
		services  []string
		flag      string
		want      string
		wantError string
	}{
		{
			name:     "simple",
			policies: []string{"testdata/authz/converter/policy-simple.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-simple-want.yaml",
		},
		{
			name:     "wildcard",
			policies: []string{"testdata/authz/converter/policy-wildcard.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-wildcard-want.yaml",
		},
		{
			name:     "multiple-rules",
			policies: []string{"testdata/authz/converter/policy-multiple-rules.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-multiple-rules-want.yaml",
		},
		{
			name:     "multiple-roles",
			policies: []string{"testdata/authz/converter/policy-multiple-roles.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-multiple-roles-want.yaml",
		},
		{
			name:     "multiple-bindings",
			policies: []string{"testdata/authz/converter/policy-multiple-bindings.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-multiple-bindings-want.yaml",
		},
		{
			name:     "destination-labels",
			policies: []string{"testdata/authz/converter/policy-destination-labels.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-destination-labels-want.yaml",
		},
		{
			name:     "multiple-services",
			policies: []string{"testdata/authz/converter/policy-multiple-services.yaml"},
			services: []string{"testdata/authz/converter/service-httpbin.yaml"},
			want:     "testdata/authz/converter/policy-multiple-services-want.yaml",
		},

		{
			name:     "clusterRbacConfig-off",
			policies: []string{"testdata/authz/converter/policy-clusterRbacConfig-off.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-clusterRbacConfig-off-want.yaml",
		},
		{
			name:     "clusterRbacConfig-inclusion-namespace",
			policies: []string{"testdata/authz/converter/policy-clusterRbacConfig-inclusion-namespace.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-clusterRbacConfig-inclusion-namespace-want.yaml",
		},
		{
			name:     "clusterRbacConfig-inclusion-service",
			policies: []string{"testdata/authz/converter/policy-clusterRbacConfig-inclusion-service.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-clusterRbacConfig-inclusion-service-want.yaml",
		},
		{
			name:     "clusterRbacConfig-exclusion-namespace",
			policies: []string{"testdata/authz/converter/policy-clusterRbacConfig-exclusion-namespace.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-clusterRbacConfig-exclusion-namespace-want.yaml",
		},
		{
			name:     "clusterRbacConfig-exclusion-service",
			policies: []string{"testdata/authz/converter/policy-clusterRbacConfig-exclusion-service.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			want:     "testdata/authz/converter/policy-clusterRbacConfig-exclusion-service-want.yaml",
		},

		{
			name:     "ignore-no-clusterRbacConfig",
			policies: []string{"testdata/authz/converter/policy-ignore-no-clusterRbacConfig.yaml"},
			services: []string{"testdata/authz/converter/service-bookinfo.yaml"},
			flag:     "--allowNoClusterRbacConfig",
			want:     "testdata/authz/converter/policy-ignore-no-clusterRbacConfig-want.yaml",
		},
		{
			name:      "error-no-clusterRbacConfig",
			policies:  []string{"testdata/authz/converter/error-no-clusterRbacConfig.yaml"},
			services:  []string{"testdata/authz/converter/service-bookinfo.yaml"},
			wantError: "no ClusterRbacConfig",
		},
		{
			name:      "error-unsupported-destination-user",
			policies:  []string{"testdata/authz/converter/error-unsupported-destination-user.yaml"},
			services:  []string{"testdata/authz/converter/service-bookinfo.yaml"},
			wantError: "destination.user is no longer supported in v1beta1 authorization policy",
		},
		{
			name:      "error-unsupported-destination-ip",
			policies:  []string{"testdata/authz/converter/error-unsupported-destination-ip.yaml"},
			services:  []string{"testdata/authz/converter/service-bookinfo.yaml"},
			wantError: "destination.ip is no longer supported in v1beta1 authorization policy",
		},
		{
			name:      "error-duplicate-workload-label",
			policies:  []string{"testdata/authz/converter/error-duplicate-workload-label.yaml"},
			services:  []string{"testdata/authz/converter/service-bookinfo.yaml"},
			wantError: `duplicate workload label "app" with conflict values: "productpage", "reviews"`,
		},
	}
	for _, c := range testCases {
		// Clean the flags. Otherwise, the variables will be appended with new values.
		v1Files = nil
		serviceFiles = nil
		allowNoClusterRbacConfig = false

		command := fmt.Sprintf("experimental authz convert -f %s -s %s -r my-root-namespace %s",
			strings.Join(c.policies, ","), strings.Join(c.services, ","), c.flag)
		t.Run(c.name, func(t *testing.T) {
			if c.wantError != "" {
				runCommandWantError(command, c.wantError, t)
			} else {
				runCommandWantPolicy(command, c.want, t)
			}
		})
	}
}
