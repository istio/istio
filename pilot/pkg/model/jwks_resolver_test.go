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
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	testRetryInterval  = time.Millisecond * 10
	testRequestTimeout = time.Second * 5
)

func TestResolveJwksURIUsingOpenID(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)
	defer r.Close()

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	cases := []struct {
		in              string
		expectedJwksURI string
		expectedError   bool
	}{
		{
			in:              ms.URL,
			expectedJwksURI: mockCertURL,
		},
		{
			in:              ms.URL,
			expectedJwksURI: mockCertURL,
		},
		{
			// if the URL has a trailing slash, it should be handled.
			in:              ms.URL + "/",
			expectedJwksURI: mockCertURL,
		},
		{
			in:            "http://xyz",
			expectedError: true,
		},
	}
	for _, c := range cases {
		jwksURI, err := r.resolveJwksURIUsingOpenID(c.in, testRequestTimeout)
		if err != nil && !c.expectedError {
			t.Errorf("resolveJwksURIUsingOpenID(%+v): got error (%v)", c.in, err)
		} else if err == nil && c.expectedError {
			t.Errorf("resolveJwksURIUsingOpenID(%+v): expected error, got no error", c.in)
		} else if c.expectedJwksURI != jwksURI {
			t.Errorf("resolveJwksURIUsingOpenID(%+v): expected (%s), got (%s)",
				c.in, c.expectedJwksURI, jwksURI)
		}
	}

	// Verify mock openID discovery http://localhost:9999/.well-known/openid-configuration was called three times.
	if got, want := ms.OpenIDHitNum, uint64(3); got != want {
		t.Errorf("Mock OpenID discovery Hit number => expected %d but got %d", want, got)
	}
}

func TestGetPublicKey(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)
	defer r.Close()

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	var prevLastUsedTime time.Time

	// Send two identical requests: the mock server is expected to be hit the first
	// time only because the second should hit the cache.
	cases := []struct {
		in                []string
		expectedJwtPubkey string
	}{
		{
			in:                []string{"testIssuer", mockCertURL},
			expectedJwtPubkey: test.JwtPubKey1,
		},
		{
			in:                []string{"testIssuer", mockCertURL},
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	for _, c := range cases {
		pk, err := r.GetPublicKey(c.in[0], c.in[1], testRequestTimeout)
		if err != nil {
			t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}

		val, found := r.keyEntries.Load(jwtKey{issuer: c.in[0], jwksURI: c.in[1]})
		if !found {
			t.Errorf("GetPublicKey(\"\", %+v): did not produce a cache entry", c.in)
		}

		lastUsedTime := val.(jwtPubKeyEntry).lastUsedTime
		if lastUsedTime.Sub(prevLastUsedTime) <= 0 {
			t.Errorf("GetPublicKey(\"\", %+v): invocation did not update lastUsedTime in the cache", c.in)
		}

		prevLastUsedTime = lastUsedTime
	}

	// Verify mock server http://localhost:9999/oauth2/v3/certs was only called once because of the cache.
	if got, want := ms.PubKeyHitNum, uint64(1); got != want {
		t.Errorf("Mock server Hit number => expected %d but got %d", want, got)
	}
}

func TestGetPublicKeyWithEmptyIssuer(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)
	defer r.Close()

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	var prevLastUsedTime time.Time

	// Send two identical requests: the mock server is expected to be hit the first
	// time only because the second should hit the cache.
	cases := []struct {
		in                []string
		expectedJwtPubkey string
	}{
		{
			in:                []string{"", mockCertURL},
			expectedJwtPubkey: test.JwtPubKey1,
		},
		{
			in:                []string{"", mockCertURL}, // Send two same request, mock server is expected to hit only once because of the cache.
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	for _, c := range cases {
		pk, err := r.GetPublicKey(c.in[0], c.in[1], testRequestTimeout)
		if err != nil {
			t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}

		val, found := r.keyEntries.Load(jwtKey{issuer: c.in[0], jwksURI: c.in[1]})
		if !found {
			t.Errorf("GetPublicKey(\"\", %+v): did not produce a cache entry", c.in)
		}

		lastUsedTime := val.(jwtPubKeyEntry).lastUsedTime
		if lastUsedTime.Sub(prevLastUsedTime) <= 0 {
			t.Errorf("GetPublicKey(\"\", %+v): invocation did not update lastUsedTime in the cache", c.in)
		}

		prevLastUsedTime = lastUsedTime
	}

	// Verify mock server http://localhost:9999/oauth2/v3/certs was only called once because of the cache.
	if got, want := ms.PubKeyHitNum, uint64(1); got != want {
		t.Errorf("Mock server Hit number => expected %d but got %d", want, got)
	}
}

func TestGetPublicKeyWithEmptyIssuerAndEmptyJwksURI(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)
	defer r.Close()

	_, err := r.GetPublicKey("", "", testRequestTimeout)
	if err == nil {
		t.Errorf("GetPublicKey(\"\", \"\") fails: expected error, got no error")
	}
}

func TestGetPublicKeyCacheAccessTimeOutOfOrder(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)
	defer r.Close()

	existingLastUsedTime := time.Now().Add(time.Hour)

	// Don't expect to actually hit the network on this test run, so
	key := jwtKey{issuer: "testIssuer", jwksURI: "https://localhost/some/arbitrary/jwks"}
	r.keyEntries.Store(key, jwtPubKeyEntry{
		lastRefreshedTime: time.Now(),
		lastUsedTime:      existingLastUsedTime,
		pubKey:            "somekey",
		timeout:           5 * time.Second,
	})

	pubKey, err := r.GetPublicKey(key.issuer, key.jwksURI, time.Minute)
	if err != nil {
		t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", key, err)
	}

	if pubKey != "somekey" {
		t.Errorf("GetPublicKey(\"\", %+v) did not returned the cached publickey", key)
	}

	val, found := r.keyEntries.Load(key)
	if !found {
		t.Fatalf("cache entry not found for key: %+v", key)
	}
	if val.(jwtPubKeyEntry).lastUsedTime != existingLastUsedTime {
		t.Errorf("GetPublicKey(\"\", %+v) unexpectedly decremented lastUsedTime in the cache", key)
	}
}

func TestGetPublicKeyWithTimeout(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)
	defer r.Close()
	serverDelay := 100 * time.Millisecond
	ms, err := test.StartNewServerWithHandlerDelay(serverDelay)
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		in              []string
		timeout         time.Duration
		expectedFailure bool
	}{
		{
			in:              []string{"testIssuer", mockCertURL},
			timeout:         5 * time.Second,
			expectedFailure: false,
		},
		{
			in:              []string{"testIssuer2", mockCertURL}, // Send two same request, mock server is expected to hit only once because of the cache.
			timeout:         20 * time.Millisecond,
			expectedFailure: true,
		},
	}
	for _, c := range cases {
		_, err := r.GetPublicKey(c.in[0], c.in[1], c.timeout)
		if c.timeout < serverDelay && err == nil {
			t.Errorf("GetPublicKey(\"\", %+v) fails: did not timed out as expected", c)
		} else if c.timeout >= serverDelay && err != nil {
			t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", c, err)
		}
	}
}

func TestGetPublicKeyReorderedKey(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, testRetryInterval*20, testRetryInterval*10, testRetryInterval)
	defer r.Close()

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	ms.ReturnReorderedKeyAfterFirstNumHits = 1

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		in                []string
		expectedJwtPubkey string
	}{
		{
			in:                []string{"", mockCertURL},
			expectedJwtPubkey: test.JwtPubKey1,
		},
		{
			in:                []string{"", mockCertURL}, // Send two same request, mock server is expected to hit only once because of the cache.
			expectedJwtPubkey: test.JwtPubKey1Reordered,
		},
	}
	for _, c := range cases {
		pk, err := r.GetPublicKey(c.in[0], c.in[1], testRequestTimeout)
		if err != nil {
			t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
		r.refresh(false)
	}

	// Verify refresh job key changed count is zero.
	if got, want := r.refreshJobKeyChangedCount, uint64(0); got != want {
		t.Errorf("JWKs Resolver Refreshed Key Count => expected %d but got %d", want, got)
	}
}

func TestGetPublicKeyUsingTLS(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(
		JwtPubKeyEvictionDuration,
		JwtPubKeyRefreshInterval,
		JwtPubKeyRefreshIntervalOnFailure,
		testRetryInterval,
		[]string{"./test/testcert/cert.pem"},
	)
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	pk, err := r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err != nil {
		t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	if test.JwtPubKey1 != pk {
		t.Errorf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}
}

func TestGetPublicKeyUsingTLSBadCert(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(
		JwtPubKeyEvictionDuration,
		JwtPubKeyRefreshInterval,
		testRetryInterval,
		testRetryInterval,
		[]string{"./test/testcert/cert2.pem"},
	)
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	_, err = r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err == nil {
		t.Errorf("GetPublicKey(\"\", %+v) did not fail: expected bad certificate error, got no error", mockCertURL)
	}
}

func TestGetPublicKeyUsingTLSWithoutCABundles(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(
		JwtPubKeyEvictionDuration,
		JwtPubKeyRefreshInterval,
		testRetryInterval,
		testRetryInterval,
		[]string{},
	)
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	_, err = r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err == nil {
		t.Errorf("GetPublicKey(\"\", %+v) did not fail: expected https unsupported error, got no error", mockCertURL)
	}
}

func TestJwtPubKeyEvictionForNotUsed(t *testing.T) {
	r := NewJwksResolver(
		100*time.Millisecond, /*EvictionDuration*/
		2*time.Millisecond,   /*RefreshInterval*/
		2*time.Millisecond,   /*RefreshIntervalOnFailure*/
		testRetryInterval,
	)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Mock server returns JwtPubKey2 for later calls.
	// Verify the refresher has run and got new key from mock server.
	verifyKeyRefresh(t, r, ms, test.JwtPubKey2)

	// Wait until unused keys are evicted.
	key := jwtKey{jwksURI: ms.URL + "/oauth2/v3/certs", issuer: "istio-test"}

	retry.UntilSuccessOrFail(t, func() error {
		// Verify the public key is evicted.
		if _, found := r.keyEntries.Load(key); found {
			return fmt.Errorf("public key is not evicted")
		}
		return nil
	})
}

func TestJwtPubKeyEvictionForNotRefreshed(t *testing.T) {
	r := NewJwksResolver(
		100*time.Millisecond, /*EvictionDuration*/
		10*time.Millisecond,  /*RefreshInterval*/
		10*time.Millisecond,  /*RefreshIntervalOnFailure*/
		testRetryInterval,    /*RetryInterval*/
	)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error after the first request.
	ms.ReturnErrorAfterFirstNumHits = 1

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	pk, err := r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	// Mock server returns JwtPubKey1 for first call.
	if test.JwtPubKey1 != pk {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}

	// Keep getting the public key to change the lastUsedTime of the public key.
	done := make(chan struct{})
	go func() {
		c := time.NewTicker(10 * time.Millisecond)
		for {
			select {
			case <-done:
				c.Stop()
				return
			case <-c.C:
				_, _ = r.GetPublicKey(mockCertURL, "", testRequestTimeout)
			}
		}
	}()
	defer func() {
		done <- struct{}{}
	}()

	// Verify the cached public key is removed after failed to refresh longer than the eviction duration.
	retry.UntilSuccessOrFail(t, func() error {
		_, err = r.GetPublicKey(mockCertURL, "", testRequestTimeout)
		if err == nil {
			return fmt.Errorf("getPublicKey(\"\", %+v) fails: expected error, got no error", mockCertURL)
		}
		return nil
	})
}

func TestJwtPubKeyLastRefreshedTime(t *testing.T) {
	r := NewJwksResolver(
		JwtPubKeyEvictionDuration,
		2*time.Millisecond, /*RefreshInterval*/
		2*time.Millisecond, /*RefreshIntervalOnFailure*/
		testRetryInterval,  /*RetryInterval*/
	)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Mock server returns JwtPubKey2 for later calls.
	// Verify the refresher has run and got new key from mock server.
	verifyKeyRefresh(t, r, ms, test.JwtPubKey2)

	// The lastRefreshedTime should change for each successful refresh.
	verifyKeyLastRefreshedTime(t, r, ms, true /* wantChanged */)
}

func TestJwtPubKeyRefreshWithNetworkError(t *testing.T) {
	r := NewJwksResolver(
		JwtPubKeyEvictionDuration,
		time.Second, /*RefreshInterval*/
		time.Second, /*RefreshIntervalOnFailure*/
		testRetryInterval,
	)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error after the first request.
	ms.ReturnErrorAfterFirstNumHits = 1

	// The refresh job should continue using the previously fetched public key (JwtPubKey1).
	verifyKeyRefresh(t, r, ms, test.JwtPubKey1)

	// The lastRefreshedTime should not change the refresh failed due to network error.
	verifyKeyLastRefreshedTime(t, r, ms, false /* wantChanged */)
}

func TestJwtRefreshIntervalRecoverFromInitialFailOnFirstHit(t *testing.T) {
	defaultRefreshInterval := 50 * time.Millisecond
	refreshIntervalOnFail := 2 * time.Millisecond
	r := NewJwksResolver(JwtPubKeyEvictionDuration, defaultRefreshInterval, refreshIntervalOnFail, 1*time.Millisecond)

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error for the first 3 requests.
	ms.ReturnErrorForFirstNumHits = 3

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	pk, err := r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err == nil {
		t.Fatalf("GetPublicKey(%q, %+v) fails: expected error, got no error: (%v)", pk, mockCertURL, err)
	}

	retry.UntilOrFail(t, func() bool {
		pk, _ := r.GetPublicKey("", mockCertURL, testRequestTimeout)
		return test.JwtPubKey2 == pk
	}, retry.Delay(time.Millisecond))
	r.Close()

	i := 0
	r.keyEntries.Range(func(_ any, _ any) bool {
		i++
		return true
	})

	expectedEntries := 1
	if i != expectedEntries {
		t.Errorf("expected entries in cache: %d , got %d", expectedEntries, i)
	}

	if r.refreshInterval != defaultRefreshInterval {
		t.Errorf("expected refreshInterval to be refreshDefaultInterval: %v, got %v", defaultRefreshInterval, r.refreshInterval)
	}
}

func TestJwtRefreshIntervalRecoverFromFail(t *testing.T) {
	defaultRefreshInterval := 50 * time.Millisecond
	refreshIntervalOnFail := 2 * time.Millisecond
	r := NewJwksResolver(JwtPubKeyEvictionDuration, defaultRefreshInterval, refreshIntervalOnFail, 1*time.Millisecond)

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error after the first request.
	ms.ReturnErrorAfterFirstNumHits = 1
	ms.ReturnSuccessAfterFirstNumHits = 3

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	_, err := r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err != nil {
		t.Fatalf("GetPublicKey(%q, %+v) fails: expected no error, got (%v)", "", mockCertURL, err)
	}

	retry.UntilOrFail(t, func() bool {
		pk, _ := r.GetPublicKey("", mockCertURL, testRequestTimeout)
		return test.JwtPubKey1 == pk
	}, retry.Delay(time.Millisecond))
	r.Close()

	if r.refreshInterval != defaultRefreshInterval {
		t.Errorf("expected defaultRefreshInterval: %v , got %v", defaultRefreshInterval, r.refreshInterval)
	}
}

func TestJwtPubKeyMetric(t *testing.T) {
	mt := monitortest.New(t)
	defaultRefreshInterval := 50 * time.Millisecond
	refreshIntervalOnFail := 2 * time.Millisecond
	r := NewJwksResolver(JwtPubKeyEvictionDuration, defaultRefreshInterval, refreshIntervalOnFail, 1*time.Millisecond)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	ms.ReturnErrorForFirstNumHits = 1

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	cases := []struct {
		name              string
		in                []string
		expectedJwtPubkey string
		metric            string
	}{
		{
			name:              "fail",
			in:                []string{"", mockCertURL},
			expectedJwtPubkey: "",
			metric:            networkFetchFailCounter.Name(),
		},
		{
			name:              "success",
			in:                []string{"", mockCertURL},
			expectedJwtPubkey: test.JwtPubKey2,
			metric:            networkFetchFailCounter.Name(),
		},
	}

	// First attempt should fail, but retries cause subsequent ones to succeed
	// Mock server only returns an error on the first attempt
	for _, c := range cases {
		retry.UntilOrFail(t, func() bool {
			pk, _ := r.GetPublicKey(c.in[0], c.in[1], testRequestTimeout)
			return c.expectedJwtPubkey == pk
		}, retry.Delay(time.Millisecond))
		mt.Assert(c.metric, nil, monitortest.AtLeast(1))
	}
}

func TestJwtPubKeyRefreshedWhenErrorsGettingOtherURLs(t *testing.T) {
	refreshInterval := 50 * time.Millisecond
	r := NewJwksResolver(
		JwtPubKeyEvictionDuration,
		refreshInterval,
		JwtPubKeyRefreshIntervalOnFailure,
		time.Millisecond, /*RetryInterval*/
	)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	mockInvalidCertURL := ms.URL + "/invalid"

	// Get a key added to the cache
	pk, err := r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	// Mock server returns JwtPubKey1 for the first call
	if test.JwtPubKey1 != pk {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}

	// Start a goroutine that requests an invalid URL repeatedly
	goroutineCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-time.After(refreshInterval / 2):
				invalidKey, err := r.GetPublicKey("", mockInvalidCertURL, testRequestTimeout)
				if err == nil {
					t.Logf("expected error for %q, but got key %q", mockInvalidCertURL, invalidKey)
				}
			case <-goroutineCtx.Done():
				return
			}
		}
	}()

	// Ensure that the good URL is still refreshed, and updated to JwtPubKey2
	time.Sleep(2 * refreshInterval)

	pk, err = r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	// Mock server returns JwtPubKey2 for later calls
	if test.JwtPubKey2 != pk {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey2, pk)
	}
}

func startMockServer(t *testing.T) *test.MockOpenIDDiscoveryServer {
	t.Helper()

	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	return ms
}

func verifyKeyRefresh(t *testing.T, r *JwksResolver, ms *test.MockOpenIDDiscoveryServer, expectedJwtPubkey string) {
	t.Helper()
	mockCertURL := ms.URL + "/oauth2/v3/certs"

	pk, err := r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	// Mock server returns JwtPubKey1 for first call.
	if test.JwtPubKey1 != pk {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}

	// Wait until refresh job at least finished once.
	retry.UntilSuccessOrFail(t, func() error {
		// Make sure refresh job has run and detect change or refresh happened.
		if atomic.LoadUint64(&r.refreshJobKeyChangedCount) > 0 || atomic.LoadUint64(&r.refreshJobFetchFailedCount) > 0 {
			return nil
		}
		return fmt.Errorf("refresher failed to run")
	})
	pk, err = r.GetPublicKey("", mockCertURL, testRequestTimeout)
	if err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	if expectedJwtPubkey != pk {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, expectedJwtPubkey, pk)
	}
}

func verifyKeyLastRefreshedTime(t *testing.T, r *JwksResolver, ms *test.MockOpenIDDiscoveryServer, wantChanged bool) {
	t.Helper()
	mockCertURL := ms.URL + "/oauth2/v3/certs"
	key := jwtKey{jwksURI: mockCertURL}

	e, found := r.keyEntries.Load(key)
	if !found {
		t.Fatalf("No cached public key for %+v", key)
	}
	oldRefreshedTime := e.(jwtPubKeyEntry).lastRefreshedTime

	time.Sleep(200 * time.Millisecond)

	e, found = r.keyEntries.Load(key)
	if !found {
		t.Fatalf("No cached public key for %+v", key)
	}
	newRefreshedTime := e.(jwtPubKeyEntry).lastRefreshedTime

	if actualChanged := oldRefreshedTime != newRefreshedTime; actualChanged != wantChanged {
		t.Errorf("Want changed: %t but got %t", wantChanged, actualChanged)
	}
}

func TestCompareJWKSResponse(t *testing.T) {
	type args struct {
		oldKeyString string
		newKeyString string
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{"testEquivalentStrings", args{test.JwtPubKey1, test.JwtPubKey1}, false, false},
		{"testReorderedKeys", args{test.JwtPubKey1, test.JwtPubKey1Reordered}, false, false},
		{"testDifferentKeys", args{test.JwtPubKey1, test.JwtPubKey2}, true, false},
		{"testOldJsonParseFailure", args{"This is not JSON", test.JwtPubKey1}, true, false},
		{"testNewJsonParseFailure", args{test.JwtPubKey1, "This is not JSON"}, false, true},
		{"testNewNoKid", args{test.JwtPubKey1, test.JwtPubKeyNoKid}, true, false},
		{"testOldNoKid", args{test.JwtPubKeyNoKid, test.JwtPubKey1}, true, false},
		{"testBothNoKidSame", args{test.JwtPubKeyNoKid, test.JwtPubKeyNoKid}, false, false},
		{"testBothNoKidDifferent", args{test.JwtPubKeyNoKid, test.JwtPubKeyNoKid2}, true, false},
		{"testNewNoKeys", args{test.JwtPubKey1, test.JwtPubKeyNoKeys}, true, false},
		{"testOldNoKeys", args{test.JwtPubKeyNoKeys, test.JwtPubKey1}, true, false},
		{"testBothNoKeysSame", args{test.JwtPubKeyNoKeys, test.JwtPubKeyNoKeys}, false, false},
		{"testBothNoKeysDifferent", args{test.JwtPubKeyNoKeys, test.JwtPubKeyNoKeys2}, true, false},
		{"testNewExtraElements", args{test.JwtPubKey1, test.JwtPubKeyExtraElements}, true, false},
		{"testOldExtraElements", args{test.JwtPubKeyExtraElements, test.JwtPubKey1}, true, false},
		{"testBothExtraElements", args{test.JwtPubKeyExtraElements, test.JwtPubKeyExtraElements}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compareJWKSResponse(tt.args.oldKeyString, tt.args.newKeyString)
			if (err != nil) != tt.wantErr {
				t.Errorf("compareJWKSResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("compareJWKSResponse() got = %v, want %v", got, tt.want)
			}
		})
	}
}
