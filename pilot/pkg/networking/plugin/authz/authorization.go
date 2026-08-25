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
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbachttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/slices"
)

type ActionType int

const (
	// Local for action ALLOW, DENY and AUDIT and is enforced by Envoy RBAC filter.
	Local ActionType = iota
	// Custom action is enforced by Envoy ext_authz filter.
	Custom
)

type Builder struct {
	// Lazy load
	httpBuilt, tcpBuilt bool

	httpFilters []*hcm.HttpFilter
	tcpFilters  []*listener.Filter
	builder     *builder.Builder
}

func NewBuilder(actionType ActionType, push *model.PushContext, proxy *model.Proxy, useFilterState bool) *Builder {
	return NewBuilderForService(actionType, push, proxy, useFilterState, nil)
}

func NewBuilderForService(actionType ActionType, push *model.PushContext, proxy *model.Proxy, useFilterState bool, svc *model.Service) *Builder {
	return newBuilder(actionType, push, proxy, useFilterState, svc, false)
}

// NewWaypointTerminationBuilder creates a builder for use on the waypoints HBONE termination layer
func NewWaypointTerminationBuilder(actionType ActionType, push *model.PushContext, proxy *model.Proxy) *Builder {
	return newBuilder(actionType, push, proxy, false, nil, true)
}

func newBuilder(
	actionType ActionType,
	push *model.PushContext,
	proxy *model.Proxy,
	useFilterState bool,
	svc *model.Service,
	alwaysTreatAsNonWaypoint bool,
) *Builder {
	tdBundle := trustdomain.NewBundle(push.Mesh.TrustDomain, push.Mesh.TrustDomainAliases)
	option := builder.Option{
		IsCustomBuilder:  actionType == Custom,
		UseFilterState:   useFilterState,
		NamedAllowFilter: features.EnableGatewayAPIHTTPRouteAuth && proxy.Type == model.Router,
	}
	selectionOpts := model.PolicyMatcherForProxy(proxy).WithService(svc).WithRootNamespace(push.AuthzPolicies.RootNamespace)
	if alwaysTreatAsNonWaypoint {
		// The intention here is to apply authz rules to the waypoint, but using the standard workload selector policy semantics,
		// rather than the per-service rules.
		// This gives us two layers of authorization policy applied.
		selectionOpts.IsWaypoint = false
	}
	policies := push.AuthzPolicies.ListAuthorizationPolicies(selectionOpts)
	b := builder.New(tdBundle, push, policies, option)
	return &Builder{builder: b}
}

func (b *Builder) BuildTCPRulesAsHTTPFilter() []*hcm.HttpFilter {
	if b == nil || b.builder == nil {
		return nil
	}

	return b.builder.BuildTCPRulesAsHTTPFilter()
}

func (b *Builder) BuildTCP() []*listener.Filter {
	if b == nil || b.builder == nil {
		return nil
	}
	if b.tcpBuilt {
		return b.tcpFilters
	}
	b.tcpBuilt = true
	b.tcpFilters = b.builder.BuildTCP()

	return b.tcpFilters
}

func (b *Builder) BuildHTTP(class networking.ListenerClass) []*hcm.HttpFilter {
	if b == nil || b.builder == nil {
		return nil
	}
	if class == networking.ListenerClassSidecarOutbound {
		// Only applies to inbound and gateways
		return nil
	}
	if b.httpBuilt {
		return b.httpFilters
	}
	b.httpBuilt = true
	b.httpFilters = b.builder.BuildHTTP()

	return b.httpFilters
}

// PartitionRouteScopedFilters splits the workload's RBAC filters into those that are evaluated
// independently of the selected route and those that resolve per-route configuration.
func PartitionRouteScopedFilters(built []*hcm.HttpFilter) (workload, routeScoped []*hcm.HttpFilter) {
	for _, f := range built {
		if f.GetName() == builder.RBACFilterNameAllow {
			routeScoped = append(routeScoped, f)
			continue
		}
		workload = append(workload, f)
	}
	return workload, routeScoped
}

// RouteAnchorFilters returns the RBAC filters a route override needs to attach to, beyond those
// the workload's own policies already produced in built. They carry no rules and enforce nothing
// until a route overrides one via typed_per_filter_config.
//
// They do not depend on which policies exist, so listener generation never has to resolve which
// routes this listener serves.
func RouteAnchorFilters(proxy *model.Proxy, class networking.ListenerClass, built []*hcm.HttpFilter) []*hcm.HttpFilter {
	if !features.EnableGatewayAPIHTTPRouteAuth {
		return nil
	}
	if proxy == nil || proxy.Type != model.Router {
		return nil
	}
	if class == networking.ListenerClassSidecarOutbound {
		return nil
	}
	out := []*hcm.HttpFilter{routeAnchorFilter(builder.RBACRouteAnchorNameDeny)}
	// Route ALLOW merges into the workload's ALLOW filter rather than chaining after it, so that
	// filter must exist even when the workload has no ALLOW policy to produce one.
	if !slices.ContainsFunc(built, func(f *hcm.HttpFilter) bool { return f.GetName() == builder.RBACFilterNameAllow }) {
		out = append(out, routeAnchorFilter(builder.RBACFilterNameAllow))
	}
	return out
}

func routeAnchorFilter(name string) *hcm.HttpFilter {
	return &hcm.HttpFilter{
		Name:       name,
		ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(&rbachttp.RBAC{})},
	}
}

// PerRouteBuilder builds authorization config that is attached to an individual HTTP route rather
// than to a listener, for AuthorizationPolicy objects that select a route with an HTTPRoute targetRef.
type PerRouteBuilder struct {
	push           *model.PushContext
	tdBundle       trustdomain.Bundle
	useFilterState bool

	// ALLOW policies applying to this proxy workload-wide. Route ALLOW policies merge with these
	// into one filter so they union rather than intersect.
	workloadAllow []model.AuthorizationPolicy

	// Cached by origin: one HTTPRoute usually expands into several Envoy routes, and merged
	// VirtualServices repeat origins across route configs.
	cache map[types.NamespacedName]map[string]*anypb.Any
}

// NewPerRouteBuilder returns a builder for per-route authorization config for the given proxy.
func NewPerRouteBuilder(push *model.PushContext, proxy *model.Proxy) *PerRouteBuilder {
	p := &PerRouteBuilder{
		push:           push,
		tdBundle:       trustdomain.NewBundle(push.Mesh.GetTrustDomain(), push.Mesh.GetTrustDomainAliases()),
		useFilterState: proxy.Type == model.Waypoint,
		cache:          map[types.NamespacedName]map[string]*anypb.Any{},
	}
	if push.AuthzPolicies != nil {
		selectionOpts := model.PolicyMatcherForProxy(proxy).WithRootNamespace(push.AuthzPolicies.RootNamespace)
		p.workloadAllow = push.AuthzPolicies.ListAuthorizationPolicies(selectionOpts).Allow
	}
	return p
}

// Build returns the authorization config for the HTTPRoute a route was generated from, keyed by
// RBAC filter name. Returns nil if no policy targets it, including routes with a zero origin,
// which did not come from an HTTPRoute.
func (p *PerRouteBuilder) Build(origin types.NamespacedName) map[string]*anypb.Any {
	if p == nil || origin.Name == "" || origin.Namespace == "" {
		return nil
	}
	if cached, ok := p.cache[origin]; ok {
		return cached
	}

	var out map[string]*anypb.Any
	route := p.push.AuthzPolicies.ListAuthorizationPoliciesForHTTPRoute(origin)
	// An action with no route policy is left out entirely, so its filter keeps enforcing whatever
	// the listener configured.
	policies := model.AuthorizationPoliciesResult{Deny: route.Deny}
	if len(route.Allow) > 0 {
		// The override replaces the ALLOW filter's config here, so it must carry the workload's
		// ALLOW policies too or they would stop applying to this route.
		policies.Allow = make([]model.AuthorizationPolicy, 0, len(p.workloadAllow)+len(route.Allow))
		policies.Allow = append(policies.Allow, p.workloadAllow...)
		policies.Allow = append(policies.Allow, route.Allow...)
	}
	if b := builder.New(p.tdBundle, p.push, policies, builder.Option{UseFilterState: p.useFilterState}); b != nil {
		for action, rbac := range b.BuildHTTPRBACForRoute() {
			name := builder.PerRouteFilterName(action)
			if name == "" {
				continue
			}
			if out == nil {
				out = make(map[string]*anypb.Any, 2)
			}
			out[name] = protoconv.MessageToAny(&rbachttp.RBACPerRoute{Rbac: rbac})
		}
	}

	p.cache[origin] = out
	return out
}
