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
	"testing"

	rbacproto "istio.io/api/rbac/v1alpha1"
)

func setupRBACStore() *ConfigStore {
	roles := make(RolesMapByNamespace)
	roles["ns1"] = make(RolesByName)

	roles.AddServiceRole("role1", "ns1", &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services:    []string{"bookstore"},
				Paths:       []string{"/books"},
				Methods:     []string{"GET"},
				Constraints: []*rbacproto.AccessRule_Constraint{},
			},
		},
	})
	roles.AddServiceRoleBinding("binding1", "ns1", &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				Properties: map[string]string{
					"source.namespace": "acme",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role1",
		},
	})

	roles.AddServiceRole("role2", "ns1", &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services: []string{"products"},
				Methods:  []string{"*"},
				Constraints: []*rbacproto.AccessRule_Constraint{
					{
						Key:    "destination.labels[version]",
						Values: []string{"v1", "v2"},
					},
				},
			},
		},
	})
	roles.AddServiceRoleBinding("binding2", "ns1", &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "alice@yahoo.com",
			},
			{
				Properties: map[string]string{
					"source.service":   "reviews",
					"source.namespace": "abc",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role2",
		},
	})

	roles.AddServiceRole("role3", "ns1", &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services:    []string{"fish*"},
				Paths:       []string{"/pond/*"},
				Methods:     []string{"GET"},
				Constraints: []*rbacproto.AccessRule_Constraint{},
			},
		},
	})
	roles.AddServiceRoleBinding("binding3", "ns1", &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				Properties: map[string]string{
					"source.namespace": "abcfish",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role3",
		},
	})

	roles.AddServiceRole("role4", "ns1", &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services:    []string{"fish"},
				Paths:       []string{"*/review"},
				Methods:     []string{"GET"},
				Constraints: []*rbacproto.AccessRule_Constraint{},
			},
		},
	})
	roles.AddServiceRoleBinding("binding4", "ns1", &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "alice@yahoo.com",
				Properties: map[string]string{
					"source.namespace": "mynamespace",
					"source.service":   "xyz",
				},
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role4",
		},
	})

	roles.AddServiceRole("role5", "ns1", &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services: []string{"abc"},
				Methods:  []string{"GET"},
			},
		},
	})
	roles.AddServiceRoleBinding("binding5", "ns1", &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "*",
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role5",
		},
	})

	return &ConfigStore{Roles: roles}
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
		subject := SubjectArgs{
			User: c.user,
			Properties: []string{
				"source.namespace=" + c.sourceNamespace,
				"source.service=" + c.sourceService,
			},
		}

		action := ActionArgs{
			Namespace:  c.namespace,
			Service:    c.service,
			Path:       c.path,
			Method:     c.method,
			Properties: []string{"destination.labels[version]=" + c.version},
		}

		if result, err := s.CheckPermission(subject, action); result != c.expected {
			t.Errorf("Does not meet expectation for case %v: %v", c, err)
		}
	}
}
