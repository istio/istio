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

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/listenertest"
	"istio.io/istio/pilot/pkg/networking/plugin/authz"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

func TestVirtualListenerBuilder(t *testing.T) {
	cg := NewConfigGenTest(t, TestOptions{Services: testServices})
	proxy := cg.SetupProxy(nil)

	vo := xdstest.ExtractListener(model.VirtualOutboundListenerName, cg.Listeners(proxy))
	if vo == nil {
		t.Fatal("didn't find virtual outbound listener")
	}
}

var (
	testServices = []*model.Service{
		buildService("test.com", wildcardIPv4, protocol.HTTP, tnow),
		buildService("test.com", wildcardIPv6, protocol.HTTP, tnow),
	}
	testServicesWithQUIC = []*model.Service{
		buildService("test.com", wildcardIPv4, protocol.HTTP, tnow),
		buildService("test.com", wildcardIPv6, protocol.HTTP, tnow),
		buildService("quick.com", wildcardIPv4, protocol.UDP, tnow),
		buildService("quick.com", wildcardIPv6, protocol.UDP, tnow),
	}
)

func buildListeners(t *testing.T, o TestOptions, p *model.Proxy) []*listener.Listener {
	cg := NewConfigGenTest(t, o)
	// Hack up some instances for each Service
	for _, s := range o.Services {
		i := &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"1.1.1.1"},
				EndpointPort: 8080, // service port is 80, target port is 8080
			},
			ServicePort: s.Ports[0],
		}
		cg.MemRegistry.AddInstance(i)
	}
	l := cg.Listeners(cg.SetupProxy(p))
	xdstest.ValidateListeners(t, l)
	return l
}

func buildListenersWithMulAddrsEP(t *testing.T, o TestOptions, p *model.Proxy) []*listener.Listener {
	cg := NewConfigGenTest(t, o)
	// Hack up some instances for each Service
	for _, s := range o.Services {
		i := &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"1.1.1.1", "2001:1::1"},
				EndpointPort: 8080, // service port is 80, target port is 8080
			},
			ServicePort: s.Ports[0],
		}
		cg.MemRegistry.AddInstance(i)
	}
	l := cg.Listeners(cg.SetupProxy(p))
	xdstest.ValidateListeners(t, l)
	return l
}

func TestVirtualInboundListenerBuilder(t *testing.T) {
	tests := []struct {
		useExactBalance bool
	}{
		{
			useExactBalance: false,
		},
		{
			useExactBalance: true,
		},
	}

	for _, tt := range tests {
		proxy := &model.Proxy{
			Metadata: &model.NodeMetadata{
				InboundListenerExactBalance:  model.StringBool(tt.useExactBalance),
				OutboundListenerExactBalance: model.StringBool(tt.useExactBalance),
			},
		}
		testCases := [][]*listener.Listener{
			buildListeners(t, TestOptions{Services: testServices}, proxy),
			// test instance with multiple addresses
			buildListenersWithMulAddrsEP(t, TestOptions{Services: testServices}, proxy),
		}
		for _, listeners := range testCases {
			if vo := xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners); vo == nil {
				t.Fatalf("expect virtual listener, found %s", listeners[0].Name)
			}
			vi := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
			if vi == nil {
				t.Fatalf("expect virtual inbound listener, found %s", listeners[0].Name)
			}

			byListenerName := map[string]int{}

			for _, fc := range vi.FilterChains {
				byListenerName[fc.Name]++
			}

			for k, v := range byListenerName {
				if k == model.VirtualInboundListenerName && v != 3 {
					t.Fatalf("expect virtual listener has 3 passthrough filter chains, found %d", v)
				}
				if k == model.VirtualInboundCatchAllHTTPFilterChainName && v != 2 {
					t.Fatalf("expect virtual listener has 2 passthrough filter chains, found %d", v)
				}
			}

			if tt.useExactBalance {
				if vi.ConnectionBalanceConfig == nil || vi.ConnectionBalanceConfig.GetExactBalance() == nil {
					t.Fatal("expected virtual listener to have connection balance config set to exact_balance")
				}
			} else {
				if vi.ConnectionBalanceConfig != nil {
					t.Fatal("expected virtual listener to not have connection balance config set")
				}
			}
		}
	}
}

func TestVirtualInboundHasPassthroughClusters(t *testing.T) {
	testCases := [][]*listener.Listener{
		buildListeners(t, TestOptions{Services: testServices}, nil),
		// test instance with multiple addresses
		buildListenersWithMulAddrsEP(t, TestOptions{Services: testServices}, nil),
	}
	for _, listeners := range testCases {
		l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
		if l == nil {
			t.Fatal("failed to find virtual inbound listener")
		}
		listenertest.VerifyListener(t, l, listenertest.ListenerTest{
			FilterChains: []listenertest.FilterChainTest{
				{Name: "virtualInbound-blackhole"},
				{Name: "virtualInbound-catchall-http", Type: listenertest.MTLSHTTP},
				{Name: "virtualInbound-catchall-http", Type: listenertest.PlainHTTP},
				{Name: "virtualInbound", Type: listenertest.MTLSTCP},
				{Name: "virtualInbound", Type: listenertest.PlainTCP},
				{Name: "virtualInbound", Type: listenertest.StandardTLS},
				{Name: "0.0.0.0_8080", Type: listenertest.MTLSHTTP},
				{Name: "0.0.0.0_8080", Type: listenertest.PlainTCP},
			},
			Filters: []string{
				wellknown.OriginalDestination,
				wellknown.TLSInspector,
				wellknown.HTTPInspector,
			},
			TotalMatch: true,
		})
	}
}

func TestSidecarInboundListenerWithOriginalSrc(t *testing.T) {
	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{InterceptionMode: model.InterceptionTproxy},
	}
	testCases := [][]*listener.Listener{
		buildListeners(t, TestOptions{Services: testServices}, proxy),
		// test instance with multiple addresses
		buildListenersWithMulAddrsEP(t, TestOptions{Services: testServices}, proxy),
	}
	for _, listeners := range testCases {
		l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
		if l == nil {
			t.Fatal("failed to find virtual inbound listener")
		}
		if _, f := xdstest.ExtractListenerFilters(l)[wellknown.OriginalSource]; !f {
			t.Fatalf("missing %v filter", wellknown.OriginalSource)
		}
	}
}

// TestSidecarInboundListenerWithQUICConnectionBalance should not set
// exact_balance for the virtualInbound listener as QUIC uses UDP
// and this works only over TCP
func TestSidecarInboundListenerWithQUICAndExactBalance(t *testing.T) {
	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{
			InboundListenerExactBalance:  true,
			OutboundListenerExactBalance: true,
		},
	}
	testCases := [][]*listener.Listener{
		buildListeners(t, TestOptions{Services: testServicesWithQUIC}, proxy),
		// test instance with multiple addresses
		buildListenersWithMulAddrsEP(t, TestOptions{Services: testServicesWithQUIC}, proxy),
	}
	for _, listeners := range testCases {
		l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
		if l == nil {
			t.Fatal("failed to find virtual inbound listener")
		}
		if l.ConnectionBalanceConfig == nil || l.ConnectionBalanceConfig.GetExactBalance() == nil {
			t.Fatal("expected listener to have exact_balance set, but was empty")
		}
	}
}

func TestListenerBuilderPatchListeners(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-outbound-listener"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-inbound-listener"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-gateway-listener"}`),
			},
		},

		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 81,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 82,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 83,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
	}
	cg := NewConfigGenTest(t, TestOptions{Configs: getEnvoyFilterConfigs(configPatches)})

	gatewayProxy := cg.SetupProxy(&model.Proxy{Type: model.Router, ConfigNamespace: "not-default"})
	sidecarProxy := cg.SetupProxy(&model.Proxy{ConfigNamespace: "not-default"})
	type fields struct {
		GatewayListeners        []*listener.Listener
		InboundListeners        []*listener.Listener
		OutboundListeners       []*listener.Listener
		HTTPProxyListener       *listener.Listener
		VirtualOutboundListener *listener.Listener
		VirtualInboundListener  *listener.Listener
	}
	tests := []struct {
		name   string
		proxy  *model.Proxy
		fields fields
		want   fields
	}{
		{
			name:  "patch add inbound and outbound listener",
			proxy: sidecarProxy,
			fields: fields{
				OutboundListeners: []*listener.Listener{
					{
						Name: "outbound-listener",
					},
				},
			},
			want: fields{
				InboundListeners: []*listener.Listener{
					{
						Name: "new-inbound-listener",
					},
				},

				OutboundListeners: []*listener.Listener{
					{
						Name: "outbound-listener",
					},
					{
						Name: "new-outbound-listener",
					},
				},
			},
		},
		{
			name:  "patch inbound and outbound listener",
			proxy: sidecarProxy,
			fields: fields{
				OutboundListeners: []*listener.Listener{
					{
						Name: "outbound-listener",
					},
					{
						Name: "remove-outbound",
						Address: &core.Address{
							Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									PortSpecifier: &core.SocketAddress_PortValue{
										PortValue: 81,
									},
								},
							},
						},
					},
				},
			},
			want: fields{
				InboundListeners: []*listener.Listener{
					{
						Name: "new-inbound-listener",
					},
				},

				OutboundListeners: []*listener.Listener{
					{
						Name: "outbound-listener",
					},
					{
						Name: "new-outbound-listener",
					},
				},
			},
		},
		{
			name:  "remove HTTP Proxy listener",
			proxy: sidecarProxy,
			fields: fields{
				HTTPProxyListener: &listener.Listener{
					Name: "127.0.0.1_81",
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								PortSpecifier: &core.SocketAddress_PortValue{
									PortValue: 81,
								},
							},
						},
					},
					FilterChains: []*listener.FilterChain{
						{
							Filters: []*listener.Filter{
								{
									Name: wellknown.HTTPConnectionManager,
								},
							},
						},
					},
				},
			},
			want: fields{
				InboundListeners: []*listener.Listener{
					{
						Name: "new-inbound-listener",
					},
				},
				OutboundListeners: []*listener.Listener{
					{
						Name: "new-outbound-listener",
					},
				},
			},
		},
		{
			name:  "patch add gateway listener",
			proxy: gatewayProxy,
			fields: fields{
				GatewayListeners: []*listener.Listener{
					{
						Name: "gateway-listener",
					},
				},
			},
			want: fields{
				GatewayListeners: []*listener.Listener{
					{
						Name: "gateway-listener",
					},
					{
						Name: "new-gateway-listener",
					},
				},
			},
		},

		{
			name:  "patch gateway listener",
			proxy: gatewayProxy,
			fields: fields{
				GatewayListeners: []*listener.Listener{
					{
						Name: "gateway-listener",
					},
					{
						Name: "remove-gateway",
						Address: &core.Address{
							Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									PortSpecifier: &core.SocketAddress_PortValue{
										PortValue: 83,
									},
								},
							},
						},
					},
				},
			},
			want: fields{
				GatewayListeners: []*listener.Listener{
					{
						Name: "gateway-listener",
					},
					{
						Name: "new-gateway-listener",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := &ListenerBuilder{
				node:                    tt.proxy,
				push:                    cg.PushContext(),
				gatewayListeners:        tt.fields.GatewayListeners,
				inboundListeners:        tt.fields.InboundListeners,
				outboundListeners:       tt.fields.OutboundListeners,
				httpProxyListener:       tt.fields.HTTPProxyListener,
				virtualOutboundListener: tt.fields.VirtualOutboundListener,
				virtualInboundListener:  tt.fields.VirtualInboundListener,
			}

			lb.patchListeners()
			got := fields{
				GatewayListeners:        lb.gatewayListeners,
				InboundListeners:        lb.inboundListeners,
				OutboundListeners:       lb.outboundListeners,
				HTTPProxyListener:       lb.httpProxyListener,
				VirtualOutboundListener: lb.virtualOutboundListener,
				VirtualInboundListener:  lb.virtualInboundListener,
			}

			assert.Equal(t, got, tt.want)
		})
	}
}

func buildPatchStruct(config string) *structpb.Struct {
	val := &structpb.Struct{}
	_ = protomarshal.UnmarshalString(config, val)
	return val
}

func getEnvoyFilterConfigs(configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch) []config.Config {
	res := []config.Config{}
	for i, cp := range configPatches {
		res = append(res, config.Config{
			Meta: config.Meta{
				Name:             fmt.Sprintf("test-envoyfilter-%d", i),
				Namespace:        "not-default",
				GroupVersionKind: gvk.EnvoyFilter,
			},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{cp},
			},
		})
	}
	return res
}

const strictMode = `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: STRICT
`

const disableMode = `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: DISABLE
`

func TestInboundListenerFilters(t *testing.T) {
	testInboundListenerFilters(t, false)
	testInboundListenerFilters(t, true)
}

func testInboundListenerFilters(t *testing.T, enableDualStack bool) {
	if enableDualStack {
		test.SetForTest(t, &features.EnableDualStack, true)
	}
	services := []*model.Service{
		buildServiceWithPort("test1.com", 80, protocol.HTTP, tnow),
		buildServiceWithPort("test2.com", 81, protocol.Unsupported, tnow),
		buildServiceWithPort("test3.com", 82, protocol.TCP, tnow),
	}
	instances := make([]*model.ServiceInstance, 0, len(services))
	for _, s := range services {
		var addrs []string
		if enableDualStack {
			addrs = []string{"1.1.1.1", "2001:1::1"}
		} else {
			addrs = []string{"1.1.1.1"}
		}
		instances = append(instances, &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				EndpointPort: uint32(s.Ports[0].Port),
				Addresses:    addrs,
			},
			ServicePort: s.Ports[0],
		})
	}
	cases := []struct {
		name   string
		config string
		http   map[int]bool
		tls    map[int]bool
	}{
		{
			name:   "permissive",
			config: "",
			http: map[int]bool{
				// Should not see HTTP inspector if we declare ports
				80: true,
				82: true,
				// But should see for passthrough or unnamed ports
				81:   false,
				1000: false,
			},
			tls: map[int]bool{
				// Permissive mode: inspector is set everywhere
				80:   false,
				82:   false,
				81:   false,
				1000: false,
			},
		},
		{
			name:   "disable",
			config: disableMode,
			http: map[int]bool{
				// Should not see HTTP inspector if we declare ports
				80: true,
				82: true,
				// But should see for passthrough or unnamed ports
				81:   false,
				1000: false,
			},
		},
		{
			name:   "strict",
			config: strictMode,
			http: map[int]bool{
				// Should not see HTTP inspector if we declare ports
				80: true,
				82: true,
				// This is 'auto', but for STRICT we always get requests over TLS so HTTP inspector is not in play
				81: true,
				// Even for passthrough, we do not need HTTP inspector because it is handled by TLS inspector
				1000: true,
			},
			tls: map[int]bool{
				// strict mode: inspector is set everywhere.
				80:   false,
				82:   false,
				81:   false,
				1000: false,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{
				Services:     services,
				Instances:    instances,
				ConfigString: tt.config,
			})
			listeners := cg.Listeners(cg.SetupProxy(nil))
			virtualInbound := xdstest.ExtractListener("virtualInbound", listeners)
			filters := xdstest.ExtractListenerFilters(virtualInbound)
			evaluateListenerFilterPredicates(t, filters[wellknown.HTTPInspector].GetFilterDisabled(), tt.http)
			if filters[wellknown.TLSInspector] == nil {
				if len(tt.tls) > 0 {
					t.Fatal("Expected tls inspector, got none")
				}
			} else {
				evaluateListenerFilterPredicates(t, filters[wellknown.TLSInspector].FilterDisabled, tt.tls)
			}
		})
	}
}

func evaluateListenerFilterPredicates(t testing.TB, predicate *listener.ListenerFilterChainMatchPredicate, expected map[int]bool) {
	t.Helper()
	for port, expect := range expected {
		got := xdstest.EvaluateListenerFilterPredicates(predicate, port)
		if got != expect {
			t.Errorf("expected port %v to have match=%v, got match=%v", port, expect, got)
		}
	}
}

func TestSidecarInboundListenerFilters(t *testing.T) {
	testSidecarInboundListenerFilters(t, false)
	testSidecarInboundListenerFilters(t, true)
}

func testSidecarInboundListenerFilters(t *testing.T, enableDualStack bool) {
	if enableDualStack {
		test.SetForTest(t, &features.EnableDualStack, true)
	}
	services := []*model.Service{buildServiceWithPort("test.com", 80, protocol.HTTPS, tnow)}

	expectIstioMTLS := func(t test.Failer, filterChain *listener.FilterChain) {
		tlsContext := &tls.DownstreamTlsContext{}
		if err := filterChain.GetTransportSocket().GetTypedConfig().UnmarshalTo(tlsContext); err != nil {
			t.Fatal(err)
		}
		commonTLSContext := tlsContext.CommonTlsContext
		if len(commonTLSContext.TlsCertificateSdsSecretConfigs) == 0 {
			t.Fatal("expected tls certificates")
		}
		if commonTLSContext.TlsCertificateSdsSecretConfigs[0].Name != "default" {
			t.Fatalf("expected certificate default, actual %s",
				commonTLSContext.TlsCertificates[0].CertificateChain.String())
		}
	}
	instances := make([]*model.ServiceInstance, 0, len(services))
	for _, s := range services {
		var addrs []string
		if enableDualStack {
			addrs = []string{"1.1.1.1", "2001:1::1"}
		} else {
			addrs = []string{"1.1.1.1"}
		}
		instances = append(instances, &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				EndpointPort: uint32(s.Ports[0].Port),
				Addresses:    addrs,
			},
			ServicePort: s.Ports[0],
		})
	}
	cases := []struct {
		name           string
		sidecarScope   *model.SidecarScope
		mtlsMode       model.MutualTLSMode
		expectedResult func(t test.Failer, filterChain *listener.FilterChain)
	}{
		{
			name: "simulate peer auth disabled on port 80",
			sidecarScope: &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.SidecarPort{Name: "https-port", Protocol: "https", Number: 80},
							Tls: &networking.ServerTLSSettings{
								Mode:              networking.ServerTLSSettings_SIMPLE,
								ServerCertificate: "cert.pem",
								PrivateKey:        "privatekey.pem",
							},
						},
					},
				},
			},
			mtlsMode: model.MTLSDisable,
			expectedResult: func(t test.Failer, filterChain *listener.FilterChain) {
				tlsContext := &tls.DownstreamTlsContext{}
				if err := filterChain.GetTransportSocket().GetTypedConfig().UnmarshalTo(tlsContext); err != nil {
					t.Fatal(err)
				}
				commonTLSContext := tlsContext.CommonTlsContext
				if len(commonTLSContext.TlsCertificateSdsSecretConfigs) == 0 {
					t.Fatal("expected tls certificates")
				}
				if commonTLSContext.TlsCertificateSdsSecretConfigs[0].Name != "file-cert:cert.pem~privatekey.pem" {
					t.Fatalf("expected certificate httpbin.pem, actual %s",
						commonTLSContext.TlsCertificates[0].CertificateChain.String())
				}
				if tlsContext.RequireClientCertificate.Value {
					t.Fatal("expected RequireClientCertificate to be false")
				}
			},
		},
		{
			name: "simulate peer auth strict",
			sidecarScope: &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.SidecarPort{Name: "https-port", Protocol: "https", Number: 80},
							Tls: &networking.ServerTLSSettings{
								Mode:              networking.ServerTLSSettings_SIMPLE,
								ServerCertificate: "cert.pem",
								PrivateKey:        "privatekey.pem",
							},
						},
					},
				},
			},
			mtlsMode:       model.MTLSStrict,
			expectedResult: expectIstioMTLS,
		},
		{
			name: "simulate peer auth permissive",
			sidecarScope: &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.SidecarPort{Name: "https-port", Protocol: "https", Number: 80},
							Tls: &networking.ServerTLSSettings{
								Mode:              networking.ServerTLSSettings_SIMPLE,
								ServerCertificate: "cert.pem",
								PrivateKey:        "privatekey.pem",
							},
						},
					},
				},
			},
			mtlsMode:       model.MTLSPermissive,
			expectedResult: expectIstioMTLS,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{
				Services:     services,
				Instances:    instances,
				ConfigString: mtlsMode(tt.mtlsMode.String()),
			})
			proxy := cg.SetupProxy(nil)
			proxy.Metadata = &model.NodeMetadata{Labels: map[string]string{"app": "foo"}}
			proxy.Labels = proxy.Metadata.Labels
			proxy.SidecarScope = tt.sidecarScope
			test.SetForTest(t, &features.EnableTLSOnSidecarIngress, true)
			listeners := cg.Listeners(proxy)
			virtualInbound := xdstest.ExtractListener("virtualInbound", listeners)
			filterChain := xdstest.ExtractFilterChain("1.1.1.1_80", virtualInbound)
			tt.expectedResult(t, filterChain)
		})
	}
}

func mtlsMode(m string) string {
	return fmt.Sprintf(`apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: %s
`, m)
}

func TestHCMInternalAddressConfig(t *testing.T) {
	cg := NewConfigGenTest(t, TestOptions{})
	sidecarProxy := cg.SetupProxy(&model.Proxy{ConfigNamespace: "not-default"})
	test.SetForTest(t, &features.EnableHCMInternalNetworks, true)
	push := cg.PushContext()
	cases := []struct {
		name           string
		networks       *meshconfig.MeshNetworks
		expectedconfig *hcm.HttpConnectionManager_InternalAddressConfig
	}{
		{
			name:           "nil networks",
			expectedconfig: nil,
		},
		{
			name: "networks populated",
			networks: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"default": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
									FromCidr: "192.168.0.0/16",
								},
							},
						},
					},
				},
			},
			expectedconfig: &hcm.HttpConnectionManager_InternalAddressConfig{
				CidrRanges: []*core.CidrRange{
					{
						AddressPrefix: "192.168.0.0",
						PrefixLen:     &wrapperspb.UInt32Value{Value: 16},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			push.Networks = tt.networks
			lb := &ListenerBuilder{
				push:               push,
				node:               sidecarProxy,
				authzCustomBuilder: &authz.Builder{},
				authzBuilder:       &authz.Builder{},
			}
			httpConnManager := lb.buildHTTPConnectionManager(&httpListenerOpts{})
			if !reflect.DeepEqual(tt.expectedconfig, httpConnManager.InternalAddressConfig) {
				t.Errorf("unexpected internal address config, expected: %v, got :%v", tt.expectedconfig, httpConnManager.InternalAddressConfig)
			}
		})
	}
}

func TestUseRemoteAddressInternalAddressConfig(t *testing.T) {
	cg := NewConfigGenTest(t, TestOptions{})
	sidecarProxy := cg.SetupProxy(&model.Proxy{ConfigNamespace: "not-default"})
	push := cg.PushContext()
	cases := []struct {
		name           string
		networks       *meshconfig.MeshNetworks
		expectedconfig *hcm.HttpConnectionManager_InternalAddressConfig
	}{
		{
			name:           "nil networks",
			expectedconfig: nil,
		},
		{
			name:           "empty networks",
			networks:       &meshconfig.MeshNetworks{},
			expectedconfig: nil,
		},
		{
			name: "networks populated",
			networks: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"default": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
									FromCidr: "192.168.0.0/16",
								},
							},
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
									FromCidr: "172.16.0.0/12",
								},
							},
						},
					},
				},
			},
			expectedconfig: &hcm.HttpConnectionManager_InternalAddressConfig{
				CidrRanges: []*core.CidrRange{
					{
						AddressPrefix: "172.16.0.0",
						PrefixLen:     &wrapperspb.UInt32Value{Value: 12},
					},
					{
						AddressPrefix: "192.168.0.0",
						PrefixLen:     &wrapperspb.UInt32Value{Value: 16},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			push.Networks = tt.networks
			lb := &ListenerBuilder{
				push:               push,
				node:               sidecarProxy,
				authzCustomBuilder: &authz.Builder{},
				authzBuilder:       &authz.Builder{},
			}
			httpConnManager := lb.buildHTTPConnectionManager(&httpListenerOpts{useRemoteAddress: true})
			if !reflect.DeepEqual(tt.expectedconfig, httpConnManager.InternalAddressConfig) {
				t.Errorf("unexpected internal address config, expected: %v, got :%v", tt.expectedconfig, httpConnManager.InternalAddressConfig)
			}
		})
	}
}

func TestAdditionalAddressesForIPv6(t *testing.T) {
	test.SetForTest(t, &features.EnableAdditionalIpv4OutboundListenerForIpv6Only, true)
	cg := NewConfigGenTest(t, TestOptions{Services: testServices})
	proxy := cg.SetupProxy(&model.Proxy{IPAddresses: []string{"1111:2222::1"}})

	listeners := buildListeners(t, TestOptions{Services: testServices}, proxy)
	vo := xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners)
	if vo == nil {
		t.Fatal("didn't find virtual outbound listener")
	}
	if vo.AdditionalAddresses == nil || len(vo.AdditionalAddresses) != 1 {
		t.Fatal("expected additional ipv4 bind addresse")
	}
}

func TestPreserveHeader(t *testing.T) {
	cg := NewConfigGenTest(t, TestOptions{
		MeshConfig: &meshconfig.MeshConfig{
			DefaultConfig: &meshconfig.ProxyConfig{
				ProxyHeaders: &meshconfig.ProxyConfig_ProxyHeaders{
					PreserveHttp1HeaderCase: wrapperspb.Bool(true),
				},
			},
		},
	})

	push := cg.PushContext()
	cases := []struct {
		name       string
		opts       *httpListenerOpts
		isExpected func(*hcm.HttpConnectionManager) error
	}{
		{
			name: "when preserve header case is enabled, http 1.0 settings should be preserved",
			opts: &httpListenerOpts{
				connectionManager: &hcm.HttpConnectionManager{
					HttpProtocolOptions: &core.Http1ProtocolOptions{
						AcceptHttp_10: true,
					},
				},
			},
			isExpected: func(httpConnManager *hcm.HttpConnectionManager) error {
				if httpConnManager.HttpProtocolOptions == nil || !httpConnManager.HttpProtocolOptions.AcceptHttp_10 {
					return fmt.Errorf("http 1.0 settings not preserved")
				}
				return nil
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sidecarProxy := cg.SetupProxy(nil)
			lb := &ListenerBuilder{
				push:               push,
				node:               sidecarProxy,
				authzCustomBuilder: &authz.Builder{},
				authzBuilder:       &authz.Builder{},
			}
			assert.NoError(t, tt.isExpected(lb.buildHTTPConnectionManager(tt.opts)))
		})
	}
}
