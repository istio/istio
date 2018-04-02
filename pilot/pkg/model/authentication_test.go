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

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	jwtfilter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/jwt_authn/v2alpha"
	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestRequireTls(t *testing.T) {
	cases := []struct {
		name           string
		in             *authn.Policy
		expected       bool
		expectedParams *authn.MutualTls
	}{
		{
			name:           "Null policy",
			in:             nil,
			expected:       false,
			expectedParams: nil,
		},
		{
			name:           "Empty policy",
			in:             &authn.Policy{},
			expected:       false,
			expectedParams: nil,
		},
		{
			name: "Policy with mTls",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expected:       true,
			expectedParams: nil,
		},
		{
			name: "Policy with mTls and Jwt",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{},
					},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								AllowTls: true,
							},
						},
					},
				},
			},
			expected:       true,
			expectedParams: &authn.MutualTls{AllowTls: true},
		},
		{
			name: "Policy with just Jwt",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{},
					},
				},
			},
			expected:       false,
			expectedParams: nil,
		},
	}
	for _, c := range cases {
		if got, params := RequireTLS(c.in); got != c.expected || !reflect.DeepEqual(c.expectedParams, params) {
			t.Errorf("%s: requireTLS(%v): got(%v, %v) != want(%v, %v)\n", c.name, c.in, got, params, c.expected, c.expectedParams)
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
			expectedErrorMessage: `URI scheme "" is not supported`,
		},
		{
			in:                   "tcp://foo.bar.com:abc",
			expectedErrorMessage: `URI scheme "tcp" is not supported`,
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
		{
			in:               "https://foo.bar.com:1234/secure/key",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "https", Port: 1234},
			expectedUseSSL:   true,
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
		expected *jwtfilter.JwtAuthentication
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
			expected: &jwtfilter.JwtAuthentication{
				Rules: []*jwtfilter.JwtRule{
					{
						Issuer:    "foo",
						Audiences: []string{"dead", "beef"},
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &core.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "jwks.abc.com|http",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward: true,
						FromHeaders: []*jwtfilter.JwtHeader{
							{
								Name: "x-jwt-foo",
							},
							{
								Name: "x-jwt-foo-another",
							},
						},
					},
				},
				AllowMissingOrFailed: true,
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
			expected: &jwtfilter.JwtAuthentication{
				Rules: []*jwtfilter.JwtRule{
					{
						Issuer:    "foo",
						Audiences: []string{"dead", "beef"},
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &core.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "jwks.abc.com|http",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward: true,
						FromHeaders: []*jwtfilter.JwtHeader{
							{
								Name: "x-jwt-foo",
							},
							{
								Name: "x-jwt-foo-another",
							},
						},
					},
					{
						Issuer: "bar",
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &core.HttpUri{
									Uri: "https://xyz.com",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "jwks.xyz.com|https",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward:    true,
						FromParams: []string{"x-jwt-bar"},
					},
				},
				AllowMissingOrFailed: true,
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
			expected: &jwtfilter.JwtAuthentication{
				Rules: []*jwtfilter.JwtRule{
					{
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &core.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "jwks.abc.com|http",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward: true,
					},
					{
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &core.HttpUri{
									Uri: "https://xyz.com",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "jwks.xyz.com|https",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward: true,
					},
					{
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &core.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "jwks.abc.com|http",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward: true,
					},
				},
				AllowMissingOrFailed: true,
			},
		},
	}
	for _, c := range cases {
		if got := ConvertPolicyToJwtConfig(&c.in); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("Test case %s: expected\n%#v\n, got\n%#v", c.name, c.expected.String(), got.String())
		}
	}
}
