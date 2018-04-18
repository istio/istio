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
	"testing"
	"time"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model/test"
)

func TestResolveJwksURIUsingOpenID(t *testing.T) {
	r := newJwksURIResolver()

	ms := test.NewServer(9999)
	if err := ms.Start(); err != nil {
		t.Fatal("failed to start mock openID discovery server")
	}
	defer func() {
		_ = ms.Stop()
	}()

	cases := []struct {
		in                   string
		expectedJwksURI      string
		expectedErrorMessage string
	}{
		{
			in:              "http://localhost:9999",
			expectedJwksURI: "http://localhost:9999/oauth2/v3/certs",
		},
		{
			in:              "http://localhost:9999", // Send two same request, mock server is expected to hit only once because of the cache.
			expectedJwksURI: "http://localhost:9999/oauth2/v3/certs",
		},
		{
			in:                   "http://xyz",
			expectedErrorMessage: "no such host",
		},
	}
	for _, c := range cases {
		jwksURI, err := r.resolveJwksURIUsingOpenID(c.in)
		if err != nil {
			if !strings.Contains(err.Error(), c.expectedErrorMessage) {
				t.Errorf("resolveJwksURIUsingOpenID(%+v): expected error (%s), got (%v)", c.in, c.expectedErrorMessage, err)
			}
		} else {
			if c.expectedErrorMessage != "" {
				t.Errorf("resolveJwksURIUsingOpenID(%+v): expected error (%s), got no error", c.in, c.expectedErrorMessage)
			}
			if c.expectedJwksURI != jwksURI {
				t.Errorf("resolveJwksURIUsingOpenID(%+v): expected (%s), got (%s)",
					c.in, c.expectedJwksURI, jwksURI)
			}
		}
	}

	// Verify mock openID discovery http://localhost:9999/.well-known/openid-configuration was only called once because of the cache.
	if got, want := ms.OpenIDHitNum, 1; got != want {
		t.Errorf("Mock OpenID discovery Hit number => expected %d but got %d", want, got)
	}
}

func TestSetAuthenticationPolicyJwksURIs(t *testing.T) {
	r := newJwksURIResolver()

	ms := test.NewServer(9999)
	if err := ms.Start(); err != nil {
		t.Fatal("failed to start mock openID discovery server")
	}
	defer func() {
		_ = ms.Stop()
	}()

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
						Issuer: "http://localhost:9999",
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
			expected: "http://localhost:9999/oauth2/v3/certs",
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

func TestResolveJwtPubKey(t *testing.T) {
	r := newJwtPubKeyResolver(newJwksURIResolver(), JwtPubKeyExpireDuration, JwtPubKeyRefreshInterval)
	defer r.Close()

	ms := test.NewServer(9999)
	if err := ms.Start(); err != nil {
		t.Fatal("failed to start mock server")
	}
	defer func() {
		_ = ms.Stop()
	}()

	cases := []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in:                "http://localhost:9999",
			expectedJwtPubkey: test.JwtPubKey1,
		},
		{
			in:                "http://localhost:9999", // Send two same request, mock server is expected to hit only once because of the cache.
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	for _, c := range cases {
		pk, err := r.ResolveJwtPubKey(c.in)
		if err != nil {
			t.Errorf("ResolveJwtPubKey(%+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("ResolveJwtPubKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
	}

	// Verify mock server http://localhost:9999/oauth2/v3/certs was only called once because of the cache.
	if got, want := ms.PubKeyHitNum, 1; got != want {
		t.Errorf("Mock server Hit number => expected %d but got %d", want, got)
	}
}

func TestJwtPubKeyRefresh(t *testing.T) {
	r := newJwtPubKeyResolver(newJwksURIResolver(), time.Millisecond /*ExpireDuration*/, 2*time.Millisecond /*RefreshInterval*/)
	defer r.Close()

	ms := test.NewServer(9999)
	if err := ms.Start(); err != nil {
		t.Fatal("failed to start mock server")
	}
	defer func() {
		_ = ms.Stop()
	}()

	cases := []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in: "http://localhost:9999",
			// Mock server returns JwtPubKey1 for first call.
			expectedJwtPubkey: test.JwtPubKey1,
		},
	}
	for _, c := range cases {
		pk, err := r.ResolveJwtPubKey(c.in)
		if err != nil {
			t.Errorf("ResolveJwtPubKey(%+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("ResolveJwtPubKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
	}

	// Sleep so that refresher job has chance to run.
	time.Sleep(10 * time.Millisecond)

	cases = []struct {
		in                string
		expectedJwtPubkey string
	}{
		{
			in: "http://localhost:9999",
			// Mock server returns JwtPubKey2 for later calls.
			// Verify the refresher has run and got new key from mock server.
			expectedJwtPubkey: test.JwtPubKey2,
		},
	}
	for _, c := range cases {
		pk, err := r.ResolveJwtPubKey(c.in)
		if err != nil {
			t.Errorf("ResolveJwtPubKey(%+v) fails: expected no error, got (%v)", c.in, err)
		}
		if c.expectedJwtPubkey != pk {
			t.Errorf("ResolveJwtPubKey(%+v): expected (%s), got (%s)", c.in, c.expectedJwtPubkey, pk)
		}
	}
}
