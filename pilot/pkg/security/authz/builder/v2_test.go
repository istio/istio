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

package builder

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

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
	serviceFooInNamespaceA := newService("foo.a.svc.cluster.local", labelFoo, t)
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
				simpleRole("role", namespaceB, ""),
				simpleAuthorizationPolicy("policy", namespaceB, labelFoo, "role"),
			},
		},
		{
			name: "no policy matched for workload foo",
			policies: []*model.Config{
				simpleRole("role", namespaceA, ""),
				simpleAuthorizationPolicy("policy", namespaceA, labelBar, "role"),
			},
		},
		{
			name: "no role for workload foo",
			policies: []*model.Config{
				simpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role-1"),
				simpleRole("role-2", namespaceA, ""),
				simpleAuthorizationPolicy("policy-2", namespaceA, labelBar, "role-2"),
				simpleRole("role-3", namespaceB, ""),
				simpleAuthorizationPolicy("policy-3", namespaceB, labelFoo, "role-3"),
			},
		},
		{
			name: "one policy and one role",
			policies: []*model.Config{
				simpleRole("role", namespaceA, ""),
				simpleAuthorizationPolicy("policy", namespaceA, labelFoo, "role"),
			},
			wantRules: map[string][]string{
				"authz-[policy]-allow[0]": {roleTag("role"), authzPolicyTag("policy")},
			},
		},
		{
			name: "one policy and one role: forTCPFilter",
			policies: []*model.Config{
				simpleRole("role", namespaceA, ""),
				simpleAuthorizationPolicy("policy", namespaceA, labelFoo, "role"),
			},
			forTCPFilter: true,
		},
		{
			name: "two policies and one role",
			policies: []*model.Config{
				simpleRole("role", namespaceA, ""),
				simpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role"),
				simpleAuthorizationPolicy("policy-2", namespaceA, labelFoo, "role"),
			},
			wantRules: map[string][]string{
				"authz-[policy-1]-allow[0]": {
					roleTag("role"),
					authzPolicyTag("policy-1"),
				},
				"authz-[policy-2]-allow[0]": {
					roleTag("role"),
					authzPolicyTag("policy-2"),
				},
			},
		},
		{
			name: "two policies and two roles",
			policies: []*model.Config{
				simpleRole("role-1", namespaceA, ""),
				simpleAuthorizationPolicy("policy-1", namespaceA, labelFoo, "role-1"),
				simpleRole("role-2", namespaceA, ""),
				simpleAuthorizationPolicy("policy-2", namespaceA, labelFoo, "role-2"),
			},
			wantRules: map[string][]string{
				"authz-[policy-1]-allow[0]": {
					roleTag("role-1"),
					authzPolicyTag("policy-1"),
				},
				"authz-[policy-2]-allow[0]": {
					roleTag("role-2"),
					authzPolicyTag("policy-2"),
				},
			},
		},
	}

	for _, tc := range testCases {
		authzPolicies := newAuthzPolicies(tc.policies, t)
		if authzPolicies == nil {
			t.Fatalf("%s: failed to create authz policies", tc.name)
		}
		b := NewBuilder(serviceFooInNamespaceA, authzPolicies, false)
		if b == nil {
			t.Fatalf("%s: failed to create builder", tc.name)
		}

		got := b.build(tc.forTCPFilter)
		gotStr := spew.Sdump(got)

		if got.GetRules() == nil {
			t.Errorf("%s: rule must not be nil", tc.name)
		}
		if err := verify(got.GetRules(), tc.wantRules); err != nil {
			t.Errorf("%s: %s\n%s", tc.name, err, gotStr)
		}
	}
}
