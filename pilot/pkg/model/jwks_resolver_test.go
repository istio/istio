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
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"go.opencensus.io/stats/view"

	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pkg/test/util/retry"
)

const testRetryInterval = time.Millisecond * 10

func addKeytoCache(r *JwksResolver, issuer string, url string, key jwtKey) {
	pk, err := r.GetPublicKey(issuer, url)

	if err == nil && pk == "" {
		ticker := time.NewTicker(1 * time.Second)
		counter := 0
		for range ticker.C {
			if val, found := r.keyEntries.Load(key); found {
				if counter == 5 {
					ticker.Stop()
					break
				}
				e := val.(jwtPubKeyEntry)
				if e.pubKey != "" {
					ticker.Stop()
					break
				}
				counter++
			}
		}

	}
}

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
			in:              ms.URL, // Send two same request, mock server is expected to hit twice.
			expectedJwksURI: mockCertURL,
		},
		{
			in:            "http://xyz",
			expectedError: true,
		},
	}
	for _, c := range cases {
		jwksURI, err := r.resolveJwksURIUsingOpenID(c.in)
		if err != nil && !c.expectedError {
			t.Errorf("resolveJwksURIUsingOpenID(%+v): got error (%v)", c.in, err)
		} else if err == nil && c.expectedError {
			t.Errorf("resolveJwksURIUsingOpenID(%+v): expected error, got no error", c.in)
		} else if c.expectedJwksURI != jwksURI {
			t.Errorf("resolveJwksURIUsingOpenID(%+v): expected (%s), got (%s)",
				c.in, c.expectedJwksURI, jwksURI)
		}
	}

	// Verify mock openID discovery http://localhost:9999/.well-known/openid-configuration was called twice.
	if got, want := ms.OpenIDHitNum, uint64(2); got != want {
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

	cases := []struct {
		in                []string
		expectedJwtPubkey string
	}{
		{
			in:                []string{"testIssuer", mockCertURL},
			expectedJwtPubkey: test.JwtPubKey1,
		},
		{
			in:                []string{"testIssuer", mockCertURL}, // Send two same request, mock server is expected to hit only once because of the cache.
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	key := jwtKey{issuer: cases[0].in[0], jwksURI: cases[0].in[1]}
	addKeytoCache(r, cases[0].in[0], cases[0].in[1], key)
	for _, c := range cases {
		val, found := r.keyEntries.Load(key)
		if found && val.(jwtPubKeyEntry).err == nil {
			pk := val.(jwtPubKeyEntry).pubKey
			if c.expectedJwtPubkey != pk {
				t.Errorf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
			}
		} else {
			t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", c.in, err)
		}
	}

	// Verify mock server http://localhost:9999/oauth2/v3/certs was only called once because of the cache.
	if got, want := ms.PubKeyHitNum, uint64(1); got != want {
		t.Errorf("Mock server Hit number => expected %d but got %d", want, got)
	}
}

func TestGetPublicKeyReorderedKey(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)

	ms, err := test.StartNewServer()

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
	key := jwtKey{issuer: cases[0].in[0], jwksURI: cases[0].in[1]}
	addKeytoCache(r, cases[0].in[0], cases[0].in[1], key)
	r.Close()

	for _, c := range cases {
		val, found := r.keyEntries.Load(key)
		if found && val.(jwtPubKeyEntry).err == nil {
			pk := val.(jwtPubKeyEntry).pubKey
			if c.expectedJwtPubkey != pk {
				t.Errorf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
			}
		} else {
			t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", c.in, val.(jwtPubKeyEntry).err)
		}
		r.refresh()
	}

	// Verify refresh job key changed count is zero.
	if got, want := r.refreshJobKeyChangedCount, uint64(0); got != want {
		t.Errorf("JWKs Resolver Refreshed Key Count => expected %d but got %d", want, got)
	}
	ms.Stop()
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
	key := jwtKey{issuer: "", jwksURI: mockCertURL}
	addKeytoCache(r, "", mockCertURL, key)
	val, found := r.keyEntries.Load(key)
	if found && val.(jwtPubKeyEntry).err != nil {
		t.Errorf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	if test.JwtPubKey1 != val.(jwtPubKeyEntry).pubKey {
		t.Errorf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, val.(jwtPubKeyEntry).pubKey)
	}
}

func TestGetPublicKeyUsingTLSBadCert(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(
		JwtPubKeyEvictionDuration,
		JwtPubKeyRefreshInterval,
		testRetryInterval,
		JwtPubKeyRefreshIntervalOnFailure,
		[]string{"./test/testcert/cert2.pem"},
	)
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	key := jwtKey{issuer: "", jwksURI: mockCertURL}
	addKeytoCache(r, "", mockCertURL, key)
	val, found := r.keyEntries.Load(key)
	if found && val.(jwtPubKeyEntry).err == nil {
		t.Errorf("GetPublicKey(\"\", %+v) did not fail: expected bad certificate error, got no error", mockCertURL)
	}

}

func TestGetPublicKeyUsingTLSWithoutCABundles(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(
		JwtPubKeyEvictionDuration,
		JwtPubKeyRefreshInterval,
		testRetryInterval,
		JwtPubKeyRefreshIntervalOnFailure,
		[]string{},
	)
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	key := jwtKey{issuer: "", jwksURI: mockCertURL}
	addKeytoCache(r, "", mockCertURL, key)

	val, found := r.keyEntries.Load(key)
	if found && val.(jwtPubKeyEntry).err == nil {
		t.Errorf("GetPublicKey(\"\", %+v) did not fail: expected bad certificate error, got no error", mockCertURL)
	}
}

func TestJwtPubKeyEvictionForNotUsed(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)

	evictDuration := 100 * time.Millisecond
	refreshInterval := 2 * time.Millisecond
	refreshIntOnFail := 2 * time.Millisecond

	defer r.Close()

	ms := startMockServer(t)

	// Mock server returns JwtPubKey2 for later calls.
	// Verify the refresher has run and got new key from mock server.
	verifyKeyRefresh(t, r, ms, test.JwtPubKey2, evictDuration, refreshInterval, refreshIntOnFail)
	defer ms.Stop()

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
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, JwtPubKeyRefreshIntervalOnFailure, testRetryInterval)

	defer r.Close()
	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error after the first request.
	ms.ReturnErrorAfterFirstNumHits = 1

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	key := jwtKey{issuer: "", jwksURI: mockCertURL}
	addKeytoCache(r, "", mockCertURL, key)

	val, found := r.keyEntries.Load(key)
	if found && val.(jwtPubKeyEntry).err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, val.(jwtPubKeyEntry).err)
	}
	// Mock server returns JwtPubKey1 for first call.
	if test.JwtPubKey1 != val.(jwtPubKeyEntry).pubKey {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, val.(jwtPubKeyEntry).pubKey)
	}

	r.evictionDuration = 100 * time.Millisecond
	r.refreshInterval = 10 * time.Millisecond
	r.refreshIntervalOnFailure = 10 * time.Millisecond

	// Keep getting the public key to change the lastUsedTime of the public key.
	done := make(chan struct{})
	go func() {
		c := time.NewTicker(10 * time.Millisecond)
		for {
			select {
			case <-done:
				return
			case <-c.C:
				_, _ = r.GetPublicKey(mockCertURL, "")
			}
		}
	}()
	defer func() {
		done <- struct{}{}
	}()

	// Verify the cached public key is removed after failed to refresh longer than the eviction duration.
	retry.UntilSuccessOrFail(t, func() error {
		_, err := r.GetPublicKey(mockCertURL, "")
		if err == nil {
			return fmt.Errorf("getPublicKey(\"\", %+v) fails: expected error, got no error", mockCertURL)
		}
		return nil
	})
}

func TestJwtPubKeyLastRefreshedTime(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, time.Second, time.Second, testRetryInterval)
	fmt.Println(JwtPubKeyRefreshInterval)
	ms := startMockServer(t)

	// Mock server returns JwtPubKey2 for later calls.
	// Verify the refresher has run and got new key from mock server.
	verifyKeyRefresh(t, r, ms, test.JwtPubKey2, r.evictionDuration, 2*time.Millisecond, 2*time.Millisecond)

	// The lastRefreshedTime should change for each successful refresh.
	verifyKeyLastRefreshedTime(t, r, ms, true /* wantChanged */)
	ms.Stop()
	r.Close()
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
	verifyKeyRefresh(t, r, ms, test.JwtPubKey1, r.evictionDuration, time.Second, time.Second)

	// The lastRefreshedTime should not change the refresh failed due to network error.
	verifyKeyLastRefreshedTime(t, r, ms, false /* wantChanged */)
}

func TestJwtRefreshIntervalRecoverFromInitialFailOnFirstHit(t *testing.T) {
	defaultRefreshInterval := 50 * time.Millisecond
	refreshIntervalOnFail := 2 * time.Millisecond
	r := NewJwksResolver(JwtPubKeyEvictionDuration, defaultRefreshInterval, refreshIntervalOnFail, 1*time.Millisecond)

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error for the first 5 requests.
	ms.ReturnErrorForFirstNumHits = 5

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	// pk, err := r.GetPublicKey("", mockCertURL)
	key := jwtKey{issuer: "", jwksURI: mockCertURL}
	addKeytoCache(r, "", mockCertURL, key)
	val, found := r.keyEntries.Load(key)
	if found && val.(jwtPubKeyEntry).err == nil {
		t.Fatalf("GetPublicKey(%q, %+v) fails: expected error, got no error: (%v)", val.(jwtPubKeyEntry).pubKey, mockCertURL, val.(jwtPubKeyEntry).err)
	}

	retry.UntilOrFail(t, func() bool {
		// pk, _ := r.GetPublicKey("", mockCertURL)
		addKeytoCache(r, "", mockCertURL, key)
		val, found := r.keyEntries.Load(key)
		if found && test.JwtPubKey2 == val.(jwtPubKeyEntry).pubKey {
			return true
		}
		return false
	}, retry.Delay(time.Millisecond))
	r.Close()

	i := 0
	r.keyEntries.Range(func(_ interface{}, _ interface{}) bool {
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
	defaultRefreshInterval := time.Second
	refreshIntervalOnFail := 2 * time.Millisecond
	r := NewJwksResolver(JwtPubKeyEvictionDuration, defaultRefreshInterval, refreshIntervalOnFail, 1*time.Millisecond)

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error after the first request.
	ms.ReturnErrorAfterFirstNumHits = 1
	ms.ReturnSuccessAfterFirstNumHits = 3

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	key := jwtKey{issuer: "", jwksURI: mockCertURL}
	addKeytoCache(r, "", mockCertURL, key)
	val, found := r.keyEntries.Load(key)
	if found && val.(jwtPubKeyEntry).err != nil {
		t.Fatalf("GetPublicKey(%q, %+v) fails: expected no error, got (%v)", "", mockCertURL, val.(jwtPubKeyEntry).err)
	}

	retry.UntilOrFail(t, func() bool {
		addKeytoCache(r, "", mockCertURL, key)
		val, found := r.keyEntries.Load(key)
		if found && test.JwtPubKey1 == val.(jwtPubKeyEntry).pubKey {
			return true
		}
		return false
	}, retry.Delay(time.Millisecond))
	r.Close()

	if r.refreshInterval != defaultRefreshInterval {
		t.Errorf("expected defaultRefreshInterval: %v , got %v", defaultRefreshInterval, r.refreshInterval)
	}
}

func getCounterValue(counterName string, t *testing.T) float64 {
	counterValue := 0.0
	if data, err := view.RetrieveData(counterName); err == nil {
		if len(data) != 0 {
			counterValue = data[0].Data.(*view.SumData).Value
		}
	} else {
		t.Fatalf("failed to get value for counter %s: %v", counterName, err)
	}
	return counterValue
}

func TestJwtPubKeyMetric(t *testing.T) {
	defaultRefreshInterval := 50 * time.Millisecond
	refreshIntervalOnFail := 2 * time.Millisecond
	r := NewJwksResolver(JwtPubKeyEvictionDuration, defaultRefreshInterval, refreshIntervalOnFail, 1*time.Millisecond)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	ms.ReturnErrorForFirstNumHits = 5

	successValueBefore := getCounterValue(networkFetchSuccessCounter.Name(), t)
	failValueBefore := getCounterValue(networkFetchFailCounter.Name(), t)

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	cases := []struct {
		in                []string
		expectedJwtPubkey string
	}{
		{
			in:                []string{"", mockCertURL},
			expectedJwtPubkey: "",
		},
		{
			in:                []string{"", mockCertURL},
			expectedJwtPubkey: test.JwtPubKey2,
		},
	}

	for _, c := range cases {
		retry.UntilOrFail(t, func() bool {
			key := jwtKey{issuer: c.in[0], jwksURI: c.in[1]}
			addKeytoCache(r, "", mockCertURL, key)
			val, found := r.keyEntries.Load(key)
			if found && c.expectedJwtPubkey == val.(jwtPubKeyEntry).pubKey {
				return true
			}
			return false
		}, retry.Delay(time.Millisecond))
	}

	successValueAfter := getCounterValue(networkFetchSuccessCounter.Name(), t)
	failValueAfter := getCounterValue(networkFetchFailCounter.Name(), t)
	if successValueBefore >= successValueAfter {
		t.Errorf("the success counter is not incremented")
	}
	if failValueBefore >= failValueAfter {
		t.Errorf("the fail counter is not incremented")
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

func verifyKeyRefresh(t *testing.T, r *JwksResolver, ms *test.MockOpenIDDiscoveryServer, expectedJwtPubkey string, evictionDuration time.Duration,
	refreshInterval time.Duration, refreshIntervalOnFailure time.Duration) {
	t.Helper()
	mockCertURL := ms.URL + "/oauth2/v3/certs"
	k := jwtKey{issuer: "", jwksURI: mockCertURL}

	addKeytoCache(r, "", mockCertURL, k)
	pk, err := r.GetPublicKey("", mockCertURL)

	if err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}

	// Mock server returns JwtPubKey1 for first call.
	if test.JwtPubKey1 != pk {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}
	r.evictionDuration = evictionDuration
	r.refreshInterval = refreshInterval
	r.refreshIntervalOnFailure = refreshIntervalOnFailure
	r.refreshTicker.Reset(r.refreshInterval)
	// Wait until refresh job at least finished once.
	retry.UntilSuccessOrFail(t, func() error {
		// Make sure refresh job has run and detect change or refresh happened.
		if atomic.LoadUint64(&r.refreshJobKeyChangedCount) > 0 || atomic.LoadUint64(&r.refreshJobFetchFailedCount) > 0 {
			return nil
		}
		return fmt.Errorf("refresher failed to run")
	})
	pk, err = r.GetPublicKey("", mockCertURL)
	if err != nil {
		t.Fatalf("GetPublicKey(\"\", %+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	if expectedJwtPubkey != pk {
		t.Fatalf("GetPublicKey(\"\", %+v): expected (%s), got (%s)", mockCertURL, expectedJwtPubkey, pk)
	}
}

func verifyKeyLastRefreshedTime(t *testing.T, r *JwksResolver, ms *test.MockOpenIDDiscoveryServer, wantChanged bool) {
	t.Helper()
	fmt.Println("insde")
	mockCertURL := ms.URL + "/oauth2/v3/certs"
	key := jwtKey{jwksURI: mockCertURL}

	e, found := r.keyEntries.Load(key)
	if !found {
		t.Fatalf("No cached public key for %+v", key)
	}
	oldRefreshedTime := e.(jwtPubKeyEntry).lastRefreshedTime

	time.Sleep(time.Second)

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
