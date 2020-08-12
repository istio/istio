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

	"istio.io/istio/pkg/spiffe"

	"github.com/golang/protobuf/ptypes"

	"github.com/davecgh/go-spew/spew"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

func TestConstructSdsSecretConfigWithCustomUds(t *testing.T) {
	testCases := []struct {
		name           string
		serviceAccount string
		sdsUdsPath     string
		expected       *auth.SdsSecretConfig
	}{
		{
			name:           "CustomUds with serviceAccount and sdsUdsPath",
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			expected: &auth.SdsSecretConfig{
				Name: "spiffe://cluster.local/ns/bar/sa/foo",
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
											TargetUri:  "/tmp/sdsuds.sock",
											StatPrefix: SDSStatPrefix,
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
			name:           "CustomUds without service account",
			serviceAccount: "",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			expected:       nil,
		},
		{
			name:           "CustomUds without sdsUdsPath",
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "",
			expected:       nil,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConstructSdsSecretConfigWithCustomUds(c.serviceAccount, c.sdsUdsPath, v3.ListenerType); !cmp.Equal(got, c.expected, protocmp.Transform()) {
				t.Errorf("ConstructSdsSecretConfigWithCustomUds: got(%#v), want(%#v)\n", got, c.expected)
			}
		})
	}
}

func TestConstructSdsSecretConfig(t *testing.T) {
	testCases := []struct {
		name           string
		serviceAccount string
		sdsUdsPath     string
		expected       *auth.SdsSecretConfig
	}{
		{
			name:           "ConstructSdsSecretConfig",
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			expected: &auth.SdsSecretConfig{
				Name: "spiffe://cluster.local/ns/bar/sa/foo",
				SdsConfig: &core.ConfigSource{
					InitialFetchTimeout: features.InitialFetchTimeout,
					ResourceApiVersion:  core.ApiVersion_V3,
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType:             core.ApiConfigSource_GRPC,
							TransportApiVersion: core.ApiVersion_V3,
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
			name:           "ConstructSdsSecretConfig without serviceAccount",
			serviceAccount: "",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			expected:       nil,
		},
		{
			name:           "ConstructSdsSecretConfig without serviceAccount",
			serviceAccount: "",
			sdsUdsPath:     "spiffe://cluster.local/ns/bar/sa/foo",
			expected:       nil,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConstructSdsSecretConfig(c.serviceAccount, v3.ListenerType); !cmp.Equal(got, c.expected, protocmp.Transform()) {
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
		sdsUdsPath         string
		node               *model.Proxy
		trustDomainAliases []string
		expected           *auth.CommonTlsContext
	}{
		{
			name:       "MTLSStrict using SDS",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					SdsEnabled: true,
				},
			},
			expected: &auth.CommonTlsContext{
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
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
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
			name:       "MTLSStrict using SDS and SAN aliases",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					SdsEnabled: true,
				},
			},
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
									ApiType:             core.ApiConfigSource_GRPC,
									TransportApiVersion: core.ApiVersion_V3,
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
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
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
			name:       "MTLS using SDS with custom certs in metadata",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					SdsEnabled:         true,
					TLSServerCertChain: "serverCertChain",
					TLSServerKey:       "serverKey",
					TLSServerRootCert:  "servrRootCert",
				},
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "file-cert:serverCertChain~serverKey",
						SdsConfig: &core.ConfigSource{
							InitialFetchTimeout: features.InitialFetchTimeout,
							ResourceApiVersion:  core.ApiVersion_V3,
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType:             core.ApiConfigSource_GRPC,
									TransportApiVersion: core.ApiVersion_V3,
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
							Name: "file-root:servrRootCert",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: features.InitialFetchTimeout,
								ResourceApiVersion:  core.ApiVersion_V3,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType:             core.ApiConfigSource_GRPC,
										TransportApiVersion: core.ApiVersion_V3,
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
			name:       "ISTIO_MUTUAL SDS without node meta",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			expected: &auth.CommonTlsContext{
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
			},
		},
		{
			name:       "ISTIO_MUTUAL with custom cert paths from proxy node metadata and SDS disabled",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					TLSServerCertChain: "/custom/path/to/cert-chain.pem",
					TLSServerKey:       "/custom-key.pem",
					TLSServerRootCert:  "/custom/path/to/root.pem",
				},
			},
			expected: &auth.CommonTlsContext{
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
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			tlsContext := &auth.CommonTlsContext{}
			ApplyToCommonTLSContext(tlsContext, test.node.Metadata, test.sdsUdsPath, []string{}, v3.ListenerType, test.trustDomainAliases)

			if !cmp.Equal(tlsContext, test.expected, protocmp.Transform()) {
				t.Errorf("got(%#v), want(%#v)\n", spew.Sdump(tlsContext), spew.Sdump(test.expected))
			}
		})
	}
}

func TestApplyCustomSDSToServerCommonTLSContext(t *testing.T) {
	testCases := []struct {
		name       string
		sdsUdsPath string
		tlsOpts    *networking.ServerTLSSettings
		expected   *auth.CommonTlsContext
	}{
		{
			name:       "static certificate validation without SubjectAltNames",
			sdsUdsPath: "/tmp/sdsuds.sock",
			tlsOpts: &networking.ServerTLSSettings{
				CredentialName: "spiffe://cluster.local/ns/bar/sa/foo",
				Mode:           networking.ServerTLSSettings_SIMPLE,
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "spiffe://cluster.local/ns/bar/sa/foo",
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
													TargetUri:  "/tmp/sdsuds.sock",
													StatPrefix: SDSStatPrefix,
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
			name:       "static certificate validation with SubjectAltNames ",
			sdsUdsPath: "/tmp/sdsuds.sock",
			tlsOpts: &networking.ServerTLSSettings{
				CredentialName:  "spiffe://cluster.local/ns/bar/sa/foo",
				Mode:            networking.ServerTLSSettings_PASSTHROUGH,
				SubjectAltNames: []string{"SystemCACertificates.keychain", "SystemRootCertificates.keychain"},
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "spiffe://cluster.local/ns/bar/sa/foo",
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
													TargetUri:  "/tmp/sdsuds.sock",
													StatPrefix: SDSStatPrefix,
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
		},
		{
			name:       "ServerTLSSettings_MUTUAL mode without SubjectAltNames ",
			sdsUdsPath: "/tmp/sdsuds.sock",
			tlsOpts: &networking.ServerTLSSettings{
				CredentialName: "spiffe://cluster.local/ns/bar/sa/foo",
				Mode:           networking.ServerTLSSettings_MUTUAL,
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "spiffe://cluster.local/ns/bar/sa/foo",
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
													TargetUri:  "/tmp/sdsuds.sock",
													StatPrefix: "sdsstat",
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
						DefaultValidationContext: &auth.CertificateValidationContext{},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "spiffe://cluster.local/ns/bar/sa/foo-cacert",
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
														TargetUri:  "/tmp/sdsuds.sock",
														StatPrefix: "sdsstat",
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
		{
			name:       "ServerTLSSettings_MUTUAL mode with SubjectAltNames ",
			sdsUdsPath: "/tmp/sdsuds.sock",
			tlsOpts: &networking.ServerTLSSettings{
				CredentialName:  "spiffe://cluster.local/ns/bar/sa/foo",
				Mode:            networking.ServerTLSSettings_MUTUAL,
				SubjectAltNames: []string{"SystemCACertificates.keychain", "SystemRootCertificates.keychain"},
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: "spiffe://cluster.local/ns/bar/sa/foo",
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
													TargetUri:  "/tmp/sdsuds.sock",
													StatPrefix: "sdsstat",
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
							MatchSubjectAltNames: util.StringToExactMatch([]string{"SystemCACertificates.keychain", "SystemRootCertificates.keychain"}),
						},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "spiffe://cluster.local/ns/bar/sa/foo-cacert",
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
														TargetUri:  "/tmp/sdsuds.sock",
														StatPrefix: "sdsstat",
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

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			tlsContext := &auth.CommonTlsContext{}
			ApplyCustomSDSToServerCommonTLSContext(tlsContext, test.tlsOpts, test.sdsUdsPath, v3.ListenerType)

			if !cmp.Equal(tlsContext, test.expected, protocmp.Transform()) {
				t.Errorf("got\n%v\n want\n%v", spew.Sdump(tlsContext), spew.Sdump(test.expected))
			}
		})
	}
}
