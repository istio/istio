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

// jwtPubKeyEntry is a single cached entry for jwt public key.
type jwtPubKeyEntry struct {
	pubKey     string
	expireTime time.Time
}

// jwksResolver is resolver for jwksURI and jwt public key.
type jwksResolver struct {
	// cache for jwksURI.
	JwksURICache cache.ExpiringCache

	// cache for JWT public key.
	// map key is jwksURI, map value is jwtPubKeyEntry.
	keyEntries sync.Map

	client          *http.Client
	closing         chan bool
	refreshTicker   *time.Ticker
	expireDuration  time.Duration
	refreshInterval time.Duration
}

// newJwksResolver creates new instance of jwksResolver.
func newJwksResolver(expireDuration, refreshInterval time.Duration) *jwksResolver {
	ret := &jwksResolver{
		JwksURICache:    cache.NewTTL(jwksURICacheExpiration, jwksURICacheEviction),
		closing:         make(chan bool, 1),
		expireDuration:  expireDuration,
		refreshInterval: refreshInterval,
		client: &http.Client{
			Timeout: jwksHTTPTimeOutInSec * time.Second,
		},
	}

	go ret.refresher()

	return ret
}

// Set jwks_uri through openID discovery if it's not set in auth policy.
func (r *jwksResolver) SetAuthenticationPolicyJwksURIs(policy *authn.Policy) error {
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

// ResolveJwtPubKey resolves JWT public key and cache the key for furture use.
func (r *jwksResolver) ResolveJwtPubKey(jwksURI string) (string, error) {
	if val, found := r.keyEntries.Load(jwksURI); found {
		return val.(*jwtPubKeyEntry).pubKey, nil
	}

	pubKey, err := r.fetchJwtPubKey(jwksURI)
	if err != nil {
		return "", err
	}

	r.keyEntries.Store(jwksURI, &jwtPubKeyEntry{
		pubKey:     pubKey,
		expireTime: time.Now().Add(r.expireDuration),
	})

	return pubKey, nil
}

// Resolve jwks_uri through openID discovery and cache the jwks_uri for furture use.
func (r *jwksResolver) resolveJwksURIUsingOpenID(issuer string) (string, error) {
	// Set policyJwt.JwksUri if the JwksUri could be found in cache.
	if uri, found := r.JwksURICache.Get(issuer); found {
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
	r.JwksURICache.Set(issuer, jwksURI)

	return jwksURI, nil
}

func (r *jwksResolver) fetchJwtPubKey(jwksURI string) (string, error) {
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

func (r *jwksResolver) refresher() {
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

func (r *jwksResolver) refresh(t time.Time) {
	var wg sync.WaitGroup
	hasChange := false

	r.keyEntries.Range(func(key interface{}, value interface{}) bool {
		jwksURI := key.(string)

		e := value.(*jwtPubKeyEntry)
		oldPubKey := e.pubKey

		// key rotation: fetch JWT public key again if it's expired.
		if e.expireTime.Before(t) {
			// Increment the WaitGroup counter.
			wg.Add(1)

			go func() {
				// Decrement the counter when the goroutine completes.
				defer wg.Done()

				newPubKey, err := r.fetchJwtPubKey(jwksURI)
				if err != nil {
					log.Errorf("Cannot fetch JWT public key from %q", jwksURI)
					return
				}

				//Update expireTime even if prev/current keys are the same.
				r.keyEntries.Store(jwksURI, &jwtPubKeyEntry{
					pubKey:     newPubKey,
					expireTime: time.Now().Add(r.expireDuration),
				})

				if oldPubKey != newPubKey {
					hasChange = true
				}
			}()
		}

		return true
	})

	// Wait for all go routine to complete.
	wg.Wait()

	if hasChange {
		// TODO(quanlin): send notification to update config and push config to sidecar.
	}
}

// Shut down the refresher job.
// TODO: may need to figure out the right place to call this function.
// (right now calls it from initDiscoveryService in pkg/bootstrap/server.go).
func (r *jwksResolver) Close() {
	r.closing <- true
}
