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

package v2

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/security/authz/policy/test"

	"istio.io/istio/pilot/pkg/model"
)

func TestBuilder_buildV2(t *testing.T) {
	labelFoo := map[string]string{
		"app": "foo",
	}
	namespaceA := "a"
	labelBar := map[string]string{
		"app": "bar",
	}
	namespaceB := "b"
	serviceFooInNamespaceA := test.NewServiceMetadata("foo.a.svc.cluster.local", labelFoo, t)
	testCases := []struct {
		name         string
		policies     []*model.Config
		wantRules    map[string][]string
		forTCPFilter bool
	}{
		{
			name: "no policy",
		},
		{
			name: "no policy in namespace a",
			policies: []*model.Config{
				test.SimpleRole("role", namespaceB, ""),
				test.SimpleAuthorizationPolicy("policy", namespaceB, labelFoo, "role"),
			},
		},
		{
			name: "no policy matched for workload foo",
			policies: []*model.Config{
				test.SimpleRole("role", namespaceA, ""),
				test.SimpleAuthorizationPolicy("policy", namespaceA, labelBar, "role"),
			},
		},
		{
			name: "no role for workload foo",
			policies: []*model.Config{
				test.SimpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role-1"),
				test.SimpleRole("role-2", namespaceA, ""),
				test.SimpleAuthorizationPolicy("policy-2", namespaceA, labelBar, "role-2"),
				test.SimpleRole("role-3", namespaceB, ""),
				test.SimpleAuthorizationPolicy("policy-3", namespaceB, labelFoo, "role-3"),
			},
		},
		{
			name: "one policy and one role",
			policies: []*model.Config{
				test.SimpleRole("role", namespaceA, ""),
				test.SimpleAuthorizationPolicy("policy", namespaceA, labelFoo, "role"),
			},
			wantRules: map[string][]string{
				"authz-[policy]-allow[0]": {test.RoleTag("role"), test.AuthzPolicyTag("policy")},
			},
		},
		{
			name: "one policy and one role: forTCPFilter",
			policies: []*model.Config{
				test.SimpleRole("role", namespaceA, ""),
				test.SimpleAuthorizationPolicy("policy", namespaceA, labelFoo, "role"),
			},
			forTCPFilter: true,
		},
		{
			name: "two policies and one role",
			policies: []*model.Config{
				test.SimpleRole("role", namespaceA, ""),
				test.SimpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role"),
				test.SimpleAuthorizationPolicy("policy-2", namespaceA, labelFoo, "role"),
			},
			wantRules: map[string][]string{
				"authz-[policy-1]-allow[0]": {
					test.RoleTag("role"),
					test.AuthzPolicyTag("policy-1"),
				},
				"authz-[policy-2]-allow[0]": {
					test.RoleTag("role"),
					test.AuthzPolicyTag("policy-2"),
				},
			},
		},
		{
			name: "two policies and two roles",
			policies: []*model.Config{
				test.SimpleRole("role-1", namespaceA, ""),
				test.SimpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role-1"),
				test.SimpleRole("role-2", namespaceA, ""),
				test.SimpleAuthorizationPolicy("policy-2", namespaceA, labelFoo, "role-2"),
			},
			wantRules: map[string][]string{
				"authz-[policy-1]-allow[0]": {
					test.RoleTag("role-1"),
					test.AuthzPolicyTag("policy-1"),
				},
				"authz-[policy-2]-allow[0]": {
					test.RoleTag("role-2"),
					test.AuthzPolicyTag("policy-2"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := test.NewAuthzPolicies(tc.policies, t)
			if authzPolicies == nil {
				t.Fatal("failed to create authz policies")
			}
			b := NewGenerator(serviceFooInNamespaceA, authzPolicies, false)
			if b == nil {
				t.Fatal("failed to create builder")
			}

			got := b.Generate(tc.forTCPFilter)
			gotStr := spew.Sdump(got)

			if got.GetRules() == nil {
				t.Fatal("rule must not be nil")
			}
			if err := test.Verify(got.GetRules(), tc.wantRules); err != nil {
				t.Fatalf("%s\n%s", err, gotStr)
			}
		})
	}
}
