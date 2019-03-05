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

package pilot

import (
	"testing"

	"istio.io/istio/tests/util"
)

const (
	rbacV2RulesTmpl = "testdata/rbac/v1alpha1/istio-rbac-v2-rules.yaml.tmpl"
)

func TestRBACV2(t *testing.T) {
	if !tc.Kube.RBACEnabled {
		t.Skipf("Skipping %s: rbac_enable=false", t.Name())
	}
	// Fill out the templates.
	params := map[string]string{
		"IstioNamespace": tc.Kube.IstioSystemNamespace(),
		"Namespace":      tc.Kube.Namespace,
	}
	rbacEnableYaml, err := util.CreateAndFill(tc.Info.TempDir, rbacEnableTmpl, params)
	if err != nil {
		t.Fatal(err)
	}
	rbacV2RulesYaml, err := util.CreateAndFill(tc.Info.TempDir, rbacV2RulesTmpl, params)
	if err != nil {
		t.Fatal(err)
	}

	// Push all of the configs
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{rbacEnableYaml, rbacV2RulesYaml},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	// Some services are only accessible when auth is enabled.
	allow := false
	if tc.Kube.AuthEnabled {
		allow = true
	}

	cases := []struct {
		dst   string
		src   string
		path  string
		port  uint32
		allow bool
	}{
		{dst: "a", src: "b", path: "/xyz", allow: false},
		{dst: "a", src: "b", port: 90, allow: false},
		{dst: "a", src: "b", port: 9090, allow: false},
		{dst: "a", src: "c", path: "/", allow: false},
		{dst: "a", src: "c", port: 90, allow: false},
		{dst: "a", src: "c", port: 9090, allow: false},
		{dst: "a", src: "d", path: "/", allow: false},
		{dst: "a", src: "d", port: 90, allow: false},
		{dst: "a", src: "d", port: 9090, allow: false},

		{dst: "b", src: "a", path: "/xyz", allow: allow},
		{dst: "b", src: "a", path: "/secret", allow: false},
		{dst: "b", src: "a", port: 90, allow: allow},
		{dst: "b", src: "a", port: 9090, allow: allow},
		{dst: "b", src: "c", path: "/", allow: allow},
		{dst: "b", src: "c", port: 90, allow: allow},
		{dst: "b", src: "c", port: 3000, allow: false},
		{dst: "b", src: "d", path: "/", allow: allow},
		{dst: "b", src: "d", port: 90, allow: allow},
		{dst: "b", src: "d", port: 9090, allow: allow},

		{dst: "c", src: "a", path: "/", allow: allow},
		{dst: "c", src: "a", path: "/good", allow: allow},
		{dst: "c", src: "a", path: "/credentials/admin", allow: false},
		{dst: "c", src: "a", path: "/secrets/admin", allow: false},
		{dst: "c", src: "a", port: 90, allow: false},
		{dst: "c", src: "a", port: 9090, allow: false},

		{dst: "c", src: "b", path: "/", allow: false},
		{dst: "c", src: "b", path: "/good", allow: false},
		{dst: "c", src: "b", path: "/prefixXYZ", allow: false},
		{dst: "c", src: "b", path: "/xyz/suffix", allow: false},
		{dst: "c", src: "b", port: 90, allow: false},
		{dst: "c", src: "b", port: 9090, allow: false},

		{dst: "c", src: "d", path: "/", allow: false},
		{dst: "c", src: "d", path: "/xyz", allow: false},
		{dst: "c", src: "d", path: "/good", allow: false},
		{dst: "c", src: "d", path: "/prefixXYZ", allow: false},
		{dst: "c", src: "d", path: "/xyz/suffix", allow: false},
		{dst: "c", src: "d", port: 90, allow: false},
		{dst: "c", src: "d", port: 9090, allow: false},

		{dst: "d", src: "a", path: "/xyz", allow: true},
		{dst: "d", src: "a", port: 90, allow: false},
		{dst: "d", src: "a", port: 9090, allow: true},
		{dst: "d", src: "b", path: "/", allow: true},
		{dst: "d", src: "b", port: 90, allow: false},
		{dst: "d", src: "b", port: 9090, allow: true},
		{dst: "d", src: "c", path: "/", allow: true},
		{dst: "d", src: "c", port: 90, allow: false},
		{dst: "d", src: "c", port: 9090, allow: true},
	}

	runRbacTestCases(t, cases)
}
