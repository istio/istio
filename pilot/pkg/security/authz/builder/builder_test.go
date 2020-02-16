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

	envoyRbacHttpPb "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	envoyRbacTcpPb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"
	"github.com/golang/protobuf/ptypes"

	istioRbacPb "istio.io/api/rbac/v1alpha1"

	"istio.io/istio/pilot/pkg/model"
	authzModel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/collections"
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
		Endpoint: &model.IstioEndpoint{
			Labels: labels,
		},
	}
}

func simpleGlobalPermissiveMode() *model.Config {
	cfg := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Kind(),
			Group:     collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Group(),
			Version:   collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Version(),
			Name:      "default",
			Namespace: "default",
		},
		Spec: &istioRbacPb.RbacConfig{
			Mode:            istioRbacPb.RbacConfig_ON,
			EnforcementMode: istioRbacPb.EnforcementMode_PERMISSIVE,
		},
	}
	return cfg
}

func TestBuilder_BuildHTTPFilter(t *testing.T) {
	service := newService("bar.a.svc.cluster.local", nil, t)

	testCases := []struct {
		name         string
		policies     []*model.Config
		wantPolicies [][]string
	}{
		{
			name: "v1alpha1 only",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
				policy.SimpleRole("role-1", "a", "bar"),
				policy.SimpleBinding("binding-1", "a", "role-1"),
			},
			wantPolicies: [][]string{
				{"role-1"},
			},
		},
		{
			name: "v1alpha1 without ClusterRbacConfig",
			policies: []*model.Config{
				policy.SimpleRole("role-1", "a", "bar"),
				policy.SimpleBinding("binding-1", "a", "role-1"),
			},
		},
		{
			name: "v1beta1 allow policies",
			policies: []*model.Config{
				policy.SimpleAllowPolicy("authz-bar", "a"),
				policy.SimpleAllowPolicy("authz-foo", "a"),
			},
			wantPolicies: [][]string{
				{"ns[a]-policy[authz-bar]-rule[0]", "ns[a]-policy[authz-foo]-rule[0]"}},
		},
		{
			name: "v1beta1 deny policies",
			policies: []*model.Config{
				policy.SimpleDenyPolicy("authz-bar", "a"),
				policy.SimpleDenyPolicy("authz-foo", "a"),
			},
			wantPolicies: [][]string{
				{"ns[a]-policy[authz-bar]-rule[0]", "ns[a]-policy[authz-foo]-rule[0]"},
			},
		},
		{
			name: "v1alpha1 and v1beta1",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
				policy.SimpleRole("role-1", "a", "bar"),
				policy.SimpleBinding("binding-1", "a", "role-1"),
				policy.SimpleAllowPolicy("authz-allow-bar", "a"),
				policy.SimpleDenyPolicy("authz-deny-bar", "a"),
			},
			wantPolicies: [][]string{
				{"ns[a]-policy[authz-deny-bar]-rule[0]"},
				{"ns[a]-policy[authz-allow-bar]-rule[0]"},
			},
		},
	}

	for _, tc := range testCases {
		p := policy.NewAuthzPolicies(tc.policies, t)
		b := NewBuilder(trustdomain.NewTrustDomainBundle("", nil), service, nil, "a", p)

		filters := b.BuildHTTPFilters()
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantPolicies == nil {
				if len(filters) != 0 {
					t.Errorf("want empty config but got: %v", filters)
				}
			} else {
				for i, got := range filters {
					if got.Name != authzModel.RBACHTTPFilterName {
						t.Errorf("got filter name %q but want %q", got.Name, authzModel.RBACHTTPFilterName)
					}
					if len(tc.wantPolicies) != len(filters) {
						t.Fatalf("want %d filters but found %d", len(tc.wantPolicies), len(filters))
					}
					rbacConfig := &envoyRbacHttpPb.RBAC{}
					if err := ptypes.UnmarshalAny(got.GetTypedConfig(), rbacConfig); err != nil {
						t.Fatalf("failed to unmarshal config: %s", err)
					}
					if len(tc.wantPolicies[i]) == 0 {
						if len(rbacConfig.GetRules().GetPolicies()) > 0 {
							t.Errorf("got rules with policies %v but want no policies", rbacConfig.GetRules().GetPolicies())
						}
					} else {
						for _, want := range tc.wantPolicies[i] {
							if _, found := rbacConfig.GetRules().GetPolicies()[want]; !found {
								t.Errorf("got rules with policies %v but want %v", rbacConfig.GetRules().GetPolicies(), want)
							}
						}
						if len(tc.wantPolicies[i]) != len(rbacConfig.GetRules().GetPolicies()) {
							t.Errorf("got %d policies but want %d", len(rbacConfig.GetRules().GetPolicies()), len(tc.wantPolicies[i]))
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
		name                 string
		policies             []*model.Config
		wantRules            bool
		wantRuleWithPolicies bool
		wantShadowRules      bool
	}{
		{
			name: "HTTP rule",
			policies: []*model.Config{
				policy.SimpleClusterRbacConfig(),
				policy.SimpleRole("role-1", "a", "foo"),
				policy.SimpleBinding("binding-1", "a", "role-1"),
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
		b := NewBuilder(trustdomain.NewTrustDomainBundle("", nil), service, nil, "a", p)

		t.Run(tc.name, func(t *testing.T) {
			filters := b.BuildTCPFilters()
			if len(filters) != 1 {
				t.Fatalf("want 1 filter but got %d", len(filters))
			}
			got := filters[0]
			if got.Name != authzModel.RBACTCPFilterName {
				t.Errorf("got filter name %q but want %q", got.Name, authzModel.RBACTCPFilterName)
			}
			rbacConfig := &envoyRbacTcpPb.RBAC{}
			if err := ptypes.UnmarshalAny(got.GetTypedConfig(), rbacConfig); err != nil {
				t.Fatalf("failed to unmarshal config: %s", err)
			}
			if rbacConfig.StatPrefix != authzModel.RBACTCPFilterStatPrefix {
				t.Errorf("got filter stat prefix %q but want %q",
					rbacConfig.StatPrefix, authzModel.RBACTCPFilterStatPrefix)
			}
			if len(rbacConfig.GetRules().GetPolicies()) > 0 != tc.wantRuleWithPolicies {
				t.Errorf("got rules with policies %v but want %v",
					len(rbacConfig.GetRules().GetPolicies()) > 0, tc.wantRuleWithPolicies)
			}
			if (rbacConfig.GetRules() != nil) != tc.wantRules {
				t.Errorf("got rules %v but want %v",
					rbacConfig.GetRules() != nil, tc.wantRules)
			}
			if (rbacConfig.GetShadowRules() != nil) != tc.wantShadowRules {
				t.Errorf("got shadow rules %v but want %v",
					rbacConfig.GetShadowRules() != nil, tc.wantShadowRules)
			}
		})
	}
}
