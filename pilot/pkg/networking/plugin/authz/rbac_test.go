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

package authz

import (
	"reflect"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	rbacconfig "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	policy "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

func newIstioStoreWithConfigs(configs []model.Config, t *testing.T) model.IstioConfigStore {
	store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))
	for _, cfg := range configs {
		_, err := store.Create(cfg)
		if err != nil {
			t.Fatalf("failed to add config %v to istio store: %v", cfg, err)
		}
	}
	return store
}

func newRbacConfig(mode rbacproto.RbacConfig_Mode,
	include *rbacproto.RbacConfig_Target, exclude *rbacproto.RbacConfig_Target) model.Config {
	return model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.RbacConfig.Type, Name: model.DefaultRbacConfigName},
		Spec: &rbacproto.RbacConfig{
			Mode:      mode,
			Inclusion: include,
			Exclusion: exclude,
		},
	}
}

func TestIsRbacEnabled(t *testing.T) {
	target := &rbacproto.RbacConfig_Target{
		Services:   []string{"review.default.svc", "product.default.svc"},
		Namespaces: []string{"special"},
	}
	cfg1 := newRbacConfig(rbacproto.RbacConfig_ON, nil, nil)
	cfg2 := newRbacConfig(rbacproto.RbacConfig_OFF, nil, nil)
	cfg3 := newRbacConfig(rbacproto.RbacConfig_ON_WITH_INCLUSION, target, nil)
	cfg4 := newRbacConfig(rbacproto.RbacConfig_ON_WITH_EXCLUSION, nil, target)

	testCases := []struct {
		Name      string
		Store     model.IstioConfigStore
		Service   string
		Namespace string
		Ret       bool
	}{
		{
			Name:  "rbac plugin enabled",
			Store: newIstioStoreWithConfigs([]model.Config{cfg1}, t),
			Ret:   true,
		},
		{
			Name:  "rbac plugin disabled",
			Store: newIstioStoreWithConfigs([]model.Config{cfg2}, t),
		},
		{
			Name:      "rbac plugin enabled by inclusion.service",
			Store:     newIstioStoreWithConfigs([]model.Config{cfg3}, t),
			Service:   "product.default.svc",
			Namespace: "default",
			Ret:       true,
		},
		{
			Name:      "rbac plugin enabled by inclusion.namespace",
			Store:     newIstioStoreWithConfigs([]model.Config{cfg3}, t),
			Service:   "other.special.svc",
			Namespace: "special",
			Ret:       true,
		},
		{
			Name:      "rbac plugin disabled by exclusion.service",
			Store:     newIstioStoreWithConfigs([]model.Config{cfg4}, t),
			Service:   "product.default.svc",
			Namespace: "default",
		},
		{
			Name:      "rbac plugin disabled by exclusion.namespace",
			Store:     newIstioStoreWithConfigs([]model.Config{cfg4}, t),
			Service:   "other.special.svc",
			Namespace: "special",
		},
	}

	for _, tc := range testCases {
		ret := isRbacEnabled(tc.Service, tc.Namespace, tc.Store)
		if tc.Ret != ret {
			t.Errorf("%s: expecting %v but got %v", tc.Name, tc.Ret, ret)
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
	store := newIstioStoreWithConfigs([]model.Config{roleCfg, roleCfgWithoutBinding, bindingCfg}, t)
	testCases := []struct {
		Name    string
		Service *serviceMetadata
		Store   model.IstioConfigStore
		Policy  string
	}{
		{
			Name: "no matched role",
			Service: &serviceMetadata{
				name: "abc.xyz", attributes: map[string]string{attrDestName: "abc", attrDestNamespace: "xyz"}},
			Store: store,
		},
		{
			Name: "no matched binding",
			Service: &serviceMetadata{
				name: "review.default", attributes: map[string]string{attrDestName: "review", attrDestNamespace: "default"}},
			Store: store,
		},
		{
			Name: "role with binding",
			Service: &serviceMetadata{
				name: "product.default", attributes: map[string]string{attrDestName: "product", attrDestNamespace: "default"}},
			Store:  store,
			Policy: "test-role-1",
		},
	}

	for _, tc := range testCases {
		filter := buildHTTPFilter(tc.Service, tc.Store)
		if fn := "envoy.filters.http.rbac"; filter.Name != fn {
			t.Errorf("%s: expecting filter name %s, but got %s", tc.Name, fn, filter.Name)
		}
		if filter == nil {
			t.Errorf("%s: expecting valid config, but got nil", tc.Name)
		} else {
			rbacConfig := &rbacconfig.RBAC{}
			if err := util.StructToMessage(filter.Config, rbacConfig); err != nil {
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
						Services: []string{"prefix.*", "*.suffix", "service"},
						Methods:  []string{"GET", "POST"},
						Paths:    []string{"*/suffix", "/prefix*", "/exact", "*"},
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
					{
						User: "*",
					},
					{
						Properties: map[string]string{
							"source.principal": "user",
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
	}

	policy1 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{
						{
							Rule: &policy.Permission_OrRules{
								OrRules: &policy.Permission_Set{
									Rules: []*policy.Permission{
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":method",
													HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
														ExactMatch: "GET",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":method",
													HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
														ExactMatch: "POST",
													},
												},
											},
										},
									},
								},
							},
						},
						{
							Rule: &policy.Permission_OrRules{
								OrRules: &policy.Permission_Set{
									Rules: []*policy.Permission{
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":path",
													HeaderMatchSpecifier: &route.HeaderMatcher_SuffixMatch{
														SuffixMatch: "/suffix",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":path",
													HeaderMatchSpecifier: &route.HeaderMatcher_PrefixMatch{
														PrefixMatch: "/prefix",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":path",
													HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
														ExactMatch: "/exact",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":path",
													HeaderMatchSpecifier: &route.HeaderMatcher_PresentMatch{
														PresentMatch: true,
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
		Principals: []*policy.Principal{{
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
									Metadata: generateMetadataStringMatcher(
										[]string{"source.principal"}, &metadata.StringMatcher{
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
											Rule: &policy.Permission_DestinationPort{
												DestinationPort: 80,
											},
										},
										{
											Rule: &policy.Permission_DestinationPort{
												DestinationPort: 443,
											},
										},
									},
								},
							},
						},
						{
							Rule: &policy.Permission_OrRules{
								OrRules: &policy.Permission_Set{
									Rules: []*policy.Permission{
										{
											Rule: &policy.Permission_DestinationIp{
												DestinationIp: &core.CidrRange{
													AddressPrefix: "192.1.2.0",
													PrefixLen:     &types.UInt32Value{Value: 24},
												},
											},
										},
										{
											Rule: &policy.Permission_DestinationIp{
												DestinationIp: &core.CidrRange{
													AddressPrefix: "2001:db8::",
													PrefixLen:     &types.UInt32Value{Value: 28},
												},
											}},
									},
								},
							},
						},
						{
							Rule: &policy.Permission_OrRules{
								OrRules: &policy.Permission_Set{
									Rules: []*policy.Permission{
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: "key1",
													HeaderMatchSpecifier: &route.HeaderMatcher_PrefixMatch{
														PrefixMatch: "prefix",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: "key1",
													HeaderMatchSpecifier: &route.HeaderMatcher_SuffixMatch{
														SuffixMatch: "suffix",
													},
												},
											},
										},
									},
								},
							},
						},
						{
							Rule: &policy.Permission_OrRules{
								OrRules: &policy.Permission_Set{
									Rules: []*policy.Permission{
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: "key2",
													HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
														ExactMatch: "simple",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: "key2",
													HeaderMatchSpecifier: &route.HeaderMatcher_PresentMatch{
														PresentMatch: true,
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
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_Metadata{
								Metadata: generateMetadataListMatcher(
									[]string{authDerivedClaims, "groups"}, "group*"),
							},
						},
						{
							Identifier: &policy.Principal_Metadata{
								Metadata: generateMetadataListMatcher(
									[]string{authDerivedClaims, "iss"}, "test-iss"),
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
								Metadata: generateMetadataStringMatcher(
									[]string{"source.principal"}, &metadata.StringMatcher{
										MatchPattern: &metadata.StringMatcher_Regex{Regex: `.*/ns/test-ns/.*`}}),
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
	expectRbac2 := &policy.RBAC{
		Action: policy.RBAC_ALLOW,
		Policies: map[string]*policy.Policy{
			"service-role-2": policy2,
		},
	}
	testCases := []struct {
		name    string
		service *serviceMetadata
		rbac    *policy.RBAC
	}{
		{
			name: "prefix matched service",
			service: &serviceMetadata{
				name:       "prefix.service",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac: expectRbac1,
		},
		{
			name: "suffix matched service",
			service: &serviceMetadata{
				name:       "service.suffix",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac: expectRbac1,
		},
		{
			name: "exact matched service",
			service: &serviceMetadata{
				name:       "service",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac: expectRbac1,
		},
		{
			name: "* matched service",
			service: &serviceMetadata{
				name:       "unknown",
				labels:     map[string]string{"version": "v10"},
				attributes: map[string]string{"destination.name": "attr-name"},
			},
			rbac: expectRbac2,
		},
	}

	for _, tc := range testCases {
		rbac := convertRbacRulesToFilterConfig(tc.service, roles, bindings)
		if !reflect.DeepEqual(*tc.rbac, *rbac.Rules) {
			t.Errorf("%s want:\n%v\nbut got:\n%v", tc.name, *tc.rbac, *rbac.Rules)
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

	rbacConfig := &rbacconfig.RBAC{
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
	emptyConfig := &rbacconfig.RBAC{
		Rules: &policy.RBAC{
			Action:   policy.RBAC_ALLOW,
			Policies: map[string]*policy.Policy{},
		},
		ShadowRules: &policy.RBAC{
			Action:   policy.RBAC_ALLOW,
			Policies: map[string]*policy.Policy{},
		},
	}

	testCases := []struct {
		name         string
		service      *serviceMetadata
		roles        []model.Config
		bindings     []model.Config
		expectConfig *rbacconfig.RBAC
	}{
		{
			name:         "exact matched service",
			service:      &serviceMetadata{name: "service"},
			roles:        roles,
			bindings:     bindings,
			expectConfig: rbacConfig,
		},
		{
			name:         "empty roles",
			service:      &serviceMetadata{name: "service"},
			roles:        []model.Config{},
			bindings:     bindings,
			expectConfig: emptyConfig,
		},
		{
			name:         "empty bindings",
			service:      &serviceMetadata{name: "service"},
			roles:        roles,
			bindings:     []model.Config{},
			expectConfig: emptyConfig,
		},
	}

	for _, tc := range testCases {
		rbac := convertRbacRulesToFilterConfig(tc.service, tc.roles, tc.bindings)
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
	actual := createServiceMetadata(
		&model.ServiceAttributes{Name: "svc-name", Namespace: "test-ns"},
		&model.ServiceInstance{
			Service: &model.Service{
				Hostname: model.Hostname("svc-name.test-ns"),
			},
			Labels:         model.Labels{"version": "v1"},
			ServiceAccount: "spiffe://xyz.com/sa/service-account/ns/test-ns",
		})

	if !reflect.DeepEqual(*actual, *expect) {
		t.Errorf("expecting %v, but got %v", *expect, *actual)
	}
}

func TestServiceMetadataMatch(t *testing.T) {
	cases := []struct {
		Name    string
		Service *serviceMetadata
		Rule    *rbacproto.AccessRule
		Expect  bool
	}{
		{
			Name:    "empty access rule",
			Service: &serviceMetadata{},
			Expect:  true,
		},
		{
			Name: "service.name not matched",
			Service: &serviceMetadata{
				name:       "product.default",
				attributes: map[string]string{"destination.name": "s2"},
			},
			Rule: &rbacproto.AccessRule{
				Services: []string{"review.default"},
				Constraints: []*rbacproto.AccessRule_Constraint{
					{Key: "destination.name", Values: []string{"s1", "s2"}},
				},
			},
			Expect: false,
		},
		{
			Name: "constraint.name not matched",
			Service: &serviceMetadata{
				name:       "product.default",
				attributes: map[string]string{"destination.name": "s3"},
			},
			Rule: &rbacproto.AccessRule{
				Services: []string{"product.default"},
				Constraints: []*rbacproto.AccessRule_Constraint{
					{Key: "destination.name", Values: []string{"s1", "s2"}},
				},
			},
			Expect: false,
		},
		{
			Name: "constraint.label not matched",
			Service: &serviceMetadata{
				name:   "product.default",
				labels: map[string]string{"token": "t3"},
			},
			Rule: &rbacproto.AccessRule{
				Services: []string{"product.default"},
				Constraints: []*rbacproto.AccessRule_Constraint{
					{Key: "destination.labels[token]", Values: []string{"t1", "t2"}},
				},
			},
			Expect: false,
		},
		{
			Name: "allt matched",
			Service: &serviceMetadata{
				name: "product.default",
				attributes: map[string]string{
					"destination.name": "s2", "destination.namespace": "ns2", "destination.user": "sa2", "other": "other"},
				labels: map[string]string{"token": "t2"},
			},
			Rule: &rbacproto.AccessRule{
				Services: []string{"product.default"},
				Constraints: []*rbacproto.AccessRule_Constraint{
					{Key: "destination.name", Values: []string{"s1", "s2"}},
					{Key: "destination.namespace", Values: []string{"ns1", "ns2"}},
					{Key: "destination.user", Values: []string{"sa1", "sa2"}},
					{Key: "destination.labels[token]", Values: []string{"t1", "t2"}},
					{Key: "request.headers[user-agent]", Values: []string{"x1", "x2"}},
				},
			},
			Expect: true,
		},
	}

	for _, tc := range cases {
		if tc.Service.match(tc.Rule) != tc.Expect {
			t.Errorf("%s: expecting %v for service %v and rule %v",
				tc.Name, tc.Expect, tc.Service, tc.Rule)
		}
	}
}

func TestGenerateMetadataStringMatcher(t *testing.T) {
	actual := generateMetadataStringMatcher(
		[]string{"aa", "bb"},
		&metadata.StringMatcher{MatchPattern: &metadata.StringMatcher_Regex{Regex: "regex"}})
	expect := &metadata.MetadataMatcher{
		Filter: "istio_authn",
		Path: []*metadata.MetadataMatcher_PathSegment{
			{Segment: &metadata.MetadataMatcher_PathSegment_Key{Key: "aa"}},
			{Segment: &metadata.MetadataMatcher_PathSegment_Key{Key: "bb"}},
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

func TestCreateDynamicMetadataMatcher(t *testing.T) {
	cases := []struct {
		k      string
		v      string
		expect *metadata.MetadataMatcher
	}{
		{
			k: attrSrcNamespace, v: "test-ns*",
			expect: generateMetadataStringMatcher([]string{attrSrcPrincipal}, &metadata.StringMatcher{
				MatchPattern: &metadata.StringMatcher_Regex{
					Regex: `.*/ns/test-ns.*/.*`,
				},
			}),
		},
		{
			k: "request.auth.claims[groups]", v: "group*",
			expect: generateMetadataListMatcher([]string{authDerivedClaims, "groups"}, "group*"),
		},
		{
			k: "request.auth.claims[iss]", v: "test-iss",
			expect: generateMetadataListMatcher([]string{authDerivedClaims, "iss"}, "test-iss"),
		},
		{
			k: attrSrcUser, v: "*test-user",
			expect: generateMetadataStringMatcher([]string{attrSrcUser}, &metadata.StringMatcher{
				MatchPattern: &metadata.StringMatcher_Suffix{
					Suffix: "test-user",
				},
			}),
		},
		{
			k: attrRequestAudiences, v: "test-audiences",
			expect: generateMetadataStringMatcher([]string{attrRequestAudiences}, &metadata.StringMatcher{
				MatchPattern: &metadata.StringMatcher_Exact{
					Exact: "test-audiences",
				},
			}),
		},
		{
			k: attrRequestPresenter, v: "*",
			expect: generateMetadataStringMatcher([]string{attrRequestPresenter}, &metadata.StringMatcher{
				MatchPattern: &metadata.StringMatcher_Regex{
					Regex: ".*",
				},
			}),
		},
	}

	for _, tc := range cases {
		actual := createDynamicMetadataMatcher(tc.k, tc.v)
		if !reflect.DeepEqual(*actual, *tc.expect) {
			t.Errorf("(%s, %v): expecting %v, but got %v", tc.k, tc.v, *tc.expect, *actual)
		}
	}
}

func generateServiceRole(services, methods []string) *rbacproto.ServiceRole {
	return &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services: services,
				Methods:  methods,
			},
		},
	}
}

func generateServiceBinding(subject, serviceRoleRef string, mode rbacproto.EnforcementMode) *rbacproto.ServiceRoleBinding {
	return &rbacproto.ServiceRoleBinding{
		Mode: mode,
		Subjects: []*rbacproto.Subject{
			{
				User: subject,
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: serviceRoleRef,
		},
	}
}

func generatePermission(headerName, matchSpecifier string) *policy.Permission {
	return &policy.Permission{
		Rule: &policy.Permission_AndRules{
			AndRules: &policy.Permission_Set{
				Rules: []*policy.Permission{
					{
						Rule: &policy.Permission_OrRules{
							OrRules: &policy.Permission_Set{
								Rules: []*policy.Permission{
									{
										Rule: &policy.Permission_Header{
											Header: &route.HeaderMatcher{
												Name: headerName,
												HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
													ExactMatch: matchSpecifier,
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
}

func generatePrincipal(principalName string) *policy.Principal {
	return &policy.Principal{
		Identifier: &policy.Principal_AndIds{
			AndIds: &policy.Principal_Set{
				Ids: []*policy.Principal{
					{
						Identifier: &policy.Principal_Metadata{
							Metadata: generateMetadataStringMatcher(
								[]string{"source.principal"}, &metadata.StringMatcher{
									MatchPattern: &metadata.StringMatcher_Exact{Exact: principalName}}),
						},
					},
				},
			},
		},
	}
}
