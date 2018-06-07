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
	"strings"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	policy "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	"github.com/gogo/protobuf/types"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
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

func newRbacConfig(name, ns string, mode rbacproto.RbacConfig_Mode) model.Config {
	return model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.RbacConfig.Type, Name: name, Namespace: ns},
		Spec: &rbacproto.RbacConfig{
			Mode: mode,
		},
	}
}

func TestIsRbacEnabled(t *testing.T) {
	cfg1 := newRbacConfig("rbac-config", "default", rbacproto.RbacConfig_ON)
	cfg2 := newRbacConfig("cfg2", kube.IstioNamespace, rbacproto.RbacConfig_ON)
	cfg3 := newRbacConfig("rbac-config", kube.IstioNamespace, rbacproto.RbacConfig_ON)
	cfg4 := newRbacConfig("rbac-config", kube.IstioNamespace, rbacproto.RbacConfig_OFF)
	testCases := []struct {
		Name  string
		Store model.IstioConfigStore
		Ret   bool
	}{
		{
			Name:  "zero rbacConfig",
			Store: newIstioStoreWithConfigs([]model.Config{cfg1}, t),
		},
		{
			Name:  "wrong rbacConfig name",
			Store: newIstioStoreWithConfigs([]model.Config{cfg1, cfg2}, t),
		},
		{
			Name:  "rbac plugin disabled",
			Store: newIstioStoreWithConfigs([]model.Config{cfg4}, t),
		},
		{
			Name:  "valid filter",
			Store: newIstioStoreWithConfigs([]model.Config{cfg1, cfg3}, t),
			Ret:   true,
		},
	}

	for _, tc := range testCases {
		ret := isRbacEnabled(tc.Store)
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
	bindingCfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.ServiceRoleBinding.Type, Name: "test-binding-1", Namespace: "default"},
		Spec: &rbacproto.ServiceRoleBinding{
			Subjects: []*rbacproto.Subject{{User: "test-user-1"}},
			RoleRef:  &rbacproto.RoleRef{Kind: "ServiceRole", Name: "test-role-1"},
		},
	}
	store := newIstioStoreWithConfigs([]model.Config{roleCfg, bindingCfg}, t)

	testCases := []struct {
		Name          string
		Host          string
		Store         model.IstioConfigStore
		Err           string
		ExpectContain bool
		Strings       []string
	}{
		{
			Name:          "empty filter",
			Host:          "abc.xyz",
			Store:         store,
			ExpectContain: false,
			Strings:       []string{"test-role-1", "GET-method", "test-user-1"},
		},
		{
			Name:          "valid filter",
			Host:          "product.default",
			Store:         store,
			ExpectContain: true,
			Strings:       []string{"test-role-1", "GET-method", "test-user-1"},
		},
	}

	for _, tc := range testCases {
		filter, err := buildHTTPFilter(model.Hostname(tc.Host), tc.Store)
		if err != nil {
			if !strings.HasPrefix(err.Error(), tc.Err) {
				t.Errorf("%s: expecting error %q, but got %q", tc.Name, tc.Err, err.Error())
			}
		} else {
			if fn := "envoy.filters.http.rbac"; filter.Name != fn {
				t.Errorf("%s: expecting filter name %s, but got %s", tc.Name, fn, filter.Name)
			}
			for _, expect := range tc.Strings {
				if r := strings.Contains(filter.Config.String(), expect); r != tc.ExpectContain {
					not := " not"
					if tc.ExpectContain {
						not = ""
					}
					t.Errorf("%s: expecting filter config to%s contain %s, but got %v",
						tc.Name, not, expect, filter.Config.String())
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
							{Key: "request.header[key1]", Values: []string{"prefix*", "*suffix"}},
							{Key: "request.header[key2]", Values: []string{"simple", "*"}},
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
						User: "user",
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
							"request.header[key]": "value",
							"source.service":      "service-name",
							"source.ip":           "192.1.2.0/24",
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
													HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
														RegexMatch: "^.*/suffix$",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":path",
													HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
														RegexMatch: "^/prefix.*$",
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
													HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
														RegexMatch: "^.*$",
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
							Identifier: &policy.Principal_Authenticated_{
								Authenticated: &policy.Principal_Authenticated{
									Name: "user",
								},
							},
						},
					},
				},
			},
		}},
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
													HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
														RegexMatch: "^prefix.*$",
													},
												},
											},
										},
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: "key1",
													HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
														RegexMatch: "^.*suffix$",
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
													HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
														RegexMatch: "^.*$",
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
							Identifier: &policy.Principal_Header{
								Header: &route.HeaderMatcher{
									Name: ":service",
									HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
										ExactMatch: "service-name",
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
	expectRbac2 := &policy.RBAC{
		Action: policy.RBAC_ALLOW,
		Policies: map[string]*policy.Policy{
			"service-role-2": policy2,
		},
	}
	testCases := []struct {
		name    string
		service string
		rbac    *policy.RBAC
	}{
		{
			name:    "prefix matched service",
			service: "prefix.service",
			rbac:    expectRbac1,
		},
		{
			name:    "suffix matched service",
			service: "service.suffix",
			rbac:    expectRbac1,
		},
		{
			name:    "exact matched service",
			service: "service",
			rbac:    expectRbac1,
		},
		{
			name:    "* matched service",
			service: "unknown",
			rbac:    expectRbac2,
		},
	}

	for _, tc := range testCases {
		rbac := convertRbacRulesToFilterConfig(tc.service, roles, bindings)
		if !reflect.DeepEqual(*tc.rbac, *rbac.Rules) {
			t.Errorf("%s want:\n%v\nbut got:\n%v", tc.name, *tc.rbac, *rbac.Rules)
		}
	}
}
