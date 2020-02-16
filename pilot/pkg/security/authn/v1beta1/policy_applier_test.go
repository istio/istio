// Copyright 2019 Istio Authors
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

package v1beta1

import (
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	envoy_auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/jwt_authn/v2alpha"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/golang/protobuf/ptypes/empty"

	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	structpb "github.com/golang/protobuf/ptypes/struct"

	authn_alpha_api "istio.io/api/authentication/v1alpha1"
	"istio.io/api/security/v1beta1"
	type_beta "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/pkg/networking/plugin"
	pilotutil "istio.io/istio/pilot/pkg/networking/util"
	protovalue "istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	authn_alpha "istio.io/istio/security/proto/authentication/v1alpha1"
	authn_filter "istio.io/istio/security/proto/envoy/config/filter/http/authn/v2alpha1"
)

type testCase struct {
	name          string
	in            []*model.Config
	alphaPolicyIn *authn_alpha_api.Policy
	expected      *http_conn.HttpFilter
}

func TestJwtFilter(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []testCase{
		{
			name:     "No policy",
			in:       []*model.Config{},
			expected: nil,
		},
		{
			name: "Empty policy",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{},
				},
			},
			expected: nil,
		},
		{
			name: "Fallback to alpha with no JWT",
			alphaPolicyIn: &authn_alpha_api.Policy{
				Peers: []*authn_alpha_api.PeerAuthenticationMethod{{
					Params: &authn_alpha_api.PeerAuthenticationMethod_Mtls{&authn_alpha_api.MutualTls{}},
				}},
			},
			in:       []*model.Config{},
			expected: nil,
		},
		{
			name: "Fallback to alpha JWT",
			alphaPolicyIn: &authn_alpha_api.Policy{
				Peers: []*authn_alpha_api.PeerAuthenticationMethod{{
					Params: &authn_alpha_api.PeerAuthenticationMethod_Mtls{&authn_alpha_api.MutualTls{}},
				}},
				Origins: []*authn_alpha_api.OriginAuthenticationMethod{
					{
						Jwt: &authn_alpha_api.Jwt{
							Issuer:  "https://secret.foo.com",
							JwksUri: jwksURI,
						},
					},
				},
			},
			in: []*model.Config{},
			expected: &http_conn.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(
						&envoy_jwt.JwtAuthentication{
							Rules: []*envoy_jwt.RequirementRule{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{
											Prefix: "/",
										},
									},
									Requires: &envoy_jwt.JwtRequirement{
										RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
											RequiresAny: &envoy_jwt.JwtRequirementOrList{
												Requirements: []*envoy_jwt.JwtRequirement{
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-0",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_AllowMissingOrFailed{
															AllowMissingOrFailed: &empty.Empty{},
														},
													},
												},
											},
										},
									},
								},
							},
							Providers: map[string]*envoy_jwt.JwtProvider{
								"origins-0": {
									Issuer: "https://secret.foo.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: test.JwtPubKey1,
											},
										},
									},
									Forward:           true,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
						}),
				},
			},
		},
		{
			name: "Single JWT policy",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: jwksURI,
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(
						&envoy_jwt.JwtAuthentication{
							Rules: []*envoy_jwt.RequirementRule{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{
											Prefix: "/",
										},
									},
									Requires: &envoy_jwt.JwtRequirement{
										RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
											RequiresAny: &envoy_jwt.JwtRequirementOrList{
												Requirements: []*envoy_jwt.JwtRequirement{
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-0",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
															AllowMissing: &empty.Empty{},
														},
													},
												},
											},
										},
									},
								},
							},
							Providers: map[string]*envoy_jwt.JwtProvider{
								"origins-0": {
									Issuer: "https://secret.foo.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: test.JwtPubKey1,
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
						}),
				},
			},
		},
		{
			name: "Multi JWTs policy",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: jwksURI,
							},
						},
					},
				},
				{
					Spec: &v1beta1.RequestAuthentication{},
				},
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer: "https://secret.bar.com",
								Jwks:   "jwks-inline-data",
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(
						&envoy_jwt.JwtAuthentication{
							Rules: []*envoy_jwt.RequirementRule{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{
											Prefix: "/",
										},
									},
									Requires: &envoy_jwt.JwtRequirement{
										RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
											RequiresAny: &envoy_jwt.JwtRequirementOrList{
												Requirements: []*envoy_jwt.JwtRequirement{
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-0",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-1",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_RequiresAll{
															RequiresAll: &envoy_jwt.JwtRequirementAndList{
																Requirements: []*envoy_jwt.JwtRequirement{
																	{
																		RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
																			RequiresAny: &envoy_jwt.JwtRequirementOrList{
																				Requirements: []*envoy_jwt.JwtRequirement{
																					{
																						RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
																							ProviderName: "origins-0",
																						},
																					},
																					{
																						RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
																							AllowMissing: &empty.Empty{},
																						},
																					},
																				},
																			},
																		},
																	},
																	{
																		RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
																			RequiresAny: &envoy_jwt.JwtRequirementOrList{
																				Requirements: []*envoy_jwt.JwtRequirement{
																					{
																						RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
																							ProviderName: "origins-1",
																						},
																					},
																					{
																						RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
																							AllowMissing: &empty.Empty{},
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
							Providers: map[string]*envoy_jwt.JwtProvider{
								"origins-0": {
									Issuer: "https://secret.bar.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: "jwks-inline-data",
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "https://secret.bar.com",
								},
								"origins-1": {
									Issuer: "https://secret.foo.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: test.JwtPubKey1,
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
						}),
				},
			},
		},
		{
			name: "JWT policy with inline Jwks",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer: "https://secret.foo.com",
								Jwks:   "inline-jwks-data",
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(
						&envoy_jwt.JwtAuthentication{
							Rules: []*envoy_jwt.RequirementRule{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{
											Prefix: "/",
										},
									},
									Requires: &envoy_jwt.JwtRequirement{
										RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
											RequiresAny: &envoy_jwt.JwtRequirementOrList{
												Requirements: []*envoy_jwt.JwtRequirement{
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-0",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
															AllowMissing: &empty.Empty{},
														},
													},
												},
											},
										},
									},
								},
							},
							Providers: map[string]*envoy_jwt.JwtProvider{
								"origins-0": {
									Issuer: "https://secret.foo.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: "inline-jwks-data",
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
						}),
				},
			},
		},
		{
			name: "JWT policy with bad Jwks URI",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: "http://site.not.exist",
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(
						&envoy_jwt.JwtAuthentication{
							Rules: []*envoy_jwt.RequirementRule{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{
											Prefix: "/",
										},
									},
									Requires: &envoy_jwt.JwtRequirement{
										RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
											RequiresAny: &envoy_jwt.JwtRequirementOrList{
												Requirements: []*envoy_jwt.JwtRequirement{
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-0",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
															AllowMissing: &empty.Empty{},
														},
													},
												},
											},
										},
									},
								},
							},
							Providers: map[string]*envoy_jwt.JwtProvider{
								"origins-0": {
									Issuer: "https://secret.foo.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: "",
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
						}),
				},
			},
		},
		{
			name: "Forward original token",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:               "https://secret.foo.com",
								JwksUri:              jwksURI,
								ForwardOriginalToken: true,
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(
						&envoy_jwt.JwtAuthentication{
							Rules: []*envoy_jwt.RequirementRule{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{
											Prefix: "/",
										},
									},
									Requires: &envoy_jwt.JwtRequirement{
										RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
											RequiresAny: &envoy_jwt.JwtRequirementOrList{
												Requirements: []*envoy_jwt.JwtRequirement{
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-0",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
															AllowMissing: &empty.Empty{},
														},
													},
												},
											},
										},
									},
								},
							},
							Providers: map[string]*envoy_jwt.JwtProvider{
								"origins-0": {
									Issuer: "https://secret.foo.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: test.JwtPubKey1,
											},
										},
									},
									Forward:           true,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
						}),
				},
			},
		},
		{
			name: "Output payload",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:                "https://secret.foo.com",
								JwksUri:               jwksURI,
								ForwardOriginalToken:  true,
								OutputPayloadToHeader: "x-foo",
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(
						&envoy_jwt.JwtAuthentication{
							Rules: []*envoy_jwt.RequirementRule{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{
											Prefix: "/",
										},
									},
									Requires: &envoy_jwt.JwtRequirement{
										RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
											RequiresAny: &envoy_jwt.JwtRequirementOrList{
												Requirements: []*envoy_jwt.JwtRequirement{
													{
														RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
															ProviderName: "origins-0",
														},
													},
													{
														RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
															AllowMissing: &empty.Empty{},
														},
													},
												},
											},
										},
									},
								},
							},
							Providers: map[string]*envoy_jwt.JwtProvider{
								"origins-0": {
									Issuer: "https://secret.foo.com",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: test.JwtPubKey1,
											},
										},
									},
									Forward:              true,
									ForwardPayloadHeader: "x-foo",
									PayloadInMetadata:    "https://secret.foo.com",
								},
							},
						}),
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NewPolicyApplier("root-namespace", c.in, nil, c.alphaPolicyIn).JwtFilter(); !reflect.DeepEqual(c.expected, got) {
				t.Errorf("got:\n%s\nwanted:\n%s", spew.Sdump(got), spew.Sdump(c.expected))
			}
		})
	}
}

func TestConvertToEnvoyJwtConfig(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name     string
		in       []*v1beta1.JWTRule
		expected *envoy_jwt.JwtAuthentication
	}{
		{
			name:     "No rule",
			in:       []*v1beta1.JWTRule{},
			expected: nil,
		},
		{
			name: "Single JWT rule",
			in: []*v1beta1.JWTRule{
				{
					Issuer:  "https://secret.foo.com",
					JwksUri: jwksURI,
				},
			},
			expected: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						Requires: &envoy_jwt.JwtRequirement{
							RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
								RequiresAny: &envoy_jwt.JwtRequirementOrList{
									Requirements: []*envoy_jwt.JwtRequirement{
										{
											RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
												ProviderName: "origins-0",
											},
										},
										{
											RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
												AllowMissing: &empty.Empty{},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey1,
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
			},
		},
		{
			name: "Multiple JWT rule",
			in: []*v1beta1.JWTRule{
				{
					Issuer:  "https://secret.foo.com",
					JwksUri: jwksURI,
				},
				{
					Issuer: "https://secret.bar.com",
					Jwks:   "jwks-inline-data",
				},
			},
			expected: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						Requires: &envoy_jwt.JwtRequirement{
							RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
								RequiresAny: &envoy_jwt.JwtRequirementOrList{
									Requirements: []*envoy_jwt.JwtRequirement{
										{
											RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
												ProviderName: "origins-0",
											},
										},
										{
											RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
												ProviderName: "origins-1",
											},
										},
										{
											RequiresType: &envoy_jwt.JwtRequirement_RequiresAll{
												RequiresAll: &envoy_jwt.JwtRequirementAndList{
													Requirements: []*envoy_jwt.JwtRequirement{
														{
															RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
																RequiresAny: &envoy_jwt.JwtRequirementOrList{
																	Requirements: []*envoy_jwt.JwtRequirement{
																		{
																			RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
																				ProviderName: "origins-0",
																			},
																		},
																		{
																			RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
																				AllowMissing: &empty.Empty{},
																			},
																		},
																	},
																},
															},
														},
														{
															RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
																RequiresAny: &envoy_jwt.JwtRequirementOrList{
																	Requirements: []*envoy_jwt.JwtRequirement{
																		{
																			RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
																				ProviderName: "origins-1",
																			},
																		},
																		{
																			RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
																				AllowMissing: &empty.Empty{},
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
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey1,
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
					"origins-1": {
						Issuer: "https://secret.bar.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: "jwks-inline-data",
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.bar.com",
					},
				},
			},
		},
		{
			name: "Empty Jwks URI",
			in: []*v1beta1.JWTRule{
				{
					Issuer: "https://secret.foo.com",
				},
			},
			expected: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						Requires: &envoy_jwt.JwtRequirement{
							RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
								RequiresAny: &envoy_jwt.JwtRequirementOrList{
									Requirements: []*envoy_jwt.JwtRequirement{
										{
											RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
												ProviderName: "origins-0",
											},
										},
										{
											RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
												AllowMissing: &empty.Empty{},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: "",
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
			},
		},
		{
			name: "Unreachable Jwks URI",
			in: []*v1beta1.JWTRule{
				{
					Issuer:  "https://secret.foo.com",
					JwksUri: "http://site.not.exist",
				},
			},
			expected: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						Requires: &envoy_jwt.JwtRequirement{
							RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
								RequiresAny: &envoy_jwt.JwtRequirementOrList{
									Requirements: []*envoy_jwt.JwtRequirement{
										{
											RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
												ProviderName: "origins-0",
											},
										},
										{
											RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
												AllowMissing: &empty.Empty{},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: "",
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := convertToEnvoyJwtConfig(c.in); !reflect.DeepEqual(c.expected, got) {
				t.Errorf("got:\n%s\nwanted:\n%s\n", spew.Sdump(got), spew.Sdump(c.expected))
			}
		})
	}
}

func TestAuthnFilterConfig(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []testCase{{
		name:     "no-request-authn-rule",
		expected: nil,
	}, {
		name: "no-request-authn-rule-alphafallback",
		alphaPolicyIn: &authn_alpha_api.Policy{
			Peers: []*authn_alpha_api.PeerAuthenticationMethod{{
				Params: &authn_alpha_api.PeerAuthenticationMethod_Mtls{&authn_alpha_api.MutualTls{}},
			}},
			Origins: []*authn_alpha_api.OriginAuthenticationMethod{
				{
					Jwt: &authn_alpha_api.Jwt{
						Issuer:  "https://secret.foo.com",
						JwksUri: jwksURI,
					},
				},
			},
		},
		expected: &http_conn.HttpFilter{
			Name: "istio_authn",
			ConfigType: &http_conn.HttpFilter_TypedConfig{
				TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
					JwtOutputPayloadLocations: map[string]string{
						"https://secret.foo.com": "istio-sec-bb4594e42ba8128d87988eea9e4a8f2eaf874856",
					},
					Policy: &authn_alpha.Policy{
						Peers: []*authn_alpha.PeerAuthenticationMethod{
							{
								Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
									Mtls: &authn_alpha.MutualTls{},
								},
							},
						},
						Origins: []*authn_alpha.OriginAuthenticationMethod{
							{
								Jwt: &authn_alpha.Jwt{
									Issuer:  "https://secret.foo.com",
									JwksUri: jwksURI,
								},
							},
						},
					},
				}),
			},
		},
	},
		{
			name: "only-mtls-alpha-fallback",
			alphaPolicyIn: &authn_alpha_api.Policy{
				Peers: []*authn_alpha_api.PeerAuthenticationMethod{{
					Params: &authn_alpha_api.PeerAuthenticationMethod_Mtls{&authn_alpha_api.MutualTls{}},
				}},
			},
			expected: &http_conn.HttpFilter{
				Name: "istio_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
						Policy: &authn_alpha.Policy{
							Peers: []*authn_alpha.PeerAuthenticationMethod{
								{
									Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
										Mtls: &authn_alpha.MutualTls{},
									},
								},
							},
						}}),
				},
			},
		},
		{
			name: "single-request-authn-rule",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: jwksURI,
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "istio_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
						Policy: &authn_alpha.Policy{
							Peers: []*authn_alpha.PeerAuthenticationMethod{
								{
									Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
										Mtls: &authn_alpha.MutualTls{},
									},
								},
							},
							Origins: []*authn_alpha.OriginAuthenticationMethod{
								{
									Jwt: &authn_alpha.Jwt{
										Issuer: "https://secret.foo.com",
									},
								},
							},
							PeerIsOptional:   true,
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
					}),
				},
			},
		},
		{
			name: "multi-rules",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.bar.com",
								JwksUri: jwksURI,
							},
						},
					},
				},
				{
					Spec: &v1beta1.RequestAuthentication{},
				},
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer: "https://secret.foo.com",
								Jwks:   "jwks-inline-data",
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "istio_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
						Policy: &authn_alpha.Policy{
							Peers: []*authn_alpha.PeerAuthenticationMethod{
								{
									Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
										Mtls: &authn_alpha.MutualTls{},
									},
								},
							},
							Origins: []*authn_alpha.OriginAuthenticationMethod{
								{
									Jwt: &authn_alpha.Jwt{
										Issuer: "https://secret.bar.com",
									},
								},
								{
									Jwt: &authn_alpha.Jwt{
										Issuer: "https://secret.foo.com",
									},
								},
							},
							PeerIsOptional:   true,
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
					}),
				},
			},
		},
		{
			name: "multi-rules-sort-by-issuer-again",
			in: []*model.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: jwksURI,
							},
						},
					},
				},
				{
					Spec: &v1beta1.RequestAuthentication{},
				},
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer: "https://secret.bar.com",
								Jwks:   "jwks-inline-data",
							},
						},
					},
				},
			},
			expected: &http_conn.HttpFilter{
				Name: "istio_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
						Policy: &authn_alpha.Policy{
							Peers: []*authn_alpha.PeerAuthenticationMethod{
								{
									Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
										Mtls: &authn_alpha.MutualTls{},
									},
								},
							},
							Origins: []*authn_alpha.OriginAuthenticationMethod{
								{
									Jwt: &authn_alpha.Jwt{
										Issuer: "https://secret.bar.com",
									},
								},
								{
									Jwt: &authn_alpha.Jwt{
										Issuer: "https://secret.foo.com",
									},
								},
							},
							PeerIsOptional:   true,
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
					}),
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NewPolicyApplier("root-namespace", c.in, nil, c.alphaPolicyIn).AuthNFilter(model.SidecarProxy)
			if !reflect.DeepEqual(c.expected, got) {
				gotYaml, _ := gogoprotomarshal.ToYAML(got)
				expectedYaml, _ := gogoprotomarshal.ToYAML(c.expected)
				t.Errorf("got:\n%s\nwanted:\n%s\n", gotYaml, expectedYaml)
			}
		})
	}
}

func TestOnInboundFilterChain(t *testing.T) {
	now := time.Now()
	tlsContext := &envoy_auth.DownstreamTlsContext{
		CommonTlsContext: &envoy_auth.CommonTlsContext{
			TlsCertificates: []*envoy_auth.TlsCertificate{
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
			ValidationContextType: &envoy_auth.CommonTlsContext_ValidationContext{
				ValidationContext: &envoy_auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: "/etc/certs/root-cert.pem",
						},
					},
				},
			},
			AlpnProtocols: []string{"istio-peer-exchange", "h2", "http/1.1"},
		},
		RequireClientCertificate: protovalue.BoolTrue,
	}

	expectedStrict := []plugin.FilterChain{
		{
			TLSContext: tlsContext,
		},
	}

	// Two filter chains, one for mtls traffic within the mesh, one for plain text traffic.
	expectedPermissive := []plugin.FilterChain{
		{
			TLSContext: tlsContext,
			FilterChainMatch: &listener.FilterChainMatch{
				ApplicationProtocols: []string{"istio-peer-exchange", "istio"},
			},
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name:       "envoy.listener.tls_inspector",
					ConfigType: &listener.ListenerFilter_Config{&structpb.Struct{}},
				},
			},
		},
		{
			FilterChainMatch: &listener.FilterChainMatch{},
		},
	}

	cases := []struct {
		name         string
		peerPolicies []*model.Config
		alphaPolicy  *authn_alpha_api.Policy
		sdsUdsPath   string
		expected     []plugin.FilterChain
	}{
		{
			name:     "No policy - behave as permissive",
			expected: expectedPermissive,
		},
		{
			name: "Single policy - disable mode",
			peerPolicies: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "Single policy - permissive mode",
			peerPolicies: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
						},
					},
				},
			},
			expected: expectedPermissive,
		},
		{
			name: "Single policy - strict mode",
			peerPolicies: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
			},
			expected: expectedStrict,
		},
		{
			name: "Multiple policies resolved to STRICT",
			peerPolicies: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "now",
						Namespace:         "my-ns",
						CreationTimestamp: now,
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "later",
						Namespace:         "my-ns",
						CreationTimestamp: now.Add(time.Second),
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
			},
			expected: expectedStrict,
		},
		{
			name: "Multiple policies resolved to PERMISSIVE",
			peerPolicies: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "now",
						Namespace:         "my-ns",
						CreationTimestamp: now,
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "earlier",
						Namespace:         "my-ns",
						CreationTimestamp: now.Add(time.Second * -1),
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
						},
					},
				},
			},
			expected: expectedPermissive,
		},
		{
			name: "Port level hit",
			peerPolicies: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
						PortLevelMtls: map[uint32]*v1beta1.PeerAuthentication_MutualTLS{
							8080: {
								Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
							},
						},
					},
				},
			},
			expected: expectedStrict,
		},
		{
			name: "Port level miss",
			peerPolicies: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						PortLevelMtls: map[uint32]*v1beta1.PeerAuthentication_MutualTLS{
							7070: {
								Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
							},
						},
					},
				},
			},
			expected: expectedPermissive,
		},
		{
			name: "Fallback to alpha API",
			alphaPolicy: &authn_alpha_api.Policy{
				Peers: []*authn_alpha_api.PeerAuthenticationMethod{
					{
						Params: &authn_alpha_api.PeerAuthenticationMethod_Mtls{
							Mtls: &authn_alpha_api.MutualTls{
								Mode: authn_alpha_api.MutualTls_PERMISSIVE,
							},
						},
					},
				},
			},
			expected: expectedPermissive,
		},
		{
			name: "Ignore alpha API",
			peerPolicies: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
			},
			alphaPolicy: &authn_alpha_api.Policy{
				Peers: []*authn_alpha_api.PeerAuthenticationMethod{
					{
						Params: &authn_alpha_api.PeerAuthenticationMethod_Mtls{
							Mtls: &authn_alpha_api.MutualTls{
								Mode: authn_alpha_api.MutualTls_PERMISSIVE,
							},
						},
					},
				},
			},
			expected: nil,
		},
	}

	testNode := &model.Proxy{
		Metadata: &model.NodeMetadata{
			Labels: map[string]string{
				"app": "foo",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewPolicyApplier("root-namespace", nil, tc.peerPolicies, tc.alphaPolicy).InboundFilterChain(
				8080,
				tc.sdsUdsPath,
				testNode,
			)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("[%v] unexpected filter chains, got %v, want %v", tc.name, got, tc.expected)
			}
		})
	}
}

func TestComposePeerAuthentication(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		configs []*model.Config
		want    *v1beta1.PeerAuthentication
	}{
		{
			name:    "no config",
			configs: []*model.Config{},
			want:    nil,
		},
		{
			name: "mesh only",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
				},
			},
		},
		{
			name: "mesh vs namespace",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "ignore non-empty selector in root namespace",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "workload vs namespace config",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "foo",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "workload vs mesh config",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "multiple workload policy",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "now",
						Namespace:         "my-ns",
						CreationTimestamp: now,
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "second ago",
						Namespace:         "my-ns",
						CreationTimestamp: now.Add(time.Second * -1),
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "second later",
						Namespace:         "my-ns",
						CreationTimestamp: now.Add(time.Second * -1),
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"stage": "prod",
							},
						},
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "inheritance: default mesh",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "inheritance: mesh to workload",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "foo",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
				},
			},
		},
		{
			name: "inheritance: namespace to workload",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "foo",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
				},
			},
		},
		{
			name: "inheritance: mesh to namespace to workload",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "foo",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
				},
			},
		},
		{
			name: "port level",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "default",
						Namespace: "root-namespace",
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "foo",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{
						Selector: &type_beta.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "foo",
							},
						},
						PortLevelMtls: map[uint32]*v1beta1.PeerAuthentication_MutualTLS{
							80: {
								Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
							},
							90: {
								Mode: v1beta1.PeerAuthentication_MutualTLS_UNSET,
							},
							100: {},
						},
					},
				},
			},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
				},
				PortLevelMtls: map[uint32]*v1beta1.PeerAuthentication_MutualTLS{
					80: {
						Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
					},
					90: {
						Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
					},
					100: {
						Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composePeerAuthentication("root-namespace", tt.configs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("composePeerAuthentication() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMutualTLSMode(t *testing.T) {
	tests := []struct {
		name string
		in   v1beta1.PeerAuthentication_MutualTLS
		want model.MutualTLSMode
	}{
		{
			name: "unset",
			in: v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_UNSET,
			},
			want: model.MTLSUnknown,
		},
		{
			name: "disable",
			in: v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
			},
			want: model.MTLSDisable,
		},
		{
			name: "permissive",
			in: v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
			},
			want: model.MTLSPermissive,
		},
		{
			name: "strict",
			in: v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
			},
			want: model.MTLSStrict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getMutualTLSMode(&tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getMutualTLSMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
