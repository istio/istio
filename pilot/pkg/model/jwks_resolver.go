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

package model

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"istio.io/api/security/v1beta1"
	"istio.io/pkg/cache"
	"istio.io/pkg/monitoring"
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

	// JwtPubKeyEvictionDuration is the life duration for cached item.
	// Cached item will be removed from the cache if it hasn't been used longer than JwtPubKeyEvictionDuration or if pilot
	// has failed to refresh it for more than JwtPubKeyEvictionDuration.
	JwtPubKeyEvictionDuration = 24 * 7 * time.Hour

	// JwtPubKeyRefreshInterval is the running interval of JWT pubKey refresh job.
	JwtPubKeyRefreshInterval = time.Minute * 20

	// getRemoteContentRetryInSec is the retry interval between the attempt to retry getting the remote
	// content from network.
	getRemoteContentRetryInSec = 1

	// How many times should we retry the failed network fetch on main flow. The main flow
	// means it's called when Pilot is pushing configs. Do not retry to make sure not to block Pilot
	// too long.
	networkFetchRetryCountOnMainFlow = 0

	// How many times should we retry the failed network fetch on refresh flow. The refresh flow
	// means it's called when the periodically refresh job is triggered. We can retry more aggressively
	// as it's running separately from the main flow.
	networkFetchRetryCountOnRefreshFlow = 3

	// jwksPublicRootCABundlePath is the path of public root CA bundle in pilot container.
	jwksPublicRootCABundlePath = "/cacert.pem"
	// jwksExtraRootCABundlePath is the path to any additional CA certificates pilot should accept when resolving JWKS URIs
	jwksExtraRootCABundlePath = "/cacerts/extra.pem"
)

var (
	// Close channel
	closeChan = make(chan bool)

	networkFetchSuccessCounter = monitoring.NewSum(
		"pilot_jwks_resolver_network_fetch_success_total",
		"Total number of successfully network fetch by pilot jwks resolver",
	)
	networkFetchFailCounter = monitoring.NewSum(
		"pilot_jwks_resolver_network_fetch_fail_total",
		"Total number of failed network fetch by pilot jwks resolver",
	)

	// jwtKeyResolverOnce lazy init jwt key resolver
	jwtKeyResolverOnce sync.Once
	jwtKeyResolver     *JwksResolver
)

// jwtPubKeyEntry is a single cached entry for jwt public key.
type jwtPubKeyEntry struct {
	pubKey string

	// The last success refreshed time of the pubKey.
	lastRefreshedTime time.Time

	// Cached item's last used time, which is set in GetPublicKey.
	lastUsedTime time.Time
}

// JwksResolver is resolver for jwksURI and jwt public key.
type JwksResolver struct {
	// cache for jwksURI.
	JwksURICache cache.ExpiringCache

	// Callback function to invoke when detecting jwt public key change.
	PushFunc func()

	// cache for JWT public key.
	// map key is jwksURI, map value is jwtPubKeyEntry.
	keyEntries sync.Map

	secureHTTPClient *http.Client
	httpClient       *http.Client
	refreshTicker    *time.Ticker

	// Cached key will be removed from cache if (time.now - cachedItem.lastUsedTime >= evictionDuration), this prevents key cache growing indefinitely.
	evictionDuration time.Duration

	// Refresher job running interval.
	refreshInterval time.Duration

	// How many times refresh job has detected JWT public key change happened, used in unit test.
	refreshJobKeyChangedCount uint64

	// How many times refresh job failed to fetch the public key from network, used in unit test.
	refreshJobFetchFailedCount uint64
}

func init() {
	monitoring.MustRegister(networkFetchSuccessCounter, networkFetchFailCounter)
}

// NewJwksResolver creates new instance of JwksResolver.
func NewJwksResolver(evictionDuration, refreshInterval time.Duration) *JwksResolver {
	return newJwksResolverWithCABundlePaths(
		evictionDuration,
		refreshInterval,
		[]string{jwksPublicRootCABundlePath, jwksExtraRootCABundlePath},
	)
}

// GetJwtKeyResolver lazy-creates JwtKeyResolver resolves JWT public key and JwksURI.
func GetJwtKeyResolver() *JwksResolver {
	jwtKeyResolverOnce.Do(func() {
		jwtKeyResolver = NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)
	})
	return jwtKeyResolver
}

func newJwksResolverWithCABundlePaths(evictionDuration, refreshInterval time.Duration, caBundlePaths []string) *JwksResolver {
	ret := &JwksResolver{
		JwksURICache:     cache.NewTTL(jwksURICacheExpiration, jwksURICacheEviction),
		evictionDuration: evictionDuration,
		refreshInterval:  refreshInterval,
		httpClient: &http.Client{
			Timeout: jwksHTTPTimeOutInSec * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true,
				TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			},
		},
	}

	caCertPool := x509.NewCertPool()
	caCertsFound := false
	for _, pemFile := range caBundlePaths {
		caCert, err := ioutil.ReadFile(pemFile)
		if err == nil {
			caCertsFound = caCertPool.AppendCertsFromPEM(caCert) || caCertsFound
		}
	}
	if caCertsFound {
		ret.secureHTTPClient = &http.Client{
			Timeout: jwksHTTPTimeOutInSec * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true,
				TLSClientConfig: &tls.Config{
					RootCAs: caCertPool,
				},
			},
		}
	}

	atomic.StoreUint64(&ret.refreshJobKeyChangedCount, 0)
	atomic.StoreUint64(&ret.refreshJobFetchFailedCount, 0)
	go ret.refresher()

	return ret
}

// ResolveJwksURI sets jwks_uri through openID discovery if it's not set in request authentication policy.
func (r *JwksResolver) ResolveJwksURI(policy *v1beta1.RequestAuthentication) {
	for _, rule := range policy.JwtRules {
		if rule.JwksUri == "" && rule.Jwks == "" {
			if uri, err := r.resolveJwksURIUsingOpenID(rule.Issuer); err == nil {
				rule.JwksUri = uri
			} else {
				log.Warnf("Failed to get jwks_uri for issuer %q: %v", rule.Issuer, err)
			}
		}
	}
}

// GetPublicKey gets JWT public key and cache the key for future use.
func (r *JwksResolver) GetPublicKey(jwksURI string) (string, error) {
	now := time.Now()
	if val, found := r.keyEntries.Load(jwksURI); found {
		e := val.(jwtPubKeyEntry)
		// Update cached key's last used time.
		e.lastUsedTime = now
		r.keyEntries.Store(jwksURI, e)
		return e.pubKey, nil
	}

	// Fetch key if it's not cached.
	resp, err := r.getRemoteContentWithRetry(jwksURI, networkFetchRetryCountOnMainFlow)
	if err != nil {
		log.Errorf("Failed to fetch public key from %q: %v", jwksURI, err)
		return "", err
	}

	pubKey := string(resp)
	r.keyEntries.Store(jwksURI, jwtPubKeyEntry{
		pubKey:            pubKey,
		lastRefreshedTime: now,
		lastUsedTime:      now,
	})

	return pubKey, nil
}

// Resolve jwks_uri through openID discovery and cache the jwks_uri for future use.
func (r *JwksResolver) resolveJwksURIUsingOpenID(issuer string) (string, error) {
	// Set policyJwt.JwksUri if the JwksUri could be found in cache.
	if uri, found := r.JwksURICache.Get(issuer); found {
		return uri.(string), nil
	}

	// Try to get jwks_uri through OpenID Discovery.
	body, err := r.getRemoteContentWithRetry(issuer+openIDDiscoveryCfgURLSuffix, networkFetchRetryCountOnMainFlow)
	if err != nil {
		log.Errorf("Failed to fetch jwks_uri from %q: %v", issuer+openIDDiscoveryCfgURLSuffix, err)
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

func (r *JwksResolver) getRemoteContentWithRetry(uri string, retry int) ([]byte, error) {
	u, err := url.Parse(uri)
	if err != nil {
		log.Errorf("Failed to parse %q", uri)
		return nil, err
	}

	client := r.httpClient
	if strings.EqualFold(u.Scheme, "https") {
		// https client may be uninitialized because of root CA bundle missing.
		if r.secureHTTPClient == nil {
			return nil, fmt.Errorf("pilot does not support fetch public key through https endpoint %q", uri)
		}

		client = r.secureHTTPClient
	}

	getPublicKey := func() (b []byte, e error) {
		resp, err := client.Get(uri)
		defer func() {
			if e != nil {
				networkFetchFailCounter.Increment()
				return
			}
			networkFetchSuccessCounter.Increment()
			_ = resp.Body.Close()
		}()
		if err != nil {
			return nil, err
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("status %d, %s", resp.StatusCode, string(body))
		}

		return body, nil
	}

	for i := 0; i < retry; i++ {
		body, err := getPublicKey()
		if err == nil {
			return body, nil
		}
		log.Warnf("Failed to GET from %q: %s. Retry in %d seconds", uri, err, getRemoteContentRetryInSec)
		time.Sleep(getRemoteContentRetryInSec * time.Second)
	}

	// Return the last fetch directly, reaching here means we have tried `retry` times, this will be
	// the last time for the retry.
	return getPublicKey()
}

func (r *JwksResolver) refresher() {
	// Wake up once in a while and refresh stale items.
	r.refreshTicker = time.NewTicker(r.refreshInterval)
	for {
		select {
		case <-r.refreshTicker.C:
			r.refresh()
		case <-closeChan:
			r.refreshTicker.Stop()
		}
	}
}

func (r *JwksResolver) refresh() {
	var wg sync.WaitGroup
	hasChange := false

	r.keyEntries.Range(func(key interface{}, value interface{}) bool {
		now := time.Now()
		jwksURI := key.(string)
		e := value.(jwtPubKeyEntry)

		// Remove cached item for either of the following 2 situations
		// 1) it hasn't been used for a while
		// 2) it hasn't been refreshed successfully for a while
		// This makes sure 2 things, we don't grow the cache infinitely and also we don't reuse a cached public key
		// with no success refresh for too much time.
		if now.Sub(e.lastUsedTime) >= r.evictionDuration || now.Sub(e.lastRefreshedTime) >= r.evictionDuration {
			log.Infof("Removed cached JWT public key (lastRefreshed: %s, lastUsed: %s) from %q",
				e.lastRefreshedTime, e.lastUsedTime, jwksURI)
			r.keyEntries.Delete(jwksURI)
			return true
		}

		oldPubKey := e.pubKey

		// Increment the WaitGroup counter.
		wg.Add(1)

		go func() {
			// Decrement the counter when the goroutine completes.
			defer wg.Done()

			resp, err := r.getRemoteContentWithRetry(jwksURI, networkFetchRetryCountOnRefreshFlow)
			if err != nil {
				log.Errorf("Failed to refresh JWT public key from %q: %v", jwksURI, err)
				atomic.AddUint64(&r.refreshJobFetchFailedCount, 1)
				return
			}
			newPubKey := string(resp)
			r.keyEntries.Store(jwksURI, jwtPubKeyEntry{
				pubKey:            newPubKey,
				lastRefreshedTime: now,            // update the lastRefreshedTime if we get a success response from the network.
				lastUsedTime:      e.lastUsedTime, // keep original lastUsedTime.
			})
			isNewKey, err := compareJWKSResponse(oldPubKey, newPubKey)
			if err != nil {
				log.Errorf("Failed to refresh JWT public key from %q: %v", jwksURI, err)
				return
			}
			if isNewKey {
				hasChange = true
				log.Infof("Updated cached JWT public key from %q", jwksURI)
			}
		}()

		return true
	})

	// Wait for all go routine to complete.
	wg.Wait()

	if hasChange {
		atomic.AddUint64(&r.refreshJobKeyChangedCount, 1)
		// Push public key changes to sidecars.
		if r.PushFunc != nil {
			r.PushFunc()
		}
	}
}

// Shut down the refresher job.
// TODO: may need to figure out the right place to call this function.
// (right now calls it from initDiscoveryService in pkg/bootstrap/server.go).
func (r *JwksResolver) Close() {
	closeChan <- true
}

// Compare two JWKS responses, returning true if there is a difference and false otherwise
func compareJWKSResponse(oldKeyString string, newKeyString string) (bool, error) {
	if oldKeyString == newKeyString {
		return false, nil
	}

	var oldJWKs map[string]interface{}
	var newJWKs map[string]interface{}
	if err := json.Unmarshal([]byte(newKeyString), &newJWKs); err != nil {
		// If the new key is not parseable as JSON return an error since we will not want to use this key
		log.Warnf("New JWKs public key JSON is not parseable: %s", newKeyString)
		return false, err
	}
	if err := json.Unmarshal([]byte(oldKeyString), &oldJWKs); err != nil {
		log.Warnf("Previous JWKs public key JSON is not parseable: %s", oldKeyString)
		return true, nil
	}

	// Sort both sets of keys by "kid (key ID)" to be able to directly compare
	oldKeys, oldKeysExists := oldJWKs["keys"].([]interface{})
	newKeys, newKeysExists := newJWKs["keys"].([]interface{})
	if oldKeysExists && newKeysExists {
		sort.Slice(oldKeys, func(i, j int) bool {
			key1, ok1 := oldKeys[i].(map[string]interface{})
			key2, ok2 := oldKeys[j].(map[string]interface{})
			if ok1 && ok2 {
				key1Id, kid1Exists := key1["kid"]
				key2Id, kid2Exists := key2["kid"]
				if kid1Exists && kid2Exists {
					key1IdStr, ok1 := key1Id.(string)
					key2IdStr, ok2 := key2Id.(string)
					if ok1 && ok2 {
						return key1IdStr < key2IdStr
					}
				}
			}
			return len(key1) < len(key2)
		})
		sort.Slice(newKeys, func(i, j int) bool {
			key1, ok1 := newKeys[i].(map[string]interface{})
			key2, ok2 := newKeys[j].(map[string]interface{})
			if ok1 && ok2 {
				key1Id, kid1Exists := key1["kid"]
				key2Id, kid2Exists := key2["kid"]
				if kid1Exists && kid2Exists {
					key1IdStr, ok1 := key1Id.(string)
					key2IdStr, ok2 := key2Id.(string)
					if ok1 && ok2 {
						return key1IdStr < key2IdStr
					}
				}
			}
			return len(key1) < len(key2)
		})

		// Once sorted, return the result of deep comparison of the arrays of keys
		return !reflect.DeepEqual(oldKeys, newKeys), nil
	}

	// If we aren't able to compare using keys, we should return true
	// since we already checked exact equality of the responses
	return true, nil
}
