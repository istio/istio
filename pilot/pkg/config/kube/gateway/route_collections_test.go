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

package gateway

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
)

// uriRoute builds an HTTPRoute with a single URI match of the given type.
func uriRoute(name string, uri *istio.StringMatch) *istio.HTTPRoute {
	return &istio.HTTPRoute{
		Name:  name,
		Match: []*istio.HTTPMatchRequest{{Uri: uri}},
	}
}

func exactURI(v string) *istio.StringMatch {
	return &istio.StringMatch{MatchType: &istio.StringMatch_Exact{Exact: v}}
}

func prefixURI(v string) *istio.StringMatch {
	return &istio.StringMatch{MatchType: &istio.StringMatch_Prefix{Prefix: v}}
}

func regexURI(v string) *istio.StringMatch {
	return &istio.StringMatch{MatchType: &istio.StringMatch_Regex{Regex: v}}
}

func TestHTTPRouteOrigins(t *testing.T) {
	origins := func(nn ...types.NamespacedName) []types.NamespacedName { return nn }
	vs := func(n int) *istio.VirtualService {
		routes := make([]*istio.HTTPRoute, n)
		for i := range routes {
			routes[i] = &istio.HTTPRoute{}
		}
		return &istio.VirtualService{Http: routes}
	}

	cases := []struct {
		name    string
		cfg     config.Config
		want    []types.NamespacedName
		wantErr bool
	}{
		{
			// GRPCRoute / native VirtualService path: no origins recorded in Extra.
			// Fall back to a zero-value origin per route so downstream stays in lockstep.
			name: "extra absent returns zero-value origins",
			cfg: config.Config{
				Spec: vs(2),
			},
			want: origins(types.NamespacedName{}, types.NamespacedName{}),
		},
		{
			name: "matching length returns origins",
			cfg: config.Config{
				Spec: vs(2),
				Extra: map[string]any{
					constants.ConfigExtraHTTPRouteOrigins: origins(
						types.NamespacedName{Name: "a", Namespace: "ns"},
						types.NamespacedName{Name: "b", Namespace: "ns"},
					),
				},
			},
			want: origins(
				types.NamespacedName{Name: "a", Namespace: "ns"},
				types.NamespacedName{Name: "b", Namespace: "ns"},
			),
		},
		{
			name: "wrong type errors",
			cfg: config.Config{
				Spec: vs(1),
				Extra: map[string]any{
					constants.ConfigExtraHTTPRouteOrigins: []string{"not-a-namespaced-name"},
				},
			},
			wantErr: true,
		},
		{
			name: "length mismatch errors",
			cfg: config.Config{
				Spec: vs(2),
				Extra: map[string]any{
					constants.ConfigExtraHTTPRouteOrigins: origins(
						types.NamespacedName{Name: "a", Namespace: "ns"},
					),
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := httpRouteOrigins(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (origins=%v)", got)
				}
				if got != nil {
					t.Fatalf("expected nil origins on error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("length mismatch: got %d want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("origin[%d] = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestHTTPRouteOriginsClone verifies the returned slice is a copy: mutating it
// must not corrupt the origins stored in the source config's Extra field.
func TestHTTPRouteOriginsClone(t *testing.T) {
	stored := []types.NamespacedName{
		{Name: "a", Namespace: "ns"},
		{Name: "b", Namespace: "ns"},
	}
	cfg := config.Config{
		Spec: &istio.VirtualService{Http: []*istio.HTTPRoute{{}, {}}},
		Extra: map[string]any{
			constants.ConfigExtraHTTPRouteOrigins: stored,
		},
	}

	got, err := httpRouteOrigins(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got[0] = types.NamespacedName{Name: "mutated", Namespace: "ns"}

	if stored[0].Name != "a" {
		t.Fatalf("mutating returned origins corrupted source Extra: got %q, want %q", stored[0].Name, "a")
	}
}

func TestSortHTTPRoutesWithOrigins(t *testing.T) {
	t.Run("lockstep reorder preserves route-origin pairing", func(t *testing.T) {
		// Fed out of sorted order. Rank: Exact(3) > Prefix(2) > Regex(1).
		routes := []*istio.HTTPRoute{
			uriRoute("a", prefixURI("/a")),
			uriRoute("b", exactURI("/b")),
			uriRoute("c", regexURI("/c")),
		}
		origins := []types.NamespacedName{
			{Name: "a"},
			{Name: "b"},
			{Name: "c"},
		}

		sortHTTPRoutesWithOrigins(routes, origins)

		// Expected order by descending rank: b (exact), a (prefix), c (regex).
		wantRoutes := []string{"b", "a", "c"}
		for i, name := range wantRoutes {
			if routes[i].Name != name {
				t.Fatalf("route[%d].Name = %q, want %q", i, routes[i].Name, name)
			}
			// Each route's origin must have moved with it: by construction the
			// origin name equals the route name.
			if origins[i].Name != name {
				t.Fatalf("origin[%d] = %q, want %q (origin did not track its route)", i, origins[i].Name, name)
			}
		}
		if len(origins) != len(routes) {
			t.Fatalf("len(origins)=%d != len(routes)=%d", len(origins), len(routes))
		}
	})

	t.Run("stable order for equal-rank routes keeps origins paired", func(t *testing.T) {
		// Two routes with identical rank and length: stable sort must keep input order.
		routes := []*istio.HTTPRoute{
			uriRoute("first", prefixURI("/same")),
			uriRoute("second", prefixURI("/same")),
		}
		origins := []types.NamespacedName{
			{Name: "first"},
			{Name: "second"},
		}

		sortHTTPRoutesWithOrigins(routes, origins)

		want := []string{"first", "second"}
		for i, name := range want {
			if routes[i].Name != name {
				t.Fatalf("route[%d].Name = %q, want %q", i, routes[i].Name, name)
			}
			if origins[i].Name != name {
				t.Fatalf("origin[%d] = %q, want %q", i, origins[i].Name, name)
			}
		}
	})
}
