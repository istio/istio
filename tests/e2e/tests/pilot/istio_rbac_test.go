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

	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type istioRBAC struct {
	*tutil.Environment
	rbacEnableYaml string
	rbacRulesYaml  string
}

const (
	istioRBACEnableTmpl = "v1alpha2/istio-rbac-enable.yaml.tmpl"
	istioRBACRulesTmpl  = "v1alpha2/istio-rbac-rules.yaml.tmpl"
)

func (t *istioRBAC) String() string {
	return "istio-rbac"
}

func (t *istioRBAC) Setup() error {
	yamlEnable, err := t.Fill(istioRBACEnableTmpl, t.ToTemplateData())
	if err != nil {
		return err
	}
	yamlRules, err := t.Fill(istioRBACRulesTmpl, t.ToTemplateData())
	if err != nil {
		return err
	}

	if err = t.KubeApply(yamlEnable, t.Config.IstioNamespace); err != nil {
		log.Warn("Failed to enable istio RBAC")
		return err
	}
	log.Info("Istio RBAC enabled")
	t.rbacEnableYaml = yamlEnable

	if err = t.KubeApply(yamlRules, t.Config.Namespace); err != nil {
		log.Warn("Failed to apply istio RBAC rules")
		return err
	}
	log.Info("Istio RBAC rules applied")
	t.rbacRulesYaml = yamlRules

	return nil
}

func (t *istioRBAC) Teardown() {
	// It's safe to keep the rules for debugging purpose, they have no effect after the rbacEnableYaml
	// is deleted.
	if len(t.rbacRulesYaml) > 0 && !t.Config.SkipCleanup && !t.Config.SkipCleanupOnFailure {
		if err := t.KubeDelete(t.rbacRulesYaml, t.Config.Namespace); err != nil {
			log.Infof("Failed to delete istio RBAC rules: %v", err)
		}
	}

	if len(t.rbacEnableYaml) > 0 {
		if err := t.KubeDelete(t.rbacEnableYaml, t.Config.IstioNamespace); err != nil {
			log.Errorf("Failed to disable istio RBAC: %v", err)
		}
	}
}

func (t *istioRBAC) Run() error {
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

	funcs := make(map[string]func() tutil.Status)
	for _, req := range cases {
		name := fmt.Sprintf("Istio RBAC: request for %+v", req)
		funcs[name] = (func(src, dst, path, expect string) func() tutil.Status {
			return func() tutil.Status {
				resp := t.ClientRequest(src, fmt.Sprintf("http://%s%s", dst, path), 1, "")
				if len(resp.Code) > 0 && resp.Code[0] == expect {
					return nil
				}
				return tutil.ErrAgain
			}
		})(req.src, req.dst, req.path, req.expect)
	}

	return tutil.Parallel(funcs)
}
