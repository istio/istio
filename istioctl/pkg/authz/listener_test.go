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

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

func rbacHTTPFilter(t *testing.T, name string, rbac *rbachttp.RBAC) *hcm.HttpFilter {
	t.Helper()
	cfg, err := anypb.New(rbac)
	if err != nil {
		t.Fatalf("failed to marshal RBAC config: %v", err)
	}
	return &hcm.HttpFilter{
		Name:       name,
		ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: cfg},
	}
}

func rbacEnforcing(action rbacpb.RBAC_Action, policy string) *rbachttp.RBAC {
	return &rbachttp.RBAC{
		Rules: &rbacpb.RBAC{
			Action:   action,
			Policies: map[string]*rbacpb.Policy{policy: {}},
		},
	}
}

func listenerWithHTTPFilters(t *testing.T, filters ...*hcm.HttpFilter) *listener.Listener {
	t.Helper()
	cfg, err := anypb.New(&hcm.HttpConnectionManager{HttpFilters: filters})
	if err != nil {
		t.Fatalf("failed to marshal HTTP connection manager: %v", err)
	}
	return &listener.Listener{
		FilterChains: []*listener.FilterChain{
			{
				Filters: []*listener.Filter{
					{
						Name:       wellknown.HTTPConnectionManager,
						ConfigType: &listener.Filter_TypedConfig{TypedConfig: cfg},
					},
				},
			},
		},
	}
}

// RBAC filters are emitted under several instance names, so parse must select them by typed
// config rather than by filter name.
func TestParseCollectsRBACFiltersUnderAnyName(t *testing.T) {
	const (
		denyPolicy  = "ns[default]-policy[deny-nothing]-rule[0]"
		allowPolicy = "ns[default]-policy[require-jwt]-rule[0]"
		routePolicy = "ns[default]-policy[route-deny]-rule[0]"
	)

	l := listenerWithHTTPFilters(t,
		rbacHTTPFilter(t, wellknown.HTTPRoleBasedAccessControl, rbacEnforcing(rbacpb.RBAC_DENY, denyPolicy)),
		rbacHTTPFilter(t, "istio.authorization.allow", rbacEnforcing(rbacpb.RBAC_ALLOW, allowPolicy)),
		rbacHTTPFilter(t, "istio.authorization.route.deny", rbacEnforcing(rbacpb.RBAC_DENY, routePolicy)),
		// No typed config at all; must not be mistaken for an RBAC filter.
		&hcm.HttpFilter{Name: wellknown.Router},
	)

	parsed := parse([]*listener.Listener{l})
	if len(parsed) != 1 || len(parsed[0].filterChains) != 1 {
		t.Fatalf("expected 1 listener with 1 filter chain, got %v", parsed)
	}

	got := sets.New[string]()
	for _, r := range parsed[0].filterChains[0].rbacHTTP {
		for name := range r.GetRules().GetPolicies() {
			got.Insert(name)
		}
	}

	want := sets.New(denyPolicy, allowPolicy, routePolicy)
	if !got.Equals(want) {
		t.Errorf("parsed policies did not match\n got: %v\nwant: %v", sets.SortedList(got), sets.SortedList(want))
	}
}

// Filters carrying neither rules nor shadow rules enforce nothing and would otherwise be
// reported as an anonymous ALLOW.
func TestParseSkipsRBACFiltersWithoutRules(t *testing.T) {
	const denyPolicy = "ns[default]-policy[deny-nothing]-rule[0]"

	l := listenerWithHTTPFilters(t,
		rbacHTTPFilter(t, wellknown.HTTPRoleBasedAccessControl, rbacEnforcing(rbacpb.RBAC_DENY, denyPolicy)),
		rbacHTTPFilter(t, "istio.authorization.route.deny", &rbachttp.RBAC{}),
		rbacHTTPFilter(t, "istio.authorization.allow", &rbachttp.RBAC{}),
	)

	parsed := parse([]*listener.Listener{l})
	if len(parsed) != 1 || len(parsed[0].filterChains) != 1 {
		t.Fatalf("expected 1 listener with 1 filter chain, got %v", parsed)
	}

	rbacFilters := parsed[0].filterChains[0].rbacHTTP
	if len(rbacFilters) != 1 {
		t.Fatalf("expected only the enforcing filter to be collected, got %d", len(rbacFilters))
	}
	if _, ok := rbacFilters[0].GetRules().GetPolicies()[denyPolicy]; !ok {
		t.Errorf("expected the enforcing DENY filter, got %v", rbacFilters[0])
	}
}
