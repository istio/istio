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
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pkg/features/pilot"
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
								InitialFetchTimeout: pilot.InitialFetchTimeout,
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
								InitialFetchTimeout: pilot.InitialFetchTimeout,
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
								InitialFetchTimeout: pilot.InitialFetchTimeout,
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
									InitialFetchTimeout: pilot.InitialFetchTimeout,
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

func TestGatewayHTTPRouteConfig(t *testing.T) {
	httpGateway := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "gateway",
			Namespace: "default",
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
		Gateways: []string{"gateway"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "example.org",
							Port: &networking.PortSelector{
								Port: &networking.PortSelector_Number{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}
	virtualService := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.VirtualService.Type,
			Name:      "virtual-service",
			Namespace: "default",
		},
		Spec: virtualServiceSpec,
	}
	virtualServiceCopy := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.VirtualService.Type,
			Name:      "virtual-service-copy",
			Namespace: "default",
		},
		Spec: virtualServiceSpec,
	}
	virtualServiceWildcard := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.VirtualService.Type,
			Name:      "virtual-service-wildcard",
			Namespace: "default",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
								Port: &networking.PortSelector{
									Port: &networking.PortSelector_Number{
										Number: 80,
									},
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
		virtualServices      []model.Config
		gateways             []model.Config
		routeName            string
		expectedVirtualHosts []string
	}{
		{
			"404 when no services",
			[]model.Config{},
			[]model.Config{httpGateway},
			"http.80",
			[]string{"blackhole:80"},
		},
		{
			"add a route for a virtual service",
			[]model.Config{virtualService},
			[]model.Config{httpGateway},
			"http.80",
			[]string{"example.org:80"},
		},
		{
			"duplicate virtual service should merge",
			[]model.Config{virtualService, virtualServiceCopy},
			[]model.Config{httpGateway},
			"http.80",
			[]string{"example.org:80"},
		},
		{
			"duplicate by wildcard should merge",
			[]model.Config{virtualService, virtualServiceWildcard},
			[]model.Config{httpGateway},
			"http.80",
			[]string{"example.org:80"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := &fakePlugin{}
			configgen := NewConfigGenerator([]plugin.Plugin{p})
			env := buildEnv(t, tt.gateways)
			for _, v := range tt.virtualServices {
				env.PushContext.AddVirtualServiceForTesting(&v)
			}
			route, err := configgen.buildGatewayHTTPRouteConfig(&env, &proxy, env.PushContext, proxyInstances, tt.routeName)
			if err != nil {
				t.Error(err)
			}
			vh := []string{}
			for _, h := range route.VirtualHosts {
				vh = append(vh, h.Name)
			}
			if !reflect.DeepEqual(tt.expectedVirtualHosts, vh) {
				t.Errorf("got unexpected virtual hosts. Expected: %v, Got: %v", tt.expectedVirtualHosts, vh)
			}
		})
	}

}

func buildEnv(t *testing.T, gateways []model.Config) model.Environment {
	serviceDiscovery := new(fakes.ServiceDiscovery)

	configStore := &fakes.IstioConfigStore{}
	configStore.GatewaysReturns(gateways)

	mesh := model.DefaultMeshConfig()
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             &mesh,
		MixerSAN:         []string{},
	}

	if err := env.PushContext.InitContext(&env); err != nil {
		t.Fatalf("failed to init push context: %v", err)
	}
	return env
}

func TestCreateGatewayHTTPFilterChainOpts(t *testing.T) {
	testCases := []struct {
		name      string
		node      *model.Proxy
		server    *networking.Server
		routeName string
		result    *filterChainOpts
	}{
		{
			name: "HTTP1.0 mode enabled",
			node: &model.Proxy{
				Metadata: map[string]string{
					model.NodeMetadataHTTP10: "1",
				},
			},
			server: &networking.Server{
				Port: &networking.Port{},
			},
			routeName: "some-route",
			result: &filterChainOpts{
				sniHosts:   nil,
				tlsContext: nil,
				httpOpts: &httpListenerOpts{
					rds:              "some-route",
					useRemoteAddress: true,
					direction:        http_conn.EGRESS,
					connectionManager: &http_conn.HttpConnectionManager{
						ForwardClientCertDetails: http_conn.SANITIZE_SET,
						SetCurrentClientCertDetails: &http_conn.HttpConnectionManager_SetCurrentClientCertDetails{
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
	}

	for _, tc := range testCases {
		cgi := NewConfigGenerator([]plugin.Plugin{})
		ret := cgi.createGatewayHTTPFilterChainOpts(tc.node, tc.server, tc.routeName)
		if !reflect.DeepEqual(tc.result, ret) {
			t.Errorf("test case %s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}
