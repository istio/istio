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

package features

import (
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// Define security related features here.
var (
	// SkipValidateTrustDomain tells the server proxy to not to check the peer's trust domain when
	// mTLS is enabled in authentication policy.
	SkipValidateTrustDomain = env.Register(
		"PILOT_SKIP_VALIDATE_TRUST_DOMAIN",
		false,
		"Skip validating the peer is from the same trust domain when mTLS is enabled in authentication policy").Get()

	XDSAuth = env.Register("XDS_AUTH", true,
		"If true, will authenticate XDS clients.").Get()

	EnableXDSIdentityCheck = env.Register(
		"PILOT_ENABLE_XDS_IDENTITY_CHECK",
		true,
		"If enabled, pilot will authorize XDS clients, to ensure they are acting only as namespaces they have permissions for.",
	).Get()

	// TODO: Move this to proper API.
	trustedGatewayCIDR = env.Register(
		"TRUSTED_GATEWAY_CIDR",
		"",
		"If set, any connections from gateway to Istiod with this CIDR range are treated as trusted for using authentication mechanisms like XFCC."+
			" This can only be used when the network where Istiod and the authenticating gateways are running in a trusted/secure network",
	)

	TrustedGatewayCIDR = func() []string {
		cidr := trustedGatewayCIDR.Get()

		// splitting the empty string will result [""]
		if cidr == "" {
			return []string{}
		}

		return strings.Split(cidr, ",")
	}()

	CATrustedNodeAccounts = func() sets.Set[types.NamespacedName] {
		accounts := env.Register(
			"CA_TRUSTED_NODE_ACCOUNTS",
			"",
			"If set, the list of service accounts that are allowed to use node authentication for CSRs. "+
				"Node authentication allows an identity to create CSRs on behalf of other identities, but only if there is a pod "+
				"running on the same node with that identity. "+
				"This is intended for use with node proxies.",
		).Get()
		res := sets.New[types.NamespacedName]()
		if accounts == "" {
			return res
		}
		for _, v := range strings.Split(accounts, ",") {
			ns, sa, valid := strings.Cut(v, "/")
			if !valid {
				log.Warnf("Invalid CA_TRUSTED_NODE_ACCOUNTS, ignoring: %v", v)
				continue
			}
			res.Insert(types.NamespacedName{
				Namespace: ns,
				Name:      sa,
			})
		}
		return res
	}()

	CertSignerDomain = env.Register("CERT_SIGNER_DOMAIN", "", "The cert signer domain info").Get()

	UseCacertsForSelfSignedCA = env.Register("USE_CACERTS_FOR_SELF_SIGNED_CA", false,
		"If enabled, istiod will use a secret named cacerts to store its self-signed istio-"+
			"generated root certificate.").Get()
)
