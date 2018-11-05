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

package model_test

import (
	"reflect"
	"testing"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

func TestAddConfig(t *testing.T) {
	roleCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRole.Type, Name: "test-role-1", Namespace: model.NamespaceAll},
		Spec: &rbacproto.ServiceRole{},
	}
	bindingCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: model.NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}

	invalidateBindingCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: model.NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: ""},
		},
	}

	cases := []struct {
		name                     string
		config                   []model.Config
		authzPolicies            *model.AuthorizationPolicies
		expectedRolesAndBindings *model.RolesAndBindings
	}{
		{
			name:                     "test add config for ServiceRole",
			config:                   []model.Config{roleCfg},
			authzPolicies:            &model.AuthorizationPolicies{},
			expectedRolesAndBindings: &model.RolesAndBindings{[]model.Config{roleCfg}, map[string][]*rbacproto.ServiceRoleBinding{}},
		},
		{
			name:                     "test add invalidate config for ServiceRoleBinding",
			config:                   []model.Config{invalidateBindingCfg},
			authzPolicies:            &model.AuthorizationPolicies{},
			expectedRolesAndBindings: nil,
		},
		{
			name:          "test add config for ServiceRoleBinding",
			config:        []model.Config{bindingCfg},
			authzPolicies: &model.AuthorizationPolicies{},
			expectedRolesAndBindings: &model.RolesAndBindings{
				[]model.Config{},
				map[string][]*rbacproto.ServiceRoleBinding{
					bindingCfg.Spec.(*rbacproto.ServiceRoleBinding).RoleRef.Name: {bindingCfg.Spec.(*rbacproto.ServiceRoleBinding)},
				},
			},
		},
		{
			name:          "test add config for both ServiceRoleBinding and ServiceRole",
			config:        []model.Config{roleCfg, bindingCfg},
			authzPolicies: &model.AuthorizationPolicies{},
			expectedRolesAndBindings: &model.RolesAndBindings{
				[]model.Config{roleCfg},
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
			if !reflect.DeepEqual(c.expectedRolesAndBindings, c.authzPolicies.NamespaceToPolicies[model.NamespaceAll]) {
				t.Errorf("[%s]Excepted:\n%v\n, Got: \n%v\n", c.name, c.expectedRolesAndBindings, c.authzPolicies.NamespaceToPolicies[model.NamespaceAll])
			}
		})
	}
}

func TestRolesForNamespace(t *testing.T) {
	roleCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRole.Type, Name: "test-role-1", Namespace: model.NamespaceAll},
		Spec: &rbacproto.ServiceRole{},
	}
	bindingCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: model.NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}

	cases := []struct {
		name                            string
		authzPolicies                   *model.AuthorizationPolicies
		ns                              string
		expectedRolesServiceRoleBinding []model.Config
	}{
		{
			name:                            "authzPolicies is nil",
			authzPolicies:                   nil,
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: []model.Config{},
		},
		{
			name:                            "the NamespaceToPolicies of authzPolicies is nil",
			authzPolicies:                   &model.AuthorizationPolicies{},
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: []model.Config{},
		},
		{
			name:                            "the namespaces of authzPolicies in NamespaceToPolicies is not exist",
			authzPolicies:                   &model.AuthorizationPolicies{map[string]*model.RolesAndBindings{}, nil},
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: []model.Config{},
		},
		{
			name: "the roles of authzPolicies in NamespaceToPolicies is nil",
			authzPolicies: &model.AuthorizationPolicies{
				map[string]*model.RolesAndBindings{
					"default": {
						Roles:              []model.Config{},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
					},
				},
				nil,
			},
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: []model.Config{},
		},
		{
			name: "all seems ok",
			authzPolicies: &model.AuthorizationPolicies{
				map[string]*model.RolesAndBindings{
					model.NamespaceAll: {
						Roles:              []model.Config{roleCfg, bindingCfg},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
					},
				},
				nil,
			},
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: []model.Config{roleCfg, bindingCfg},
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
	bindingCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: model.NamespaceAll},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}

	cases := []struct {
		name                            string
		authzPolicies                   *model.AuthorizationPolicies
		ns                              string
		expectedRolesServiceRoleBinding map[string][]*rbacproto.ServiceRoleBinding
	}{
		{
			name:                            "authzPolicies is nil",
			authzPolicies:                   nil,
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name:                            "the NamespaceToPolicies of authzPolicies is nil",
			authzPolicies:                   &model.AuthorizationPolicies{},
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name:                            "the namespaces of authzPolicies in NamespaceToPolicies is not exist",
			authzPolicies:                   &model.AuthorizationPolicies{map[string]*model.RolesAndBindings{}, nil},
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name: "the roles of authzPolicies in NamespaceToPolicies is nil",
			authzPolicies: &model.AuthorizationPolicies{
				map[string]*model.RolesAndBindings{
					"default": {
						Roles:              []model.Config{},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
					},
				},
				nil,
			},
			ns:                              model.NamespaceAll,
			expectedRolesServiceRoleBinding: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name: "all seems ok",
			authzPolicies: &model.AuthorizationPolicies{
				map[string]*model.RolesAndBindings{
					model.NamespaceAll: {
						Roles: []model.Config{},
						RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{
							bindingCfg.Spec.(*rbacproto.ServiceRoleBinding).RoleRef.Name: {bindingCfg.Spec.(*rbacproto.ServiceRoleBinding)},
						},
					},
				},
				nil,
			},
			ns: model.NamespaceAll,
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

func TestNewAuthzPolicies(t *testing.T) {
	clusterRbacConfig := &rbacproto.RbacConfig{Mode: rbacproto.RbacConfig_ON}
	rbacConfig := &rbacproto.RbacConfig{Mode: rbacproto.RbacConfig_OFF}
	cases := []struct {
		name   string
		store  model.IstioConfigStore
		expect *rbacproto.RbacConfig
	}{
		{name: "no policy", store: storeWithConfig(nil, nil)},
		{name: "ClusterRbacConfig only", store: storeWithConfig(clusterRbacConfig, nil), expect: clusterRbacConfig},
		{name: "RbacConfig only", store: storeWithConfig(nil, rbacConfig), expect: rbacConfig},
		{name: "both ClusterRbacConfig and RbacConfig", store: storeWithConfig(clusterRbacConfig, rbacConfig), expect: clusterRbacConfig},
	}

	for _, c := range cases {
		environment := &model.Environment{IstioConfigStore: c.store}
		actual, _ := model.NewAuthzPolicies(environment)
		if actual == nil || c.expect == nil {
			if actual != nil {
				t.Errorf("%s: Got %v but expecting nil", c.name, *actual)
			} else if c.expect != nil {
				t.Errorf("%s: Got nil but expecting %v", c.name, *c.expect)
			}
		} else {
			if !reflect.DeepEqual(*actual.RbacConfig, *c.expect) {
				t.Errorf("%s: Got %v but expecting %v", c.name, *actual.RbacConfig, *c.expect)
			}
		}
	}
}

func storeWithConfig(
	clusterRbacConfig *rbacproto.RbacConfig, rbacConfig *rbacproto.RbacConfig) model.IstioConfigStore {
	store := memory.Make(model.IstioConfigTypes)

	if clusterRbacConfig != nil {
		config := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.ClusterRbacConfig.Type,
				Name:      "default",
				Namespace: "default",
			},
			Spec: clusterRbacConfig,
		}
		store.Create(config)
	}
	if rbacConfig != nil {
		config := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.RbacConfig.Type,
				Name:      "default",
				Namespace: "default",
			},
			Spec: rbacConfig,
		}
		store.Create(config)
	}
	return model.MakeIstioStore(store)
}
