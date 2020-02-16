// Copyright 2020 Istio Authors
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
	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ldsv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	structpb "github.com/golang/protobuf/ptypes/struct"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/config/constants"
	protovalue "istio.io/istio/pkg/proto"
	"istio.io/pkg/log"
)

// BuildInboundFilterChain returns the filter chain(s) correspoinding to the mTLS mode.
func BuildInboundFilterChain(mTLSMode model.MutualTLSMode, sdsUdsPath string, node *model.Proxy) []plugin.FilterChain {
	if mTLSMode == model.MTLSDisable || mTLSMode == model.MTLSUnknown {
		return nil
	}

	meta := node.Metadata
	var alpnIstioMatch *ldsv2.FilterChainMatch
	var tls *auth.DownstreamTlsContext
	if util.IsTCPMetadataExchangeEnabled(node) {
		alpnIstioMatch = &ldsv2.FilterChainMatch{
			ApplicationProtocols: util.ALPNInMeshWithMxc,
		}
		tls = &auth.DownstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				// For TCP with mTLS, we advertise "istio-peer-exchange" from client and
				// expect the same from server. This  is so that secure metadata exchange
				// transfer can take place between sidecars for TCP with mTLS.
				AlpnProtocols: util.ALPNDownstream,
			},
			RequireClientCertificate: protovalue.BoolTrue,
		}
	} else {
		alpnIstioMatch = &ldsv2.FilterChainMatch{
			ApplicationProtocols: util.ALPNInMesh,
		}
		tls = &auth.DownstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
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
	}

	if !node.Metadata.SdsEnabled || sdsUdsPath == "" {
		base := meta.SdsBase + constants.AuthCertsPath
		tlsServerRootCert := model.GetOrDefault(meta.TLSServerRootCert, base+constants.RootCertFilename)

		tls.CommonTlsContext.ValidationContextType = authn_model.ConstructValidationContext(tlsServerRootCert, []string{} /*subjectAltNames*/)

		tlsServerCertChain := model.GetOrDefault(meta.TLSServerCertChain, base+constants.CertChainFilename)
		tlsServerKey := model.GetOrDefault(meta.TLSServerKey, base+constants.KeyFilename)

		tls.CommonTlsContext.TlsCertificates = []*auth.TlsCertificate{
			{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: tlsServerCertChain,
					},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: tlsServerKey,
					},
				},
			},
		}
	} else {
		tls.CommonTlsContext.TlsCertificateSdsSecretConfigs = []*auth.SdsSecretConfig{
			authn_model.ConstructSdsSecretConfig(authn_model.SDSDefaultResourceName, sdsUdsPath),
		}

		tls.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{VerifySubjectAltName: []string{} /*subjectAltNames*/},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(authn_model.SDSRootResourceName, sdsUdsPath),
			},
		}
	}
	if mTLSMode == model.MTLSStrict {
		log.Debug("Allow only istio mutual TLS traffic")
		return []plugin.FilterChain{
			{
				TLSContext: tls,
			}}
	}
	if mTLSMode == model.MTLSPermissive {
		log.Debug("Allow both, ALPN istio and legacy traffic")
		return []plugin.FilterChain{
			{
				FilterChainMatch: alpnIstioMatch,
				TLSContext:       tls,
				ListenerFilters: []*ldsv2.ListenerFilter{
					{
						Name:       xdsutil.TlsInspector,
						ConfigType: &ldsv2.ListenerFilter_Config{Config: &structpb.Struct{}},
					},
				},
			},
			{
				FilterChainMatch: &ldsv2.FilterChainMatch{},
			},
		}
	}
	return nil
}
