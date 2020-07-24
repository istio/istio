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

package networking

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	thrift_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/thrift_proxy/v2alpha1"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/protocol"
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
	// ListenerProtocolThrift is a Thrift listener.
	ListenerProtocolThrift
	// ListenerProtocolAuto enables auto protocol detection
	ListenerProtocolAuto
)

// ModelProtocolToListenerProtocol converts from a config.Protocol to its corresponding plugin.ListenerProtocol
func ModelProtocolToListenerProtocol(p protocol.Instance,
	trafficDirection core.TrafficDirection) ListenerProtocol {
	// If protocol sniffing is not enabled, the default value is TCP
	if p == protocol.Unsupported {
		switch trafficDirection {
		case core.TrafficDirection_INBOUND:
			if !features.EnableProtocolSniffingForInbound {
				p = protocol.TCP
			}
		case core.TrafficDirection_OUTBOUND:
			if !features.EnableProtocolSniffingForOutbound {
				p = protocol.TCP
			}
		default:
			// Should not reach here.
		}
	}

	switch p {
	case protocol.HTTP, protocol.HTTP2, protocol.GRPC, protocol.GRPCWeb:
		return ListenerProtocolHTTP
	case protocol.TCP, protocol.HTTPS, protocol.TLS,
		protocol.Mongo, protocol.Redis, protocol.MySQL:
		return ListenerProtocolTCP
	case protocol.Thrift:
		if features.EnableThriftFilter {
			return ListenerProtocolThrift
		}
		return ListenerProtocolTCP
	case protocol.UDP:
		return ListenerProtocolUnknown
	case protocol.Unsupported:
		return ListenerProtocolAuto
	default:
		// Should not reach here.
		return ListenerProtocolAuto
	}
}

// FilterChain describes a set of filters (HTTP or TCP) with a shared TLS context.
type FilterChain struct {
	// FilterChainMatch is the match used to select the filter chain.
	FilterChainMatch *listener.FilterChainMatch
	// TLSContext is the TLS settings for this filter chains.
	TLSContext *tls.DownstreamTlsContext
	// ListenerFilters are the filters needed for the whole listener, not particular to this
	// filter chain.
	ListenerFilters []*listener.ListenerFilter
	// ListenerProtocol indicates whether this filter chain is for HTTP or TCP
	// Note that HTTP filter chains can also have network filters
	ListenerProtocol ListenerProtocol
	// IstioMutualGateway is set only when this filter chain is part of a Gateway, and
	// the Server corresponding to this filter chain is doing TLS termination with ISTIO_MUTUAL as the TLS mode.
	// This allows the authN plugin to add the istio_authn filter to gateways in addition to sidecars.
	IstioMutualGateway bool

	// HTTP is the set of HTTP filters for this filter chain
	HTTP []*http_conn.HttpFilter
	// Thrift is the set of Thrift filters for this filter chain
	Thrift []*thrift_proxy.ThriftFilter
	// TCP is the set of network (TCP) filters for this filter chain.
	TCP []*listener.Filter
	// IsFallthrough indicates if the filter chain is fallthrough.
	IsFallThrough bool
}

// MutableObjects is a set of objects passed to On*Listener callbacks. Fields may be nil or empty.
// Any lists should not be overridden, but rather only appended to.
// Non-list fields may be mutated; however it's not recommended to do this since it can affect other plugins in the
// chain in unpredictable ways.
type MutableObjects struct {
	// Listener is the listener being built. Must be initialized before Plugin methods are called.
	Listener *listener.Listener

	// FilterChains is the set of filter chains that will be attached to Listener.
	FilterChains []FilterChain
}
