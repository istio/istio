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

package core

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	internalupstream "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/wellknown"
)

func TestApplyUpstreamTLSSettings(t *testing.T) {
	istioMutualTLSSettings := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}
	mutualTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_MUTUAL,
		CaCertificates:    "root-cert.pem",
		ClientCertificate: "cert-chain.pem",
		PrivateKey:        "key.pem",
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	simpleTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_SIMPLE,
		CaCertificates:  "root-cert.pem",
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}

	tests := []struct {
		name                       string
		mtlsCtx                    mtlsContextType
		discoveryType              cluster.Cluster_DiscoveryType
		tls                        *networking.ClientTLSSettings
		h2                         bool
		expectTransportSocket      bool
		expectTransportSocketMatch bool

		validateTLSContext func(t *testing.T, ctx *tls.UpstreamTlsContext)
	}{
		{
			name:                       "user specified without tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        nil,
			expectTransportSocket:      false,
			expectTransportSocketMatch: false,
		},
		{
			name:                       "user specified with istio_mutual tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
		{
			name:                       "user specified with istio_mutual tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			h2:                         true,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshH2WithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshH2WithMxc, got)
				}
			},
		},
		{
			name:                       "user specified simple tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        simpleTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); got != nil {
					t.Fatalf("expected alpn list nil as not h2 or Istio_Mutual TLS Setting; got %v", got)
				}
				if got := ctx.GetSni(); got != simpleTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", simpleTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "user specified simple tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        simpleTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			h2:                         true,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNH2Only) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNH2Only, got)
				}
				if got := ctx.GetSni(); got != simpleTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", simpleTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "user specified mutual tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        mutualTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettingsWithCerts.ClientCertificate, mutualTLSSettingsWithCerts.PrivateKey)
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()[0].GetName(); certName != got {
					t.Fatalf("expected cert name %v got %v", certName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); got != nil {
					t.Fatalf("expected alpn list nil as not h2 or Istio_Mutual TLS Setting; got %v", got)
				}
				if got := ctx.GetSni(); got != mutualTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", mutualTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "user specified mutual tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        mutualTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			h2:                         true,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettingsWithCerts.ClientCertificate, mutualTLSSettingsWithCerts.PrivateKey)
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()[0].GetName(); certName != got {
					t.Fatalf("expected cert name %v got %v", certName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNH2Only) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNH2Only, got)
				}
				if got := ctx.GetSni(); got != mutualTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", mutualTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "auto detect with tls",
			mtlsCtx:                    autoDetected,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
		{
			name:                       "auto detect with tls and h2 options",
			mtlsCtx:                    autoDetected,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
			h2:                         true,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshH2WithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshH2WithMxc, got)
				}
			},
		},
	}

	proxy := &model.Proxy{
		Type:         model.SidecarProxy,
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}
	push := model.NewPushContext()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cb := NewClusterBuilder(proxy, &model.PushRequest{Push: push}, model.DisabledCache{})
			opts := &buildClusterOpts{
				mutable: newClusterWrapper(&cluster.Cluster{
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: test.discoveryType},
				}),
				mesh: push.Mesh,
			}
			if test.h2 {
				setH2Options(opts.mutable)
			}
			cb.applyUpstreamTLSSettings(opts, test.tls, test.mtlsCtx)

			if test.expectTransportSocket && opts.mutable.cluster.TransportSocket == nil ||
				!test.expectTransportSocket && opts.mutable.cluster.TransportSocket != nil {
				t.Errorf("Expected TransportSocket %v", test.expectTransportSocket)
			}
			if test.expectTransportSocketMatch && opts.mutable.cluster.TransportSocketMatches == nil ||
				!test.expectTransportSocketMatch && opts.mutable.cluster.TransportSocketMatches != nil {
				t.Errorf("Expected TransportSocketMatch %v", test.expectTransportSocketMatch)
			}

			if test.validateTLSContext != nil {
				ctx := &tls.UpstreamTlsContext{}
				if test.expectTransportSocket {
					if err := opts.mutable.cluster.TransportSocket.GetTypedConfig().UnmarshalTo(ctx); err != nil {
						t.Fatal(err)
					}
				} else if test.expectTransportSocketMatch {
					if err := opts.mutable.cluster.TransportSocketMatches[0].TransportSocket.GetTypedConfig().UnmarshalTo(ctx); err != nil {
						t.Fatal(err)
					}
				}
				test.validateTLSContext(t, ctx)
			}
		})
	}
}

func TestApplyUpstreamTLSSettingsHBONE(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	istioMutualTLSSettings := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}
	simpleTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_SIMPLE,
		CaCertificates:  "root-cert.pem",
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}
	type transportSocket struct {
		Name  string
		Inner string
	}
	type transportSocketMatch struct {
		Name   string
		Socket transportSocket
	}
	waypoint := &model.Proxy{
		Type:         model.Waypoint,
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}
	sidecar := &model.Proxy{
		Type:         model.SidecarProxy,
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}

	const internalUpstream = "envoy.transport_sockets.internal_upstream"
	tests := []struct {
		name    string
		mtlsCtx mtlsContextType
		tls     *networking.ClientTLSSettings
		proxy   *model.Proxy
		matches []transportSocketMatch
		socket  transportSocket

		validateTLSContext func(t *testing.T, ctx *tls.UpstreamTlsContext)
	}{
		{
			name:    "waypoint: auto mTLS and HBONE",
			mtlsCtx: autoDetected,
			proxy:   waypoint,
			tls:     istioMutualTLSSettings,
			matches: []transportSocketMatch{
				{
					Name:   "hbone",
					Socket: transportSocket{Name: internalUpstream, Inner: wellknown.TransportSocketRawBuffer},
				},
				{
					Name:   "tlsMode-istio",
					Socket: transportSocket{Name: wellknown.TransportSocketTLS},
				},
				{
					Name:   "tlsMode-disabled",
					Socket: transportSocket{Name: wellknown.TransportSocketRawBuffer},
				},
			},
		},
		{
			name:    "sidecar: auto mTLS and HBONE",
			mtlsCtx: autoDetected,
			proxy:   sidecar,
			tls:     istioMutualTLSSettings,
			matches: []transportSocketMatch{
				{
					Name:   "hbone",
					Socket: transportSocket{Name: internalUpstream, Inner: wellknown.TransportSocketRawBuffer},
				},
				{
					Name:   "tlsMode-istio",
					Socket: transportSocket{Name: wellknown.TransportSocketTLS},
				},
				{
					Name:   "tlsMode-disabled",
					Socket: transportSocket{Name: wellknown.TransportSocketRawBuffer},
				},
			},
		},
		{
			name:  "sidecar: explicit TLS and HBONE",
			proxy: sidecar,
			tls:   simpleTLSSettingsWithCerts,
			matches: []transportSocketMatch{
				{
					// HBONE over TLS
					Name:   "hbone",
					Socket: transportSocket{Name: internalUpstream, Inner: wellknown.TransportSocketTLS},
				},
				{
					// Just TLS
					Name:   "user",
					Socket: transportSocket{Name: wellknown.TransportSocketTLS},
				},
			},
		},
	}

	push := model.NewPushContext()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewClusterBuilder(tt.proxy, &model.PushRequest{Push: push}, model.DisabledCache{})
			cb.sendHbone = true
			opts := &buildClusterOpts{
				mutable: newClusterWrapper(&cluster.Cluster{
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				}),
				mesh: push.Mesh,
			}
			cb.applyUpstreamTLSSettings(opts, tt.tls, tt.mtlsCtx)

			translateSock := func(e *core.TransportSocket) transportSocket {
				inner := ""
				if e.Name == internalUpstream {
					us := xdstest.UnmarshalAny[internalupstream.InternalUpstreamTransport](t, e.GetTypedConfig())
					inner = (us).TransportSocket.Name
				}
				return transportSocket{Name: e.Name, Inner: inner}
			}

			if len(tt.matches) > 0 {
				gotMatches := slices.Map(opts.mutable.cluster.TransportSocketMatches, func(e *cluster.Cluster_TransportSocketMatch) transportSocketMatch {
					return transportSocketMatch{
						Name:   e.Name,
						Socket: translateSock(e.TransportSocket),
					}
				})
				assert.Equal(t, tt.matches, gotMatches)
			} else {
				assert.Equal(t, 0, len(opts.mutable.cluster.TransportSocketMatches), "expected no matches")
			}
			if tt.socket != (transportSocket{}) {
				assert.Equal(t, tt.socket, translateSock(opts.mutable.cluster.TransportSocket))
			} else {
				assert.Equal(t, true, opts.mutable.cluster.TransportSocket == nil, "expected no transport socket")
			}
			t.Log(xdstest.Dump(t, opts.mutable.cluster.TransportSocket))
			t.Log(xdstest.DumpList(t, opts.mutable.cluster.TransportSocketMatches))
		})
	}
}

type expectedResult struct {
	tlsContext *tls.UpstreamTlsContext
	err        error
}

// TestBuildUpstreamClusterTLSContext tests the buildUpstreamClusterTLSContext function
func TestBuildUpstreamClusterTLSContext(t *testing.T) {
	systemRoot := &tls.CommonTlsContext_CombinedValidationContext{
		CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
			DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
			ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
				Name: "file-root:system",
				SdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType:                   core.ApiConfigSource_GRPC,
							SetNodeOnFirstMessageOnly: true,
							TransportApiVersion:       core.ApiVersion_V3,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
										EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
									},
								},
							},
						},
					},
					ResourceApiVersion: core.ApiVersion_V3,
				},
			},
		},
	}
	clientCert := "/path/to/cert"
	rootCert := "path/to/cacert"
	clientKey := "/path/to/key"

	credentialName := "some-fake-credential"

	testCases := []struct {
		name   string
		opts   *buildClusterOpts
		tls    *networking.ClientTLSSettings
		h2     bool
		router bool
		result expectedResult
	}{
		{
			name: "tls mode disabled",
			opts: &buildClusterOpts{
				mutable: newClusterWrapper(&cluster.Cluster{
					Name: "test-cluster",
				}),
			},
			tls: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_DISABLE,
			},
			result: expectedResult{nil, nil},
		},
		{
			name: "tls mode ISTIO_MUTUAL",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
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
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ResourceApiVersion:  core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
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
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ResourceApiVersion:  core.ApiVersion_V3,
									},
								},
							},
						},
						AlpnProtocols: util.ALPNInMeshWithMxc,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode ISTIO_MUTUAL and H2",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			h2: true,
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
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
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ResourceApiVersion:  core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
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
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ResourceApiVersion:  core.ApiVersion_V3,
									},
								},
							},
						},
						AlpnProtocols: util.ALPNInMeshH2WithMxc,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with no certs specified in tls",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: systemRoot,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with no certs specified in tls insecure",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:               networking.ClientTLSSettings_SIMPLE,
				SubjectAltNames:    []string{"SAN"},
				Sni:                "some-sni.com",
				InsecureSkipVerify: wrappers.Bool(true),
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_ValidationContext{},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with AutoSni enabled and no sni specified in tls",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				SubjectAltNames: []string{"SAN"},
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: systemRoot,
					},
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with AutoSni enabled and sni specified in tls",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: systemRoot,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with VerifyCert and AutoSni enabled with SubjectAltNames set",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: systemRoot,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with VerifyCert and AutoSni enabled without SubjectAltNames set",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_SIMPLE,
				Sni:  "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: "file-root:system",
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with certs specified in tls",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CaCertificates:  rootCert,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with certs specified in tls, with crl",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CaCertificates:  rootCert,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
				CaCrl:           "path/to/crl",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									Crl: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "path/to/crl",
										},
									},
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with certs specified in tls with h2",
			opts: &buildClusterOpts{
				mutable: newH2TestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CaCertificates:  rootCert,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			h2: true,
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
						AlpnProtocols: util.ALPNH2Only,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with certs specified in tls",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CaCertificates:  rootCert,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with SANs specified in service entries",
			opts: &buildClusterOpts{
				mutable:         newTestCluster(),
				serviceAccounts: []string{"se-san.com"},
				serviceRegistry: provider.External,
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_SIMPLE,
				CaCertificates: rootCert,
				Sni:            "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"se-san.com"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with no client certificate",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "",
				PrivateKey:        "some-fake-key",
			},
			result: expectedResult{
				nil,
				fmt.Errorf("client cert must be provided"),
			},
		},
		{
			name: "tls mode MUTUAL, with no client key",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "some-fake-cert",
				PrivateKey:        "",
			},
			result: expectedResult{
				nil,
				fmt.Errorf("client key must be provided"),
			},
		},
		{
			name: "tls mode MUTUAL, with node metadata sdsEnabled true no root CA specified",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: clientCert,
				PrivateKey:        clientKey,
				SubjectAltNames:   []string{"SAN"},
				Sni:               "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name: fmt.Sprintf("file-cert:%s~%s", clientCert, clientKey),
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									ResourceApiVersion: core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: systemRoot,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with node metadata sdsEnabled true",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: clientCert,
				PrivateKey:        clientKey,
				CaCertificates:    rootCert,
				SubjectAltNames:   []string{"SAN"},
				Sni:               "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name: fmt.Sprintf("file-cert:%s~%s", clientCert, clientKey),
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									ResourceApiVersion: core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with node metadata sdsEnabled true, with crl",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: clientCert,
				PrivateKey:        clientKey,
				CaCertificates:    rootCert,
				CaCrl:             "path/to/crl",
				SubjectAltNames:   []string{"SAN"},
				Sni:               "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name: fmt.Sprintf("file-cert:%s~%s", clientCert, clientKey),
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									ResourceApiVersion: core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									Crl: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "path/to/crl",
										},
									},
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
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
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with CredentialName specified",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			router: true,
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with CredentialName specified with h2 and no SAN",
			opts: &buildClusterOpts{
				mutable: newH2TestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_SIMPLE,
				CredentialName: credentialName,
				Sni:            "some-sni.com",
			},
			h2:     true,
			router: true,
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
						AlpnProtocols: util.ALPNH2Only,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with CredentialName specified",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_MUTUAL,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			router: true,
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name:      "kubernetes://" + credentialName,
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with CredentialName specified with h2 and no SAN",
			opts: &buildClusterOpts{
				mutable: newH2TestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_MUTUAL,
				CredentialName: credentialName,
				Sni:            "some-sni.com",
			},
			h2:     true,
			router: true,
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name:      "kubernetes://" + credentialName,
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
						AlpnProtocols: util.ALPNH2Only,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, credentialName is set with proxy type Sidecar",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_MUTUAL,
				CredentialName: "fake-cred",
			},
			result: expectedResult{
				nil,
				nil,
			},
		},
		{
			name: "tls mode SIMPLE, credentialName is set with proxy type Sidecar",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_SIMPLE,
				CredentialName: "fake-cred",
			},
			result: expectedResult{
				nil,
				nil,
			},
		},
		{
			name: "tls mode SIMPLE, CredentialName is set with proxy type Sidecar and destinationRule has workload Selector",
			opts: &buildClusterOpts{
				mutable:          newTestCluster(),
				isDrWithSelector: true,
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with EcdhCurves specified in Mesh Config",
			opts: &buildClusterOpts{
				mutable:          newTestCluster(),
				isDrWithSelector: true,
				mesh: &meshconfig.MeshConfig{
					TlsDefaults: &meshconfig.MeshConfig_TLSConfig{
						EcdhCurves: []string{"P-256"},
					},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
							EcdhCurves:                []string{"P-256"},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with EcdhCurves specified in Mesh Config",
			opts: &buildClusterOpts{
				mutable:          newTestCluster(),
				isDrWithSelector: true,
				mesh: &meshconfig.MeshConfig{
					TlsDefaults: &meshconfig.MeshConfig_TLSConfig{
						EcdhCurves: []string{"P-256", "P-384"},
					},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_MUTUAL,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
							EcdhCurves:                []string{"P-256", "P-384"},
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name:      "kubernetes://" + credentialName,
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		// ecdh curves from MeshConfig should be ignored for ISTIO_MUTUAL mode
		{
			name: "tls mode ISTIO_MUTUAL with EcdhCurves specified in Mesh Config",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
				mesh: &meshconfig.MeshConfig{
					TlsDefaults: &meshconfig.MeshConfig_TLSConfig{
						EcdhCurves: []string{"P-256", "P-384"},
					},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
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
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									InitialFetchTimeout: durationpb.New(time.Second * 0),
									ResourceApiVersion:  core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
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
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										InitialFetchTimeout: durationpb.New(time.Second * 0),
										ResourceApiVersion:  core.ApiVersion_V3,
									},
								},
							},
						},
						AlpnProtocols: util.ALPNInMeshWithMxc,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, CredentialName is set with proxy type Sidecar and destinationRule has workload Selector",
			opts: &buildClusterOpts{
				mutable:          newTestCluster(),
				isDrWithSelector: true,
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_MUTUAL,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsParams: &tls.TlsParameters{
							// if not specified, envoy use TLSv1_2 as default for client.
							TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
						},
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name:      "kubernetes://" + credentialName,
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var proxy *model.Proxy
			if tc.router {
				proxy = newGatewayProxy()
			} else {
				proxy = newSidecarProxy()
			}
			cb := NewClusterBuilder(proxy, nil, model.DisabledCache{})
			if tc.h2 {
				setH2Options(tc.opts.mutable)
			}
			ret, err := cb.buildUpstreamClusterTLSContext(tc.opts, tc.tls)
			if err != nil && tc.result.err == nil || err == nil && tc.result.err != nil {
				t.Errorf("expecting:\n err=%v but got err=%v", tc.result.err, err)
			} else if diff := cmp.Diff(tc.result.tlsContext, ret, protocmp.Transform()); diff != "" {
				t.Errorf("got diff: `%v", diff)
			}
			if tc.result.tlsContext != nil {
				if len(tc.tls.Sni) == 0 {
					assert.Equal(t, tc.opts.mutable.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSni, true)
				}
				if len(tc.tls.Sni) == 0 && len(tc.tls.SubjectAltNames) == 0 {
					assert.Equal(t, tc.opts.mutable.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSanValidation, true)
				}
			}
		})
	}
}

func TestBuildAutoMtlsSettings(t *testing.T) {
	tlsSettings := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}
	tests := []struct {
		name              string
		tls               *networking.ClientTLSSettings
		sans              []string
		sni               string
		proxy             *model.Proxy
		autoMTLSEnabled   bool
		meshExternal      bool
		serviceMTLSMode   model.MutualTLSMode
		want              *networking.ClientTLSSettings
		extraTrustDomains []string
		wantCtxType       mtlsContextType
	}{
		{
			name:            "Destination rule TLS sni and SAN override",
			tls:             tlsSettings,
			sans:            []string{"spiffe://foo/serviceaccount/1"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			serviceMTLSMode: model.MTLSUnknown,
			want:            tlsSettings,
			wantCtxType:     userSupplied,
		},
		{
			name: "Metadata cert path override ISTIO_MUTUAL",
			tls:  tlsSettings,
			sans: []string{"custom.foo.com"},
			sni:  "custom.foo.com",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{
				TLSClientCertChain: "/custom/chain.pem",
				TLSClientKey:       "/custom/key.pem",
				TLSClientRootCert:  "/custom/root.pem",
			}},
			serviceMTLSMode: model.MTLSUnknown,
			want: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				PrivateKey:        "/custom/key.pem",
				ClientCertificate: "/custom/chain.pem",
				CaCertificates:    "/custom/root.pem",
				SubjectAltNames:   []string{"custom.foo.com"},
				Sni:               "custom.foo.com",
			},
			wantCtxType: userSupplied,
		},
		{
			name:            "Auto fill nil settings when mTLS nil for internal service in strict mode",
			sans:            []string{"spiffe://foo/serviceaccount/1"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSStrict,
			want: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"spiffe://foo/serviceaccount/1"},
				Sni:             "foo.com",
			},
			wantCtxType: autoDetected,
		},
		{
			name:            "Auto fill nil settings when mTLS nil for internal service in permissive mode",
			sans:            []string{"spiffe://foo/serviceaccount/1"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSPermissive,
			want: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"spiffe://foo/serviceaccount/1"},
				Sni:             "foo.com",
			},
			wantCtxType: autoDetected,
		},
		{
			name:            "Auto fill nil settings when mTLS nil for internal service in plaintext mode",
			sans:            []string{"spiffe://foo/serviceaccount/1"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSDisable,
			wantCtxType:     userSupplied,
		},
		{
			name:            "Auto fill nil settings when mTLS nil for internal service in unknown mode",
			sans:            []string{"spiffe://foo/serviceaccount/1"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSUnknown,
			wantCtxType:     userSupplied,
		},
		{
			name:            "Do not auto fill nil settings for external",
			sans:            []string{"spiffe://foo/serviceaccount/1"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			meshExternal:    true,
			serviceMTLSMode: model.MTLSUnknown,
			wantCtxType:     userSupplied,
		},
		{
			name:            "Do not auto fill nil settings if server mTLS is disabled",
			sans:            []string{"spiffe://foo/serviceaccount/1"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			serviceMTLSMode: model.MTLSDisable,
			wantCtxType:     userSupplied,
		},
		{
			name: "TLS nil auto build tls with metadata cert path",
			sans: []string{"spiffe://foo/serviceaccount/1"},
			sni:  "foo.com",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{
				TLSClientCertChain: "/custom/chain.pem",
				TLSClientKey:       "/custom/key.pem",
				TLSClientRootCert:  "/custom/root.pem",
			}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSPermissive,
			want: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "/custom/chain.pem",
				PrivateKey:        "/custom/key.pem",
				CaCertificates:    "/custom/root.pem",
				SubjectAltNames:   []string{"spiffe://foo/serviceaccount/1"},
				Sni:               "foo.com",
			},
			wantCtxType: autoDetected,
		},
		{
			name: "Simple TLS",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				PrivateKey:        "/custom/key.pem",
				ClientCertificate: "/custom/chain.pem",
				CaCertificates:    "/custom/root.pem",
			},
			sans: []string{"custom.foo.com"},
			sni:  "custom.foo.com",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{
				TLSClientCertChain: "/custom/meta/chain.pem",
				TLSClientKey:       "/custom/meta/key.pem",
				TLSClientRootCert:  "/custom/meta/root.pem",
			}},
			serviceMTLSMode: model.MTLSUnknown,
			want: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				PrivateKey:        "/custom/key.pem",
				ClientCertificate: "/custom/chain.pem",
				CaCertificates:    "/custom/root.pem",
			},
			wantCtxType: userSupplied,
		},
		{
			name: "Metadata certs with Mesh Exteranl",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				PrivateKey:        "/custom/external/key.pem",
				ClientCertificate: "/custom/external/chain.pem",
				CaCertificates:    "/custom/external/root.pem",
			},
			sans: []string{"custom.foo.com"},
			sni:  "custom.foo.com",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{
				TLSClientCertChain: "/custom/meta/chain.pem",
				TLSClientKey:       "/custom/meta/key.pem",
				TLSClientRootCert:  "/custom/meta/root.pem",
			}},
			meshExternal:    true,
			serviceMTLSMode: model.MTLSUnknown,
			want: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				PrivateKey:        "/custom/external/key.pem",
				ClientCertificate: "/custom/external/chain.pem",
				CaCertificates:    "/custom/external/root.pem",
			},
			wantCtxType: userSupplied,
		},
		{
			name:            "spiffe IDs with extra trust domains should be appended if there are no custom TLS settings",
			sans:            []string{"spiffe://east.local/ns/default/sa/app"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSPermissive,
			want: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{
					"spiffe://central.local/ns/default/sa/app",
					"spiffe://east.local/ns/default/sa/app",
					"spiffe://west.local/ns/default/sa/app",
				},
				Sni: "foo.com",
			},
			extraTrustDomains: []string{"west.local", "central.local"},
			wantCtxType:       autoDetected,
		},
		{
			name: "spiffe IDs with extra trust domains should be appended if custom TLS settings do not change SANs",
			tls: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				Sni:  "api.foo.com",
			},
			sans:            []string{"spiffe://east.local/ns/default/sa/app"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSPermissive,
			want: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{
					"spiffe://central.local/ns/default/sa/app",
					"spiffe://east.local/ns/default/sa/app",
					"spiffe://west.local/ns/default/sa/app",
				},
				Sni: "api.foo.com",
			},
			extraTrustDomains: []string{"west.local", "central.local"},
			wantCtxType:       userSupplied,
		},
		{
			name: "spiffe IDs with extra trust domains should not be appended if custom TLS settings change SANs",
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"spiffe://east.local/ns/default/sa/app"},
			},
			sans:            []string{"spiffe://east.local/ns/default/sa/app"},
			sni:             "foo.com",
			proxy:           &model.Proxy{Metadata: &model.NodeMetadata{}},
			autoMTLSEnabled: true,
			serviceMTLSMode: model.MTLSPermissive,
			want: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"spiffe://east.local/ns/default/sa/app"},
				Sni:             "foo.com",
			},
			extraTrustDomains: []string{"west.local", "central.local"},
			wantCtxType:       userSupplied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewClusterBuilder(tt.proxy, nil, nil)
			gotTLS, gotCtxType := cb.buildUpstreamTLSSettings(tt.tls, tt.sans, tt.sni, tt.autoMTLSEnabled, tt.meshExternal, tt.serviceMTLSMode, tt.extraTrustDomains)
			if !reflect.DeepEqual(gotTLS, tt.want) {
				t.Errorf("cluster TLS does not match expected result want %#v, got %#v", tt.want, gotTLS)
			}
			if gotCtxType != tt.wantCtxType {
				t.Errorf("cluster TLS context type does not match expected result want %#v, got %#v", tt.wantCtxType, gotCtxType)
			}
		})
	}
}
