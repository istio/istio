// Copyright Istio Authors
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
	"testing"

	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	authpb "istio.io/api/security/v1beta1"
	apitypev1beta1 "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config/schema/gvk"
)

const testNS = "ns-x"

// classicGatewayTargetRefPolicy builds an ALLOW AuthorizationPolicy whose (single) targetRef points at a
// classic networking.istio.io Gateway named gwName.
func classicGatewayTargetRefPolicy(name, ns, gwName string) model.AuthorizationPolicy {
	return model.AuthorizationPolicy{
		Name:      name,
		Namespace: ns,
		Spec: &authpb.AuthorizationPolicy{
			Action: authpb.AuthorizationPolicy_ALLOW,
			TargetRefs: []*apitypev1beta1.PolicyTargetReference{{
				Group: gvk.Gateway.Group, // networking.istio.io
				Kind:  gvk.Gateway.Kind,  // Gateway
				Name:  gwName,
			}},
			Rules: []*authpb.Rule{{
				From: []*authpb.Rule_From{{
					Source: &authpb.Source{Principals: []string{"cluster.local/ns/default/sa/client"}},
				}},
			}},
		},
	}
}

// selectorPolicy builds an ALLOW AuthorizationPolicy selected by workload labels (no targetRef).
func selectorPolicy(name, ns string, matchLabels map[string]string) model.AuthorizationPolicy {
	return model.AuthorizationPolicy{
		Name:      name,
		Namespace: ns,
		Spec: &authpb.AuthorizationPolicy{
			Action:   authpb.AuthorizationPolicy_ALLOW,
			Selector: &apitypev1beta1.WorkloadSelector{MatchLabels: matchLabels},
			Rules: []*authpb.Rule{{
				From: []*authpb.Rule_From{{
					Source: &authpb.Source{Principals: []string{"cluster.local/ns/default/sa/client"}},
				}},
			}},
		},
	}
}

func pushWith(policies ...model.AuthorizationPolicy) *model.PushContext {
	byNS := map[string][]model.AuthorizationPolicy{}
	for _, p := range policies {
		byNS[p.Namespace] = append(byNS[p.Namespace], p)
	}
	return &model.PushContext{
		Mesh: &meshconfig.MeshConfig{RootNamespace: "istio-system"},
		AuthzPolicies: &model.AuthorizationPolicies{
			NamespaceToPolicies: byNS,
			RootNamespace:       "istio-system",
		},
	}
}

// classicGatewayProxy is a classic ingress gateway: a Router with no gateway.networking.k8s.io/gateway-name
// label, so it is NOT a Gateway API workload and is matched only via WorkloadPolicyMatcher.Gateways.
func classicGatewayProxy(ns string) *model.Proxy {
	return &model.Proxy{
		Type:            model.Router,
		ConfigNamespace: ns,
		Labels:          map[string]string{"app": "istio-ingressgateway"},
	}
}

// TestNewBuilderForGateways_Attached verifies that a policy targeting a classic Gateway attaches (produces
// a non-nil HTTP RBAC filter) only when the matching Gateway is passed to NewBuilderForGateways.
func TestNewBuilderForGateways_Attached(t *testing.T) {
	push := pushWith(classicGatewayTargetRefPolicy("policy1", testNS, "gw-a"))
	proxy := classicGatewayProxy(testNS)

	b := NewBuilderForGateways(Local, push, proxy, false, []types.NamespacedName{{Name: "gw-a", Namespace: testNS}})
	got := b.BuildHTTP(networking.ListenerClassGateway)
	if len(got) == 0 {
		t.Fatalf("expected policy targeting gw-a to attach (non-nil HTTP filter), got %d filters", len(got))
	}
}

// TestNewBuilderForGateways_NotAttached verifies the policy does NOT attach when a different gateway (or no
// gateway) is supplied — the scoping must be exact.
func TestNewBuilderForGateways_NotAttached(t *testing.T) {
	cases := []struct {
		name     string
		gateways []types.NamespacedName
	}{
		{name: "wrong gateway name", gateways: []types.NamespacedName{{Name: "gw-b", Namespace: testNS}}},
		{name: "nil gateways", gateways: nil},
		{name: "wrong namespace", gateways: []types.NamespacedName{{Name: "gw-a", Namespace: "other-ns"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			push := pushWith(classicGatewayTargetRefPolicy("policy1", testNS, "gw-a"))
			proxy := classicGatewayProxy(testNS)

			b := NewBuilderForGateways(Local, push, proxy, false, tc.gateways)
			got := b.BuildHTTP(networking.ListenerClassGateway)
			if len(got) != 0 {
				t.Fatalf("expected policy NOT to attach for %q, got %d filters", tc.name, len(got))
			}
		})
	}
}

// TestNewBuilderForGateways_CrossNamespace verifies that a policy targeting a classic Gateway attaches even
// when the policy/gateway live in a namespace different from the proxy's config namespace (the shared-ingress
// topology).
func TestNewBuilderForGateways_CrossNamespace(t *testing.T) {
	push := pushWith(classicGatewayTargetRefPolicy("policy", "tenant-a", "gateway"))
	proxy := classicGatewayProxy("not-default")

	b := NewBuilderForGateways(Local, push, proxy, false, []types.NamespacedName{{Name: "gateway", Namespace: "tenant-a"}})
	got := b.BuildHTTP(networking.ListenerClassGateway)
	if len(got) == 0 {
		t.Fatalf("expected cross-namespace policy targeting tenant-a/gateway to attach, got %d filters", len(got))
	}
}

// TestNewBuilderForGateways_CrossNamespaceNoLeak is the anti-leak guard: a selector policy in the gateway's
// namespace matching the shared proxy's labels must NOT attach, even when that gateway is passed in.
func TestNewBuilderForGateways_CrossNamespaceNoLeak(t *testing.T) {
	push := pushWith(selectorPolicy("selleak", "tenant-a", map[string]string{"app": "istio-ingressgateway"}))
	proxy := classicGatewayProxy("not-default")

	b := NewBuilderForGateways(Local, push, proxy, false, []types.NamespacedName{{Name: "gateway", Namespace: "tenant-a"}})
	got := b.BuildHTTP(networking.ListenerClassGateway)
	if len(got) != 0 {
		t.Fatalf("selector policy in gateway namespace must not attach to the shared proxy, got %d filters", len(got))
	}
}

// TestNewBuilderForService_SelectorRegression is a regression guard: NewBuilderForService with a nil service
// still attaches a selector-based policy to a matching sidecar workload, exactly as before this change.
func TestNewBuilderForService_SelectorRegression(t *testing.T) {
	push := pushWith(selectorPolicy("policy1", testNS, map[string]string{"app": "my-app"}))
	proxy := &model.Proxy{
		Type:            model.SidecarProxy,
		ConfigNamespace: testNS,
		Labels:          map[string]string{"app": "my-app"},
	}

	b := NewBuilderForService(Local, push, proxy, false, nil)
	got := b.BuildHTTP(networking.ListenerClassSidecarInbound)
	if len(got) == 0 {
		t.Fatalf("selector-based policy should attach to matching workload via NewBuilderForService(nil svc), got %d filters", len(got))
	}
}
