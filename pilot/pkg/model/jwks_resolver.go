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
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"istio.io/istio/pkg/monitoring"
)

const (
	// https://openid.net/specs/openid-connect-discovery-1_0.html
	// OpenID Providers supporting Discovery MUST make a JSON document available at the path
	// formed by concatenating the string /.well-known/openid-configuration to the Issuer.
	openIDDiscoveryCfgURLSuffix = "/.well-known/openid-configuration"

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

	// How many times should we attempt to update a cache bucket via load + compare and swap before giving up.
	JwtMaxCacheBucketUpdateCompareAndSwapAttempts = 10

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

// jwtPubKeyEntry is a single cached entry for jwt public key and the http context options
type jwtPubKeyEntry struct {
	pubKey string

	// The last success refreshed time of the pubKey.
	lastRefreshedTime time.Time

	// Cached item's last used time, which is set in GetPublicKey.
	lastUsedTime time.Time

	// OpenID Discovery web request timeout
	timeout time.Duration
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
}

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
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				DisableKeepAlives: true,
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
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				DisableKeepAlives: true,
				TLSClientConfig: &tls.Config{
					// nolint: gosec // user explicitly opted into insecure
					InsecureSkipVerify: features.JwksResolverInsecureSkipVerify,
					RootCAs:            caCertPool,
					MinVersion:         tls.VersionTLS12,
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

// GetPublicKey returns the JWT public key if it is available in the cache
// or fetch with from jwksuri if there is a error while fetching then it adds the
// jwksURI in the cache to fetch the public key in the background process
func (r *JwksResolver) GetPublicKey(issuer string, jwksURI string, timeout time.Duration) (string, error) {
	now := time.Now()
	key := jwtKey{issuer: issuer, jwksURI: jwksURI}
	if val, found := r.keyEntries.Load(key); found {
		e := val.(jwtPubKeyEntry)

		if !r.updateCacheBucket(key, func(entry *jwtPubKeyEntry) {
			entry.timeout = timeout
			if now.Sub(entry.lastUsedTime) > 0 {
				entry.lastUsedTime = now // Update if lastUsedTime is before "now"
			}
		}) {
			// Updating the cache bucket may fail if enough competing goroutines are writing to this bucket at the same time.
			// We set the number of CompareAndSwap attempts to be sufficiently large such that its most likely as a result of
			// a large number of parallel GetPublicKey calls (since the refresher will only ever have 1 goroutine writing to the cache at a given time).
			// As a result, it's likely that lastUsedTime will be approximately up to date since so many cache hits are being processed at the same time.
			log.Warnf("Failed to update lastUsedTime in the cache for %q due to write contention", jwksURI)
		}

		if e.pubKey == "" {
			return e.pubKey, errEmptyPubKeyFoundInCache
		}
		return e.pubKey, nil
	}

	var (
		err    error
		pubKey string
	)

	if jwksURI == "" && issuer == "" {
		err = fmt.Errorf("jwksURI and issuer are both empty")
		log.Errorf("Failed to fetch public key: %v", err)
	} else if jwksURI == "" {
		// Fetch the jwks URI if it is not hardcoded on config.
		jwksURI, err = r.resolveJwksURIUsingOpenID(issuer, timeout)
	}
	if err != nil {
		log.Errorf("Failed to get jwks URI from issuer %q: %v", issuer, err)
	} else {
		var resp []byte
		resp, err = r.getRemoteContentWithRetry(jwksURI, networkFetchRetryCountOnMainFlow, timeout)
		if err != nil {
			log.Errorf("Failed to fetch public key from jwks URI %q: %v", jwksURI, err)
		}
		pubKey = string(resp)
	}

	r.keyEntries.Store(key, jwtPubKeyEntry{
		pubKey:            pubKey,
		lastRefreshedTime: now,
		lastUsedTime:      now,
		timeout:           timeout,
	})
	if err != nil {
		// fetching the public key in the background
		jwksuriChannel <- key
	}
	return pubKey, err
}

// BuildLocalJwks builds local Jwks by fetching the Jwt Public Key from the URL passed if it is empty.
func (r *JwksResolver) BuildLocalJwks(jwksURI, jwtIssuer, jwtPubKey string, timeout time.Duration) *envoy_jwt.JwtProvider_LocalJwks {
	var err error
	if jwtPubKey == "" {
		// jwtKeyResolver should never be nil since the function is only called in Discovery Server request processing
		// workflow, where the JWT key resolver should have already been initialized on server creation.
		jwtPubKey, err = r.GetPublicKey(jwtIssuer, jwksURI, timeout)
		if err != nil {
			log.Infof("The JWKS key is not yet fetched for issuer %s (%s), using a fake JWKS for now", jwtIssuer, jwksURI)
			// This is a temporary workaround to reject a request with JWT token by using a fake jwks when istiod failed to fetch it.
			// TODO(xulingqing): Find a better way to reject the request without using the fake jwks.
			jwtPubKey = FakeJwks
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

// FakeJwks is a fake jwks, generated by following code
/*
	fakeJwksRSAKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	key, _ := jwk.FromRaw(fakeJwksRSAKey)
	rsaKey, _ := key.(jwk.RSAPrivateKey)
	res, _ := json.Marshal(rsaKey)
	fmt.Printf("{\"keys\":[ %s]}\n", string(res))
*/
// it should be static across different instances and versions.
// more details can be found: https://github.com/istio/istio/pull/47661.
// nolint: lll
const FakeJwks = `{
  "keys": [
    {
      "d": "T6cYL1_1mWHQLtOcbOgWV6HjhS0HVh3Apt4xEar5beaMBX3IYLFITz684DOHNy5dzaxTRqvGj-zHEgNrgy2T-Izoo2Z-xJ2Zse6wQ4R0xbwd0by8IbhiePcjgNWXXzildMHkBVrxNZhUICpb_r8efTHZfEwc6FPjJDVgJKtEc6WGCOiWnRYcGTTlsB5-QrQQlDFLmrU2Z6QDmqJU33aDJFr_qzmRiVNXeHuhlNca2JnKNPpxjRVsy7Kbc8PorxiPijnLzV8_pccsMyLvA8pWUl5FRtAJNSss7x_81HEcInlj7yA896zMiELSPps1rW68yVvpuKEuYulzGi4z74gz0Q",
      "dp": "YkH_MFMlgnGZntOCXLhib1LLW1JJCYmTzebn-JSluFJbG_qQgzuZkUu5s2cYBHmiZkDGmnTDOAYXrOaQSgVIBQMPxMqdUf8WjRIlEb88zvKpM_Curp59wuy6MhI7Ej3xKiixHX3bIq5Qujk3ZdsDbHUi3HH56-V7cdFKccqlg6E",
      "dq": "CXCwRpRgbtqzLcsfuy-5IUZosrvEDHCrFh0C-A6OYvKpHzn8PDwb62YGddhiHzSrgr1EUgykQxiIF2xG8dBaq8xXg9Bh4G1kkgIsqJmL5DG1lwyh_-Jt4nPyiLHZ--ERc48cjj515uRpGd-CWXdIf2EWYaJNsEkiNaYEClJQIA8",
      "e": "AQAB",
      "kty": "RSA",
      "n": "vqS7RN4b34i3_5YyhygtBe33gI6GK_0ldW8WMZaunS28T-WAzJOAoZ7E9Y0mHS8vcDES0eZIUpp6Ft9sRPhOlzQfo_7l-3DnaD9LxJVKdXjE1jugxfI9YX1qJpD9S9wRZxQIhPky9UzZDkpFh_KpL6pZUt4cbPtW0VCctjqvpI11yHNk4CEbzw-RRFLMJkLFJqgPa2JPzGZ-TqJdkSDQ7UtRiKzjRcWGnAdLsTq6WabDA1Fn1JVI9TWu-YDbLufDUDco46qyPgpxAqcRQG39cWZAQzMwNEZ-Yec_WiqDYqGTU6K8BBWeEIuMhiWfxGmtqX35rb9Qk_qeYDsqqT95Pw",
      "p": "7EK8xaN7qCdWCeQ1ptXWvuc6qotZc6oD-j1ecgel9FqmfkmaioVEbEAfP_N73QAjw-sU60sK3XK8LV4fkGUoJV-MDvmiCzy3wUPe-adSaTCxFykgOm6SPA9NKCqAh8lUm6GUm9RZkjwkv4xzZ8pJjng3d74WXx7zhTEH6yi4E00",
      "q": "zpJPbhAn79s_jPm4OhOvvPKT-ISN6EyLu_g6joh1Dzf-HCF149KKQfuLDtwDCsCNf1cE_BCb4qoHAVBLDjbqusQF019zNIFTHeUL8oMpbv-5of7km0K8oo-DQp5b8u05PKaEQu3OXmRZFwuO6dSTPvXO094X-8vm791FLcJ-4Ls",
      "qi": "SXz-JeBcTYMcO5lDBlrI9qd2eMQAYfVFDyq523L-RFhdravaxaYutT7dWk5f4Smzbh5KtvKifcFUMnV88On4HCiTrdBjLJJhIYqZQwzP8hYbXZlw4SvCtXKUrvLwLEUQaYg6bopp4VJ5c3XCZD5z3paHlZ45oCDsMeSEWxAD6lo"
    }
  ]
}`

// Resolve jwks_uri through openID discovery.
func (r *JwksResolver) resolveJwksURIUsingOpenID(issuer string, timeout time.Duration) (string, error) {
	// Try to get jwks_uri through OpenID Discovery.
	issuer = strings.TrimSuffix(issuer, "/")
	body, err := r.getRemoteContentWithRetry(issuer+openIDDiscoveryCfgURLSuffix, networkFetchRetryCountOnMainFlow, timeout)
	if err != nil {
		log.Errorf("Failed to fetch jwks_uri from %q: %v", issuer+openIDDiscoveryCfgURLSuffix, err)
		return "", err
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}

	jwksURI, ok := data["jwks_uri"].(string)
	if !ok {
		return "", fmt.Errorf("invalid jwks_uri %v in openID discovery configuration", data["jwks_uri"])
	}

	return jwksURI, nil
}

func (r *JwksResolver) getRemoteContentWithRetry(uri string, retry int, timeout time.Duration) ([]byte, error) {
	u, err := url.Parse(uri)
	if err != nil {
		log.Errorf("Failed to parse %q", uri)
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
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

func (r *JwksResolver) updateCacheBucket(key jwtKey, updateFunc func(*jwtPubKeyEntry)) bool {
	for attempt := 0; attempt < JwtMaxCacheBucketUpdateCompareAndSwapAttempts; attempt++ {
		val, found := r.keyEntries.Load(key)
		if !found {
			return false
		}

		e := val.(jwtPubKeyEntry)
		newEntry := jwtPubKeyEntry{
			pubKey:            e.pubKey,
			timeout:           e.timeout,
			lastRefreshedTime: e.lastRefreshedTime,
			lastUsedTime:      e.lastUsedTime,
		}
		updateFunc(&newEntry)

		// CompareAndSwap will not modify the cache unless the bucket has not been altered since we loaded it. If multiple
		// goroutines are updating the bucket at the same time, one is guaranteed to succeed.
		if r.keyEntries.CompareAndSwap(key, e, newEntry) {
			return true
		}
	}

	return false
}

func (r *JwksResolver) refresher() {
	// Wake up once in a while and refresh stale items.
	r.refreshTicker = time.NewTicker(r.refreshInterval)
	lastHasError := false
	for {
		select {
		case <-r.refreshTicker.C:
			lastHasError = r.refreshCache(lastHasError)
		case <-closeChan:
			r.refreshTicker.Stop()
			return
		case <-jwksuriChannel:
			// When triggered due to an error in the main flow, only URIs without a cached value
			// get fetched, so don't modify the ticker or interval used for the background refresh.
			r.refresh(true)
		}
	}
}

func (r *JwksResolver) refreshCache(lastHasError bool) bool {
	currentHasError := r.refresh(false)
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

func (r *JwksResolver) refresh(jwksURIBackgroundChannel bool) bool {
	var wg sync.WaitGroup
	var hasChange, hasErrors atomic.Bool
	r.keyEntries.Range(func(key any, value any) bool {
		now := time.Now()
		k := key.(jwtKey)
		e := value.(jwtPubKeyEntry)

		// If the refresh was triggered by a failure in the main flow, only fetch URIs that don't
		// have an entry in the cache. Cached entries will be fetched when triggered by the
		// background refresh ticker.
		if e.pubKey != "" && jwksURIBackgroundChannel {
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
				jwksURI, err = r.resolveJwksURIUsingOpenID(k.issuer, e.timeout)
				if err != nil {
					hasErrors.Store(true)
					log.Errorf("Failed to resolve Jwks from issuer %q: %v", k.issuer, err)
					atomic.AddUint64(&r.refreshJobFetchFailedCount, 1)
					return
				}
				r.keyEntries.Delete(k)
				k.jwksURI = jwksURI
			}
			resp, err := r.getRemoteContentWithRetry(jwksURI, networkFetchRetryCountOnRefreshFlow, e.timeout)
			if err != nil {
				hasErrors.Store(true)
				log.Errorf("Failed to refresh JWT public key from %q: %v", jwksURI, err)
				atomic.AddUint64(&r.refreshJobFetchFailedCount, 1)
				if oldPubKey == "" {
					r.keyEntries.Delete(k)
				}
				return
			}
			newPubKey := string(resp)
			if !r.updateCacheBucket(k, func(entry *jwtPubKeyEntry) {
				entry.lastRefreshedTime = now
				entry.pubKey = newPubKey
			}) {
				// While unlikely, in the event we are unable to update the cached entry via compare and swap, forcefully write refreshed data to the cache.
				log.Warnf("Failed to safely update cache for JWT public key from %q due to write contention, forcefully writing refreshed JWKs to cache", jwksURI)

				lastUsedTime := e.lastUsedTime
				timeout := e.timeout
				if latestEntry, found := r.keyEntries.Load(k); found {
					lastUsedTime = latestEntry.(jwtPubKeyEntry).lastUsedTime
					timeout = latestEntry.(jwtPubKeyEntry).timeout
				}

				r.keyEntries.Store(k, jwtPubKeyEntry{
					lastRefreshedTime: now,
					pubKey:            newPubKey,
					lastUsedTime:      lastUsedTime,
					timeout:           timeout,
				})
			}

			isNewKey, err := compareJWKSResponse(oldPubKey, newPubKey)
			if err != nil {
				hasErrors.Store(true)
				log.Errorf("Failed to refresh JWT public key from %q: %v", jwksURI, err)
				return
			}
			if isNewKey {
				hasChange.Store(true)
				log.Infof("Updated cached JWT public key from %q", jwksURI)
			}
		}()

		return true
	})

	// Wait for all go routine to complete.
	wg.Wait()

	if hasChange.Load() {
		atomic.AddUint64(&r.refreshJobKeyChangedCount, 1)
		// Push public key changes to sidecars.
		if r.PushFunc != nil {
			r.PushFunc()
		}
	}
	return hasErrors.Load()
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

	var oldJWKs map[string]any
	var newJWKs map[string]any
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
	oldKeys, oldKeysExists := oldJWKs["keys"].([]any)
	newKeys, newKeysExists := newJWKs["keys"].([]any)
	if oldKeysExists && newKeysExists {
		sort.Slice(oldKeys, func(i, j int) bool {
			key1, ok1 := oldKeys[i].(map[string]any)
			key2, ok2 := oldKeys[j].(map[string]any)
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
			key1, ok1 := newKeys[i].(map[string]any)
			key2, ok2 := newKeys[j].(map[string]any)
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
