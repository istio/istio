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
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"

	authn_alpha "istio.io/api/authentication/v1alpha1"
	authn_filter "istio.io/api/envoy/config/filter/http/authn/v2alpha1"
	"istio.io/api/security/v1beta1"
	type_beta "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/pkg/networking/plugin"
	pilotutil "istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	protovalue "istio.io/istio/pkg/proto"
)

func TestJwtFilter(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name             string
		in               []*config.Config
		enableRemoteJwks bool
		expected         *http_conn.HttpFilter
	}{
		{
			name:     "No policy",
			in:       []*config.Config{},
			expected: nil,
		},
		{
			name: "Empty policy",
			in: []*config.Config{
				{
					Spec: &v1beta1.RequestAuthentication{},
				},
			},
			expected: nil,
		},
		{
			name: "Single JWT policy",
			in: []*config.Config{
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																AllowMissing: &emptypb.Empty{},
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
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with Mesh cluster as issuer and remote jwks enabled",
			in: []*config.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "mesh cluster",
								JwksUri: "http://jwt-token-issuer.mesh:7443/jwks",
							},
						},
					},
				},
			},
			enableRemoteJwks: true,
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																AllowMissing: &emptypb.Empty{},
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
									Issuer: "mesh cluster",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_RemoteJwks{
										RemoteJwks: &envoy_jwt.RemoteJwks{
											HttpUri: &core.HttpUri{
												Uri: "http://jwt-token-issuer.mesh:7443/jwks",
												HttpUpstreamType: &core.HttpUri_Cluster{
													Cluster: "outbound|7443||jwt-token-issuer.mesh.svc.cluster.local",
												},
												Timeout: &durationpb.Duration{Seconds: 5},
											},
											CacheDuration: &durationpb.Duration{Seconds: 5 * 60},
										},
									},
									Forward:           false,
									PayloadInMetadata: "mesh cluster",
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with non Mesh cluster as issuer and remote jwks enabled",
			in: []*config.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "invalid|7443|",
								JwksUri: jwksURI,
							},
						},
					},
				},
			},
			enableRemoteJwks: true,
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																AllowMissing: &emptypb.Empty{},
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
									Issuer: "invalid|7443|",
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
										LocalJwks: &core.DataSource{
											Specifier: &core.DataSource_InlineString{
												InlineString: test.JwtPubKey2,
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "invalid|7443|",
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "Multi JWTs policy",
			in: []*config.Config{
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																								AllowMissing: &emptypb.Empty{},
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
																								AllowMissing: &emptypb.Empty{},
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
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with inline Jwks",
			in: []*config.Config{
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																AllowMissing: &emptypb.Empty{},
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
												InlineString: "inline-jwks-data",
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with bad Jwks URI",
			in: []*config.Config{
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																AllowMissing: &emptypb.Empty{},
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
												InlineString: model.CreateFakeJwks("http://site.not.exist"),
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "Forward original token",
			in: []*config.Config{
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																AllowMissing: &emptypb.Empty{},
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
									Forward:           true,
									PayloadInMetadata: "https://secret.foo.com",
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "Output payload",
			in: []*config.Config{
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
									RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																AllowMissing: &emptypb.Empty{},
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
									Forward:              true,
									ForwardPayloadHeader: "x-foo",
									PayloadInMetadata:    "https://secret.foo.com",
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
	}

	push := model.NewPushContext()
	push.JwtKeyResolver = model.NewJwksResolver(
		model.JwtPubKeyEvictionDuration, model.JwtPubKeyRefreshInterval,
		model.JwtPubKeyRefreshIntervalOnFailure, model.JwtPubKeyRetryInterval)
	defer push.JwtKeyResolver.Close()

	push.ServiceIndex.HostnameAndNamespace[host.Name("jwt-token-issuer.mesh")] = map[string]*model.Service{}
	push.ServiceIndex.HostnameAndNamespace[host.Name("jwt-token-issuer.mesh")]["mesh"] = &model.Service{
		Hostname: "jwt-token-issuer.mesh.svc.cluster.local",
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defaultValue := features.EnableRemoteJwks
			features.EnableRemoteJwks = c.enableRemoteJwks
			defer func() { features.EnableRemoteJwks = defaultValue }()
			if got := NewPolicyApplier("root-namespace", c.in, nil, push).JwtFilter(); !reflect.DeepEqual(c.expected, got) {
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
						RequirementType: &envoy_jwt.RequirementRule_Requires{
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
													AllowMissing: &emptypb.Empty{},
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
				},
				BypassCorsPreflight: true,
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
						RequirementType: &envoy_jwt.RequirementRule_Requires{
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
																					AllowMissing: &emptypb.Empty{},
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
																					AllowMissing: &emptypb.Empty{},
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
				BypassCorsPreflight: true,
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
						RequirementType: &envoy_jwt.RequirementRule_Requires{
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
													AllowMissing: &emptypb.Empty{},
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
									InlineString: model.CreateFakeJwks(""),
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
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
						RequirementType: &envoy_jwt.RequirementRule_Requires{
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
													AllowMissing: &emptypb.Empty{},
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
									InlineString: model.CreateFakeJwks("http://site.not.exist"),
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
			},
		},
	}

	push := &model.PushContext{}
	push.JwtKeyResolver = model.NewJwksResolver(
		model.JwtPubKeyEvictionDuration, model.JwtPubKeyRefreshInterval,
		model.JwtPubKeyRefreshIntervalOnFailure, model.JwtPubKeyRetryInterval)
	defer push.JwtKeyResolver.Close()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := convertToEnvoyJwtConfig(c.in, push); !reflect.DeepEqual(c.expected, got) {
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
	filter.GetTypedConfig().UnmarshalTo(config)
	return spew.Sdump(config)
}

func TestAuthnFilterConfig(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name       string
		forSidecar bool
		jwtIn      []*config.Config
		peerIn     []*config.Config
		expected   *http_conn.HttpFilter
	}{
		{
			name:     "no-policy",
			expected: nil,
		},
		{
			name: "beta-jwt",
			jwtIn: []*config.Config{
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
						SkipValidateTrustDomain: true,
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
					}),
				},
			},
		},
		{
			name:       "beta-jwt-for-sidecar",
			forSidecar: true,
			jwtIn: []*config.Config{
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
						SkipValidateTrustDomain: true,
						DisableClearRouteCache:  true,
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
					}),
				},
			},
		},
		{
			name: "multi-beta-jwt",
			jwtIn: []*config.Config{
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
						SkipValidateTrustDomain: true,
						Policy: &authn_alpha.Policy{
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
					}),
				},
			},
		},
		{
			name: "multi-beta-jwt-sort-by-issuer-again",
			jwtIn: []*config.Config{
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
						SkipValidateTrustDomain: true,
						Policy: &authn_alpha.Policy{
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
					}),
				},
			},
		},
		{
			name: "beta-mtls",
			peerIn: []*config.Config{
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
			name: "beta-mtls-disable",
			peerIn: []*config.Config{
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
			name: "beta-mtls-skip-trust-domain",
			peerIn: []*config.Config{
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NewPolicyApplier("root-namespace", c.jwtIn, c.peerIn, &model.PushContext{}).AuthNFilter(c.forSidecar)
			if !reflect.DeepEqual(c.expected, got) {
				t.Errorf("got:\n%v\nwanted:\n%v\n", humanReadableAuthnFilterDump(got), humanReadableAuthnFilterDump(c.expected))
			}
		})
	}
}

func TestInboundMTLSSettings(t *testing.T) {
	now := time.Now()
	tlsContext := &tls.DownstreamTlsContext{
		CommonTlsContext: &tls.CommonTlsContext{
			TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
				{
					Name: "default",
					SdsConfig: &core.ConfigSource{
						ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
							ApiConfigSource: &core.ApiConfigSource{
								ApiType:                   core.ApiConfigSource_GRPC,
								SetNodeOnFirstMessageOnly: true,
								TransportApiVersion:       core.ApiVersion_V3,
								GrpcServices: []*core.GrpcService{
									{
										TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
											EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
										},
									},
								},
							},
						},
						InitialFetchTimeout: durationpb.New(time.Second * 0),
						ResourceApiVersion:  core.ApiVersion_V3,
					},
				},
			},
			ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
				CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
					DefaultValidationContext: &tls.CertificateValidationContext{},
					ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
						Name: "ROOTCA",
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType:                   core.ApiConfigSource_GRPC,
									SetNodeOnFirstMessageOnly: true,
									TransportApiVersion:       core.ApiVersion_V3,
									GrpcServices: []*core.GrpcService{
										{
											TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
											},
										},
									},
								},
							},
							InitialFetchTimeout: durationpb.New(time.Second * 0),
							ResourceApiVersion:  core.ApiVersion_V3,
						},
					},
				},
			},
			AlpnProtocols: []string{"istio-peer-exchange", "h2", "http/1.1"},
			TlsParams: &tls.TlsParameters{
				TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
				TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
				CipherSuites: []string{
					"ECDHE-ECDSA-AES256-GCM-SHA384",
					"ECDHE-RSA-AES256-GCM-SHA384",
					"ECDHE-ECDSA-AES128-GCM-SHA256",
					"ECDHE-RSA-AES128-GCM-SHA256",
					"AES256-GCM-SHA384",
					"AES128-GCM-SHA256",
				},
			},
		},
		RequireClientCertificate: protovalue.BoolTrue,
	}
	tlsContextHTTP := proto.Clone(tlsContext).(*tls.DownstreamTlsContext)
	tlsContextHTTP.CommonTlsContext.AlpnProtocols = []string{"h2", "http/1.1"}

	expectedStrict := plugin.MTLSSettings{
		Port: 8080,
		Mode: model.MTLSStrict,
		TCP:  tlsContext,
		HTTP: tlsContextHTTP,
	}
	expectedPermissive := plugin.MTLSSettings{
		Port: 8080,
		Mode: model.MTLSPermissive,
		TCP:  tlsContext,
		HTTP: tlsContextHTTP,
	}

	cases := []struct {
		name         string
		peerPolicies []*config.Config
		expected     plugin.MTLSSettings
	}{
		{
			name:     "No policy - behave as permissive",
			expected: expectedPermissive,
		},
		{
			name: "Single policy - disable mode",
			peerPolicies: []*config.Config{
				{
					Spec: &v1beta1.PeerAuthentication{
						Mtls: &v1beta1.PeerAuthentication_MutualTLS{
							Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
			},
			expected: plugin.MTLSSettings{Port: 8080, Mode: model.MTLSDisable},
		},
		{
			name: "Single policy - permissive mode",
			peerPolicies: []*config.Config{
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
			peerPolicies: []*config.Config{
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
			peerPolicies: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			peerPolicies: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			peerPolicies: []*config.Config{
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
			peerPolicies: []*config.Config{
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
			got := NewPolicyApplier("root-namespace", nil, tc.peerPolicies, &model.PushContext{}).InboundMTLSSettings(
				8080,
				testNode,
				[]string{},
			)
			if diff := cmp.Diff(tc.expected, got, protocmp.Transform()); diff != "" {
				t.Errorf("unexpected filter chains: %v", diff)
			}
		})
	}
}

func TestComposePeerAuthentication(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		configs []*config.Config
		want    *v1beta1.PeerAuthentication
	}{
		{
			name:    "no config",
			configs: []*config.Config{},
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "mesh only",
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			name: "ignore non-emptypb selector in root namespace",
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
			want: &v1beta1.PeerAuthentication{
				Mtls: &v1beta1.PeerAuthentication_MutualTLS{
					Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
				},
			},
		},
		{
			name: "workload vs namespace config",
			configs: []*config.Config{
				{
					Meta: config.Meta{
						Name:      "default",
						Namespace: "my-ns",
					},
					Spec: &v1beta1.PeerAuthentication{},
				},
				{
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			configs: []*config.Config{
				{
					Meta: config.Meta{
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
					Meta: config.Meta{
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
			if got := ComposePeerAuthentication("root-namespace", tt.configs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("composePeerAuthentication() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMutualTLSMode(t *testing.T) {
	tests := []struct {
		name string
		in   *v1beta1.PeerAuthentication_MutualTLS
		want model.MutualTLSMode
	}{
		{
			name: "unset",
			in: &v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_UNSET,
			},
			want: model.MTLSUnknown,
		},
		{
			name: "disable",
			in: &v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_DISABLE,
			},
			want: model.MTLSDisable,
		},
		{
			name: "permissive",
			in: &v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
			},
			want: model.MTLSPermissive,
		},
		{
			name: "strict",
			in: &v1beta1.PeerAuthentication_MutualTLS{
				Mode: v1beta1.PeerAuthentication_MutualTLS_STRICT,
			},
			want: model.MTLSStrict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getMutualTLSMode(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getMutualTLSMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
