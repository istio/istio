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
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schemas"
)

const (
	wildcardIP           = "0.0.0.0"
	fakePluginHTTPFilter = "fake-plugin-http-filter"
	fakePluginTCPFilter  = "fake-plugin-tcp-filter"
)

var (
	tnow  = time.Now()
	tzero = time.Time{}
	proxy = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.1",
		},
		IstioVersion:    &model.IstioVersion{Major: 1, Minor: 3},
		ConfigNamespace: "not-default",
	}
	proxyHTTP10 = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.1",
			HTTP10:          "1",
		},
		ConfigNamespace: "not-default",
	}
	proxy13 = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.3",
		},
		ConfigNamespace: "not-default",
	}
	proxy13HTTP10 = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.3",
			HTTP10:          "1",
		},
		IstioVersion:    &model.IstioVersion{Major: 1, Minor: 3},
		ConfigNamespace: "not-default",
	}
	proxy13Gateway = model.Proxy{
		Type:        model.Router,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.3",
		},
		ConfigNamespace: "not-default",
		WorkloadLabels:  labels.Collection{{"istio": "ingressgateway"}},
	}
	proxyInstances = []*model.ServiceInstance{
		{
			Service: &model.Service{
				Hostname:     "v0.default.example.org",
				Address:      "9.9.9.9",
				CreationTime: tnow,
				Attributes: model.ServiceAttributes{
					Namespace: "not-default",
				},
			},
			Labels: nil,
		},
	}
	virtualServiceSpec = &networking.VirtualService{
		Hosts:    []string{"test.com"},
		Gateways: []string{"mesh"},
		Tcp: []*networking.TCPRoute{
			{
				Match: []*networking.L4MatchAttributes{
					{
						DestinationSubnets: []string{"10.10.0.0/24"},
						Port:               8080,
					},
				},
				Route: []*networking.RouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 80,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
)

func TestInboundListenerConfigProxyV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForInbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForInbound.Name) }()

	for _, p := range []*model.Proxy{&proxy13, &proxy13HTTP10} {
		testInboundListenerConfigV13(t, p,
			buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
			buildService("test2.com", wildcardIP, "unknown", tnow),
			buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
		testInboundListenerConfigWithoutServiceV13(t, p)
		testInboundListenerConfigWithSidecarV13(t, p,
			buildService("test.com", wildcardIP, protocol.HTTP, tnow))
		testInboundListenerConfigWithSidecarWithoutServicesV13(t, p)
	}
}

func TestOutboundListenerConflict_HTTPWithCurrentUnknownV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV13(t,
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, "unknown", tnow),
		buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_WellKnowPortsV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV13(t,
		buildServiceWithPort("test1.com", 3306, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test2.com", 3306, protocol.MySQL, tnow))
	testOutboundListenerConflictV13(t,
		buildServiceWithPort("test1.com", 9999, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test2.com", 9999, protocol.MySQL, tnow))
}

func TestOutboundListenerConflict_TCPWithCurrentUnknownV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV13(t,
		buildService("test1.com", wildcardIP, protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, "unknown", tnow),
		buildService("test3.com", wildcardIP, protocol.TCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentTCPV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV13(t,
		buildService("test1.com", wildcardIP, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentHTTPV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV13(t,
		buildService("test1.com", wildcardIP, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.HTTP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerRouteV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	testOutboundListenerRouteV13(t,
		buildService("test1.com", "1.2.3.4", "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", "2.3.4.5", protocol.HTTP, tnow),
		buildService("test3.com", "3.4.5.6", "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConfig_WithSidecarV13(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second))}
	testOutboundListenerConfigWithSidecarV13(t, services...)
}

func TestOutboundListenerConflict_HTTPWithCurrentTCP(t *testing.T) {
	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_TCPWithCurrentHTTP(t *testing.T) {
	// The oldest service port is HTTP.  We should encounter conflicts when attempting to add the TCP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.HTTP, tnow),
		buildService("test3.com", wildcardIP, protocol.TCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_Unordered(t *testing.T) {
	// Ensure that the order is preserved when all the times match. The first service in the list wins.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, protocol.HTTP, tzero),
		buildService("test2.com", wildcardIP, protocol.TCP, tzero),
		buildService("test3.com", wildcardIP, protocol.TCP, tzero))
}

func TestOutboundListenerConflict_HTTPoverHTTPS(t *testing.T) {
	cases := []struct {
		name             string
		service          *model.Service
		expectedListener []string
	}{
		{
			"http on 443",
			buildServiceWithPort("test1.com", CanonicalHTTPSPort, protocol.HTTP, tnow.Add(1*time.Second)),
			[]string{},
		},
		{
			"http on 80",
			buildServiceWithPort("test1.com", CanonicalHTTPSPort, protocol.HTTP, tnow.Add(1*time.Second)),
			[]string{},
		},
		{
			"https on 443",
			buildServiceWithPort("test1.com", CanonicalHTTPSPort, protocol.HTTPS, tnow.Add(1*time.Second)),
			[]string{"0.0.0.0_443"},
		},
		{
			"tcp on 443",
			buildServiceWithPort("test1.com", CanonicalHTTPSPort, protocol.TCP, tnow.Add(1*time.Second)),
			[]string{"0.0.0.0_443"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := &fakePlugin{}
			listeners := buildOutboundListeners(p, &proxy, nil, nil, tt.service)
			got := []string{}
			for _, l := range listeners {
				got = append(got, l.Name)
			}
			if !reflect.DeepEqual(got, tt.expectedListener) {
				t.Fatalf("expected listener %v got %v", tt.expectedListener, got)
			}
		})
	}
}

func TestOutboundListenerConflict_TCPWithCurrentTCP(t *testing.T) {
	services := []*model.Service{
		buildService("test1.com", "1.2.3.4", protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", "1.2.3.4", protocol.TCP, tnow),
		buildService("test3.com", "1.2.3.4", protocol.TCP, tnow.Add(2*time.Second)),
	}
	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, &proxy, nil, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	// The filter chains should all be merged into one.
	if len(listeners[0].FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(listeners[0].FilterChains))
	}
	verifyOutboundTCPListenerHostname(t, listeners[0], "test2.com")

	oldestService := getOldestService(services...)
	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != protocol.HTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == protocol.HTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}

	if p.outboundListenerParams[0].Service != oldestService {
		t.Fatalf("listener conflict failed to preserve listener for the oldest service")
	}
}

func TestOutboundListenerTCPWithVS(t *testing.T) {
	_ = os.Setenv("PILOT_ENABLE_FALLTHROUGH_ROUTE", "false")

	defer func() { _ = os.Unsetenv("PILOT_ENABLE_FALLTHROUGH_ROUTE") }()

	tests := []struct {
		name           string
		CIDR           string
		expectedChains []string
	}{
		{
			name:           "same CIDR",
			CIDR:           "10.10.0.0/24",
			expectedChains: []string{"10.10.0.0"},
		},
		{
			name:           "different CIDR",
			CIDR:           "10.10.10.0/24",
			expectedChains: []string{"10.10.0.0", "10.10.10.0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if features.RestrictPodIPTrafficLoops.Get() {
				// Expect a filter chain on the node IP
				tt.expectedChains = append([]string{"1.1.1.1"}, tt.expectedChains...)
			}
			services := []*model.Service{
				buildService("test.com", tt.CIDR, protocol.TCP, tnow),
			}

			p := &fakePlugin{}
			virtualService := model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:      schemas.VirtualService.Type,
					Version:   schemas.VirtualService.Version,
					Name:      "test_vs",
					Namespace: "default",
				},
				Spec: virtualServiceSpec,
			}
			listeners := buildOutboundListeners(p, &proxy, nil, &virtualService, services...)

			if len(listeners) != 1 {
				t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
			}
			var chains []string
			for _, fc := range listeners[0].FilterChains {
				for _, cidr := range fc.FilterChainMatch.PrefixRanges {
					chains = append(chains, cidr.AddressPrefix)
				}
			}
			// There should not be multiple filter chains with same CIDR match
			if !reflect.DeepEqual(chains, tt.expectedChains) {
				t.Fatalf("expected filter chains %v, found %v", tt.expectedChains, chains)
			}
		})
	}
}

func TestOutboundListenerForHeadlessServices(t *testing.T) {
	_ = os.Setenv("PILOT_ENABLE_FALLTHROUGH_ROUTE", "false")

	defer func() { _ = os.Unsetenv("PILOT_ENABLE_FALLTHROUGH_ROUTE") }()

	svc := buildServiceWithPort("test.com", 9999, protocol.TCP, tnow)
	svc.Attributes.ServiceRegistry = string(serviceregistry.KubernetesRegistry)
	svc.Resolution = model.Passthrough
	services := []*model.Service{svc}

	p := &fakePlugin{}
	tests := []struct {
		name                      string
		instances                 []*model.ServiceInstance
		numListenersOnServicePort int
	}{
		{
			name: "gen a listener per instance",
			instances: []*model.ServiceInstance{
				// This instance is the proxy itself, will not gen a outbound listener for it.
				buildServiceInstance(services[0], "1.1.1.1"),
				buildServiceInstance(services[0], "10.10.10.10"),
				buildServiceInstance(services[0], "11.11.11.11"),
				buildServiceInstance(services[0], "12.11.11.11"),
			},
			numListenersOnServicePort: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configgen := NewConfigGenerator([]plugin.Plugin{p})

			env := buildListenerEnv(services)
			serviceDiscovery := new(fakes.ServiceDiscovery)
			serviceDiscovery.ServicesReturns(services, nil)
			serviceDiscovery.InstancesByPortReturns(tt.instances, nil)
			env.ServiceDiscovery = serviceDiscovery
			if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
				t.Errorf("Failed to initialize push context: %v", err)
			}

			proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
			proxy.ServiceInstances = proxyInstances

			listeners := configgen.buildSidecarOutboundListeners(&env, &proxy, env.PushContext)
			listenersToCheck := make([]*xdsapi.Listener, 0)
			for _, l := range listeners {
				if l.Address.GetSocketAddress().GetPortValue() == 9999 {
					listenersToCheck = append(listenersToCheck, l)
				}
			}

			if len(listenersToCheck) != tt.numListenersOnServicePort {
				t.Errorf("Expected %d listeners on service port 9999, got %d", tt.numListenersOnServicePort, len(listenersToCheck))
			}
		})
	}
}

func TestInboundListenerConfig_HTTP(t *testing.T) {
	for _, p := range []*model.Proxy{&proxy, &proxyHTTP10} {
		// Add a service and verify it's config
		testInboundListenerConfig(t, p,
			buildService("test.com", wildcardIP, protocol.HTTP, tnow))
		testInboundListenerConfigWithoutServices(t, p)
		testInboundListenerConfigWithSidecar(t, p,
			buildService("test.com", wildcardIP, protocol.HTTP, tnow))
		testInboundListenerConfigWithSidecarWithoutServices(t, p)
	}
}

func TestOutboundListenerConfig_WithSidecar(t *testing.T) {
	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second))}
	testOutboundListenerConfigWithSidecar(t, services...)
	testOutboundListenerConfigWithSidecarWithCaptureModeNone(t, services...)
	testOutboundListenerConfigWithSidecarWithUseRemoteAddress(t, services...)
}

func TestGetActualWildcardAndLocalHost(t *testing.T) {
	tests := []struct {
		name     string
		proxy    *model.Proxy
		expected [2]string
	}{
		{
			name: "ipv4 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1.1.1.1", "127.0.0.1", "2.2.2.2"},
			},
			expected: [2]string{WildcardAddress, LocalhostAddress},
		},
		{
			name: "ipv6 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "2222:3333::1"},
			},
			expected: [2]string{WildcardIPv6Address, LocalhostIPv6Address},
		},
		{
			name: "mixed ipv4 and ipv6",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "127.0.0.1", "2.2.2.2", "2222:3333::1"},
			},
			expected: [2]string{WildcardAddress, LocalhostAddress},
		},
	}
	for _, tt := range tests {
		wm, lh := getActualWildcardAndLocalHost(tt.proxy)
		if wm != tt.expected[0] && lh != tt.expected[1] {
			t.Errorf("Test %s failed, expected: %s / %s got: %s / %s", tt.name, tt.expected[0], tt.expected[1], wm, lh)
		}
	}
}

func testOutboundListenerConflict(t *testing.T, services ...*model.Service) {
	t.Helper()

	oldestService := getOldestService(services...)

	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, &proxy, nil, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != protocol.HTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == protocol.HTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}

	if len(p.outboundListenerParams) != 1 {
		t.Fatalf("expected %d listener params, found %d", 1, len(p.outboundListenerParams))
	}

	if p.outboundListenerParams[0].Service != oldestService {
		t.Fatalf("listener conflict failed to preserve listener for the oldest service")
	}
}

func testOutboundListenerRouteV13(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, &proxy13, nil, nil, services...)
	if len(listeners) != 3 {
		t.Fatalf("expected %d listeners, found %d", 3, len(listeners))
	}

	l := findListenerByAddress(listeners, wildcardIP)
	if l == nil {
		t.Fatalf("expect listener %s", "0.0.0.0_8080")
	}

	f := l.FilterChains[1].Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	rds := cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
	if rds != "8080" {
		t.Fatalf("expect routes %s, found %s", "8080", rds)
	}

	l = findListenerByAddress(listeners, "1.2.3.4")
	if l == nil {
		t.Fatalf("expect listener %s", "1.2.3.4_8080")
	}
	f = l.FilterChains[1].Filters[0]
	cfg, _ = conversion.MessageToStruct(f.GetTypedConfig())
	rds = cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
	if rds != "test1.com:8080" {
		t.Fatalf("expect routes %s, found %s", "test1.com:8080", rds)
	}

	l = findListenerByAddress(listeners, "3.4.5.6")
	if l == nil {
		t.Fatalf("expect listener %s", "3.4.5.6_8080")
	}
	f = l.FilterChains[1].Filters[0]
	cfg, _ = conversion.MessageToStruct(f.GetTypedConfig())
	rds = cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
	if rds != "test3.com:8080" {
		t.Fatalf("expect routes %s, found %s", "test3.com:8080", rds)
	}
}

func testOutboundListenerConflictV13(t *testing.T, services ...*model.Service) {
	t.Helper()
	oldestService := getOldestService(services...)
	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, &proxy13, nil, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol == protocol.MySQL {
		if len(listeners[0].FilterChains) != 2 {
			t.Fatalf("expectd %d filter chains, found %d", 2, len(listeners[0].FilterChains))
		} else if !isTCPFilterChain(listeners[0].FilterChains[1]) {
			t.Fatalf("expected tcp filter chain, found %s", listeners[0].FilterChains[1].Filters[0].Name)
		}
	} else if oldestProtocol != protocol.HTTP && oldestProtocol != protocol.TCP {
		if len(listeners[0].FilterChains) != 3 {
			t.Fatalf("expectd %d filter chains, found %d", 3, len(listeners[0].FilterChains))
		} else {
			if !isHTTPFilterChain(listeners[0].FilterChains[2]) {
				t.Fatalf("expected http filter chain, found %s", listeners[0].FilterChains[1].Filters[0].Name)
			}

			if !isTCPFilterChain(listeners[0].FilterChains[1]) {
				t.Fatalf("expected tcp filter chain, found %s", listeners[0].FilterChains[2].Filters[0].Name)
			}
		}

		verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[2], model.TrafficDirectionOutbound)
		if len(listeners[0].ListenerFilters) != 2 ||
			listeners[0].ListenerFilters[0].Name != "envoy.listener.tls_inspector" ||
			listeners[0].ListenerFilters[1].Name != "envoy.listener.http_inspector" {
			t.Fatalf("expected %d listener filter, found %d", 2, len(listeners[0].ListenerFilters))
		}

		f := listeners[0].FilterChains[2].Filters[0]
		cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
		rds := cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
		expect := fmt.Sprintf("%d", oldestService.Ports[0].Port)
		if rds != expect {
			t.Fatalf("expect routes %s, found %s", expect, rds)
		}
	} else {
		if len(listeners[0].FilterChains) != 3 {
			t.Fatalf("expectd %d filter chains, found %d", 3, len(listeners[0].FilterChains))
		}

		if !isTCPFilterChain(listeners[0].FilterChains[1]) {
			t.Fatalf("expected tcp filter chain, found %s", listeners[0].FilterChains[2].Filters[0].Name)
		}

		if !isHTTPFilterChain(listeners[0].FilterChains[2]) {
			t.Fatalf("expected http filter chain, found %s", listeners[0].FilterChains[1].Filters[0].Name)
		}

		verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[2], model.TrafficDirectionOutbound)
		if len(listeners[0].ListenerFilters) != 2 ||
			listeners[0].ListenerFilters[0].Name != "envoy.listener.tls_inspector" ||
			listeners[0].ListenerFilters[1].Name != "envoy.listener.http_inspector" {
			t.Fatalf("expected %d listener filter, found %d", 2, len(listeners[0].ListenerFilters))
		}
	}
}

func testInboundListenerConfigV13(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, proxy, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	if len(listeners[0].FilterChains) != 4 ||
		!isHTTPFilterChain(listeners[0].FilterChains[0]) ||
		!isHTTPFilterChain(listeners[0].FilterChains[1]) ||
		!isTCPFilterChain(listeners[0].FilterChains[2]) ||
		!isTCPFilterChain(listeners[0].FilterChains[3]) {
		t.Fatalf("expectd %d filter chains, %d http filter chains and %d tcp filter chain", 4, 2, 2)
	}

	verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[0], model.TrafficDirectionInbound)
	verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[1], model.TrafficDirectionInbound)
}

func testInboundListenerConfigWithSidecarV13(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Number:   8080,
						Protocol: "unknown",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:80",
				},
			},
		},
	}
	listeners := buildInboundListeners(p, proxy, sidecarConfig, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	if len(listeners[0].FilterChains) != 4 ||
		!isHTTPFilterChain(listeners[0].FilterChains[0]) ||
		!isHTTPFilterChain(listeners[0].FilterChains[1]) ||
		!isTCPFilterChain(listeners[0].FilterChains[2]) ||
		!isTCPFilterChain(listeners[0].FilterChains[3]) {
		t.Fatalf("expectd %d filter chains, %d http filter chains and %d tcp filter chain", 4, 2, 2)
	}

	verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[0], model.TrafficDirectionInbound)
	verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[1], model.TrafficDirectionInbound)
}

func testInboundListenerConfigWithSidecarWithoutServicesV13(t *testing.T, proxy *model.Proxy) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo-without-service",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Number:   8080,
						Protocol: "unknown",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:80",
				},
			},
		},
	}
	listeners := buildInboundListeners(p, proxy, sidecarConfig)
	if expected := 1; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}

	if len(listeners[0].FilterChains) != 4 ||
		!isHTTPFilterChain(listeners[0].FilterChains[0]) ||
		!isHTTPFilterChain(listeners[0].FilterChains[1]) ||
		!isTCPFilterChain(listeners[0].FilterChains[2]) ||
		!isTCPFilterChain(listeners[0].FilterChains[3]) {
		t.Fatalf("expectd %d filter chains, %d http filter chains and %d tcp filter chain", 4, 2, 2)
	}

	verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[0], model.TrafficDirectionInbound)
	verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[1], model.TrafficDirectionInbound)
}

func testInboundListenerConfigWithoutServiceV13(t *testing.T, proxy *model.Proxy) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, proxy, nil)
	if expected := 0; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}
}

func verifyHTTPFilterChainMatch(t *testing.T, fc *listener.FilterChain, direction model.TrafficDirection) {
	t.Helper()
	if direction == model.TrafficDirectionInbound &&
		(len(fc.FilterChainMatch.ApplicationProtocols) != 2 ||
			fc.FilterChainMatch.ApplicationProtocols[0] != "http/1.0" ||
			fc.FilterChainMatch.ApplicationProtocols[1] != "http/1.1") {
		t.Fatalf("expected %d application protocols, [http/1.0, http/1.1]", 2)
	}

	if direction == model.TrafficDirectionOutbound &&
		(len(fc.FilterChainMatch.ApplicationProtocols) != 3 ||
			fc.FilterChainMatch.ApplicationProtocols[0] != "http/1.0" ||
			fc.FilterChainMatch.ApplicationProtocols[1] != "http/1.1" ||
			fc.FilterChainMatch.ApplicationProtocols[2] != "h2") {
		t.Fatalf("expected %d application protocols, [http/1.0, http/1.1, h2]", 3)
	}
}

func isHTTPFilterChain(fc *listener.FilterChain) bool {
	return len(fc.Filters) > 0 && fc.Filters[0].Name == "envoy.http_connection_manager"
}

func isTCPFilterChain(fc *listener.FilterChain) bool {
	return len(fc.Filters) > 0 && fc.Filters[0].Name == "envoy.tcp_proxy"
}

func testOutboundListenerConfigWithSidecarV13(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   9000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.Port{
						Number:   3306,
						Protocol: string(protocol.MySQL),
						Name:     "MySQL",
					},
					Bind:  "8.8.8.8",
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.Port{
						Number:   8888,
						Protocol: "unknown",
						Name:     "unknown",
					},
					Bind:  "2.2.2.2",
					Hosts: []string{"*/*"},
				},
				{
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	// enable mysql filter that is used here
	_ = os.Setenv(features.EnableMysqlFilter.Name, "true")

	defer func() { _ = os.Unsetenv(features.EnableMysqlFilter.Name) }()

	listeners := buildOutboundListeners(p, &proxy13, sidecarConfig, nil, services...)
	if len(listeners) != 4 {
		t.Fatalf("expected %d listeners, found %d", 4, len(listeners))
	}

	l := findListenerByPort(listeners, 8080)
	if len(l.FilterChains) != 4 {
		t.Fatalf("expectd %d filter chains, found %d", 4, len(l.FilterChains))
	} else {
		if !isHTTPFilterChain(l.FilterChains[3]) {
			t.Fatalf("expected http filter chain, found %s", l.FilterChains[3].Filters[0].Name)
		}

		if !isTCPFilterChain(l.FilterChains[1]) {
			t.Fatalf("expected tcp filter chain, found %s", l.FilterChains[1].Filters[0].Name)
		}

		verifyHTTPFilterChainMatch(t, l.FilterChains[3], model.TrafficDirectionOutbound)

		if len(l.ListenerFilters) != 2 ||
			l.ListenerFilters[0].Name != "envoy.listener.tls_inspector" ||
			l.ListenerFilters[1].Name != "envoy.listener.http_inspector" {
			t.Fatalf("expected %d listener filter, found %d", 2, len(l.ListenerFilters))
		}
	}

	if l := findListenerByPort(listeners, 3306); !isMysqlListener(l) {
		t.Fatalf("expected MySQL listener on port 3306, found %v", l)
	}

	if l := findListenerByPort(listeners, 9000); !isHTTPListener(l) {
		t.Fatalf("expected HTTP listener on port 9000, found TCP\n%v", l)
	}

	l = findListenerByPort(listeners, 8888)
	if len(l.FilterChains) != 2 {
		t.Fatalf("expectd %d filter chains, found %d", 2, len(l.FilterChains))
	} else {
		if !isHTTPFilterChain(l.FilterChains[1]) {
			t.Fatalf("expected http filter chain, found %s", l.FilterChains[0].Filters[0].Name)
		}

		if !isTCPFilterChain(l.FilterChains[0]) {
			t.Fatalf("expected tcp filter chain, found %s", l.FilterChains[1].Filters[0].Name)
		}
	}

	verifyHTTPFilterChainMatch(t, l.FilterChains[1], model.TrafficDirectionOutbound)
	if len(l.ListenerFilters) != 2 ||
		l.ListenerFilters[0].Name != "envoy.listener.tls_inspector" ||
		l.ListenerFilters[1].Name != "envoy.listener.http_inspector" {
		t.Fatalf("expected %d listener filter, found %d", 2, len(l.ListenerFilters))
	}
}

func testInboundListenerConfig(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	oldestService := getOldestService(services...)
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, proxy, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != protocol.HTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == protocol.HTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}
	verifyInboundHTTPListenerServerName(t, listeners[0])
	verifyInboundHTTPListenerStatPrefix(t, listeners[0])
	if isHTTPListener(listeners[0]) {
		verifyInboundHTTPListenerCertDetails(t, listeners[0])
		verifyInboundHTTPListenerNormalizePath(t, listeners[0])
	}
	for _, l := range listeners {
		verifyInboundHTTP10(t, isNodeHTTP10(proxy), l)
	}

	verifyInboundEnvoyListenerNumber(t, listeners[0])
}

func testInboundListenerConfigWithoutServices(t *testing.T, proxy *model.Proxy) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, proxy, nil)
	if expected := 0; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}
}

func testInboundListenerConfigWithSidecar(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Number:   8080,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:80",
				},
			},
		},
	}
	listeners := buildInboundListeners(p, proxy, sidecarConfig, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	if !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}
	for _, l := range listeners {
		verifyInboundHTTP10(t, isNodeHTTP10(proxy), l)
	}
}

func testInboundListenerConfigWithSidecarWithoutServices(t *testing.T, proxy *model.Proxy) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo-without-service",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Number:   8080,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:80",
				},
			},
		},
	}
	listeners := buildInboundListeners(p, proxy, sidecarConfig)
	if expected := 1; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}
	if !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}
	for _, l := range listeners {
		verifyInboundHTTP10(t, isNodeHTTP10(proxy), l)
	}
}

func testOutboundListenerConfigWithSidecar(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   9000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.Port{
						Number:   3306,
						Protocol: string(protocol.MySQL),
						Name:     "MySQL",
					},
					Bind:  "8.8.8.8",
					Hosts: []string{"*/*"},
				},
				{
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	// enable mysql filter that is used here
	_ = os.Setenv(features.EnableMysqlFilter.Name, "true")

	defer func() { _ = os.Unsetenv(features.EnableMysqlFilter.Name) }()

	listeners := buildOutboundListeners(p, &proxy, sidecarConfig, nil, services...)
	if len(listeners) != 3 {
		t.Fatalf("expected %d listeners, found %d", 3, len(listeners))
	}

	if l := findListenerByPort(listeners, 8080); isHTTPListener(l) {
		t.Fatalf("expected TCP listener on port 8080, found HTTP: %v", l)
	}

	if l := findListenerByPort(listeners, 3306); !isMysqlListener(l) {
		t.Fatalf("expected MySQL listener on port 3306, found %v", l)
	}

	if l := findListenerByPort(listeners, 9000); !isHTTPListener(l) {
		t.Fatalf("expected HTTP listener on port 9000, found TCP\n%v", l)
	} else {
		f := l.FilterChains[0].Filters[0]
		cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
		if !strings.HasPrefix(cfg.Fields["stat_prefix"].GetStringValue(), "outbound_") {
			t.Fatalf("expected stat prefix to have outbound, %s", cfg.Fields["stat_prefix"].GetStringValue())
		}
		if useRemoteAddress, exists := cfg.Fields["use_remote_address"]; exists {
			if exists && useRemoteAddress.GetBoolValue() {
				t.Fatalf("expected useRemoteAddress false, found true %v", l)
			}
		}
	}
}

func testOutboundListenerConfigWithSidecarWithUseRemoteAddress(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   9000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	// enable use remote address to true
	_ = os.Setenv(features.UseRemoteAddress.Name, "true")

	defer func() { _ = os.Unsetenv(features.UseRemoteAddress.Name) }()

	listeners := buildOutboundListeners(p, &proxy, sidecarConfig, nil, services...)

	if l := findListenerByPort(listeners, 9000); !isHTTPListener(l) {
		t.Fatalf("expected HTTP listener on port 9000, found TCP\n%v", l)
	} else {
		f := l.FilterChains[0].Filters[0]
		cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
		if useRemoteAddress, exists := cfg.Fields["use_remote_address"]; exists {
			if !exists || !useRemoteAddress.GetBoolValue() {
				t.Fatalf("expected useRemoteAddress true, found false %v", l)
			}
		}
	}
}

func testOutboundListenerConfigWithSidecarWithCaptureModeNone(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					// Bind + Port
					CaptureMode: networking.CaptureMode_NONE,
					Port: &networking.Port{
						Number:   9090,
						Protocol: "HTTP",
						Name:     "grpc",
					},
					Bind:  "127.1.1.2",
					Hosts: []string{"*/*"},
				},
				{
					// Bind Only
					CaptureMode: networking.CaptureMode_NONE,
					Bind:        "127.1.1.2",
					Hosts:       []string{"*/*"},
				},
				{
					// Port Only
					CaptureMode: networking.CaptureMode_NONE,
					Port: &networking.Port{
						Number:   9090,
						Protocol: "HTTP",
						Name:     "grpc",
					},
					Hosts: []string{"*/*"},
				},
				{
					// None
					CaptureMode: networking.CaptureMode_NONE,
					Hosts:       []string{"*/*"},
				},
			},
		},
	}
	listeners := buildOutboundListeners(p, &proxy, sidecarConfig, nil, services...)
	if len(listeners) != 4 {
		t.Fatalf("expected %d listeners, found %d", 4, len(listeners))
	}

	expectedListeners := map[string]string{
		"127.1.1.2_9090": "HTTP",
		"127.1.1.2_8080": "TCP",
		"127.0.0.1_9090": "HTTP",
		"127.0.0.1_8080": "TCP",
	}

	for _, l := range listeners {
		listenerName := l.Name
		expectedListenerType := expectedListeners[listenerName]
		if expectedListenerType == "" {
			t.Fatalf("listener %s not expected", listenerName)
		}
		if expectedListenerType == "TCP" && isHTTPListener(l) {
			t.Fatalf("expected TCP listener %s, but found HTTP", listenerName)
		}
		if expectedListenerType == "HTTP" && !isHTTPListener(l) {
			t.Fatalf("expected HTTP listener %s, but found TCP", listenerName)
		}
	}
}

func TestOutboundListenerAccessLogs(t *testing.T) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildAllListeners(p, nil)
	for _, l := range listeners {
		if l.Name == "virtual" {
			fc := &tcp_proxy.TcpProxy{}
			if err := getFilterConfig(l.FilterChains[0].Filters[0], fc); err != nil {
				t.Fatalf("failed to get TCP Proxy config: %s", err)
			}
			if fc.AccessLog == nil {
				t.Fatal("expected access log configuration")
			}
		}
	}
}

func verifyOutboundTCPListenerHostname(t *testing.T, l *xdsapi.Listener, hostname host.Name) {
	t.Helper()
	if len(l.FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
	}
	f := fc.Filters[0]
	expectedStatPrefix := fmt.Sprintf("outbound|8080||%s", hostname)
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	statPrefix := cfg.Fields["stat_prefix"].GetStringValue()
	if statPrefix != expectedStatPrefix {
		t.Fatalf("expected listener to contain stat_prefix %s, found %s", expectedStatPrefix, statPrefix)
	}
}

func verifyInboundHTTPListenerServerName(t *testing.T, l *xdsapi.Listener) {
	t.Helper()
	if len(l.FilterChains) != 2 {
		t.Fatalf("expected %d filter chains, found %d", 2, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
	}
	f := fc.Filters[0]
	expectedServerName := "istio-envoy"
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	serverName := cfg.Fields["server_name"].GetStringValue()
	if serverName != expectedServerName {
		t.Fatalf("expected listener to contain server_name %s, found %s", expectedServerName, serverName)
	}
}

func verifyInboundHTTPListenerStatPrefix(t *testing.T, l *xdsapi.Listener) {
	t.Helper()
	if len(l.FilterChains) != 2 {
		t.Fatalf("expected %d filter chains, found %d", 2, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
	}
	f := fc.Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	if !strings.HasPrefix(cfg.Fields["stat_prefix"].GetStringValue(), "inbound_") {
		t.Fatalf("expected stat prefix to have %s , found %s", "inbound", cfg.Fields["stat_prefix"].GetStringValue())
	}

}

func verifyInboundEnvoyListenerNumber(t *testing.T, l *xdsapi.Listener) {
	t.Helper()
	if len(l.FilterChains) != 2 {
		t.Fatalf("expected %d filter chains, found %d", 2, len(l.FilterChains))
	}

	for _, fc := range l.FilterChains {
		if len(fc.Filters) != 1 {
			t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
		}

		f := fc.Filters[0]
		cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
		hf := cfg.Fields["http_filters"].GetListValue()
		if len(hf.Values) != 4 {
			t.Fatalf("expected %d http filters, found %d", 4, len(hf.Values))
		}
		envoyLua := hf.Values[0].GetStructValue().Fields["name"].GetStringValue()
		envoyCors := hf.Values[1].GetStructValue().Fields["name"].GetStringValue()
		if envoyLua != "envoy.lua" || envoyCors != "envoy.cors" {
			t.Fatalf("expected %q %q http filter, found %q %q", "envoy.lua", "envoy.cors", envoyLua, envoyCors)
		}
	}
}

func verifyInboundHTTPListenerCertDetails(t *testing.T, l *xdsapi.Listener) {
	t.Helper()
	if len(l.FilterChains) != 2 {
		t.Fatalf("expected %d filter chains, found %d", 2, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
	}
	f := fc.Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	forwardDetails, expected := cfg.Fields["forward_client_cert_details"].GetStringValue(), "APPEND_FORWARD"
	if forwardDetails != expected {
		t.Fatalf("expected listener to contain forward_client_cert_details %s, found %s", expected, forwardDetails)
	}
	setDetails := cfg.Fields["set_current_client_cert_details"].GetStructValue()
	subject := setDetails.Fields["subject"].GetBoolValue()
	dns := setDetails.Fields["dns"].GetBoolValue()
	uri := setDetails.Fields["uri"].GetBoolValue()
	if !subject || !dns || !uri {
		t.Fatalf("expected listener to contain set_current_client_cert_details (subject: true, dns: true, uri: true), "+
			"found (subject: %t, dns: %t, uri %t)", subject, dns, uri)
	}
}

func verifyInboundHTTPListenerNormalizePath(t *testing.T, l *xdsapi.Listener) {
	t.Helper()
	if len(l.FilterChains) != 2 {
		t.Fatalf("expected 2 filter chains, found %d", len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected 1 filter, found %d", len(fc.Filters))
	}
	f := fc.Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	actual := cfg.Fields["normalize_path"].GetBoolValue()
	if actual != true {
		t.Errorf("expected HTTP listener with normalize_path set to true, found false")
	}
}

func verifyInboundHTTP10(t *testing.T, http10Expected bool, l *xdsapi.Listener) {
	t.Helper()
	for _, fc := range l.FilterChains {
		for _, f := range fc.Filters {
			if f.Name == "envoy.http_connection_manager" {
				cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
				httpProtocolOptionsField := cfg.Fields["http_protocol_options"]
				if http10Expected && httpProtocolOptionsField == nil {
					t.Error("expected http_protocol_options for http_connection_manager, found nil")
					return
				}
				if !http10Expected && httpProtocolOptionsField == nil {
					continue
				}
				httpProtocolOptions := httpProtocolOptionsField.GetStructValue()
				acceptHTTP10Field := httpProtocolOptions.Fields["accept_http_10"]
				if http10Expected && acceptHTTP10Field == nil {
					t.Error("expected http protocol option accept_http_10, found nil")
					return
				}
				if http10Expected && acceptHTTP10Field.GetBoolValue() != http10Expected {
					t.Errorf("expected accepting HTTP 1.0: %v, found: %v", http10Expected, acceptHTTP10Field.GetBoolValue())
				}
			}
		}
	}
}

func getOldestService(services ...*model.Service) *model.Service {
	var oldestService *model.Service
	for _, s := range services {
		if oldestService == nil || s.CreationTime.Before(oldestService.CreationTime) {
			oldestService = s
		}
	}
	return oldestService
}

func buildAllListeners(p plugin.Plugin, sidecarConfig *model.Config, services ...*model.Service) []*xdsapi.Listener {
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnv(services)

	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		return nil
	}

	proxy.ServiceInstances = nil
	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}
	builder := NewListenerBuilder(&proxy)
	return configgen.buildSidecarListeners(&env, &proxy, env.PushContext, builder).getListeners()
}

func getFilterConfig(filter *listener.Filter, out proto.Message) error {
	switch c := filter.ConfigType.(type) {
	case *listener.Filter_Config:
		if err := conversion.StructToMessage(c.Config, out); err != nil {
			return err
		}
	case *listener.Filter_TypedConfig:
		if err := ptypes.UnmarshalAny(c.TypedConfig, out); err != nil {
			return err
		}
	}
	return nil
}

func buildOutboundListeners(p plugin.Plugin, proxy *model.Proxy, sidecarConfig *model.Config,
	virtualService *model.Config, services ...*model.Service) []*xdsapi.Listener {
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	var env model.Environment
	if virtualService != nil {
		env = buildListenerEnvWithVirtualServices(services, []*model.Config{virtualService})
	} else {
		env = buildListenerEnv(services)
	}

	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		return nil
	}

	proxy.IstioVersion = model.ParseIstioVersion(proxy.Metadata.IstioVersion)
	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}
	proxy.ServiceInstances = proxyInstances

	return configgen.buildSidecarOutboundListeners(&env, proxy, env.PushContext)
}

func buildInboundListeners(p plugin.Plugin, proxy *model.Proxy, sidecarConfig *model.Config, services ...*model.Service) []*xdsapi.Listener {
	configgen := NewConfigGenerator([]plugin.Plugin{p})
	env := buildListenerEnv(services)
	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		return nil
	}
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service:  s,
			Endpoint: buildEndpoint(s),
		}
	}

	proxy.IstioVersion = model.ParseIstioVersion(proxy.Metadata.IstioVersion)
	proxy.ServiceInstances = instances
	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}
	return configgen.buildSidecarInboundListeners(&env, proxy, env.PushContext)
}

type fakePlugin struct {
	outboundListenerParams []*plugin.InputParams
}

var _ plugin.Plugin = (*fakePlugin)(nil)

func (p *fakePlugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	p.outboundListenerParams = append(p.outboundListenerParams, in)
	return nil
}

func (p *fakePlugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

func (p *fakePlugin) OnVirtualListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

func (p *fakePlugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

func (p *fakePlugin) OnInboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

func (p *fakePlugin) OnOutboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

func (p *fakePlugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

func (p *fakePlugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return []plugin.FilterChain{{}, {}}
}

func (p *fakePlugin) OnInboundPassthrough(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	switch in.ListenerProtocol {
	case plugin.ListenerProtocolTCP:
		for cnum := range mutable.FilterChains {
			filter := &listener.Filter{
				Name: fakePluginTCPFilter,
			}
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, filter)
		}
	case plugin.ListenerProtocolHTTP:
		for cnum := range mutable.FilterChains {
			filter := &http_filter.HttpFilter{
				Name: fakePluginHTTPFilter,
			}
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
		}
	}
	return nil
}

func isHTTPListener(listener *xdsapi.Listener) bool {
	if listener == nil {
		return false
	}

	for _, fc := range listener.FilterChains {
		if fc.Filters[0].Name == "envoy.http_connection_manager" {
			return true
		}
	}
	return false
}

func isMysqlListener(listener *xdsapi.Listener) bool {
	if len(listener.FilterChains) > 0 && len(listener.FilterChains[0].Filters) > 0 {
		return listener.FilterChains[0].Filters[0].Name == xdsutil.MySQLProxy
	}
	return false
}

func isNodeHTTP10(proxy *model.Proxy) bool {
	return proxy.Metadata.HTTP10 == "1"
}

func findListenerByPort(listeners []*xdsapi.Listener, port uint32) *xdsapi.Listener {
	for _, l := range listeners {
		if port == l.Address.GetSocketAddress().GetPortValue() {
			return l
		}
	}

	return nil
}

func findListenerByAddress(listeners []*xdsapi.Listener, address string) *xdsapi.Listener {
	for _, l := range listeners {
		if address == l.Address.GetSocketAddress().Address {
			return l
		}
	}

	return nil
}

func buildService(hostname string, ip string, protocol protocol.Instance, creationTime time.Time) *model.Service {
	return &model.Service{
		CreationTime: creationTime,
		Hostname:     host.Name(hostname),
		Address:      ip,
		ClusterVIPs:  make(map[string]string),
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     8080,
				Protocol: protocol,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
}

func buildServiceWithPort(hostname string, port int, protocol protocol.Instance, creationTime time.Time) *model.Service {
	return &model.Service{
		CreationTime: creationTime,
		Hostname:     host.Name(hostname),
		Address:      wildcardIP,
		ClusterVIPs:  make(map[string]string),
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     port,
				Protocol: protocol,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
}

func buildEndpoint(service *model.Service) model.NetworkEndpoint {
	return model.NetworkEndpoint{
		ServicePort: service.Ports[0],
		Port:        8080,
	}
}

func buildServiceInstance(service *model.Service, instanceIP string) *model.ServiceInstance {
	return &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: instanceIP,
		},
		Service: service,
	}
}

func buildListenerEnv(services []*model.Service) model.Environment {
	return buildListenerEnvWithVirtualServices(services, nil)
}

func buildListenerEnvWithVirtualServices(services []*model.Service, virtualServices []*model.Config) model.Environment {
	serviceDiscovery := new(fakes.ServiceDiscovery)
	serviceDiscovery.ServicesReturns(services, nil)

	configStore := &fakes.IstioConfigStore{
		EnvoyFilterStub: func(workloadLabels labels.Collection) *model.Config {
			return &model.Config{
				ConfigMeta: model.ConfigMeta{
					Name:      "test-envoyfilter",
					Namespace: "not-default",
				},
				Spec: &networking.EnvoyFilter{
					Filters: []*networking.EnvoyFilter_Filter{
						{
							InsertPosition: &networking.EnvoyFilter_InsertPosition{
								Index: networking.EnvoyFilter_InsertPosition_FIRST,
							},
							FilterType:   networking.EnvoyFilter_Filter_HTTP,
							FilterName:   "envoy.lua",
							FilterConfig: &types.Struct{},
						},
					},
				},
			}
		},
		ListStub: func(typ, namespace string) (configs []model.Config, e error) {
			if typ == "virtual-service" {
				result := make([]model.Config, len(virtualServices))
				for i := range virtualServices {
					result[i] = *virtualServices[i]
				}
				return result, nil
			}
			return nil, nil

		},
	}

	m := mesh.DefaultMeshConfig()
	m.EnableEnvoyAccessLogService = true
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             &m,
	}

	return env
}

func TestAppendListenerFallthroughRoute(t *testing.T) {
	env := &model.Environment{
		Mesh: &meshconfig.MeshConfig{},
	}
	tests := []struct {
		name         string
		listener     *xdsapi.Listener
		listenerOpts *buildListenerOpts
		node         *model.Proxy
		hostname     string
	}{
		{
			name:         "Registry_Only",
			listener:     &xdsapi.Listener{},
			listenerOpts: &buildListenerOpts{},
			node: &model.Proxy{
				ID:       "foo.bar",
				Metadata: &model.NodeMetadata{},
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
					},
				},
			},
			hostname: util.BlackHoleCluster,
		},
		{
			name:         "Allow_Any",
			listener:     &xdsapi.Listener{},
			listenerOpts: &buildListenerOpts{},
			node: &model.Proxy{
				ID:       "foo.bar",
				Metadata: &model.NodeMetadata{},
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
					},
				},
			},
			hostname: util.PassthroughCluster,
		},
	}
	for idx := range tests {
		t.Run(tests[idx].name, func(t *testing.T) {
			appendListenerFallthroughRoute(tests[idx].listener, tests[idx].listenerOpts,
				tests[idx].node, env, nil)
			if len(tests[idx].listenerOpts.filterChainOpts) != 1 {
				t.Errorf("Expected exactly 1 filter chain options")
			}
			if !tests[idx].listenerOpts.filterChainOpts[0].isFallThrough {
				t.Errorf("Expected fall through to be set")
			}
			if len(tests[idx].listenerOpts.filterChainOpts[0].networkFilters) != 1 {
				t.Errorf("Expected exactly 1 network filter in the chain")
			}
			filter := tests[idx].listenerOpts.filterChainOpts[0].networkFilters[0]
			var tcpProxy tcp_proxy.TcpProxy
			cfg := filter.GetTypedConfig()
			ptypes.UnmarshalAny(cfg, &tcpProxy)
			if tcpProxy.StatPrefix != tests[idx].hostname {
				t.Errorf("Expected stat prefix %s but got %s\n", tests[idx].hostname, tcpProxy.StatPrefix)
			}
			if tcpProxy.GetCluster() != tests[idx].hostname {
				t.Errorf("Expected cluster %s but got %s\n", tests[idx].hostname, tcpProxy.GetCluster())
			}
			if len(tests[idx].listener.FilterChains) != 1 {
				t.Errorf("Expected exactly 1 filter chain on the tests[idx].listener")
			}
		})
	}
}
