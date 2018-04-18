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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
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
	jwksHTTPTimeOutInSec = 5

	// JwksURI Cache expiration time duration, individual cached JwksURI item will be removed
	// from cache after its duration expires.
	jwksURICacheExpiration = time.Hour * 24

	// JwksURI Cache eviction time duration, cache eviction is done on a periodic basis,
	// jwksURICacheEviction specifies the frequency at which eviction activities take place.
	jwksURICacheEviction = time.Minute * 30

	// JwtPubKeyExpireDuration is the expire duration for JWT public key in the cache.
	// After this duration expire, refresher job will fetch key for the cached item again.
	JwtPubKeyExpireDuration = time.Hour

	// JwtPubKeyRefreshInterval is the running interval of JWT pubKey refresh job.
	JwtPubKeyRefreshInterval = time.Minute * 20
)

// jwksURIResolver is wrapper of expiringCache for jwksUri.
type jwksURIResolver struct {
	cache  cache.ExpiringCache
	client *http.Client
}

// jwtPubKeyEntry is a single cached entry for jwt public key.
type jwtPubKeyEntry struct {
	pubKey     string
	expireTime time.Time
}

// jwtPubKeyResolver is cache for jwt public key.
type jwtPubKeyResolver struct {
	// map key is issuer(not use jwks_uri as key since only JWT public key will be in config eventually).
	// map value is jwtPubKeyEntry.
	entries sync.Map

	client          *http.Client
	closing         chan bool
	refreshTicker   *time.Ticker
	jwksURIResolver *jwksURIResolver
	expireDuration  time.Duration
	refreshInterval time.Duration
}

// newJwksURIResolver creates new instance of jwksURIResolver.
func newJwksURIResolver() *jwksURIResolver {
	return &jwksURIResolver{
		cache: cache.NewTTL(jwksURICacheExpiration, jwksURICacheEviction),
		client: &http.Client{
			Timeout: jwksHTTPTimeOutInSec * time.Second,
		},
	}
}

// newJwtPubKeyResolver creates new instance of jwtPubKeyResolver.
func newJwtPubKeyResolver(r *jwksURIResolver, expireDuration, refreshInterval time.Duration) *jwtPubKeyResolver {
	ret := &jwtPubKeyResolver{
		client: &http.Client{
			Timeout: jwksHTTPTimeOutInSec * time.Second,
		},
		closing:         make(chan bool, 1),
		jwksURIResolver: r,
		expireDuration:  expireDuration,
		refreshInterval: refreshInterval,
	}

	go ret.refresher()

	return ret
}

// Set jwks_uri through openID discovery if it's not set in auth policy.
func (r *jwksURIResolver) SetAuthenticationPolicyJwksURIs(policy *authn.Policy) error {
	if policy == nil {
		return fmt.Errorf("invalid nil policy")
	}

	for _, method := range policy.Peers {
		switch method.GetParams().(type) {
		case *authn.PeerAuthenticationMethod_Jwt:
			policyJwt := method.GetJwt()
			if policyJwt.JwksUri == "" {
				uri, err := r.resolveJwksURIUsingOpenID(policyJwt.Issuer)
				if err != nil {
					log.Warnf("Failed to get jwks_uri for issuer %q: %v", policyJwt.Issuer, err)
					return err
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
				return err
			}
			policyJwt.JwksUri = uri
		}
	}

	return nil
}

// Resolve jwks_uri through openID discovery and cache the jwks_uri for furture use.
func (r *jwksURIResolver) resolveJwksURIUsingOpenID(issuer string) (string, error) {
	// Set policyJwt.JwksUri if the JwksUri could be found in cache.
	if uri, found := r.cache.Get(issuer); found {
		return uri.(string), nil
	}

	// Try to get jwks_uri through OpenID Discovery.
	discoveryURL := issuer + openIDDiscoveryCfgURLSuffix
	resp, err := r.client.Get(discoveryURL)
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

// ResolveJwtPubKey resolves JWT public key for issuer and cache the key for furture use.
func (r *jwtPubKeyResolver) ResolveJwtPubKey(issuer string) (string, error) {
	if val, found := r.entries.Load(issuer); found {
		return val.(*jwtPubKeyEntry).pubKey, nil
	}

	jwksURI, err := r.jwksURIResolver.resolveJwksURIUsingOpenID(issuer)
	if err != nil {
		return "", err
	}

	pubKey, err := r.fetchJwtPubKey(jwksURI)
	if err != nil {
		return "", err
	}

	r.entries.Store(issuer, &jwtPubKeyEntry{
		pubKey:     pubKey,
		expireTime: time.Now().Add(r.expireDuration),
	})

	return pubKey, nil
}

func (r *jwtPubKeyResolver) fetchJwtPubKey(jwksURI string) (string, error) {
	resp, err := r.client.Get(jwksURI)
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

	return string(body), nil
}

func (r *jwtPubKeyResolver) refresher() {
	// Wake up once in a while and refresh stale items.
	r.refreshTicker = time.NewTicker(r.refreshInterval)
	for {
		select {
		case now := <-r.refreshTicker.C:
			r.refresh(now)
		case <-r.closing:
			r.refreshTicker.Stop()
			return
		}
	}
}

func (r *jwtPubKeyResolver) refresh(t time.Time) {
	hasChange := false

	r.entries.Range(func(key interface{}, value interface{}) bool {
		issuer := key.(string)
		jwksURI, err := r.jwksURIResolver.resolveJwksURIUsingOpenID(issuer)
		if err != nil {
			log.Warnf("Failed to resolve jwksURI for issuer %q", issuer)
			// Return true to continue iterate remain entries even if one fails.
			return true
		}

		e := value.(*jwtPubKeyEntry)
		oldPubKey := e.pubKey

		if e.expireTime.Before(t) {
			// key rotation: fetch JWT public key again if it's expired.
			go func() {
				newPubKey, err := r.fetchJwtPubKey(jwksURI)
				if err != nil {
					log.Errorf("Cannot fetch JWT public key from %q", jwksURI)
					return
				}
				now := time.Now()
				//Update expireTime even if prev/current keys are the same.
				r.entries.Store(issuer, &jwtPubKeyEntry{
					pubKey:     newPubKey,
					expireTime: now.Add(r.expireDuration),
				})

				if oldPubKey != newPubKey {
					hasChange = true
				}
			}()
		}

		return true
	})

	if hasChange {
		// TODO(quanlin): send notification to update config and push config to sidecar.
	}
}

// Shut down the refresher job.
// TODO: may need to figure out the right place to call this function.
// (right now calls it from initDiscoveryService in pkg/bootstrap/server.go).
func (r *jwtPubKeyResolver) Close() {
	if r.refreshTicker != nil {
		r.refreshTicker.Stop()
	}
}
