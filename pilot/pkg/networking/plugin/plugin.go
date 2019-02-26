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

package plugin

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

// ListenerProtocol is the protocol associated with the listener.
type ListenerProtocol int

const (
	// ListenerProtocolUnknown is an unknown type of listener.
	ListenerProtocolUnknown = iota
	// ListenerProtocolTCP is a TCP listener.
	ListenerProtocolTCP
	// ListenerProtocolHTTP is an HTTP listener.
	ListenerProtocolHTTP

	// Authn is the name of the authentication plugin passed through the command line
	Authn = "authn"
	// Authz is the name of the rbac plugin passed through the command line
	Authz = "authz"
	// Health is the name of the health plugin passed through the command line
	Health = "health"
	// Mixer is the name of the mixer plugin passed through the command line
	Mixer = "mixer"
)

// ModelProtocolToListenerProtocol converts from a model.Protocol to its corresponding plugin.ListenerProtocol
func ModelProtocolToListenerProtocol(protocol model.Protocol) ListenerProtocol {
	switch protocol {
	case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC, model.ProtocolGRPCWeb:
		return ListenerProtocolHTTP
	case model.ProtocolTCP, model.ProtocolHTTPS, model.ProtocolTLS,
		model.ProtocolMongo, model.ProtocolRedis, model.ProtocolMySQL:
		return ListenerProtocolTCP
	default:
		return ListenerProtocolUnknown
	}
}

// InputParams is a set of values passed to Plugin callback methods. Not all fields are guaranteed to
// be set, it's up to the callee to validate required fields are set and emit error if they are not.
// These are for reading only and should not be modified.
type InputParams struct {
	// ListenerProtocol is the protocol/class of listener (TCP, HTTP etc.). Must be set.
	// This is valid only for the inbound listener
	// Outbound listeners could have multiple filter chains, where one filter chain could be
	// a HTTP connection manager with TLS context, while the other could be a tcp proxy with sni
	ListenerProtocol ListenerProtocol
	// ListenerCategory is the type of listener (sidecar_inbound, sidecar_outbound, gateway). Must be set
	ListenerCategory networking.EnvoyFilter_ListenerMatch_ListenerType

	// Env is the model environment. Must be set.
	Env *model.Environment
	// Node is the node the response is for.
	Node *model.Proxy
	// ProxyInstances is a slice of all proxy service instances in the mesh.
	ProxyInstances []*model.ServiceInstance
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
	// SidecarConfig holds the Sidecar CRD associated with this listener
	SidecarConfig *model.Config

	// The subset associated with the service for which the cluster is being programmed
	Subset string
	// Push holds stats and other information about the current push.
	Push *model.PushContext
}

// FilterChain describes a set of filters (HTTP or TCP) with a shared TLS context.
type FilterChain struct {
	// FilterChainMatch is the match used to select the filter chain.
	FilterChainMatch *listener.FilterChainMatch
	// TLSContext is the TLS settings for this filter chains.
	TLSContext *auth.DownstreamTlsContext
	// ListenerFilters are the filters needed for the whole listener, not particular to this
	// filter chain.
	ListenerFilters []listener.ListenerFilter
	// ListenerProtocol indicates whether this filter chain is for HTTP or TCP
	// Note that HTTP filter chains can also have network filters
	ListenerProtocol ListenerProtocol
	// HTTP is the set of HTTP filters for this filter chain
	HTTP []*http_conn.HttpFilter
	// TCP is the set of network (TCP) filters for this filter chain.
	TCP []listener.Filter
}

// MutableObjects is a set of objects passed to On*Listener callbacks. Fields may be nil or empty.
// Any lists should not be overridden, but rather only appended to.
// Non-list fields may be mutated; however it's not recommended to do this since it can affect other plugins in the
// chain in unpredictable ways.
type MutableObjects struct {
	// Listener is the listener being built. Must be initialized before Plugin methods are called.
	Listener *xdsapi.Listener

	// FilterChains is the set of filter chains that will be attached to Listener.
	FilterChains []FilterChain
}

// Plugin is called during the construction of a xdsapi.Listener which may alter the Listener in any
// way. Examples include AuthenticationPlugin that sets up mTLS authentication on the inbound Listener
// and outbound Cluster, the mixer plugin that sets up policy checks on the inbound listener, etc.
type Plugin interface {
	// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service.
	// Can be used to add additional filters on the outbound path.
	OnOutboundListener(in *InputParams, mutable *MutableObjects) error

	// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
	// Can be used to add additional filters.
	OnInboundListener(in *InputParams, mutable *MutableObjects) error

	// OnOutboundCluster is called whenever a new cluster is added to the CDS output.
	// This is called once per push cycle, and not for every sidecar/gateway, except for gateways with non-standard
	// operating modes.
	OnOutboundCluster(in *InputParams, cluster *xdsapi.Cluster)

	// OnInboundCluster is called whenever a new cluster is added to the CDS output.
	// Called for each sidecar
	OnInboundCluster(in *InputParams, cluster *xdsapi.Cluster)

	// OnOutboundRouteConfiguration is called whenever a new set of virtual hosts (a set of virtual hosts with routes) is
	// added to RDS in the outbound path.
	OnOutboundRouteConfiguration(in *InputParams, routeConfiguration *xdsapi.RouteConfiguration)

	// OnInboundRouteConfiguration is called whenever a new set of virtual hosts are added to the inbound path.
	OnInboundRouteConfiguration(in *InputParams, routeConfiguration *xdsapi.RouteConfiguration)

	// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain
	// configuration, like FilterChainMatch and TLSContext.
	OnInboundFilterChains(in *InputParams) []FilterChain
}
