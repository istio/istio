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

package authn

import (
	"reflect"
	"strings"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha1"
	authn_filter "istio.io/api/envoy/config/filter/http/authn/v2alpha1"
	jwtfilter "istio.io/api/envoy/config/filter/http/jwt_auth/v2alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
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
		if got, params := RequireTLS(c.in, model.Sidecar); got != c.expected || !reflect.DeepEqual(c.expectedParams, params) {
			t.Errorf("%s: requireTLS(%v): got(%v, %v) != want(%v, %v)\n", c.name, c.in, got, params, c.expected, c.expectedParams)
		}
	}
}

func TestJwksURIClusterName(t *testing.T) {
	cases := []struct {
		hostname string
		port     *model.Port
		expected string
	}{
		{
			hostname: "foo.bar.com",
			port:     &model.Port{Name: "http", Port: 80},
			expected: "jwks.foo.bar.com|http",
		},
		{
			hostname: "very.l" + strings.Repeat("o", 180) + "ng.hostname.com",
			port:     &model.Port{Name: "http", Port: 80},
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
								HttpUri: &jwtfilter.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &jwtfilter.HttpUri_Cluster{
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
						ForwardPayloadHeader: "istio-sec-0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33",
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
								HttpUri: &jwtfilter.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &jwtfilter.HttpUri_Cluster{
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
						ForwardPayloadHeader: "istio-sec-0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33",
					},
					{
						Issuer: "bar",
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &jwtfilter.HttpUri{
									Uri: "https://xyz.com",
									HttpUpstreamType: &jwtfilter.HttpUri_Cluster{
										Cluster: "jwks.xyz.com|https",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward:              true,
						FromParams:           []string{"x-jwt-bar"},
						ForwardPayloadHeader: "istio-sec-62cdb7020ff920e5aa642c3d4066950dd1f01f4d",
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
								HttpUri: &jwtfilter.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &jwtfilter.HttpUri_Cluster{
										Cluster: "jwks.abc.com|http",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward:              true,
						ForwardPayloadHeader: "istio-sec-da39a3ee5e6b4b0d3255bfef95601890afd80709",
					},
					{
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &jwtfilter.HttpUri{
									Uri: "https://xyz.com",
									HttpUpstreamType: &jwtfilter.HttpUri_Cluster{
										Cluster: "jwks.xyz.com|https",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward:              true,
						ForwardPayloadHeader: "istio-sec-da39a3ee5e6b4b0d3255bfef95601890afd80709",
					},
					{
						JwksSourceSpecifier: &jwtfilter.JwtRule_RemoteJwks{
							RemoteJwks: &jwtfilter.RemoteJwks{
								HttpUri: &jwtfilter.HttpUri{
									Uri: "http://abc.com",
									HttpUpstreamType: &jwtfilter.HttpUri_Cluster{
										Cluster: "jwks.abc.com|http",
									},
								},
								CacheDuration: &types.Duration{Seconds: 300},
							},
						},
						Forward:              true,
						ForwardPayloadHeader: "istio-sec-da39a3ee5e6b4b0d3255bfef95601890afd80709",
					},
				},
				AllowMissingOrFailed: true,
			},
		},
	}
	for _, c := range cases {
		if got := ConvertPolicyToJwtConfig(&c.in, false /*useInlinePublicKey*/); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("Test case %s: expected\n%#v\n, got\n%#v", c.name, c.expected.String(), got.String())
		}
	}
}

func TestConvertPolicyToJwtConfigWithInlineKey(t *testing.T) {
	ms, err := test.NewServer()
	if err != nil {
		t.Fatal("failed to create mock server")
	}
	if err := ms.Start(); err != nil {
		t.Fatal("failed to start mock server")
	}

	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		in       *authn.Policy
		expected *jwtfilter.JwtAuthentication
	}{
		{
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								JwksUri: jwksURI,
							},
						},
					},
				},
			},
			expected: &jwtfilter.JwtAuthentication{
				Rules: []*jwtfilter.JwtRule{
					{
						JwksSourceSpecifier: &jwtfilter.JwtRule_LocalJwks{
							LocalJwks: &jwtfilter.DataSource{
								Specifier: &jwtfilter.DataSource_InlineString{
									InlineString: test.JwtPubKey1,
								},
							},
						},
						Forward:              true,
						ForwardPayloadHeader: "istio-sec-da39a3ee5e6b4b0d3255bfef95601890afd80709"},
				},
				AllowMissingOrFailed: true,
			},
		},
	}

	for _, c := range cases {
		if got := ConvertPolicyToJwtConfig(c.in, true); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("ConvertPolicyToJwtConfig(%#v), got:\n%#v\nwanted:\n%#v\n", c.in, got, c.expected)
		}
	}
}

func TestBuildJwtFilter(t *testing.T) {
	ms, err := test.NewServer()
	if err != nil {
		t.Fatal("failed to create mock server")
	}
	if err := ms.Start(); err != nil {
		t.Fatal("failed to start mock server")
	}

	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		in       *authn.Policy
		expected *http_conn.HttpFilter
	}{
		{
			in:       nil,
			expected: nil,
		},
		{
			in:       &authn.Policy{},
			expected: nil,
		},
		{
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								JwksUri: jwksURI,
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "jwt-auth",
				Config: &types.Struct{
					Fields: map[string]*types.Value{
						"allow_missing_or_failed": {Kind: &types.Value_BoolValue{BoolValue: true}},
						"rules": {
							Kind: &types.Value_ListValue{
								ListValue: &types.ListValue{
									Values: []*types.Value{
										{
											Kind: &types.Value_StructValue{
												StructValue: &types.Struct{
													Fields: map[string]*types.Value{
														"forward": {Kind: &types.Value_BoolValue{BoolValue: true}},
														"forward_payload_header": {
															Kind: &types.Value_StringValue{
																StringValue: "istio-sec-da39a3ee5e6b4b0d3255bfef95601890afd80709",
															},
														},
														"local_jwks": {
															Kind: &types.Value_StructValue{
																StructValue: &types.Struct{
																	Fields: map[string]*types.Value{
																		"inline_string": {
																			Kind: &types.Value_StringValue{StringValue: test.JwtPubKey1},
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		if got := BuildJwtFilter(c.in); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("buildJwtFilter(%#v), got:\n%#v\nwanted:\n%#v\n", c.in, got, c.expected)
		}
	}
}

func TestConvertPolicyToAuthNFilterConfig(t *testing.T) {
	cases := []struct {
		name     string
		in       *authn.Policy
		expected *authn_filter.FilterConfig
	}{
		{
			name:     "nil policy",
			in:       nil,
			expected: nil,
		},
		{
			name:     "empty policy",
			in:       &authn.Policy{},
			expected: nil,
		},
		{
			name: "no jwt policy",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expected: &authn_filter.FilterConfig{
				Policy: &authn.Policy{
					Peers: []*authn.PeerAuthenticationMethod{{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							&authn.MutualTls{},
						},
					}},
				},
			},
		},
		{
			name: "jwt policy",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								Issuer: "foo",
							},
						},
					},
				},
			},
			expected: &authn_filter.FilterConfig{
				Policy: &authn.Policy{
					Peers: []*authn.PeerAuthenticationMethod{
						{
							Params: &authn.PeerAuthenticationMethod_Jwt{
								Jwt: &authn.Jwt{
									Issuer: "foo",
								},
							},
						},
					},
				},
				JwtOutputPayloadLocations: map[string]string{
					"foo": "istio-sec-0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33",
				},
			},
		},
		{
			name: "complex",
			in: &authn.Policy{
				Targets: []*authn.TargetSelector{
					{
						Name: "svc1",
					},
				},
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								Issuer: "foo",
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
							Issuer: "bar",
						},
					},
				},
			},
			expected: &authn_filter.FilterConfig{
				Policy: &authn.Policy{
					Peers: []*authn.PeerAuthenticationMethod{
						{
							Params: &authn.PeerAuthenticationMethod_Jwt{
								Jwt: &authn.Jwt{
									Issuer: "foo",
								},
							},
						},
						{
							Params: &authn.PeerAuthenticationMethod_Mtls{
								&authn.MutualTls{},
							},
						},
					},
					Origins: []*authn.OriginAuthenticationMethod{
						{
							Jwt: &authn.Jwt{
								Issuer: "bar",
							},
						},
					},
				},
				JwtOutputPayloadLocations: map[string]string{
					"foo": "istio-sec-0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33",
					"bar": "istio-sec-62cdb7020ff920e5aa642c3d4066950dd1f01f4d",
				},
			},
		},
	}
	for _, c := range cases {
		if got := ConvertPolicyToAuthNFilterConfig(c.in, model.Sidecar); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("Test case %s: expected\n%#v\n, got\n%#v", c.name, c.expected.String(), got.String())
		}
	}
}

func TestBuildAuthNFilter(t *testing.T) {
	cases := []struct {
		in                   *authn.Policy
		expectedFilterConfig *authn_filter.FilterConfig
	}{

		{
			in:                   nil,
			expectedFilterConfig: nil,
		},
		{
			in:                   &authn.Policy{},
			expectedFilterConfig: nil,
		},
		{
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								JwksUri: "http://abc.com",
							},
						},
					},
				},
			},
			expectedFilterConfig: &authn_filter.FilterConfig{
				Policy: &authn.Policy{
					Peers: []*authn.PeerAuthenticationMethod{
						{
							Params: &authn.PeerAuthenticationMethod_Jwt{
								Jwt: &authn.Jwt{
									JwksUri: "http://abc.com",
								},
							},
						},
					},
				},
				JwtOutputPayloadLocations: map[string]string{
					"": "istio-sec-da39a3ee5e6b4b0d3255bfef95601890afd80709",
				},
			},
		},
	}

	for _, c := range cases {
		got := BuildAuthNFilter(c.in, model.Sidecar)
		if got == nil {
			if c.expectedFilterConfig != nil {
				t.Errorf("BuildAuthNFilter(%#v), got: nil, wanted filter with config %s", c.in, c.expectedFilterConfig.String())
			}
		} else {
			if c.expectedFilterConfig == nil {
				t.Errorf("BuildAuthNFilter(%#v), got: \n%#v\n, wanted none", c.in, got)
			} else {
				if got.GetName() != AuthnFilterName {
					t.Errorf("BuildAuthNFilter(%#v), filter name is %s, wanted %s", c.in, got.GetName(), AuthnFilterName)
				}
				filterConfig := &authn_filter.FilterConfig{}
				if err := util.StructToMessage(got.GetConfig(), filterConfig); err != nil {
					t.Errorf("BuildAuthNFilter(%#v), bad filter config: %v", c.in, err)
				} else if !reflect.DeepEqual(c.expectedFilterConfig, filterConfig) {
					t.Errorf("BuildAuthNFilter(%#v), got filter config:\n%s\nwanted:\n%s\n", c.in, filterConfig.String(), c.expectedFilterConfig.String())
				}
			}
		}
	}
}

func TestBuildListenerTLSContex(t *testing.T) {
	cases := []struct {
		name     string
		in       *authn.Policy
		expected *auth.DownstreamTlsContext
	}{
		{
			name:     "nil policy",
			in:       nil,
			expected: nil,
		},
		{
			name:     "empty policy",
			in:       &authn.Policy{},
			expected: nil,
		},
		{
			name: "non-mTLS policy",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								Issuer: "foo",
							},
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "mTLS policy with nil param",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
			},
			expected: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					TlsCertificates: []*auth.TlsCertificate{
						{
							CertificateChain: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "/etc/certs/cert-chain.pem",
								},
							},
							PrivateKey: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "/etc/certs/key.pem",
								},
							},
						},
					},
					ValidationContextType: &auth.CommonTlsContext_ValidationContext{
						ValidationContext: &auth.CertificateValidationContext{
							TrustedCa: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "/etc/certs/root-cert.pem",
								},
							},
						},
					},
					AlpnProtocols: []string{"h2", "http/1.1"},
				},
				RequireClientCertificate: &types.BoolValue{true},
			},
		},
		{
			name: "mTLS policy allowTls",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							&authn.MutualTls{
								AllowTls: true,
							},
						},
					},
				},
			},
			expected: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					TlsCertificates: []*auth.TlsCertificate{
						{
							CertificateChain: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "/etc/certs/cert-chain.pem",
								},
							},
							PrivateKey: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "/etc/certs/key.pem",
								},
							},
						},
					},
					ValidationContextType: &auth.CommonTlsContext_ValidationContext{
						ValidationContext: &auth.CertificateValidationContext{
							TrustedCa: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "/etc/certs/root-cert.pem",
								},
							},
						},
					},
					AlpnProtocols: []string{"h2", "http/1.1"},
				},
				RequireClientCertificate: &types.BoolValue{false},
			},
		},
	}
	for _, c := range cases {
		if got := buildListenerTLSContext(c.in, nil, model.Sidecar); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("Test case %s: expected\n%#v\n, got\n%#v", c.name, c.expected.String(), got.String())
		}
	}
}
