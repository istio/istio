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

	"strings"

	"istio.io/istio/tests/util"
)

const (
	rbacEnableTmpl = "testdata/rbac/v1alpha1/istio-rbac-enable.yaml.tmpl"
	rbacRulesTmpl  = "testdata/rbac/v1alpha1/istio-rbac-rules.yaml.tmpl"
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

		{dst: "b", src: "a", path: "/xyz", allow: true},
		{dst: "b", src: "a", path: "/", allow: true},
		{dst: "b", src: "a", port: 90, allow: true},
		{dst: "b", src: "a", port: 9090, allow: true},
		{dst: "b", src: "c", path: "/", allow: true},
		{dst: "b", src: "c", port: 90, allow: true},
		{dst: "b", src: "c", port: 9090, allow: true},
		{dst: "b", src: "d", path: "/", allow: true},
		{dst: "b", src: "d", port: 90, allow: true},
		{dst: "b", src: "d", port: 9090, allow: true},

		{dst: "c", src: "a", path: "/", allow: false},
		{dst: "c", src: "a", path: "/good", allow: false},
		{dst: "c", src: "a", path: "/prefixXYZ", allow: false},
		{dst: "c", src: "a", path: "/xyz/suffix", allow: false},
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
		{dst: "c", src: "d", path: "/good", allow: allow},
		{dst: "c", src: "d", path: "/prefixXYZ", allow: allow},
		{dst: "c", src: "d", path: "/xyz/suffix", allow: allow},
		{dst: "c", src: "d", port: 90, allow: allow},
		{dst: "c", src: "d", port: 9090, allow: false},

		{dst: "d", src: "a", path: "/xyz", allow: allow},
		{dst: "d", src: "a", port: 90, allow: false},
		{dst: "d", src: "a", port: 9090, allow: true},
		{dst: "d", src: "b", path: "/", allow: allow},
		{dst: "d", src: "b", port: 90, allow: false},
		{dst: "d", src: "b", port: 9090, allow: true},
		{dst: "d", src: "c", path: "/", allow: allow},
		{dst: "d", src: "c", port: 90, allow: false},
		{dst: "d", src: "c", port: 9090, allow: true},
	}

	for _, req := range cases {
		for cluster := range tc.Kube.Clusters {
			port := ""
			if req.port != 0 {
				port = fmt.Sprintf(":%d", req.port)
			}
			expectStr := "deny"
			if req.allow {
				expectStr = "allow"
			}
			testName := fmt.Sprintf("%s from %s cluster->%s%s%s[%s]",
				req.src, cluster, req.dst, req.path, port, expectStr)

			runRetriableTest(t, cluster, testName, 30, func() error {
				reqPath := fmt.Sprintf("http://%s%s%s", req.dst, port, req.path)
				if !req.allow && port != "" {
					// There is no response code for TCP service but we can just check the GET request is failed
					// due to EOF.
					err := ClientRequestForError(cluster, req.src, reqPath, 1)
					if err != nil && strings.Contains(err.Error(), fmt.Sprintf("Error Get %s: EOF", reqPath)) {
						return nil
					}
				} else {
					resp := ClientRequest(cluster, req.src, reqPath, 1, "")
					expectCode := "403"
					if req.allow {
						expectCode = "200"
					}
					if len(resp.Code) > 0 && resp.Code[0] == expectCode {
						return nil
					}
				}

				return errAgain
			})
		}
	}
}
