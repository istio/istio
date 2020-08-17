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

package utils

import (
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	protovalue "istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/spiffe"

	"istio.io/pkg/log"
)

const (
	// Service account for Pilot (hardcoded values at setup time)
	PilotSvcAccName string = "istio-pilot-service-account"
)

var (
	// These are sniffed by the HTTP Inspector in the outbound listener
	// We need to forward these ALPNs to upstream so that the upstream can
	// properly use a HTTP or TCP listener
	plaintextHTTPALPNs  = []string{"http/1.0", "http/1.1", "h2c"}
	mtlsHTTPALPNs       = []string{"istio-http/1.0", "istio-http/1.1", "istio-h2"}
	mtlsTCPWithMxcALPNs = []string{"istio-peer-exchange"}

	// ALPN used for TCP Metadata Exchange.
	tcpMxcALPN = "istio-peer-exchange"

	// Double the number of filter chains. Half of filter chains are used as http filter chain and half of them are used as tcp proxy
	// id in [0, len(allChains)/2) are configured as http filter chain, [(len(allChains)/2, len(allChains)) are configured as tcp proxy
	// If mTLS permissive is enabled, there are five filter chains. The filter chain match should be
	//  FCM 1: ALPN [istio-http/1.0, istio-http/1.1, istio-h2] Transport protocol: tls      --> HTTP traffic from remote sidecar over Istio mTLS
	//  FCM 2: ALPN [http/1.0, http/1.1, h2c] Transport protocol: N/A                       --> Sniffed as HTTP traffic by the HTTP Inspector (plain text)
	//  FCM 3: ALPN [istio-peer-exchange] Transport protocol: tls                           --> TCP traffic from remote sidecar over Istio mTLS
	//  FCM 4: ALPN [] Transport protocol: N/A                                              --> TCP traffic over plain text
	//  FCM 5: ALPN [] Transport protocol: tls                                              --> TCP traffic over TLS (no remote sidecar or remote forward app tls as is)
	// If traffic is over mTLS is strict mode, there are two filter chains. The filter chain match should be
	//  FCM 1: ALPN [istio-http/1.0, istio-http/1.1, istio-h2]                              --> HTTP traffic from remote sidecar over Istio mTLS
	//  FCM 2: ALPN [istio-peer-exchange]                                                   --> TCP traffic from remote sidecar over Istio mTLS

	// TODO: (@rshriram) Rather than having FCMs here, construct the entire network filter here in advance
	// and just copy the TLS context
	inboundPermissiveFCMWithSniffing = []networking.FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener, sent over mTLS
			ApplicationProtocols: mtlsHTTPALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          networking.ListenerProtocolHTTP,
		},
		{
			// client side traffic was detected as plaintext HTTP by the HTTP & TLS Inspector
			ApplicationProtocols: plaintextHTTPALPNs,
			// No transport protocol match as this filter chain (+match) will be used for plain text connections
			Protocol: networking.ListenerProtocolHTTP,
		},
		{
			// client side TCP traffic sent over mTLS
			ApplicationProtocols: mtlsTCPWithMxcALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          networking.ListenerProtocolTCP,
		},
		{
			// client side traffic is plaintext/standard TLS TCP as identified by TLS & HTTP Inspector
			Protocol: networking.ListenerProtocolTCP,
		},
	}

	inboundPermissiveFCMForHTTP = []networking.FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener, sent over mTLS
			ApplicationProtocols: mtlsHTTPALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          networking.ListenerProtocolHTTP,
		},
		{
			// No transport protocol match as this filter chain (+match) will be used for plain text connections
			Protocol: networking.ListenerProtocolHTTP,
		},
	}

	inboundPermissiveFCMForTCP = []networking.FilterChainMatchOptions{
		{
			// client side TCP traffic over mTLS
			ApplicationProtocols: mtlsTCPWithMxcALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          networking.ListenerProtocolTCP,
		},
		{
			// client side traffic is plaintext TCP as identified by TLS Inspector
			Protocol: networking.ListenerProtocolTCP,
		},
	}

	inboundStrictFCMForHTTP = networking.FilterChainMatchOptions{
		ApplicationProtocols: mtlsHTTPALPNs,
			TransportProtocol: "tls",
			Protocol:             networking.ListenerProtocolHTTP,
		}


	inboundStrictFCMForTCP = networking.FilterChainMatchOptions{
			ApplicationProtocols: mtlsTCPWithMxcALPNs,
			TransportProtocol: "tls",
			Protocol:             networking.ListenerProtocolTCP,
		}


	inboundStrictFCMWithSniffing = []networking.FilterChainMatchOptions{
		inboundStrictFCMForHTTP, inboundStrictFCMForTCP,
	}

	inboundPlainTextForHTTP = networking.FilterChainMatchOptions{
		Protocol: networking.ListenerProtocolHTTP,
	}
	inboundPlainTextForTCP = networking.FilterChainMatchOptions{
		Protocol: networking.ListenerProtocolTCP,
	}
	inboundPlainTextFCMWithSniffing = []networking.FilterChainMatchOptions{
		{
			// Client side traffic detected as plaintext HTTP by the HTTP Inspector
			ApplicationProtocols: plaintextHTTPALPNs,
			Protocol:             networking.ListenerProtocolHTTP,
		},
		{
			// Client side traffic detected as TCP (not sure if its TLS or not) by the HTTP Inspector.
			Protocol: networking.ListenerProtocolTCP,
		},
	}
)

// BuildInboundFilterChain returns the filter chain(s) corresponding to the mTLS mode.
func BuildInboundFilterChain(mTLSMode model.MutualTLSMode, sdsUdsPath string, node *model.Proxy,
	listenerProtocol networking.ListenerProtocol, trustDomainAliases []string) []networking.FilterChain {
	if mTLSMode == model.MTLSDisable || mTLSMode == model.MTLSUnknown {
		return nil
	}

	meta := node.Metadata
	var ctx *tls.DownstreamTlsContext
	// TLS context to use, if we are going to use it (in permissive/strict)
	// NOTE: We do not need to set ALPN protocols in the sidecar's downstream TLS context
	// because the client envoys do not support any form of protocol negotiation anyway
	// plus we know fully well who we are going to talk to and the protocol used by them
	// on a given port. If ALPN is missing, envoy on server side will simply accept all.
	// Irrespective of ALPN negotiation, we use filter chain matches to distinguish the
	// type of traffic and activate the appropriate decoders (http/tcp).
	ctx = &tls.DownstreamTlsContext{
		CommonTlsContext:         &tls.CommonTlsContext{},
		RequireClientCertificate: protovalue.BoolTrue,
	}
	authn_model.ApplyToCommonTLSContext(ctx.CommonTlsContext, meta, sdsUdsPath, []string{} /*subjectAltNames*/, trustDomainAliases)

	switch mTLSMode{
	case model.MTLSStrict:
		log.Debug("Allow only istio mutual TLS traffic")
		switch listenerProtocol {
		case networking.ListenerProtocolHTTP:
			return []networking.FilterChain{
				{
					TLSContext: ctx,
					FilterChainMatchOptions: inboundStrictFCMForHTTP,
				}}
		case networking.ListenerProtocolTCP, case networking.ListenerProtocolThrift:
			return []networking.FilterChain{
				{
					TLSContext: ctx,
					FilterChainMatchOptions: inboundStrictFCMForTCP,
				}}
		case networking.ListenerProtocolAuto:
			out := make([]networking.FilterChain, 0)
			for _, fcm := range inboundStrictFCMWithSniffing {
				out = append(out, networking.FilterChain{
					TLSContext: ctx,
					FilterChainMatchOptions: fcm,
				})
			}
			return out
		}
	case model.MTLSPermissive:
		log.Debug("Allow both, ALPN istio and legacy traffic")
		switch listenerProtocol {
		case networking.ListenerProtocolHTTP:
		case networking.ListenerProtocolTCP, networking.ListenerProtocolThrift:
		case networking.ListenerProtocolAuto:
		}
	default:
		log.Debug("Allow only legacy traffic")
		switch listenerProtocol {
		case networking.ListenerProtocolHTTP:
		case networking.ListenerProtocolTCP, networking.ListenerProtocolThrift:
		case networking.ListenerProtocolAuto:
		}
	}
	return nil
}

// GetSAN returns the SAN used for passed in identity for mTLS.
func GetSAN(ns string, identity string) string {

	if ns != "" {
		return spiffe.MustGenSpiffeURI(ns, identity)
	}
	return spiffe.GenCustomSpiffe(identity)
}
