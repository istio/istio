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

// TODO(pitlv2109, yangminzhu): Need to refactor all unit tests. Tests are becoming hard to maintain.

package authz

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/networking/plugin/authz/matcher"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	network_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"
	policy "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
	"github.com/gogo/protobuf/types"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	authn_v1alpha1 "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
)

func newAuthzPoliciesWithRolesAndBindings(configs ...[]model.Config) *model.AuthorizationPolicies {
	authzPolicies := &model.AuthorizationPolicies{}

	for _, config := range configs {
		for _, cfg := range config {
			authzPolicies.AddConfig(&cfg)
		}
	}
	return authzPolicies
}

func newAuthzPolicyWithRbacConfig(mode rbacproto.RbacConfig_Mode, include *rbacproto.RbacConfig_Target,
	exclude *rbacproto.RbacConfig_Target, enforceMode rbacproto.EnforcementMode) *model.AuthorizationPolicies {
	return &model.AuthorizationPolicies{
		RbacConfig: &rbacproto.RbacConfig{
			Mode:            mode,
			Inclusion:       include,
			Exclusion:       exclude,
			EnforcementMode: enforceMode,
		},
	}
}

func TestIsRbacEnabled(t *testing.T) {
	target := &rbacproto.RbacConfig_Target{
		Services:   []string{"review.default.svc", "product.default.svc"},
		Namespaces: []string{"special"},
	}
	cfg1 := newAuthzPolicyWithRbacConfig(rbacproto.RbacConfig_ON, nil, nil, rbacproto.EnforcementMode_ENFORCED)
	cfg2 := newAuthzPolicyWithRbacConfig(rbacproto.RbacConfig_OFF, nil, nil, rbacproto.EnforcementMode_ENFORCED)
	cfg3 := newAuthzPolicyWithRbacConfig(rbacproto.RbacConfig_ON_WITH_INCLUSION, target, nil, rbacproto.EnforcementMode_ENFORCED)
	cfg4 := newAuthzPolicyWithRbacConfig(rbacproto.RbacConfig_ON_WITH_EXCLUSION, nil, target, rbacproto.EnforcementMode_ENFORCED)
	cfg5 := newAuthzPolicyWithRbacConfig(rbacproto.RbacConfig_ON, nil, nil, rbacproto.EnforcementMode_PERMISSIVE)

	testCases := []struct {
		Name               string
		AuthzPolicies      *model.AuthorizationPolicies
		Service            string
		Namespace          string
		ExpectedEnabled    bool
		ExpectedPermissive bool
	}{
		{
			Name:            "rbac plugin enabled",
			AuthzPolicies:   cfg1,
			ExpectedEnabled: true,
		},
		{
			Name:          "rbac plugin disabled",
			AuthzPolicies: cfg2,
		},
		{
			Name:            "rbac plugin enabled by inclusion.service",
			AuthzPolicies:   cfg3,
			Service:         "product.default.svc",
			Namespace:       "default",
			ExpectedEnabled: true,
		},
		{
			Name:            "rbac plugin enabled by inclusion.namespace",
			AuthzPolicies:   cfg3,
			Service:         "other.special.svc",
			Namespace:       "special",
			ExpectedEnabled: true,
		},
		{
			Name:          "rbac plugin disabled by exclusion.service",
			AuthzPolicies: cfg4,
			Service:       "product.default.svc",
			Namespace:     "default",
		},
		{
			Name:          "rbac plugin disabled by exclusion.namespace",
			AuthzPolicies: cfg4,
			Service:       "other.special.svc",
			Namespace:     "special",
		},
		{
			Name:               "rbac plugin enabled with permissive",
			AuthzPolicies:      cfg5,
			ExpectedEnabled:    true,
			ExpectedPermissive: true,
		},
	}

	for _, tc := range testCases {
		gotEnabled, gotPermissive := isRbacEnabled(tc.Service, tc.Namespace, tc.AuthzPolicies)
		if tc.ExpectedEnabled != gotEnabled {
			t.Errorf("%s: expecting %v but got %v", tc.Name, tc.ExpectedEnabled, gotEnabled)
		}

		if tc.ExpectedPermissive != gotPermissive {
			t.Errorf("%s: expecting %v but got %v", tc.Name, tc.ExpectedPermissive, gotPermissive)
		}
	}
}

func TestBuildTCPFilter(t *testing.T) {
	roleCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRole.Type, Name: "test-role-1", Namespace: "default"},
		Spec: &rbacproto.ServiceRole{
			Rules: []*rbacproto.AccessRule{{Services: []string{"mongoDB1.default"}}},
		},
	}
	bindingCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: "default"},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}

	roleCfg2 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRole.Type, Name: "test-role-2", Namespace: "default"},
		Spec: &rbacproto.ServiceRole{
			Rules: []*rbacproto.AccessRule{{Services: []string{"mongoDB2.default"}}},
		},
	}
	bindingCfg2 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-group", Namespace: "default"},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{Group: "test-group"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-2"},
		},
	}

	roleCfg3 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRole.Type, Name: "test-role-3", Namespace: "default"},
		Spec: &rbacproto.ServiceRole{
			Rules: []*rbacproto.AccessRule{
				{Services: []string{"mongoDB3.default"}, Methods: []string{"GET-method"}},
			},
		},
	}
	bindingCfg3 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-3", Namespace: "default"},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-3"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-3"},
		},
	}

	option := rbacOption{
		authzPolicies: newAuthzPoliciesWithRolesAndBindings([]model.Config{
			roleCfg, roleCfg2, roleCfg3, bindingCfg, bindingCfg2, bindingCfg3}),
	}

	testCases := []struct {
		Name    string
		Service *serviceMetadata
		Option  rbacOption
		Policy  string
	}{
		{
			Name: "role with binding",
			Service: &serviceMetadata{
				name: "mongoDB1.default", attributes: map[string]string{attrDestName: "mongoDB1", attrDestNamespace: "default"}},
			Option: option,
			Policy: "test-role-1",
		},
		{
			Name: "no group binding for role",
			Service: &serviceMetadata{
				name: "mongoDB2.default", attributes: map[string]string{attrDestName: "mongoDB2", attrDestNamespace: "default"}},
			Option: option,
		},
		{
			Name: "no binding for role with HTTP method",
			Service: &serviceMetadata{
				name: "mongoDB3.default", attributes: map[string]string{attrDestName: "mongoDB3", attrDestNamespace: "default"}},
			Option: option,
		},
	}

	for _, tc := range testCases {
		filter := buildTCPFilter(tc.Service, tc.Option, true)
		if fn := "envoy.filters.network.rbac"; filter.Name != fn {
			t.Errorf("%s: expecting filter name %s, but got %s", tc.Name, fn, filter.Name)
		}
		if filter == nil {
			t.Errorf("%s: expecting valid config, but got nil", tc.Name)
		} else {
			rbacConfig := &network_config.RBAC{}
			if err := rbacConfig.Unmarshal(filter.GetTypedConfig().GetValue()); err != nil {
				t.Errorf("%s: bad rbac config: %v", tc.Name, err)
			} else {
				if rbacConfig.StatPrefix != "tcp." {
					t.Errorf("%s: expecting stat prefix tcp. but got %v", tc.Name, rbacConfig.StatPrefix)
				}
				rbac := rbacConfig.Rules
				if rbac.Action != policy.RBAC_ALLOW {
					t.Errorf("%s: expecting allow action but got %v", tc.Name, rbac.Action.String())
				}
				if tc.Policy == "" {
					if len(rbac.Policies) != 0 {
						t.Errorf("%s: expecting empty policies but got %v", tc.Name, rbac.Policies)
					}
				} else if _, ok := rbac.Policies[tc.Policy]; !ok {
					t.Errorf("%s: expecting policy %s but got %v", tc.Name, tc.Policy, rbac.Policies)
				}
			}
		}
	}
}

func TestBuildHTTPFilter(t *testing.T) {
	roleCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRole.Type, Name: "test-role-1", Namespace: "default"},
		Spec: &rbacproto.ServiceRole{
			Rules: []*rbacproto.AccessRule{
				{Services: []string{"product.default"}, Methods: []string{"GET-method"}},
			},
		},
	}
	roleCfgWithoutBinding := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRole.Type, Name: "test-role-2", Namespace: "default"},
		Spec: &rbacproto.ServiceRole{
			Rules: []*rbacproto.AccessRule{
				{Services: []string{"review.default"}, Methods: []string{"GET-method"}},
			},
		},
	}
	bindingCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: "default"},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}
	option := rbacOption{
		authzPolicies: newAuthzPoliciesWithRolesAndBindings([]model.Config{roleCfg, roleCfgWithoutBinding, bindingCfg}),
	}

	testCases := []struct {
		Name    string
		Service *serviceMetadata
		Option  rbacOption
		Policy  string
	}{
		{
			Name: "no matched role",
			Service: &serviceMetadata{
				name: "abc.xyz", attributes: map[string]string{attrDestName: "abc", attrDestNamespace: "xyz"}},
			Option: option,
		},
		{
			Name: "no matched binding",
			Service: &serviceMetadata{
				name: "review.default", attributes: map[string]string{attrDestName: "review", attrDestNamespace: "default"}},
			Option: option,
		},
		{
			Name: "role with binding",
			Service: &serviceMetadata{
				name: "product.default", attributes: map[string]string{attrDestName: "product", attrDestNamespace: "default"}},
			Option: option,
			Policy: "test-role-1",
		},
	}

	for _, tc := range testCases {
		filter := buildHTTPFilter(tc.Service, tc.Option, true)
		if fn := "envoy.filters.http.rbac"; filter.Name != fn {
			t.Errorf("%s: expecting filter name %s, but got %s", tc.Name, fn, filter.Name)
		}
		if filter == nil {
			t.Errorf("%s: expecting valid config, but got nil", tc.Name)
		} else {
			rbacConfig := &http_config.RBAC{}
			if err := rbacConfig.Unmarshal(filter.GetTypedConfig().GetValue()); err != nil {
				t.Errorf("%s: bad rbac config: %v", tc.Name, err)
			} else {
				rbac := rbacConfig.Rules
				if rbac.Action != policy.RBAC_ALLOW {
					t.Errorf("%s: expecting allow action but got %v", tc.Name, rbac.Action.String())
				}
				if tc.Policy == "" {
					if len(rbac.Policies) != 0 {
						t.Errorf("%s: expecting empty policies but got %v", tc.Name, rbac.Policies)
					}
				} else if _, ok := rbac.Policies[tc.Policy]; !ok {
					t.Errorf("%s: expecting policy %s but got %v", tc.Name, tc.Policy, rbac.Policies)
				}
			}
		}
	}
}

func TestConvertRbacRulesToFilterConfig(t *testing.T) {
	roles := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-1"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services:   []string{"prefix.*", "*.suffix", "service"},
						Methods:    []string{"GET", "POST"},
						NotMethods: []string{"DELETE"},
						Paths:      []string{"*/suffix", "/prefix*", "/exact", "*"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-2"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"*"},
						Constraints: []*rbacproto.AccessRule_Constraint{
							{Key: "destination.port", Values: []string{"80", "443"}},
							{Key: "destination.ip", Values: []string{"192.1.2.0/24", "2001:db8::/28"}},
							{Key: "request.headers[key1]", Values: []string{"prefix*", "*suffix"}},
							{Key: "request.headers[key2]", Values: []string{"simple", "*"}},
							{Key: "destination.labels[version]", Values: []string{"v10"}},
							{Key: "destination.name", Values: []string{"attr-name"}},
							{Key: "connection.sni", Values: []string{"*.example.com"}},
						},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-3"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services:   []string{"allow-group"},
						Methods:    []string{"GET"},
						NotMethods: []string{"*"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-4"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"mongoDB1"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-5"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"mongoDB2"},
						Constraints: []*rbacproto.AccessRule_Constraint{
							{Key: "destination.port", Values: []string{"80", "443"}},
							{Key: "destination.ip", Values: []string{"192.1.2.0/24", "2001:db8::/28"}},
						},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-6"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{Services: []string{"service_empty"}},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-7"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"mysql"},
						Constraints: []*rbacproto.AccessRule_Constraint{
							{Key: "experimental.envoy.filters.network.mysql_proxy[db.table]", Values: []string{"[update]"}},
						},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-8"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"dummy"},
						Constraints: []*rbacproto.AccessRule_Constraint{
							{Key: "experimental.envoy.filters.dummy[key]", Values: []string{"value"}},
						},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-9"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"service-9"},
						Ports:    []int32{9080, 3000},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-10"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"service-10"},
						Hosts:    []string{"*.google.com"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-11"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"service-11"},
						NotHosts: []string{"finances.google.com"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-12"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services:   []string{"backup_service"},
						NotMethods: []string{"DELETE"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-13"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"service-13"},
						NotPaths: []string{"/secret_path"},
					},
				},
			},
		},
	}
	bindings := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-1"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						User: "*",
					},
					{
						Properties: map[string]string{
							attrSrcPrincipal: "user",
						},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-1",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-2"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Properties: map[string]string{
							"request.auth.claims[groups]": "group*",
							"request.auth.claims[iss]":    "test-iss",
							"request.headers[key]":        "value",
							"source.ip":                   "192.1.2.0/24",
							"source.namespace":            "test-ns",
						},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-2",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-3"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Group:      "group*",
						Properties: map[string]string{},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-3",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-4"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						User:          "admin",
						Namespaces:    []string{"default"},
						NotNamespaces: []string{"user", "deprecated"},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-4",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-5"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Properties: map[string]string{
							"source.ip":        "192.1.2.0/24",
							"source.namespace": "default",
							attrSrcPrincipal:   "cluster.local/ns/default/sa/productpage",
						},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-5",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-6"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-6",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-7"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						User: "*",
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-7",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-8"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						User:   "*",
						NotIps: []string{"192.1.2.0/24"},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-8",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-9"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Ips: []string{"10.38.25.152", "10.48.1.18"},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-9",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-10"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Groups: []string{"admin-group", "testing-group"},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-10",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-11"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Groups:    []string{"*-group"},
						NotGroups: []string{"deprecated-group"},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-11",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-12"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Names: []string{allAuthenticatedUsers},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-12",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-13"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						NotNames: []string{"cluster.local/ns/testing/sa/unstable-service"},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-13",
				},
			},
		},
	}

	policy1 := &policy.Policy{
		Permissions: []*policy.Permission{
			{
				Rule: &policy.Permission_AndRules{
					AndRules: &policy.Permission_Set{
						Rules: []*policy.Permission{
							{
								Rule: generateHeaderRule([]*route.HeaderMatcher{
									{Name: ":method", HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "GET"}},
									{Name: ":method", HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "POST"}},
								}),
							},
							{
								Rule: &policy.Permission_NotRule{
									NotRule: &policy.Permission{
										Rule: generateHeaderRule([]*route.HeaderMatcher{
											{Name: ":method", HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "DELETE"}},
										}),
									},
								},
							},
							{
								Rule: generateHeaderRule([]*route.HeaderMatcher{
									{Name: ":path", HeaderMatchSpecifier: &route.HeaderMatcher_SuffixMatch{SuffixMatch: "/suffix"}},
									{Name: ":path", HeaderMatchSpecifier: &route.HeaderMatcher_PrefixMatch{PrefixMatch: "/prefix"}},
									{Name: ":path", HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "/exact"}},
									{Name: ":path", HeaderMatchSpecifier: &route.HeaderMatcher_PresentMatch{PresentMatch: true}},
								}),
							},
						},
					},
				},
			},
		},
		Principals: []*policy.Principal{
			{
				Identifier: &policy.Principal_AndIds{
					AndIds: &policy.Principal_Set{
						Ids: []*policy.Principal{
							{
								Identifier: &policy.Principal_Any{
									Any: true,
								},
							},
						},
					},
				},
			},
			{
				Identifier: &policy.Principal_AndIds{
					AndIds: &policy.Principal_Set{
						Ids: []*policy.Principal{
							{
								Identifier: &policy.Principal_Metadata{
									Metadata: matcher.MetadataStringMatcher(
										authn_v1alpha1.AuthnFilterName,
										attrSrcPrincipal, &metadata.StringMatcher{
											MatchPattern: &metadata.StringMatcher_Exact{Exact: "user"}}),
								},
							},
						},
					},
				},
			},
		},
	}

	policy2 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{
						{
							Rule: &policy.Permission_OrRules{
								OrRules: &policy.Permission_Set{
									Rules: []*policy.Permission{
										{
											Rule: &policy.Permission_RequestedServerName{
												RequestedServerName: &metadata.StringMatcher{
													MatchPattern: &metadata.StringMatcher_Suffix{
														Suffix: ".example.com",
													},
												},
											},
										},
									},
								},
							},
						},
						{Rule: generateDestinationCidrRule([]string{"192.1.2.0", "2001:db8::"}, []uint32{24, 28})},
						{Rule: generateDestinationPortRule([]uint32{80, 443})},
						{
							Rule: generateHeaderRule([]*route.HeaderMatcher{
								{Name: "key1", HeaderMatchSpecifier: &route.HeaderMatcher_PrefixMatch{PrefixMatch: "prefix"}},
								{Name: "key1", HeaderMatchSpecifier: &route.HeaderMatcher_SuffixMatch{SuffixMatch: "suffix"}},
							}),
						},
						{
							Rule: generateHeaderRule([]*route.HeaderMatcher{
								{Name: "key2", HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "simple"}},
								{Name: "key2", HeaderMatchSpecifier: &route.HeaderMatcher_PresentMatch{PresentMatch: true}},
							}),
						},
					},
				},
			},
		}},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_Metadata{
								Metadata: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName,
									[]string{attrRequestClaims, "groups"}, "group*"),
							},
						},
						{
							Identifier: &policy.Principal_Metadata{
								Metadata: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName,
									[]string{attrRequestClaims, "iss"}, "test-iss"),
							},
						},
						{
							Identifier: &policy.Principal_Header{
								Header: &route.HeaderMatcher{
									Name: "key",
									HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
										ExactMatch: "value",
									},
								},
							},
						},
						{
							Identifier: &policy.Principal_SourceIp{
								SourceIp: &core.CidrRange{
									AddressPrefix: "192.1.2.0",
									PrefixLen:     &types.UInt32Value{Value: 24},
								},
							},
						},
						{
							Identifier: &policy.Principal_Metadata{
								Metadata: matcher.MetadataStringMatcher(
									authn_v1alpha1.AuthnFilterName,
									attrSrcPrincipal, &metadata.StringMatcher{
										MatchPattern: &metadata.StringMatcher_Regex{Regex: `.*/ns/test-ns/.*`}}),
							},
						},
					},
				},
			},
		}},
	}

	policy3 := generatePolicyWithHTTPMethodAndGroupClaim("GET", "group*")

	policy4 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{
						{
							Rule: &policy.Permission_Any{
								Any: true,
							},
						},
					},
				},
			},
		},
		},
		Principals: []*policy.Principal{
			{
				Identifier: &policy.Principal_AndIds{
					AndIds: &policy.Principal_Set{
						Ids: []*policy.Principal{
							{
								Identifier: &policy.Principal_Authenticated_{
									Authenticated: &policy.Principal_Authenticated{
										PrincipalName: &metadata.StringMatcher{
											MatchPattern: &metadata.StringMatcher_Exact{
												Exact: "spiffe://admin",
											},
										},
									},
								},
							},
							{
								Identifier: &policy.Principal_OrIds{
									OrIds: &policy.Principal_Set{
										Ids: []*policy.Principal{
											{
												Identifier: &policy.Principal_Authenticated_{
													Authenticated: &policy.Principal_Authenticated{
														PrincipalName: &metadata.StringMatcher{
															MatchPattern: &metadata.StringMatcher_Regex{
																Regex: ".*/ns/default/.*",
															},
														},
													},
												},
											},
										},
									},
								},
							},
							{
								Identifier: &policy.Principal_NotId{
									NotId: &policy.Principal{
										Identifier: &policy.Principal_OrIds{
											OrIds: &policy.Principal_Set{
												Ids: []*policy.Principal{
													{
														Identifier: &policy.Principal_Authenticated_{
															Authenticated: &policy.Principal_Authenticated{
																PrincipalName: &metadata.StringMatcher{
																	MatchPattern: &metadata.StringMatcher_Regex{
																		Regex: ".*/ns/user/.*",
																	},
																},
															},
														},
													},
													{
														Identifier: &policy.Principal_Authenticated_{
															Authenticated: &policy.Principal_Authenticated{
																PrincipalName: &metadata.StringMatcher{
																	MatchPattern: &metadata.StringMatcher_Regex{
																		Regex: ".*/ns/deprecated/.*",
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	policy5 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{
						{Rule: generateDestinationCidrRule([]string{"192.1.2.0", "2001:db8::"}, []uint32{24, 28})},
						{Rule: generateDestinationPortRule([]uint32{80, 443})},
					},
				},
			},
		},
		},
		Principals: []*policy.Principal{
			{
				Identifier: &policy.Principal_AndIds{
					AndIds: &policy.Principal_Set{
						Ids: []*policy.Principal{
							{
								Identifier: &policy.Principal_SourceIp{
									SourceIp: &core.CidrRange{
										AddressPrefix: "192.1.2.0",
										PrefixLen:     &types.UInt32Value{Value: 24},
									},
								},
							},
							{
								Identifier: &policy.Principal_Authenticated_{
									Authenticated: &policy.Principal_Authenticated{
										PrincipalName: &metadata.StringMatcher{
											MatchPattern: &metadata.StringMatcher_Regex{
												Regex: ".*/ns/default/.*",
											},
										},
									},
								},
							},
							{
								Identifier: &policy.Principal_Authenticated_{
									Authenticated: &policy.Principal_Authenticated{
										PrincipalName: &metadata.StringMatcher{
											MatchPattern: &metadata.StringMatcher_Exact{
												Exact: "spiffe://cluster.local/ns/default/sa/productpage",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	policy6 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{
						{Rule: &policy.Permission_Any{Any: true}},
					},
				},
			},
		},
		},
		Principals: []*policy.Principal{
			{
				Identifier: &policy.Principal_AndIds{
					AndIds: &policy.Principal_Set{
						Ids: []*policy.Principal{
							{
								Identifier: &policy.Principal_NotId{
									NotId: &policy.Principal{
										Identifier: &policy.Principal_Any{Any: true},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	policy7 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{{
						Rule: &policy.Permission_OrRules{
							OrRules: &policy.Permission_Set{
								Rules: []*policy.Permission{{
									Rule: &policy.Permission_Metadata{
										Metadata: matcher.MetadataListMatcher("envoy.filters.network.mysql_proxy", []string{"db.table"}, "update"),
									},
								}},
							},
						},
					}},
				},
			},
		}},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{{
						Identifier: &policy.Principal_Any{
							Any: true,
						},
					}},
				},
			},
		}},
	}

	policy8sm := matcher.StringMatcher("value")
	policy8 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{{
						Rule: &policy.Permission_OrRules{
							OrRules: &policy.Permission_Set{
								Rules: []*policy.Permission{{
									Rule: &policy.Permission_Metadata{
										Metadata: matcher.MetadataStringMatcher("envoy.filters.dummy", "key", policy8sm),
									},
								}},
							},
						},
					}},
				},
			},
		}},

		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_Any{
								Any: true,
							},
						},
						{
							Identifier: &policy.Principal_NotId{
								NotId: &policy.Principal{
									Identifier: &policy.Principal_OrIds{
										OrIds: &policy.Principal_Set{
											Ids: []*policy.Principal{
												{
													Identifier: &policy.Principal_SourceIp{SourceIp: &core.CidrRange{
														AddressPrefix: "192.1.2.0",
														PrefixLen:     &types.UInt32Value{Value: uint32(24)},
													}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
	}

	policy9 := &policy.Policy{
		Permissions: []*policy.Permission{
			{
				Rule: &policy.Permission_AndRules{
					AndRules: &policy.Permission_Set{
						Rules: []*policy.Permission{
							{
								Rule: generateDestinationPortRule([]uint32{9080, 3000}),
							},
						},
					},
				},
			},
		},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_OrIds{
								OrIds: &policy.Principal_Set{
									Ids: []*policy.Principal{
										{
											Identifier: &policy.Principal_SourceIp{
												SourceIp: &core.CidrRange{
													AddressPrefix: "10.38.25.152",
													PrefixLen:     &types.UInt32Value{Value: 32},
												},
											},
										},
										{
											Identifier: &policy.Principal_SourceIp{
												SourceIp: &core.CidrRange{
													AddressPrefix: "10.48.1.18",
													PrefixLen:     &types.UInt32Value{Value: 32},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
	}

	policy10 := &policy.Policy{
		Permissions: []*policy.Permission{
			{
				Rule: &policy.Permission_AndRules{
					AndRules: &policy.Permission_Set{
						Rules: []*policy.Permission{
							{
								Rule: generateHeaderRule([]*route.HeaderMatcher{
									{Name: hostHeader, HeaderMatchSpecifier: &route.HeaderMatcher_SuffixMatch{SuffixMatch: ".google.com"}},
								}),
							},
						},
					},
				},
			},
		},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_OrIds{
								OrIds: &policy.Principal_Set{
									Ids: []*policy.Principal{
										{
											Identifier: &policy.Principal_Metadata{
												Metadata: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName,
													[]string{attrRequestClaims, "groups"}, "admin-group"),
											},
										},
										{
											Identifier: &policy.Principal_Metadata{
												Metadata: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName,
													[]string{attrRequestClaims, "groups"}, "testing-group"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
	}

	policy11 := &policy.Policy{
		Permissions: []*policy.Permission{
			{
				Rule: &policy.Permission_AndRules{
					AndRules: &policy.Permission_Set{
						Rules: []*policy.Permission{
							{
								Rule: &policy.Permission_NotRule{
									NotRule: &policy.Permission{
										Rule: generateHeaderRule([]*route.HeaderMatcher{
											{Name: hostHeader, HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "finances.google.com"}},
										}),
									},
								},
							},
						},
					},
				},
			},
		},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_OrIds{
								OrIds: &policy.Principal_Set{
									Ids: []*policy.Principal{
										{
											Identifier: &policy.Principal_Metadata{
												Metadata: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName,
													[]string{attrRequestClaims, "groups"}, "*-group"),
											},
										},
									},
								},
							},
						},
						{
							Identifier: &policy.Principal_NotId{
								NotId: &policy.Principal{
									Identifier: &policy.Principal_OrIds{
										OrIds: &policy.Principal_Set{
											Ids: []*policy.Principal{
												{
													Identifier: &policy.Principal_Metadata{
														Metadata: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName,
															[]string{attrRequestClaims, "groups"}, "deprecated-group"),
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
	}

	policy12 := &policy.Policy{
		Permissions: []*policy.Permission{
			{
				Rule: &policy.Permission_AndRules{
					AndRules: &policy.Permission_Set{
						Rules: []*policy.Permission{
							{
								Rule: &policy.Permission_NotRule{
									NotRule: &policy.Permission{
										Rule: generateHeaderRule([]*route.HeaderMatcher{
											{Name: methodHeader, HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "DELETE"}},
										}),
									},
								},
							},
						},
					},
				},
			},
		},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_OrIds{
								OrIds: &policy.Principal_Set{
									Ids: []*policy.Principal{
										{
											Identifier: &policy.Principal_Metadata{
												Metadata: matcher.MetadataStringMatcher(
													authn_v1alpha1.AuthnFilterName,
													attrSrcPrincipal, &metadata.StringMatcher{
														MatchPattern: &metadata.StringMatcher_Regex{
															Regex: ".*",
														}}),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
	}
	policy13 := &policy.Policy{
		Permissions: []*policy.Permission{
			{
				Rule: &policy.Permission_AndRules{
					AndRules: &policy.Permission_Set{
						Rules: []*policy.Permission{
							{
								Rule: &policy.Permission_NotRule{
									NotRule: &policy.Permission{
										Rule: generateHeaderRule([]*route.HeaderMatcher{
											{Name: pathHeader, HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "/secret_path"}},
										}),
									},
								},
							},
						},
					},
				},
			},
		},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_NotId{
								NotId: &policy.Principal{
									Identifier: &policy.Principal_OrIds{
										OrIds: &policy.Principal_Set{
											Ids: []*policy.Principal{
												{
													Identifier: &policy.Principal_Metadata{
														Metadata: matcher.MetadataStringMatcher(
															authn_v1alpha1.AuthnFilterName,
															attrSrcPrincipal, &metadata.StringMatcher{
																MatchPattern: &metadata.StringMatcher_Exact{Exact: "cluster.local/ns/testing/sa/unstable-service"}}),
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
	}

	expectRbac1 := &policy.RBAC{
		Action: policy.RBAC_ALLOW,
		Policies: map[string]*policy.Policy{
			"service-role-1": policy1,
			"service-role-2": policy2,
		},
	}

	expectRbac2 := generateExpectRBACForSinglePolicy("service-role-2", policy2)
	expectRbac3 := generateExpectRBACForSinglePolicy("service-role-3", policy3)
	expectRbac4 := generateExpectRBACForSinglePolicy("service-role-4", policy4)
	expectRbac5 := generateExpectRBACForSinglePolicy("service-role-5", policy5)
	expectRbac6 := generateExpectRBACForSinglePolicy("service-role-6", policy6)

	expectRbac7 := &policy.RBAC{
		Action: policy.RBAC_ALLOW,
		Policies: map[string]*policy.Policy{
			"service-role-7": policy7,
		},
	}
	expectRbac8 := &policy.RBAC{
		Action: policy.RBAC_ALLOW,
		Policies: map[string]*policy.Policy{
			"service-role-8": policy8,
		},
	}
	expectRbac9 := generateExpectRBACForSinglePolicy("service-role-9", policy9)
	expectRbac10 := generateExpectRBACForSinglePolicy("service-role-10", policy10)
	expectRbac11 := generateExpectRBACForSinglePolicy("service-role-11", policy11)
	expectRbac12 := generateExpectRBACForSinglePolicy("service-role-12", policy12)
	expectRbac13 := generateExpectRBACForSinglePolicy("service-role-13", policy13)

	authzPolicies := newAuthzPoliciesWithRolesAndBindings(roles, bindings)
	option := rbacOption{authzPolicies: authzPolicies}
	testCases := []struct {
		name    string
		service *serviceMetadata
		rbac    *policy.RBAC
		option  rbacOption
	}{
		{
			name: "prefix matched service",
			service: &serviceMetadata{
				name:       "prefix.service",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac:   expectRbac1,
			option: option,
		},
		{
			name: "suffix matched service",
			service: &serviceMetadata{
				name:       "service.suffix",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac:   expectRbac1,
			option: option,
		},
		{
			name: "exact matched service",
			service: &serviceMetadata{
				name:       "service",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac:   expectRbac1,
			option: option,
		},
		{
			name: "* matched service",
			service: &serviceMetadata{
				name:       "unknown",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac:   expectRbac2,
			option: option,
		},
		{
			name: "test group",
			service: &serviceMetadata{
				name: "allow-group",
			},
			rbac:   expectRbac3,
			option: option,
		},
		{
			name: "tcp service without properties and constraints",
			service: &serviceMetadata{
				name: "mongoDB1",
			},
			rbac:   expectRbac4,
			option: rbacOption{authzPolicies: authzPolicies, forTCPFilter: true},
		},
		{
			name: "tcp service with properties and constraints",
			service: &serviceMetadata{
				name: "mongoDB2",
			},
			rbac:   expectRbac5,
			option: rbacOption{authzPolicies: authzPolicies, forTCPFilter: true},
		},
		{
			name: "empty service",
			service: &serviceMetadata{
				name: "service_empty",
			},
			rbac:   expectRbac6,
			option: option,
		},
		{
			name: "service with metadata list constraints",
			service: &serviceMetadata{
				name: "mysql",
			},
			rbac:   expectRbac7,
			option: rbacOption{authzPolicies: authzPolicies, forTCPFilter: true},
		},
		{
			name: "service with metadata value constraints",
			service: &serviceMetadata{
				name: "dummy",
			},
			rbac:   expectRbac8,
			option: rbacOption{authzPolicies: authzPolicies, forTCPFilter: true},
		},
		{
			name: "ports rule",
			service: &serviceMetadata{
				name: "service-9",
			},
			rbac:   expectRbac9,
			option: option,
		},
		{
			name: "hosts rule",
			service: &serviceMetadata{
				name: "service-10",
			},
			rbac:   expectRbac10,
			option: option,
		},
		{
			name: "not_hosts rule",
			service: &serviceMetadata{
				name: "service-11",
			},
			rbac:   expectRbac11,
			option: option,
		},
		{
			name: "not_methods rule",
			service: &serviceMetadata{
				name: "backup_service",
			},
			rbac:   expectRbac12,
			option: option,
		},
		{
			name: "not_paths rule",
			service: &serviceMetadata{
				name: "service-13",
			},
			rbac:   expectRbac13,
			option: option,
		},
	}

	spewCfg := spew.NewDefaultConfig()
	spewCfg.SortKeys = true
	for _, tc := range testCases {
		rbac := convertRbacRulesToFilterConfig(tc.service, tc.option)
		want := spewCfg.Sdump(*tc.rbac)
		got := spewCfg.Sdump(*rbac.Rules)
		if got != want {
			t.Errorf("%s\nwant: %s\n got: %s", tc.name, want, got)
		}
	}
}

func TestConvertRbacRulesToFilterConfigForServiceWithBothHTTPAndTCP(t *testing.T) {
	roles := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-3"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"svc_http_tcp"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-2"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"svc_http_tcp"},
						Constraints: []*rbacproto.AccessRule_Constraint{
							{Key: "destination.port", Values: []string{"9090"}},
						},
					},
					{
						Services: []string{"svc_http_tcp"},
						Methods:  []string{"GET"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-1"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"svc_http_tcp"},
						Methods:  []string{"GET"},
					},
					{
						Services: []string{"svc_http_tcp"},
					},
					{
						Services: []string{"svc_http_tcp"},
						Constraints: []*rbacproto.AccessRule_Constraint{
							{Key: "destination.port", Values: []string{"9090"}},
						},
					},
				},
			},
		},
	}
	bindings := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-1"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{User: "admin"},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-1",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-2"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{User: "admin"},
					{Properties: map[string]string{attrSrcIP: "1.2.3.4"}},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-2",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-3"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{User: "user"},
					{Group: "group"},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-3",
				},
			},
		},
	}

	authzPolicies := newAuthzPoliciesWithRolesAndBindings(roles, bindings)
	testCases := []struct {
		name           string
		expectPolicies map[string]struct {
			permissionCount int
			principalCount  int
		}
		option rbacOption
	}{
		{
			name: "http",
			expectPolicies: map[string]struct {
				permissionCount int
				principalCount  int
			}{
				"service-role-1": {permissionCount: 3, principalCount: 1},
				"service-role-2": {permissionCount: 2, principalCount: 2},
				"service-role-3": {permissionCount: 1, principalCount: 2},
			},
			option: rbacOption{authzPolicies: authzPolicies},
		},
		{
			name: "tcp",
			expectPolicies: map[string]struct {
				permissionCount int
				principalCount  int
			}{
				"service-role-1": {permissionCount: 2, principalCount: 1},
				"service-role-2": {permissionCount: 1, principalCount: 2},
			},
			option: rbacOption{authzPolicies: authzPolicies, forTCPFilter: true},
		},
	}

	for _, tc := range testCases {
		rbac := convertRbacRulesToFilterConfig(&serviceMetadata{name: "svc_http_tcp"}, tc.option)
		if rbac.ShadowRules != nil && len(rbac.ShadowRules.Policies) != 0 {
			t.Errorf("%s: expecting empty shadow rules but found: %v", tc.name, rbac.ShadowRules)
		}
		if rbac.Rules == nil {
			t.Errorf("%s: expecting rules but found nil", tc.name)
		}
		for expectPolicy, value := range tc.expectPolicies {
			actual, ok := rbac.Rules.Policies[expectPolicy]
			if !ok {
				t.Errorf("%s: expecting policy %s but not found in %v", tc.name, expectPolicy, rbac.Rules)
			}
			if len(actual.Principals) != value.principalCount {
				t.Errorf("%s: expecting %d principal but found %v", tc.name, value.principalCount, actual.Principals)
			}
			if len(actual.Permissions) != value.permissionCount {
				t.Errorf("%s: expecting %d permission but found %v", tc.name, value.permissionCount, actual.Permissions)
			}
		}
	}
}

func TestConvertRbacRulesToFilterConfigPermissive(t *testing.T) {
	roles := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-1"},
			Spec:       generateServiceRole([]string{"service"}, []string{"GET"}),
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-2"},
			Spec:       generateServiceRole([]string{"service"}, []string{"POST"}),
		},
	}
	bindings := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-1"},
			Spec:       generateServiceBinding("user1", "service-role-1", rbacproto.EnforcementMode_ENFORCED),
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-2"},
			Spec:       generateServiceBinding("user2", "service-role-1", rbacproto.EnforcementMode_PERMISSIVE),
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-3"},
			Spec:       generateServiceBinding("user3", "service-role-2", rbacproto.EnforcementMode_ENFORCED),
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-4"},
			Spec:       generateServiceBinding("user4", "service-role-2", rbacproto.EnforcementMode_PERMISSIVE),
		},
	}

	rbacConfig := &http_config.RBAC{
		Rules: &policy.RBAC{
			Action: policy.RBAC_ALLOW,
			Policies: map[string]*policy.Policy{
				"service-role-1": {
					Permissions: []*policy.Permission{generatePermission(":method", "GET")},
					Principals:  []*policy.Principal{generatePrincipal("user1")},
				},
				"service-role-2": {
					Permissions: []*policy.Permission{generatePermission(":method", "POST")},
					Principals:  []*policy.Principal{generatePrincipal("user3")},
				},
			},
		},
		ShadowRules: &policy.RBAC{
			Action: policy.RBAC_ALLOW,
			Policies: map[string]*policy.Policy{
				"service-role-1": {
					Permissions: []*policy.Permission{generatePermission(":method", "GET")},
					Principals:  []*policy.Principal{generatePrincipal("user2")},
				},
				"service-role-2": {
					Permissions: []*policy.Permission{generatePermission(":method", "POST")},
					Principals:  []*policy.Principal{generatePrincipal("user4")},
				},
			},
		},
	}

	globalPermissiveConfig := &http_config.RBAC{
		ShadowRules: &policy.RBAC{
			Action: policy.RBAC_ALLOW,
			Policies: map[string]*policy.Policy{
				"service-role-1": {
					Permissions: []*policy.Permission{generatePermission(":method", "GET")},
					Principals:  []*policy.Principal{generatePrincipal("user1"), generatePrincipal("user2")},
				},
				"service-role-2": {
					Permissions: []*policy.Permission{generatePermission(":method", "POST")},
					Principals:  []*policy.Principal{generatePrincipal("user3"), generatePrincipal("user4")},
				},
			},
		},
	}

	emptyConfig := &http_config.RBAC{
		Rules: &policy.RBAC{
			Action:   policy.RBAC_ALLOW,
			Policies: map[string]*policy.Policy{},
		},
	}

	testCases := []struct {
		name         string
		service      *serviceMetadata
		option       rbacOption
		expectConfig *http_config.RBAC
	}{
		{
			name:         "exact matched service",
			service:      &serviceMetadata{name: "service"},
			option:       rbacOption{authzPolicies: newAuthzPoliciesWithRolesAndBindings(roles, bindings)},
			expectConfig: rbacConfig,
		},
		{
			name:         "empty roles",
			service:      &serviceMetadata{name: "service"},
			option:       rbacOption{authzPolicies: newAuthzPoliciesWithRolesAndBindings(bindings)},
			expectConfig: emptyConfig,
		},
		{
			name:         "empty bindings",
			service:      &serviceMetadata{name: "service"},
			option:       rbacOption{authzPolicies: newAuthzPoliciesWithRolesAndBindings(roles)},
			expectConfig: emptyConfig,
		},
		{
			name:    "global permissive",
			service: &serviceMetadata{name: "service"},
			option: rbacOption{
				authzPolicies: newAuthzPoliciesWithRolesAndBindings(roles, bindings), globalPermissiveMode: true},
			expectConfig: globalPermissiveConfig,
		},
	}

	for _, tc := range testCases {
		rbac := convertRbacRulesToFilterConfig(tc.service, tc.option)
		if !reflect.DeepEqual(*tc.expectConfig, *rbac) {
			t.Errorf("%s rbac config want:\n%v\nbut got:\n%v", tc.name, *tc.expectConfig, *rbac)
		}
	}
}

func TestCreateServiceMetadata(t *testing.T) {
	expect := &serviceMetadata{
		name:   "svc-name.test-ns",
		labels: map[string]string{"version": "v1"},
		attributes: map[string]string{
			attrDestName:      "svc-name",
			attrDestNamespace: "test-ns",
			attrDestUser:      "service-account",
		},
	}
	serviceInstance := &model.ServiceInstance{
		Service: &model.Service{
			Hostname: model.Hostname("svc-name.test-ns"),
		},
		Labels:         model.Labels{"version": "v1"},
		ServiceAccount: "spiffe://xyz.com/sa/service-account/ns/test-ns",
	}
	actual := newServiceMetadata(&model.ServiceAttributes{Name: "svc-name", Namespace: "test-ns"}, serviceInstance)

	if !reflect.DeepEqual(*actual, *expect) {
		t.Errorf("expecting %v, but got %v", *expect, *actual)
	}

	actual = newServiceMetadata(&model.ServiceAttributes{Name: "svc-name", Namespace: ""}, serviceInstance)
	if actual != nil {
		t.Errorf("expecting nil, but got %v", *actual)
	}
}

func TestGenerateMetadataStringMatcher(t *testing.T) {
	actual := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName,
		"aa", &metadata.StringMatcher{MatchPattern: &metadata.StringMatcher_Regex{Regex: "regex"}})
	expect := &metadata.MetadataMatcher{
		Filter: "istio_authn",
		Path: []*metadata.MetadataMatcher_PathSegment{
			{Segment: &metadata.MetadataMatcher_PathSegment_Key{Key: "aa"}},
		},
		Value: &metadata.ValueMatcher{
			MatchPattern: &metadata.ValueMatcher_StringMatch{
				StringMatch: &metadata.StringMatcher{
					MatchPattern: &metadata.StringMatcher_Regex{
						Regex: "regex",
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(*actual, *expect) {
		t.Errorf("expecting %v, but got %v", *expect, *actual)
	}
}

func TestPrincipalForProperty(t *testing.T) {
	cases := []struct {
		k    string
		v    string
		tcp  bool
		want *metadata.MetadataMatcher
	}{
		{
			k: attrSrcNamespace, v: "test-ns*", tcp: false,
			want: matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName,
				attrSrcPrincipal, &metadata.StringMatcher{
					MatchPattern: &metadata.StringMatcher_Regex{
						Regex: `.*/ns/test-ns.*/.*`,
					},
				}),
		},
		{
			k: attrRequestClaims + "[groups]", v: "group*", tcp: false,
			want: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName, []string{attrRequestClaims, "groups"}, "group*"),
		},
		{
			k: attrRequestClaims + "[iss]", v: "test-iss", tcp: false,
			want: matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName, []string{attrRequestClaims, "iss"}, "test-iss"),
		},
		{
			k: attrSrcUser, v: "*test-user", tcp: false,
			want: matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName,
				attrSrcUser, &metadata.StringMatcher{
					MatchPattern: &metadata.StringMatcher_Suffix{
						Suffix: "test-user",
					},
				}),
		},
		{
			k: attrRequestAudiences, v: "test-audiences", tcp: false,
			want: matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName,
				attrRequestAudiences, &metadata.StringMatcher{
					MatchPattern: &metadata.StringMatcher_Exact{
						Exact: "test-audiences",
					},
				}),
		},
		{
			k: attrRequestPresenter, v: "*", tcp: false,
			want: matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName,
				attrRequestPresenter, &metadata.StringMatcher{
					MatchPattern: &metadata.StringMatcher_Regex{
						Regex: ".*",
					},
				}),
		},
		{
			k: "custom.attribute", v: "custom-value", tcp: false,
			want: matcher.MetadataStringMatcher(rbacHTTPFilterName,
				"custom.attribute", &metadata.StringMatcher{
					MatchPattern: &metadata.StringMatcher_Exact{
						Exact: "custom-value",
					},
				}),
		},
		{
			k: "custom.attribute", v: "custom-value", tcp: true,
			want: matcher.MetadataStringMatcher(rbacTCPFilterName,
				"custom.attribute", &metadata.StringMatcher{
					MatchPattern: &metadata.StringMatcher_Exact{
						Exact: "custom-value",
					},
				}),
		},
	}

	for _, tc := range cases {
		got := principalForKeyValue(tc.k, tc.v, tc.tcp).GetMetadata()
		if !reflect.DeepEqual(*got, *tc.want) {
			t.Errorf("(%s, %v):\nwant: %v\n got: %v", tc.k, tc.v, *tc.want, *got)
		}
	}
}
