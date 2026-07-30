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
	"testing"

	rbachttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	authpb "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
)

func httpRoutePolicy(t *testing.T, name, ns, routeName string, action authpb.AuthorizationPolicy_Action) config.Config {
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
				Name:  routeName,
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
	store := memory.Make(collections.Pilot)
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
	cases := []struct {
		name      string
		configs   []config.Config
		origin    types.NamespacedName
		wantNames []string
	}{
		{
			name:      "allow policy targeting the route",
			configs:   []config.Config{httpRoutePolicy(t, "allow", "foo", "route-a", authpb.AuthorizationPolicy_ALLOW)},
			origin:    types.NamespacedName{Name: "route-a", Namespace: "foo"},
			wantNames: []string{builder.RBACFilterNameAllow},
		},
		{
			name: "allow and deny produce independently overridable slots",
			configs: []config.Config{
				httpRoutePolicy(t, "allow", "foo", "route-a", authpb.AuthorizationPolicy_ALLOW),
				httpRoutePolicy(t, "deny", "foo", "route-a", authpb.AuthorizationPolicy_DENY),
			},
			origin:    types.NamespacedName{Name: "route-a", Namespace: "foo"},
			wantNames: []string{builder.RBACFilterNameAllow, builder.RBACFilterNameDeny},
		},
		{
			name:    "policy targeting a different route is not applied",
			configs: []config.Config{httpRoutePolicy(t, "allow", "foo", "route-a", authpb.AuthorizationPolicy_ALLOW)},
			origin:  types.NamespacedName{Name: "route-b", Namespace: "foo"},
		},
		{
			name:    "policy in a different namespace is not applied",
			configs: []config.Config{httpRoutePolicy(t, "allow", "bar", "route-a", authpb.AuthorizationPolicy_ALLOW)},
			origin:  types.NamespacedName{Name: "route-a", Namespace: "foo"},
		},
		{
			// Routes not generated from an HTTPRoute carry a zero origin and must never be overridden.
			name:    "zero origin yields no override",
			configs: []config.Config{httpRoutePolicy(t, "allow", "foo", "route-a", authpb.AuthorizationPolicy_ALLOW)},
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
	if got := p.Build(types.NamespacedName{Name: "route-a", Namespace: "foo"}); got != nil {
		t.Fatalf("expected nil from nil builder, got %v", got)
	}

	push := &model.PushContext{Mesh: &meshconfig.MeshConfig{TrustDomain: "cluster.local"}}
	p = NewPerRouteBuilder(push, &model.Proxy{Type: model.Router})
	if got := p.Build(types.NamespacedName{Name: "route-a", Namespace: "foo"}); got != nil {
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
