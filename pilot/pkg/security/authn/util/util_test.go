package util

import (
	"reflect"
	"testing"

	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	protovalue "istio.io/istio/pkg/proto"
)

func TestBuildInboundFilterChain(t *testing.T) {
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
			AlpnProtocols: []string{"istio-peer-exchange", "h2", "http/1.1"},
		},
		RequireClientCertificate: protovalue.BoolTrue,
	}

	type args struct {
		mTLSMode   model.MutualTLSMode
		sdsUdsPath string
		node       *model.Proxy
	}
	tests := []struct {
		name string
		args args
		want []plugin.FilterChain
	}{
		{
			name: "MTLSUnknown",
			args: args{
				mTLSMode: model.MTLSUnknown,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
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
			},
			want: []plugin.FilterChain{
				{
					TLSContext: tlsContext,
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
			},
			// Two filter chains, one for mtls traffic within the mesh, one for plain text traffic.
			want: []plugin.FilterChain{
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
			},
		},
		{
			name: "MTLSStrict using SDS",
			args: args{
				mTLSMode:   model.MTLSStrict,
				sdsUdsPath: "/tmp/sdsuds.sock",
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			want: []plugin.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								ConstructSDSConfig(authn_model.SDSDefaultResourceName, "/tmp/sdsuds.sock"),
							},
							ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
								CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
									DefaultValidationContext:         &auth.CertificateValidationContext{VerifySubjectAltName: []string{} /*subjectAltNames*/},
									ValidationContextSdsSecretConfig: ConstructSDSConfig(authn_model.SDSRootResourceName, "/tmp/sdsuds.sock"),
								},
							},
							AlpnProtocols: []string{"istio-peer-exchange", "h2", "http/1.1"},
						},
						RequireClientCertificate: protovalue.BoolTrue,
					},
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
			},
			// Only one filter chain with mTLS settings should be generated.
			want: []plugin.FilterChain{
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
							AlpnProtocols: []string{"istio-peer-exchange", "h2", "http/1.1"},
						},
						RequireClientCertificate: protovalue.BoolTrue,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildInboundFilterChain(tt.args.mTLSMode, tt.args.sdsUdsPath, tt.args.node); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildInboundFilterChain() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConstructSDSConfig(t *testing.T) {
	tests := []struct {
		name       string
		sdsudspath string
		want       *auth.SdsSecretConfig
	}{
		{
			name:       "Basic",
			sdsudspath: "/tmp/sdsuds.sock",
			want: &auth.SdsSecretConfig{
				Name: "Basic",
				SdsConfig: &core.ConfigSource{
					InitialFetchTimeout: features.InitialFetchTimeout,
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType: core.ApiConfigSource_GRPC,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_GoogleGrpc_{
										GoogleGrpc: &core.GrpcService_GoogleGrpc{
											TargetUri:              "/tmp/sdsuds.sock",
											StatPrefix:             authn_model.SDSStatPrefix,
											CredentialsFactoryName: "envoy.grpc_credentials.file_based_metadata",
											ChannelCredentials: &core.GrpcService_GoogleGrpc_ChannelCredentials{
												CredentialSpecifier: &core.GrpcService_GoogleGrpc_ChannelCredentials_LocalCredentials{
													LocalCredentials: &core.GrpcService_GoogleGrpc_GoogleLocalCredentials{},
												},
											},
											CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
												{
													CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_FromPlugin{
														FromPlugin: &core.GrpcService_GoogleGrpc_CallCredentials_MetadataCredentialsFromPlugin{
															Name: "envoy.grpc_credentials.file_based_metadata",
															ConfigType: &core.GrpcService_GoogleGrpc_CallCredentials_MetadataCredentialsFromPlugin_TypedConfig{
																TypedConfig: &any.Any{
																	TypeUrl: "type.googleapis.com/envoy.config.grpc_credential.v2alpha.FileBasedMetadataConfig",
																	Value:   []byte("\n%\n#/var/run/secrets/tokens/istio-token\022 istio_sds_credentials_header-bin"),
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConstructSDSConfig(tt.name, tt.sdsudspath); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConstructSDSConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}
