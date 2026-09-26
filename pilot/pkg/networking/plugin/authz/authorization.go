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
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

type ActionType int

const (
	// Local for action ALLOW, DENY and AUDIT and is enforced by Envoy RBAC filter.
	Local ActionType = iota
	// Custom action is enforced by Envoy ext_authz filter.
	Custom
)

// Builder builds the authorization filters applicable to a proxy. Filters are compiled and
// cached per listenerSetScope (the zero value meaning "not a ListenerSet listener"), since a
// ListenerSet-targeted policy only applies to that ListenerSet's own filter chains.
type Builder struct {
	push     *model.PushContext
	tdBundle trustdomain.Bundle
	option   builder.Option
	policies model.AuthorizationPoliciesResult

	httpFilters map[types.NamespacedName][]*hcm.HttpFilter
	tcpFilters  map[types.NamespacedName][]*listener.Filter
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
		IsCustomBuilder: actionType == Custom,
		UseFilterState:  useFilterState,
	}
	selectionOpts := model.PolicyMatcherForProxy(proxy).WithService(svc).WithRootNamespace(push.AuthzPolicies.RootNamespace)
	if alwaysTreatAsNonWaypoint {
		// The intention here is to apply authz rules to the waypoint, but using the standard workload selector policy semantics,
		// rather than the per-service rules.
		// This gives us two layers of authorization policy applied.
		selectionOpts.IsWaypoint = false
	}
	policies := push.AuthzPolicies.ListAuthorizationPolicies(selectionOpts)
	return &Builder{push: push, tdBundle: tdBundle, option: option, policies: policies}
}

// scopedPolicies drops any policy whose targetRefs include a Kind: ListenerSet reference that
// does not match scope.
func scopedPolicies(policies model.AuthorizationPoliciesResult, scope types.NamespacedName) model.AuthorizationPoliciesResult {
	filter := func(in []model.AuthorizationPolicy) []model.AuthorizationPolicy {
		var out []model.AuthorizationPolicy
		for _, p := range in {
			if appliesToScope(p, scope) {
				out = append(out, p)
			}
		}
		return out
	}
	return model.AuthorizationPoliciesResult{
		Custom: filter(policies.Custom),
		Deny:   filter(policies.Deny),
		Allow:  filter(policies.Allow),
		Audit:  filter(policies.Audit),
	}
}

func appliesToScope(p model.AuthorizationPolicy, scope types.NamespacedName) bool {
	for _, targetRef := range model.GetTargetRefs(p.Spec) {
		if config.CanonicalGroup(targetRef.GetGroup()) == gvk.ListenerSet.CanonicalGroup() && targetRef.GetKind() == gvk.ListenerSet.Kind {
			return targetRef.GetName() == scope.Name && p.Namespace == scope.Namespace
		}
	}
	return true
}

func (b *Builder) BuildTCPRulesAsHTTPFilter() []*hcm.HttpFilter {
	if b == nil {
		return nil
	}
	inner := builder.New(b.tdBundle, b.push, b.policies, b.option)
	if inner == nil {
		return nil
	}
	return inner.BuildTCPRulesAsHTTPFilter()
}

func (b *Builder) BuildTCP(scope types.NamespacedName) []*listener.Filter {
	if b == nil {
		return nil
	}
	if filters, ok := b.tcpFilters[scope]; ok {
		return filters
	}
	var filters []*listener.Filter
	if inner := builder.New(b.tdBundle, b.push, scopedPolicies(b.policies, scope), b.option); inner != nil {
		filters = inner.BuildTCP()
	}
	if b.tcpFilters == nil {
		b.tcpFilters = map[types.NamespacedName][]*listener.Filter{}
	}
	b.tcpFilters[scope] = filters
	return filters
}

func (b *Builder) BuildHTTP(class networking.ListenerClass, scope types.NamespacedName) []*hcm.HttpFilter {
	if b == nil {
		return nil
	}
	if class == networking.ListenerClassSidecarOutbound {
		// Only applies to inbound and gateways
		return nil
	}
	if filters, ok := b.httpFilters[scope]; ok {
		return filters
	}
	var filters []*hcm.HttpFilter
	if inner := builder.New(b.tdBundle, b.push, scopedPolicies(b.policies, scope), b.option); inner != nil {
		filters = inner.BuildHTTP()
	}
	if b.httpFilters == nil {
		b.httpFilters = map[types.NamespacedName][]*hcm.HttpFilter{}
	}
	b.httpFilters[scope] = filters
	return filters
}
