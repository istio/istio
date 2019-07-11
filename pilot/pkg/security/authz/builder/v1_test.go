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

func TestBuilder_buildV1(t *testing.T) {
	serviceFoo := "foo"
	namespaceA := "a"
	serviceBar := "bar"
	namespaceB := "b"
	serviceFooInNamespaceA := newService("foo.a.svc.cluster.local", nil, t)
	testCases := []struct {
		name                      string
		policies                  []*model.Config
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
				simpleRole("role", namespaceB, serviceFoo),
				simpleBinding("binding", namespaceB, "role"),
			},
		},
		{
			name: "no role matched for service foo",
			policies: []*model.Config{
				simpleRole("role", namespaceA, serviceBar),
				simpleBinding("binding", namespaceA, "role"),
			},
		},
		{
			name: "no role matched for service foo: global permissive",
			policies: []*model.Config{
				simpleRole("role", namespaceA, serviceBar),
				simpleBinding("binding", namespaceA, "role"),
			},
			isGlobalPermissiveEnabled: true,
		},
		{
			name: "no binding for service foo",
			policies: []*model.Config{
				simpleRole("role-1", namespaceA, serviceFoo),
				simpleRole("role-2", namespaceA, serviceBar),
				simpleBinding("binding-2", namespaceA, "role-2"),
				simpleRole("role-3", namespaceB, serviceFoo),
				simpleBinding("binding-3", namespaceB, "role-3"),
			},
		},
		{
			name: "one role and one binding",
			policies: []*model.Config{
				simpleRole("role", namespaceA, serviceFoo),
				simpleBinding("binding", namespaceA, "role"),
			},
			wantRules: map[string][]string{
				"role": {roleTag("role"), bindingTag("binding")},
			},
		},
		{
			name: "one role and one binding: forTCPFilter",
			policies: []*model.Config{
				simpleRole("role", namespaceA, serviceFoo),
				simpleBinding("binding", namespaceA, "role"),
			},
			forTCPFilter: true,
		},
		{
			name: "one role and one binding: permissive",
			policies: []*model.Config{
				simpleRole("role-1", namespaceA, serviceFoo),
				simpleBinding("binding-1", namespaceA, "role-1"),
				simpleRole("role-2", namespaceA, serviceFoo),
				simplePermissiveBinding("binding-2", namespaceA, "role-2"),
			},
			wantRules: map[string][]string{
				"role-1": {
					roleTag("role-1"),
					bindingTag("binding-1"),
				},
			},
			wantShadowRules: map[string][]string{
				"role-2": {
					roleTag("role-2"),
					bindingTag("binding-2"),
				},
			},
		},
		{
			name: "one role and two bindings",
			policies: []*model.Config{
				simpleRole("role", namespaceA, serviceFoo),
				simpleBinding("binding-1", namespaceA, "role"),
				simpleBinding("binding-2", namespaceA, "role"),
			},
			wantRules: map[string][]string{
				"role": {
					roleTag("role"),
					bindingTag("binding-1"),
					bindingTag("binding-2"),
				},
			},
		},
		{
			name: "two roles and two bindings",
			policies: []*model.Config{
				simpleRole("role-1", namespaceA, serviceFoo),
				simpleBinding("binding-1", namespaceA, "role-1"),
				simpleRole("role-2", namespaceA, serviceFoo),
				simpleBinding("binding-2", namespaceA, "role-2"),
			},
			wantRules: map[string][]string{
				"role-1": {
					roleTag("role-1"),
					bindingTag("binding-1"),
				},
				"role-2": {
					roleTag("role-2"),
					bindingTag("binding-2"),
				},
			},
		},
		{
			name: "two roles and two bindings: global permissive",
			policies: []*model.Config{
				simpleRole("role-1", namespaceA, serviceFoo),
				simpleBinding("binding-1", namespaceA, "role-1"),
				simpleRole("role-2", namespaceA, serviceFoo),
				simpleBinding("binding-2", namespaceA, "role-2"),
			},
			wantShadowRules: map[string][]string{
				"role-1": {
					roleTag("role-1"),
					bindingTag("binding-1"),
				},
				"role-2": {
					roleTag("role-2"),
					bindingTag("binding-2"),
				},
			},
			isGlobalPermissiveEnabled: true,
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
		b.isGlobalPermissiveEnabled = tc.isGlobalPermissiveEnabled

		got := b.build(tc.forTCPFilter)
		gotStr := spew.Sdump(got)

		if tc.isGlobalPermissiveEnabled {
			if got.GetRules() != nil {
				t.Errorf("%s: rule must be nil when global permissive is true", tc.name)
			}
		} else if got.GetRules() == nil {
			t.Errorf("%s: rule must not be nil when global permissive is false", tc.name)
		}

		if err := verify(got.GetRules(), tc.wantRules); err != nil {
			t.Errorf("%s: %s\n%s", tc.name, err, gotStr)
		}
		if err := verify(got.GetShadowRules(), tc.wantShadowRules); err != nil {
			t.Errorf("%s: %s\n%s", tc.name, err, gotStr)
		}
	}
}
