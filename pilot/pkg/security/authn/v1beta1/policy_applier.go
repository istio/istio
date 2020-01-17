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

package v1beta1

import (
	"fmt"
	"sort"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/jwt_authn/v2alpha"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/golang/protobuf/ptypes/empty"

	authn_alpha_api "istio.io/api/authentication/v1alpha1"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	alpha_applier "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	authn_alpha "istio.io/istio/security/proto/authentication/v1alpha1"
	authn_filter "istio.io/istio/security/proto/envoy/config/filter/http/authn/v2alpha1"
	"istio.io/pkg/log"
)

// Implemenation of authn.PolicyApplier with v1beta1 API.
type v1beta1PolicyApplier struct {
	jwtPolicies []*model.Config
	// TODO: add mTLS configs.

	// processedJwtRules is the consolidate JWT rules from all jwtPolicies.
	processedJwtRules []*v1beta1.JWTRule

	alphaApplier authn.PolicyApplier
}

func (a *v1beta1PolicyApplier) JwtFilter(isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
	if len(a.processedJwtRules) == 0 {
		log.Debugf("JwtFilter: RequestAuthentication (beta policy) not found, fallback to alpha if available")
		return a.alphaApplier.JwtFilter(isXDSMarshalingToAnyEnabled)
	}

	filterConfigProto := convertToEnvoyJwtConfig(a.processedJwtRules)

	if filterConfigProto == nil {
		return nil
	}
	out := &http_conn.HttpFilter{
		Name: authn_model.EnvoyJwtFilterName,
	}
	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)}
	} else {
		out.ConfigType = &http_conn.HttpFilter_Config{Config: util.MessageToStruct(filterConfigProto)}
	}
	return out
}

// All explaining code link can be removed before merging.
func convertToIstioAuthnFilterConfig(jwtRules []*v1beta1.JWTRule) *authn_filter.FilterConfig {
	p := authn_alpha.Policy{
		// Targets are not used in the authn filter.
		// Origin will be optional since we don't reject req.
		// Add Peers since we need that to trigger identity extraction in Authn filter.
		Peers: []*authn_alpha.PeerAuthenticationMethod{
			{
				Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
					Mtls: &authn_alpha.MutualTls{},
				},
			},
		},
		OriginIsOptional: true,
		PeerIsOptional:   true,
		// Always bind request.auth.principal from JWT origin. In v2 policy, authorization config specifies what principal to
		// choose from instead, rather than in authn config.
		PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
	}
	for _, jwt := range jwtRules {
		p.Origins = append(p.Origins, &authn_alpha.OriginAuthenticationMethod{
			Jwt: &authn_alpha.Jwt{
				// used for getting the filter data, and all other fields are irrelevant.
				Issuer: jwt.GetIssuer(),
			},
		})
	}

	return &authn_filter.FilterConfig{
		Policy: &p,
		// JwtOutputPayloadLocations is nil because now authn filter uses the issuer use the issuer as
		// key in the jwt filter metadata to find the output.
		SkipValidateTrustDomain: features.SkipValidateTrustDomain.Get(),
	}
}

// AuthNFilter returns the Istio authn filter config for a given authn Beta policy:
// istio.authentication.v1alpha1.Policy policy, we specially constructs the old filter config to
// ensure Authn Filter won't reject the request, but still transform the attributes, e.g. request.auth.principal.
// proxyType does not matter here, exists only for legacy reason.
func (a *v1beta1PolicyApplier) AuthNFilter(proxyType model.NodeType, isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
	if len(a.processedJwtRules) == 0 {
		log.Debugf("AuthnFilter: RequestAuthentication (beta policy) not found, fallback to alpha if available")
		return a.alphaApplier.AuthNFilter(proxyType, isXDSMarshalingToAnyEnabled)
	}
	out := &http_conn.HttpFilter{
		Name: authn_model.AuthnFilterName,
	}
	filterConfigProto := convertToIstioAuthnFilterConfig(a.processedJwtRules)
	if filterConfigProto == nil {
		return nil
	}
	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)}
	} else {
		out.ConfigType = &http_conn.HttpFilter_Config{Config: util.MessageToStruct(filterConfigProto)}
	}
	return out
}

func (a *v1beta1PolicyApplier) InboundFilterChain(sdsUdsPath string, meta *model.NodeMetadata) []plugin.FilterChain {
	return a.alphaApplier.InboundFilterChain(sdsUdsPath, meta)
}

// NewPolicyApplier returns new applier for v1beta1 authentication policies.
func NewPolicyApplier(jwtPolicies []*model.Config, policy *authn_alpha_api.Policy) authn.PolicyApplier {
	processedJwtRules := []*v1beta1.JWTRule{}

	// TODO(diemtvu) should we need to deduplicate JWT with the same issuer.
	// https://github.com/istio/istio/issues/19245
	for idx := range jwtPolicies {
		spec := jwtPolicies[idx].Spec.(*v1beta1.RequestAuthentication)
		processedJwtRules = append(processedJwtRules, spec.JwtRules...)
	}

	// Sort the jwt rules by the issuer alphabetically to make the later-on generated filter
	// config deteministic.
	sort.Slice(processedJwtRules, func(i, j int) bool {
		return strings.Compare(
			processedJwtRules[i].GetIssuer(), processedJwtRules[j].GetIssuer()) < 0
	})

	return &v1beta1PolicyApplier{
		jwtPolicies:       jwtPolicies,
		processedJwtRules: processedJwtRules,
		alphaApplier:      alpha_applier.NewPolicyApplier(policy),
	}
}

// convertToEnvoyJwtConfig converts a list of JWT rules into Envoy JWT filter config to enforce it.
// Each rule is expected corresponding to one JWT issuer (provider).
// The behavior of the filter should reject all requests with invalid token. On the other hand,
// if no token provided, the request is allowed.
func convertToEnvoyJwtConfig(jwtRules []*v1beta1.JWTRule) *envoy_jwt.JwtAuthentication {
	if len(jwtRules) == 0 {
		return nil
	}

	providers := map[string]*envoy_jwt.JwtProvider{}
	// Each element of innerAndList is the requirement for each provider, in the form of
	// {provider OR `allow_missing`}
	// This list will be ANDed (if have more than one provider) for the final requirement.
	innerAndList := []*envoy_jwt.JwtRequirement{}

	// This is an (or) list for all providers. This will be OR with the innerAndList above so
	// it can pass the requirement in the case that providers share the same location.
	outterOrList := []*envoy_jwt.JwtRequirement{}

	for i, jwtRule := range jwtRules {
		provider := &envoy_jwt.JwtProvider{
			Issuer:               jwtRule.Issuer,
			Audiences:            jwtRule.Audiences,
			Forward:              jwtRule.ForwardOriginalToken,
			ForwardPayloadHeader: jwtRule.OutputPayloadToHeader,
			PayloadInMetadata:    jwtRule.Issuer,
		}

		for _, location := range jwtRule.FromHeaders {
			provider.FromHeaders = append(provider.FromHeaders, &envoy_jwt.JwtHeader{
				Name:        location.Name,
				ValuePrefix: location.Prefix,
			})
		}
		provider.FromParams = jwtRule.FromParams

		jwtPubKey := jwtRule.Jwks
		if jwtPubKey == "" {
			var err error
			jwtPubKey, err = model.JwtKeyResolver.GetPublicKey(jwtRule.JwksUri)
			if err != nil {
				log.Errorf("Failed to fetch jwt public key from %q: %s", jwtRule.JwksUri, err)
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
		innerAndList = append(innerAndList, &envoy_jwt.JwtRequirement{
			RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
				RequiresAny: &envoy_jwt.JwtRequirementOrList{
					Requirements: []*envoy_jwt.JwtRequirement{
						{
							RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
								ProviderName: name,
							},
						},
						{
							RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
								AllowMissing: &empty.Empty{},
							},
						},
					},
				},
			},
		})
		outterOrList = append(outterOrList, &envoy_jwt.JwtRequirement{
			RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
				ProviderName: name,
			},
		})
	}

	// If there is only one provider, simply use an OR of {provider, `allow_missing`}.
	if len(innerAndList) == 1 {
		return &envoy_jwt.JwtAuthentication{
			Rules: []*envoy_jwt.RequirementRule{
				{
					Match: &route.RouteMatch{
						PathSpecifier: &route.RouteMatch_Prefix{
							Prefix: "/",
						},
					},
					Requires: innerAndList[0],
				},
			},
			Providers: providers,
		}
	}

	// If there are more than one provider, filter should OR of
	// {P1, P2 .., AND of {OR{P1, allow_missing}, OR{P2, allow_missing} ...}}
	// where the innerAnd enforce a token, if provided, must be valid, and the
	// outter OR aids the case where providers share the same location (as
	// it will always fail with the innerAND).
	outterOrList = append(outterOrList, &envoy_jwt.JwtRequirement{
		RequiresType: &envoy_jwt.JwtRequirement_RequiresAll{
			RequiresAll: &envoy_jwt.JwtRequirementAndList{
				Requirements: innerAndList,
			},
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
							Requirements: outterOrList,
						},
					},
				},
			},
		},
		Providers: providers,
	}
}
