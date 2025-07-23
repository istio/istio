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

package model

import (
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
)

func TestConstructSdsSecretConfig(t *testing.T) {
	testCases := []struct {
		name       string
		secretName string
		expected   *auth.SdsSecretConfig
	}{
		{
			name:       "ConstructSdsSecretConfig",
			secretName: "spiffe://cluster.local/ns/bar/sa/foo",
			expected: &auth.SdsSecretConfig{
				Name: "spiffe://cluster.local/ns/bar/sa/foo",
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
										EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "ConstructSdsSecretConfig without secretName",
			secretName: "",
			expected:   nil,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConstructSdsSecretConfig(c.secretName); !cmp.Equal(got, c.expected, protocmp.Transform()) {
				t.Errorf("ConstructSdsSecretConfig: got(%#v), want(%#v)\n", got, c.expected)
			}
		})
	}
}

func TestApplyToCommonTLSContext(t *testing.T) {
	testCases := []struct {
		name               string
		node               *model.Proxy
		trustDomainAliases []string
		crl                string
		validateClient     bool
		tlsCertificates    []*networking.ServerTLSSettings_TLSCertificate
		expected           *auth.CommonTlsContext
	}{
		{
			name: "MTLSStrict using SDS",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			validateClient: true,
			expected: &auth.CommonTlsContext{
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
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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
		{
			name: "MTLSStrict using SDS and SAN aliases",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			validateClient:     true,
			trustDomainAliases: []string{"alias-1.domain", "some-other-alias-1.domain", "alias-2.domain"},
			expected: &auth.CommonTlsContext{
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
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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
							{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: spiffe.URIPrefix + "alias-1.domain" + "/"}},
							{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: spiffe.URIPrefix + "some-other-alias-1.domain" + "/"}},
							{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: spiffe.URIPrefix + "alias-2.domain" + "/"}},
						}},
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
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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
		{
			name: "MTLS using SDS with custom certs in metadata",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					TLSServerCertChain: "serverCertChain",
					TLSServerKey:       "serverKey",
					TLSServerRootCert:  "servrRootCert",
				},
			},
			validateClient: true,
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "file-cert:serverCertChain~serverKey",
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType:                   core.ApiConfigSource_GRPC,
									SetNodeOnFirstMessageOnly: true,
									TransportApiVersion:       core.ApiVersion_V3,
									GrpcServices: []*core.GrpcService{
										{
											TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
											},
										},
									},
								},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &auth.CertificateValidationContext{},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "file-root:servrRootCert",
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
												},
											},
										},
									},
								},
								ResourceApiVersion: core.ApiVersion_V3,
							},
						},
					},
				},
			},
		},
		{
			name: "ISTIO_MUTUAL SDS without node meta",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			validateClient: true,
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
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
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
											},
										},
									},
								},
							},
							ResourceApiVersion:  core.ApiVersion_V3,
							InitialFetchTimeout: durationpb.New(time.Second * 0),
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &auth.CertificateValidationContext{},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
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
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
												},
											},
										},
									},
								},
								ResourceApiVersion:  core.ApiVersion_V3,
								InitialFetchTimeout: durationpb.New(time.Second * 0),
							},
						},
					},
				},
			},
		},
		{
			name: "ISTIO_MUTUAL with custom cert paths from proxy node metadata",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					TLSServerCertChain: "/custom/path/to/cert-chain.pem",
					TLSServerKey:       "/custom-key.pem",
					TLSServerRootCert:  "/custom/path/to/root.pem",
				},
			},
			validateClient: true,
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "file-cert:/custom/path/to/cert-chain.pem~/custom-key.pem",
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType:                   core.ApiConfigSource_GRPC,
									SetNodeOnFirstMessageOnly: true,
									TransportApiVersion:       core.ApiVersion_V3,
									GrpcServices: []*core.GrpcService{
										{
											TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
											},
										},
									},
								},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &auth.CertificateValidationContext{},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "file-root:/custom/path/to/root.pem",
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
												},
											},
										},
									},
								},
								ResourceApiVersion: core.ApiVersion_V3,
							},
						},
					},
				},
			},
		},
		{
			name: "SIMPLE with custom cert paths from proxy node metadata without cacerts",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					TLSServerCertChain: "/custom/path/to/cert-chain.pem",
					TLSServerKey:       "/custom-key.pem",
				},
			},
			validateClient: false,
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "file-cert:/custom/path/to/cert-chain.pem~/custom-key.pem",
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType:                   core.ApiConfigSource_GRPC,
									SetNodeOnFirstMessageOnly: true,
									TransportApiVersion:       core.ApiVersion_V3,
									GrpcServices: []*core.GrpcService{
										{
											TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
											},
										},
									},
								},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
				},
			},
		},
		{
			name: "MTLSStrict using SDS with CRL",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			validateClient: true,
			crl:            "/custom/path/to/crl.pem",
			expected: &auth.CommonTlsContext{
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
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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
							Crl: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: "/custom/path/to/crl.pem",
								},
							},
						},
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
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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
		{
			name: "Multiple TLSServerCertificates",
			node: &model.Proxy{Metadata: &model.NodeMetadata{
				TLSServerRootCert: "/path/to/root.pem",
			}},
			tlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
				{
					ServerCertificate: "/path/to/cert1.pem",
					PrivateKey:        "/path/to/key1.pem",
				},
				{
					ServerCertificate: "/path/to/cert2.pem",
					PrivateKey:        "/path/to/key2.pem",
				},
			},
			validateClient: true,
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "file-cert:/path/to/cert1.pem~/path/to/key1.pem",
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType:                   core.ApiConfigSource_GRPC,
									SetNodeOnFirstMessageOnly: true,
									TransportApiVersion:       core.ApiVersion_V3,
									GrpcServices: []*core.GrpcService{
										{
											TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
											},
										},
									},
								},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
					{
						Name: "file-cert:/path/to/cert2.pem~/path/to/key2.pem",
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType:                   core.ApiConfigSource_GRPC,
									SetNodeOnFirstMessageOnly: true,
									TransportApiVersion:       core.ApiVersion_V3,
									GrpcServices: []*core.GrpcService{
										{
											TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
											},
										},
									},
								},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &auth.CertificateValidationContext{},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "file-root:/path/to/root.pem",
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:                   core.ApiConfigSource_GRPC,
										SetNodeOnFirstMessageOnly: true,
										TransportApiVersion:       core.ApiVersion_V3,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
												},
											},
										},
									},
								},
								ResourceApiVersion: core.ApiVersion_V3,
							},
						},
					},
				},
			},
		},
		{
			name: "Nil TLSServerCertificates",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			validateClient: true,
			expected: &auth.CommonTlsContext{
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
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
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

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			tlsContext := &auth.CommonTlsContext{}
			ApplyToCommonTLSContext(tlsContext, test.node, []string{}, test.crl, test.trustDomainAliases, test.validateClient, test.tlsCertificates)

			if !cmp.Equal(tlsContext, test.expected, protocmp.Transform()) {
				t.Errorf("got(%#v), want(%#v)\n", spew.Sdump(tlsContext), spew.Sdump(test.expected))
			}
		})
	}
}

func TestConstructSdsSecretConfigForCredential(t *testing.T) {
	cases := []struct {
		name                   string
		credentialSocketExists bool
		push                   *model.PushContext
		expected               *auth.SdsSecretConfig
	}{
		{
			name:                   "",
			credentialSocketExists: false,
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			expected:               nil,
		},
		{
			name:                   "builtin://",
			credentialSocketExists: false,
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			expected: &auth.SdsSecretConfig{
				Name: SDSDefaultResourceName,
				SdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType:                   core.ApiConfigSource_GRPC,
							SetNodeOnFirstMessageOnly: true,
							TransportApiVersion:       core.ApiVersion_V3,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
										EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
									},
								},
							},
						},
					},
					ResourceApiVersion:  core.ApiVersion_V3,
					InitialFetchTimeout: durationpb.New(time.Second * 0),
				},
			},
		},
		{
			name:                   "builtin://-cacert",
			credentialSocketExists: false,
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			expected: &auth.SdsSecretConfig{
				Name: SDSRootResourceName,
				SdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType:                   core.ApiConfigSource_GRPC,
							SetNodeOnFirstMessageOnly: true,
							TransportApiVersion:       core.ApiVersion_V3,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
										EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
									},
								},
							},
						},
					},
					ResourceApiVersion:  core.ApiVersion_V3,
					InitialFetchTimeout: durationpb.New(time.Second * 0),
				},
			},
		},
		{
			name:                   "sds://external-cert",
			credentialSocketExists: true,
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			expected: &auth.SdsSecretConfig{
				Name: "sds://external-cert",
				SdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType:                   core.ApiConfigSource_GRPC,
							SetNodeOnFirstMessageOnly: true,
							TransportApiVersion:       core.ApiVersion_V3,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
										EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: security.SDSExternalClusterName},
									},
								},
							},
						},
					},
					ResourceApiVersion: core.ApiVersion_V3,
				},
			},
		},
		{
			name:                   "test-cert",
			credentialSocketExists: false,
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			expected: &auth.SdsSecretConfig{
				Name:      "kubernetes://test-cert",
				SdsConfig: SDSAdsConfig,
			},
		},
		{
			name:                   "sds://provider-cert",
			credentialSocketExists: false,
			push: func() *model.PushContext {
				pc := &model.PushContext{
					Mesh: &meshconfig.MeshConfig{
						ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
							{
								Name: "provider-cert",
								Provider: &meshconfig.MeshConfig_ExtensionProvider_Sds{
									Sds: &meshconfig.MeshConfig_ExtensionProvider_SDSProvider{
										Name:    "provider-cert",
										Service: "sds-provider-service",
										Port:    8080,
									},
								},
							},
						},
					},
				}
				pc.ServiceIndex.HostnameAndNamespace = map[host.Name]map[string]*model.Service{
					"sds-provider-service": {
						"": &model.Service{
							Hostname: "sds-provider-service",
							Ports: []*model.Port{
								{
									Name:     "grpc",
									Port:     8080,
									Protocol: protocol.GRPC,
								},
							},
						},
					},
				}
				return pc
			}(),
			expected: &auth.SdsSecretConfig{
				Name: "sds://provider-cert",
				SdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType:                   core.ApiConfigSource_GRPC,
							SetNodeOnFirstMessageOnly: true,
							TransportApiVersion:       core.ApiVersion_V3,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
										EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "outbound|8080||sds-provider-service"},
									},
								},
							},
						},
					},
					ResourceApiVersion: core.ApiVersion_V3,
				},
			},
		},
		{
			name:                   "sds://provider-cert",
			credentialSocketExists: false,
			push: func() *model.PushContext {
				pc := &model.PushContext{
					Mesh: &meshconfig.MeshConfig{
						ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
							{
								Name: "provider-cert",
								Provider: &meshconfig.MeshConfig_ExtensionProvider_Sds{
									Sds: &meshconfig.MeshConfig_ExtensionProvider_SDSProvider{
										Name:    "provider-cert",
										Service: "missing-service",
										Port:    8080,
									},
								},
							},
						},
					},
				}
				// ServiceIndex is empty, so LookupCluster will fail
				pc.ServiceIndex.HostnameAndNamespace = map[host.Name]map[string]*model.Service{}
				return pc
			}(),
			expected: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConstructSdsSecretConfigForCredential(c.name, c.credentialSocketExists, c.push); !cmp.Equal(got, c.expected, protocmp.Transform()) {
				t.Errorf("ConstructSdsSecretConfigForCredential() = %v, want %v", got, c.expected)
			}
		})
	}
}

func TestApplyCredentialSDSToServerCommonTLSContext(t *testing.T) {
	tests := []struct {
		name                   string
		tlsOpts                *networking.ServerTLSSettings
		credentialSocketExist  bool
		expectedCertCount      int
		expectedValidation     bool
		expectedValidationType string
		push                   *model.PushContext
	}{
		{
			name: "single certificate with credential name",
			tlsOpts: &networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "test-cert",
			},
			credentialSocketExist:  false,
			expectedCertCount:      1,
			expectedValidation:     false,
			expectedValidationType: "",
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
		},
		{
			name: "multiple certificates with credential names",
			tlsOpts: &networking.ServerTLSSettings{
				Mode:            networking.ServerTLSSettings_SIMPLE,
				CredentialNames: []string{"rsa-cert", "ecdsa-cert"},
			},
			credentialSocketExist:  false,
			expectedCertCount:      2,
			expectedValidation:     false,
			expectedValidationType: "",
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
		},
		{
			name: "credential name with validation context",
			tlsOpts: &networking.ServerTLSSettings{
				Mode:            networking.ServerTLSSettings_SIMPLE,
				CredentialName:  "test-cert",
				SubjectAltNames: []string{"test.com"},
			},
			credentialSocketExist:  false,
			expectedCertCount:      1,
			expectedValidation:     true,
			expectedValidationType: "ValidationContext",
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
		},
		{
			name: "credential name with socket",
			tlsOpts: &networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "sds://external-cert",
			},
			credentialSocketExist:  true,
			expectedCertCount:      1,
			expectedValidation:     false,
			expectedValidationType: "",
			push:                   &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsContext := &auth.CommonTlsContext{}
			ApplyCredentialSDSToServerCommonTLSContext(tlsContext, tt.tlsOpts, tt.credentialSocketExist, tt.push)

			// Check certificate count
			if len(tlsContext.TlsCertificateSdsSecretConfigs) != tt.expectedCertCount {
				t.Errorf("expected %d certificates, got %d", tt.expectedCertCount, len(tlsContext.TlsCertificateSdsSecretConfigs))
			}

			// Check validation context
			if tt.expectedValidation {
				if tlsContext.ValidationContextType == nil {
					t.Error("expected validation context to be set")
				}
				switch tt.expectedValidationType {
				case "CombinedValidationContext":
					combinedCtx, ok := tlsContext.ValidationContextType.(*auth.CommonTlsContext_CombinedValidationContext)
					if !ok {
						t.Error("expected CombinedValidationContext")
					}
					if combinedCtx.CombinedValidationContext == nil {
						t.Error("expected CombinedValidationContext to be set")
					}
				case "ValidationContext":
					validationCtx, ok := tlsContext.ValidationContextType.(*auth.CommonTlsContext_ValidationContext)
					if !ok {
						t.Error("expected ValidationContext")
					}
					if validationCtx.ValidationContext == nil {
						t.Error("expected ValidationContext to be set")
					}
				default:
					t.Errorf("unexpected validation type: %s", tt.expectedValidationType)
				}
			} else if tlsContext.ValidationContextType != nil {
				t.Error("unexpected validation context")
			}
		})
	}
}
