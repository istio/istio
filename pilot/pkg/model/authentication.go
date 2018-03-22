// Copyright 2018 Istio Authors
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

package model

import (
	"crypto/sha1"
	"fmt"
	"net/url"
	"strconv"

	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha2"
	meshconfig "istio.io/api/mesh/v1alpha1"
	mccpb "istio.io/api/mixer/v1/config/client"
	"istio.io/istio/pkg/log"
)

const (
	// Defautl cache duration for JWT public key. This should be moved to a global config.
	jwtPublicKeyCacheSeconds = 60 * 5
)

// GetConsolidateAuthenticationPolicy returns the authentication policy for
// service specified by hostname and port, if defined.
// If not, it generates and output a policy that is equivalent to the legacy flag
// and/or service annotation. Once these legacy flags/config deprecated,
// this function can be placed by a call to store.AuthenticationPolicyByDestination
// directly.
func GetConsolidateAuthenticationPolicy(mesh *meshconfig.MeshConfig, store IstioConfigStore, hostname string, port *Port) *authn.Policy {
	config := store.AuthenticationPolicyByDestination(hostname, port)
	if config == nil {
		legacyPolicy := consolidateAuthPolicy(mesh, port.AuthenticationPolicy)
		log.Debugf("No authentication policy found for  %s:%d. Fallback to legacy authentication mode %v\n",
			hostname, port.Port, legacyPolicy)
		return legacyAuthenticationPolicyToPolicy(legacyPolicy)
	}

	return config.Spec.(*authn.Policy)
}

// consolidateAuthPolicy returns service auth policy, if it's not INHERIT. Else,
// returns mesh policy.
func consolidateAuthPolicy(mesh *meshconfig.MeshConfig,
	serviceAuthPolicy meshconfig.AuthenticationPolicy) meshconfig.AuthenticationPolicy {
	if serviceAuthPolicy != meshconfig.AuthenticationPolicy_INHERIT {
		return serviceAuthPolicy
	}
	// TODO: use AuthenticationPolicy for mesh policy and remove this conversion
	switch mesh.AuthPolicy {
	case meshconfig.MeshConfig_MUTUAL_TLS:
		return meshconfig.AuthenticationPolicy_MUTUAL_TLS
	case meshconfig.MeshConfig_NONE:
		return meshconfig.AuthenticationPolicy_NONE
	default:
		// Never get here, there are no other enum value for mesh.AuthPolicy.
		panic(fmt.Sprintf("Unknown mesh auth policy: %v\n", mesh.AuthPolicy))
	}
}

// If input legacy is AuthenticationPolicy_MUTUAL_TLS, return a authentication policy equivalent
// to it. Else, returns nil (implies no authentication is used)
func legacyAuthenticationPolicyToPolicy(legacy meshconfig.AuthenticationPolicy) *authn.Policy {
	if legacy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		return &authn.Policy{
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{}}},
		}
	}
	return nil
}

// RequireTLS returns true if the policy use mTLS for (peer) authentication.
func RequireTLS(policy *authn.Policy) bool {
	if policy == nil {
		return false
	}
	if len(policy.Peers) > 0 {
		for _, method := range policy.Peers {
			switch method.GetParams().(type) {
			case *authn.PeerAuthenticationMethod_Mtls:
				return true
			default:
				continue
			}
		}
	}
	return false
}

// ParseJwksURI parses the input URI and returns the corresponding hostname, port, and whether SSL is used.
// URI must start with "http://" or "https://", which corresponding to "http" or "https" scheme.
// Port number is extracted from URI if available (i.e from postfix :<port>, eg. ":80"), or assigned
// to a default value based on URI scheme (80 for http and 443 for https).
// Port name is set to URI scheme value.
// Note: this is to replace [buildJWKSURIClusterNameAndAddress]
// (https://github.com/istio/istio/blob/master/pilot/pkg/proxy/envoy/v1/mixer.go#L401),
// which is used for the old EUC policy.
func ParseJwksURI(jwksURI string) (string, *Port, bool, error) {
	u, err := url.Parse(jwksURI)
	if err != nil {
		return "", nil, false, err
	}

	var useSSL bool
	var portNumber int
	switch u.Scheme {
	case "http":
		useSSL = false
		portNumber = 80
	case "https":
		useSSL = true
		portNumber = 443
	default:
		return "", nil, false, fmt.Errorf("URI scheme %s is not supported", u.Scheme)
	}

	if u.Port() != "" {
		portNumber, err = strconv.Atoi(u.Port())
		if err != nil {
			return "", nil, useSSL, err
		}
	}

	return u.Hostname(), &Port{
		Name: u.Scheme,
		Port: portNumber,
	}, useSSL, nil
}

// JwksURIClusterName returns cluster name for the jwks URI. This should be used
// to override the name for outbound cluster that are added for Jwks URI so that they
// can be referred correctly in the JWT filter config.
func JwksURIClusterName(hostname string, port *Port) string {
	const clusterPrefix = "jwks."
	const maxClusterNameLength = 189 - len(clusterPrefix)
	name := hostname + "|" + port.Name
	if len(name) > maxClusterNameLength {
		prefix := name[:maxClusterNameLength-sha1.Size*2]
		sum := sha1.Sum([]byte(name))
		name = fmt.Sprintf("%s%x", prefix, sum)
	}
	return clusterPrefix + name
}

// CollectJwtSpecs returns a list of all JWT specs (ponters) defined the policy. This
// provides a convenient way to iterate all Jwt specs.
func CollectJwtSpecs(policy *authn.Policy) []*authn.Jwt {
	ret := []*authn.Jwt{}
	if policy == nil {
		return ret
	}
	for _, method := range policy.Peers {
		switch method.GetParams().(type) {
		case *authn.PeerAuthenticationMethod_Jwt:
			ret = append(ret, method.GetJwt())
		}
	}
	for _, method := range policy.Origins {
		ret = append(ret, method.Jwt)
	}
	return ret
}

// ConvertPolicyToJwtConfig converts policy into Jwt filter config for envoy. The
// config is still incomplete though: the jwks_uri_envoy_cluster has not been set
// yet; it should be filled by pilot, accordingly how those clusters are added.
// Also note,  the Jwt filter implementation is in Istio proxy, but it is under
// upstreamming process
// (https://github.com/envoyproxy/data-plane-api/pull/530/files).
// The output of this function should use the Envoy data-plane-api proto once
// this migration finished.
func ConvertPolicyToJwtConfig(policy *authn.Policy) *mccpb.EndUserAuthenticationPolicySpec {
	policyJwts := CollectJwtSpecs(policy)
	if len(policyJwts) == 0 {
		return nil
	}
	ret := &mccpb.EndUserAuthenticationPolicySpec{}
	for _, policyJwt := range policyJwts {
		hostname, port, _, err := ParseJwksURI(policyJwt.JwksUri)
		if err != nil {
			log.Errorf("Cannot parse jwks_uri %q: %v", policyJwt.JwksUri, err)
			continue
		}
		jwt := &mccpb.JWT{
			Issuer:                 policyJwt.Issuer,
			Audiences:              policyJwt.Audiences,
			JwksUri:                policyJwt.JwksUri,
			ForwardJwt:             true,
			PublicKeyCacheDuration: &types.Duration{Seconds: jwtPublicKeyCacheSeconds},
			JwksUriEnvoyCluster:    JwksURIClusterName(hostname, port),
		}
		for _, location := range policyJwt.JwtHeaders {
			jwt.Locations = append(jwt.Locations, &mccpb.JWT_Location{
				Scheme: &mccpb.JWT_Location_Header{Header: location},
			})
		}
		for _, location := range policyJwt.JwtParams {
			jwt.Locations = append(jwt.Locations, &mccpb.JWT_Location{
				Scheme: &mccpb.JWT_Location_Query{Query: location},
			})
		}
		ret.Jwts = append(ret.Jwts, jwt)
	}
	return ret
}
