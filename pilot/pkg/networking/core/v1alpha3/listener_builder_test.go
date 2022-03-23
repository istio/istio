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
	"fmt"
	"reflect"
	"strings"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
)

type LdsEnv struct {
	configgen *ConfigGeneratorImpl
}

func getDefaultLdsEnv() *LdsEnv {
	listenerEnv := LdsEnv{
		configgen: NewConfigGenerator([]plugin.Plugin{&fakePlugin{}}, &model.DisabledCache{}),
	}
	return &listenerEnv
}

func getDefaultProxy() *model.Proxy {
	proxy := model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			IstioVersion: "1.4",
			Namespace:    "not-default",
		},
		IstioVersion:    model.ParseIstioVersion("1.4"),
		ConfigNamespace: "not-default",
	}

	proxy.DiscoverIPVersions()
	return &proxy
}

func setNilSidecarOnProxy(proxy *model.Proxy, pushContext *model.PushContext) {
	proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(pushContext, "not-default")
}

func TestVirtualListenerBuilder(t *testing.T) {
	// prepare
	t.Helper()
	ldsEnv := getDefaultLdsEnv()
	service := buildService("test.com", wildcardIP, protocol.HTTP, tnow)
	services := []*model.Service{service}

	env := buildListenerEnv(services)
	if err := env.PushContext.InitContext(env, nil, nil); err != nil {
		t.Fatalf("init push context error: %s", err.Error())
	}
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				EndpointPort: 8080,
			},
			ServicePort: s.Ports[0],
		}
	}
	proxy := getDefaultProxy()
	proxy.ServiceInstances = instances
	setNilSidecarOnProxy(proxy, env.PushContext)

	builder := NewListenerBuilder(proxy, env.PushContext)
	listeners := builder.
		buildVirtualOutboundListener(ldsEnv.configgen).
		getListeners()

	// virtual outbound listener
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	if !strings.HasPrefix(listeners[0].Name, model.VirtualOutboundListenerName) {
		t.Fatalf("expect virtual listener, found %s", listeners[0].Name)
	} else {
		t.Logf("found virtual listener: %s", listeners[0].Name)
	}
}

func setInboundCaptureAllOnThisNode(proxy *model.Proxy, mode model.TrafficInterceptionMode) {
	proxy.Metadata.InterceptionMode = mode
}

var (
	testServices         = []*model.Service{buildService("test.com", wildcardIP, protocol.HTTP, tnow)}
	testServicesWithQUIC = []*model.Service{
		buildService("test.com", wildcardIP, protocol.HTTP, tnow),
		buildService("quick.com", wildcardIP, protocol.UDP, tnow),
	}
)

func prepareListeners(t *testing.T, services []*model.Service, mode model.TrafficInterceptionMode, exactBalance bool) []*listener.Listener {
	// prepare
	ldsEnv := getDefaultLdsEnv()

	env := buildListenerEnv(services)
	if err := env.PushContext.InitContext(env, nil, nil); err != nil {
		t.Fatalf("init push context error: %s", err.Error())
	}
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				EndpointPort: 8080,
			},
			ServicePort: s.Ports[0],
		}
	}

	proxy := getDefaultProxy()
	proxy.ServiceInstances = instances
	proxy.Metadata.InboundListenerExactBalance = model.StringBool(exactBalance)
	proxy.Metadata.OutboundListenerExactBalance = model.StringBool(exactBalance)
	setInboundCaptureAllOnThisNode(proxy, mode)
	setNilSidecarOnProxy(proxy, env.PushContext)

	builder := NewListenerBuilder(proxy, env.PushContext)
	return builder.buildSidecarInboundListeners(ldsEnv.configgen).
		buildHTTPProxyListener(ldsEnv.configgen).
		buildVirtualOutboundListener(ldsEnv.configgen).
		buildVirtualInboundListener(ldsEnv.configgen).
		getListeners()
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
		// prepare
		t.Helper()
		listeners := prepareListeners(t, testServices, model.InterceptionRedirect, tt.useExactBalance)
		// virtual inbound and outbound listener
		if len(listeners) != 2 {
			t.Fatalf("expected %d listeners, found %d", 2, len(listeners))
		}

		if !strings.HasPrefix(listeners[0].Name, model.VirtualOutboundListenerName) {
			t.Fatalf("expect virtual listener, found %s", listeners[0].Name)
		} else {
			t.Logf("found virtual listener: %s", listeners[0].Name)
		}

		if !strings.HasPrefix(listeners[1].Name, model.VirtualInboundListenerName) {
			t.Fatalf("expect virtual listener, found %s", listeners[1].Name)
		} else {
			t.Logf("found virtual inbound listener: %s", listeners[1].Name)
		}

		l := listeners[1]

		byListenerName := map[string]int{}

		for _, fc := range l.FilterChains {
			byListenerName[fc.Name]++
		}

		for k, v := range byListenerName {
			if k == model.VirtualInboundListenerName && v != 3 {
				t.Fatalf("expect virtual listener has 3 passthrough filter chains, found %d", v)
			}
			if k == model.VirtualInboundCatchAllHTTPFilterChainName && v != 2 {
				t.Fatalf("expect virtual listener has 2 passthrough filter chains, found %d", v)
			}
			if k == listeners[0].Name && v != len(listeners[0].FilterChains) {
				t.Fatalf("expect virtual listener has %d filter chains from listener %s, found %d", len(listeners[0].FilterChains), l.Name, v)
			}
		}

		if tt.useExactBalance {
			if l.ConnectionBalanceConfig == nil || l.ConnectionBalanceConfig.GetExactBalance() == nil {
				t.Fatal("expected virtual listener to have connection balance config set to exact_balance")
			}
		} else {
			if l.ConnectionBalanceConfig != nil {
				t.Fatal("expected virtual listener to not have connection balance config set")
			}
		}
	}
}

func TestVirtualInboundHasPassthroughClusters(t *testing.T) {
	// prepare
	t.Helper()
	listeners := prepareListeners(t, testServices, model.InterceptionRedirect, true)
	// virtual inbound and outbound listener
	if len(listeners) != 2 {
		t.Fatalf("expect %d listeners, found %d", 2, len(listeners))
	}

	l := listeners[1]
	sawFakePluginFilter := false
	sawIpv4PassthroughCluster := 0
	sawIpv6PassthroughCluster := false
	sawIpv4PassthroughFilterChainMatchTLSFromFakePlugin := false
	for _, fc := range l.FilterChains {
		if fc.TransportSocket != nil && fc.FilterChainMatch.TransportProtocol != "tls" {
			t.Fatalf("expect passthrough filter chain sets transport protocol to tls if transport socket is set")
		}

		if f := getTCPFilter(fc); f != nil && fc.Name == model.VirtualInboundListenerName {
			if fc.Filters[0].Name == fakePluginTCPFilter {
				sawFakePluginFilter = true
			}
			if ipLen := len(fc.FilterChainMatch.PrefixRanges); ipLen != 1 {
				t.Fatalf("expect passthrough filter chain has 1 ip address, found %d", ipLen)
			}

			if fc.TransportSocket != nil {
				sawIpv4PassthroughFilterChainMatchTLSFromFakePlugin = true
			}
			if fc.FilterChainMatch.PrefixRanges[0].AddressPrefix == util.ConvertAddressToCidr("0.0.0.0/0").AddressPrefix &&
				fc.FilterChainMatch.PrefixRanges[0].PrefixLen.Value == 0 {
				if sawIpv4PassthroughCluster == 3 {
					t.Fatalf("duplicated ipv4 passthrough cluster filter chain in listener %v", l)
				}
				sawIpv4PassthroughCluster++
			} else if fc.FilterChainMatch.PrefixRanges[0].AddressPrefix == util.ConvertAddressToCidr("::0/0").AddressPrefix &&
				fc.FilterChainMatch.PrefixRanges[0].PrefixLen.Value == 0 {
				if sawIpv6PassthroughCluster {
					t.Fatalf("duplicated ipv6 passthrough cluster filter chain in listener %v", l)
				}
				sawIpv6PassthroughCluster = true
			}
		}

		if f := getHTTPFilter(fc); f != nil && fc.Name == model.VirtualInboundCatchAllHTTPFilterChainName {
			if fc.TransportSocket != nil && !reflect.DeepEqual(fc.FilterChainMatch.ApplicationProtocols, mtlsHTTPALPNs) {
				t.Fatalf("expect %v application protocols, found %v", mtlsHTTPALPNs, fc.FilterChainMatch.ApplicationProtocols)
			}

			if fc.TransportSocket == nil && !reflect.DeepEqual(fc.FilterChainMatch.ApplicationProtocols, plaintextHTTPALPNs) {
				t.Fatalf("expect %v application protocols, found %v", plaintextHTTPALPNs, fc.FilterChainMatch.ApplicationProtocols)
			}

			if !strings.Contains(fc.Filters[1].GetTypedConfig().String(), fakePluginHTTPFilter) {
				t.Errorf("failed to find the fake plugin HTTP filter: %v", fc.Filters[1].GetTypedConfig().String())
			}
		}
	}

	if sawIpv4PassthroughCluster != 3 {
		t.Fatalf("fail to find the ipv4 passthrough filter chain in listener, got %v: %v", sawIpv4PassthroughCluster, xdstest.Dump(t, l))
	}

	if !sawFakePluginFilter {
		t.Fatalf("fail to find the fake plugin TCP filter in listener %v", l)
	}

	if !sawIpv4PassthroughFilterChainMatchTLSFromFakePlugin {
		t.Fatalf("fail to find the fake plugin filter chain match with TLS in listener %v", l)
	}

	if len(l.ListenerFilters) != 3 {
		t.Fatalf("expected %d listener filters, found %d", 3, len(l.ListenerFilters))
	}

	if l.ListenerFilters[0].Name != wellknown.OriginalDestination ||
		l.ListenerFilters[1].Name != wellknown.TlsInspector ||
		l.ListenerFilters[2].Name != wellknown.HttpInspector {
		t.Fatalf("expect listener filters [%q, %q, %q], found [%q, %q, %q]",
			wellknown.OriginalDestination, wellknown.TlsInspector, wellknown.HttpInspector,
			l.ListenerFilters[0].Name, l.ListenerFilters[1].Name, l.ListenerFilters[2].Name)
	}
}

func TestSidecarInboundListenerWithOriginalSrc(t *testing.T) {
	// prepare
	t.Helper()
	listeners := prepareListeners(t, testServices, model.InterceptionTproxy, false)

	if len(listeners) != 2 {
		t.Fatalf("expected %d listeners, found %d", 2, len(listeners))
	}
	l := listeners[1]
	originalSrcFilterFound := false
	for _, lf := range l.ListenerFilters {
		if lf.Name == wellknown.OriginalSource {
			originalSrcFilterFound = true
			break
		}
	}
	if !originalSrcFilterFound {
		t.Fatalf("listener filter %s expected", wellknown.OriginalSource)
	}
}

// TestSidecarInboundListenerWithQUICConnectionBalance should not set
// exact_balance for the virtualInbound listener as QUIC uses UDP
// and this works only over TCP
func TestSidecarInboundListenerWithQUICAndExactBalance(t *testing.T) {
	// prepare
	t.Helper()
	listeners := prepareListeners(t, testServicesWithQUIC, model.InterceptionTproxy, true)

	if len(listeners) != 2 {
		t.Fatalf("expected %d listeners, found %d", 2, len(listeners))
	}
	l := listeners[1]
	if l.ConnectionBalanceConfig == nil || l.ConnectionBalanceConfig.GetExactBalance() == nil {
		t.Fatal("expected listener to have exact_balance set, but was empty")
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
		gatewayListeners        []*listener.Listener
		inboundListeners        []*listener.Listener
		outboundListeners       []*listener.Listener
		httpProxyListener       *listener.Listener
		virtualOutboundListener *listener.Listener
		virtualInboundListener  *listener.Listener
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
				outboundListeners: []*listener.Listener{
					{
						Name: "outbound-listener",
					},
				},
			},
			want: fields{
				inboundListeners: []*listener.Listener{
					{
						Name: "new-inbound-listener",
					},
				},

				outboundListeners: []*listener.Listener{
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
				outboundListeners: []*listener.Listener{
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
				inboundListeners: []*listener.Listener{
					{
						Name: "new-inbound-listener",
					},
				},

				outboundListeners: []*listener.Listener{
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
			name:  "patch add gateway listener",
			proxy: gatewayProxy,
			fields: fields{
				gatewayListeners: []*listener.Listener{
					{
						Name: "gateway-listener",
					},
				},
			},
			want: fields{
				gatewayListeners: []*listener.Listener{
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
				gatewayListeners: []*listener.Listener{
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
				gatewayListeners: []*listener.Listener{
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
				gatewayListeners:        tt.fields.gatewayListeners,
				inboundListeners:        tt.fields.inboundListeners,
				outboundListeners:       tt.fields.outboundListeners,
				httpProxyListener:       tt.fields.httpProxyListener,
				virtualOutboundListener: tt.fields.virtualOutboundListener,
				virtualInboundListener:  tt.fields.virtualInboundListener,
			}

			lb.patchListeners()
			got := fields{
				gatewayListeners:        lb.gatewayListeners,
				inboundListeners:        lb.inboundListeners,
				outboundListeners:       lb.outboundListeners,
				httpProxyListener:       lb.httpProxyListener,
				virtualOutboundListener: lb.virtualOutboundListener,
				virtualInboundListener:  lb.virtualInboundListener,
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Unexpected default listener, want \n%#v got \n%#v", tt.want, got)
			}
		})
	}
}

func buildPatchStruct(config string) *structpb.Struct {
	val := &structpb.Struct{}
	_ = jsonpb.Unmarshal(strings.NewReader(config), val)
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
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: STRICT
`

const disableMode = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: DISABLE
`

func TestInboundListenerFilters(t *testing.T) {
	services := []*model.Service{
		buildServiceWithPort("test1.com", 80, protocol.HTTP, tnow),
		buildServiceWithPort("test2.com", 81, protocol.Unsupported, tnow),
		buildServiceWithPort("test3.com", 82, protocol.TCP, tnow),
	}
	instances := make([]*model.ServiceInstance, 0, len(services))
	for _, s := range services {
		instances = append(instances, &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				EndpointPort: uint32(s.Ports[0].Port),
				Address:      "1.1.1.1",
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
				// But should see for passthrough or unnamed ports
				81:   false,
				1000: false,
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
			evaluateListenerFilterPredicates(t, filters[wellknown.HttpInspector].FilterDisabled, tt.http)
			if filters[wellknown.TlsInspector] == nil {
				if len(tt.tls) > 0 {
					t.Fatalf("Expected tls inspector, got none")
				}
			} else {
				evaluateListenerFilterPredicates(t, filters[wellknown.TlsInspector].FilterDisabled, tt.tls)
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

type TestAuthnPlugin struct {
	mtlsSettings []plugin.MTLSSettings
}

func TestSidecarInboundListenerFilters(t *testing.T) {
	services := []*model.Service{
		buildServiceWithPort("test.com", 80, protocol.HTTPS, tnow),
	}
	instances := make([]*model.ServiceInstance, 0, len(services))
	for _, s := range services {
		instances = append(instances, &model.ServiceInstance{
			Service: s,
			Endpoint: &model.IstioEndpoint{
				EndpointPort: uint32(s.Ports[0].Port),
				Address:      "1.1.1.1",
			},
			ServicePort: s.Ports[0],
		})
	}
	cases := []struct {
		name           string
		sidecarScope   *model.SidecarScope
		mtlsSettings   []plugin.MTLSSettings
		expectedResult func(filterChain *listener.FilterChain)
	}{
		{
			name: "simulate peer auth disabled on port 80",
			sidecarScope: &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.Port{Name: "https-port", Protocol: "https", Number: 80},
							Tls: &networking.ServerTLSSettings{
								Mode:              networking.ServerTLSSettings_SIMPLE,
								ServerCertificate: "cert.pem",
								PrivateKey:        "privatekey.pem",
							},
						},
					},
				},
			},
			mtlsSettings: []plugin.MTLSSettings{{
				Mode: model.MTLSDisable,
			}},
			expectedResult: func(filterChain *listener.FilterChain) {
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
				if tlsContext.RequireClientCertificate.Value == true {
					t.Fatalf("expected RequireClientCertificate to be false")
				}
			},
		},
		{
			name: "simulate peer auth strict",
			sidecarScope: &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.Port{Name: "https-port", Protocol: "https", Number: 80},
							Tls: &networking.ServerTLSSettings{
								Mode:              networking.ServerTLSSettings_SIMPLE,
								ServerCertificate: "cert.pem",
								PrivateKey:        "privatekey.pem",
							},
						},
					},
				},
			},
			mtlsSettings: []plugin.MTLSSettings{{
				Mode: model.MTLSStrict,
			}},
			expectedResult: func(filterChain *listener.FilterChain) {
				if filterChain.GetTransportSocket() != nil {
					t.Fatal("expected transport socket to be nil")
				}
			},
		},
		{
			name: "simulate peer auth permissive",
			sidecarScope: &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.Port{Name: "https-port", Protocol: "https", Number: 80},
							Tls: &networking.ServerTLSSettings{
								Mode:              networking.ServerTLSSettings_SIMPLE,
								ServerCertificate: "cert.pem",
								PrivateKey:        "privatekey.pem",
							},
						},
					},
				},
			},
			mtlsSettings: []plugin.MTLSSettings{{
				Mode: model.MTLSPermissive,
			}},
			expectedResult: func(filterChain *listener.FilterChain) {
				if filterChain.GetTransportSocket() != nil {
					t.Fatal("expected transport socket to be nil")
				}
			},
		},
		{
			name: "simulate multiple mode returned in mtlssettings",
			sidecarScope: &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.Port{Name: "https-port", Protocol: "https", Number: 80},
							Tls: &networking.ServerTLSSettings{
								Mode:              networking.ServerTLSSettings_SIMPLE,
								ServerCertificate: "cert.pem",
								PrivateKey:        "privatekey.pem",
							},
						},
					},
				},
			},
			mtlsSettings: []plugin.MTLSSettings{
				{
					Mode: model.MTLSStrict,
				},
				{
					Mode: model.MTLSDisable,
				},
			},
			expectedResult: func(filterChain *listener.FilterChain) {
				if filterChain.GetTransportSocket() != nil {
					t.Fatal("expected transport socket to be nil")
				}
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			testPlugin := &TestAuthnPlugin{
				mtlsSettings: tt.mtlsSettings,
			}
			cg := NewConfigGenTest(t, TestOptions{
				Services:  services,
				Instances: instances,
				Plugins:   []plugin.Plugin{testPlugin},
			})
			proxy := cg.SetupProxy(nil)
			proxy.Metadata = &model.NodeMetadata{Labels: map[string]string{"app": "foo"}}
			proxy.SidecarScope = tt.sidecarScope
			features.EnableTLSOnSidecarIngress = true
			listeners := cg.Listeners(proxy)
			virtualInbound := xdstest.ExtractListener("virtualInbound", listeners)
			filterChain := xdstest.ExtractFilterChain("1.1.1.1_80", virtualInbound)
			tt.expectedResult(filterChain)
		})
	}
}

func (t TestAuthnPlugin) OnOutboundListener(in *plugin.InputParams, mutable *istionetworking.MutableObjects) error {
	return nil
}

func (t TestAuthnPlugin) OnInboundListener(in *plugin.InputParams, mutable *istionetworking.MutableObjects) error {
	return nil
}

func (t TestAuthnPlugin) OnInboundPassthrough(in *plugin.InputParams, mutable *istionetworking.MutableObjects) error {
	return nil
}

func (t TestAuthnPlugin) InboundMTLSConfiguration(in *plugin.InputParams, passthrough bool) []plugin.MTLSSettings {
	return t.mtlsSettings
}
