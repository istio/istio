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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model/test"
)

func TestResolveJwksURIUsingOpenID(t *testing.T) {
	r := newJwksResolver(JwtPubKeyExpireDuration, JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)

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

func TestSetAuthenticationPolicyJwksURIs(t *testing.T) {
	r := newJwksResolver(JwtPubKeyExpireDuration, JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)

	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	authNPolicies := map[string]*authn.Policy{
		"one": {
			Targets: []*authn.TargetSelector{{
				Name: "one",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 80,
						},
					},
				},
			}},
			Origins: []*authn.OriginAuthenticationMethod{
				{
					Jwt: &authn.Jwt{
						Issuer: ms.URL,
					},
				},
			},
			PrincipalBinding: authn.PrincipalBinding_USE_ORIGIN,
		},
		"two": {
			Targets: []*authn.TargetSelector{{
				Name: "two",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 80,
						},
					},
				},
			}},
			Origins: []*authn.OriginAuthenticationMethod{
				{
					Jwt: &authn.Jwt{
						Issuer:  "http://abc",
						JwksUri: "http://xyz",
					},
				},
			},
			PrincipalBinding: authn.PrincipalBinding_USE_ORIGIN,
		},
	}

	cases := []struct {
		in       *authn.Policy
		expected string
	}{
		{
			in:       authNPolicies["one"],
			expected: mockCertURL,
		},
		{
			in:       authNPolicies["two"],
			expected: "http://xyz",
		},
	}
	for _, c := range cases {
		_ = r.SetAuthenticationPolicyJwksURIs(c.in)
		got := c.in.GetOrigins()[0].GetJwt().JwksUri
		if want := c.expected; got != want {
			t.Errorf("setAuthenticationPolicyJwksURIs(%+v): expected (%s), got (%s)", c.in, c.expected, c.in)
		}
	}
}

func TestGetPublicKey(t *testing.T) {
	r := newJwksResolver(JwtPubKeyExpireDuration, JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)
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

func TestGetPublicKeyWithRetry(t *testing.T) {
	r := newJwksResolver(JwtPubKeyExpireDuration, JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error for the first 3 requests and return the successful
	// results starting from the 4th request.
	ms.ReturnErrorForFirstNumHits = 3
	mockCertURL := ms.URL + "/oauth2/v3/certs"

	// The first request should fail for all of its 2 fetches (1 retry).
	pk, err := r.GetPublicKey(mockCertURL)
	if err == nil || !strings.Contains(err.Error(), "unsuccessful response") {
		t.Errorf("GetPublicKey(%+v) fails: expected error, got (%s)", mockCertURL, pk)
	}

	// The second request should succeed for the last of its 2 fetches (1 retry).
	pk, err = r.GetPublicKey(mockCertURL)
	if err != nil {
		t.Errorf("GetPublicKey(%+v) fails: expected no error, got (%s)", mockCertURL, err)
	} else if pk != test.JwtPubKey1 {
		t.Errorf("GetPublicKey(%+v) fails: expected (%s), got (%s)", mockCertURL, test.JwtPubKey1, pk)
	}

	// Verify mock server http://localhost:9999/oauth2/v3/certs was called 4 times because of the retry.
	if got, want := ms.PubKeyHitNum, uint64(4); got != want {
		t.Errorf("Mock server Hit number => expected %d but got %d", want, got)
	}
}

func TestJwtPubKeyRefreshAndEviction(t *testing.T) {
	r := newJwksResolver(time.Millisecond /*ExpireDuration*/, 100*time.Millisecond /*EvictionDuration*/, 2*time.Millisecond /*RefreshInterval*/)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Mock server returns JwtPubKey2 for later calls.
	// Verify the refresher has run and got new key from mock server.
	verifyPubkeyRefresh(t, r, ms, test.JwtPubKey2)

	// Verify the public key is evicted.
	checkPubkeyEviction(t, r, ms)
}

func TestJwtPubKeyRefreshWithNetworkError(t *testing.T) {
	r := newJwksResolver(JwtPubKeyExpireDuration, JwtPubKeyEvictionDuration, time.Second /*RefreshInterval*/)
	defer r.Close()

	ms := startMockServer(t)
	defer ms.Stop()

	// Configures the mock server to return error after the first requests.
	ms.ReturnErrorAfterFirstNumHits = 1

	// The refresh job should continue using the previously fetched public key (JwtPubKey1).
	verifyPubkeyRefresh(t, r, ms, test.JwtPubKey1)
}

func startMockServer(t *testing.T) *test.MockOpenIDDiscoveryServer {
	t.Helper()

	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	return ms
}

func verifyPubkeyRefresh(t *testing.T, r *jwksResolver, ms *test.MockOpenIDDiscoveryServer, expectedJwtPubkey string) {
	t.Helper()

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	cases := []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in: mockCertURL,
			// Mock server returns JwtPubKey1 for first call.
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	for _, c := range cases {
		pk, err := r.GetPublicKey(c.in)
		if err != nil {
			t.Fatalf("GetPublicKey(%+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Fatalf("GetPublicKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
	}

	// Wait until refresh job at least finished once.
	retries := 0
	for ; retries < 20; retries++ {
		time.Sleep(time.Second)
		// Make sure refresh job has run and detect change or refresh happened.
		if atomic.LoadUint64(&r.keyChangedCount) > 0 || atomic.LoadUint64(&r.keyRefreshFailedCount) > 0 {
			break
		}
	}

	if retries == 20 {
		t.Fatalf("Refresher failed to run")
	}

	cases = []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in:                mockCertURL,
			expectedJwtPubkey: expectedJwtPubkey,
		},
	}
	for _, c := range cases {
		pk, err := r.GetPublicKey(c.in)
		if err != nil {
			t.Fatalf("GetPublicKey(%+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Fatalf("GetPublicKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
	}
}

func checkPubkeyEviction(t *testing.T, r *jwksResolver, ms *test.MockOpenIDDiscoveryServer) {
	t.Helper()

	mockCertURL := ms.URL + "/oauth2/v3/certs"
	// Wait until unused keys are evicted.
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(time.Second)
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
