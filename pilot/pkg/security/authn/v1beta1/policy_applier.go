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

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/jwt_authn/v2alpha"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/golang/protobuf/ptypes/empty"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/pkg/log"
)

// Implemenation of authn.PolicyApplier with v1beta1 API.
type v1beta1PolicyApplier struct {
	jwtPolicies []*model.Config
	// TODO: add mTLS configs.
	// TODO: add v1alpha1 fallback configs.

	// processedJwtRules is the consolidate JWT rules from all jwtPolicies.
	processedJwtRules []*v1beta1.JWT
}

func (a *v1beta1PolicyApplier) JwtFilter(isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
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

func (a *v1beta1PolicyApplier) AuthNFilter(proxyType model.NodeType, isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
	// TODO(diemtvu) implement this.
	log.Errorf("AuthNFilter(%v, %v) is not yet implemented", proxyType, isXDSMarshalingToAnyEnabled)
	return nil
}

func (a *v1beta1PolicyApplier) InboundFilterChain(sdsUdsPath string, meta *model.NodeMetadata) []plugin.FilterChain {
	// TODO(diemtvu) implement this.
	log.Errorf("InboundFilterChain is not yet implemented")
	return nil
}

// NewPolicyApplier returns new applier for v1beta1 authentication policies.
func NewPolicyApplier(jwtPolicies []*model.Config) authn.PolicyApplier {
	processedJwtRules := []*v1beta1.JWT{}

	// TODO(diemtvu) should we need to deduplicate JWT with the same issuer.
	// https://github.com/istio/istio/issues/19245
	for idx := range jwtPolicies {
		spec := jwtPolicies[idx].Spec.(*v1beta1.RequestAuthentication)
		processedJwtRules = append(processedJwtRules, spec.JwtRules...)
	}

	return &v1beta1PolicyApplier{
		jwtPolicies:       jwtPolicies,
		processedJwtRules: processedJwtRules,
	}
}

func convertToEnvoyJwtConfig(jwtRules []*v1beta1.JWT) *envoy_jwt.JwtAuthentication {
	if len(jwtRules) == 0 {
		return nil
	}
	providers := map[string]*envoy_jwt.JwtProvider{}
	for i, jwtRule := range jwtRules {
		// TODO(diemtvu): set forward based on input spec after https://github.com/istio/api/pull/1172
		provider := &envoy_jwt.JwtProvider{
			Issuer:            jwtRule.Issuer,
			Audiences:         jwtRule.Audiences,
			Forward:           false,
			PayloadInMetadata: jwtRule.Issuer,
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
	}

	return &envoy_jwt.JwtAuthentication{
		Rules: []*envoy_jwt.RequirementRule{
			{
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{
						Prefix: "/",
					},
				},
				Requires: &envoy_jwt.JwtRequirement{
					// TODO(diemvu): change to AllowMissing requirement.
					RequiresType: &envoy_jwt.JwtRequirement_AllowMissingOrFailed{
						AllowMissingOrFailed: &empty.Empty{},
					},
				},
			},
		},
		Providers: providers,
	}
}
