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

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	pilot_model "istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/test"
)

func TestBuildGatewayListenerTlsContext(t *testing.T) {
	testCases := []struct {
		name              string
		server            *networking.Server
		result            *auth.DownstreamTlsContext
		transportProtocol istionetworking.TransportProtocol
	}{
		{
			name: "mesh SDS enabled, tls mode ISTIO_MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "default",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: durationpb.New(time.Second * 0),
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ResourceApiVersion:  core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
		{
			// regression test for having both fields set. This is rejected in validation.
			name: "tls mode ISTIO_MUTUAL, with credentialName",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:           networking.ServerTLSSettings_ISTIO_MUTUAL,
					CredentialName: "ignored",
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "default",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: durationpb.New(time.Second * 0),
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ResourceApiVersion:  core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_SIMPLE,
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "default",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: durationpb.New(time.Second * 0),
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{ // Credential name is specified, SDS config is generated for fetching key/cert.
			name: "credential name no key no cert tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
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
							Name:      "kubernetes://ingress-sds-resource-name",
							SdsConfig: model.SDSAdsConfig,
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
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
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
							Name:      "kubernetes://ingress-sds-resource-name",
							SdsConfig: model.SDSAdsConfig,
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
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_SIMPLE,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "file-cert:server-cert.crt~private-key.key",
							SdsConfig: &core.ConfigSource{
								ResourceApiVersion: core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{
			name: "no credential name key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_MUTUAL,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
					CaCertificates:    "ca-cert.crt",
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "file-cert:server-cert.crt~private-key.key",
							SdsConfig: &core.ConfigSource{
								ResourceApiVersion: core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
								Name: "file-root:ca-cert.crt",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion: core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
		{
			name: "no credential name key and cert subject alt names tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_MUTUAL,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
					CaCertificates:    "ca-cert.crt",
					SubjectAltNames:   []string{"subject.name.a.com", "subject.name.b.com"},
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "file-cert:server-cert.crt~private-key.key",
							SdsConfig: &core.ConfigSource{
								ResourceApiVersion: core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
							DefaultValidationContext: &auth.CertificateValidationContext{
								MatchSubjectAltNames: util.StringToExactMatch([]string{"subject.name.a.com", "subject.name.b.com"}),
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name: "file-root:ca-cert.crt",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion: core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
		{
			// Credential name and subject names are specified, SDS configs are generated for fetching
			// key/cert and root cert.
			name: "credential name subject alternative name key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
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
							Name:      "kubernetes://ingress-sds-resource-name",
							SdsConfig: model.SDSAdsConfig,
						},
					},
					ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &auth.CertificateValidationContext{
								MatchSubjectAltNames: util.StringToExactMatch([]string{"subject.name.a.com", "subject.name.b.com"}),
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name:      "kubernetes://ingress-sds-resource-name-cacert",
								SdsConfig: model.SDSAdsConfig,
							},
						},
					},
				},
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{
			// Credential name and VerifyCertificateSpki options are specified, SDS configs are generated for fetching
			// key/cert and root cert
			name: "credential name verify spki key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
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
							Name:      "kubernetes://ingress-sds-resource-name",
							SdsConfig: model.SDSAdsConfig,
						},
					},
					ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &auth.CertificateValidationContext{
								VerifyCertificateSpki: []string{"abcdef"},
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name:      "kubernetes://ingress-sds-resource-name-cacert",
								SdsConfig: model.SDSAdsConfig,
							},
						},
					},
				},
				RequireClientCertificate: proto.BoolTrue,
			},
		},
		{
			// Credential name and VerifyCertificateHash options are specified, SDS configs are generated for fetching
			// key/cert and root cert
			name: "credential name verify hash key and cert tls MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
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
							Name:      "kubernetes://ingress-sds-resource-name",
							SdsConfig: model.SDSAdsConfig,
						},
					},
					ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &auth.CertificateValidationContext{
								VerifyCertificateHash: []string{"fedcba"},
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name:      "kubernetes://ingress-sds-resource-name-cacert",
								SdsConfig: model.SDSAdsConfig,
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
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_PASSTHROUGH,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
				},
			},
			result: nil,
		},
		{
			name: "Downstream TLS settings for QUIC transport",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:           networking.ServerTLSSettings_SIMPLE,
					CredentialName: "httpbin-cred",
				},
			},
			transportProtocol: istionetworking.TransportProtocolQUIC,
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp3OverQUIC,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name:      "kubernetes://httpbin-cred",
							SdsConfig: model.SDSAdsConfig,
						},
					},
				},
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{
			name: "duplicated cipher suites with tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.HTTPS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_SIMPLE,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
					CipherSuites:      []string{"ECDHE-ECDSA-AES128-SHA", "ECDHE-ECDSA-AES128-SHA"},
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsParams: &auth.TlsParameters{
						CipherSuites: []string{"ECDHE-ECDSA-AES128-SHA"},
					},
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "file-cert:server-cert.crt~private-key.key",
							SdsConfig: &core.ConfigSource{
								ResourceApiVersion: core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
				RequireClientCertificate: proto.BoolFalse,
			},
		},
		{
			// tcp server is non-istio mtls, no istio-peer-exchange in the alpns
			name: "tcp server with terminating (non-istio)mutual tls",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.TLS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_MUTUAL,
					ServerCertificate: "server-cert.crt",
					PrivateKey:        "private-key.key",
					CaCertificates:    "ca-cert.crt",
					SubjectAltNames:   []string{"subject.name.a.com", "subject.name.b.com"},
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNHttp,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "file-cert:server-cert.crt~private-key.key",
							SdsConfig: &core.ConfigSource{
								ResourceApiVersion: core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
							DefaultValidationContext: &auth.CertificateValidationContext{
								MatchSubjectAltNames: util.StringToExactMatch([]string{"subject.name.a.com", "subject.name.b.com"}),
							},
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name: "file-root:ca-cert.crt",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion: core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
		{
			// tcp server is istio mtls, istio-peer-exchange in the alpns
			name: "mesh SDS enabled, tcp server, tls mode ISTIO_MUTUAL",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.TLS),
				},
				Tls: &networking.ServerTLSSettings{
					Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
				},
			},
			result: &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					AlpnProtocols: util.ALPNDownstreamWithMxc,
					TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
						{
							Name: "default",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: durationpb.New(time.Second * 0),
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
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
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ResourceApiVersion:  core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
		{
			// tcp server is simple tls, no istio-peer-exchange in the alpns
			name: "tcp server, tls SIMPLE",
			server: &networking.Server{
				Hosts: []string{"httpbin.example.com", "bookinfo.example.com"},
				Port: &networking.Port{
					Protocol: string(protocol.TLS),
				},
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
							Name:      "kubernetes://ingress-sds-resource-name",
							SdsConfig: model.SDSAdsConfig,
						},
					},
				},
				RequireClientCertificate: proto.BoolFalse,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret := buildGatewayListenerTLSContext(tc.server, &pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{},
			}, tc.transportProtocol)
			if diff := cmp.Diff(tc.result, ret, protocmp.Transform()); diff != "" {
				t.Errorf("got diff: %v", diff)
			}
		})
	}
}

func TestCreateGatewayHTTPFilterChainOpts(t *testing.T) {
	var stripPortMode *hcm.HttpConnectionManager_StripAnyHostPort
	testCases := []struct {
		name              string
		node              *pilot_model.Proxy
		server            *networking.Server
		routeName         string
		proxyConfig       *meshconfig.ProxyConfig
		result            *filterChainOpts
		transportProtocol istionetworking.TransportProtocol
	}{
		{
			name: "HTTP1.0 mode enabled",
			node: &pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{HTTP10: "1"},
			},
			server: &networking.Server{
				Port: &networking.Port{
					Protocol: protocol.HTTP.String(),
				},
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
						StripPortMode: stripPortMode,
					},
					class:    istionetworking.ListenerClassGateway,
					protocol: protocol.HTTP,
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
					CommonTlsContext: &auth.CommonTlsContext{
						AlpnProtocols: util.ALPNHttp,
						TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
							{
								Name: "default",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion:  core.ApiVersion_V3,
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
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
						StripPortMode:       stripPortMode,
					},
					class:    istionetworking.ListenerClassGateway,
					protocol: protocol.HTTPS,
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
					CommonTlsContext: &auth.CommonTlsContext{
						AlpnProtocols: util.ALPNHttp,
						TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
							{
								Name: "default",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion:  core.ApiVersion_V3,
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
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
						StripPortMode:       stripPortMode,
					},
					class:    istionetworking.ListenerClassGateway,
					protocol: protocol.HTTPS,
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
					CommonTlsContext: &auth.CommonTlsContext{
						AlpnProtocols: util.ALPNHttp,
						TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
							{
								Name: "default",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion:  core.ApiVersion_V3,
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
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
						StripPortMode:       stripPortMode,
					},
					class:    istionetworking.ListenerClassGateway,
					protocol: protocol.HTTPS,
				},
			},
		},
		{
			name: "Topology HTTP Protocol",
			node: &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			server: &networking.Server{
				Port: &networking.Port{
					Protocol: protocol.HTTP.String(),
				},
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
						StripPortMode:       stripPortMode,
					},
					class:    istionetworking.ListenerClassGateway,
					protocol: protocol.HTTP,
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
					CommonTlsContext: &auth.CommonTlsContext{
						AlpnProtocols: util.ALPNHttp,
						TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
							{
								Name: "default",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion:  core.ApiVersion_V3,
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
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
						StripPortMode:       stripPortMode,
					},
					class:    istionetworking.ListenerClassGateway,
					protocol: protocol.HTTPS,
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
						AlpnProtocols: util.ALPNHttp,
						TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
							{
								Name: "default",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion:  core.ApiVersion_V3,
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
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
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
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
						StripPortMode:       stripPortMode,
					},
					statPrefix: "server1",
					class:      istionetworking.ListenerClassGateway,
					protocol:   protocol.HTTPS,
				},
			},
		},
		{
			name:              "QUIC protocol with server name",
			node:              &pilot_model.Proxy{Metadata: &pilot_model.NodeMetadata{}},
			transportProtocol: istionetworking.TransportProtocolQUIC,
			server: &networking.Server{
				Name: "server1",
				Port: &networking.Port{
					Name:       "https-app",
					Number:     443,
					TargetPort: 8443,
					Protocol:   "HTTPS",
				},
				Hosts: []string{"example.org"},
				Tls: &networking.ServerTLSSettings{
					Mode:              networking.ServerTLSSettings_SIMPLE,
					ServerCertificate: "/etc/cert/example.crt",
					PrivateKey:        "/etc/cert/example.key",
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
					RequireClientCertificate: proto.BoolFalse,
					CommonTlsContext: &auth.CommonTlsContext{
						AlpnProtocols: util.ALPNHttp3OverQUIC,
						TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
							{
								Name: "file-cert:/etc/cert/example.crt~/etc/cert/example.key",
								SdsConfig: &core.ConfigSource{
									ResourceApiVersion: core.ApiVersion_V3,
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType: core.ApiConfigSource_GRPC,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
															ClusterName: "sds-grpc",
														},
													},
												},
											},
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
										},
									},
								},
							},
						},
					},
				},
				httpOpts: &httpListenerOpts{
					rds:       "some-route",
					http3Only: true,
					connectionManager: &hcm.HttpConnectionManager{
						XffNumTrustedHops:        3,
						ForwardClientCertDetails: hcm.HttpConnectionManager_FORWARD_ONLY,
						SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
							Subject: proto.BoolTrue,
							Cert:    true,
							Uri:     true,
							Dns:     true,
						},
						ServerName:           EnvoyServerName,
						HttpProtocolOptions:  &core.Http1ProtocolOptions{},
						Http3ProtocolOptions: &core.Http3ProtocolOptions{},
						CodecType:            hcm.HttpConnectionManager_HTTP3,
						StripPortMode:        stripPortMode,
					},
					useRemoteAddress: true,
					statPrefix:       "server1",
					class:            istionetworking.ListenerClassGateway,
					protocol:         protocol.HTTPS,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cgi := NewConfigGenerator(&pilot_model.DisabledCache{})
			tc.node.MergedGateway = &pilot_model.MergedGateway{TLSServerInfo: map[*networking.Server]*pilot_model.TLSServerInfo{
				tc.server: {SNIHosts: pilot_model.GetSNIHostsForServer(tc.server)},
			}}
			ret := cgi.createGatewayHTTPFilterChainOpts(tc.node, tc.server.Port, tc.server,
				tc.routeName, tc.proxyConfig, tc.transportProtocol)
			if diff := cmp.Diff(tc.result.tlsContext, ret.tlsContext, protocmp.Transform()); diff != "" {
				t.Errorf("got diff in tls context: %v", diff)
			}
			if !reflect.DeepEqual(tc.result.httpOpts, ret.httpOpts) {
				t.Errorf("expecting httpopts:\n %+v \nbut got:\n %+v", tc.result.httpOpts, ret.httpOpts)
			}
			if !reflect.DeepEqual(tc.result.sniHosts, ret.sniHosts) {
				t.Errorf("expecting snihosts %+v but got %+v", tc.result.sniHosts, ret.sniHosts)
			}
		})
	}
}

func TestGatewayHTTPRouteConfig(t *testing.T) {
	httpRedirectGateway := config.Config{
		Meta: config.Meta{
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
	httpRedirectGatewayWithoutVS := config.Config{
		Meta: config.Meta{
			Name:             "gateway-redirect-noroutes",
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
	httpGateway := config.Config{
		Meta: config.Meta{
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
	httpsGateway := config.Config{
		Meta: config.Meta{
			Name:             "gateway-https",
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
				{
					Hosts: []string{"example.org"},
					Port:  &networking.Port{Name: "https", Number: 443, Protocol: "HTTPS"},
					Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_TLSmode(networking.ClientTLSSettings_SIMPLE)},
				},
			},
		},
	}
	httpsGatewayRedirect := config.Config{
		Meta: config.Meta{
			Name:             "gateway-https",
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
				{
					Hosts: []string{"example.org"},
					Port:  &networking.Port{Name: "https", Number: 443, Protocol: "HTTPS"},
					Tls:   &networking.ServerTLSSettings{HttpsRedirect: true, Mode: networking.ServerTLSSettings_TLSmode(networking.ClientTLSSettings_SIMPLE)},
				},
			},
		},
	}
	httpGatewayWildcard := config.Config{
		Meta: config.Meta{
			Name:             "gateway",
			Namespace:        "default",
			GroupVersionKind: gvk.Gateway,
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": "ingressgateway"},
			Servers: []*networking.Server{
				{
					Hosts: []string{"*"},
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
	virtualServiceHTTPSMatchSpec := &networking.VirtualService{
		Hosts:    []string{"example.org"},
		Gateways: []string{"gateway-https"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Port: 443,
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "example.default.svc.cluster.local",
							Port: &networking.PortSelector{
								Number: 8080,
							},
						},
					},
				},
			},
		},
	}
	virtualService := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service",
			Namespace:        "default",
		},
		Spec: virtualServiceSpec,
	}
	virtualServiceHTTPS := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service-https",
			Namespace:        "default",
		},
		Spec: virtualServiceHTTPSMatchSpec,
	}
	virtualServiceCopy := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service-copy",
			Namespace:        "default",
		},
		Spec: virtualServiceSpec,
	}
	virtualServiceWildcard := config.Config{
		Meta: config.Meta{
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
		name                              string
		virtualServices                   []config.Config
		gateways                          []config.Config
		routeName                         string
		expectedVirtualHosts              map[string][]string
		expectedVirtualHostsHostPortStrip map[string][]string
		expectedHTTPRoutes                map[string]int
		redirect                          bool
	}{
		{
			"404 when no services",
			[]config.Config{},
			[]config.Config{httpGateway},
			"http.80",
			map[string][]string{
				"blackhole:80": {
					"*",
				},
			},
			map[string][]string{
				"blackhole:80": {
					"*",
				},
			},
			map[string]int{"blackhole:80": 0},
			false,
		},
		{
			"tls redirect without virtual services",
			[]config.Config{virtualService},
			[]config.Config{httpRedirectGatewayWithoutVS},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			// We will setup a VHost which just redirects; no routes
			map[string]int{"example.org:80": 0},
			true,
		},
		{
			"virtual services with tls redirect",
			[]config.Config{virtualService},
			[]config.Config{httpRedirectGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 1},
			true,
		},
		{
			"merging of virtual services when tls redirect is set",
			[]config.Config{virtualService, virtualServiceCopy},
			[]config.Config{httpRedirectGateway, httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 4},
			true,
		},
		{
			"reverse merging of virtual services when tls redirect is set",
			[]config.Config{virtualService, virtualServiceCopy},
			[]config.Config{httpGateway, httpRedirectGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 4},
			true,
		},
		{
			"merging of virtual services when tls redirect is set without VS",
			[]config.Config{virtualService, virtualServiceCopy},
			[]config.Config{httpGateway, httpRedirectGatewayWithoutVS},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 2},
			true,
		},
		{
			"reverse merging of virtual services when tls redirect is set without VS",
			[]config.Config{virtualService, virtualServiceCopy},
			[]config.Config{httpRedirectGatewayWithoutVS, httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 2},
			true,
		},
		{
			"add a route for a virtual service",
			[]config.Config{virtualService},
			[]config.Config{httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 1},
			false,
		},
		{
			"duplicate virtual service should merge",
			[]config.Config{virtualService, virtualServiceCopy},
			[]config.Config{httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 2},
			false,
		},
		{
			"duplicate by wildcard should merge",
			[]config.Config{virtualService, virtualServiceWildcard},
			[]config.Config{httpGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {
					"example.org", "example.org:*",
				},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			map[string]int{"example.org:80": 2},
			false,
		},
		{
			"wildcard virtual service",
			[]config.Config{virtualServiceWildcard},
			[]config.Config{httpGatewayWildcard},
			"http.80",
			map[string][]string{
				"*.org:80": {"*.org", "*.org:80"},
			},
			map[string][]string{
				"*.org:80": {"*.org"},
			},
			map[string]int{"*.org:80": 1},
			false,
		},
		{
			"http redirection not working when virtualservice not match http port",
			[]config.Config{virtualServiceHTTPS},
			[]config.Config{httpsGateway},
			"https.443.https.gateway-https.default",
			map[string][]string{
				"example.org:443": {"example.org", "example.org:*"},
			},
			map[string][]string{
				"example.org:443": {"example.org"},
			},
			map[string]int{"example.org:443": 1},
			false,
		},
		{
			"http redirection not working when virtualservice not match http port",
			[]config.Config{virtualServiceHTTPS},
			[]config.Config{httpsGateway},
			"http.80",
			map[string][]string{
				"example.org:80": {"example.org", "example.org:*"},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			// We will setup a VHost which just redirects; no routes
			map[string]int{"example.org:80": 0},
			true,
		},
		{
			"http & https redirection not working when virtualservice not match http port",
			[]config.Config{virtualServiceHTTPS},
			[]config.Config{httpsGatewayRedirect},
			"https.443.https.gateway-https.default",
			map[string][]string{
				"example.org:443": {"example.org", "example.org:*"},
			},
			map[string][]string{
				"example.org:443": {"example.org"},
			},
			map[string]int{"example.org:443": 1},
			true,
		},
		{
			"http & https redirection not working when virtualservice not match http port",
			[]config.Config{virtualServiceHTTPS},
			[]config.Config{httpsGatewayRedirect},
			"http.80",
			map[string][]string{
				"example.org:80": {"example.org", "example.org:*"},
			},
			map[string][]string{
				"example.org:80": {"example.org"},
			},
			// We will setup a VHost which just redirects; no routes
			map[string]int{"example.org:80": 0},
			true,
		},
	}

	for _, value := range []bool{false, true} {
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				test.SetBoolForTest(t, &features.StripHostPort, value)
				cfgs := tt.gateways
				cfgs = append(cfgs, tt.virtualServices...)
				cg := NewConfigGenTest(t, TestOptions{
					Configs: cfgs,
				})
				r := cg.ConfigGen.buildGatewayHTTPRouteConfig(cg.SetupProxy(&proxyGateway), cg.PushContext(), tt.routeName)
				if r == nil {
					t.Fatal("got an empty route configuration")
				}
				vh := make(map[string][]string)
				hr := make(map[string]int)
				for _, h := range r.VirtualHosts {
					vh[h.Name] = h.Domains
					hr[h.Name] = len(h.Routes)
					if h.Name != "blackhole:80" && !h.IncludeRequestAttemptCount {
						t.Errorf("expected attempt count to be set in virtual host, but not found")
					}
					if tt.redirect != (h.RequireTls == route.VirtualHost_ALL) {
						t.Errorf("expected redirect %v, got %v", tt.redirect, h.RequireTls)
					}
				}

				if features.StripHostPort {
					if !reflect.DeepEqual(tt.expectedVirtualHostsHostPortStrip, vh) {
						t.Errorf("got unexpected virtual hosts. Expected: %v, Got: %v", tt.expectedVirtualHostsHostPortStrip, vh)
					}
				} else {
					if !reflect.DeepEqual(tt.expectedVirtualHosts, vh) {
						t.Errorf("got unexpected virtual hosts. Expected: %v, Got: %v", tt.expectedVirtualHosts, vh)
					}
				}
				if !reflect.DeepEqual(tt.expectedHTTPRoutes, hr) {
					t.Errorf("got unexpected number of http routes. Expected: %v, Got: %v", tt.expectedHTTPRoutes, hr)
				}
			})
		}
	}
}

func TestBuildGatewayListeners(t *testing.T) {
	cases := []struct {
		name              string
		node              *pilot_model.Proxy
		gateways          []config.Config
		virtualServices   []config.Config
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
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
							},
						},
					},
				},
			},
			nil,
			[]string{"0.0.0.0_8080"},
		},
		{
			"multiple ports",
			&pilot_model.Proxy{},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
							},
							{
								Port: &networking.Port{Name: "http", Number: 801, Protocol: "HTTP"},
							},
						},
					},
				},
			},
			nil,
			[]string{"0.0.0.0_80", "0.0.0.0_801"},
		},
		{
			"privileged port on unprivileged pod",
			&pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{
					UnprivilegedPod: "true",
				},
			},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
							},
							{
								Port: &networking.Port{Name: "http", Number: 8080, Protocol: "HTTP"},
							},
						},
					},
				},
			},
			nil,
			[]string{"0.0.0.0_8080"},
		},
		{
			"privileged port on privileged pod when empty env var is set",
			&pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{
					UnprivilegedPod: "",
				},
			},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
							},
							{
								Port: &networking.Port{Name: "http", Number: 8080, Protocol: "HTTP"},
							},
						},
					},
				},
			},
			nil,
			[]string{"0.0.0.0_80", "0.0.0.0_8080"},
		},
		{
			"privileged port on privileged pod",
			&pilot_model.Proxy{},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
							},
							{
								Port: &networking.Port{Name: "http", Number: 8080, Protocol: "HTTP"},
							},
						},
					},
				},
			},
			nil,
			[]string{"0.0.0.0_80", "0.0.0.0_8080"},
		},
		{
			"gateway with bind",
			&pilot_model.Proxy{},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port: &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
							},
							{
								Port:  &networking.Port{Name: "http", Number: 8080, Protocol: "HTTP"},
								Hosts: []string{"externalgatewayclient.com"},
							},
							{
								Port:  &networking.Port{Name: "http", Number: 8080, Protocol: "HTTP"},
								Bind:  "127.0.0.1",
								Hosts: []string{"internalmesh.svc.cluster.local"},
							},
						},
					},
				},
			},
			nil,
			[]string{"0.0.0.0_80", "0.0.0.0_8080", "127.0.0.1_8080"},
		},
		{
			"gateway with simple and passthrough",
			&pilot_model.Proxy{},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port:  &networking.Port{Name: "http", Number: 443, Protocol: "HTTPS"},
								Hosts: []string{"*.example.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
							{
								Port:  &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
								Hosts: []string{"foo.example.com"},
							},
						},
					},
				},
				{
					Meta: config.Meta{Name: "passthrough-gateway", Namespace: "testns", GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port:  &networking.Port{Name: "http", Number: 443, Protocol: "HTTPS"},
								Hosts: []string{"*.example.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
							{
								Port:  &networking.Port{Name: "tcp", Number: 9443, Protocol: "TLS"},
								Hosts: []string{"barone.example.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_PASSTHROUGH},
							},
							{
								Port:  &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
								Hosts: []string{"bar.example.com"},
							},
						},
					},
				},
			},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.VirtualService},
					Spec: &networking.VirtualService{
						Gateways: []string{"testns/passthrough-gateway"},
						Hosts:    []string{"barone.example.com"},
						Tls: []*networking.TLSRoute{
							{
								Match: []*networking.TLSMatchAttributes{
									{
										Port:     9443,
										SniHosts: []string{"barone.example.com"},
									},
								},
								Route: []*networking.RouteDestination{
									{
										Destination: &networking.Destination{
											Host: "foo.com",
										},
									},
								},
							},
						},
					},
				},
			},
			[]string{"0.0.0.0_443", "0.0.0.0_80", "0.0.0.0_9443"},
		},
		{
			"gateway with multiple http servers",
			&pilot_model.Proxy{},
			[]config.Config{
				{
					Meta: config.Meta{Name: "gateway1", Namespace: "testns", GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port:  &networking.Port{Name: "http", Number: 443, Protocol: "HTTPS"},
								Hosts: []string{"*.example.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
							{
								Port:  &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
								Hosts: []string{"foo.example.com"},
							},
						},
					},
				},
				{
					Meta: config.Meta{Name: "gateway2", Namespace: "testns", GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port:  &networking.Port{Name: "http", Number: 443, Protocol: "HTTPS"},
								Hosts: []string{"*.exampleone.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
							{
								Port:  &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
								Hosts: []string{"bar.example.com"},
							},
						},
					},
				},
			},
			nil,
			[]string{"0.0.0.0_443", "0.0.0.0_80"},
		},
		{
			"gateway with multiple TLS HTTPS TCP servers",
			&pilot_model.Proxy{},
			[]config.Config{
				{
					Meta: config.Meta{Name: "gateway1", Namespace: "testns", GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port:  &networking.Port{Name: "tcp", Number: 443, Protocol: "TLS"},
								Hosts: []string{"*.example.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
							{
								Port:  &networking.Port{Name: "tcp", Number: 443, Protocol: "HTTPS"},
								Hosts: []string{"https.example.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
							{
								Port:  &networking.Port{Name: "tcp", Number: 9443, Protocol: "TCP"},
								Hosts: []string{"tcp.example.com"},
							},
						},
					},
				},
				{
					Meta: config.Meta{Name: "gateway2", Namespace: "testns", GroupVersionKind: gvk.Gateway},
					Spec: &networking.Gateway{
						Servers: []*networking.Server{
							{
								Port:  &networking.Port{Name: "http", Number: 443, Protocol: "HTTPS"},
								Hosts: []string{"*.exampleone.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
							{
								Port:  &networking.Port{Name: "tcp", Number: 443, Protocol: "TLS"},
								Hosts: []string{"*.exampleone.com"},
								Tls:   &networking.ServerTLSSettings{CredentialName: "test", Mode: networking.ServerTLSSettings_SIMPLE},
							},
						},
					},
				},
			},
			[]config.Config{
				{
					Meta: config.Meta{Name: uuid.NewString(), Namespace: uuid.NewString(), GroupVersionKind: gvk.VirtualService},
					Spec: &networking.VirtualService{
						Gateways: []string{"testns/gateway1"},
						Hosts:    []string{"tcp.example.com"},
						Tcp: []*networking.TCPRoute{
							{
								Match: []*networking.L4MatchAttributes{
									{
										Port: 9443,
									},
								},
								Route: []*networking.RouteDestination{
									{
										Destination: &networking.Destination{
											Host: "foo.com",
										},
									},
								},
							},
						},
					},
				},
			},
			[]string{"0.0.0.0_443", "0.0.0.0_9443"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			Configs := make([]config.Config, 0)
			Configs = append(Configs, tt.gateways...)
			Configs = append(Configs, tt.virtualServices...)
			cg := NewConfigGenTest(t, TestOptions{
				Configs: Configs,
			})
			cg.MemRegistry.WantGetProxyServiceInstances = tt.node.ServiceInstances
			proxy := cg.SetupProxy(&proxyGateway)
			if tt.node.Metadata != nil {
				proxy.Metadata = tt.node.Metadata
			} else {
				proxy.Metadata = &proxyGatewayMetadata
			}

			builder := cg.ConfigGen.buildGatewayListeners(NewListenerBuilder(proxy, cg.PushContext()))
			listeners := xdstest.ExtractListenerNames(builder.gatewayListeners)
			sort.Strings(listeners)
			sort.Strings(tt.expectedListeners)
			if !reflect.DeepEqual(listeners, tt.expectedListeners) {
				t.Fatalf("Expected listeners: %v, got: %v\n%v", tt.expectedListeners, listeners, proxyGateway.MergedGateway.MergedServers)
			}
			xdstest.ValidateListeners(t, builder.gatewayListeners)

			// gateways bind to port, but exact_balance can still be used
			for _, l := range builder.gatewayListeners {
				if l.ConnectionBalanceConfig != nil {
					t.Fatalf("expected connection balance config to be empty, found %v", l.ConnectionBalanceConfig)
				}
			}
		})
	}
}

func TestBuildNameToServiceMapForHttpRoutes(t *testing.T) {
	virtualServiceSpec := &networking.VirtualService{
		Hosts: []string{"*.example.org"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
						},
					},
					{
						Destination: &networking.Destination{
							Host: "bar.example.org",
						},
					},
				},
				Mirror: &networking.Destination{
					Host: "baz.example.org",
				},
			},
		},
	}
	virtualService := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service",
			Namespace:        "test",
		},
		Spec: virtualServiceSpec,
	}

	fooHostName := host.Name("foo.example.org")
	fooServiceInTestNamespace := &pilot_model.Service{
		Hostname: fooHostName,
		Ports: []*pilot_model.Port{{
			Name:     "http",
			Protocol: "HTTP",
			Port:     80,
		}},
		Attributes: pilot_model.ServiceAttributes{
			Namespace: "test",
			ExportTo: map[visibility.Instance]bool{
				visibility.Private: true,
			},
		},
	}

	barHostName := host.Name("bar.example.org")
	barServiceInDefaultNamespace := &pilot_model.Service{
		Hostname: barHostName,
		Ports: []*pilot_model.Port{{
			Name:     "http",
			Protocol: "HTTP",
			Port:     8080,
		}},
		Attributes: pilot_model.ServiceAttributes{
			Namespace: "default",
			ExportTo: map[visibility.Instance]bool{
				visibility.Public: true,
			},
		},
	}

	bazHostName := host.Name("baz.example.org")
	bazServiceInDefaultNamespace := &pilot_model.Service{
		Hostname: bazHostName,
		Ports: []*pilot_model.Port{{
			Name:     "http",
			Protocol: "HTTP",
			Port:     8090,
		}},
		Attributes: pilot_model.ServiceAttributes{
			Namespace: "default",
			ExportTo: map[visibility.Instance]bool{
				visibility.Private: true,
			},
		},
	}

	cg := NewConfigGenTest(t, TestOptions{
		Configs:  []config.Config{virtualService},
		Services: []*pilot_model.Service{fooServiceInTestNamespace, barServiceInDefaultNamespace, bazServiceInDefaultNamespace},
	})
	proxy := &pilot_model.Proxy{
		Type:            pilot_model.Router,
		ConfigNamespace: "test",
	}
	proxy = cg.SetupProxy(proxy)

	nameToServiceMap := buildNameToServiceMapForHTTPRoutes(proxy, cg.env.PushContext, virtualService)

	if len(nameToServiceMap) != 3 {
		t.Errorf("The length of nameToServiceMap is wrong.")
	}

	if service, exist := nameToServiceMap[fooHostName]; !exist || service == nil {
		t.Errorf("The service of %s not found or should be not nil.", fooHostName)
	} else {
		if service.Ports[0].Port != 80 {
			t.Errorf("The port of %s is wrong.", fooHostName)
		}

		if service.Attributes.Namespace != "test" {
			t.Errorf("The namespace of %s is wrong.", fooHostName)
		}
	}

	if service, exist := nameToServiceMap[barHostName]; !exist || service == nil {
		t.Errorf("The service of %s not found or should be not nil", barHostName)
	} else {
		if service.Ports[0].Port != 8080 {
			t.Errorf("The port of %s is wrong.", barHostName)
		}

		if service.Attributes.Namespace != "default" {
			t.Errorf("The namespace of %s is wrong.", barHostName)
		}
	}

	if service, exist := nameToServiceMap[bazHostName]; !exist || service != nil {
		t.Errorf("The value of hostname %s mapping must be exist and it should be nil.", bazHostName)
	}
}
