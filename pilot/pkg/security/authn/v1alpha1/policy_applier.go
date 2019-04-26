// Copyright 2019 Istio Authors
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

package v1alpha1

import (
	"crypto/sha1"
	"fmt"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ldsv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	authn_v1alpha1 "istio.io/api/authentication/v1alpha1"
	authn_filter "istio.io/api/envoy/config/filter/http/authn/v2alpha1"
	jwtfilter "istio.io/api/envoy/config/filter/http/jwt_auth/v2alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pkg/features/pilot"
	"istio.io/istio/pkg/log"
	protovalue "istio.io/istio/pkg/proto"
)

const (
	// JwtFilterName is the name for the Jwt filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/jwt_auth/http_filter_factory.cc#L50
	JwtFilterName = "jwt-auth"

	// AuthnFilterName is the name for the Istio AuthN filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/authn/http_filter_factory.cc#L30
	AuthnFilterName = "istio_authn"

	// EnvoyTLSInspectorFilterName is the name for Envoy TLS sniffing listener filter.
	EnvoyTLSInspectorFilterName = "envoy.listener.tls_inspector"
)

// GetMutualTLS returns pointer to mTLS params if the policy use mTLS for (peer) authentication.
// (note that mTLS params can still be nil). Otherwise, return (false, nil).
// Callers should ensure the proxy is of sidecar type.
func GetMutualTLS(policy *authn_v1alpha1.Policy) *authn_v1alpha1.MutualTls {
	if policy == nil {
		return nil
	}
	if len(policy.Peers) > 0 {
		for _, method := range policy.Peers {
			switch method.GetParams().(type) {
			case *authn_v1alpha1.PeerAuthenticationMethod_Mtls:
				if method.GetMtls() == nil {
					return &authn_v1alpha1.MutualTls{Mode: authn_v1alpha1.MutualTls_STRICT}
				}
				return method.GetMtls()
			default:
				continue
			}
		}
	}
	return nil
}

// collectJwtSpecs returns a list of all JWT specs (pointers) defined the policy. This
// provides a convenient way to iterate all Jwt specs.
func collectJwtSpecs(policy *authn_v1alpha1.Policy) []*authn_v1alpha1.Jwt {
	ret := []*authn_v1alpha1.Jwt{}
	if policy == nil {
		return ret
	}
	for _, method := range policy.Peers {
		switch method.GetParams().(type) {
		case *authn_v1alpha1.PeerAuthenticationMethod_Jwt:
			ret = append(ret, method.GetJwt())
		}
	}
	for _, method := range policy.Origins {
		ret = append(ret, method.Jwt)
	}
	return ret
}

// OutputLocationForJwtIssuer returns the header location that should be used to output payload if
// authentication succeeds.
func outputLocationForJwtIssuer(issuer string) string {
	const locationPrefix = "istio-sec-"
	sum := sha1.Sum([]byte(issuer))
	return locationPrefix + fmt.Sprintf("%x", sum)
}

// ConvertPolicyToJwtConfig converts policy into Jwt filter config for envoy.
func convertPolicyToJwtConfig(policy *authn_v1alpha1.Policy) *jwtfilter.JwtAuthentication {
	policyJwts := collectJwtSpecs(policy)
	if len(policyJwts) == 0 {
		return nil
	}
	ret := &jwtfilter.JwtAuthentication{
		AllowMissingOrFailed: true,
	}
	for _, policyJwt := range policyJwts {
		jwt := &jwtfilter.JwtRule{
			Issuer:               policyJwt.Issuer,
			Audiences:            policyJwt.Audiences,
			ForwardPayloadHeader: outputLocationForJwtIssuer(policyJwt.Issuer),
			Forward:              true,
		}

		for _, location := range policyJwt.JwtHeaders {
			jwt.FromHeaders = append(jwt.FromHeaders, &jwtfilter.JwtHeader{
				Name: location,
			})
		}
		jwt.FromParams = policyJwt.JwtParams

		jwtPubKey, err := model.JwtKeyResolver.GetPublicKey(policyJwt.JwksUri)
		if err != nil {
			log.Warnf("Failed to fetch jwt public key from %q", policyJwt.JwksUri)
		}

		// Put empty string in config even if above ResolveJwtPubKey fails.
		jwt.JwksSourceSpecifier = &jwtfilter.JwtRule_LocalJwks{
			LocalJwks: &jwtfilter.DataSource{
				Specifier: &jwtfilter.DataSource_InlineString{
					InlineString: jwtPubKey,
				},
			},
		}

		ret.Rules = append(ret.Rules, jwt)
	}
	return ret
}

// convertPolicyToAuthNFilterConfig returns an authn filter config corresponding for the input policy.
func convertPolicyToAuthNFilterConfig(policy *authn_v1alpha1.Policy, proxyType model.NodeType) *authn_filter.FilterConfig {
	if policy == nil || (len(policy.Peers) == 0 && len(policy.Origins) == 0) {
		return nil
	}

	p := proto.Clone(policy).(*authn_v1alpha1.Policy)
	// Create default mTLS params for params type mTLS but value is nil.
	// This walks around the issue https://github.com/istio/istio/issues/4763
	var usedPeers []*authn_v1alpha1.PeerAuthenticationMethod
	for _, peer := range p.Peers {
		switch peer.GetParams().(type) {
		case *authn_v1alpha1.PeerAuthenticationMethod_Mtls:
			// Only enable mTLS for sidecar, not Ingress/Router for now.
			if proxyType == model.SidecarProxy {
				if peer.GetMtls() == nil {
					peer.Params = &authn_v1alpha1.PeerAuthenticationMethod_Mtls{Mtls: &authn_v1alpha1.MutualTls{}}
				}
				usedPeers = append(usedPeers, peer)
			}
		case *authn_v1alpha1.PeerAuthenticationMethod_Jwt:
			usedPeers = append(usedPeers, peer)
		}
	}

	p.Peers = usedPeers
	filterConfig := &authn_filter.FilterConfig{
		Policy: p,
	}

	// Remove targets part.
	filterConfig.Policy.Targets = nil
	locations := make(map[string]string)
	for _, jwt := range collectJwtSpecs(policy) {
		locations[jwt.Issuer] = outputLocationForJwtIssuer(jwt.Issuer)
	}
	if len(locations) > 0 {
		filterConfig.JwtOutputPayloadLocations = locations
	}

	if len(filterConfig.Policy.Peers) == 0 && len(filterConfig.Policy.Origins) == 0 {
		return nil
	}

	return filterConfig
}

// Implemenation of authn.PolicyApplier
type v1alpha1PolicyApplier struct {
	policy *authn_v1alpha1.Policy
}

func (a v1alpha1PolicyApplier) JwtFilter(isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
	// v2 api will use inline public key.
	filterConfigProto := convertPolicyToJwtConfig(a.policy)
	if filterConfigProto == nil {
		return nil
	}
	out := &http_conn.HttpFilter{
		Name: JwtFilterName,
	}
	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)}
	} else {
		out.ConfigType = &http_conn.HttpFilter_Config{Config: util.MessageToStruct(filterConfigProto)}
	}
	return out
}

func (a v1alpha1PolicyApplier) AuthNFilter(proxyType model.NodeType, isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
	filterConfigProto := convertPolicyToAuthNFilterConfig(a.policy, proxyType)
	if filterConfigProto == nil {
		return nil
	}
	out := &http_conn.HttpFilter{
		Name: AuthnFilterName,
	}
	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)}
	} else {
		out.ConfigType = &http_conn.HttpFilter_Config{Config: util.MessageToStruct(filterConfigProto)}
	}
	return out
}

func (a v1alpha1PolicyApplier) InboundFilterChain(sdsUdsPath string, sdsUseTrustworthyJwt, sdsUseNormalJwt bool, meta map[string]string) []plugin.FilterChain {
	if a.policy == nil || len(a.policy.Peers) == 0 {
		return nil
	}
	alpnIstioMatch := &ldsv2.FilterChainMatch{
		ApplicationProtocols: util.ALPNInMesh,
	}
	tls := &auth.DownstreamTlsContext{
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
	if sdsUdsPath == "" {
		base := meta[pilot.BaseDir] + model.AuthCertsPath
		tlsServerRootCert := model.GetOrDefaultFromMap(meta, model.NodeMetadataTLSServerRootCert, base+model.RootCertFilename)

		tls.CommonTlsContext.ValidationContextType = model.ConstructValidationContext(tlsServerRootCert, []string{} /*subjectAltNames*/)

		tlsServerCertChain := model.GetOrDefaultFromMap(meta, model.NodeMetadataTLSServerCertChain, base+model.CertChainFilename)
		tlsServerKey := model.GetOrDefaultFromMap(meta, model.NodeMetadataTLSServerKey, base+model.KeyFilename)

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
			model.ConstructSdsSecretConfig(model.SDSDefaultResourceName, sdsUdsPath, sdsUseTrustworthyJwt, sdsUseNormalJwt, meta),
		}

		tls.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{VerifySubjectAltName: []string{} /*subjectAltNames*/},
				ValidationContextSdsSecretConfig: model.ConstructSdsSecretConfig(model.SDSRootResourceName, sdsUdsPath, sdsUseTrustworthyJwt, sdsUseNormalJwt, meta),
			},
		}
	}
	mtls := GetMutualTLS(a.policy)
	if mtls == nil {
		return nil
	}
	if mtls.GetMode() == authn_v1alpha1.MutualTls_STRICT {
		log.Debug("Allow only istio mutual TLS traffic")
		return []plugin.FilterChain{
			{
				TLSContext: tls,
			}}
	}
	if mtls.GetMode() == authn_v1alpha1.MutualTls_PERMISSIVE {
		log.Debug("Allow both, ALPN istio and legacy traffic")
		return []plugin.FilterChain{
			{
				FilterChainMatch: alpnIstioMatch,
				TLSContext:       tls,
				ListenerFilters: []ldsv2.ListenerFilter{
					{
						Name:       EnvoyTLSInspectorFilterName,
						ConfigType: &ldsv2.ListenerFilter_Config{Config: &types.Struct{}},
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

// NewPolicyApplier returns new applier for v1alpha1 authentication policy.
func NewPolicyApplier(policy *authn_v1alpha1.Policy) authn.PolicyApplier {
	return &v1alpha1PolicyApplier{
		policy: policy,
	}
}
