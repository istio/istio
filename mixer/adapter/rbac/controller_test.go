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

package rbac

import (
	"reflect"
	"testing"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/config/store"
)

func TestController_processRBACRoles(t *testing.T) {
	configState := map[store.Key]*store.Resource{
		{serviceRoleKind, "ns1", "role1"}: {Spec: &rbacproto.ServiceRole{
			Rules: []*rbacproto.AccessRule{
				{
					Services: []string{"bookstore"},
					Paths:    []string{"/books"},
					Methods:  []string{"GET"},
					Constraints: []*rbacproto.AccessRule_Constraint{
						{
							Key:    "version",
							Values: []string{"v1", "v2"},
						},
					},
				},
			},
		}},
		{serviceRoleBindingKind, "ns1", "binding1"}: {Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{
				{
					User: "alice@yahoo.com",
					Properties: map[string]string{
						"namespace": "abc",
					},
				},
			},
			RoleRef: &rbacproto.RoleRef{
				Kind: "ServiceRole",
				Name: "role1",
			},
		}},
	}

	r := &ConfigStore{}
	c := &controller{
		configState: configState,
		rbacStore:   r,
	}

	c.processRBACRoles(test.NewEnv(t))

	wantRole := &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services: []string{"bookstore"},
				Paths:    []string{"/books"},
				Methods:  []string{"GET"},
				Constraints: []*rbacproto.AccessRule_Constraint{
					{
						Key:    "version",
						Values: []string{"v1", "v2"},
					},
				},
			},
		},
	}

	wantRoleBinding := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "alice@yahoo.com",
				Properties: map[string]string{
					"namespace": "abc",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role1",
		},
	}

	if len(r.Roles) != 1 {
		t.Fatalf("Got %d, want 1 instance", len(r.Roles))
	}

	roles := r.Roles["ns1"]
	if roles == nil || roles["role1"] == nil {
		t.Fatalf("role1 is not populated.")
	}

	info := roles["role1"].Info
	if !reflect.DeepEqual(info, wantRole) {
		t.Fatalf("Got %v, want %v", info, wantRole)
	}

	bindings := roles["role1"].Bindings
	if len(bindings) != 1 {
		t.Fatalf("Got %d, want 1 binding associated with role1", len(bindings))
	}
	binding := bindings["binding1"]
	if !reflect.DeepEqual(binding, wantRoleBinding) {
		t.Fatalf("Got %v, Want %v", binding, wantRoleBinding)
	}
}
