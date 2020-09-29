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

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	protovalue "istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/spiffe"
)

const (
	// Service accounts for Mixer and Pilot, these are hardcoded values at setup time
	PilotSvcAccName string = "istio-pilot-service-account"

	MixerSvcAccName string = "istio-mixer-service-account"
)

var (
	// SupportedCiphers for server side TLS configuration.
	SupportedCiphers = []string{
		"ECDHE-ECDSA-AES256-GCM-SHA384",
		"ECDHE-RSA-AES256-GCM-SHA384",
		"ECDHE-ECDSA-AES128-GCM-SHA256",
		"ECDHE-RSA-AES128-GCM-SHA256",
		"AES256-GCM-SHA384",
		"AES128-GCM-SHA256",
	}
)

// BuildInboundFilterChain returns the filter chain(s) corresponding to the mTLS mode.
func BuildInboundFilterChain(mTLSMode model.MutualTLSMode, sdsUdsPath string, node *model.Proxy,
	listenerProtocol networking.ListenerProtocol, trustDomainAliases []string) []networking.FilterChain {
	if mTLSMode == model.MTLSDisable || mTLSMode == model.MTLSUnknown {
		return nil
	}

	meta := node.Metadata
	var alpnIstioMatch *listener.FilterChainMatch
	var ctx *tls.DownstreamTlsContext
	if features.EnableTCPMetadataExchange &&
		(listenerProtocol == networking.ListenerProtocolTCP || listenerProtocol == networking.ListenerProtocolAuto) {
		alpnIstioMatch = &listener.FilterChainMatch{
			ApplicationProtocols: util.ALPNInMeshWithMxc,
		}
		ctx = &tls.DownstreamTlsContext{
			CommonTlsContext: &tls.CommonTlsContext{
				// For TCP with mTLS, we advertise "istio-peer-exchange" from client and
				// expect the same from server. This  is so that secure metadata exchange
				// transfer can take place between sidecars for TCP with mTLS.
				AlpnProtocols: util.ALPNDownstream,
			},
			RequireClientCertificate: protovalue.BoolTrue,
		}
	} else {
		alpnIstioMatch = &listener.FilterChainMatch{
			ApplicationProtocols: util.ALPNInMesh,
		}
		ctx = &tls.DownstreamTlsContext{
			CommonTlsContext: &tls.CommonTlsContext{
				// Note that in the PERMISSIVE mode, we match filter chain on "istio" ALPN,
				// which is used to differentiate between service mesh and legacy traffic.
				//
				// Client sidecar outbound cluster's TLSContext.ALPN must include "istio".
				//
				// Server sidecar filter chain's FilterChainMatch.ApplicationProtocols must
				// include "istio" for the secure traffic, but its TLSContext.ALPN must not
				// include "istio", which would interfere with negotiation of the underlying
				// protocol, e.g. HTTP/2.
				AlpnProtocols: util.ALPNHttp,
			},
			RequireClientCertificate: protovalue.BoolTrue,
		}

		if features.EnableTLSv2OnInboundPath {
			// Set Minimum TLS version to match the default client version and allowed strong cipher suites for sidecars.
			ctx.CommonTlsContext.TlsParams = &tls.TlsParameters{
				TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
				CipherSuites:              SupportedCiphers,
			}
		}
	}
	authn_model.ApplyToCommonTLSContext(ctx.CommonTlsContext, meta, sdsUdsPath, []string{} /*subjectAltNames*/, node.RequestedTypes.LDS, trustDomainAliases)

	if mTLSMode == model.MTLSStrict {
		log.Debug("Allow only istio mutual TLS traffic")
		return []networking.FilterChain{
			{
				TLSContext: ctx,
			}}
	}
	if mTLSMode == model.MTLSPermissive {
		log.Debug("Allow both, ALPN istio and legacy traffic")
		return []networking.FilterChain{
			{
				FilterChainMatch: alpnIstioMatch,
				TLSContext:       ctx,
				ListenerFilters: []*listener.ListenerFilter{
					xdsfilters.TLSInspector,
				},
			},
			{
				FilterChainMatch: &listener.FilterChainMatch{},
			},
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
