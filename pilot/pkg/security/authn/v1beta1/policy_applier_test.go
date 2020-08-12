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

package v1beta1

import (
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"

	"istio.io/api/security/v1beta1"
	type_beta "istio.io/api/type/v1beta1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/pkg/networking"
	pilotutil "istio.io/istio/pilot/pkg/networking/util"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	authn_alpha "istio.io/istio/pkg/envoy/config/authentication/v1alpha1"
	authn_filter "istio.io/istio/pkg/envoy/config/filter/http/authn/v2alpha1"
	protovalue "istio.io/istio/pkg/proto"
)

func TestJwtFilter(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name     string
		in       []*model.Config
		expected *http_conn.HttpFilter
	}{
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
			if got := NewPolicyApplier("root-namespace", c.in, nil).JwtFilter(); !reflect.DeepEqual(c.expected, got) {
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

func humanReadableAuthnFilterDump(filter *http_conn.HttpFilter) string {
	if filter == nil {
		return "<nil>"
	}
	config := &authn_filter.FilterConfig{}
	ptypes.UnmarshalAny(filter.GetTypedConfig(), config)
	return spew.Sdump(*config)
}

func TestAuthnFilterConfig(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name                         string
		isGateway                    bool
		gatewayServerUsesIstioMutual bool
		jwtIn                        []*model.Config
		peerIn                       []*model.Config
		expected                     *http_conn.HttpFilter
	}{
		{
			name: "no-policy",
			expected: &http_conn.HttpFilter{
				Name: "istio_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
						Policy: &authn_alpha.Policy{
							Peers: []*authn_alpha.PeerAuthenticationMethod{
								{
									Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_PERMISSIVE,
										},
									},
								},
							},
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name:      "no-policy-for-gateway-with-non-istio-mutual-servers",
			isGateway: true,
			expected:  nil,
		},
		{
			name:                         "policy-for-gateway-with-istio-mutual-server",
			isGateway:                    true,
			gatewayServerUsesIstioMutual: true,
			expected: &http_conn.HttpFilter{
				Name: "istio_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
						Policy: &authn_alpha.Policy{
							Peers: []*authn_alpha.PeerAuthenticationMethod{
								{
									Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_STRICT,
										},
									},
								},
							},
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name: "no-request-authn-rule-skip-trust-domain",
			expected: &http_conn.HttpFilter{
				Name: "istio_authn",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: pilotutil.MessageToAny(&authn_filter.FilterConfig{
						Policy: &authn_alpha.Policy{
							Peers: []*authn_alpha.PeerAuthenticationMethod{
								{
									Params: &authn_alpha.PeerAuthenticationMethod_Mtls{
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_PERMISSIVE,
										},
									},
								},
							},
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name: "beta-jwt",
			jwtIn: []*model.Config{
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
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_PERMISSIVE,
										},
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
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name:      "beta-jwt-for-gateway",
			isGateway: true,
			jwtIn: []*model.Config{
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
							Origins: []*authn_alpha.OriginAuthenticationMethod{
								{
									Jwt: &authn_alpha.Jwt{
										Issuer: "https://secret.foo.com",
									},
								},
							},
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name:                         "beta-jwt-for-gateway-with-istio-mutual",
			isGateway:                    true,
			gatewayServerUsesIstioMutual: true,
			jwtIn: []*model.Config{
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
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_STRICT,
										},
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
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name: "multi-beta-jwt",
			jwtIn: []*model.Config{
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
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_PERMISSIVE,
										},
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
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name: "multi-beta-jwt-sort-by-issuer-again",
			jwtIn: []*model.Config{
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
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_PERMISSIVE,
										},
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
							OriginIsOptional: true,
							PrincipalBinding: authn_alpha.PrincipalBinding_USE_ORIGIN,
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name: "beta-mtls",
			peerIn: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
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
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_STRICT,
										},
									},
								},
							},
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
		{
			name:      "beta-mtls-for-gateway-does-not-respect-mtls-configs",
			isGateway: true,
			peerIn: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "beta-mtls-skip-trust-domain",
			peerIn: []*model.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
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
										Mtls: &authn_alpha.MutualTls{
											Mode: authn_alpha.MutualTls_STRICT,
										},
									},
								},
							},
						},
						SkipValidateTrustDomain: true,
					}),
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proxyType := model.SidecarProxy
			if c.isGateway {
				proxyType = model.Router
			}
			got := NewPolicyApplier("root-namespace", c.jwtIn, c.peerIn).AuthNFilter(proxyType, 80, c.gatewayServerUsesIstioMutual)
			if !reflect.DeepEqual(c.expected, got) {
				t.Errorf("got:\n%v\nwanted:\n%v\n", humanReadableAuthnFilterDump(got), humanReadableAuthnFilterDump(c.expected))
			}
		})
	}
}

func TestOnInboundFilterChain(t *testing.T) {
	now := time.Now()
	tlsContext := &tls.DownstreamTlsContext{
		CommonTlsContext: &tls.CommonTlsContext{
			TlsCertificates: []*tls.TlsCertificate{
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
			ValidationContextType: &tls.CommonTlsContext_ValidationContext{
				ValidationContext: &tls.CertificateValidationContext{
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

	expectedStrict := []networking.FilterChain{
		{
			TLSContext: tlsContext,
		},
	}

	// Two filter chains, one for mtls traffic within the mesh, one for plain text traffic.
	expectedPermissive := []networking.FilterChain{
		{
			TLSContext: tlsContext,
			FilterChainMatch: &listener.FilterChainMatch{
				ApplicationProtocols: []string{"istio-peer-exchange", "istio"},
			},
			ListenerFilters: []*listener.ListenerFilter{
				xdsfilters.TLSInspector,
			},
		},
		{
			FilterChainMatch: &listener.FilterChainMatch{},
		},
	}

	cases := []struct {
		name         string
		peerPolicies []*model.Config
		sdsUdsPath   string
		expected     []networking.FilterChain
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
			got := NewPolicyApplier("root-namespace", nil, tc.peerPolicies).InboundFilterChain(
				8080,
				tc.sdsUdsPath,
				testNode,
				networking.ListenerProtocolAuto,
				[]string{},
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
			name: "multiple mesh policy",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "now",
						Namespace:         "root-namespace",
						CreationTimestamp: now,
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "second ago",
						Namespace:         "root-namespace",
						CreationTimestamp: now.Add(time.Second * -1),
					},
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
						},
					},
				},
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "second later",
						Namespace:         "root-namespace",
						CreationTimestamp: now.Add(time.Second * -1),
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
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "multiple namespace policy",
			configs: []*model.Config{
				{
					ConfigMeta: model.ConfigMeta{
						Name:              "now",
						Namespace:         "my-ns",
						CreationTimestamp: now,
					},
					Spec: &v1beta1.PeerAuthentication{
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
