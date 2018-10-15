// Copyright 2018 Istio Authors
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

package snidnat

import (
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

// Plugin implements plugin interface
type Plugin struct{}

// NewPlugin returns an instance of the sni-dnat plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

const (
	istioPolicySvcIdentifier    = "istio-policy."
	istioTelemetrySvcIdentifier = "istio-telemetry."
)

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
	if in.Node == nil || in.Node.Type != model.Router ||
		in.Node.GetRouterMode() != model.SniDnatRouter {
		return
	}

	// The SNI-DNAT router selects internal clusters using the incoming SNI value.
	// Since its doing SNI passthrough, we should not add any upstream TLS options for this router.
	// The only exception is istio-policy and istio-telemetry, as traffic to these services
	// originate from the mixer filter in Envoy and not from the end user.
	if in.Service == nil || in.Service.MeshExternal ||
		strings.HasPrefix(string(in.Service.Hostname), istioPolicySvcIdentifier) ||
		strings.HasPrefix(string(in.Service.Hostname), istioTelemetrySvcIdentifier) {
		// This is a gross hack - looking for istio-policy/istio-telemetry prefixes
		// in the service names and making an exception for them. However, there is
		// no other way to accomplish this in a clean way. Atleast, this ugly code is
		// confined to a plugin that can be disabled if need be.
		return
	}

	// Reset the TLS context for all other clusters belonging to internal services
	cluster.TlsContext = nil
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (Plugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}
