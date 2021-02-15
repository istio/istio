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
	envoy_config_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
)

var authnLog = log.RegisterScope("authn", "authn debugging", 0)

// Plugin implements Istio mTLS auth
type Plugin struct{}

// NewPlugin returns an instance of the authn plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnInboundFilterChains setups filter chains based on the authentication policy.
func (Plugin) OnInboundFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return factory.NewPolicyApplier(in.Push,
		in.Node.Metadata.Namespace, labels.Collection{in.Node.Metadata.Labels}).InboundFilterChain(
		in.ServiceInstance.Endpoint.EndpointPort, in.Node,
		in.ListenerProtocol, trustDomainsForValidation(in.Push.Mesh))
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.Router {
		// Only care about router.
		return nil
	}

	return buildFilter(in, mutable, false)
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}
	return buildFilter(in, mutable, false)
}

func buildFilter(in *plugin.InputParams, mutable *networking.MutableObjects, isPassthrough bool) error {
	ns := in.Node.Metadata.Namespace
	applier := factory.NewPolicyApplier(in.Push, ns, labels.Collection{in.Node.Metadata.Labels})
	endpointPort := uint32(0)
	if in.ServiceInstance != nil {
		endpointPort = in.ServiceInstance.Endpoint.EndpointPort
	}

	for i := range mutable.FilterChains {
		if isPassthrough {
			// Get the real port from the filter chain match if this is generated for pass through filter chain.
			endpointPort = mutable.FilterChains[i].FilterChainMatch.GetDestinationPort().GetValue()
		}
		if in.ListenerProtocol == networking.ListenerProtocolHTTP || mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolHTTP {
			// Adding Jwt filter and authn filter, if needed.
			if filter := applier.JwtFilter(); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
			istioMutualGateway := (in.Node.Type == model.Router) && mutable.FilterChains[i].IstioMutualGateway
			if filter := applier.AuthNFilter(in.Node.Type, endpointPort, istioMutualGateway); filter != nil {
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

	return buildFilter(in, mutable, true)
}

// OnInboundPassthroughFilterChains is called for plugin to update the pass through filter chain.
func (Plugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []networking.FilterChain {
	applier := factory.NewPolicyApplier(in.Push, in.Node.Metadata.Namespace, labels.Collection{in.Node.Metadata.Labels})
	trustDomains := trustDomainsForValidation(in.Push.Mesh)
	// First generate the default passthrough filter chains, pass 0 for endpointPort so that it never matches any port-level policy.
	filterChains := applier.InboundFilterChain(0, in.Node, in.ListenerProtocol, trustDomains)

	// Then generate the per-port passthrough filter chains.
	for port := range applier.PortLevelSetting() {
		// Skip the per-port passthrough filterchain if the port is already handled by OnInboundFilterChains().
		if !needPerPortPassthroughFilterChain(port, in.Node) {
			continue
		}

		authnLog.Debugf("InboundPassthroughFilterChains: build extra pass through filter chain for %v:%d", in.Node.ID, port)
		portLevelFilterChains := applier.InboundFilterChain(port, in.Node, in.ListenerProtocol, trustDomains)
		for _, fc := range portLevelFilterChains {
			// Set the port to distinguish from the default passthrough filter chain.
			if fc.FilterChainMatch == nil {
				fc.FilterChainMatch = &envoy_config_listener_v3.FilterChainMatch{}
			}
			fc.FilterChainMatch.DestinationPort = &wrappers.UInt32Value{Value: port}
			filterChains = append(filterChains, fc)
		}
	}

	return filterChains
}

func needPerPortPassthroughFilterChain(port uint32, node *model.Proxy) bool {
	// If there is any Sidecar defined, check if the port is explicitly defined there.
	// This means the Sidecar resource takes precedence over the service. A port defined in service but not in Sidecar
	// means the port is going to be handled by the pass through filter chain.
	if node.SidecarScope.HasCustomIngressListeners {
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
