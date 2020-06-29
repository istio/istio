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
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"go.opencensus.io/stats/view"

	"istio.io/api/security/v1beta1"

	"istio.io/istio/pilot/pkg/model/test"
)

func TestResolveJwksURIUsingOpenID(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)

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
			in:              ms.URL, // Send two same request, mock server is expected to hit only once because of the cache.
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

	// Verify mock openID discovery http://localhost:9999/.well-known/openid-configuration was only called once because of the cache.
	if got, want := ms.OpenIDHitNum, uint64(1); got != want {
		t.Errorf("Mock OpenID discovery Hit number => expected %d but got %d", want, got)
	}
}

func TestResolveJwksURI(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name     string
		in       *v1beta1.RequestAuthentication
		expected []string
	}{
		{
			name: "single jwt",
			in: &v1beta1.RequestAuthentication{
				JwtRules: []*v1beta1.JWTRule{
					{
						Issuer: ms.URL,
					},
				},
			},
			expected: []string{mockCertURL},
		},
		{
			name: "duplicate single jwt",
			in: &v1beta1.RequestAuthentication{
				JwtRules: []*v1beta1.JWTRule{
					{
						Issuer: ms.URL,
					},
					{
						Issuer: ms.URL,
					},
				},
			},
			expected: []string{mockCertURL, mockCertURL},
		},
		{
			name: "bad one",
			in: &v1beta1.RequestAuthentication{
				JwtRules: []*v1beta1.JWTRule{
					{
						Issuer: "bad-one",
					},
					{
						Issuer: ms.URL,
					},
				},
			},
			expected: []string{"", mockCertURL},
		},
		{
			name: "JwksURI provided",
			in: &v1beta1.RequestAuthentication{
				JwtRules: []*v1beta1.JWTRule{
					{
						Issuer:  "jwks URI provided",
						JwksUri: "example.com",
					},
					{
						Issuer: ms.URL,
					},
				},
			},
			expected: []string{"example.com", mockCertURL},
		},
		{
			name: "Jwks provided",
			in: &v1beta1.RequestAuthentication{
				JwtRules: []*v1beta1.JWTRule{
					{
						Issuer: "jwks provided",
						Jwks:   "deadbeef",
					},
					{
						Issuer: ms.URL,
					},
				},
			},
			expected: []string{"", mockCertURL},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r.ResolveJwksURI(c.in)
			got := make([]string, 0, len(c.in.JwtRules))
			for _, rule := range c.in.JwtRules {
				got = append(got, rule.JwksUri)
			}
			if !reflect.DeepEqual(c.expected, got) {
				t.Errorf("want %v, got %v", c.expected, c.in)
			}
		})
	}
}

func TestGetPublicKey(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)
	defer r.Close()

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in:                mockCertURL,
			expectedJwtPubkey: test.JwtPubKey1,
		},
		{
			in:                mockCertURL, // Send two same request, mock server is expected to hit only once because of the cache.
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	for _, c := range cases {
		pk, err := r.GetPublicKey(c.in)
		if err != nil {
			t.Errorf("GetPublicKey(%+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("GetPublicKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
	}

	// Verify mock server http://localhost:9999/oauth2/v3/certs was only called once because of the cache.
	if got, want := ms.PubKeyHitNum, uint64(1); got != want {
		t.Errorf("Mock server Hit number => expected %d but got %d", want, got)
	}
}

func TestGetPublicKeyReorderedKey(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, time.Millisecond*20)
	defer r.Close()

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	ms.ReturnReorderedKeyAfterFirstNumHits = 1

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in:                mockCertURL,
			expectedJwtPubkey: test.JwtPubKey1,
		},
		{
			in:                mockCertURL, // Send two same request, mock server is expected to hit only once because of the cache.
			expectedJwtPubkey: test.JwtPubKey1Reordered,
		},
	}
	for _, c := range cases {
		pk, err := r.GetPublicKey(c.in)
		if err != nil {
			t.Errorf("GetPublicKey(%+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("GetPublicKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
		r.refresh()
	}

	// Verify mock server http://localhost:9999/oauth2/v3/certs was only called once because of the cache.
	if got, want := r.refreshJobKeyChangedCount, uint64(0); got != want {
		t.Errorf("JWKs Resolver Refreshed Key Count => expected %d but got %d", want, got)
	}
}

func TestGetPublicKeyUsingTLS(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, []string{"./test/testcert/cert.pem"})
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	pk, err := r.GetPublicKey(mockCertURL)
	if err != nil {
		t.Errorf("GetPublicKey(%+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	if test.JwtPubKey1 != pk {
		t.Errorf("GetPublicKey(%+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}
}

func TestGetPublicKeyUsingTLSBadCert(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, []string{"./test/testcert/cert2.pem"})
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	_, err = r.GetPublicKey(mockCertURL)
	if err == nil {
		t.Errorf("GetPublicKey(%+v) did not fail: expected bad certificate error, got no error", mockCertURL)
	}
}

func TestGetPublicKeyUsingTLSWithoutCABundles(t *testing.T) {
	r := newJwksResolverWithCABundlePaths(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval, []string{})
	defer r.Close()

	ms, err := test.StartNewTLSServer("./test/testcert/cert.pem", "./test/testcert/key.pem")
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	_, err = r.GetPublicKey(mockCertURL)
	if err == nil {
		t.Errorf("GetPublicKey(%+v) did not fail: expected https unsupported error, got no error", mockCertURL)
	}
}

func TestJwtPubKeyEvictionForNotUsed(t *testing.T) {
	r := NewJwksResolver(100*time.Millisecond /*EvictionDuration*/, 2*time.Millisecond /*RefreshInterval*/)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Mock server returns JwtPubKey2 for later calls.
	// Verify the refresher has run and got new key from mock server.
	verifyKeyRefresh(t, r, ms, test.JwtPubKey2)

	// Wait until unused keys are evicted.
	mockCertURL := ms.URL + "/oauth2/v3/certs"
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(time.Second)
		// Verify the public key is evicted.
		if _, found := r.keyEntries.Load(mockCertURL); found {
			// Retry after some sleep.
			continue
		}
		break
	}
	if retries == 3 {
		t.Errorf("Unused keys failed to be evicted")
	}
}

func TestJwtPubKeyEvictionForNotRefreshed(t *testing.T) {
	r := NewJwksResolver(2*time.Second /*EvictionDuration*/, 100*time.Millisecond /*RefreshInterval*/)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error after the first request.
	ms.ReturnErrorAfterFirstNumHits = 1

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	// Keep getting the public key to change the lastUsedTime of the public key.
	done := make(chan struct{})
	go func() {
		c := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-done:
				return
			case <-c.C:
				_, _ = r.GetPublicKey(mockCertURL)
			}
		}
	}()
	defer func() {
		done <- struct{}{}
	}()

	pk, err := r.GetPublicKey(mockCertURL)
	if err != nil {
		t.Fatalf("GetPublicKey(%+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	// Mock server returns JwtPubKey1 for first call.
	if test.JwtPubKey1 != pk {
		t.Fatalf("GetPublicKey(%+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}

	// Verify the cached public key is removed after failed to refresh longer than the eviction duration.
	time.Sleep(5 * time.Second)
	_, err = r.GetPublicKey(mockCertURL)
	if err == nil {
		t.Errorf("GetPublicKey(%+v) fails: expected error, got no error", mockCertURL)
	}
}

func TestJwtPubKeyLastRefreshedTime(t *testing.T) {
	r := NewJwksResolver(JwtPubKeyEvictionDuration, 2*time.Millisecond /*RefreshInterval*/)
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
	r := NewJwksResolver(JwtPubKeyEvictionDuration, time.Second /*RefreshInterval*/)
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
	r := NewJwksResolver(JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)
	defer r.Close()

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	ms.ReturnErrorForFirstNumHits = 1

	successValueBefore := getCounterValue(networkFetchSuccessCounter.Name(), t)
	failValueBefore := getCounterValue(networkFetchFailCounter.Name(), t)

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	cases := []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in:                mockCertURL,
			expectedJwtPubkey: "",
		},
		{
			in:                mockCertURL,
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	for _, c := range cases {
		pk, _ := r.GetPublicKey(c.in)
		if c.expectedJwtPubkey != pk {
			t.Errorf("GetPublicKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
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

func verifyKeyRefresh(t *testing.T, r *JwksResolver, ms *test.MockOpenIDDiscoveryServer, expectedJwtPubkey string) {
	t.Helper()
	mockCertURL := ms.URL + "/oauth2/v3/certs"

	pk, err := r.GetPublicKey(mockCertURL)
	if err != nil {
		t.Fatalf("GetPublicKey(%+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	// Mock server returns JwtPubKey1 for first call.
	if test.JwtPubKey1 != pk {
		t.Fatalf("GetPublicKey(%+v): expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}

	// Wait until refresh job at least finished once.
	retries := 0
	for ; retries < 20; retries++ {
		time.Sleep(time.Second)
		// Make sure refresh job has run and detect change or refresh happened.
		if atomic.LoadUint64(&r.refreshJobKeyChangedCount) > 0 || atomic.LoadUint64(&r.refreshJobFetchFailedCount) > 0 {
			break
		}
	}
	if retries == 20 {
		t.Fatalf("Refresher failed to run")
	}

	pk, err = r.GetPublicKey(mockCertURL)
	if err != nil {
		t.Fatalf("GetPublicKey(%+v) fails: expected no error, got (%v)", mockCertURL, err)
	}
	if expectedJwtPubkey != pk {
		t.Fatalf("GetPublicKey(%+v): expected (%s), got (%s)", mockCertURL, expectedJwtPubkey, pk)
	}
}

func verifyKeyLastRefreshedTime(t *testing.T, r *JwksResolver, ms *test.MockOpenIDDiscoveryServer, wantChanged bool) {
	t.Helper()
	mockCertURL := ms.URL + "/oauth2/v3/certs"

	e, found := r.keyEntries.Load(mockCertURL)
	if !found {
		t.Fatalf("No cached public key for %s", mockCertURL)
	}
	oldRefreshedTime := e.(jwtPubKeyEntry).lastRefreshedTime

	time.Sleep(200 * time.Millisecond)

	e, found = r.keyEntries.Load(mockCertURL)
	if !found {
		t.Fatalf("No cached public key for %s", mockCertURL)
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
