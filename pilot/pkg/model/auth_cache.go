// Copyright 2017 Istio Authors
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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pkg/cache"
	"istio.io/istio/pkg/log"
)

const (
	// https://openid.net/specs/openid-connect-discovery-1_0.html
	// OpenID Providers supporting Discovery MUST make a JSON document available at the path
	// formed by concatenating the string /.well-known/openid-configuration to the Issuer.
	openIDDiscoveryCfgURLSuffix = "/.well-known/openid-configuration"

	// OpenID Discovery web request timeout.
	openIDDiscoveryHTTPTimeOutInSec = 5

	// JwksURI Cache expiration time duration, individual cached JwksURI item will be removed
	// from cache after its duration expires.
	jwksURICacheExpiration = time.Hour * 24

	// JwksURI Cache eviction time duration, cache eviction is done on a periodic basis,
	// jwksURICacheEviction specifies the frequency at which eviction activities take place.
	jwksURICacheEviction = time.Minute * 30
)

// jwksURIResolver is wrapper of expiringCache for jwksUri.
type jwksURIResolver struct {
	cache cache.ExpiringCache
}

// newJwksURIResolver creates new instance of jwksURIResolver.
func newJwksURIResolver() jwksURIResolver {
	return jwksURIResolver{cache.NewTTL(jwksURICacheExpiration, jwksURICacheEviction)}
}

// Set jwks_uri through openID discovery if it's not set in auth policy.
func (r *jwksURIResolver) setAuthenticationPolicyJwksURIs(policy *authn.Policy) {
	if policy == nil {
		return
	}

	for _, method := range policy.Peers {
		switch method.GetParams().(type) {
		case *authn.PeerAuthenticationMethod_Jwt:
			policyJwt := method.GetJwt()
			if policyJwt.JwksUri == "" {
				uri, err := r.resolveJwksURIUsingOpenID(policyJwt.Issuer)
				if err != nil {
					log.Warnf("Failed to get jwks_uri for issuer %q: %v", policyJwt.Issuer, err)
					continue
				}
				policyJwt.JwksUri = uri
			}
		}
	}
	for _, method := range policy.Origins {
		// JWT is only allowed authentication method type for Origin.
		policyJwt := method.GetJwt()
		if policyJwt.JwksUri == "" {
			uri, err := r.resolveJwksURIUsingOpenID(policyJwt.Issuer)
			if err != nil {
				log.Warnf("Failed to get jwks_uri for issuer %q: %v", policyJwt.Issuer, err)
				continue
			}
			policyJwt.JwksUri = uri
		}
	}
}

// Resolve jwks_uri through openID discovery and cache the jwks_uri for furture use.
func (r *jwksURIResolver) resolveJwksURIUsingOpenID(issuer string) (string, error) {
	// Set policyJwt.JwksUri if the JwksUri could be found in cache.
	if uri, found := r.cache.Get(issuer); found {
		return uri.(string), nil
	}

	// Try to get jwks_uri through OpenID Discovery.
	discoveryURL := issuer + openIDDiscoveryCfgURLSuffix
	client := &http.Client{
		Timeout: openIDDiscoveryHTTPTimeOutInSec * time.Second,
	}
	resp, err := client.Get(discoveryURL)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}

	jwksURI, ok := data["jwks_uri"].(string)
	if !ok {
		return "", fmt.Errorf("invalid jwks_uri %v in openID discovery configuration", data["jwks_uri"])
	}

	// Set JwksUri in cache.
	r.cache.Set(issuer, jwksURI)

	return jwksURI, nil
}
