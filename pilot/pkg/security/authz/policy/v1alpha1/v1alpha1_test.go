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

package v1alpha1

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"
)

func TestV1alpha1Generator_Generate(t *testing.T) {
	serviceFoo := "foo"
	namespaceA := "a"
	serviceBar := "bar"
	namespaceB := "b"
	serviceFooInNamespaceA := policy.NewServiceMetadata("foo.a.svc.cluster.local", nil, t)
	// TODO(pitlv2109): Also include trust domain and trust domain aliases to existing tests.
	testCases := []struct {
		name                      string
		policies                  []*model.Config
		trustDomainBundle         trustdomain.Bundle
		isGlobalPermissiveEnabled bool
		forTCPFilter              bool
		wantRules                 map[string][]string
		wantShadowRules           map[string][]string
	}{
		{
			name: "no policy",
		},
		{
			name: "no role in namespace a",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceB, serviceFoo),
				policy.SimpleBinding("binding", namespaceB, "role"),
			},
		},
		{
			name: "no role matched for service foo",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceBar),
				policy.SimpleBinding("binding", namespaceA, "role"),
			},
		},
		{
			name: "no role matched for service foo: global permissive",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceBar),
				policy.SimpleBinding("binding", namespaceA, "role"),
			},
			isGlobalPermissiveEnabled: true,
		},
		{
			name: "no binding for service foo",
			policies: []*model.Config{
				policy.SimpleRole("role-1", namespaceA, serviceFoo),
				policy.SimpleRole("role-2", namespaceA, serviceBar),
				policy.SimpleBinding("binding-2", namespaceA, "role-2"),
				policy.SimpleRole("role-3", namespaceB, serviceFoo),
				policy.SimpleBinding("binding-3", namespaceB, "role-3"),
			},
		},
		{
			name: "one role and one binding",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBinding("binding", namespaceA, "role"),
			},
			wantRules: map[string][]string{
				"role": {policy.RoleTag("role"), policy.BindingTag("binding")},
			},
		},
		{
			name: "one role and one binding: forTCPFilter",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBinding("binding", namespaceA, "role"),
			},
			forTCPFilter: true,
		},
		{
			name: "one role and one binding: permissive",
			policies: []*model.Config{
				policy.SimpleRole("role-1", namespaceA, serviceFoo),
				policy.SimpleBinding("binding-1", namespaceA, "role-1"),
				policy.SimpleRole("role-2", namespaceA, serviceFoo),
				policy.SimplePermissiveBinding("binding-2", namespaceA, "role-2"),
			},
			wantRules: map[string][]string{
				"role-1": {
					policy.RoleTag("role-1"),
					policy.BindingTag("binding-1"),
				},
			},
			wantShadowRules: map[string][]string{
				"role-2": {
					policy.RoleTag("role-2"),
					policy.BindingTag("binding-2"),
				},
			},
		},
		{
			name: "one role and two bindings",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBinding("binding-1", namespaceA, "role"),
				policy.SimpleBinding("binding-2", namespaceA, "role"),
			},
			wantRules: map[string][]string{
				"role": {
					policy.RoleTag("role"),
					policy.BindingTag("binding-1"),
					policy.BindingTag("binding-2"),
				},
			},
		},
		{
			name: "two roles and two bindings",
			policies: []*model.Config{
				policy.SimpleRole("role-1", namespaceA, serviceFoo),
				policy.SimpleBinding("binding-1", namespaceA, "role-1"),
				policy.SimpleRole("role-2", namespaceA, serviceFoo),
				policy.SimpleBinding("binding-2", namespaceA, "role-2"),
			},
			wantRules: map[string][]string{
				"role-1": {
					policy.RoleTag("role-1"),
					policy.BindingTag("binding-1"),
				},
				"role-2": {
					policy.RoleTag("role-2"),
					policy.BindingTag("binding-2"),
				},
			},
		},
		{
			name: "two roles and two bindings: global permissive",
			policies: []*model.Config{
				policy.SimpleRole("role-1", namespaceA, serviceFoo),
				policy.SimpleBinding("binding-1", namespaceA, "role-1"),
				policy.SimpleRole("role-2", namespaceA, serviceFoo),
				policy.SimpleBinding("binding-2", namespaceA, "role-2"),
			},
			wantShadowRules: map[string][]string{
				"role-1": {
					policy.RoleTag("role-1"),
					policy.BindingTag("binding-1"),
				},
				"role-2": {
					policy.RoleTag("role-2"),
					policy.BindingTag("binding-2"),
				},
			},
			isGlobalPermissiveEnabled: true,
		},
		{
			name: "one role and one binding with no trust domain aliases",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBindingWithUser("binding", namespaceA, "role",
					policy.CustomPrincipal("td", "foo", "bar")),
			},
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td", nil),
			wantRules: map[string][]string{
				"role": {policy.RoleTag("role"),
					policy.CustomPrincipal("td", "foo", "bar")},
			},
		},
		{
			name: "one role and one binding with trust domain aliases",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBindingWithUser("binding", namespaceA, "role",
					policy.CustomPrincipal("td1", "foo", "bar")),
			},
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td2", []string{"td1"}),
			wantRules: map[string][]string{
				"role": {policy.RoleTag("role"),
					policy.CustomPrincipal("td2", "foo", "bar"),
					policy.CustomPrincipal("td1", "foo", "bar")},
			},
		},
		{
			name: "one role and one binding with trust domain aliases",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBindingWithUser("binding", namespaceA, "role",
					policy.CustomPrincipal("td1", "foo", "bar")),
			},
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"td2", "td3"}),
			wantRules: map[string][]string{
				"role": {policy.RoleTag("role"),
					policy.CustomPrincipal("td1", "foo", "bar"),
					policy.CustomPrincipal("td2", "foo", "bar"),
					policy.CustomPrincipal("td3", "foo", "bar")},
			},
		},
		{
			name: "multiple principals and multiple trust domain aliases",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBindingWithUser("binding-1", namespaceA, "role",
					policy.CustomPrincipal("td1", "foo", "bar")),
				policy.SimpleBindingWithUser("binding-2", namespaceA, "role",
					policy.CustomPrincipal("td1", "avengers", "thanos")),
			},
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"td2", "td3"}),
			wantRules: map[string][]string{
				"role": {policy.RoleTag("role"),
					policy.CustomPrincipal("td1", "foo", "bar"),
					policy.CustomPrincipal("td2", "foo", "bar"),
					policy.CustomPrincipal("td3", "foo", "bar"),
					policy.CustomPrincipal("td1", "avengers", "thanos"),
					policy.CustomPrincipal("td2", "avengers", "thanos"),
					policy.CustomPrincipal("td3", "avengers", "thanos")},
			},
		},
		{
			name: "trust domain *",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBindingWithUser("binding", namespaceA, "role",
					policy.CustomPrincipal("*", "foo", "bar")),
			},
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"cluster.local", "td2"}),
			wantRules: map[string][]string{
				"role": {policy.RoleTag("role"),
					// The trust domain here becomes an empty string because the RBAC config of the input
					// will check ns/namespaceA/sa/role for suffix matching.
					policy.CustomPrincipal("", "foo", "bar")},
			},
		},
		{
			name: "trust domain with prefix *",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, serviceFoo),
				policy.SimpleBindingWithUser("binding", namespaceA, "role",
					policy.CustomPrincipal("*-td", "foo", "bar")),
			},
			trustDomainBundle: trustdomain.NewTrustDomainBundle("foo-td", []string{"td1", "td2"}),
			wantRules: map[string][]string{
				"role": {policy.RoleTag("role"),
					policy.CustomPrincipal("-td", "foo", "bar"),
					policy.CustomPrincipal("td1", "foo", "bar"),
					policy.CustomPrincipal("td2", "foo", "bar")},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := policy.NewAuthzPolicies(tc.policies, t)
			if authzPolicies == nil {
				t.Fatal("failed to create authz policies")
			}
			g := NewGenerator(tc.trustDomainBundle, serviceFooInNamespaceA, authzPolicies, tc.isGlobalPermissiveEnabled)
			if g == nil {
				t.Fatal("failed to create generator")
			}

			_, got := g.Generate(tc.forTCPFilter)
			gotStr := spew.Sdump(got)

			if tc.isGlobalPermissiveEnabled {
				if got.GetRules() != nil {
					t.Fatal("rule must be nil when global permissive is true")
				}
			} else if got.GetRules() == nil {
				t.Fatal("rule must not be nil when global permissive is false")
			}
			needToCheckPrincipals := false
			if len(tc.trustDomainBundle.TrustDomains) > 0 {
				needToCheckPrincipals = true
			}
			if err := policy.Verify(got.GetRules(), tc.wantRules, needToCheckPrincipals, false /* wantDeny */); err != nil {
				t.Fatalf("%s\n%s", err, gotStr)
			}
			if err := policy.Verify(got.GetShadowRules(), tc.wantShadowRules, needToCheckPrincipals, false /* wantDeny */); err != nil {
				t.Fatalf("%s\n%s", err, gotStr)
			}
		})
	}
}
