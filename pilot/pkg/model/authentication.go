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
	"net/url"
	"strconv"

	authn "istio.io/api/authentication/v1alpha1"
)

// JwtKeyResolver resolves JWT public key and JwksURI.
var JwtKeyResolver = newJwksResolver(JwtPubKeyExpireDuration, JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)

// GetConsolidateAuthenticationPolicy returns the authentication policy for
// service specified by hostname and port, if defined. It also tries to resolve JWKS URI if necessary.
func GetConsolidateAuthenticationPolicy(store IstioConfigStore, service *Service, port *Port) *authn.Policy {
	config := store.AuthenticationPolicyByDestination(service, port)
	if config != nil {
		policy := config.Spec.(*authn.Policy)
		if err := JwtKeyResolver.SetAuthenticationPolicyJwksURIs(policy); err == nil {
			return policy
		}
	}

	return nil
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
		return "", nil, false, fmt.Errorf("URI scheme %q is not supported", u.Scheme)
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
