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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/pkg/monitoring"
)

const (
	// https://openid.net/specs/openid-connect-discovery-1_0.html
	// OpenID Providers supporting Discovery MUST make a JSON document available at the path
	// formed by concatenating the string /.well-known/openid-configuration to the Issuer.
	openIDDiscoveryCfgURLSuffix = "/.well-known/openid-configuration"

	// OpenID Discovery web request timeout.
	jwksHTTPTimeOutInSec = 5

	// JwtPubKeyEvictionDuration is the life duration for cached item.
	// Cached item will be removed from the cache if it hasn't been used longer than JwtPubKeyEvictionDuration or if pilot
	// has failed to refresh it for more than JwtPubKeyEvictionDuration.
	JwtPubKeyEvictionDuration = 24 * 7 * time.Hour

	// JwtPubKeyRefreshIntervalOnFailure is the running interval of JWT pubKey refresh job on failure.
	JwtPubKeyRefreshIntervalOnFailure = time.Minute

	// JwtPubKeyRetryInterval is the retry interval between the attempt to retry getting the remote
	// content from network.
	JwtPubKeyRetryInterval = time.Second

	// JwtPubKeyRefreshIntervalOnFailureResetThreshold is the threshold to reset the refresh interval on failure.
	JwtPubKeyRefreshIntervalOnFailureResetThreshold = 60 * time.Minute

	// How many times should we retry the failed network fetch on main flow. The main flow
	// means it's called when Pilot is pushing configs. Do not retry to make sure not to block Pilot
	// too long.
	networkFetchRetryCountOnMainFlow = 0

	// How many times should we retry the failed network fetch on refresh flow. The refresh flow
	// means it's called when the periodically refresh job is triggered. We can retry more aggressively
	// as it's running separately from the main flow.
	networkFetchRetryCountOnRefreshFlow = 7

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

	// JwtPubKeyRefreshInterval is the running interval of JWT pubKey refresh job.
	JwtPubKeyRefreshInterval = features.PilotJwtPubKeyRefreshInterval

	// channel for making jwksuri request aynsc
	jwksuriChannel = make(chan jwtKey, 5)
)

// jwtPubKeyEntry is a single cached entry for jwt public key.
type jwtPubKeyEntry struct {
	pubKey string

	// The last success refreshed time of the pubKey.
	lastRefreshedTime time.Time

	// Cached item's last used time, which is set in GetPublicKey.
	lastUsedTime time.Time
}

// jwtKey is a key in the JwksResolver keyEntries map.
type jwtKey struct {
	jwksURI string
	issuer  string
}

// JwksResolver is resolver for jwksURI and jwt public key.
type JwksResolver struct {
	// Callback function to invoke when detecting jwt public key change.
	PushFunc func()

	// cache for JWT public key.
	// map key is jwtKey, map value is jwtPubKeyEntry.
	keyEntries sync.Map

	secureHTTPClient *http.Client
	httpClient       *http.Client
	refreshTicker    *time.Ticker

	// Cached key will be removed from cache if (time.now - cachedItem.lastUsedTime >= evictionDuration), this prevents key cache growing indefinitely.
	evictionDuration time.Duration

	// Refresher job running interval.
	refreshInterval time.Duration

	// Refresher job running interval on failure.
	refreshIntervalOnFailure time.Duration

	// Refresher job default running interval without failure.
	refreshDefaultInterval time.Duration

	retryInterval time.Duration

	// How many times refresh job has detected JWT public key change happened, used in unit test.
	refreshJobKeyChangedCount uint64

	// How many times refresh job failed to fetch the public key from network, used in unit test.
	refreshJobFetchFailedCount uint64

	// Whenever istiod fails to fetch the pubkey from jwksuri in main flow this variable becomes true for background trigger
	jwksUribackgroundChannel bool
}

func init() {
	monitoring.MustRegister(networkFetchSuccessCounter, networkFetchFailCounter)
}

// NewJwksResolver creates new instance of JwksResolver.
func NewJwksResolver(evictionDuration, refreshDefaultInterval, refreshIntervalOnFailure, retryInterval time.Duration) *JwksResolver {
	return newJwksResolverWithCABundlePaths(
		evictionDuration,
		refreshDefaultInterval,
		refreshIntervalOnFailure,
		retryInterval,
		[]string{jwksExtraRootCABundlePath},
	)
}

func newJwksResolverWithCABundlePaths(
	evictionDuration,
	refreshDefaultInterval,
	refreshIntervalOnFailure,
	retryInterval time.Duration,
	caBundlePaths []string,
) *JwksResolver {
	ret := &JwksResolver{
		evictionDuration:         evictionDuration,
		refreshInterval:          refreshDefaultInterval,
		refreshDefaultInterval:   refreshDefaultInterval,
		refreshIntervalOnFailure: refreshIntervalOnFailure,
		retryInterval:            retryInterval,
		httpClient: &http.Client{
			Timeout: jwksHTTPTimeOutInSec * time.Second,
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				DisableKeepAlives: true,
				TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			},
		},
	}

	caCertPool, err := x509.SystemCertPool()
	caCertsFound := true
	if err != nil {
		caCertsFound = false
		log.Errorf("Failed to fetch Cert from SystemCertPool: %v", err)
	}

	if caCertPool != nil {
		for _, pemFile := range caBundlePaths {
			caCert, err := os.ReadFile(pemFile)
			if err == nil {
				caCertsFound = caCertPool.AppendCertsFromPEM(caCert) || caCertsFound
			}
		}
	}

	if caCertsFound {
		ret.secureHTTPClient = &http.Client{
			Timeout: jwksHTTPTimeOutInSec * time.Second,
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
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

var errEmptyPubKeyFoundInCache = errors.New("empty public key found in cache")

// GetPublicKey returns the  JWT public key if it is available in the cache
// or fetch with from jwksuri if there is a error while fetching then it adds the
// jwksURI in the cache to fetch the public key in the background process
func (r *JwksResolver) GetPublicKey(issuer string, jwksURI string) (string, error) {
	now := time.Now()
	key := jwtKey{issuer: issuer, jwksURI: jwksURI}
	if val, found := r.keyEntries.Load(key); found {
		e := val.(jwtPubKeyEntry)
		// Update cached key's last used time.
		e.lastUsedTime = now
		r.keyEntries.Store(key, e)
		if e.pubKey == "" {
			return e.pubKey, errEmptyPubKeyFoundInCache
		}
		return e.pubKey, nil
	}

	var err error
	var pubKey string
	if jwksURI == "" {
		// Fetch the jwks URI if it is not hardcoded on config.
		jwksURI, err = r.resolveJwksURIUsingOpenID(issuer)
	}
	if err != nil {
		log.Errorf("Failed to jwks URI from %q: %v", issuer, err)
	} else {
		var resp []byte
		resp, err = r.getRemoteContentWithRetry(jwksURI, networkFetchRetryCountOnMainFlow)
		if err != nil {
			log.Errorf("Failed to fetch public key from %q: %v", jwksURI, err)
		}
		pubKey = string(resp)
	}

	r.keyEntries.Store(key, jwtPubKeyEntry{
		pubKey:            pubKey,
		lastRefreshedTime: now,
		lastUsedTime:      now,
	})
	if err != nil {
		// fetching the public key in the background
		jwksuriChannel <- key
	}
	return pubKey, err
}

// BuildLocalJwks builds local Jwks by fetching the Jwt Public Key from the URL passed if it is empty.
func (r *JwksResolver) BuildLocalJwks(jwksURI, jwtIssuer, jwtPubKey string) *envoy_jwt.JwtProvider_LocalJwks {
	var err error
	if jwtPubKey == "" {
		// jwtKeyResolver should never be nil since the function is only called in Discovery Server request processing
		// workflow, where the JWT key resolver should have already been initialized on server creation.
		jwtPubKey, err = r.GetPublicKey(jwtIssuer, jwksURI)
		if err != nil {
			log.Infof("The JWKS key is not yet fetched for issuer %s (%s), using a fake JWKS for now", jwtIssuer, jwksURI)
			// This is a temporary workaround to reject a request with JWT token by using a fake jwks when istiod failed to fetch it.
			// TODO(xulingqing): Find a better way to reject the request without using the fake jwks.
			jwtPubKey = CreateFakeJwks(jwksURI)
		}
	}
	return &envoy_jwt.JwtProvider_LocalJwks{
		LocalJwks: &core.DataSource{
			Specifier: &core.DataSource_InlineString{
				InlineString: jwtPubKey,
			},
		},
	}
}

// CreateFakeJwks is a helper function to make a fake jwks when istiod failed to fetch it.
func CreateFakeJwks(jwksURI string) string {
	// Create a fake jwksURI
	fakeJwksURI := "Error-IstiodFailedToFetchJwksUri-" + jwksURI
	// Encode jwksURI with base64 to make dynamic n in jwks
	encodedString := base64.RawURLEncoding.EncodeToString([]byte(fakeJwksURI))
	return fmt.Sprintf(`{"keys":[ {"e":"AQAB","kid":"abc","kty":"RSA","n":"%s"}]}`, encodedString)
}

// Resolve jwks_uri through openID discovery.
func (r *JwksResolver) resolveJwksURIUsingOpenID(issuer string) (string, error) {
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
		defer func() {
			if e != nil {
				networkFetchFailCounter.Increment()
			} else {
				networkFetchSuccessCounter.Increment()
			}
		}()
		resp, err := client.Get(uri)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			message := strconv.Quote(string(body))
			if len(message) > 100 {
				message = message[:100]
				return nil, fmt.Errorf("status %d, message %s(truncated)", resp.StatusCode, message)
			}
			return nil, fmt.Errorf("status %d, message %s", resp.StatusCode, message)
		}

		return body, nil
	}

	for i := 0; i < retry; i++ {
		body, err := getPublicKey()
		if err == nil {
			return body, nil
		}
		log.Warnf("Failed to GET from %q: %s. Retry in %v", uri, err, r.retryInterval)
		time.Sleep(r.retryInterval)
	}

	// Return the last fetch directly, reaching here means we have tried `retry` times, this will be
	// the last time for the retry.
	return getPublicKey()
}

func (r *JwksResolver) refresher() {
	// Wake up once in a while and refresh stale items.
	r.refreshTicker = time.NewTicker(r.refreshInterval)
	lastHasError := false
	for {
		select {
		case <-r.refreshTicker.C:
			if !r.jwksUribackgroundChannel {
				lastHasError = r.refreshCache(lastHasError)
			}
		case <-closeChan:
			r.refreshTicker.Stop()
			return
		case <-jwksuriChannel:
			r.jwksUribackgroundChannel = true
			lastHasError = r.refreshCache(lastHasError)
			r.jwksUribackgroundChannel = false
		}
	}
}

func (r *JwksResolver) refreshCache(lastHasError bool) bool {
	currentHasError := r.refresh()
	if currentHasError {
		if lastHasError {
			// update to exponential backoff if last time also failed.
			r.refreshInterval *= 2
			if r.refreshInterval > JwtPubKeyRefreshIntervalOnFailureResetThreshold {
				r.refreshInterval = JwtPubKeyRefreshIntervalOnFailureResetThreshold
			}
		} else {
			// change to the refreshIntervalOnFailure if failed for the first time.
			r.refreshInterval = r.refreshIntervalOnFailure
		}
	} else {
		// reset the refresh interval if success.
		r.refreshInterval = r.refreshDefaultInterval
	}
	r.refreshTicker.Reset(r.refreshInterval)
	return currentHasError
}

func (r *JwksResolver) refresh() bool {
	var wg sync.WaitGroup
	hasChange := false
	hasErrors := false
	r.keyEntries.Range(func(key interface{}, value interface{}) bool {
		now := time.Now()
		k := key.(jwtKey)
		e := value.(jwtPubKeyEntry)

		if e.pubKey != "" && r.jwksUribackgroundChannel {
			return true
		}
		// Remove cached item for either of the following 2 situations
		// 1) it hasn't been used for a while
		// 2) it hasn't been refreshed successfully for a while
		// This makes sure 2 things, we don't grow the cache infinitely and also we don't reuse a cached public key
		// with no success refresh for too much time.
		if now.Sub(e.lastUsedTime) >= r.evictionDuration || now.Sub(e.lastRefreshedTime) >= r.evictionDuration {
			log.Infof("Removed cached JWT public key (lastRefreshed: %s, lastUsed: %s) from %q",
				e.lastRefreshedTime, e.lastUsedTime, k.issuer)
			r.keyEntries.Delete(k)
			return true
		}

		oldPubKey := e.pubKey
		// Increment the WaitGroup counter.
		wg.Add(1)

		go func() {
			// Decrement the counter when the goroutine completes.
			defer wg.Done()
			jwksURI := k.jwksURI
			if jwksURI == "" {
				var err error
				jwksURI, err = r.resolveJwksURIUsingOpenID(k.issuer)
				if err != nil {
					hasErrors = true
					log.Errorf("Failed to resolve Jwks from issuer %q: %v", k.issuer, err)
					atomic.AddUint64(&r.refreshJobFetchFailedCount, 1)
					return
				}
				r.keyEntries.Delete(k)
				k.jwksURI = jwksURI
			}
			resp, err := r.getRemoteContentWithRetry(jwksURI, networkFetchRetryCountOnRefreshFlow)
			if err != nil {
				hasErrors = true
				log.Errorf("Failed to refresh JWT public key from %q: %v", jwksURI, err)
				atomic.AddUint64(&r.refreshJobFetchFailedCount, 1)
				if oldPubKey == "" {
					r.keyEntries.Delete(k)
				}
				return
			}
			newPubKey := string(resp)
			r.keyEntries.Store(k, jwtPubKeyEntry{
				pubKey:            newPubKey,
				lastRefreshedTime: now,            // update the lastRefreshedTime if we get a success response from the network.
				lastUsedTime:      e.lastUsedTime, // keep original lastUsedTime.
			})
			isNewKey, err := compareJWKSResponse(oldPubKey, newPubKey)
			if err != nil {
				hasErrors = true
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
	return hasErrors
}

// Close will shut down the refresher job.
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
