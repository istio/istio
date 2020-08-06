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

package v1alpha3

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	networking "istio.io/api/networking/v1alpha3"

	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	pilot_model "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/model"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/proto"
)

func TestBuildGatewayListenerTlsContext(t *testing.T) {
	testCases := []struct {
		name    string
		server  *networking.Server
		sdsPath string
		result  *auth.DownstreamTlsContext
	}{
		{
			name: "mesh SDS disabled, tls mode ISTIO_MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificates: []*auth.TlsCertificate{
						{
							CertificateChain: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: constants.DefaultCertChain,
								},
							},
							PrivateKey: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: constants.DefaultKey,
								},
							},
						},
					},
					ValidationContextType: &auth.CommonTlsContext_ValidationContext{
						ValidationContext: &auth.CertificateValidationContext{
							TrustedCa: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: constants.DefaultRootCert,
								},
							},
						},
					},
				},
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{
			name: "mesh SDS enabled, tls mode ISTIO_MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			sdsPath: "unix:/var/run/sds/uds_path",
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "default",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: model.SDSClusterName},
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
									InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
									ResourceApiVersion:  core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:             core.ApiConfigSource_GRPC,
											TransportApiVersion: core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: model.SDSClusterName},
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
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{ // No credential name is specified, generate file paths for key/cert.
			name: "no credential name no key no cert tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_SIMPLE,
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificates: []*auth.TlsCertificate{
						{
							CertificateChain: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "",
								},
							},
							PrivateKey: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "",
								},
							},
						},
					},
				},
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{ // Credential name is specified, SDS config is generated for fetching key/cert.
			name: "credential name no key no cert tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:           networking.ServerTLSSettings_SIMPLE,
					CredentialName: "ingress-sds-resource-name",
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: features.InitialFetchTimeout,
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.GatewaySdsUdsPath,
														StatPrefix: model.SDSStatPrefix,
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
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{ // Credential name and subject alternative names are specified, generate SDS configs for
			// key/cert and static validation context config.
			name: "credential name subject alternative name no key no cert tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:            networking.ServerTLSSettings_SIMPLE,
					CredentialName:  "ingress-sds-resource-name",
					SubjectAltNames: []string{"subject.name.a.com", "subject.name.b.com"},
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: features.InitialFetchTimeout,
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.GatewaySdsUdsPath,
														StatPrefix: model.SDSStatPrefix,
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
							MatchSubjectAltNames: util.StringToExactMatch([]string{"subject.name.a.com", "subject.name.b.com"}),
						},
					},
				},
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{
			name: "no credential name key and cert tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_SIMPLE,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificates: []*auth.TlsCertificate{
						{
							CertificateChain: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "server-cert.crt",
								},
							},
							PrivateKey: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "private-key.key",
								},
							},
						},
					},
				},
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{
			name: "no credential name key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_MUTUAL,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificates: []*auth.TlsCertificate{
						{
							CertificateChain: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "server-cert.crt",
								},
							},
							PrivateKey: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "private-key.key",
								},
							},
						},
					},
				},
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{ // Credential name and subject names are specified, SDS configs are generated for fetching
			// key/cert and root cert.
			name: "credential name subject alternative name key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_MUTUAL,
					CredentialName:    "ingress-sds-resource-name",
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
					SubjectAltNames:   []string{"subject.name.a.com", "subject.name.b.com"},
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: features.InitialFetchTimeout,
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.GatewaySdsUdsPath,
														StatPrefix: model.SDSStatPrefix,
													},
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
							DefaultValidationContext: &auth.CertificateValidationContext{
								MatchSubjectAltNames: util.StringToExactMatch([]string{"subject.name.a.com", "subject.name.b.com"}),
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name: "ingress-sds-resource-name-cacert",
								SdsConfig: &core.ConfigSource{
									InitialFetchTimeout: features.InitialFetchTimeout,
									ResourceApiVersion:  core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:             core.ApiConfigSource_GRPC,
											TransportApiVersion: core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_GoogleGrpc_{
														GoogleGrpc: &core.GrpcService_GoogleGrpc{
															TargetUri:  model.GatewaySdsUdsPath,
															StatPrefix: model.SDSStatPrefix,
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
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{ // Credential name and VerifyCertificateSpki options are specified, SDS configs are generated for fetching
			// key/cert and root cert
			name: "credential name verify spki key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:                  networking.ServerTLSSettings_MUTUAL,
					CredentialName:        "ingress-sds-resource-name",
					VerifyCertificateSpki: []string{"abcdef"},
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: features.InitialFetchTimeout,
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.GatewaySdsUdsPath,
														StatPrefix: model.SDSStatPrefix,
													},
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
							DefaultValidationContext: &auth.CertificateValidationContext{
								VerifyCertificateSpki: []string{"abcdef"},
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name: "ingress-sds-resource-name-cacert",
								SdsConfig: &core.ConfigSource{
									InitialFetchTimeout: features.InitialFetchTimeout,
									ResourceApiVersion:  core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:             core.ApiConfigSource_GRPC,
											TransportApiVersion: core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_GoogleGrpc_{
														GoogleGrpc: &core.GrpcService_GoogleGrpc{
															TargetUri:  model.GatewaySdsUdsPath,
															StatPrefix: model.SDSStatPrefix,
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
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{ // Credential name and VerifyCertificateHash options are specified, SDS configs are generated for fetching
			// key/cert and root cert
			name: "credential name verify hash key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:                  networking.ServerTLSSettings_MUTUAL,
					CredentialName:        "ingress-sds-resource-name",
					VerifyCertificateHash: []string{"fedcba"},
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: features.InitialFetchTimeout,
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.GatewaySdsUdsPath,
														StatPrefix: model.SDSStatPrefix,
													},
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
							DefaultValidationContext: &auth.CertificateValidationContext{
								VerifyCertificateHash: []string{"fedcba"},
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name: "ingress-sds-resource-name-cacert",
								SdsConfig: &core.ConfigSource{
									InitialFetchTimeout: features.InitialFetchTimeout,
									ResourceApiVersion:  core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:             core.ApiConfigSource_GRPC,
											TransportApiVersion: core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_GoogleGrpc_{
														GoogleGrpc: &core.GrpcService_GoogleGrpc{
															TargetUri:  model.GatewaySdsUdsPath,
															StatPrefix: model.SDSStatPrefix,
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
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{
			name: "no credential name key and cert tls PASSTHROUGH",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_PASSTHROUGH,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			result: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret := buildGatewayListenerTLSContext(tc.server, tc.sdsPath, &pilot_model.NodeMetadata{SdsEnabled: true}, v3.ListenerType)
			if diff := cmp.Diff(tc.result, ret, protocmp.Transform()); diff != "" {
				t.Errorf("got diff: %v", diff)
			}
		})
	}
}

func TestCreateGatewayHTTPFilterChainOpts(t *testing.T) {
	testCases := []struct {
		name        string
		node        *pilot_model.Proxy
		server      *networking.Server
		routeName   string
		proxyConfig *meshconfig.ProxyConfig
		result      *filterChainOpts
	}{
		{
			name: "HTTP1.0 mode enabled",
			node: &pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{HTTP10: "1"},
			},
			server: &networking.Server{
				Port: &networking.Port{},
			},
			routeName:   "some-route",
			proxyConfig: nil,
			result: &filterChainOpts{
				sniHosts:   nil,
				tlsContext: nil,
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        0,
						ForwardClientCertDetails: hcm.HttpConnectionManager_SANITIZE_SET,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName: EnvoyServerName,
						HttpProtocolOptions: &core.Http1ProtocolOptions{
							AcceptHttp_10: true,
						},
					},
				},
			},
		},
		{
			name: "Duplicate hosts in TLS filterChain",
			node: &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			server: &networking.Server{
				Port: &networking.Port{
					Protocol: "HTTPS",
				},
				Hosts: []string{"example.org", "example.org"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			routeName:   "some-route",
			proxyConfig: nil,
			result: &filterChainOpts{
				sniHosts: []string{"example.org"},
				tlsContext: &auth.DownstreamTlsContext{
					RequireClientCertificate: proto.BoolTrue,
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
				},
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        0,
						ForwardClientCertDetails: hcm.HttpConnectionManager_SANITIZE_SET,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName:          EnvoyServerName,
						HttpProtocolOptions: &core.Http1ProtocolOptions{},
					},
				},
			},
		},
		{
			name: "Unique hosts in TLS filterChain",
			node: &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			server: &networking.Server{
				Port: &networking.Port{
					Protocol: "HTTPS",
				},
				Hosts: []string{"example.org", "test.org"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			routeName:   "some-route",
			proxyConfig: nil,
			result: &filterChainOpts{
				sniHosts: []string{"example.org", "test.org"},
				tlsContext: &auth.DownstreamTlsContext{
					RequireClientCertificate: proto.BoolTrue,
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
				},
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        0,
						ForwardClientCertDetails: hcm.HttpConnectionManager_SANITIZE_SET,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName:          EnvoyServerName,
						HttpProtocolOptions: &core.Http1ProtocolOptions{},
					},
				},
			},
		},
		{
			name: "Wildcard hosts in TLS filterChain are not duplicates",
			node: &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			server: &networking.Server{
				Port: &networking.Port{
					Protocol: "HTTPS",
				},
				Hosts: []string{"*.example.org", "example.org"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			routeName:   "some-route",
			proxyConfig: nil,
			result: &filterChainOpts{
				sniHosts: []string{"*.example.org", "example.org"},
				tlsContext: &auth.DownstreamTlsContext{
					RequireClientCertificate: proto.BoolTrue,
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
				},
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        0,
						ForwardClientCertDetails: hcm.HttpConnectionManager_SANITIZE_SET,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName:          EnvoyServerName,
						HttpProtocolOptions: &core.Http1ProtocolOptions{},
					},
				},
			},
		},
		{
			name: "Topology HTTP Protocol",
			node: &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			server: &networking.Server{
				Port: &networking.Port{},
			},
			routeName: "some-route",
			proxyConfig: &meshconfig.ProxyConfig{
				GatewayTopology: &meshconfig.Topology{
					NumTrustedProxies:        2,
					ForwardClientCertDetails: meshconfig.Topology_APPEND_FORWARD,
				},
			},
			result: &filterChainOpts{
				sniHosts:   nil,
				tlsContext: nil,
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        2,
						ForwardClientCertDetails: hcm.HttpConnectionManager_APPEND_FORWARD,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName:          EnvoyServerName,
						HttpProtocolOptions: &core.Http1ProtocolOptions{},
					},
				},
			},
		},
		{
			name: "Topology HTTPS Protocol",
			node: &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			server: &networking.Server{
				Port: &networking.Port{
					Protocol: "HTTPS",
				},
				Hosts: []string{"example.org"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			routeName: "some-route",
			proxyConfig: &meshconfig.ProxyConfig{
				GatewayTopology: &meshconfig.Topology{
					NumTrustedProxies:        3,
					ForwardClientCertDetails: meshconfig.Topology_FORWARD_ONLY,
				},
			},
			result: &filterChainOpts{
				sniHosts: []string{"example.org"},
				tlsContext: &auth.DownstreamTlsContext{
					RequireClientCertificate: proto.BoolTrue,
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
				},
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        3,
						ForwardClientCertDetails: hcm.HttpConnectionManager_FORWARD_ONLY,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName:          EnvoyServerName,
						HttpProtocolOptions: &core.Http1ProtocolOptions{},
					},
				},
			},
		},
		{
			name: "HTTPS Protocol with server name",
			node: &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			server: &networking.Server{
				Name: "server1",
				Port: &networking.Port{
					Protocol: "HTTPS",
				},
				Hosts: []string{"example.org"},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			routeName: "some-route",
			proxyConfig: &meshconfig.ProxyConfig{
				GatewayTopology: &meshconfig.Topology{
					NumTrustedProxies:        3,
					ForwardClientCertDetails: meshconfig.Topology_FORWARD_ONLY,
				},
			},
			result: &filterChainOpts{
				sniHosts: []string{"example.org"},
				tlsContext: &auth.DownstreamTlsContext{
					RequireClientCertificate: proto.BoolTrue,
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
				},
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        3,
						ForwardClientCertDetails: hcm.HttpConnectionManager_FORWARD_ONLY,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName:          EnvoyServerName,
						HttpProtocolOptions: &core.Http1ProtocolOptions{},
					},
					statPrefix: "server1",
				},
			},
		},
	}

	for _, tc := range testCases {
		cgi := NewConfigGenerator([]plugin.Plugin{})
		tc.node.MergedGateway = &pilot_model.MergedGateway{SNIHostsByServer: map[*networking.Server][]string{
			tc.server: pilot_model.GetSNIHostsForServer(tc.server),
		}}
		ret := cgi.createGatewayHTTPFilterChainOpts(tc.node, tc.server, tc.routeName, "", tc.proxyConfig)
		if !reflect.DeepEqual(tc.result, ret) {
			t.Errorf("test case %s: expecting %+v but got %+v", tc.name, tc.result.httpOpts.connectionManager, ret.httpOpts.connectionManager)
		}
	}
}

func TestGatewayHTTPRouteConfig(t *testing.T) {
	httpsRedirectGateway := pilot_model.Config{
		ConfigMeta: pilot_model.ConfigMeta{
			Name:             "gateway-redirect",
			Namespace:        "default",
			GroupVersionKind: gvk.Gateway,
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": "ingressgateway"},
			Servers: []*networking.Server{
				{
					Hosts: []string{"example.org"},
					Port:  &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
					Tls:   &networking.ServerTLSSettings{HttpsRedirect: true},
				},
			},
		},
	}
	httpGateway := pilot_model.Config{
		ConfigMeta: pilot_model.ConfigMeta{
			Name:             "gateway",
			Namespace:        "default",
			GroupVersionKind: gvk.Gateway,
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": "ingressgateway"},
			Servers: []*networking.Server{
				{
					Hosts: []string{"example.org"},
					Port:  &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
				},
			},
		},
	}
	virtualServiceSpec := &networking.VirtualService{
		Hosts:    []string{"example.org"},
		Gateways: []string{"gateway", "gateway-redirect"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "example.org",
							Port: &networking.PortSelector{
								Number: 80,
							},
						},
					},
				},
			},
		},
	}
	virtualService := pilot_model.Config{
		ConfigMeta: pilot_model.ConfigMeta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service",
			Namespace:        "default",
		},
		Spec: virtualServiceSpec,
	}
	virtualServiceCopy := pilot_model.Config{
		ConfigMeta: pilot_model.ConfigMeta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service-copy",
			Namespace:        "default",
		},
		Spec: virtualServiceSpec,
	}
	virtualServiceWildcard := pilot_model.Config{
		ConfigMeta: pilot_model.ConfigMeta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service-wildcard",
			Namespace:        "default",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"gateway", "gateway-redirect"},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}
	cases := []struct {
		name                 string
		virtualServices      []pilot_model.Config
		gateways             []pilot_model.Config
		routeName            string
		expectedVirtualHosts map[string][]string
		expectedHTTPRoutes   map[string]int
	}{
		{
			"404 when no services",
			[]pilot_model.Config{},
			[]pilot_model.Config{httpGateway},
			"http.80",
			map[string][]string{
				"blackhole:80": {
					"*",
				},
			},
			map[string]int{"blackhole:80": 1},
		},
		{
			"virtual services do not matter when tls redirect is set",
			[]pilot_model.Config{virtualService},
			[]pilot_model.Config{httpsRedirectGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string]int{"example.org:80": 0},
		},
		{
			"no merging of virtual services when tls redirect is set",
			[]pilot_model.Config{virtualService, virtualServiceCopy},
			[]pilot_model.Config{httpsRedirectGateway, httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string]int{"example.org:80": 0},
		},
		{
			"add a route for a virtual service",
			[]pilot_model.Config{virtualService},
			[]pilot_model.Config{httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string]int{"example.org:80": 1},
		},
		{
			"duplicate virtual service should merge",
			[]pilot_model.Config{virtualService, virtualServiceCopy},
			[]pilot_model.Config{httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string]int{"example.org:80": 2},
		},
		{
			"duplicate by wildcard should merge",
			[]pilot_model.Config{virtualService, virtualServiceWildcard},
			[]pilot_model.Config{httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string]int{"example.org:80": 2},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := &fakePlugin{}
			configgen := NewConfigGenerator([]plugin.Plugin{p})
			env := buildEnv(t, tt.gateways, tt.virtualServices)
			proxyGateway.SetGatewaysForProxy(env.PushContext)
			route := configgen.buildGatewayHTTPRouteConfig(&proxyGateway, env.PushContext, tt.routeName)
			if route == nil {
				t.Fatal("got an empty route configuration")
			}
			vh := make(map[string][]string)
			hr := make(map[string]int)
			for _, h := range route.VirtualHosts {
				vh[h.Name] = h.Domains
				hr[h.Name] = len(h.Routes)
				if h.Name != "blackhole:80" && !h.IncludeRequestAttemptCount {
					t.Errorf("expected attempt count to be set in virtual host, but not found")
				}
			}
			if !reflect.DeepEqual(tt.expectedVirtualHosts, vh) {
				t.Errorf("got unexpected virtual hosts. Expected: %v, Got: %v", tt.expectedVirtualHosts, vh)
			}
			if !reflect.DeepEqual(tt.expectedHTTPRoutes, hr) {
				t.Errorf("got unexpected number of http routes. Expected: %v, Got: %v", tt.expectedHTTPRoutes, hr)
			}
		})
	}

}

func TestBuildGatewayListeners(t *testing.T) {
	cases := []struct {
		name              string
		node              *pilot_model.Proxy
		gateway           *networking.Gateway
		expectedListeners []string
	}{
		{
			"targetPort overrides service port",
			&pilot_model.Proxy{
				ServiceInstances: []*pilot_model.ServiceInstance{
					{
						Service: &pilot_model.Service{
							Hostname: "test",
						},
						ServicePort: &pilot_model.Port{
							Port: 80,
						},
						Endpoint: &pilot_model.IstioEndpoint{
							EndpointPort: 8080,
						},
					},
				},
			},
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
					},
				},
			},
			[]string{"0.0.0.0_8080"},
		},
		{
			"multiple ports",
			&pilot_model.Proxy{},
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
					},
					{
						Port: &networking.Port{Name: "http", Number: 801, Protocol: "HTTP"},
					},
				},
			},
			[]string{"0.0.0.0_80", "0.0.0.0_801"},
		},
	}

	for _, tt := range cases {
		p := &fakePlugin{}
		configgen := NewConfigGenerator([]plugin.Plugin{p})
		env := buildEnv(t, []pilot_model.Config{{ConfigMeta: pilot_model.ConfigMeta{GroupVersionKind: gvk.Gateway}, Spec: tt.gateway}}, []pilot_model.Config{})
		proxyGateway.SetGatewaysForProxy(env.PushContext)
		proxyGateway.ServiceInstances = tt.node.ServiceInstances
		proxyGateway.DiscoverIPVersions()
		builder := configgen.buildGatewayListeners(&ListenerBuilder{node: &proxyGateway, push: env.PushContext})
		var listeners []string
		for _, l := range builder.gatewayListeners {
			if err := l.Validate(); err != nil {
				t.Fatalf("Validation failed for listener %s with error %v", l.Name, err)
			}
			listeners = append(listeners, l.Name)
		}
		sort.Strings(listeners)
		sort.Strings(tt.expectedListeners)
		if !reflect.DeepEqual(listeners, tt.expectedListeners) {
			t.Fatalf("Expected listeners: %v, got: %v\n%v", tt.expectedListeners, listeners, proxyGateway.MergedGateway.Servers)
		}
	}
}

func buildEnv(t *testing.T, gateways []pilot_model.Config, virtualServices []pilot_model.Config) pilot_model.Environment {
	serviceDiscovery := memregistry.NewServiceDiscovery(nil)

	configStore := pilot_model.MakeIstioStore(memory.MakeWithoutValidation(collections.Pilot))
	for _, cfg := range gateways {
		if _, err := configStore.Create(cfg); err != nil {
			panic(err.Error())
		}
	}
	for _, cfg := range virtualServices {
		if _, err := configStore.Create(cfg); err != nil {
			panic(err.Error())
		}
	}
	m := mesh.DefaultMeshConfig()
	env := pilot_model.Environment{
		PushContext:      pilot_model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Watcher:          mesh.NewFixedWatcher(&m),
	}

	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		t.Fatalf("failed to init push context: %v", err)
	}
	return env
}
