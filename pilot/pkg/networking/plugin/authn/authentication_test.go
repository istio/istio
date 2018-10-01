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
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha1"
	authn_filter "istio.io/api/envoy/config/filter/http/authn/v2alpha1"
	jwtfilter "istio.io/api/envoy/config/filter/http/jwt_auth/v2alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

func TestRequireTls(t *testing.T) {
	cases := []struct {
		name           string
		in             *authn.Policy
		expectedParams *authn.MutualTls
	}{
		{
			name:           "Null policy",
			in:             nil,
			expectedParams: nil,
		},
		{
			name:           "Empty policy",
			in:             &authn.Policy{},
			expectedParams: nil,
		},
		{
			name: "Policy with mTls",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expectedParams: &authn.MutualTls{Mode: authn.MutualTls_STRICT},
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
			expectedParams: nil,
		},
	}
	for _, c := range cases {
		if params := GetMutualTLS(c.in); !reflect.DeepEqual(c.expectedParams, params) {
			t.Errorf("%s: requireTLS(%v): got(%v) != want(%v)\n", c.name, c.in, params, c.expectedParams)
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
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
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
		if got := ConvertPolicyToJwtConfig(c.in); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("ConvertPolicyToJwtConfig(%#v), got:\n%#v\nwanted:\n%#v\n", c.in, got, c.expected)
		}
	}
}

func TestBuildJwtFilter(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
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

func TestOnInboundFilterChains(t *testing.T) {
	tlsContext := &auth.DownstreamTlsContext{
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
		RequireClientCertificate: &types.BoolValue{Value: true},
	}
	cases := []struct {
		name           string
		in             *authn.Policy
		serviceAccount string
		sdsUdsPath     string
		expected       []plugin.FilterChain
	}{
		{
			name: "NoAuthnPolicy",
			in:   nil,
			// No need to set up filter chain, default one is okay.
			expected: nil,
		},
		{
			name: "NonMTLSPolicy",
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
			name: "mTLSWithNilParamMode",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
			},
			expected: []plugin.FilterChain{
				{
					TLSContext: tlsContext,
				},
			},
		},
		{
			name: "StrictMTLS",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								Mode: authn.MutualTls_STRICT,
							},
						},
					},
				},
			},
			// Only one filter chain with mTLS settings should be generated.
			expected: []plugin.FilterChain{
				{
					TLSContext: tlsContext,
				},
			},
		},
		{
			name: "PermissiveMTLS",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								Mode: authn.MutualTls_PERMISSIVE,
							},
						},
					},
				},
			},
			// Two filter chains, one for mtls traffic within the mesh, one for plain text traffic.
			expected: []plugin.FilterChain{
				{
					TLSContext: tlsContext,
					FilterChainMatch: &listener.FilterChainMatch{
						ApplicationProtocols: []string{"istio"},
					},
					RequiredListenerFilters: []listener.ListenerFilter{
						{
							Name:   "envoy.listener.tls_inspector",
							Config: &types.Struct{},
						},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{},
				},
			},
		},
		{
			name: "mTLS policy using SDS",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
			},
			sdsUdsPath:     "/tmp/sdsuds.sock",
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			expected: []plugin.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								{
									Name: "spiffe://cluster.local/ns/bar/sa/foo",
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType: core.ApiConfigSource_GRPC,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_GoogleGrpc_{
															GoogleGrpc: &core.GrpcService_GoogleGrpc{
																TargetUri:  "/tmp/sdsuds.sock",
																StatPrefix: model.SDSStatPrefix,
																CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
																	&core.GrpcService_GoogleGrpc_CallCredentials{
																		CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_GoogleComputeEngine{
																			GoogleComputeEngine: &types.Empty{},
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
							ValidationContextType: &auth.CommonTlsContext_ValidationContext{
								ValidationContext: &auth.CertificateValidationContext{
									TrustedCa: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: model.CARootCertPath,
										},
									},
								},
							},
							AlpnProtocols: []string{"h2", "http/1.1"},
						},
						RequireClientCertificate: &types.BoolValue{Value: true},
					},
				},
			},
		},
	}
	for _, c := range cases {
		if got := setupFilterChains(c.in, c.serviceAccount, c.sdsUdsPath); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("[%v] unexpected filter chains, got %v, want %v", c.name, got, c.expected)
		}
	}
}
