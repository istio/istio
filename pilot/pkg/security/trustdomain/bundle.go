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

	istiolog "istio.io/pkg/log"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type Bundle struct {
	// Contain the local trust domain and its aliases.
	// The trust domain corresponds to the trust root of a system.
	// Refer to [SPIFFE-ID](https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain)
	// The trust domain aliases represent the aliases of `trust_domain`.
	// For example, if we have
	// trustDomain: td1, trustDomainAliases: ["td2", "td3"]
	// Any service with the identity `td1/ns/foo/sa/a-service-account`, `td2/ns/foo/sa/a-service-account`,
	// or `td3/ns/foo/sa/a-service-account` will be treated the same in the Istio mesh.
	TrustDomains []string
}

func NewTrustDomainBundle(trustDomain string, trustDomainAliases []string) Bundle {

	return Bundle{
		// Put the new trust domain to the beginning of the list to avoid changing existing tests.
		TrustDomains: append([]string{trustDomain}, trustDomainAliases...),
	}
}

// ReplaceTrustDomainAliases checks the existing principals and returns a list of new principals
// with the current trust domain and its aliases.
// For example, for a user "bar" in namespace "foo".
// If the local trust domain is "td2" and its alias is "td1" (migrating from td1 to td2),
// replaceTrustDomainAliases returns ["td2/ns/foo/sa/bar", "td1/ns/foo/sa/bar]].
func (t Bundle) ReplaceTrustDomainAliases(principals []string) []string {
	principalsIncludingAliases := []string{}
	for _, principal := range principals {
		trustDomainFromPrincipal := getTrustDomain(principal)
		// Return the existing principals if getTrustDomain() returns an empty string or "*".
		// This also works for principals with "*".
		// If the principal is "*" or "*/sa/bar", the trust domain is disregard, so we don't need to generate
		// extra config (getTrustDomain returns an empty string)
		// If the principal is "*/ns/foo/sa/bar", getTrustDomain returns *, and the trust domain is disregarded
		// in the Envoy RBAC filter.
		// If the principal is "*some-thing/ns/foo/sa/bar", getTrustDomain returns "*some-thing" and we
		// will need to generate extra config.
		if trustDomainFromPrincipal == "" || trustDomainFromPrincipal == "*" {
			principalsIncludingAliases = append(principalsIncludingAliases, principal)
			continue
		}
		// Only generate configuration if the extracted trust domain from the policy is part of the trust domain list,
		// or if the extracted/existing trust domain is "cluster.local", which is a pointer to the local trust domain
		// and its aliases.
		if stringMatch(trustDomainFromPrincipal, t.TrustDomains) || trustDomainFromPrincipal == "cluster.local" {
			// Generate configuration for trust domain and trust domain aliases.
			principalsIncludingAliases = append(principalsIncludingAliases, t.replaceTrustDomains(principal)...)
		} else {
			rbacLog.Warnf("Trust domain %s from principal %s does not match the current trust "+
				"domain or its aliases", trustDomainFromPrincipal, principal)
			// If the trust domain from the existing doesn't match with the new trust domain aliases or "cluster.local",
			// keep the policy as it is.
			principalsIncludingAliases = append(principalsIncludingAliases, principal)
		}
	}
	return principalsIncludingAliases
}

// replaceTrustDomains replace the given principal's trust domain with the trust domains from the
// trustDomains list and return the new principals.
func (t Bundle) replaceTrustDomains(principal string) []string {
	trustDomainFromPrincipal, err := getTrustDomainOrError(principal)
	if err != nil {
		// This should not happen as we already make sure trustDomainFromPrincipal is not * or "" from
		// the caller. However, if it happens, return the principal as-is.
		return []string{principal}
	}
	principalsForAliases := []string{}
	for _, td := range t.TrustDomains {
		// If the trust domain has a prefix * (e.g. *local from *local/ns/foo/ns/bar), keep the principal
		// as-is for the matched trust domain. For others, replace the trust domain with the new trust domain
		// or alias.
		if suffixMatch(td, trustDomainFromPrincipal) {
			principalsForAliases = append(principalsForAliases, principal)
		} else {
			principalsForAliases = append(principalsForAliases, replaceTrustDomainInPrincipal(td, principal))
		}
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
		rbacLog.Errorf("Wrong SPIFFE format: %s", principal)
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
		return ""
	}
	return identityParts[0]
}

// getTrustDomainOrError is similar to getTrustDomain. However, it expects principal to have the right
// SPIFFE format and the trust domain cannot be * (i.g. */ns/foo/sa/bar).
func getTrustDomainOrError(principal string) (string, error) {
	trustDomain := getTrustDomain(principal)
	if trustDomain == "" || trustDomain == "*" {
		return "", fmt.Errorf("wrong SPIFFE format: %s", principal)
	}
	return trustDomain, nil
}
