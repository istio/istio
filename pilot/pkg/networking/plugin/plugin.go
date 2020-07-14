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

package plugin

import (
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
)

const (
	// Authn is the name of the authentication plugin passed through the command line
	Authn = "authn"
	// Authz is the name of the rbac plugin passed through the command line
	Authz = "authz"
	// Health is the name of the health plugin passed through the command line
	Health = "health"
	// Mixer is the name of the mixer plugin passed through the command line
	Mixer = "mixer"
)

// InputParams is a set of values passed to Plugin callback methods. Not all fields are guaranteed to
// be set, it's up to the callee to validate required fields are set and emit error if they are not.
// These are for reading only and should not be modified.
type InputParams struct {
	// ListenerProtocol is the protocol/class of listener (TCP, HTTP etc.). Must be set.
	// This is valid only for the inbound listener
	// Outbound listeners could have multiple filter chains, where one filter chain could be
	// a HTTP connection manager with TLS context, while the other could be a tcp proxy with sni
	ListenerProtocol istionetworking.ListenerProtocol
	// ListenerCategory is the type of listener (sidecar_inbound, sidecar_outbound, gateway). Must be set
	ListenerCategory networking.EnvoyFilter_PatchContext

	// Node is the node the response is for.
	Node *model.Proxy
	// ServiceInstance is the service instance colocated with the listener (applies to sidecar).
	ServiceInstance *model.ServiceInstance
	// Service is the service colocated with the listener (applies to sidecar).
	// For outbound TCP listeners, it is the destination service.
	Service *model.Service
	// Port is the port for which the listener is being built
	// For outbound/inbound sidecars this is the service port (not endpoint port)
	// For inbound listener on gateway, this is the gateway server port
	Port *model.Port
	// Bind holds the listener IP or unix domain socket to which this listener is bound
	// if bind is using UDS, the port will be 0 with valid protocol and name
	Bind string

	// Push holds stats and other information about the current push.
	Push *model.PushContext

	// Inbound cluster name. It's only used by newHTTPPassThroughFilterChain.
	// For other scenarios, the field is empty.
	InboundClusterName string
}

// Plugin is called during the construction of a listener.Listener which may alter the Listener in any
// way. Examples include AuthenticationPlugin that sets up mTLS authentication on the inbound Listener
// and outbound Cluster, the mixer plugin that sets up policy checks on the inbound listener, etc.
type Plugin interface {
	// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service.
	// Can be used to add additional filters on the outbound path.
	OnOutboundListener(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnOutboundPassthroughFilterChain is called when the outbound listener is built. The mutable.FilterChains provides
	// all the passthough filter chains with a TCP proxy at the end of the filters.
	OnOutboundPassthroughFilterChain(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
	// Can be used to add additional filters.
	OnInboundListener(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnVirtualListener is called whenever a new virtual listener is added to the
	// LDS output for a given service
	// Can be used to add additional filters.
	OnVirtualListener(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnOutboundCluster is called whenever a new cluster is added to the CDS output.
	// This is called once per push cycle, and not for every sidecar/gateway, except for gateways with non-standard
	// operating modes.
	OnOutboundCluster(in *InputParams, cluster *cluster.Cluster)

	// OnInboundCluster is called whenever a new cluster is added to the CDS output.
	// Called for each sidecar
	OnInboundCluster(in *InputParams, cluster *cluster.Cluster)

	// OnOutboundRouteConfiguration is called whenever a new set of virtual hosts (a set of virtual hosts with routes) is
	// added to RDS in the outbound path.
	OnOutboundRouteConfiguration(in *InputParams, routeConfiguration *route.RouteConfiguration)

	// OnInboundRouteConfiguration is called whenever a new set of virtual hosts are added to the inbound path.
	OnInboundRouteConfiguration(in *InputParams, routeConfiguration *route.RouteConfiguration)

	// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain
	// configuration, like FilterChainMatch and TLSContext.
	OnInboundFilterChains(in *InputParams) []istionetworking.FilterChain

	// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
	// Can be used to add additional filters.
	OnInboundPassthrough(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnInboundPassthroughFilterChains is called whenever a plugin needs to setup custom pass through filter chain.
	OnInboundPassthroughFilterChains(in *InputParams) []istionetworking.FilterChain
}
