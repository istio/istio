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
	"fmt"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pkg/config/labels"
)

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
		in.ServiceInstance.Endpoint.EndpointPort, in.Push.Mesh.SdsUdsPath, in.Node, in.ListenerProtocol, trustDomainsForValidation(in.Push.Mesh))
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.Router {
		// Only care about router.
		return nil
	}

	return buildFilter(in, mutable)
}

func (Plugin) OnOutboundPassthroughFilterChain(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
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
	applier := factory.NewPolicyApplier(in.Push, ns, labels.Collection{in.Node.Metadata.Labels})
	if mutable.Listener == nil || (len(mutable.Listener.FilterChains) != len(mutable.FilterChains)) {
		return fmt.Errorf("expected same number of filter chains in listener (%d) and mutable (%d)", len(mutable.Listener.FilterChains), len(mutable.FilterChains))
	}
	endpointPort := uint32(0)
	if in.ServiceInstance != nil {
		endpointPort = in.ServiceInstance.Endpoint.EndpointPort
	}

	for i := range mutable.Listener.FilterChains {
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

// OnVirtualListener implments the Plugin interface method.
func (Plugin) OnVirtualListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(in *plugin.InputParams, cluster *cluster.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *route.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *route.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(in *plugin.InputParams, cluster *cluster.Cluster) {
}

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
func (Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return nil
}

// OnInboundPassthroughFilterChains is called for plugin to update the pass through filter chain.
func (Plugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []networking.FilterChain {
	// Pass nil for ServiceInstance so that we never consider any alpha policy for the pass through filter chain.
	applier := factory.NewPolicyApplier(in.Push, in.Node.Metadata.Namespace, labels.Collection{in.Node.Metadata.Labels})
	// Pass 0 for endpointPort so that it never matches any port-level policy.
	return applier.InboundFilterChain(0, in.Push.Mesh.SdsUdsPath, in.Node, in.ListenerProtocol, trustDomainsForValidation(in.Push.Mesh))
}
