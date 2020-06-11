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
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	xdsfilters "istio.io/istio/pilot/pkg/proxy/envoy/filters"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	protovalue "istio.io/istio/pkg/proto"
)

func TestBuildInboundFilterChain(t *testing.T) {
	tlsContext := func(alpnProtocols []string) *auth.DownstreamTlsContext {
		return &auth.DownstreamTlsContext{
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
				AlpnProtocols: alpnProtocols,
			},
			RequireClientCertificate: protovalue.BoolTrue,
		}
	}

	type args struct {
		mTLSMode         model.MutualTLSMode
		sdsUdsPath       string
		node             *model.Proxy
		listenerProtocol networking.ListenerProtocol
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
			// No need to set up filter chain, default one is okay.
			want: nil,
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
			want: nil,
		},
		{
			name: "MTLSStrict",
			args: args{
				mTLSMode: model.MTLSStrict,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			want: []networking.FilterChain{
				{
					TLSContext: tlsContext([]string{"h2", "http/1.1"}),
				},
			},
		},
		{
			name: "MTLSPermissive",
			args: args{
				mTLSMode: model.MTLSPermissive,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolTCP,
			},
			// Two filter chains, one for mtls traffic within the mesh, one for plain text traffic.
			want: []networking.FilterChain{
				{
					TLSContext: tlsContext([]string{"istio-peer-exchange", "h2", "http/1.1"}),
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
			},
		},
		{
			name: "MTLSStrict using SDS",
			args: args{
				mTLSMode:   model.MTLSStrict,
				sdsUdsPath: "/tmp/sdsuds.sock",
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{
						SdsEnabled: true,
					},
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
										InitialFetchTimeout: features.InitialFetchTimeout,
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType: core.ApiConfigSource_GRPC,
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
									DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{})},
									ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
										Name: "ROOTCA",
										SdsConfig: &core.ConfigSource{
											InitialFetchTimeout: features.InitialFetchTimeout,
											ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
												ApiConfigSource: &core.ApiConfigSource{
													ApiType: core.ApiConfigSource_GRPC,
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
						},
						RequireClientCertificate: protovalue.BoolTrue,
					},
				},
			},
		},
		{
			name: "MTLSStrict using SDS without node meta",
			args: args{
				mTLSMode:   model.MTLSStrict,
				sdsUdsPath: "/tmp/sdsuds.sock",
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			want: []networking.FilterChain{
				{
					TLSContext: tlsContext([]string{"h2", "http/1.1"}),
				},
			},
		},
		{
			name: "MTLSStrict with custom cert paths from proxy node metadata",
			args: args{
				mTLSMode: model.MTLSStrict,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{
						TLSServerCertChain: "/custom/path/to/cert-chain.pem",
						TLSServerKey:       "/custom-key.pem",
						TLSServerRootCert:  "/custom/path/to/root.pem",
					},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			// Only one filter chain with mTLS settings should be generated.
			want: []networking.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificates: []*auth.TlsCertificate{
								{
									CertificateChain: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "/custom/path/to/cert-chain.pem",
										},
									},
									PrivateKey: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "/custom-key.pem",
										},
									},
								},
							},
							ValidationContextType: &auth.CommonTlsContext_ValidationContext{
								ValidationContext: &auth.CertificateValidationContext{
									TrustedCa: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "/custom/path/to/root.pem",
										},
									},
								},
							},
							AlpnProtocols: []string{"h2", "http/1.1"},
						},
						RequireClientCertificate: protovalue.BoolTrue,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildInboundFilterChain(tt.args.mTLSMode, tt.args.sdsUdsPath, tt.args.node, tt.args.listenerProtocol); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildInboundFilterChain() = %v, want %v", spew.Sdump(got), spew.Sdump(tt.want))
				t.Logf("got:\n%v\n", got[0].TLSContext.CommonTlsContext.TlsCertificateSdsSecretConfigs[0])
			}
		})
	}
}
