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

package authn

import (
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/pkg/log"
)

var authnLog = log.RegisterScope("authn", "authn debugging", 0)

type Builder struct {
	applier      authn.PolicyApplier
	trustDomains []string
	proxy        *model.Proxy
}

func NewBuilder(push *model.PushContext, proxy *model.Proxy) *Builder {
	applier := factory.NewPolicyApplier(push, proxy.Metadata.Namespace, proxy.Labels)
	trustDomains := TrustDomainsForValidation(push.Mesh)
	return &Builder{
		applier:      applier,
		proxy:        proxy,
		trustDomains: trustDomains,
	}
}

func (b *Builder) ForPort(port uint32) authn.MTLSSettings {
	if b == nil {
		return authn.MTLSSettings{
			Port: port,
			Mode: model.MTLSDisable,
		}
	}
	return b.applier.InboundMTLSSettings(port, b.proxy, b.trustDomains)
}

func (b *Builder) ForPassthrough() []authn.MTLSSettings {
	if b == nil {
		return []authn.MTLSSettings{{
			Port: 0,
			Mode: model.MTLSDisable,
		}}
	}
	//	We need to create configuration for the passthrough,
	// but also any ports that are not explicitly declared in the Service but are in the mTLS port level settings.

	resp := []authn.MTLSSettings{
		// Full passthrough - no port match
		b.applier.InboundMTLSSettings(0, b.proxy, b.trustDomains),
	}

	// Then generate the per-port passthrough filter chains.
	for port := range b.applier.PortLevelSetting() {
		// Skip the per-port passthrough filterchain if the port is already handled by InboundMTLSConfiguration().
		if !needPerPortPassthroughFilterChain(port, b.proxy) {
			continue
		}

		authnLog.Debugf("InboundMTLSConfiguration: build extra pass through filter chain for %v:%d", b.proxy.ID, port)
		resp = append(resp, b.applier.InboundMTLSSettings(port, b.proxy, b.trustDomains))
	}
	return resp
}

func (b *Builder) BuildHTTP(class networking.ListenerClass) []*httppb.HttpFilter {
	if b == nil {
		return nil
	}
	if class == networking.ListenerClassSidecarOutbound {
		// Only applies to inbound and gateways
		return nil
	}
	res := []*httppb.HttpFilter{}
	if filter := b.applier.JwtFilter(); filter != nil {
		res = append(res, filter)
	}
	forSidecar := b.proxy.Type == model.SidecarProxy
	if filter := b.applier.AuthNFilter(forSidecar); filter != nil {
		res = append(res, filter)
	}

	return res
}

func needPerPortPassthroughFilterChain(port uint32, node *model.Proxy) bool {
	// If there is any Sidecar defined, check if the port is explicitly defined there.
	// This means the Sidecar resource takes precedence over the service. A port defined in service but not in Sidecar
	// means the port is going to be handled by the pass through filter chain.
	if node.SidecarScope.HasIngressListener() {
		for _, ingressListener := range node.SidecarScope.Sidecar.Ingress {
			if port == ingressListener.Port.Number {
				return false
			}
		}
		return true
	}

	// If there is no Sidecar, check if the port is appearing in any service.
	for _, si := range node.ServiceInstances {
		if port == si.Endpoint.EndpointPort {
			return false
		}
	}
	return true
}
