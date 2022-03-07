// Copyright Istio Authors
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

package model

import (
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"

	authzpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestModel_MigrateTrustDomain(t *testing.T) {
	cases := []struct {
		name     string
		tdBundle trustdomain.Bundle
		rule     *authzpb.Rule
		want     []string
		notWant  []string
	}{
		{
			name:     "no-source-principal",
			tdBundle: trustdomain.NewBundle("td-1", []string{"td-2"}),
			rule: yamlRule(t, `
from:
- source:
    requestPrincipals: ["td-1/ns/foo/sa/sleep"]
`),
			want: []string{
				"td-1/ns/foo/sa/sleep",
			},
			notWant: []string{
				"td-2/ns/foo/sa/sleep",
			},
		},
		{
			name:     "source-principal-field",
			tdBundle: trustdomain.NewBundle("td-1", []string{"td-2"}),
			rule: yamlRule(t, `
from:
- source:
    principals: ["td-1/ns/foo/sa/sleep"]
`),
			want: []string{
				"td-1/ns/foo/sa/sleep",
				"td-2/ns/foo/sa/sleep",
			},
		},
		{
			name:     "source-principal-attribute",
			tdBundle: trustdomain.NewBundle("td-1", []string{"td-2"}),
			rule: yamlRule(t, `
when:
- key: source.principal
  values: ["td-1/ns/foo/sa/sleep"]
`),
			want: []string{
				"td-1/ns/foo/sa/sleep",
				"td-2/ns/foo/sa/sleep",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.rule)
			if err != nil {
				t.Fatal(err)
			}
			got.MigrateTrustDomain(tc.tdBundle)
			gotStr := spew.Sdump(got)
			for _, want := range tc.want {
				if !strings.Contains(gotStr, want) {
					t.Errorf("got %s but not found %s", gotStr, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(gotStr, notWant) {
					t.Errorf("got %s but not want %s", gotStr, notWant)
				}
			}
		})
	}
}

func TestModel_Generate(t *testing.T) {
	rule := yamlRule(t, `
from:
- source:
    requestPrincipals: ["td-1/ns/foo/sa/sleep-1"]
    notRequestPrincipals: ["td-1/ns/foo/sa/sleep-2"]
- source:
    requestPrincipals: ["td-1/ns/foo/sa/sleep-3"]
    notRequestPrincipals: ["td-1/ns/foo/sa/sleep-4"]
to:
- operation:
    ports: ["8001"]
    notPorts: ["8002"]
- operation:
    ports: ["8003"]
    notPorts: ["8004"]
when:
- key: "source.principal"
  values: ["td-1/ns/foo/sa/httpbin-1"]
  notValues: ["td-1/ns/foo/sa/httpbin-2"]
- key: "destination.ip"
  values: ["10.0.0.1"]
  notValues: ["10.0.0.2"]
`)

	cases := []struct {
		name    string
		forTCP  bool
		action  rbacpb.RBAC_Action
		rule    *authzpb.Rule
		want    []string
		notWant []string
	}{
		{
			name:   "allow-http",
			action: rbacpb.RBAC_ALLOW,
			rule:   rule,
			want: []string{
				"td-1/ns/foo/sa/sleep-1",
				"td-1/ns/foo/sa/sleep-2",
				"td-1/ns/foo/sa/sleep-3",
				"td-1/ns/foo/sa/sleep-4",
				"td-1/ns/foo/sa/httpbin-1",
				"td-1/ns/foo/sa/httpbin-2",
				"8001",
				"8002",
				"8003",
				"8004",
				"10.0.0.1",
				"10.0.0.2",
			},
		},
		{
			name:   "allow-tcp",
			action: rbacpb.RBAC_ALLOW,
			forTCP: true,
			rule:   rule,
			notWant: []string{
				"td-1/ns/foo/sa/sleep-1",
				"td-1/ns/foo/sa/sleep-2",
				"td-1/ns/foo/sa/sleep-3",
				"td-1/ns/foo/sa/sleep-4",
				"td-1/ns/foo/sa/httpbin-1",
				"td-1/ns/foo/sa/httpbin-2",
				"8001",
				"8002",
				"8003",
				"8004",
				"10.0.0.1",
				"10.0.0.2",
			},
		},
		{
			name:   "deny-http",
			action: rbacpb.RBAC_DENY,
			rule:   rule,
			want: []string{
				"td-1/ns/foo/sa/sleep-1",
				"td-1/ns/foo/sa/sleep-2",
				"td-1/ns/foo/sa/sleep-3",
				"td-1/ns/foo/sa/sleep-4",
				"td-1/ns/foo/sa/httpbin-1",
				"td-1/ns/foo/sa/httpbin-2",
				"8001",
				"8002",
				"8003",
				"8004",
				"10.0.0.1",
				"10.0.0.2",
			},
		},
		{
			name:   "deny-tcp",
			action: rbacpb.RBAC_DENY,
			forTCP: true,
			rule:   rule,
			want: []string{
				"8001",
				"8002",
				"8003",
				"8004",
				"10.0.0.1",
				"10.0.0.2",
				"td-1/ns/foo/sa/httpbin-1",
				"td-1/ns/foo/sa/httpbin-2",
			},
			notWant: []string{
				"td-1/ns/foo/sa/sleep-1",
				"td-1/ns/foo/sa/sleep-2",
				"td-1/ns/foo/sa/sleep-3",
				"td-1/ns/foo/sa/sleep-4",
			},
		},
		{
			name:   "audit-http",
			action: rbacpb.RBAC_LOG,
			rule:   rule,
			want: []string{
				"td-1/ns/foo/sa/sleep-1",
				"td-1/ns/foo/sa/sleep-2",
				"td-1/ns/foo/sa/sleep-3",
				"td-1/ns/foo/sa/sleep-4",
				"td-1/ns/foo/sa/httpbin-1",
				"td-1/ns/foo/sa/httpbin-2",
				"8001",
				"8002",
				"8003",
				"8004",
				"10.0.0.1",
				"10.0.0.2",
			},
		},
		{
			name:   "audit-tcp",
			action: rbacpb.RBAC_LOG,
			forTCP: true,
			rule:   rule,
			want: []string{
				"8001",
				"8002",
				"8003",
				"8004",
				"10.0.0.1",
				"10.0.0.2",
				"td-1/ns/foo/sa/httpbin-1",
				"td-1/ns/foo/sa/httpbin-2",
			},
			notWant: []string{
				"td-1/ns/foo/sa/sleep-1",
				"td-1/ns/foo/sa/sleep-2",
				"td-1/ns/foo/sa/sleep-3",
				"td-1/ns/foo/sa/sleep-4",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := New(tc.rule)
			if err != nil {
				t.Fatal(err)
			}
			p, _ := m.Generate(tc.forTCP, tc.action)
			var gotYaml string
			if p != nil {
				if gotYaml, err = protomarshal.ToYAML(p); err != nil {
					t.Fatalf("%s: failed to parse yaml: %s", tc.name, err)
				}
			}

			for _, want := range tc.want {
				if !strings.Contains(gotYaml, want) {
					t.Errorf("got:\n%s but not found %s", gotYaml, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(gotYaml, notWant) {
					t.Errorf("got:\n%s but not want %s", gotYaml, notWant)
				}
			}
		})
	}
}

func yamlRule(t *testing.T, yaml string) *authzpb.Rule {
	t.Helper()
	p := &authzpb.Rule{}
	if err := gogoprotomarshal.ApplyYAML(yaml, p); err != nil {
		t.Fatalf("failed to parse yaml: %s", err)
	}
	return p
}
