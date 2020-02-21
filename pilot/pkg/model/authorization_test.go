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
	"fmt"
	"reflect"
	"testing"

	"istio.io/pkg/ledger"

	"github.com/gogo/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	meshconfig "istio.io/api/mesh/v1alpha1"
	rbacproto "istio.io/api/rbac/v1alpha1"
	authpb "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
)

func TestGetAuthorizationPolicies(t *testing.T) {
	testNS := "test-ns"
	roleCfg := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioRbacV1Alpha1Serviceroles.Resource().Kind(),
			Version:   collections.IstioRbacV1Alpha1Serviceroles.Resource().Version(),
			Group:     collections.IstioRbacV1Alpha1Serviceroles.Resource().Group(),
			Name:      "test-role-1",
			Namespace: testNS,
		},
		Spec: &rbacproto.ServiceRole{
			Rules: []*rbacproto.AccessRule{{Services: []string{"test-svc-1"}}},
		},
	}
	bindingCfg := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Kind(),
			Version:   collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Version(),
			Group:     collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Group(),
			Name:      "test-binding-1",
			Namespace: testNS,
		},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}
	invalidateBindingCfg := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Kind(),
			Version:   collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Version(),
			Group:     collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Group(),
			Name:      "test-binding-1",
			Namespace: testNS,
		},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: ""},
		},
	}

	cases := []struct {
		name   string
		config []Config
		want   *RolesAndBindings
	}{
		{
			name:   "add ServiceRole",
			config: []Config{roleCfg},
			want: &RolesAndBindings{
				Roles: []ServiceRoleConfig{
					{
						Name:        roleCfg.Name,
						ServiceRole: roleCfg.Spec.(*rbacproto.ServiceRole),
					},
				},
				Bindings: map[string][]*rbacproto.ServiceRoleBinding{}},
		},
		{
			name:   "add invalidate ServiceRoleBinding",
			config: []Config{invalidateBindingCfg},
			want:   nil,
		},
		{
			name:   "add ServiceRoleBinding",
			config: []Config{bindingCfg},
			want: &RolesAndBindings{
				Bindings: map[string][]*rbacproto.ServiceRoleBinding{
					"test-role-1": {&rbacproto.ServiceRoleBinding{
						Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
						RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
					}},
				},
			},
		},
		{
			name:   "add ServiceRoleBinding and ServiceRole",
			config: []Config{roleCfg, bindingCfg},
			want: &RolesAndBindings{
				Roles: []ServiceRoleConfig{
					{
						Name:        roleCfg.Name,
						ServiceRole: roleCfg.Spec.(*rbacproto.ServiceRole),
					},
				},
				Bindings: map[string][]*rbacproto.ServiceRoleBinding{
					"test-role-1": {bindingCfg.Spec.(*rbacproto.ServiceRoleBinding)},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(c.config, t)
			got := authzPolicies.NamespaceToV1alpha1Policies[testNS]
			if !reflect.DeepEqual(c.want, got) {
				t.Errorf("want:\n%s\n, got:\n%s\n", c.want, got)
			}
		})
	}
}

func TestAuthorizationPolicies_ListNamespacesOfServiceRoles(t *testing.T) {
	role := &rbacproto.ServiceRole{}
	binding := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "user-1",
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role-1",
		},
	}

	cases := []struct {
		name    string
		ns      string
		configs []Config
		want    []string
	}{
		{
			name: "no roles",
			ns:   "foo",
			want: []string{},
		},
		{
			name: "role and binding same namespace",
			ns:   "bar",
			configs: []Config{
				newConfig("role", "bar", role),
				newConfig("binding", "bar", binding),
			},
			want: []string{"bar"},
		},
		{
			name: "two roles different namespaces",
			ns:   "bar",
			configs: []Config{
				newConfig("role-1", "foo", role),
				newConfig("role-2", "bar", role),
			},
			want: []string{"foo", "bar"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(tc.configs, t)

			got := authzPolicies.ListV1alpha1Namespaces()
			if diff := cmp.Diff(tc.want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("want:%v\n got: %v diff %v\n", tc.want, got, diff)
			}
		})
	}
}

func TestAuthorizationPolicies_ListServiceRolesRoles(t *testing.T) {
	role := &rbacproto.ServiceRole{}
	binding := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "user-1",
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role-1",
		},
	}

	cases := []struct {
		name    string
		ns      string
		configs []Config
		want    []ServiceRoleConfig
	}{
		{
			name: "no roles",
			ns:   "foo",
			want: nil,
		},
		{
			name: "only binding",
			ns:   "foo",
			configs: []Config{
				newConfig("binding", "foo", binding),
			},
			want: nil,
		},
		{
			name: "no roles in namespace foo",
			ns:   "foo",
			configs: []Config{
				newConfig("role", "bar", role),
				newConfig("binding", "bar", binding),
			},
			want: nil,
		},
		{
			name: "one role",
			ns:   "bar",
			configs: []Config{
				newConfig("role", "bar", role),
				newConfig("binding", "bar", binding),
			},
			want: []ServiceRoleConfig{
				{
					Name:        "role",
					ServiceRole: role,
				},
			},
		},
		{
			name: "two roles",
			ns:   "bar",
			configs: []Config{
				newConfig("role-1", "foo", role),
				newConfig("role-1", "bar", role),
				newConfig("role-2", "bar", role),
			},
			want: []ServiceRoleConfig{
				{
					Name:        "role-1",
					ServiceRole: role,
				},
				{
					Name:        "role-2",
					ServiceRole: role,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(tc.configs, t)

			got := authzPolicies.ListServiceRoles(tc.ns)
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("want:%v\n but got: %v\n", tc.want, got)
			}
		})
	}
}

func TestAuthorizationPolicies_ListServiceRoleBindings(t *testing.T) {
	role := &rbacproto.ServiceRole{}
	binding := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "user-1",
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role-1",
		},
	}
	binding2 := &rbacproto.ServiceRoleBinding{
		Subjects: []*rbacproto.Subject{
			{
				User: "user-2",
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: "role-2",
		},
	}

	cases := []struct {
		name    string
		ns      string
		configs []Config
		want    map[string][]*rbacproto.ServiceRoleBinding
	}{
		{
			name: "no configs",
			ns:   "foo",
			want: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name: "no configs in namespace foo",
			ns:   "foo",
			configs: []Config{
				newConfig("role-1", "bar", role),
				newConfig("binding-1", "bar", binding),
			},
			want: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name: "no bindings in namespace foo",
			ns:   "foo",
			configs: []Config{
				newConfig("role-1", "foo", role),
				newConfig("role-1", "bar", role),
				newConfig("binding-1", "bar", binding),
			},
			want: map[string][]*rbacproto.ServiceRoleBinding{},
		},
		{
			name: "one binding",
			ns:   "bar",
			configs: []Config{
				newConfig("role-1", "bar", role),
				newConfig("binding-1", "bar", binding),
				newConfig("role-2", "foo", role),
				newConfig("binding-2", "foo", binding2),
			},
			want: map[string][]*rbacproto.ServiceRoleBinding{
				"role-1": {
					binding,
				},
			},
		},
		{
			name: "two bindings",
			ns:   "foo",
			configs: []Config{
				newConfig("role-1", "foo", role),
				newConfig("binding-1", "foo", binding),
				newConfig("role-2", "foo", role),
				newConfig("binding-2", "foo", binding2),
			},
			want: map[string][]*rbacproto.ServiceRoleBinding{
				"role-1": {
					binding,
				},
				"role-2": {
					binding2,
				},
			},
		},
		{
			name: "multiple bindings for same role",
			ns:   "foo",
			configs: []Config{
				newConfig("role-1", "foo", role),
				newConfig("binding-1", "foo", binding),
				newConfig("binding-2", "foo", binding),
				newConfig("binding-3", "foo", binding),
			},
			want: map[string][]*rbacproto.ServiceRoleBinding{
				"role-1": {
					binding,
					binding,
					binding,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(tc.configs, t)

			got := authzPolicies.ListServiceRoleBindings(tc.ns)
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("want: %v\n but got: %v", tc.want, got)
			}
		})
	}
}

func TestAuthorizationPolicies_ListAuthorizationPolicies(t *testing.T) {
	policy := &authpb.AuthorizationPolicy{
		Rules: []*authpb.Rule{
			{
				From: []*authpb.Rule_From{
					{
						Source: &authpb.Source{
							Principals: []string{"sleep"},
						},
					},
				},
				To: []*authpb.Rule_To{
					{
						Operation: &authpb.Operation{
							Methods: []string{"GET"},
						},
					},
				},
			},
		},
	}
	policyWithSelector := proto.Clone(policy).(*authpb.AuthorizationPolicy)
	policyWithSelector.Selector = &selectorpb.WorkloadSelector{
		MatchLabels: map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	}
	denyPolicy := proto.Clone(policy).(*authpb.AuthorizationPolicy)
	denyPolicy.Action = authpb.AuthorizationPolicy_DENY

	cases := []struct {
		name           string
		ns             string
		workloadLabels map[string]string
		configs        []Config
		wantDeny       []AuthorizationPolicyConfig
		wantAllow      []AuthorizationPolicyConfig
	}{
		{
			name:      "no policies",
			ns:        "foo",
			wantAllow: nil,
		},
		{
			name: "no policies in namespace foo",
			ns:   "foo",
			configs: []Config{
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: nil,
		},
		{
			name: "one allow policy",
			ns:   "bar",
			configs: []Config{
				newConfig("authz-1", "bar", policy),
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "bar",
					AuthorizationPolicy: policy,
				},
			},
		},
		{
			name: "one deny policy",
			ns:   "bar",
			configs: []Config{
				newConfig("authz-1", "bar", denyPolicy),
			},
			wantDeny: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "bar",
					AuthorizationPolicy: denyPolicy,
				},
			},
		},
		{
			name: "two policies",
			ns:   "bar",
			configs: []Config{
				newConfig("authz-1", "foo", policy),
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "bar",
					AuthorizationPolicy: policy,
				},
				{
					Name:                "authz-2",
					Namespace:           "bar",
					AuthorizationPolicy: policy,
				},
			},
		},
		{
			name: "mixing allow and deny policies",
			ns:   "bar",
			configs: []Config{
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", denyPolicy),
			},
			wantDeny: []AuthorizationPolicyConfig{
				{
					Name:                "authz-2",
					Namespace:           "bar",
					AuthorizationPolicy: denyPolicy,
				},
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "bar",
					AuthorizationPolicy: policy,
				},
			},
		},
		{
			name: "selector exact match",
			ns:   "bar",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v1",
			},
			configs: []Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "bar",
					AuthorizationPolicy: policyWithSelector,
				},
			},
		},
		{
			name: "selector subset match",
			ns:   "bar",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v1",
				"env":     "dev",
			},
			configs: []Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "bar",
					AuthorizationPolicy: policyWithSelector,
				},
			},
		},
		{
			name: "selector not match",
			ns:   "bar",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v2",
			},
			configs: []Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: nil,
		},
		{
			name: "namespace not match",
			ns:   "foo",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v1",
			},
			configs: []Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: nil,
		},
		{
			name: "root namespace",
			ns:   "bar",
			configs: []Config{
				newConfig("authz-1", "istio-config", policy),
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "istio-config",
					AuthorizationPolicy: policy,
				},
			},
		},
		{
			name: "root namespace equals config namespace",
			ns:   "istio-config",
			configs: []Config{
				newConfig("authz-1", "istio-config", policy),
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "istio-config",
					AuthorizationPolicy: policy,
				},
			},
		},
		{
			name: "root namespace and config namespace",
			ns:   "bar",
			configs: []Config{
				newConfig("authz-1", "istio-config", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: []AuthorizationPolicyConfig{
				{
					Name:                "authz-1",
					Namespace:           "istio-config",
					AuthorizationPolicy: policy,
				},
				{
					Name:                "authz-2",
					Namespace:           "bar",
					AuthorizationPolicy: policy,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(tc.configs, t)

			gotDeny, gotAllow := authzPolicies.ListAuthorizationPolicies(
				tc.ns, []labels.Instance{tc.workloadLabels})
			if !reflect.DeepEqual(tc.wantAllow, gotAllow) {
				t.Errorf("wantAllow:%v\n but got: %v\n", tc.wantAllow, gotAllow)
			}
			if !reflect.DeepEqual(tc.wantDeny, gotDeny) {
				t.Errorf("wantDeny:%v\n but got: %v\n", tc.wantDeny, gotDeny)
			}
		})
	}
}

func TestAuthorizationPolicies_IsRBACEnabled(t *testing.T) {
	target := &rbacproto.RbacConfig_Target{
		Services:   []string{"review.default.svc", "product.default.svc"},
		Namespaces: []string{"special"},
	}

	testCases := []struct {
		name      string
		config    []Config
		service   string
		namespace string
		want      bool
	}{
		{
			name: "enabled",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode: rbacproto.RbacConfig_ON,
					}),
			},
			service:   "product.default.svc",
			namespace: "default",
			want:      true,
		},
		{
			name: "enabled with permissive",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode:            rbacproto.RbacConfig_ON,
						EnforcementMode: rbacproto.EnforcementMode_PERMISSIVE,
					}),
			},
			service:   "product.default.svc",
			namespace: "default",
			want:      true,
		},
		{
			name: "enabled by inclusion.service",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode:      rbacproto.RbacConfig_ON_WITH_INCLUSION,
						Inclusion: target,
					}),
			},
			service:   "product.default.svc",
			namespace: "default",
			want:      true,
		},
		{
			name: "enabled by inclusion.namespace",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode:      rbacproto.RbacConfig_ON,
						Inclusion: target,
					}),
			},
			service:   "other.special.svc",
			namespace: "special",
			want:      true,
		},
		{
			name: "enabled by ClusterRbacConfig overriding RbacConfig",
			config: []Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      collections.IstioRbacV1Alpha1Rbacconfigs.Resource().Kind(),
						Name:      "default",
						Namespace: "",
					},
					Spec: &rbacproto.RbacConfig{
						Mode: rbacproto.RbacConfig_OFF,
					},
				},
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode: rbacproto.RbacConfig_ON,
					}),
			},
			service:   "override.svc",
			namespace: "ns",
			want:      true,
		},
		{
			name: "disabled by default",
		},
		{
			name: "disabled",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode: rbacproto.RbacConfig_OFF,
					}),
			},
		},
		{
			name: "disabled-if-service-empty",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode: rbacproto.RbacConfig_ON,
					}),
			},
			service:   "",
			namespace: "default",
			want:      false,
		},
		{
			name: "disabled-if-ns-empty",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode: rbacproto.RbacConfig_ON,
					}),
			},
			service: "product.default.svc",
			want:    false,
		},
		{
			name: "disabled by exclusion.service",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode:      rbacproto.RbacConfig_ON_WITH_EXCLUSION,
						Exclusion: target,
					}),
			},
			service:   "product.default.svc",
			namespace: "default",
		},
		{
			name: "disabled by exclusion.namespace",
			config: []Config{
				newConfig("default", "",
					&rbacproto.RbacConfig{
						Mode:      rbacproto.RbacConfig_ON_WITH_EXCLUSION,
						Exclusion: target,
					}),
			},
			service:   "other.special.svc",
			namespace: "special",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(tc.config, t)
			got := authzPolicies.IsRBACEnabled(tc.service, tc.namespace)
			if tc.want != got {
				t.Errorf("want %v but got %v", tc.want, got)
			}
		})
	}
}

func createFakeAuthorizationPolicies(configs []Config, t *testing.T) *AuthorizationPolicies {
	store := &authzFakeStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	environment := &Environment{
		IstioConfigStore: MakeIstioStore(store),
		Watcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-config"}),
	}
	authzPolicies, err := GetAuthorizationPolicies(environment)
	if err != nil {
		t.Fatalf("GetAuthorizationPolicies failed: %v", err)
	}
	return authzPolicies
}

func newConfig(name, ns string, spec proto.Message) Config {
	var kind, version, group string

	switch spec.(type) {
	case *rbacproto.RbacConfig:
		kind = collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Kind()
		version = collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Version()
		group = collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Group()
	case *rbacproto.ServiceRole:
		kind = collections.IstioRbacV1Alpha1Serviceroles.Resource().Kind()
		version = collections.IstioRbacV1Alpha1Serviceroles.Resource().Version()
		group = collections.IstioRbacV1Alpha1Serviceroles.Resource().Group()
	case *rbacproto.ServiceRoleBinding:
		kind = collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Kind()
		version = collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Version()
		group = collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Group()
	case *authpb.AuthorizationPolicy:
		kind = collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Kind()
		version = collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Version()
		group = collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Group()
	}
	return Config{
		ConfigMeta: ConfigMeta{
			Type:      kind,
			Version:   version,
			Group:     group,
			Name:      name,
			Namespace: ns,
		},
		Spec: spec,
	}
}

type authzFakeStore struct {
	data []struct {
		typ resource.GroupVersionKind
		ns  string
		cfg Config
	}
}

func (fs *authzFakeStore) GetLedger() ledger.Ledger {
	panic("implement me")
}

func (fs *authzFakeStore) SetLedger(ledger.Ledger) error {
	panic("implement me")
}

func (fs *authzFakeStore) add(config Config) {
	fs.data = append(fs.data, struct {
		typ resource.GroupVersionKind
		ns  string
		cfg Config
	}{
		typ: config.GroupVersionKind(),
		ns:  config.Namespace,
		cfg: config,
	})
}

func (fs *authzFakeStore) Schemas() collection.Schemas {
	return collection.SchemasFor()
}

func (fs *authzFakeStore) Get(_ resource.GroupVersionKind, _, _ string) *Config {
	return nil
}

func (fs *authzFakeStore) List(typ resource.GroupVersionKind, namespace string) ([]Config, error) {
	var configs []Config
	for _, data := range fs.data {
		if data.typ == typ {
			if namespace != "" && data.ns == namespace {
				continue
			}
			configs = append(configs, data.cfg)
		}
	}
	return configs, nil
}

func (fs *authzFakeStore) Delete(_ resource.GroupVersionKind, _, _ string) error {
	return fmt.Errorf("not implemented")
}
func (fs *authzFakeStore) Create(Config) (string, error) {
	return "not implemented", nil
}

func (fs *authzFakeStore) Update(Config) (string, error) {
	return "not implemented", nil
}

func (fs *authzFakeStore) Version() string {
	return "not implemented"
}
func (fs *authzFakeStore) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return "not implemented", nil
}
