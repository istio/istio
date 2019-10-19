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

package trustdomain

import (
	"fmt"
	"strings"

	"istio.io/pkg/log"
)

type Bundle struct {
	// The trust domain corresponds to the trust root of a system.
	// Refer to [SPIFFE-ID](https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain)
	TrustDomain string
	// The trust domain aliases represent the aliases of `trust_domain`.
	// For example, if we have
	// trustDomain: td1, trustDomainAliases: ["td2", "td3"]
	// Any service with the identity `td1/ns/foo/sa/a-service-account`, `td2/ns/foo/sa/a-service-account`,
	// or `td3/ns/foo/sa/a-service-account` will be treated the same in the Istio mesh.
	TrustDomainAliases []string
}

func NewTrustDomainBundle(trustDomain string, trustDomainAliases []string) Bundle {
	return Bundle{
		TrustDomain:        trustDomain,
		TrustDomainAliases: trustDomainAliases,
	}
}

// ReplaceTrustDomainAliases checks the existing principals and returns a list of new principals
// with the current trust domain and its aliases.
// For example, for a user "bar" in namespace "foo".
// If the local trust domain is "td2" and its alias is "td1" (migrating from td1 to td2),
// replaceTrustDomainAliases returns ["td2/ns/foo/sa/bar", "td1/ns/foo/sa/bar]].
func (t Bundle) ReplaceTrustDomainAliases(principals []string) []string {
	// If trust domain aliases are empty, return the existing principals.
	if len(t.TrustDomainAliases) == 0 {
		return principals
	}
	principalsIncludingAliases := []string{}
	for _, principal := range principals {
		trustDomainFromPrincipal := getTrustDomain(principal)
		//TODO(pitlv2109): Handle * and prefix/suffix.
		if trustDomainFromPrincipal == "" {
			return principals
		}
		// Only generate configuration if the extracted trust domain from the policy is part of the trust domain aliases,
		// or if the extracted/existing trust domain is "cluster.local", which is a pointer to the local trust domain
		// and its aliases.
		if found(trustDomainFromPrincipal, t.TrustDomainAliases) || trustDomainFromPrincipal == "cluster.local" {
			// Generate configuration for trust domain and trust domain aliases.
			principalsIncludingAliases = append(principalsIncludingAliases, t.replaceTrustDomains(principal)...)
		}
	}
	return principalsIncludingAliases
}

func (t Bundle) replaceTrustDomains(principal string) []string {
	principalsForAliases := []string{}
	principalsForAliases = append(principalsForAliases, replaceTrustDomainInPrincipal(t.TrustDomain, principal))
	for _, tdAlias := range t.TrustDomainAliases {
		principalsForAliases = append(principalsForAliases, replaceTrustDomainInPrincipal(tdAlias, principal))
	}
	return principalsForAliases
}

// replaceTrustDomainInPrincipal returns a new SPIFFE identity with the new trust domain.
// The trust domain corresponds to the trust root of a system.
// Refer to
// [SPIFFE-ID](https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain)
// In Istio authorization, an identity is presented in the format:
// <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
// TODO(pitlv2109): See if we can return an error here instead of empty string.
// Resolve it in https://github.com/istio/istio/pull/18011
func replaceTrustDomainInPrincipal(trustDomain string, principal string) string {
	identityParts := strings.Split(principal, "/")
	// A valid SPIFFE identity in authorization has no SPIFFE:// prefix.
	// It is presented as <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
	if len(identityParts) != 5 {
		log.Errorf("Wrong SPIFFE format: %s", principal)
		return ""
	}
	return fmt.Sprintf("%s/%s", trustDomain, strings.Join(identityParts[1:], "/"))
}

// getTrustDomain returns the trust domain from an Istio authorization principal.
// In Istio authorization, an identity is presented in the format:
// <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
// "cluster.local/ns/default/sa/bookinfo-ratings-v2" will return "cluster.local"
func getTrustDomain(principal string) string {
	identityParts := strings.Split(principal, "/")
	// A valid SPIFFE identity in authorization has no SPIFFE:// prefix.
	// It is presented as <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
	if len(identityParts) != 5 {
		log.Errorf("Wrong SPIFFE format: %s", principal)
		return ""
	}
	return identityParts[0]
}

func found(key string, list []string) bool {
	for _, l := range list {
		if key == l {
			return true
		}
	}
	return false
}
