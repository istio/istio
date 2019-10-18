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

type TrustDomainBundle struct {
	// The trust domain corresponds to the trust root of a system.
	// Refer to [SPIFFE-ID](https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain)
	TrustDomain string
	// The trust domain aliases represent the aliases of `trust_domain`.
	// For example, if we have
	// ```yaml
	// trustDomain: td1
	// trustDomainAliases: ["td2", "td3"]
	// ```
	// Any service with the identity `td1/ns/foo/sa/a-service-account`, `td2/ns/foo/sa/a-service-account`,
	// or `td3/ns/foo/sa/a-service-account` will be treated the same in the Istio mesh.
	TrustDomainAliases []string
}

func NewTrustDomainBundle(trustDomain string, trustDomainAliases []string) *TrustDomainBundle {
	return &TrustDomainBundle{
		TrustDomain: trustDomain,
		TrustDomainAliases: trustDomainAliases,
	}
}

// replaceTrustDomainInPrincipal returns a new SPIFFE identity with the new trust domain.
// The trust domain corresponds to the trust root of a system.
// Refer to
// [SPIFFE-ID](https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain)
// In Istio authorization, an identity is presented in the format:
// <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
func (t *TrustDomainBundle) replaceTrustDomainInPrincipal(principal string) string {
	identityParts := strings.Split(principal, "/")
	// A valid SPIFFE identity in authorization has no SPIFFE:// prefix.
	// It is presented as <trust-domain>/ns/<some-namespace>/sa/<some-service-account>
	if len(identityParts) != 5 {
		log.Errorf("Wrong SPIFFE format found: %s", principal)
		return ""
	}
	return fmt.Sprintf("%s/%s", t.TrustDomain, strings.Join(identityParts[1:], "/"))
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
		log.Errorf("Wrong SPIFFE format found: %s", principal)
		return ""
	}
	return identityParts[0]
}
