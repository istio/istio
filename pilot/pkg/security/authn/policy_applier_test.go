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

package authn

import (
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"

	"istio.io/api/security/v1beta1"
	type_beta "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/jwt"
	protovalue "istio.io/istio/pkg/proto"
	istiotest "istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestJwtFilter(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name          string
		in            []*config.Config
		jwksFetchMode jwt.JwksFetchMode
		expected      *hcm.HttpFilter
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
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with Mesh cluster as issuer and remote jwks mode Hybrid",
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
			jwksFetchMode: jwt.Hybrid,
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with Mesh cluster as issuer and remote jwks mode Envoy",
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
			jwksFetchMode: jwt.Envoy,
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with non Mesh cluster as issuer and remote jwks mode Hybrid",
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
			jwksFetchMode: jwt.Hybrid,
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "JWT policy with non Mesh cluster as issuer and remote jwks mode Envoy",
			in: []*config.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "invalid|7443|",
								JwksUri: "http://invalid-issuer.com:7443/jwks",
							},
						},
					},
				},
			},
			jwksFetchMode: jwt.Envoy,
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									JwksSourceSpecifier: &envoy_jwt.JwtProvider_RemoteJwks{
										RemoteJwks: &envoy_jwt.RemoteJwks{
											HttpUri: &core.HttpUri{
												Uri: "http://invalid-issuer.com:7443/jwks",
												HttpUpstreamType: &core.HttpUri_Cluster{
													Cluster: "outbound|7443||invalid-issuer.com",
												},
												Timeout: &durationpb.Duration{Seconds: 5},
											},
											CacheDuration: &durationpb.Duration{Seconds: 5 * 60},
										},
									},
									Forward:           false,
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
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
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
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
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
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
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
												InlineString: model.FakeJwks,
											},
										},
									},
									Forward:           false,
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
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
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
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
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									PayloadInMetadata:    "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
								},
							},
							BypassCorsPreflight: true,
						}),
				},
			},
		},
		{
			name: "Output claim to header",
			in: []*config.Config{
				{
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:               "https://secret.foo.com",
								JwksUri:              jwksURI,
								ForwardOriginalToken: true,
								OutputClaimToHeaders: []*v1beta1.ClaimToHeader{
									{Header: "x-jwt-key1", Claim: "value1"},
									{Header: "x-jwt-key2", Claim: "value2"},
								},
							},
						},
					},
				},
			},
			expected: &hcm.HttpFilter{
				Name: "envoy.filters.http.jwt_authn",
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(
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
									Forward: true,
									ClaimToHeaders: []*envoy_jwt.JwtClaimToHeader{
										{HeaderName: "x-jwt-key1", ClaimName: "value1"},
										{HeaderName: "x-jwt-key2", ClaimName: "value2"},
									},
									PayloadInMetadata: "payload",
									NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
										SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
									},
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
		model.JwtPubKeyRefreshIntervalOnFailure, 10*time.Millisecond)

	defer push.JwtKeyResolver.Close()

	push.ServiceIndex.HostnameAndNamespace[host.Name("jwt-token-issuer.mesh")] = map[string]*model.Service{}
	push.ServiceIndex.HostnameAndNamespace[host.Name("jwt-token-issuer.mesh")]["mesh"] = &model.Service{
		Hostname: "jwt-token-issuer.mesh.svc.cluster.local",
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			istiotest.SetForTest(t, &features.JwksFetchMode, c.jwksFetchMode)
			if got := newPolicyApplier("root-namespace", c.in, nil, push).JwtFilter(false); !reflect.DeepEqual(c.expected, got) {
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
						PayloadInMetadata: "payload",
						NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
							SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
						},
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
						PayloadInMetadata: "payload",
						NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
							SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
						},
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
						PayloadInMetadata: "payload",
						NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
							SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
						},
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
									InlineString: model.FakeJwks,
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "payload",
						NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
							SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
						},
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
									InlineString: model.FakeJwks,
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "payload",
						NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
							SpaceDelimitedClaims: buildSpaceDelimitedClaims(nil),
						},
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name: "Single JWT policy with custom space delimited claims",
			in: []*v1beta1.JWTRule{
				{
					Issuer:               "https://secret.foo.com",
					JwksUri:              jwksURI,
					SpaceDelimitedClaims: []string{"custom_scope", "roles"},
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
						PayloadInMetadata: "payload",
						NormalizePayloadInMetadata: &envoy_jwt.JwtProvider_NormalizePayload{
							SpaceDelimitedClaims: buildSpaceDelimitedClaims([]string{"custom_scope", "roles"}),
						},
					},
				},
				BypassCorsPreflight: true,
			},
		},
	}

	push := &model.PushContext{}
	push.JwtKeyResolver = model.NewJwksResolver(
		model.JwtPubKeyEvictionDuration, model.JwtPubKeyRefreshInterval,
		model.JwtPubKeyRefreshIntervalOnFailure, 10*time.Millisecond)
	defer push.JwtKeyResolver.Close()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := convertToEnvoyJwtConfig(c.in, push, false); !reflect.DeepEqual(c.expected, got) {
				t.Errorf("got:\n%s\nwanted:\n%s\n%s\n", spew.Sdump(got), spew.Sdump(c.expected), cmp.Diff(c.expected, got, protocmp.Transform()))
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
	tlsContextHTTP := protomarshal.Clone(tlsContext)
	tlsContextHTTP.CommonTlsContext.AlpnProtocols = []string{"h2", "http/1.1"}

	expectedStrict := MTLSSettings{
		Port: 8080,
		Mode: model.MTLSStrict,
		TCP:  tlsContext,
		HTTP: tlsContextHTTP,
	}
	expectedPermissive := MTLSSettings{
		Port: 8080,
		Mode: model.MTLSPermissive,
		TCP:  tlsContext,
		HTTP: tlsContextHTTP,
	}

	cases := []struct {
		name         string
		peerPolicies []*config.Config
		expected     MTLSSettings
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
			expected: MTLSSettings{Port: 8080, Mode: model.MTLSDisable},
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
		Labels: map[string]string{
			"app": "foo",
		},
		Metadata: &model.NodeMetadata{},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newPolicyApplier("root-namespace", nil, tc.peerPolicies, &model.PushContext{}).InboundMTLSSettings(
				8080,
				testNode,
				[]string{},
				NoOverride,
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
		want    MergedPeerAuthentication
	}{
		{
			name:    "no config",
			configs: []*config.Config{},
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSStrict,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSPermissive,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSStrict,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSStrict,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSStrict,
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
			want: MergedPeerAuthentication{
				Mode: model.MTLSStrict,
				PerPort: map[uint32]model.MutualTLSMode{
					80:  model.MTLSDisable,
					90:  model.MTLSStrict,
					100: model.MTLSStrict,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComposePeerAuthentication("root-namespace", tt.configs)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestBuildSpaceDelimitedClaims(t *testing.T) {
	cases := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil input returns defaults",
			input:    nil,
			expected: []string{"permission", "scope"},
		},
		{
			name:     "empty input returns defaults",
			input:    []string{},
			expected: []string{"permission", "scope"},
		},
		{
			name:     "custom claims with defaults included",
			input:    []string{"custom_scope", "roles"},
			expected: []string{"custom_scope", "permission", "roles", "scope"},
		},
		{
			name:     "custom claims with duplicate defaults",
			input:    []string{"scope", "custom_scope", "permission", "roles"},
			expected: []string{"custom_scope", "permission", "roles", "scope"},
		},
		{
			name:     "only custom claims",
			input:    []string{"custom1", "custom2"},
			expected: []string{"custom1", "custom2", "permission", "scope"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildSpaceDelimitedClaims(c.input)

			// Since the function sorts the result, we can compare directly
			if !reflect.DeepEqual(got, c.expected) {
				t.Errorf("buildSpaceDelimitedClaims() = %v, expected %v", got, c.expected)
			}
		})
	}
}
