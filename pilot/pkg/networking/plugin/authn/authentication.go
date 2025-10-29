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
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pkg/log"
)

var authnLog = log.RegisterScope("authn", "authn debugging")

type Builder struct {
	applier      authn.PolicyApplier
	trustDomains []string
	proxy        *model.Proxy
}

func NewBuilder(push *model.PushContext, proxy *model.Proxy) *Builder {
	return NewBuilderForService(push, proxy, nil)
}

func NewBuilderForService(push *model.PushContext, proxy *model.Proxy, svc *model.Service) *Builder {
	applier := authn.NewPolicyApplier(push, proxy, svc)
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
	return b.applier.InboundMTLSSettings(port, b.proxy, b.trustDomains, authn.NoOverride)
}

func (b *Builder) ForHBONE() authn.MTLSSettings {
	if b == nil {
		return authn.MTLSSettings{
			Port: model.HBoneInboundListenPort,
			Mode: model.MTLSDisable,
		}
	}
	// HBONE is always strict
	return b.applier.InboundMTLSSettings(model.HBoneInboundListenPort, b.proxy, b.trustDomains, model.MTLSStrict)
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
		b.applier.InboundMTLSSettings(0, b.proxy, b.trustDomains, authn.NoOverride),
	}

	// Then generate the per-port passthrough filter chains.
	for port := range b.applier.PortLevelSetting() {
		// Skip the per-port passthrough filterchain if the port is already handled by InboundMTLSConfiguration().
		if !needPerPortPassthroughFilterChain(port, b.proxy) {
			continue
		}

		authnLog.Debugf("InboundMTLSConfiguration: build extra pass through filter chain for %v:%d", b.proxy.ID, port)
		resp = append(resp, b.applier.InboundMTLSSettings(port, b.proxy, b.trustDomains, authn.NoOverride))
	}
	return resp
}

func (b *Builder) BuildHTTP(class networking.ListenerClass) []*hcm.HttpFilter {
	if b == nil {
		return nil
	}
	if class == networking.ListenerClassSidecarOutbound {
		// Only applies to inbound and gateways
		return nil
	}
	filter := b.applier.JwtFilter(b.proxy.Type != model.SidecarProxy)
	if filter != nil {
		return []*hcm.HttpFilter{filter}
	}
	return nil
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
	// Use GetServiceTargetsSnapshot to avoid race conditions with concurrent updates.
	serviceTargets := node.GetServiceTargetsSnapshot()
	for _, si := range serviceTargets {
		if port == si.Port.TargetPort {
			return false
		}
	}
	return true
}
