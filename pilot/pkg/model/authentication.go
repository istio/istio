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
	"fmt"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/log"
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
