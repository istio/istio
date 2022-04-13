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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/pkg/log"
)

var authnLog = log.RegisterScope("authn", "authn debugging", 0)

// Plugin implements Istio mTLS auth
type Plugin struct{}

// NewPlugin returns an instance of the authn plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

var _ plugin.Plugin = Plugin{}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.Router {
		// Only care about router.
		return nil
	}

	return buildFilter(in, mutable)
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}
	return buildFilter(in, mutable)
}

func buildFilter(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	ns := in.Node.Metadata.Namespace
	applier := factory.NewPolicyApplier(in.Push, ns, in.Node.Metadata.Labels)
	forSidecar := in.Node.Type == model.SidecarProxy
	for i := range mutable.FilterChains {
		if mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolHTTP {
			// Adding Jwt filter and authn filter, if needed.
			if filter := applier.JwtFilter(); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
			if filter := applier.AuthNFilter(forSidecar); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
		}
	}

	return nil
}

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
func (Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	return buildFilter(in, mutable)
}

func (p Plugin) InboundMTLSConfiguration(in *plugin.InputParams, passthrough bool) []plugin.MTLSSettings {
	applier := factory.NewPolicyApplier(in.Push, in.Node.Metadata.Namespace, in.Node.Metadata.Labels)
	trustDomains := TrustDomainsForValidation(in.Push.Mesh)

	port := in.ServiceInstance.Endpoint.EndpointPort

	// For non passthrough, set up the specific port
	if !passthrough {
		return []plugin.MTLSSettings{
			applier.InboundMTLSSettings(port, in.Node, trustDomains),
		}
	}
	// Otherwise, this is for passthrough configuration. We need to create configuration for the passthrough,
	// but also any ports that are not explicitly declared in the Service but are in the mTLS port level settings.
	resp := []plugin.MTLSSettings{
		// Full passthrough - no port match
		applier.InboundMTLSSettings(0, in.Node, trustDomains),
	}

	// Then generate the per-port passthrough filter chains.
	for port := range applier.PortLevelSetting() {
		// Skip the per-port passthrough filterchain if the port is already handled by InboundMTLSConfiguration().
		if !needPerPortPassthroughFilterChain(port, in.Node) {
			continue
		}

		authnLog.Debugf("InboundMTLSConfiguration: build extra pass through filter chain for %v:%d", in.Node.ID, port)
		resp = append(resp, applier.InboundMTLSSettings(port, in.Node, trustDomains))
	}
	return resp
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
