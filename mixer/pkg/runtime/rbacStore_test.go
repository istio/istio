// Copyright 2017 Istio Authors
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

package runtime

import (
	"testing"

	cpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/template/authorization"
)

func setupRbacStore() *RbacStore {
	s := &RbacStore{}
	roles := make(rolesMapByNamespace)
	s.roles = roles
	roles["ns1"] = make(rolesByName)
	rn := roles["ns1"]

	role1Spec := &cpb.ServiceRole{
		Rules: []*cpb.AccessRule{
			{
				Services:    []string{"bookstore"},
				Paths:       []string{"/books"},
				Methods:     []string{"GET"},
				Constraints: []*cpb.AccessRule_Constraint{},
			},
		},
	}

	binding1Spec := &cpb.ServiceRoleBinding{
		Subjects: []*cpb.Subject{
			{
				Properties: map[string]string{
					"namespace": "acme",
				},
			},
		},
		RoleRef: &cpb.RoleRef{
			Kind: "ServiceRole",
			Name: "role1",
		},
	}

	rn["role1"] = newRoleInfo(role1Spec)
	rn["role1"].setBinding("binding1", binding1Spec)

	role2Spec := &cpb.ServiceRole{
		Rules: []*cpb.AccessRule{
			{
				Services: []string{"products"},
				Methods:  []string{"*"},
				Constraints: []*cpb.AccessRule_Constraint{
					{
						Name:  "version",
						Value: []string{"v1", "v2"},
					},
				},
			},
		},
	}

	binding2Spec := &cpb.ServiceRoleBinding{
		Subjects: []*cpb.Subject{
			{
				Properties: map[string]string{
					"user": "alice@yahoo.com",
				},
			},
			{
				Properties: map[string]string{
					"service":   "reviews",
					"namespace": "abc",
				},
			},
		},
		RoleRef: &cpb.RoleRef{
			Kind: "ServiceRole",
			Name: "role2",
		},
	}

	rn["role2"] = newRoleInfo(role2Spec)
	rn["role2"].setBinding("binding2", binding2Spec)

	return s
}

func TestRbacStore_CheckPermission(t *testing.T) {
	s := setupRbacStore()

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

		result, _ := s.CheckPermission(instance)
		if result != c.expected {
			t.Errorf("Does not meet expectation for case %v", c)
		}
	}
}
