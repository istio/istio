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

package utils

import (
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	protovalue "istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/spiffe"
)

func TestBuildInboundFilterChain(t *testing.T) {
	type args struct {
		mTLSMode         model.MutualTLSMode
		node             *model.Proxy
		listenerProtocol networking.ListenerProtocol
		trustDomains     []string
	}
	tests := []struct {
		name string
		args args
		want []networking.FilterChain
	}{
		{
			name: "MTLSUnknown",
			args: args{
				mTLSMode: model.MTLSUnknown,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolAuto,
			},
			want: []networking.FilterChain{{}},
		},
		{
			name: "MTLSDisable",
			args: args{
				mTLSMode: model.MTLSDisable,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolAuto,
			},
			want: []networking.FilterChain{{}},
		},
		{
			name: "MTLSStrict using SDS",
			args: args{
				mTLSMode: model.MTLSStrict,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			want: []networking.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								{
									Name: "default",
									SdsConfig: &core.ConfigSource{
										InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
										ResourceApiVersion:  core.ApiVersion_V3,
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
														},
													},
												},
											},
										},
									},
								},
							},
							ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
								CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
									DefaultValidationContext: &auth.CertificateValidationContext{},
									ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
										Name: "ROOTCA",
										SdsConfig: &core.ConfigSource{
											InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
											ResourceApiVersion:  core.ApiVersion_V3,
											ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
												ApiConfigSource: &core.ApiConfigSource{
													ApiType:                   core.ApiConfigSource_GRPC,
													SetNodeOnFirstMessageOnly: true,
													TransportApiVersion:       core.ApiVersion_V3,
													GrpcServices: []*core.GrpcService{
														{
															TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
																EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							AlpnProtocols: []string{"h2", "http/1.1"},
							TlsParams: &auth.TlsParameters{
								TlsMinimumProtocolVersion: auth.TlsParameters_TLSv1_2,
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
					},
				},
			},
		},
		{
			name: "MTLSStrict using SDS with local trust domain",
			args: args{
				mTLSMode: model.MTLSStrict,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolTCP,
				trustDomains:     []string{"cluster.local"},
			},
			want: []networking.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								{
									Name: "default",
									SdsConfig: &core.ConfigSource{
										InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
										ResourceApiVersion:  core.ApiVersion_V3,
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
														},
													},
												},
											},
										},
									},
								},
							},
							ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
								CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
									DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: []*matcher.StringMatcher{
										{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: spiffe.URIPrefix + spiffe.GetTrustDomain() + "/"}},
									}},
									ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
										Name: "ROOTCA",
										SdsConfig: &core.ConfigSource{
											InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
											ResourceApiVersion:  core.ApiVersion_V3,
											ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
												ApiConfigSource: &core.ApiConfigSource{
													ApiType:                   core.ApiConfigSource_GRPC,
													SetNodeOnFirstMessageOnly: true,
													TransportApiVersion:       core.ApiVersion_V3,
													GrpcServices: []*core.GrpcService{
														{
															TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
																EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							AlpnProtocols: []string{"istio-peer-exchange", "h2", "http/1.1"},
							TlsParams: &auth.TlsParameters{
								TlsMinimumProtocolVersion: auth.TlsParameters_TLSv1_2,
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
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildInboundFilterChain(tt.args.mTLSMode, tt.args.node, tt.args.listenerProtocol, tt.args.trustDomains)
			if diff := cmp.Diff(got, tt.want, protocmp.Transform()); diff != "" {
				t.Errorf("BuildInboundFilterChain() = %v", diff)
			}
		})
	}
}
