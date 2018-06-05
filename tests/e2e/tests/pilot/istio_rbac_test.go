// Copyright 2018 Istio Authors
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
	"fmt"
	"testing"

	"istio.io/istio/tests/util"
)

const (
	rbacEnableTmpl = "testdata/v1alpha2/istio-rbac-enable.yaml.tmpl"
	rbacRulesTmpl  = "testdata/v1alpha2/istio-rbac-rules.yaml.tmpl"
)

func TestRBAC(t *testing.T) {
	if !tc.Kube.RBACEnabled {
		t.Skipf("Skipping %s: rbac_enable=false", t.Name())
	}
	// Fill out the templates.
	params := map[string]string{
		"IstioNamespace": tc.Kube.IstioSystemNamespace(),
		"Namespace":      tc.Kube.Namespace,
	}
	rbackEnableYaml, err := util.CreateAndFill(tc.Info.TempDir, rbacEnableTmpl, params)
	if err != nil {
		t.Fatal(err)
	}
	rbackRulesYaml, err := util.CreateAndFill(tc.Info.TempDir, rbacRulesTmpl, params)
	if err != nil {
		t.Fatal(err)
	}

	// Push all of the configs
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{rbackEnableYaml, rbackRulesYaml},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	cases := []struct {
		dst    string
		src    string
		path   string
		expect string
	}{
		{dst: "a", src: "b", path: "/xyz", expect: "403"},
		{dst: "a", src: "c", path: "/", expect: "403"},
		{dst: "a", src: "d", path: "/", expect: "403"},

		{dst: "b", src: "a", path: "/xyz", expect: "200"},
		{dst: "b", src: "a", path: "/", expect: "200"},
		{dst: "b", src: "c", path: "/", expect: "200"},
		{dst: "b", src: "d", path: "/", expect: "200"},

		{dst: "c", src: "a", path: "/", expect: "403"},
		{dst: "c", src: "a", path: "/good", expect: "403"},
		{dst: "c", src: "a", path: "/prefixXYZ", expect: "403"},
		{dst: "c", src: "a", path: "/xyz/suffix", expect: "403"},

		{dst: "c", src: "b", path: "/", expect: "403"},
		{dst: "c", src: "b", path: "/good", expect: "403"},
		{dst: "c", src: "b", path: "/prefixXYZ", expect: "403"},
		{dst: "c", src: "b", path: "/xyz/suffix", expect: "403"},

		{dst: "c", src: "d", path: "/", expect: "403"},
		{dst: "c", src: "d", path: "/xyz", expect: "403"},
		{dst: "c", src: "d", path: "/good", expect: "200"},
		{dst: "c", src: "d", path: "/prefixXYZ", expect: "200"},
		{dst: "c", src: "d", path: "/xyz/suffix", expect: "200"},

		{dst: "d", src: "a", path: "/xyz", expect: "200"},
		{dst: "d", src: "b", path: "/", expect: "200"},
		{dst: "d", src: "c", path: "/", expect: "200"},
	}

	for _, req := range cases {
		for cluster := range tc.Kube.Clusters {
			testName := fmt.Sprintf("%s from %s cluster->%s%s[%s]", req.src, cluster, req.dst, req.path, req.expect)
			runRetriableTest(t, cluster, testName, defaultRetryBudget, func() error {
				resp := ClientRequest(cluster, req.src, fmt.Sprintf("http://%s%s", req.dst, req.path), 1, "")
				if len(resp.Code) > 0 && resp.Code[0] == req.expect {
					return nil
				}
				return errAgain
			})
		}
	}
}
