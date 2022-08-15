// Copyright Istio Authors
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

	"istio.io/istio/pkg/config/constants"
	istiolog "istio.io/pkg/log"
)

var authzLog = istiolog.RegisterScope("authorization", "Istio Authorization Policy", 0)

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

// NewBundle returns a new trust domain bundle.
func NewBundle(trustDomain string, trustDomainAliases []string) Bundle {
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
		isTrustDomainBeingEnforced := isTrustDomainBeingEnforced(principal)
		// Return the existing principals if the policy doesn't care about the trust domain.
		if !isTrustDomainBeingEnforced {
			principalsIncludingAliases = append(principalsIncludingAliases, principal)
			continue
		}
		trustDomainFromPrincipal, err := getTrustDomainFromSpiffeIdentity(principal)
		if err != nil {
			authzLog.Errorf("unexpected incorrect Spiffe format: %s", principal)
			principalsIncludingAliases = append(principalsIncludingAliases, principal)
			continue
		}
		// Only generate configuration if the extracted trust domain from the policy is part of the trust domain list,
		// or if the extracted/existing trust domain is "cluster.local", which is a pointer to the local trust domain
		// and its aliases.
		if stringMatch(trustDomainFromPrincipal, t.TrustDomains) || trustDomainFromPrincipal == constants.DefaultClusterLocalDomain {
			// Generate configuration for trust domain and trust domain aliases.
			principalsIncludingAliases = append(principalsIncludingAliases, t.replaceTrustDomains(principal, trustDomainFromPrincipal)...)
		} else {
			authzLog.Warnf("Trust domain %s from principal %s does not match the current trust "+
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
func (t Bundle) replaceTrustDomains(principal, trustDomainFromPrincipal string) []string {
	principalsForAliases := []string{}
	for _, td := range t.TrustDomains {
		// If the trust domain has a prefix * (e.g. *local from *local/ns/foo/sa/bar), keep the principal
		// as-is for the matched trust domain. For others, replace the trust domain with the new trust domain
		// or alias.
		var newPrincipal string
		var err error
		if suffixMatch(td, trustDomainFromPrincipal) {
			newPrincipal = principal
		} else {
			newPrincipal, err = replaceTrustDomainInPrincipal(td, principal)
			if err != nil {
				authzLog.Errorf("Failed to replace trust domain with %s from principal %s: %v", td, principal, err)
				continue
			}
		}
		// Check to make sure we don't generate duplicated principals. This happens when trust domain
		// has a * prefix. For example, "*-td" can match with "old-td" and "new-td", but we only want
		// to keep the principal as-is in the generated config, .i.e. *-td.
		if !isKeyInList(newPrincipal, principalsForAliases) {
			principalsForAliases = append(principalsForAliases, newPrincipal)
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
func replaceTrustDomainInPrincipal(trustDomain string, principal string) (string, error) {
	identityParts := strings.Split(principal, "/")
	// A valid SPIFFE identity in authorization has no SPIFFE:// prefix.
	// It is presented as <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
	if len(identityParts) != 5 {
		return "", fmt.Errorf("wrong SPIFFE format: %s", principal)
	}
	return fmt.Sprintf("%s/%s", trustDomain, strings.Join(identityParts[1:], "/")), nil
}

// isTrustDomainBeingEnforced checks whether the trust domain is being checked in the filter or not.
// For example, in the principal "*/ns/foo/sa/bar", the trust domain is * and it matches to any trust domain,
// so it won't be checked in the filter.
func isTrustDomainBeingEnforced(principal string) bool {
	identityParts := strings.Split(principal, "/")
	if len(identityParts) != 5 {
		// If a principal is mis-configured and doesn't follow Spiffe format, e.g. "sa/bar",
		// there is really no trust domain from the principal, so the trust domain is also considered not being enforced.
		return false
	}
	// Check if the first part of the spiffe string is "*" (as opposed to *-something or "").
	return identityParts[0] != "*"
}

// getTrustDomainFromSpiffeIdentity gets the trust domain from the given principal and expects
// principal to have the right SPIFFE format.
func getTrustDomainFromSpiffeIdentity(principal string) (string, error) {
	identityParts := strings.Split(principal, "/")
	// A valid SPIFFE identity in authorization has no SPIFFE:// prefix.
	// It is presented as <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
	if len(identityParts) != 5 {
		return "", fmt.Errorf("wrong SPIFFE format: %s", principal)
	}
	trustDomain := identityParts[0]
	return trustDomain, nil
}
