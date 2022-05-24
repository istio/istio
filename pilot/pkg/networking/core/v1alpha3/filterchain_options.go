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
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

// FilterChainMatchOptions describes options used for filter chain matches.
type FilterChainMatchOptions struct {
	// Application protocols of the filter chain match
	ApplicationProtocols []string
	// Transport protocol of the filter chain match. "tls" or empty
	TransportProtocol string
	// Filter chain protocol. HTTP for HTTP proxy and TCP for TCP proxy
	Protocol networking.ListenerProtocol
	// Whether this chain should terminate TLS or not
	TLS bool
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
			Protocol:          networking.ListenerProtocolHTTP,
			TLS:               true,
		},
		{
			// client side traffic was detected as HTTP by the outbound listener, sent out as plain text
			ApplicationProtocols: plaintextHTTPALPNs,
			// No transport protocol match as this filter chain (+match) will be used for plain text connections
			Protocol:          networking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
		{
			// client side traffic could not be identified by the outbound listener, but sent over mTLS
			ApplicationProtocols: mtlsTCPWithMxcALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			Protocol:          networking.ListenerProtocolTCP,
			TLS:               true,
		},
		{
			// client side traffic could not be identified by the outbound listener, sent over plaintext
			// or it could be that the client has no sidecar. In this case, this filter chain is simply
			// receiving plaintext TCP traffic.
			Protocol:          networking.ListenerProtocolTCP,
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
			Protocol:          networking.ListenerProtocolTCP,
		},
	}
	inboundPermissiveHTTPFilterChainMatchWithMxcOptions = []FilterChainMatchOptions{
		{
			// HTTP over MTLS
			ApplicationProtocols: allIstioMtlsALPNs,
			TransportProtocol:    xdsfilters.TLSTransportProtocol,
			Protocol:             networking.ListenerProtocolHTTP,
			TLS:                  true,
		},
		{
			// Plaintext HTTP
			Protocol:          networking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
		// We do not need to handle other simple TLS or others, as this is explicitly declared as HTTP type.
	}
	inboundPermissiveTCPFilterChainMatchWithMxcOptions = []FilterChainMatchOptions{
		{
			// MTLS
			ApplicationProtocols: allIstioMtlsALPNs,
			TransportProtocol:    xdsfilters.TLSTransportProtocol,
			Protocol:             networking.ListenerProtocolTCP,
			TLS:                  true,
		},
		{
			// Plain TLS
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			Protocol:          networking.ListenerProtocolTCP,
		},
		{
			// Plaintext
			Protocol:          networking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}

	inboundStrictFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener.
			// If we are in strict mode, we will get mTLS HTTP ALPNS only.
			ApplicationProtocols: mtlsHTTPALPNs,
			Protocol:             networking.ListenerProtocolHTTP,
			TransportProtocol:    xdsfilters.TLSTransportProtocol,
			TLS:                  true,
		},
		{
			// Could not detect traffic on the client side. Server side has no mTLS.
			Protocol:          networking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			TLS:               true,
		},
	}
	inboundStrictTCPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          networking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			TLS:               true,
		},
	}
	inboundStrictHTTPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          networking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			TLS:               true,
		},
	}

	inboundPlainTextFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			ApplicationProtocols: plaintextHTTPALPNs,
			Protocol:             networking.ListenerProtocolHTTP,
			TransportProtocol:    xdsfilters.RawBufferTransportProtocol,
		},
		{
			// Could not detect traffic on the client side. Server side has no mTLS.
			Protocol:          networking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}
	inboundPlainTextTCPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          networking.ListenerProtocolTCP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}
	inboundPlainTextHTTPFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			Protocol:          networking.ListenerProtocolHTTP,
			TransportProtocol: xdsfilters.RawBufferTransportProtocol,
		},
	}

	emptyFilterChainMatch = &listener.FilterChainMatch{}
)

// getTLSFilterChainMatchOptions returns the FilterChainMatchOptions that should be used based on mTLS mode and protocol
func getTLSFilterChainMatchOptions(protocol networking.ListenerProtocol) []FilterChainMatchOptions {
	return []FilterChainMatchOptions{{
		Protocol:          protocol,
		TransportProtocol: xdsfilters.TLSTransportProtocol,
		TLS:               true,
	}}
}

// getFilterChainMatchOptions returns the FilterChainMatchOptions that should be used based on mTLS mode and protocol
func getFilterChainMatchOptions(settings plugin.MTLSSettings, protocol networking.ListenerProtocol) []FilterChainMatchOptions {
	switch protocol {
	case networking.ListenerProtocolHTTP:
		switch settings.Mode {
		case model.MTLSStrict:
			return inboundStrictHTTPFilterChainMatchOptions
		case model.MTLSPermissive:
			return inboundPermissiveHTTPFilterChainMatchWithMxcOptions
		default:
			return inboundPlainTextHTTPFilterChainMatchOptions
		}
	case networking.ListenerProtocolAuto:
		switch settings.Mode {
		case model.MTLSStrict:
			return inboundStrictFilterChainMatchOptions
		case model.MTLSPermissive:
			return inboundPermissiveFilterChainMatchWithMxcOptions
		default:
			return inboundPlainTextFilterChainMatchOptions
		}
	default:
		switch settings.Mode {
		case model.MTLSStrict:
			return inboundStrictTCPFilterChainMatchOptions
		case model.MTLSPermissive:
			return inboundPermissiveTCPFilterChainMatchWithMxcOptions
		default:
			return inboundPlainTextTCPFilterChainMatchOptions
		}
	}
}

type fcOpts struct {
	matchOpts FilterChainMatchOptions
	fc        networking.FilterChain
}

func (opt fcOpts) populateFilterChain(mtls plugin.MTLSSettings, port uint32, matchingIP string) fcOpts {
	opt.fc.FilterChainMatch = &listener.FilterChainMatch{}
	opt.fc.FilterChainMatch.ApplicationProtocols = opt.matchOpts.ApplicationProtocols
	opt.fc.FilterChainMatch.TransportProtocol = opt.matchOpts.TransportProtocol
	if len(matchingIP) > 0 {
		opt.fc.FilterChainMatch.PrefixRanges = []*core.CidrRange{util.ConvertAddressToCidr(matchingIP)}
	}
	if port > 0 {
		opt.fc.FilterChainMatch.DestinationPort = &wrappers.UInt32Value{Value: port}
	}
	opt.fc.ListenerProtocol = opt.matchOpts.Protocol
	if opt.fc.ListenerProtocol == networking.ListenerProtocolHTTP {
		opt.fc.TLSContext = mtls.HTTP
	} else {
		opt.fc.TLSContext = mtls.TCP
	}
	return opt
}

func (opt FilterChainMatchOptions) ToTransportSocket(mtls plugin.MTLSSettings) *tls.DownstreamTlsContext {
	if !opt.TLS {
		return nil
	}
	if opt.Protocol == networking.ListenerProtocolHTTP {
		return mtls.HTTP
	}
	return mtls.TCP
}
