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

package model

import (
	"reflect"
	"testing"

	rbacproto "istio.io/api/rbac/v1alpha1"
)

func TestAddConfig(t *testing.T) {

	roleCfg := Config{
		ConfigMeta: ConfigMeta{
			Type: ServiceRole.Type, Name: "test-role-1", Namespace: NamespaceAll},
		Spec: &rbacproto.ServiceRole{},
	}
	bindingCfg := Config{
		ConfigMeta: ConfigMeta{
			Type: ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}

	invalidateBindingCfg := Config{
		ConfigMeta: ConfigMeta{
			Type: ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: ""},
		},
	}

	cases := []struct {
		name                     string
		config                   []Config
		authzPolicies            *AuthorizationPolicies
		expectedRolesAndBindings *RolesAndBindings
	}{
		{
			name:                     "test add config for ServiceRole",
			config:                   []Config{roleCfg},
			authzPolicies:            &AuthorizationPolicies{},
			expectedRolesAndBindings: &RolesAndBindings{[]Config{roleCfg}, map[string][]*rbacproto.ServiceRoleBinding{}},
		},
		{
			name:                     "test add invalidate config for ServiceRoleBinding",
			config:                   []Config{invalidateBindingCfg},
			authzPolicies:            &AuthorizationPolicies{},
			expectedRolesAndBindings: nil,
		},
		{
			name:          "test add config for ServiceRoleBinding",
			config:        []Config{bindingCfg},
			authzPolicies: &AuthorizationPolicies{},
			expectedRolesAndBindings: &RolesAndBindings{
				[]Config{},
				map[string][]*rbacproto.ServiceRoleBinding{
					bindingCfg.Spec.(*rbacproto.ServiceRoleBinding).RoleRef.Name: {bindingCfg.Spec.(*rbacproto.ServiceRoleBinding)},
				},
			},
		},
		{
			name:          "test add config for both ServiceRoleBinding and ServiceRole",
			config:        []Config{roleCfg, bindingCfg},
			authzPolicies: &AuthorizationPolicies{},
			expectedRolesAndBindings: &RolesAndBindings{
				[]Config{roleCfg},
				map[string][]*rbacproto.ServiceRoleBinding{
					bindingCfg.Spec.(*rbacproto.ServiceRoleBinding).RoleRef.Name: {bindingCfg.Spec.(*rbacproto.ServiceRoleBinding)},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, role := range c.config {
				c.authzPolicies.AddConfig(&role)
			}
			if !reflect.DeepEqual(c.expectedRolesAndBindings, c.authzPolicies.NamespaceToPolicies[NamespaceAll]) {
				t.Errorf("[%s]Excepted:\n%v\n, Got: \n%v\n", c.name, c.expectedRolesAndBindings, c.authzPolicies.NamespaceToPolicies[NamespaceAll])
			}
		})
	}
}

func TestRolesForNamespace(t *testing.T) {
	roleCfg := Config{
		ConfigMeta: ConfigMeta{
			Type: ServiceRole.Type, Name: "test-role-1", Namespace: NamespaceAll},
		Spec: &rbacproto.ServiceRole{},
	}
	bindingCfg := Config{
		ConfigMeta: ConfigMeta{
			Type: ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}

	cases := []struct {
		name                            string
		authzPolicies                   *AuthorizationPolicies
		ns                              string
		expectedRolesServiceRoleBinding []Config
	}{
		{
			name:          "authzPolicies is nil",
			authzPolicies: nil,
			ns:            NamespaceAll,
			expectedRolesServiceRoleBinding: []Config{},
		},
		{
			name:          "the NamespaceToPolicies of authzPolicies is nil",
			authzPolicies: &AuthorizationPolicies{},
			ns:            NamespaceAll,
			expectedRolesServiceRoleBinding: []Config{},
		},
		{
			name:          "the namespaces of authzPolicies in NamespaceToPolicies is not exist",
			authzPolicies: &AuthorizationPolicies{map[string]*RolesAndBindings{}, nil},
			ns:            NamespaceAll,
			expectedRolesServiceRoleBinding: []Config{},
		},
		{
			name: "the roles of authzPolicies in NamespaceToPolicies is nil",
			authzPolicies: &AuthorizationPolicies{
				map[string]*RolesAndBindings{
					"default": {
						Roles:              []Config{},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
					},
				},
				nil,
			},
			ns: NamespaceAll,
			expectedRolesServiceRoleBinding: []Config{},
		},
		{
			name: "all seems ok",
			authzPolicies: &AuthorizationPolicies{
				map[string]*RolesAndBindings{
					NamespaceAll: {
						Roles:              []Config{roleCfg, bindingCfg},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
					},
				},
				nil,
			},
			ns: NamespaceAll,
			expectedRolesServiceRoleBinding: []Config{roleCfg, bindingCfg},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := c.authzPolicies.RolesForNamespace(c.ns)
			if !reflect.DeepEqual(c.expectedRolesServiceRoleBinding, actual) {
				t.Errorf("Got different Config, Excepted:\n%v\n, Got: \n%v\n", c.expectedRolesServiceRoleBinding, actual)
			}
		})
	}
}

func TestRoleToBindingsForNamespace(t *testing.T) {

	bindingCfg := Config{
		ConfigMeta: ConfigMeta{
			Type: ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}

	cases := []struct {
		name                            string
		authzPolicies                   *AuthorizationPolicies
		ns                              string
		expectedRolesServiceRoleBinding map[string][]*rbacproto.ServiceRoleBinding
	}{
		{
			name:          "authzPolicies is nil",
			authzPolicies: nil,
			ns:            NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name:          "the NamespaceToPolicies of authzPolicies is nil",
			authzPolicies: &AuthorizationPolicies{},
			ns:            NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name:          "the namespaces of authzPolicies in NamespaceToPolicies is not exist",
			authzPolicies: &AuthorizationPolicies{map[string]*RolesAndBindings{}, nil},
			ns:            NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name: "the roles of authzPolicies in NamespaceToPolicies is nil",
			authzPolicies: &AuthorizationPolicies{
				map[string]*RolesAndBindings{
					"default": {
						Roles:              []Config{},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
					},
				},
				nil,
			},
			ns: NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name: "all seems ok",
			authzPolicies: &AuthorizationPolicies{
				map[string]*RolesAndBindings{
					NamespaceAll: {
						Roles: []Config{},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{
							bindingCfg.Spec.(*rbacproto.ServiceRoleBinding).RoleRef.Name: {bindingCfg.Spec.(*rbacproto.ServiceRoleBinding)},
						},
					},
				},
				nil,
			},
			ns: NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{
				bindingCfg.Spec.(*rbacproto.ServiceRoleBinding).RoleRef.Name: {bindingCfg.Spec.(*rbacproto.ServiceRoleBinding)},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := c.authzPolicies.RoleToBindingsForNamespace(c.ns)
			if !reflect.DeepEqual(c.expectedRolesServiceRoleBinding, actual) {
				t.Errorf("Got different ServiceRoleBinding, Excepted:\n%v\n, Got: \n%v\n", c.expectedRolesServiceRoleBinding, actual)
			}
		})
	}
}
