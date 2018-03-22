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
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha2"
	meshconfig "istio.io/api/mesh/v1alpha1"
	mccpb "istio.io/api/mixer/v1/config/client"
)

func TestRequireTls(t *testing.T) {
	cases := []struct {
		in       authn.Policy
		expected bool
	}{
		{
			in:       authn.Policy{},
			expected: false,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expected: true,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{},
				},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
			},
			expected: true,
		},
	}
	for _, c := range cases {
		if got := RequireTLS(&c.in); got != c.expected {
			t.Errorf("requireTLS(%v): got(%v) != want(%v)\n", c.in, got, c.expected)
		}
	}
}

func TestParseJwksURI(t *testing.T) {
	cases := []struct {
		in                   string
		expectedHostname     string
		expectedPort         *Port
		expectedUseSSL       bool
		expectedErrorMessage string
	}{
		{
			in:                   "foo.bar.com",
			expectedErrorMessage: "URI scheme  is not supported",
		},
		{
			in:                   "tcp://foo.bar.com:abc",
			expectedErrorMessage: "URI scheme tcp is not supported",
		},
		{
			in:                   "http://foo.bar.com:abc",
			expectedErrorMessage: `strconv.Atoi: parsing "abc": invalid syntax`,
		},
		{
			in:               "http://foo.bar.com",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "http", Port: 80},
			expectedUseSSL:   false,
		},
		{
			in:               "https://foo.bar.com",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "https", Port: 443},
			expectedUseSSL:   true,
		},
		{
			in:               "http://foo.bar.com:1234",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "http", Port: 1234},
			expectedUseSSL:   false,
		},
	}
	for _, c := range cases {
		host, port, useSSL, err := ParseJwksURI(c.in)
		if err != nil {
			if c.expectedErrorMessage != err.Error() {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got (%v)", c.in, c.expectedErrorMessage, err)
			}
		} else {
			if c.expectedErrorMessage != "" {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got no error", c.in, c.expectedErrorMessage)
			}
			if c.expectedHostname != host || !reflect.DeepEqual(c.expectedPort, port) || c.expectedUseSSL != useSSL {
				t.Errorf("ParseJwksURI(%s): expected (%s, %#v, %v), got (%s, %#v, %v)",
					c.in, c.expectedHostname, c.expectedPort, c.expectedUseSSL,
					host, port, useSSL)
			}
		}
	}
}

func TestJwksURIClusterName(t *testing.T) {
	cases := []struct {
		hostname string
		port     *Port
		expected string
	}{
		{
			hostname: "foo.bar.com",
			port:     &Port{Name: "http", Port: 80},
			expected: "jwks.foo.bar.com|http",
		},
		{
			hostname: "very.l" + strings.Repeat("o", 180) + "ng.hostname.com",
			port:     &Port{Name: "http", Port: 80},
			expected: "jwks.very.loooooooooooooooooooooooooooooooooooooooooooooooooo" +
				"oooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooo" +
				"ooooooooooooooooooooooooa96644bbd2fd09d9b6f9f0114d6c4dc792fa7efe",
		},
	}
	for _, c := range cases {
		if got := JwksURIClusterName(c.hostname, c.port); c.expected != got {
			t.Errorf("JwksURIClusterName(%s, %#v): expected (%s), got (%s)", c.hostname, c.port, c.expected, got)
		}
	}
}

func TestLegacyAuthenticationPolicyToPolicy(t *testing.T) {
	cases := []struct {
		in       meshconfig.AuthenticationPolicy
		expected *authn.Policy
	}{
		{
			in: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
			expected: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
		},
		{
			in:       meshconfig.AuthenticationPolicy_NONE,
			expected: nil,
		},
	}

	for _, c := range cases {
		if got := legacyAuthenticationPolicyToPolicy(c.in); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("legacyAuthenticationPolicyToPolicy(%v): got(%#v) != want(%#v)\n", c.in, got, c.expected)
		}
	}
}

func TestCollectJwtSpecs(t *testing.T) {
	cases := []struct {
		in           authn.Policy
		expectedSize int
	}{
		{
			in:           authn.Policy{},
			expectedSize: 0,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expectedSize: 0,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{
						Jwt: &authn.Jwt{
							JwksUri: "http://abc.com",
						},
					},
				},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
			},
			expectedSize: 1,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{
						Jwt: &authn.Jwt{
							JwksUri: "http://abc.com",
						},
					},
				},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							JwksUri: "http://xyz.com",
						},
					},
				},
			},
			expectedSize: 2,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{
						Jwt: &authn.Jwt{
							JwksUri: "http://abc.com",
						},
					},
				},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							JwksUri: "http://xyz.com",
						},
					},
					{
						Jwt: &authn.Jwt{
							JwksUri: "http://abc.com",
						},
					},
				},
			},
			expectedSize: 3,
		},
	}
	for _, c := range cases {
		if got := CollectJwtSpecs(&c.in); len(got) != c.expectedSize {
			t.Errorf("CollectJwtSpecs(%#v): return map of size (%d) != want(%d)\n", c.in, len(got), c.expectedSize)
		}
	}
}

func TestConvertPolicyToJwtConfig(t *testing.T) {
	cases := []struct {
		name     string
		in       authn.Policy
		expected *mccpb.EndUserAuthenticationPolicySpec
	}{
		{
			name:     "empty policy",
			in:       authn.Policy{},
			expected: nil,
		},
		{
			name: "no jwt policy",
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expected: nil,
		},
		{
			name: "one jwt policy",
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								Issuer:     "foo",
								Audiences:  []string{"dead", "beef"},
								JwksUri:    "http://abc.com",
								JwtHeaders: []string{"x-jwt-foo", "x-jwt-foo-another"},
							},
						},
					},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
			},
			expected: &mccpb.EndUserAuthenticationPolicySpec{
				Jwts: []*mccpb.JWT{
					{
						Issuer:                 "foo",
						Audiences:              []string{"dead", "beef"},
						JwksUri:                "http://abc.com",
						JwksUriEnvoyCluster:    "jwks.abc.com|http",
						ForwardJwt:             true,
						PublicKeyCacheDuration: &types.Duration{Seconds: 300},
						Locations: []*mccpb.JWT_Location{
							{
								Scheme: &mccpb.JWT_Location_Header{Header: "x-jwt-foo"},
							},
							{
								Scheme: &mccpb.JWT_Location_Header{Header: "x-jwt-foo-another"},
							},
						},
					},
				},
			},
		},
		{
			name: "two jwt policy",
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								Issuer:     "foo",
								Audiences:  []string{"dead", "beef"},
								JwksUri:    "http://abc.com",
								JwtHeaders: []string{"x-jwt-foo", "x-jwt-foo-another"},
							},
						},
					},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							Issuer:    "bar",
							JwksUri:   "https://xyz.com",
							JwtParams: []string{"x-jwt-bar"},
						},
					},
				},
			},
			expected: &mccpb.EndUserAuthenticationPolicySpec{
				Jwts: []*mccpb.JWT{
					{
						Issuer:                 "foo",
						Audiences:              []string{"dead", "beef"},
						JwksUri:                "http://abc.com",
						JwksUriEnvoyCluster:    "jwks.abc.com|http",
						ForwardJwt:             true,
						PublicKeyCacheDuration: &types.Duration{Seconds: 300},
						Locations: []*mccpb.JWT_Location{
							{
								Scheme: &mccpb.JWT_Location_Header{Header: "x-jwt-foo"},
							},
							{
								Scheme: &mccpb.JWT_Location_Header{Header: "x-jwt-foo-another"},
							},
						},
					},
					{
						Issuer:                 "bar",
						Audiences:              nil,
						JwksUri:                "https://xyz.com",
						JwksUriEnvoyCluster:    "jwks.xyz.com|https",
						ForwardJwt:             true,
						PublicKeyCacheDuration: &types.Duration{Seconds: 300},
						Locations: []*mccpb.JWT_Location{
							{
								Scheme: &mccpb.JWT_Location_Query{Query: "x-jwt-bar"},
							},
						},
					},
				},
			},
		},
		{
			name: "duplicate jwt policy",
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{
						Jwt: &authn.Jwt{
							JwksUri: "http://abc.com",
						},
					},
				},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							JwksUri: "https://xyz.com",
						},
					},
					{
						Jwt: &authn.Jwt{
							JwksUri: "http://abc.com",
						},
					},
				},
			},
			expected: &mccpb.EndUserAuthenticationPolicySpec{
				Jwts: []*mccpb.JWT{
					{
						JwksUri:                "http://abc.com",
						JwksUriEnvoyCluster:    "jwks.abc.com|http",
						ForwardJwt:             true,
						PublicKeyCacheDuration: &types.Duration{Seconds: 300},
					},
					{
						JwksUri:                "https://xyz.com",
						JwksUriEnvoyCluster:    "jwks.xyz.com|https",
						ForwardJwt:             true,
						PublicKeyCacheDuration: &types.Duration{Seconds: 300},
					},
					{
						JwksUri:                "http://abc.com",
						JwksUriEnvoyCluster:    "jwks.abc.com|http",
						ForwardJwt:             true,
						PublicKeyCacheDuration: &types.Duration{Seconds: 300},
					},
				},
			},
		},
	}
	for _, c := range cases {
		if got := ConvertPolicyToJwtConfig(&c.in); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("Test case %s: expected\n%#v\n, got\n%#v", c.name, c.expected.String(), got.String())
		}
	}
}
