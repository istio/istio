// Copyright 2019 Istio Authors
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

package builder

import (
	"strings"
	"testing"

	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	tcp_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"

	istio_rbac "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schemas"
)

func newService(hostname string, labels map[string]string, t *testing.T) *model.ServiceInstance {
	t.Helper()
	splits := strings.Split(hostname, ".")
	if len(splits) < 2 {
		t.Fatalf("failed to initialize service instance: invalid hostname")
	}
	name := splits[0]
	namespace := splits[1]
	return &model.ServiceInstance{
		Service: &model.Service{
			Attributes: model.ServiceAttributes{
				Name:      name,
				Namespace: namespace,
			},
			Hostname: host.Name(hostname),
		},
		Labels: labels,
	}
}

func simpleGlobalPermissiveMode() *model.Config {
	cfg := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      schemas.ClusterRbacConfig.Type,
			Name:      "default",
			Namespace: "default",
		},
		Spec: &istio_rbac.RbacConfig{
			Mode:            istio_rbac.RbacConfig_ON,
			EnforcementMode: istio_rbac.EnforcementMode_PERMISSIVE,
		},
	}
	return cfg
}

func TestBuilder_BuildHTTPFilter(t *testing.T) {
	service := newService("bar.a.svc.cluster.local", nil, t)

	testCases := []struct {
		name                        string
		policies                    []*model.Config
		isXDSMarshalingToAnyEnabled bool
		wantPolicies                []string
	}{
		{
			name: "XDSMarshalingToAnyEnabled",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
			},
			isXDSMarshalingToAnyEnabled: true,
			wantPolicies:                []string{},
		},
		{
			name: "v1alpha1 only",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
				policy.SimpleRole("role-1", "a", "bar"),
				policy.SimpleBinding("binding-1", "a", "role-1", policy.SimplePrincipal("binding-1")),
			},
			wantPolicies: []string{"role-1"},
		},
		{
			name: "v1alpha1 without ClusterRbacConfig",
			policies: []*model.Config{
				policy.SimpleRole("role-1", "a", "bar"),
				policy.SimpleBinding("binding-1", "a", "role-1", policy.SimplePrincipal("binding-1")),
			},
		},
		{
			name: "v1beta1 only",
			policies: []*model.Config{
				policy.SimpleAuthzPolicy("authz-bar", "a"),
				policy.SimpleAuthzPolicy("authz-foo", "a"),
			},
			wantPolicies: []string{"ns[a]-policy[authz-bar]-rule[0]", "ns[a]-policy[authz-foo]-rule[0]"},
		},
		{
			name: "v1alpha1 and v1beta1",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
				policy.SimpleRole("role-1", "a", "bar"),
				policy.SimpleBinding("binding-1", "a", "role-1", policy.SimplePrincipal("binding-1")),
				policy.SimpleAuthzPolicy("authz-bar", "a"),
			},
			wantPolicies: []string{"ns[a]-policy[authz-bar]-rule[0]"},
		},
	}

	for _, tc := range testCases {
		p := policy.NewAuthzPolicies(tc.policies, t)
		b := NewBuilder(trustdomain.NewTrustDomainBundle("", nil), service, nil, "a", p, tc.isXDSMarshalingToAnyEnabled)

		got := b.BuildHTTPFilter()
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantPolicies == nil {
				if got != nil {
					t.Errorf("want empty config but got: %v", got)
				}
			} else {
				if got.Name != authz_model.RBACHTTPFilterName {
					t.Errorf("got filter name %q but want %q", got.Name, authz_model.RBACHTTPFilterName)
				}
				if tc.isXDSMarshalingToAnyEnabled {
					if got.GetTypedConfig() == nil {
						t.Errorf("want typed config when isXDSMarshalingToAnyEnabled is true")
					}
				} else {
					rbacConfig := &http_config.RBAC{}
					if got.GetConfig() == nil {
						t.Errorf("want struct config when isXDSMarshalingToAnyEnabled is false")
					} else if err := conversion.StructToMessage(got.GetConfig(), rbacConfig); err != nil {
						t.Errorf("failed to convert struct to message: %s", err)
					} else {
						if len(tc.wantPolicies) == 0 {
							if len(rbacConfig.GetRules().GetPolicies()) > 0 {
								t.Errorf("got rules with policies %v but want no policies", rbacConfig.GetRules().GetPolicies())
							}
						} else {
							for _, want := range tc.wantPolicies {
								if _, found := rbacConfig.GetRules().GetPolicies()[want]; !found {
									t.Errorf("got rules with policies %v but want %v", rbacConfig.GetRules().GetPolicies(), want)
								}
							}
							if len(tc.wantPolicies) != len(rbacConfig.GetRules().GetPolicies()) {
								t.Errorf("got %d policies but want %d", len(rbacConfig.GetRules().GetPolicies()), len(tc.wantPolicies))
							}
						}
					}
				}
			}
		})
	}
}

func TestBuilder_BuildTCPFilter(t *testing.T) {
	service := newService("foo.a.svc.cluster.local", nil, t)

	testCases := []struct {
		name                        string
		policies                    []*model.Config
		isXDSMarshalingToAnyEnabled bool
		wantRules                   bool
		wantRuleWithPolicies        bool
		wantShadowRules             bool
	}{
		{
			name: "XDSMarshalingToAnyEnabled",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
			},
			isXDSMarshalingToAnyEnabled: true,
		},
		{
			name: "HTTP rule",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
				policy.SimpleRole("role-1", "a", "foo"),
				policy.SimpleBinding("binding-1", "a", "role-1", policy.SimplePrincipal("binding-1")),
			},
			wantRules:            true,
			wantRuleWithPolicies: false,
		},
		{
			name: "normal rule",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
			},
			wantRules: true,
		},
		{
			name: "normal shadow rule",
			policies: []*model.Config{
				simpleGlobalPermissiveMode(),
			},
			wantShadowRules: true,
		},
	}

	for _, tc := range testCases {
		p := policy.NewAuthzPolicies(tc.policies, t)
		b := NewBuilder(trustdomain.NewTrustDomainBundle("", nil), service, nil, "a", p, tc.isXDSMarshalingToAnyEnabled)

		t.Run(tc.name, func(t *testing.T) {
			got := b.BuildTCPFilter()
			if got.Name != authz_model.RBACTCPFilterName {
				t.Errorf("got filter name %q but want %q", got.Name, authz_model.RBACTCPFilterName)
			}

			if tc.isXDSMarshalingToAnyEnabled {
				if got.GetTypedConfig() == nil {
					t.Errorf("want typed config when isXDSMarshalingToAnyEnabled is true")
				}
			} else {
				rbacConfig := &tcp_config.RBAC{}
				if got.GetConfig() == nil {
					t.Errorf("want struct config when isXDSMarshalingToAnyEnabled is false")
				} else if err := conversion.StructToMessage(got.GetConfig(), rbacConfig); err != nil {
					t.Errorf("failed to convert struct to message: %s", err)
				} else {
					if rbacConfig.StatPrefix != authz_model.RBACTCPFilterStatPrefix {
						t.Errorf("got filter stat prefix %q but want %q",
							rbacConfig.StatPrefix, authz_model.RBACTCPFilterStatPrefix)
					}

					if len(rbacConfig.GetRules().GetPolicies()) > 0 != tc.wantRuleWithPolicies {
						t.Errorf("got rules with policies %v but want %v",
							len(rbacConfig.GetRules().GetPolicies()) > 0, tc.wantRuleWithPolicies)
					}
					if (rbacConfig.GetRules().GetPolicies() != nil) != tc.wantRules {
						t.Errorf("got rules %v but want %v",
							rbacConfig.GetRules().GetPolicies() != nil, tc.wantRules)
					}
					if (rbacConfig.GetShadowRules().GetPolicies() != nil) != tc.wantShadowRules {
						t.Errorf("got shadow rules %v but want %v",
							rbacConfig.GetShadowRules().GetPolicies() != nil, tc.wantShadowRules)
					}
				}
			}
		})
	}
}
