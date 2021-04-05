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

package v1alpha3

import (
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

// FilterChainMatchOptions describes options used for filter chain matches.
type FilterChainMatchOptions struct {
	// Application protocols of the filter chain match
	ApplicationProtocols []string
	// Transport protocol of the filter chain match. "tls" or empty
	TransportProtocol string
	// Filter chain protocol. HTTP for HTTP proxy and TCP for TCP proxy
	Protocol istionetworking.ListenerProtocol
	// Whether this chain should terminate mTLS or not
	MTLS bool
}

// Set of filter chain match options used for various combinations.
var (
	// Same as inboundPermissiveFilterChainMatchOptions except for following case:
	// FCM 3: ALPN [istio-peer-exchange, istio] Transport protocol: tls            --> TCP traffic from sidecar over TLS
	inboundPermissiveFilterChainMatchWithMxcOptions = []FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener, sent over mTLS
			ApplicationProtocols: mtlsHTTPALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			Protocol:          istionetworking.ListenerProtocolHTTP,
			MTLS:              true,
		},
		{
			// client side traffic was detected as HTTP by the outbound listener, sent out as plain text
			ApplicationProtocols: plaintextHTTPALPNs,
			// No transport protocol match as this filter chain (+match) will be used for plain text connections
			Protocol:          istionetworking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
		{
			// client side traffic could not be identified by the outbound listener, but sent over mTLS
			ApplicationProtocols: mtlsTCPWithMxcALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			Protocol:          istionetworking.ListenerProtocolTCP,
			MTLS:              true,
		},
		{
			// client side traffic could not be identified by the outbound listener, sent over plaintext
			// or it could be that the client has no sidecar. In this case, this filter chain is simply
			// receiving plaintext TCP traffic.
			Protocol:          istionetworking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
		{
			// client side traffic could not be identified by the outbound listener, sent over one-way
			// TLS (HTTPS for example) by the downstream application.
			// or it could be that the client has no sidecar, and it is directly making a HTTPS connection to
			// this sidecar. In this case, this filter chain is receiving plaintext one-way TLS traffic. The TLS
			// inspector would detect this as TLS traffic [not necessarily mTLS]. But since there is no ALPN to match,
			// this filter chain match will treat the traffic as just another TCP proxy.
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			Protocol:          istionetworking.ListenerProtocolTCP,
		},
	}
	inboundPermissiveHTTPFilterChainMatchWithMxcOptions = []FilterChainMatchOptions{
		{
			// HTTP over MTLS
			ApplicationProtocols: allIstioMtlsALPNs,
			TransportProtocol:    xdsfilters.TLSTransportProtocol,
			Protocol:             istionetworking.ListenerProtocolHTTP,
			MTLS:                 true,
		},
		{
			// Plaintext HTTP
			Protocol:          istionetworking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
		// We do not need to handle other simple TLS or others, as this is explicitly declared as HTTP type.
	}
	inboundPermissiveTCPFilterChainMatchWithMxcOptions = []FilterChainMatchOptions{
		{
			// MTLS
			ApplicationProtocols: allIstioMtlsALPNs,
			TransportProtocol:    xdsfilters.TLSTransportProtocol,
			Protocol:             istionetworking.ListenerProtocolTCP,
			MTLS:                 true,
		},
		{
			// Plain TLS
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			Protocol:          istionetworking.ListenerProtocolTCP,
		},
		{
			// Plaintext
			Protocol:          istionetworking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}

	inboundStrictFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener.
			// If we are in strict mode, we will get mTLS HTTP ALPNS only.
			ApplicationProtocols: mtlsHTTPALPNs,
			Protocol:             istionetworking.ListenerProtocolHTTP,
			TransportProtocol:    xdsfilters.TLSTransportProtocol,
			MTLS:                 true,
		},
		{
			// Could not detect traffic on the client side. Server side has no mTLS.
			Protocol:          istionetworking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			MTLS:              true,
		},
	}
	inboundStrictTCPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          istionetworking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			MTLS:              true,
		},
	}
	inboundStrictHTTPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          istionetworking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			MTLS:              true,
		},
	}

	inboundPlainTextFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			ApplicationProtocols: plaintextHTTPALPNs,
			Protocol:             istionetworking.ListenerProtocolHTTP,
			TransportProtocol:    xdsfilters.RawBufferTransportProtocol,
		},
		{
			// Could not detect traffic on the client side. Server side has no mTLS.
			Protocol:          istionetworking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}
	inboundPlainTextTCPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          istionetworking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}
	inboundPlainTextHTTPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          istionetworking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}

	emptyFilterChainMatch = &listener.FilterChainMatch{}
)
