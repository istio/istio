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

package v1alpha3

import (
	"reflect"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/proto"

	networking "istio.io/api/networking/v1alpha3"
)

func TestBuildGatewayListenerTlsContext(t *testing.T) {
	testCases := []struct {
		name      string
		server    *networking.Server
		enableSds bool
		result    *auth.DownstreamTlsContext
	}{
		{ // No credential name is specified, generate file paths for key/cert.
			name: "no credential name no key no cert tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Tls: &networking.Server_TLSOptions{
					Mode: networking.Server_TLSOptions_SIMPLE,
				},
			},
			enableSds: true,
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: ListenersALPNProtocols,
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
				Tls: &networking.Server_TLSOptions{
					Mode:           networking.Server_TLSOptions_SIMPLE,
					CredentialName: "ingress-sds-resource-name",
				},
			},
			enableSds: true,
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: ListenersALPNProtocols,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType: core.ApiConfigSource_GRPC,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.IngressGatewaySdsUdsPath,
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
				Tls: &networking.Server_TLSOptions{
					Mode:            networking.Server_TLSOptions_SIMPLE,
					CredentialName:  "ingress-sds-resource-name",
					SubjectAltNames: []string{"subject.name.a.com", "subject.name.b.com"},
				},
			},
			enableSds: true,
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: ListenersALPNProtocols,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType: core.ApiConfigSource_GRPC,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.IngressGatewaySdsUdsPath,
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
							VerifySubjectAltName: []string{"subject.name.a.com", "subject.name.b.com"},
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
				Tls: &networking.Server_TLSOptions{
					Mode:              networking.Server_TLSOptions_SIMPLE,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			enableSds: false,
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: ListenersALPNProtocols,
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
				Tls: &networking.Server_TLSOptions{
					Mode:              networking.Server_TLSOptions_MUTUAL,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			enableSds: true,
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: ListenersALPNProtocols,
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
				Tls: &networking.Server_TLSOptions{
					Mode:              networking.Server_TLSOptions_MUTUAL,
					CredentialName:    "ingress-sds-resource-name",
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
					SubjectAltNames:   []string{"subject.name.a.com", "subject.name.b.com"},
				},
			},
			enableSds: true,
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: ListenersALPNProtocols,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "ingress-sds-resource-name",
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType: core.ApiConfigSource_GRPC,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_GoogleGrpc_{
													GoogleGrpc: &core.GrpcService_GoogleGrpc{
														TargetUri:  model.IngressGatewaySdsUdsPath,
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
								VerifySubjectAltName: []string{"subject.name.a.com", "subject.name.b.com"},
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name: "ingress-sds-resource-name-cacert",
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType: core.ApiConfigSource_GRPC,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_GoogleGrpc_{
														GoogleGrpc: &core.GrpcService_GoogleGrpc{
															TargetUri:  model.IngressGatewaySdsUdsPath,
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
				Tls: &networking.Server_TLSOptions{
					Mode:              networking.Server_TLSOptions_PASSTHROUGH,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			enableSds: true,
			result:    nil,
		},
	}

	for _, tc := range testCases {
		ret := buildGatewayListenerTLSContext(tc.server, tc.enableSds)
		if !reflect.DeepEqual(tc.result, ret) {
			t.Errorf("test case %s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}
