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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
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
	tdBundle := trustdomain.NewBundle(push.Mesh.TrustDomain, push.Mesh.TrustDomainAliases)
	option := builder.Option{
		IsCustomBuilder: actionType == Custom,
		UseFilterState:  useFilterState,
	}
	selectionOpts := model.PolicyMatcherForProxy(proxy).WithService(svc).WithRootNamespace(push.AuthzPolicies.RootNamespace)
	policies := push.AuthzPolicies.ListAuthorizationPolicies(selectionOpts)
	b := builder.New(tdBundle, push, policies, option)
	return &Builder{builder: b}
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
