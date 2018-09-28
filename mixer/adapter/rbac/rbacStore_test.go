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

	"github.com/golang/protobuf/proto"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/authorization"
)

func setupRBACStore() *ConfigStore {
	s := &ConfigStore{}
	roles := make(RolesMapByNamespace)
	s.Roles = roles
	roles["ns1"] = make(RolesByName)
	rn := roles["ns1"]

	role1Spec := &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services:    []string{"bookstore"},
				Paths:       []string{"/books"},
				Methods:     []string{"GET"},
				Constraints: []*rbacproto.AccessRule_Constraint{},
			},
		},
	}

	binding1Spec := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				Properties: map[string]string{
					"namespace": "acme",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role1",
		},
	}

	rn["role1"] = newRoleInfo(role1Spec)
	rn["role1"].setBinding("binding1", binding1Spec)

	role2Spec := &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services: []string{"products"},
				Methods:  []string{"*"},
				Constraints: []*rbacproto.AccessRule_Constraint{
					{
						Key:    "version",
						Values: []string{"v1", "v2"},
					},
				},
			},
		},
	}

	binding2Spec := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "alice@yahoo.com",
			},
			{
				Properties: map[string]string{
					"service":   "reviews",
					"namespace": "abc",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role2",
		},
	}

	rn["role2"] = newRoleInfo(role2Spec)
	rn["role2"].setBinding("binding2", binding2Spec)

	role3Spec := &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services:    []string{"fish*"},
				Paths:       []string{"/pond/*"},
				Methods:     []string{"GET"},
				Constraints: []*rbacproto.AccessRule_Constraint{},
			},
		},
	}

	binding3Spec := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				Properties: map[string]string{
					"namespace": "abcfish",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role3",
		},
	}

	rn["role3"] = newRoleInfo(role3Spec)
	rn["role3"].setBinding("binding3", binding3Spec)

	role4Spec := &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services:    []string{"fish"},
				Paths:       []string{"*/review"},
				Methods:     []string{"GET"},
				Constraints: []*rbacproto.AccessRule_Constraint{},
			},
		},
	}

	binding4Spec := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "alice@yahoo.com",
				Properties: map[string]string{
					"namespace": "mynamespace",
					"service":   "xyz",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role4",
		},
	}

	rn["role4"] = newRoleInfo(role4Spec)
	rn["role4"].setBinding("binding4", binding4Spec)

	role5Spec := &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services: []string{"abc"},
				Methods:  []string{"GET"},
			},
		},
	}

	binding5Spec := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "*",
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role5",
		},
	}

	rn["role5"] = newRoleInfo(role5Spec)
	rn["role5"].setBinding("binding5", binding5Spec)

	return s
}

func TestRBACStore_CheckPermission(t *testing.T) {
	s := setupRBACStore()

	cases := []struct {
		namespace       string
		service         string
		path            string
		method          string
		version         string
		sourceNamespace string
		sourceService   string
		user            string
		expected        bool
	}{
		{"ns1", "products", "/products", "GET", "v1", "ns2", "svc1", "alice@yahoo.com", true},
		{"ns1", "products", "/somepath", "POST", "v2", "abc", "reviews", "bob@yahoo.com", true},
		{"ns1", "products", "/somepath", "POST", "v2", "abc", "svc1", "bob@yahoo.com", false},
		{"ns1", "products", "/somepath", "POST", "v3", "ns2", "svc1", "alice@yahoo.com", false},
		{"ns1", "bookstore", "/books", "GET", "", "acme", "svc1", "svc@gserviceaccount.com", true},
		{"ns1", "bookstore", "/books", "POST", "", "acme", "svc1", "svc@gserviceaccount.com", false},
		{"ns1", "bookstore", "/shelf", "GET", "", "acme", "svc1", "svc@gserviceaccount.com", false},
		{"ns1", "fishpond", "/pond/a", "GET", "v1", "abcfish", "serv", "svc@gserviceaccount.com", true},
		{"ns1", "fish", "/pond/review", "GET", "v1", "mynamespace", "xyz", "alice@yahoo.com", true},
		{"ns1", "fish", "/pond/review", "GET", "v1", "wonrgnamespace", "xyz", "alice@yahoo.com", false},
		{"ns1", "fishpond", "/pond/review", "GET", "v1", "mynamespace", "xyz", "alice@yahoo.com", false},
		{"ns1", "fish", "/pond/review", "GET", "v1", "mynamespace", "xyz", "bob@yahoo.com", false},
		{"ns1", "abc", "/index", "GET", "", "mynamespace", "xyz", "anyuser", true},
	}

	for _, c := range cases {
		instance := &authorization.Instance{}
		instance.Subject = &authorization.Subject{}
		instance.Subject.User = c.user
		instance.Subject.Groups = c.user
		instance.Subject.Properties = make(map[string]interface{})
		instance.Subject.Properties["namespace"] = c.sourceNamespace
		instance.Subject.Properties["service"] = c.sourceService
		instance.Action = &authorization.Action{}
		instance.Action.Namespace = c.namespace
		instance.Action.Service = c.service
		instance.Action.Path = c.path
		instance.Action.Method = c.method
		instance.Action.Properties = make(map[string]interface{})
		instance.Action.Properties["version"] = c.version

		result, _ := s.CheckPermission(instance, test.NewEnv(t))
		if result != c.expected {
			t.Errorf("Does not meet expectation for case %v", c)
		}
	}
}

func TestRBACStore_ListAccessRules(t *testing.T) {
	s := setupRBACStore()

	cases := []struct {
		subject  SubjectArgs
		action   ActionArgs
		expected *map[string]*rbacproto.ServiceRole
	}{
		{
			subject: SubjectArgs{
				User: "alice@yahoo.com",
			},
			action: ActionArgs{
				Namespace: "ns1",
			},
			expected: &map[string]*rbacproto.ServiceRole{
				"role2": {
					Rules: []*rbacproto.AccessRule{
						{
							Services: []string{"products"},
							Methods:  []string{"*"},
							Constraints: []*rbacproto.AccessRule_Constraint{
								{
									Key:    "version",
									Values: []string{"v1", "v2"},
								},
							},
						},
					},
				},
				"role5": {
					Rules: []*rbacproto.AccessRule{
						{
							Services: []string{"abc"},
							Methods:  []string{"GET"},
						},
					},
				},
			},
		},
		{
			subject: SubjectArgs{
				Properties: []string{"namespace=acme"},
			},
			action: ActionArgs{
				Namespace: "ns1",
			},
			expected: &map[string]*rbacproto.ServiceRole{
				"role1": {
					Rules: []*rbacproto.AccessRule{
						{
							Services: []string{"bookstore"},
							Paths:    []string{"/books"},
							Methods:  []string{"GET"},
						},
					},
				},
				"role5": {
					Rules: []*rbacproto.AccessRule{
						{
							Services: []string{"abc"},
							Methods:  []string{"GET"},
						},
					},
				},
			},
		},
		{
			subject: SubjectArgs{
				User:       "alice@yahoo.com",
				Properties: []string{"namespace=mynamespace", "service=xyz"},
			},
			action: ActionArgs{
				Namespace: "ns1",
			},
			expected: &map[string]*rbacproto.ServiceRole{
				"role2": {
					Rules: []*rbacproto.AccessRule{
						{
							Services: []string{"products"},
							Methods:  []string{"*"},
							Constraints: []*rbacproto.AccessRule_Constraint{
								{
									Key:    "version",
									Values: []string{"v1", "v2"},
								},
							},
						},
					},
				},
				"role4": {
					Rules: []*rbacproto.AccessRule{
						{
							Services: []string{"fish"},
							Paths:    []string{"*/review"},
							Methods:  []string{"GET"},
						},
					},
				},
				"role5": {
					Rules: []*rbacproto.AccessRule{
						{
							Services: []string{"abc"},
							Methods:  []string{"GET"},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		result, _ := s.ListPermissions(c.subject, c.action)
		if len(*c.expected) != len(*result) {
			t.Fatalf("invalid numbers of response. expected: %v got: %v", len(*c.expected), len(*result))
		}

		for roleName, role := range *c.expected {
			if _, ok := (*result)[roleName]; !ok {
				t.Errorf("No response for %v", roleName)
				continue
			}

			if !proto.Equal((*result)[roleName], role) {
				t.Errorf("unexpected response for %v expected: %v got: %v", roleName, role, (*result)[roleName])
			}
		}
	}
}

func TestRBACStore_ListUsers(t *testing.T) {
	s := setupRBACStore()

	cases := []struct {
		action   ActionArgs
		expected []string
	}{
		{
			action: ActionArgs{
				Service:   "fish",
				Path:      "/book/123/review",
				Method:    "GET",
				Namespace: "ns1",
			},
			expected: []string{"alice@yahoo.com"},
		},
		{
			action: ActionArgs{
				Service:    "products",
				Path:       "/",
				Method:     "GET",
				Namespace:  "ns1",
				Properties: []string{"version=v1"},
			},
			expected: []string{"alice@yahoo.com"},
		},
		{
			action: ActionArgs{
				Service:   "abc",
				Path:      "/",
				Method:    "GET",
				Namespace: "ns1",
			},
			expected: []string{"*"},
		},
		{
			action: ActionArgs{
				Service:   "noservice",
				Path:      "/",
				Method:    "GET",
				Namespace: "nonamespace",
			},
			expected: []string{},
		},
	}

	for _, c := range cases {
		result, err := s.ListMembers(SubjectArgs{}, c.action)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(c.expected) != len(*result) {
			t.Fatalf("invalid numbers of response. expected: %v got: %v", len(c.expected), len(*result))
		}

		if !reflect.DeepEqual(c.expected, *result) {
			t.Errorf("unexpected response. expected: %v got: %v", c.expected, *result)
		}
	}
}
