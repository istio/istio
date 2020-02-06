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
	authn_utils "istio.io/istio/pilot/pkg/security/authn/utils"
	alpha_applier "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	authn_alpha "istio.io/istio/security/proto/authentication/v1alpha1"
	authn_filter "istio.io/istio/security/proto/envoy/config/filter/http/authn/v2alpha1"
	"istio.io/pkg/log"
	istiolog "istio.io/pkg/log"
)

var (
	authnLog = istiolog.RegisterScope("authn", "authn debugging", 0)
)

// Implemenation of authn.PolicyApplier with v1beta1 API.
type v1beta1PolicyApplier struct {
	jwtPolicies []*model.Config

	peerPolices []*model.Config

	// processedJwtRules is the consolidate JWT rules from all jwtPolicies.
	processedJwtRules []*v1beta1.JWTRule

	consolidatedPeerPolicy *v1beta1.PeerAuthentication

	hasAlphaMTLSPolicy bool
	alphaApplier       authn.PolicyApplier
}

func (a *v1beta1PolicyApplier) JwtFilter() *http_conn.HttpFilter {
	if len(a.processedJwtRules) == 0 {
		authnLog.Debug("JwtFilter: RequestAuthentication (beta policy) not found, fallback to alpha if available")
		return a.alphaApplier.JwtFilter()
	}

	filterConfigProto := convertToEnvoyJwtConfig(a.processedJwtRules)

	if filterConfigProto == nil {
		return nil
	}
	return &http_conn.HttpFilter{
		Name:       authn_model.EnvoyJwtFilterName,
		ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)},
	}
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
func (a *v1beta1PolicyApplier) AuthNFilter(proxyType model.NodeType) *http_conn.HttpFilter {
	if len(a.processedJwtRules) == 0 && a.consolidatedPeerPolicy == nil {
		authnLog.Debug("AuthnFilter: RequestAuthentication nor PeerAuthentication (beta policy) not found, fallback to alpha if available")
		return a.alphaApplier.AuthNFilter(proxyType)
	}
	filterConfigProto := convertToIstioAuthnFilterConfig(a.processedJwtRules)
	if filterConfigProto == nil {
		return nil
	}

	return &http_conn.HttpFilter{
		Name:       authn_model.AuthnFilterName,
		ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(filterConfigProto)},
	}
}

func (a *v1beta1PolicyApplier) InboundFilterChain(sdsUdsPath string, node *model.Proxy) []plugin.FilterChain {
	// If beta mTLS policy (PeerAuthentication) is not used for this workload, fallback to alpha policy.
	if a.consolidatedPeerPolicy == nil && a.hasAlphaMTLSPolicy {
		authnLog.Debug("InboundFilterChain: fallback to alpha policy applier")
		return a.alphaApplier.InboundFilterChain(sdsUdsPath, node)
	}
	effectiveMTLSMode := model.MTLSPermissive
	if a.consolidatedPeerPolicy != nil {
		effectiveMTLSMode = getMutualTLSMode(a.consolidatedPeerPolicy.Mtls)
	}
	authnLog.Debugf("InboundFilterChain: build inbound filter change for %v in %s mode", node.ID, effectiveMTLSMode)
	return authn_utils.BuildInboundFilterChain(effectiveMTLSMode, sdsUdsPath, node)
}

// NewPolicyApplier returns new applier for v1beta1 authentication policies.
func NewPolicyApplier(rootNamespace string,
	jwtPolicies []*model.Config,
	peerPolicies []*model.Config,
	policy *authn_alpha_api.Policy) authn.PolicyApplier {
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
		jwtPolicies:            jwtPolicies,
		peerPolices:            peerPolicies,
		processedJwtRules:      processedJwtRules,
		consolidatedPeerPolicy: composePeerAuthentication(rootNamespace, peerPolicies),
		alphaApplier:           alpha_applier.NewPolicyApplier(policy),
		hasAlphaMTLSPolicy:     alpha_applier.GetMutualTLS(policy) != nil,
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

// getMutualTLSMode returns the MutualTLSMode enum corresponding peer MutualTLS settings.
// Input cannot be nil.
func getMutualTLSMode(mtls *v1beta1.PeerAuthentication_MutualTLS) model.MutualTLSMode {
	switch mtls.Mode {
	case v1beta1.PeerAuthentication_MutualTLS_DISABLE:
		return model.MTLSDisable
	case v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE:
		return model.MTLSPermissive
	case v1beta1.PeerAuthentication_MutualTLS_STRICT:
		return model.MTLSStrict
	default:
		return model.MTLSUnknown
	}
}

// composePeerAuthentication returns the effective PeerAuthentication given the list of applicable
// configs. This list should contains at most 1 mesh-level and 1 namespace-level configs.
// Workload-level configs should not be in root namespace (this should be guaranteed by the caller,
// though they will be safely ignored in this function). If the input config list is empty, returns
// nil which can be used to indicate no applicable (beta) policy exist in order to trigger fallback
// to alpha policy. This can be simplified once we deprecate alpha policy.
// If there is at least one applicable config, returns should be not nil, and is a combined policy
// based on following rules:
// - It should have the setting from the most narrow scope (i.e workload-level is  preferred over
// namespace-level, which is preferred over mesh-level).
// - When there are more than one policy in the same scope (i.e workload-level), the one with
// stronger security (STRICT > PERMISSIVE > DISABLE) is preferred.
// - In a tie, the first in the list will be used (typically, the list is passed in sorted order by
// timestamp, so it's more or less the latest).
// - Port-level setting will be combined in similar manner: if there are more than 1 policy define
// port-level mTLS for the same port, the stronger one is used.
// - UNSET will be replaced with the setting from the parrent. I.e UNSET port-level config will be
// replaced with config from workload-level, UNSET in workload-level config will be replaced with
// one in namespace-level and so on.
func composePeerAuthentication(rootNamespace string, configs []*model.Config) *v1beta1.PeerAuthentication {
	var meshPolicy, namespacePolicy *v1beta1.PeerAuthentication

	// Track number of workload level policies for early return.
	numWorkloadLevelPolicies := 0

	// First round to find mesh and namespace level policy.
	for _, cfg := range configs {
		spec := cfg.Spec.(*v1beta1.PeerAuthentication)
		if cfg.Namespace == rootNamespace && spec.Selector == nil {
			meshPolicy = spec
		} else if spec.Selector == nil {
			namespacePolicy = spec
		} else if cfg.Namespace != rootNamespace {
			numWorkloadLevelPolicies++
		}
	}

	if meshPolicy == nil && namespacePolicy == nil && numWorkloadLevelPolicies == 0 {
		return nil
	}

	// Initial parentPolicy is set to a PERMISSIVE.
	parentPolicy := v1beta1.PeerAuthentication{
		Mtls: &v1beta1.PeerAuthentication_MutualTLS{
			Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
		},
	}

	if meshPolicy != nil && meshPolicy.Mtls != nil && meshPolicy.Mtls.Mode != v1beta1.PeerAuthentication_MutualTLS_UNSET {
		// If mesh policy is define, update parentPolicy to mesh policy.
		parentPolicy.Mtls = meshPolicy.Mtls
	}

	if namespacePolicy != nil && namespacePolicy.Mtls != nil && namespacePolicy.Mtls.Mode != v1beta1.PeerAuthentication_MutualTLS_UNSET {
		// If namespace policy is define, update parent policy to namespace policy. This means namespace
		// policy can completely overwrite mesh policy.
		parentPolicy.Mtls = namespacePolicy.Mtls
	}

	if numWorkloadLevelPolicies == 0 {
		// There is no workload-level policy, return the parent policy.
		return &parentPolicy
	}

	// Placeholder for the final consolidated policy.
	finalPolicy := v1beta1.PeerAuthentication{
		Mtls: &v1beta1.PeerAuthentication_MutualTLS{
			Mode: v1beta1.PeerAuthentication_MutualTLS_UNSET,
		},
	}

	// Second round to handle workload level policies. For each policy:
	// - If its workload-level mTLS is not defined or is UNSET, treat it as if it has the parent mTLS.
	// - If the policy mTLS is "strictly stronger" than the consolidation, update the consolidation's
	// mTLS to that.
	// - If the policy has port-level mTLS and is "strictly stronger" than the one in consolidation
	// (if exists), copy it to consolidation.
	// - If the port-level mTLS is UNSET, mark the port and do another round of reconcile with workload level.
	unsetPorts := make(map[uint32]bool)
	for _, cfg := range configs {
		spec := cfg.Spec.(*v1beta1.PeerAuthentication)
		// Ignore namespace and mesh level policies. Also ignore policies defined in root namespace
		// but have selector (only mesh-level policy can be defined in root namespace).
		if cfg.Namespace == rootNamespace || spec.Selector == nil {
			continue
		}

		if spec.Mtls != nil &&
			spec.Mtls.Mode != v1beta1.PeerAuthentication_MutualTLS_UNSET &&
			isStrictlyStronger(spec.Mtls,finalPolicy.Mtls) {
			// Current policy has explicit mTLS, with stronger mTLS mode than the consolidated policy: update to current.
			finalPolicy.Mtls = spec.Mtls
		} else if isStrictlyStronger(parentPolicy.Mtls, finalPolicy.Mtls) {
			// Current policy inherit from parent, and parent has stronger mTLS mode than the consolidate policy: update to parent.
			finalPolicy.Mtls = parentPolicy.Mtls
		}

		// Check port level settings.
		for port, mtls := range spec.PortLevelMtls {
			if finalPolicy.PortLevelMtls == nil {
				finalPolicy.PortLevelMtls = make(map[uint32]*v1beta1.PeerAuthentication_MutualTLS)
			}

			if mtls.Mode == v1beta1.PeerAuthentication_MutualTLS_UNSET {
				unsetPorts[port] = true
			} else {
				existing, exist := finalPolicy.PortLevelMtls[port]
				if !exist || isStrictlyStronger(mtls, existing) {
					finalPolicy.PortLevelMtls[port] = mtls
				}
			}
		}
	}

	// Review ports with UNSET mode and update the final workload level mTLS if it is stronger.
	for port := range unsetPorts {
		existing, exist := finalPolicy.PortLevelMtls[port]
		if !exist || isStrictlyStronger(finalPolicy.Mtls, existing) {
			finalPolicy.PortLevelMtls[port] = finalPolicy.Mtls
		}
	}

	return &finalPolicy
}

func isStrictlyStronger(left, right *v1beta1.PeerAuthentication_MutualTLS) bool {
	return getMutualTLSMode(left) > getMutualTLSMode(right)
}
