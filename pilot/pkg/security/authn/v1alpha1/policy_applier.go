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

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/jwt_authn/v2alpha"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"

	authn_v1alpha1 "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	authn_utils "istio.io/istio/pilot/pkg/security/authn/utils"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	authn_filter_policy "istio.io/istio/security/proto/authentication/v1alpha1"
	authn_filter "istio.io/istio/security/proto/envoy/config/filter/http/authn/v2alpha1"
	istio_jwt "istio.io/istio/security/proto/envoy/config/filter/http/jwt_auth/v2alpha1"
	"istio.io/pkg/log"
)

const (
	// The default header name for an exchanged token.
	exchangedTokenHeaderName = "ingress-authorization"

	// The default header prefix for an exchanged token.
	exchangedTokenHeaderPrefix = "istio"
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
	ret := make([]*authn_v1alpha1.Jwt, 0)
	if policy == nil {
		return ret
	}
	for _, method := range policy.Peers {
		switch method.GetParams().(type) {
		case *authn_v1alpha1.PeerAuthenticationMethod_Jwt:
			// nolint: staticcheck
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

func convertToEnvoyJwtConfig(policyJwts []*authn_v1alpha1.Jwt) *envoy_jwt.JwtAuthentication {
	providers := map[string]*envoy_jwt.JwtProvider{}
	requirementOrList := []*envoy_jwt.JwtRequirement{}
	for i, policyJwt := range policyJwts {
		provider := &envoy_jwt.JwtProvider{
			Issuer:            policyJwt.Issuer,
			Audiences:         policyJwt.Audiences,
			Forward:           true,
			PayloadInMetadata: policyJwt.Issuer,
		}

		for _, location := range policyJwt.JwtHeaders {
			header := &envoy_jwt.JwtHeader{
				Name: location,
			}
			if location == exchangedTokenHeaderName {
				header.ValuePrefix = exchangedTokenHeaderPrefix
			}
			provider.FromHeaders = append(provider.FromHeaders, header)
		}
		provider.FromParams = policyJwt.JwtParams

		jwtPubKey := policyJwt.Jwks
		if jwtPubKey == "" {
			var err error
			jwtPubKey, err = model.JwtKeyResolver.GetPublicKey(policyJwt.JwksUri)
			if err != nil {
				log.Errorf("Failed to fetch jwt public key from %q: %s", policyJwt.JwksUri, err)
			}
		}
		provider.JwksSourceSpecifier = &envoy_jwt.JwtProvider_LocalJwks{
			LocalJwks: &core.DataSource{
				Specifier: &core.DataSource_InlineString{
					InlineString: jwtPubKey,
				},
			},
		}

		name := fmt.Sprintf("origins-%d", i)
		providers[name] = provider
		requirementOrList = append(requirementOrList, &envoy_jwt.JwtRequirement{
			RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
				ProviderName: name,
			},
		})
	}

	requirementOrList = append(requirementOrList, &envoy_jwt.JwtRequirement{
		RequiresType: &envoy_jwt.JwtRequirement_AllowMissingOrFailed{
			AllowMissingOrFailed: &empty.Empty{},
		},
	})

	return &envoy_jwt.JwtAuthentication{
		Rules: []*envoy_jwt.RequirementRule{
			{
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{
						Prefix: "/",
					},
				},
				Requires: &envoy_jwt.JwtRequirement{
					RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
						RequiresAny: &envoy_jwt.JwtRequirementOrList{
							Requirements: requirementOrList,
						},
					},
				},
			},
		},
		Providers: providers,
	}
}

// TODO: Remove after fully migrate to Envoy JWT filter.
func convertToIstioJwtConfig(policyJwts []*authn_v1alpha1.Jwt) *istio_jwt.JwtAuthentication {
	ret := &istio_jwt.JwtAuthentication{
		AllowMissingOrFailed: true,
	}
	for _, policyJwt := range policyJwts {
		jwt := &istio_jwt.JwtRule{
			Issuer:               policyJwt.Issuer,
			Audiences:            policyJwt.Audiences,
			ForwardPayloadHeader: outputLocationForJwtIssuer(policyJwt.Issuer),
			Forward:              true,
		}

		for _, location := range policyJwt.JwtHeaders {
			jwt.FromHeaders = append(jwt.FromHeaders, &istio_jwt.JwtHeader{
				Name: location,
			})
		}
		jwt.FromParams = policyJwt.JwtParams

		jwtPubKey := policyJwt.Jwks
		if jwtPubKey == "" {
			var err error
			jwtPubKey, err = model.JwtKeyResolver.GetPublicKey(policyJwt.JwksUri)
			if err != nil {
				log.Errorf("Failed to fetch jwt public key from %q: %s", policyJwt.JwksUri, err)
			}
		}

		// Put empty string in config even if above ResolveJwtPubKey fails.
		jwt.JwksSourceSpecifier = &istio_jwt.JwtRule_LocalJwks{
			LocalJwks: &istio_jwt.DataSource{
				Specifier: &istio_jwt.DataSource_InlineString{
					InlineString: jwtPubKey,
				},
			},
		}

		ret.Rules = append(ret.Rules, jwt)
	}
	return ret
}

// ConvertPolicyToJwtConfig converts policy into Jwt filter config for envoy.
// Returns nil if there is no JWT policy. Returns the Istio JWT filter config if USE_ENVOY_JWT_FILTER
// is false, otherwise returns the Envoy JWT filter config.
func convertPolicyToJwtConfig(policy *authn_v1alpha1.Policy) (string, proto.Message) {
	policyJwts := collectJwtSpecs(policy)
	if len(policyJwts) == 0 {
		return "", nil
	}

	if features.UseIstioJWTFilter.Get() {
		return authn_model.IstioJwtFilterName, convertToIstioJwtConfig(policyJwts)
	}

	log.Debugf("Envoy JWT filter is used for JWT verification")
	return authn_model.EnvoyJwtFilterName, convertToEnvoyJwtConfig(policyJwts)
}

// convertPolicyToAuthNFilterConfig returns an authn filter config corresponding for the input policy.
func convertPolicyToAuthNFilterConfig(policy *authn_v1alpha1.Policy, proxyType model.NodeType) *authn_filter.FilterConfig {
	if policy == nil || (len(policy.Peers) == 0 && len(policy.Origins) == 0) {
		return nil
	}

	// cloning proto from gogo to golang world
	bytes, _ := policy.Marshal()
	p := &authn_filter_policy.Policy{}
	if err := proto.Unmarshal(bytes, p); err != nil {
		return nil
	}

	// Create default mTLS params for params type mTLS but value is nil.
	// This walks around the issue https://github.com/istio/istio/issues/4763
	var usedPeers []*authn_filter_policy.PeerAuthenticationMethod
	for _, peer := range p.Peers {
		switch peer.GetParams().(type) {
		case *authn_filter_policy.PeerAuthenticationMethod_Mtls:
			// Only enable mTLS for sidecar, not Ingress/Router for now.
			if proxyType == model.SidecarProxy {
				if peer.GetMtls() == nil {
					peer.Params = &authn_filter_policy.PeerAuthenticationMethod_Mtls{Mtls: &authn_filter_policy.MutualTls{}}
				}
				usedPeers = append(usedPeers, peer)
			}
		case *authn_filter_policy.PeerAuthenticationMethod_Jwt:
			usedPeers = append(usedPeers, peer)
		}
	}

	p.Peers = usedPeers
	filterConfig := &authn_filter.FilterConfig{
		Policy: p,
		// we can always set this field, it's no-op if mTLS is not used.
		SkipValidateTrustDomain: features.SkipValidateTrustDomain.Get(),
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

func (a v1alpha1PolicyApplier) JwtFilter() *http_conn.HttpFilter {
	// v2 api will use inline public key.
	filterName, filterConfigProto := convertPolicyToJwtConfig(a.policy)
	if filterConfigProto == nil {
		return nil
	}
	out := &http_conn.HttpFilter{
		Name:       filterName,
		ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)},
	}

	return out
}

func (a v1alpha1PolicyApplier) AuthNFilter(proxyType model.NodeType) *http_conn.HttpFilter {
	filterConfigProto := convertPolicyToAuthNFilterConfig(a.policy, proxyType)
	if filterConfigProto == nil {
		return nil
	}
	out := &http_conn.HttpFilter{
		Name:       authn_model.AuthnFilterName,
		ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)},
	}
	return out
}

// v1alpha1 applier is already per port, so the endpointPort param is not needed.
func (a v1alpha1PolicyApplier) InboundFilterChain(_ uint32, sdsUdsPath string, node *model.Proxy) []plugin.FilterChain {
	if a.policy == nil || len(a.policy.Peers) == 0 {
		return nil
	}
	return authn_utils.BuildInboundFilterChain(GetMutualTLSMode(a.policy), sdsUdsPath, node)
}

// NewPolicyApplier returns new applier for v1alpha1 authentication policy.
func NewPolicyApplier(policy *authn_v1alpha1.Policy) authn.PolicyApplier {
	return &v1alpha1PolicyApplier{
		policy: policy,
	}
}
