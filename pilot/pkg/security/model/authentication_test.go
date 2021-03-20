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
	"github.com/golang/protobuf/ptypes"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/model"
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
			if got := ConstructSdsSecretConfig(c.secretName, &model.Proxy{}); !cmp.Equal(got, c.expected, protocmp.Transform()) {
				t.Errorf("ConstructSdsSecretConfig: got(%#v), want(%#v)\n", got, c.expected)
			}
		})
	}
}

func TestConstructValidationContext(t *testing.T) {
	testCases := []struct {
		name            string
		rootCAFilePath  string
		subjectAltNames []string
		expected        *auth.CommonTlsContext_ValidationContext
	}{
		{
			name:            "default CA",
			rootCAFilePath:  "/root/ca",
			subjectAltNames: []string{"SystemCACertificates.keychain", "SystemRootCertificates.keychain"},
			expected: &auth.CommonTlsContext_ValidationContext{
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: "/root/ca",
						},
					},
					MatchSubjectAltNames: []*matcher.StringMatcher{
						{
							MatchPattern: &matcher.StringMatcher_Exact{
								Exact: "SystemCACertificates.keychain",
							},
						},
						{
							MatchPattern: &matcher.StringMatcher_Exact{
								Exact: "SystemRootCertificates.keychain",
							},
						},
					},
				},
			},
		},
		{
			name:           "default CA without subjectAltNames",
			rootCAFilePath: "/root/ca",
			expected: &auth.CommonTlsContext_ValidationContext{
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: "/root/ca",
						},
					},
				},
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConstructValidationContext(c.rootCAFilePath, c.subjectAltNames); !cmp.Equal(got, c.expected, protocmp.Transform()) {
				t.Errorf("ConstructValidationContext: got(%#v), want(%#v)\n", got, c.expected)
			}
		})
	}
}

func TestApplyToCommonTLSContext(t *testing.T) {
	testCases := []struct {
		name               string
		node               *model.Proxy
		trustDomainAliases []string
		validateClient     bool
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
							InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
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
								InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
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
							InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
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
								InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
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
							InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
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
								InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
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
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			tlsContext := &auth.CommonTlsContext{}
			ApplyToCommonTLSContext(tlsContext, test.node, []string{}, test.trustDomainAliases, test.validateClient)

			if !cmp.Equal(tlsContext, test.expected, protocmp.Transform()) {
				t.Errorf("got(%#v), want(%#v)\n", spew.Sdump(tlsContext), spew.Sdump(test.expected))
			}
		})
	}
}
