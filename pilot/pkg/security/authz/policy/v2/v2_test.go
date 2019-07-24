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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
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
	serviceFooInNamespaceA := policy.NewServiceMetadata("foo.a.svc.cluster.local", labelFoo, t)
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
				policy.SimpleRole("role", namespaceB, ""),
				policy.SimpleAuthorizationPolicy("policy", namespaceB, labelFoo, "role"),
			},
		},
		{
			name: "no policy matched for workload foo",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, ""),
				policy.SimpleAuthorizationPolicy("policy", namespaceA, labelBar, "role"),
			},
		},
		{
			name: "no role for workload foo",
			policies: []*model.Config{
				policy.SimpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role-1"),
				policy.SimpleRole("role-2", namespaceA, ""),
				policy.SimpleAuthorizationPolicy("policy-2", namespaceA, labelBar, "role-2"),
				policy.SimpleRole("role-3", namespaceB, ""),
				policy.SimpleAuthorizationPolicy("policy-3", namespaceB, labelFoo, "role-3"),
			},
		},
		{
			name: "one policy and one role",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, ""),
				policy.SimpleAuthorizationPolicy("policy", namespaceA, labelFoo, "role"),
			},
			wantRules: map[string][]string{
				"authz-[policy]-allow[0]": {policy.RoleTag("role"), policy.AuthzPolicyTag("policy")},
			},
		},
		{
			name: "one policy and one role: forTCPFilter",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, ""),
				policy.SimpleAuthorizationPolicy("policy", namespaceA, labelFoo, "role"),
			},
			forTCPFilter: true,
		},
		{
			name: "two policies and one role",
			policies: []*model.Config{
				policy.SimpleRole("role", namespaceA, ""),
				policy.SimpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role"),
				policy.SimpleAuthorizationPolicy("policy-2", namespaceA, labelFoo, "role"),
			},
			wantRules: map[string][]string{
				"authz-[policy-1]-allow[0]": {
					policy.RoleTag("role"),
					policy.AuthzPolicyTag("policy-1"),
				},
				"authz-[policy-2]-allow[0]": {
					policy.RoleTag("role"),
					policy.AuthzPolicyTag("policy-2"),
				},
			},
		},
		{
			name: "two policies and two roles",
			policies: []*model.Config{
				policy.SimpleRole("role-1", namespaceA, ""),
				policy.SimpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role-1"),
				policy.SimpleRole("role-2", namespaceA, ""),
				policy.SimpleAuthorizationPolicy("policy-2", namespaceA, labelFoo, "role-2"),
			},
			wantRules: map[string][]string{
				"authz-[policy-1]-allow[0]": {
					policy.RoleTag("role-1"),
					policy.AuthzPolicyTag("policy-1"),
				},
				"authz-[policy-2]-allow[0]": {
					policy.RoleTag("role-2"),
					policy.AuthzPolicyTag("policy-2"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := policy.NewAuthzPolicies(tc.policies, t)
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
			if err := policy.Verify(got.GetRules(), tc.wantRules); err != nil {
				t.Fatalf("%s\n%s", err, gotStr)
			}
		})
	}
}
