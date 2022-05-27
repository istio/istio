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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/networking/util"
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

func TestConstructSdsSecretConfigForCredential(t *testing.T) {
	testCases := []struct {
		name       string
		secretName string
		expected   *auth.SdsSecretConfig
	}{
		{
			name:       "Credential test with resourceName",
			secretName: "spiffe://cluster.local/ns/bar/sa/foo",
			expected: &auth.SdsSecretConfig{
				Name:      credentials.ToResourceName("spiffe://cluster.local/ns/bar/sa/foo"),
				SdsConfig: SDSAdsConfig,
			},
		},
		{
			name:       "Credential test with buildin gateway secret type URI",
			secretName: "builtin://",
			expected:   defaultSDSConfig,
		},
		{
			name:       "Credential test with buildin gateway secret type URI and Suffix",
			secretName: "builtin://-cacert",
			expected:   rootSDSConfig,
		},
		{
			name:       "Credential test without secretName",
			secretName: "",
			expected:   nil,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConstructSdsSecretConfigForCredential(c.secretName); !cmp.Equal(got, c.expected, protocmp.Transform()) {
				t.Errorf("got(%#v), want(%#v)\n", got, c.expected)
			}
		})
	}
}

func TestApplyCustomSDSToClientCommonTLSContext(t *testing.T) {
	testCases := []struct {
		name       string
		tlsContext *auth.CommonTlsContext
		tlsOpts    *networking.ClientTLSSettings
		expected   *auth.CommonTlsContext
	}{
		{
			name:       "test SDSToClient without ClientTLS settings",
			tlsContext: &auth.CommonTlsContext{},
			tlsOpts: &networking.ClientTLSSettings{
				CredentialName:  "testCredential",
				SubjectAltNames: []string{"testCredential"},
			},
			expected: &auth.CommonTlsContext{
				ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"testCredential"})},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "kubernetes://testCredential" + SdsCaSuffix,
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_Ads{
									Ads: &core.AggregatedConfigSource{},
								},
								ResourceApiVersion: core.ApiVersion_V3,
							},
						},
					},
				},
			},
		},
		{
			name:       "test SDSToClient with ClientTLS settings",
			tlsContext: &auth.CommonTlsContext{},
			tlsOpts: &networking.ClientTLSSettings{
				CredentialName:  "testCredential",
				SubjectAltNames: []string{"testCredential"},
				Mode:            networking.ClientTLSSettings_MUTUAL,
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: credentials.ToResourceName("testCredential"),
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_Ads{
								Ads: &core.AggregatedConfigSource{},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"testCredential"})},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "kubernetes://testCredential" + SdsCaSuffix,
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_Ads{
									Ads: &core.AggregatedConfigSource{},
								},
								ResourceApiVersion: core.ApiVersion_V3,
							},
						},
					},
				},
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			ApplyCustomSDSToClientCommonTLSContext(c.tlsContext, c.tlsOpts)
			if !cmp.Equal(c.tlsContext, c.expected, protocmp.Transform()) {
				t.Errorf("got(%#v), want(%#v)\n", spew.Sdump(c.tlsContext), spew.Sdump(c.expected))
			}
		})
	}
}

func TestApplyCredentialSDSToServerCommonTLSContext(t *testing.T) {
	testCases := []struct {
		name       string
		tlsContext *auth.CommonTlsContext
		tlsOpts    *networking.ServerTLSSettings
		expected   *auth.CommonTlsContext
	}{
		{
			name:       "test SDSToClient without ServerTLS settings",
			tlsContext: &auth.CommonTlsContext{},
			tlsOpts: &networking.ServerTLSSettings{
				CredentialName:  "testCredential",
				SubjectAltNames: []string{"testCredential"},
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: credentials.ToResourceName("testCredential"),
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_Ads{
								Ads: &core.AggregatedConfigSource{},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_ValidationContext{
					ValidationContext: &auth.CertificateValidationContext{
						MatchSubjectAltNames: util.StringToExactMatch([]string{"testCredential"}),
					},
				},
			},
		},
		{
			name:       "test SDSToClient with ServerTLS settings",
			tlsContext: &auth.CommonTlsContext{},
			tlsOpts: &networking.ServerTLSSettings{
				CredentialName:        "testCredential",
				SubjectAltNames:       []string{"testCredential"},
				Mode:                  networking.ServerTLSSettings_MUTUAL,
				VerifyCertificateSpki: []string{},
				VerifyCertificateHash: []string{},
			},
			expected: &auth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
					{
						Name: credentials.ToResourceName("testCredential"),
						SdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_Ads{
								Ads: &core.AggregatedConfigSource{},
							},
							ResourceApiVersion: core.ApiVersion_V3,
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &auth.CertificateValidationContext{
							MatchSubjectAltNames:  util.StringToExactMatch([]string{"testCredential"}),
							VerifyCertificateSpki: []string{},
							VerifyCertificateHash: []string{},
						},
						ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
							Name: "kubernetes://testCredential" + SdsCaSuffix,
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_Ads{
									Ads: &core.AggregatedConfigSource{},
								},
								ResourceApiVersion: core.ApiVersion_V3,
							},
						},
					},
				},
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			ApplyCredentialSDSToServerCommonTLSContext(c.tlsContext, c.tlsOpts)
			if !cmp.Equal(c.tlsContext, c.expected, protocmp.Transform()) {
				t.Errorf("got(%#v), want(%#v)\n", spew.Sdump(c.tlsContext), spew.Sdump(c.expected))
			}
		})
	}
}
