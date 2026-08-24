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
	"sort"
	"strings"
	"testing"

	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	authpb "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

// testHTTPRouteName is the HTTPRoute every policy in this file targets; cases vary the origin
// looked up against it rather than the target itself.
const testHTTPRouteName = "route-a"

func httpRoutePolicy(t *testing.T, name, ns string, action authpb.AuthorizationPolicy_Action) config.Config {
	t.Helper()
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.AuthorizationPolicy,
			Name:             name,
			Namespace:        ns,
		},
		Spec: &authpb.AuthorizationPolicy{
			Action: action,
			TargetRef: &selectorpb.PolicyTargetReference{
				Group: gvk.HTTPRoute.Group,
				Kind:  gvk.HTTPRoute.Kind,
				Name:  testHTTPRouteName,
			},
			Rules: []*authpb.Rule{
				{
					From: []*authpb.Rule_From{
						{Source: &authpb.Source{Principals: []string{"cluster.local/ns/foo/sa/bar"}}},
					},
				},
			},
		},
	}
}

func newTestPerRouteBuilder(t *testing.T, configs ...config.Config) *PerRouteBuilder {
	t.Helper()
	store := memory.Make(collections.Pilot, false, test.NewStop(t))
	for _, c := range configs {
		if _, err := store.Create(c); err != nil {
			t.Fatalf("failed to create config: %v", err)
		}
	}
	push := &model.PushContext{
		Mesh:          &meshconfig.MeshConfig{TrustDomain: "cluster.local"},
		AuthzPolicies: model.GetAuthorizationPolicies(&model.Environment{ConfigStore: store}),
	}
	return NewPerRouteBuilder(push, &model.Proxy{Type: model.Router})
}

func TestPerRouteBuilderBuild(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)
	cases := []struct {
		name      string
		configs   []config.Config
		origin    types.NamespacedName
		wantNames []string
	}{
		{
			name:      "allow policy targeting the route",
			configs:   []config.Config{httpRoutePolicy(t, "allow", "foo", authpb.AuthorizationPolicy_ALLOW)},
			origin:    types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"},
			wantNames: []string{builder.RBACFilterNameAllow},
		},
		{
			name: "allow and deny produce independently overridable slots",
			configs: []config.Config{
				httpRoutePolicy(t, "allow", "foo", authpb.AuthorizationPolicy_ALLOW),
				httpRoutePolicy(t, "deny", "foo", authpb.AuthorizationPolicy_DENY),
			},
			origin:    types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"},
			wantNames: []string{builder.RBACFilterNameAllow, builder.RBACRouteAnchorNameDeny},
		},
		{
			name:    "policy targeting a different route is not applied",
			configs: []config.Config{httpRoutePolicy(t, "allow", "foo", authpb.AuthorizationPolicy_ALLOW)},
			origin:  types.NamespacedName{Name: "route-b", Namespace: "foo"},
		},
		{
			name:    "policy in a different namespace is not applied",
			configs: []config.Config{httpRoutePolicy(t, "allow", "bar", authpb.AuthorizationPolicy_ALLOW)},
			origin:  types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"},
		},
		{
			// Routes not generated from an HTTPRoute carry a zero origin and must never be overridden.
			name:    "zero origin yields no override",
			configs: []config.Config{httpRoutePolicy(t, "allow", "foo", authpb.AuthorizationPolicy_ALLOW)},
			origin:  types.NamespacedName{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPerRouteBuilder(t, tc.configs...)

			got := p.Build(tc.origin)
			if len(got) != len(tc.wantNames) {
				t.Fatalf("got %d per-route configs %v, want %d (%v)", len(got), keys(got), len(tc.wantNames), tc.wantNames)
			}
			for _, name := range tc.wantNames {
				raw, ok := got[name]
				if !ok {
					t.Fatalf("missing per-route config for filter %q, got %v", name, keys(got))
				}
				perRoute := &rbachttp.RBACPerRoute{}
				if err := raw.UnmarshalTo(perRoute); err != nil {
					t.Fatalf("filter %q: not an RBACPerRoute: %v", name, err)
				}
				// An RBACPerRoute with no Rbac disables RBAC for the route, which would fail open.
				if perRoute.GetRbac() == nil {
					t.Fatalf("filter %q: RBACPerRoute has no rbac config", name)
				}
				if len(perRoute.GetRbac().GetRules().GetPolicies()) == 0 {
					t.Errorf("filter %q: expected generated policies", name)
				}
			}
		})
	}
}

// TestPerRouteBuilderNilSafe covers the route build path being reached with no authz policies set.
func TestPerRouteBuilderNilSafe(t *testing.T) {
	var p *PerRouteBuilder
	if got := p.Build(types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"}); got != nil {
		t.Fatalf("expected nil from nil builder, got %v", got)
	}

	push := &model.PushContext{Mesh: &meshconfig.MeshConfig{TrustDomain: "cluster.local"}}
	p = NewPerRouteBuilder(push, &model.Proxy{Type: model.Router})
	if got := p.Build(types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"}); got != nil {
		t.Fatalf("expected nil when no authorization policies exist, got %v", got)
	}
}

func keys(m map[string]*anypb.Any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func workloadPolicy(name, ns string, action authpb.AuthorizationPolicy_Action) model.AuthorizationPolicy {
	return model.AuthorizationPolicy{
		Name:      name,
		Namespace: ns,
		Spec: &authpb.AuthorizationPolicy{
			Action: action,
			Rules: []*authpb.Rule{
				{From: []*authpb.Rule_From{{Source: &authpb.Source{Namespaces: []string{"prod"}}}}},
			},
		},
	}
}

func anchorRBAC(t *testing.T, f *hcm.HttpFilter) *rbachttp.RBAC {
	t.Helper()
	got := &rbachttp.RBAC{}
	if err := f.GetTypedConfig().UnmarshalTo(got); err != nil {
		t.Fatalf("filter %q does not hold an RBAC config: %v", f.Name, err)
	}
	return got
}

// I9/I3: anchors exist only for gateways with the flag on, and enforce nothing until a route
// overrides them.
func TestRouteAnchorFilters(t *testing.T) {
	router := &model.Proxy{Type: model.Router}
	allowFilter := []*hcm.HttpFilter{{Name: builder.RBACFilterNameAllow}}
	cases := []struct {
		name    string
		enabled bool
		proxy   *model.Proxy
		class   networking.ListenerClass
		built   []*hcm.HttpFilter
		want    []string
	}{
		{
			name:  "flag off emits nothing",
			proxy: router,
			class: networking.ListenerClassGateway,
			want:  nil,
		},
		{
			name:    "gateway with no allow policy gets both anchors",
			enabled: true,
			proxy:   router,
			class:   networking.ListenerClassGateway,
			want:    []string{builder.RBACRouteAnchorNameDeny, builder.RBACFilterNameAllow},
		},
		{
			// The workload's own ALLOW policy already produced the filter that route ALLOW
			// policies merge into, so a second instance would split them across two filters
			// and make them intersect rather than union.
			name:    "gateway with an allow policy gets only the deny anchor",
			enabled: true,
			proxy:   router,
			class:   networking.ListenerClassGateway,
			built:   allowFilter,
			want:    []string{builder.RBACRouteAnchorNameDeny},
		},
		{
			name:    "sidecar gets nothing",
			enabled: true,
			proxy:   &model.Proxy{Type: model.SidecarProxy},
			class:   networking.ListenerClassSidecarInbound,
			want:    nil,
		},
		{
			name:    "waypoint gets nothing yet",
			enabled: true,
			proxy:   &model.Proxy{Type: model.Waypoint},
			class:   networking.ListenerClassSidecarInbound,
			want:    nil,
		},
		{
			name:    "outbound gets nothing",
			enabled: true,
			proxy:   router,
			class:   networking.ListenerClassSidecarOutbound,
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, tc.enabled)
			filters := RouteAnchorFilters(tc.proxy, tc.class, tc.built)
			got := make([]string, 0, len(filters))
			for _, f := range filters {
				got = append(got, f.Name)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got anchors %v, want %v", got, tc.want)
			}
			for _, f := range filters {
				rbac := anchorRBAC(t, f)
				if rbac.GetRules() != nil {
					t.Errorf("anchor %q carries rules %v; anchors must not enforce anything", f.Name, rbac.GetRules())
				}
				if rbac.GetShadowRules() != nil {
					t.Errorf("anchor %q carries shadow rules; anchors must be inert", f.Name)
				}
			}
		})
	}
}

// I1: no key a route can emit may name a filter instance that carries workload or
// root-namespace policy. This is the invariant that makes the design safe, so it is asserted
// on the two sets directly rather than on any single name.
func TestRouteOverridesDisjointFromWorkloadFilters(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)

	policies := model.AuthorizationPoliciesResult{
		Audit: []model.AuthorizationPolicy{workloadPolicy("audit", "istio-system", authpb.AuthorizationPolicy_AUDIT)},
		Deny:  []model.AuthorizationPolicy{workloadPolicy("root-deny", "istio-system", authpb.AuthorizationPolicy_DENY)},
		Allow: []model.AuthorizationPolicy{workloadPolicy("root-allow", "istio-system", authpb.AuthorizationPolicy_ALLOW)},
	}
	wb := builder.New(trustdomain.NewBundle("cluster.local", nil), nil, policies, builder.Option{})
	if wb == nil {
		t.Fatal("expected a workload builder")
	}
	workloadFilters := wb.BuildHTTP()
	if len(workloadFilters) == 0 {
		t.Fatal("expected workload filters; the disjointness check would be vacuous")
	}
	workloadNames := sets.New[string]()
	for _, f := range workloadFilters {
		workloadNames.Insert(f.Name)
	}

	overrides := newTestPerRouteBuilder(t,
		httpRoutePolicy(t, "route-allow", "foo", authpb.AuthorizationPolicy_ALLOW),
		httpRoutePolicy(t, "route-deny", "foo", authpb.AuthorizationPolicy_DENY),
	).Build(types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"})
	if len(overrides) != 2 {
		t.Fatalf("got %d per-route overrides, want 2; the disjointness check would be vacuous", len(overrides))
	}

	for name := range overrides {
		if workloadNames.Contains(name) {
			t.Errorf("per-route override key %q also names a workload filter: a route could replace "+
				"workload or root-namespace policy", name)
		}
	}

	// The workload filters must also keep the well-known name, which istioctl and the
	// ext_authz metadata namespacing depend on.
	for _, f := range workloadFilters {
		if f.Name != wellknown.HTTPRoleBasedAccessControl {
			t.Errorf("workload filter is named %q, want %q", f.Name, wellknown.HTTPRoleBasedAccessControl)
		}
	}
}

// I2: a dry-run route policy must not disable enforcement. Under the anchor design it overrides
// an inert filter, so shadow rules are preserved and nothing is switched off.
func TestPerRouteBuilderDryRunDoesNotDisableEnforcement(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)

	policy := httpRoutePolicy(t, "dry-allow", "foo", authpb.AuthorizationPolicy_ALLOW)
	policy.Annotations = map[string]string{annotation.IoIstioDryRun.Name: "true"}

	overrides := newTestPerRouteBuilder(t, policy).
		Build(types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"})
	if len(overrides) != 1 {
		t.Fatalf("got %d overrides, want 1", len(overrides))
	}

	for name, any := range overrides {
		if name == wellknown.HTTPRoleBasedAccessControl {
			t.Fatalf("dry-run policy overrides the workload filter %q, which would disable it", name)
		}
		perRoute := &rbachttp.RBACPerRoute{}
		if err := any.UnmarshalTo(perRoute); err != nil {
			t.Fatalf("unmarshal RBACPerRoute: %v", err)
		}
		if perRoute.GetRbac().GetRules() != nil {
			t.Errorf("dry-run override carries enforced rules")
		}
		if perRoute.GetRbac().GetShadowRules() == nil {
			t.Errorf("dry-run override lost its shadow rules; the annotation would do nothing")
		}
	}
}

func perRouteRBAC(t *testing.T, got map[string]*anypb.Any, name string) *rbacpb.RBAC {
	t.Helper()
	raw, ok := got[name]
	if !ok {
		t.Fatalf("missing per-route config for filter %q, got %v", name, keys(got))
	}
	perRoute := &rbachttp.RBACPerRoute{}
	if err := raw.UnmarshalTo(perRoute); err != nil {
		t.Fatalf("filter %q: not an RBACPerRoute: %v", name, err)
	}
	if perRoute.GetRbac() == nil {
		t.Fatalf("filter %q: RBACPerRoute has no rbac config, which disables RBAC for the route", name)
	}
	return perRoute.GetRbac().GetRules()
}

func policyNames(rbac *rbacpb.RBAC) []string {
	out := make([]string, 0, len(rbac.GetPolicies()))
	for k := range rbac.GetPolicies() {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A route ALLOW must widen the workload's ALLOW rather than intersect with it, matching how
// root-namespace and workload-namespace ALLOW policies already combine. Both have to land in a
// single filter's policy map to union, because separate ALLOW filters intersect.
//
// DENY is the opposite: it stays route-only, because it rides a separate anchor filter that
// chains after the workload DENY filter, and chained DENY filters already union.
func TestRouteAllowUnionsWithWorkloadAllowButDenyStaysRouteOnly(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)

	p := newTestPerRouteBuilder(t,
		httpRoutePolicy(t, "route-allow", "foo", authpb.AuthorizationPolicy_ALLOW),
		httpRoutePolicy(t, "route-deny", "foo", authpb.AuthorizationPolicy_DENY),
	)
	p.workloadAllow = []model.AuthorizationPolicy{workloadPolicy("gateway-allow", "foo", authpb.AuthorizationPolicy_ALLOW)}

	got := p.Build(types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"})

	allow := policyNames(perRouteRBAC(t, got, builder.RBACFilterNameAllow))
	if len(allow) != 2 {
		t.Fatalf("allow override has %d policies %v, want the workload and route policies unioned", len(allow), allow)
	}
	for _, want := range []string{"gateway-allow", "route-allow"} {
		if !slices.ContainsFunc(allow, func(s string) bool { return strings.Contains(s, want) }) {
			t.Errorf("allow override %v is missing %q", allow, want)
		}
	}

	deny := policyNames(perRouteRBAC(t, got, builder.RBACRouteAnchorNameDeny))
	if len(deny) != 1 {
		t.Fatalf("deny override has %d policies %v, want only the route policy", len(deny), deny)
	}
	if !strings.Contains(deny[0], "route-deny") {
		t.Errorf("deny override %v is not the route policy", deny)
	}
}

// With no workload ALLOW policy the route ALLOW stands alone, which makes the targeted route
// deny-by-default while every other route on the gateway stays open.
func TestRouteAllowWithoutWorkloadAllow(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)

	p := newTestPerRouteBuilder(t, httpRoutePolicy(t, "route-allow", "foo", authpb.AuthorizationPolicy_ALLOW))

	got := p.Build(types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"})
	allow := policyNames(perRouteRBAC(t, got, builder.RBACFilterNameAllow))
	if len(allow) != 1 || !strings.Contains(allow[0], "route-allow") {
		t.Fatalf("allow override %v, want only the route policy", allow)
	}
}

// A route with only a DENY policy must not touch the ALLOW filter, or the workload's ALLOW
// policies would stop applying to that route.
func TestRouteDenyOnlyLeavesAllowFilterAlone(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)

	p := newTestPerRouteBuilder(t, httpRoutePolicy(t, "route-deny", "foo", authpb.AuthorizationPolicy_DENY))
	p.workloadAllow = []model.AuthorizationPolicy{workloadPolicy("gateway-allow", "foo", authpb.AuthorizationPolicy_ALLOW)}

	got := p.Build(types.NamespacedName{Name: testHTTPRouteName, Namespace: "foo"})
	if _, ok := got[builder.RBACFilterNameAllow]; ok {
		t.Fatalf("deny-only route overrode the allow filter: %v", keys(got))
	}
	if _, ok := got[builder.RBACRouteAnchorNameDeny]; !ok {
		t.Fatalf("deny-only route produced no deny override: %v", keys(got))
	}
}
