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
	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	thrift_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/thrift_proxy/v2alpha1"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mixerClient "istio.io/api/mixer/v1/config/client"
	"istio.io/api/networking/v1alpha3"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/mixer/client"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
)

const (
	wildcardIP                     = "0.0.0.0"
	fakePluginHTTPFilter           = "fake-plugin-http-filter"
	fakePluginTCPFilter            = "fake-plugin-tcp-filter"
	fakePluginFilterChainMatchAlpn = "fake-plugin-alpn"
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
	proxy14 = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.4",
		},
		ConfigNamespace: "not-default",
	}
	proxy14HTTP10 = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.4",
			HTTP10:          "1",
		},
		IstioVersion:    &model.IstioVersion{Major: 1, Minor: 3},
		ConfigNamespace: "not-default",
	}
	proxy14Gateway = model.Proxy{
		Type:        model.Router,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.4",
			Labels: map[string]string{
				"istio": "ingressgateway",
			},
		},
		ConfigNamespace: "not-default",
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
			Endpoint: &model.IstioEndpoint{},
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

func TestInboundListenerConfigProxyV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForInbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForInbound.Name) }()

	for _, p := range []*model.Proxy{&proxy14, &proxy14HTTP10} {
		testInboundListenerConfigV14(t, p,
			buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
			buildService("test2.com", wildcardIP, "unknown", tnow),
			buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
		testInboundListenerConfigWithoutServiceV14(t, p)
		testInboundListenerConfigWithSidecarV14(t, p,
			buildService("test.com", wildcardIP, protocol.HTTP, tnow))
		testInboundListenerConfigWithSidecarWithoutServicesV14(t, p)
	}
}

func TestOutboundListenerConflict_HTTPWithCurrentUnknownV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV14(t,
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, "unknown", tnow),
		buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_WellKnowPortsV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV14(t,
		buildServiceWithPort("test1.com", 3306, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test2.com", 3306, protocol.MySQL, tnow))
	testOutboundListenerConflictV14(t,
		buildServiceWithPort("test1.com", 9999, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test2.com", 9999, protocol.MySQL, tnow))
}

func TestOutboundListenerConflict_TCPWithCurrentUnknownV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV14(t,
		buildService("test1.com", wildcardIP, protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, "unknown", tnow),
		buildService("test3.com", wildcardIP, protocol.TCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentTCPV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV14(t,
		buildService("test1.com", wildcardIP, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentHTTPV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictV14(t,
		buildService("test1.com", wildcardIP, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.HTTP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerRouteV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	testOutboundListenerRouteV14(t,
		buildService("test1.com", "1.2.3.4", "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", "2.3.4.5", protocol.HTTP, tnow),
		buildService("test3.com", "3.4.5.6", "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConfig_WithSidecarV14(t *testing.T) {
	_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
	defer func() { _ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name) }()

	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second))}
	service4 := &model.Service{
		CreationTime: tnow.Add(1 * time.Second),
		Hostname:     host.Name("test4.com"),
		Address:      wildcardIP,
		ClusterVIPs:  make(map[string]string),
		Ports: model.PortList{
			&model.Port{
				Name:     "udp",
				Port:     9000,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
	services = append(services, service4)
	service5 := &model.Service{
		CreationTime: tnow.Add(1 * time.Second),
		Hostname:     host.Name("test5.com"),
		Address:      "8.8.8.8",
		ClusterVIPs:  make(map[string]string),
		Ports: model.PortList{
			&model.Port{
				Name:     "MySQL",
				Port:     3306,
				Protocol: protocol.MySQL,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
	services = append(services, service5)
	service6 := &model.Service{
		CreationTime: tnow.Add(1 * time.Second),
		Hostname:     host.Name("test6.com"),
		Address:      "2.2.2.2",
		ClusterVIPs:  make(map[string]string),
		Ports: model.PortList{
			&model.Port{
				Name:     "unknown",
				Port:     8888,
				Protocol: "unknown",
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
	services = append(services, service6)
	testOutboundListenerConfigWithSidecarV14(t, services...)
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
			got := make([]string, 0)
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
					Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
					Version:   collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Version(),
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
	svc := buildServiceWithPort("test.com", 9999, protocol.TCP, tnow)
	svc.Attributes.ServiceRegistry = string(serviceregistry.Kubernetes)
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

			listeners := configgen.buildSidecarOutboundListeners(&proxy, env.PushContext)
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
	service4 := &model.Service{
		CreationTime: tnow.Add(1 * time.Second),
		Hostname:     host.Name("test4.com"),
		Address:      wildcardIP,
		ClusterVIPs:  make(map[string]string),
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     9090,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
	testOutboundListenerConfigWithSidecar(t, services...)
	services = append(services, service4)
	testOutboundListenerConfigWithSidecarWithCaptureModeNone(t, services...)
	testOutboundListenerConfigWithSidecarWithUseRemoteAddress(t, services...)
}

func TestOutboundTlsTrafficWithoutTimeout(t *testing.T) {
	services := []*model.Service{
		{
			CreationTime: tnow,
			Hostname:     host.Name("test.com"),
			Address:      wildcardIP,
			ClusterVIPs:  make(map[string]string),
			Ports: model.PortList{
				&model.Port{
					Name:     "https",
					Port:     8080,
					Protocol: protocol.HTTPS,
				},
			},
			Resolution: model.Passthrough,
			Attributes: model.ServiceAttributes{
				Namespace: "default",
			},
		},
		{
			CreationTime: tnow,
			Hostname:     host.Name("test1.com"),
			Address:      wildcardIP,
			ClusterVIPs:  make(map[string]string),
			Ports: model.PortList{
				&model.Port{
					Name:     "foo",
					Port:     9090,
					Protocol: "unknown",
				},
			},
			Resolution: model.Passthrough,
			Attributes: model.ServiceAttributes{
				Namespace: "default",
			},
		},
	}
	testOutboundListenerFilterTimeoutV14(t, services...)
}

func TestOutboundListenerConfigWithSidecarHTTPProxy(t *testing.T) {
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "sidecar-with-http-proxy",
			Namespace: "default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"default/*"},
					Port: &networking.Port{
						Number:   15080,
						Protocol: "HTTP_PROXY",
						Name:     "15080",
					},
					Bind:        "127.0.0.1",
					CaptureMode: v1alpha3.CaptureMode_NONE,
				},
			},
		},
	}
	services := []*model.Service{buildService("httpbin.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second))}

	listeners := buildOutboundListeners(p, &proxy, sidecarConfig, nil, services...)

	if expected := 1; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}
	l := findListenerByPort(listeners, 15080)
	if l == nil {
		t.Fatalf("expected listener on port %d, but not found", 15080)
	}
	if len(l.FilterChains) != 1 {
		t.Fatalf("expectd %d filter chains, found %d", 1, len(l.FilterChains))
	} else {
		if !isHTTPFilterChain(l.FilterChains[0]) {
			t.Fatalf("expected http filter chain, found %s", l.FilterChains[1].Filters[0].Name)
		}
		if len(l.ListenerFilters) > 0 {
			t.Fatalf("expected %d listener filter, found %d", 0, len(l.ListenerFilters))
		}
	}
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

func testOutboundListenerRouteV14(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, &proxy14, nil, nil, services...)
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

func testOutboundListenerFilterTimeoutV14(t *testing.T, services ...*model.Service) {
	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, &proxy14, nil, nil, services...)
	if len(listeners) != 2 {
		t.Fatalf("expected %d listeners, found %d", 2, len(listeners))
	}

	if listeners[0].ContinueOnListenerFiltersTimeout {
		t.Fatalf("expected timeout disabled, found ContinueOnListenerFiltersTimeout %v",
			listeners[0].ContinueOnListenerFiltersTimeout)
	}

	if !listeners[1].ContinueOnListenerFiltersTimeout || listeners[1].ListenerFiltersTimeout == nil {
		t.Fatalf("expected timeout enabled, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
			listeners[1].ContinueOnListenerFiltersTimeout,
			listeners[1].ListenerFiltersTimeout)
	}
}

func testOutboundListenerConflictV14(t *testing.T, services ...*model.Service) {
	t.Helper()
	oldestService := getOldestService(services...)
	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, &proxy14, nil, nil, services...)
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

		verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[2], model.TrafficDirectionOutbound, false)
		if len(listeners[0].ListenerFilters) != 2 ||
			listeners[0].ListenerFilters[0].Name != "envoy.listener.tls_inspector" ||
			listeners[0].ListenerFilters[1].Name != "envoy.listener.http_inspector" {
			t.Fatalf("expected %d listener filter, found %d", 2, len(listeners[0].ListenerFilters))
		}

		if !listeners[0].ContinueOnListenerFiltersTimeout || listeners[0].ListenerFiltersTimeout == nil {
			t.Fatalf("exptected timeout, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
				listeners[0].ContinueOnListenerFiltersTimeout,
				listeners[0].ListenerFiltersTimeout)
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

		verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[2], model.TrafficDirectionOutbound, false)
		if len(listeners[0].ListenerFilters) != 2 ||
			listeners[0].ListenerFilters[0].Name != "envoy.listener.tls_inspector" ||
			listeners[0].ListenerFilters[1].Name != "envoy.listener.http_inspector" {
			t.Fatalf("expected %d listener filter, found %d", 2, len(listeners[0].ListenerFilters))
		}

		if !listeners[0].ContinueOnListenerFiltersTimeout || listeners[0].ListenerFiltersTimeout == nil {
			t.Fatalf("exptected timeout, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
				listeners[0].ContinueOnListenerFiltersTimeout,
				listeners[0].ListenerFiltersTimeout)
		}
	}
}

func testInboundListenerConfigV14(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, proxy, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	verifyFilterChainMatch(t, listeners[0])
}

func testInboundListenerConfigWithSidecarV14(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
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
	verifyFilterChainMatch(t, listeners[0])
}

func testInboundListenerConfigWithSidecarWithoutServicesV14(t *testing.T, proxy *model.Proxy) {
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
	verifyFilterChainMatch(t, listeners[0])
}

func testInboundListenerConfigWithoutServiceV14(t *testing.T, proxy *model.Proxy) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, proxy, nil)
	if expected := 0; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}
}

func verifyHTTPFilterChainMatch(t *testing.T, fc *listener.FilterChain, direction model.TrafficDirection, isTLS bool) {
	t.Helper()
	if isTLS {
		if direction == model.TrafficDirectionInbound &&
			!reflect.DeepEqual(mtlsHTTPALPNs, fc.FilterChainMatch.ApplicationProtocols) {
			t.Fatalf("expected %d application protocols, %v", len(mtlsHTTPALPNs), mtlsHTTPALPNs)
		}

		if fc.FilterChainMatch.TransportProtocol != "tls" {
			t.Fatalf("exepct %q transport protocol, found %q", "tls", fc.FilterChainMatch.TransportProtocol)
		}
	} else {
		if direction == model.TrafficDirectionInbound &&
			!reflect.DeepEqual(plaintextHTTPALPNs, fc.FilterChainMatch.ApplicationProtocols) {
			t.Fatalf("expected %d application protocols, %v", len(plaintextHTTPALPNs), plaintextHTTPALPNs)
		}

		if fc.FilterChainMatch.TransportProtocol != "" {
			t.Fatalf("exepct %q transport protocol, found %q", "", fc.FilterChainMatch.TransportProtocol)
		}
	}

	if direction == model.TrafficDirectionOutbound &&
		!reflect.DeepEqual(plaintextHTTPALPNs, fc.FilterChainMatch.ApplicationProtocols) {
		t.Fatalf("expected %d application protocols, %v", len(plaintextHTTPALPNs), plaintextHTTPALPNs)
	}

	hcm := &http_filter.HttpConnectionManager{}
	if err := getFilterConfig(fc.Filters[0], hcm); err != nil {
		t.Fatalf("failed to get HCM, config %v", hcm)
	}

	hasAlpn := hasAlpnFilter(hcm.HttpFilters)

	if direction == model.TrafficDirectionInbound && hasAlpn {
		t.Fatal("ALPN filter is unexpected")
	}

	if direction == model.TrafficDirectionOutbound && !hasAlpn {
		t.Fatal("ALPN filter is not found")
	}
}

func hasAlpnFilter(filters []*http_filter.HttpFilter) bool {
	for _, f := range filters {
		if f.Name == AlpnFilterName {
			return true
		}
	}
	return false
}

func isHTTPFilterChain(fc *listener.FilterChain) bool {
	return len(fc.Filters) > 0 && fc.Filters[0].Name == "envoy.http_connection_manager"
}

func isTCPFilterChain(fc *listener.FilterChain) bool {
	return len(fc.Filters) > 0 && fc.Filters[0].Name == "envoy.tcp_proxy"
}

func testOutboundListenerConfigWithSidecarV14(t *testing.T, services ...*model.Service) {
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

	listeners := buildOutboundListeners(p, &proxy14, sidecarConfig, nil, services...)
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

		verifyHTTPFilterChainMatch(t, l.FilterChains[3], model.TrafficDirectionOutbound, false)

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

	verifyHTTPFilterChainMatch(t, l.FilterChains[1], model.TrafficDirectionOutbound, false)
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
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	if l := findListenerByPort(listeners, 8080); isHTTPListener(l) {
		t.Fatalf("expected TCP listener on port 8080, found HTTP: %v", l)
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
						Number:   9090,
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

	if l := findListenerByPort(listeners, 9090); !isHTTPListener(l) {
		t.Fatalf("expected HTTP listener on port 9090, found TCP\n%v", l)
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

	if l := findListenerByPort(listeners, 9090); !isHTTPListener(l) {
		t.Fatalf("expected HTTP listener on port 9090, but not found\n%v", l)
	} else {
		f := l.FilterChains[0].Filters[0]
		cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
		if useRemoteAddress, exists := cfg.Fields["use_remote_address"]; exists {
			if exists && useRemoteAddress.GetBoolValue() {
				t.Fatalf("expected useRemoteAddress false, found true %v", l)
			}
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

func TestHttpProxyListener(t *testing.T) {
	p := &fakePlugin{}
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnv(nil)
	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		t.Fatalf("error in initializing push context: %s", err)
	}

	proxy.ServiceInstances = nil
	env.Mesh().ProxyHttpPort = 15007
	proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	httpProxy := configgen.buildHTTPProxy(&proxy, env.PushContext)
	f := httpProxy.FilterChains[0].Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())

	if httpProxy.Address.GetSocketAddress().GetPortValue() != 15007 {
		t.Fatalf("expected http proxy is not listening on %d, but on port %d", env.Mesh().ProxyHttpPort,
			httpProxy.Address.GetSocketAddress().GetPortValue())
	}
	if !strings.HasPrefix(cfg.Fields["stat_prefix"].GetStringValue(), "outbound_") {
		t.Fatalf("expected http proxy stat prefix to have outbound, %s", cfg.Fields["stat_prefix"].GetStringValue())
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

func verifyFilterChainMatch(t *testing.T, listener *xdsapi.Listener) {
	if len(listener.FilterChains) != 5 ||
		!isHTTPFilterChain(listener.FilterChains[0]) ||
		!isHTTPFilterChain(listener.FilterChains[1]) ||
		!isTCPFilterChain(listener.FilterChains[2]) ||
		!isTCPFilterChain(listener.FilterChains[3]) ||
		!isTCPFilterChain(listener.FilterChains[4]) {
		t.Fatalf("expectd %d filter chains, %d http filter chains and %d tcp filter chain", 5, 2, 3)
	}

	verifyHTTPFilterChainMatch(t, listener.FilterChains[0], model.TrafficDirectionInbound, true)
	verifyHTTPFilterChainMatch(t, listener.FilterChains[1], model.TrafficDirectionInbound, false)
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
	return configgen.buildSidecarListeners(&proxy, env.PushContext, builder).getListeners()
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

	return configgen.buildSidecarOutboundListeners(proxy, env.PushContext)
}

func buildInboundListeners(p plugin.Plugin, proxy *model.Proxy, sidecarConfig *model.Config, services ...*model.Service) []*xdsapi.Listener {
	configgen := NewConfigGenerator([]plugin.Plugin{p})
	env := buildListenerEnv(services)
	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		return nil
	}
	if err := proxy.SetServiceInstances(&env); err != nil {
		return nil
	}

	proxy.IstioVersion = model.ParseIstioVersion(proxy.Metadata.IstioVersion)
	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}
	return configgen.buildSidecarInboundListeners(proxy, env.PushContext)
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
	return []plugin.FilterChain{
		{
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name: xdsutil.TlsInspector,
				},
			},
		},
		{},
	}
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

func (p *fakePlugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return []plugin.FilterChain{
		// A filter chain configured by the plugin for mutual TLS support.
		{
			FilterChainMatch: &listener.FilterChainMatch{
				ApplicationProtocols: []string{fakePluginFilterChainMatchAlpn},
			},
			TLSContext: &auth.DownstreamTlsContext{},
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name: xdsutil.TlsInspector,
				},
			},
		},
		// An empty filter chain for the default pass through behavior.
		{},
	}
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

func buildServiceInstance(service *model.Service, instanceIP string) *model.ServiceInstance {
	return &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
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
	serviceDiscovery.GetProxyServiceInstancesReturns(instances, nil)

	envoyFilter := model.Config{
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
	configStore := &fakes.IstioConfigStore{
		ListStub: func(kind resource.GroupVersionKind, namespace string) (configs []model.Config, e error) {
			switch kind {
			case collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind():
				result := make([]model.Config, len(virtualServices))
				for i := range virtualServices {
					result[i] = *virtualServices[i]
				}
				return result, nil
			case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
				return []model.Config{envoyFilter}, nil
			default:
				return nil, nil
			}
		},
	}

	m := mesh.DefaultMeshConfig()
	m.EnableEnvoyAccessLogService = true
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Watcher:          mesh.NewFixedWatcher(&m),
	}

	return env
}

func TestAppendListenerFallthroughRoute(t *testing.T) {
	push := &model.PushContext{
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
			name:     "Registry_Only",
			listener: &xdsapi.Listener{},
			listenerOpts: &buildListenerOpts{
				push: push,
			},
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
			name:     "Allow_Any",
			listener: &xdsapi.Listener{},
			listenerOpts: &buildListenerOpts{
				push: push,
			},
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
				tests[idx].node, nil)
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
			_ = ptypes.UnmarshalAny(cfg, &tcpProxy)
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

func TestMergeTCPFilterChains(t *testing.T) {
	push := &model.PushContext{
		Mesh:        &meshconfig.MeshConfig{},
		ProxyStatus: map[string]map[string]model.ProxyPushStatus{},
	}

	node := &model.Proxy{
		ID:       "foo.bar",
		Metadata: &model.NodeMetadata{},
		SidecarScope: &model.SidecarScope{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
	}

	tcpProxy := &tcp_proxy.TcpProxy{
		StatPrefix:       "outbound|443||foo.com",
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: "outbound|443||foo.com"},
	}

	tcpProxyFilter := &listener.Filter{
		Name:       xdsutil.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(tcpProxy)},
	}

	tcpProxy = &tcp_proxy.TcpProxy{
		StatPrefix:       "outbound|443||bar.com",
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: "outbound|443||bar.com"},
	}

	tcpProxyFilter2 := &listener.Filter{
		Name:       xdsutil.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(tcpProxy)},
	}

	svcPort := &model.Port{
		Name:     "https",
		Port:     443,
		Protocol: protocol.HTTPS,
	}
	var l xdsapi.Listener
	filterChains := []*listener.FilterChain{
		{
			FilterChainMatch: &listener.FilterChainMatch{
				PrefixRanges: []*core.CidrRange{
					{
						AddressPrefix: "10.244.0.18",
						PrefixLen:     &wrappers.UInt32Value{Value: 32},
					},
					{
						AddressPrefix: "fe80::1c97:c3ff:fed7:5940",
						PrefixLen:     &wrappers.UInt32Value{Value: 128},
					},
				},
			},
			Filters: nil, // This is not a valid config, just for test
		},
		{
			FilterChainMatch: &listener.FilterChainMatch{
				ServerNames: []string{"foo.com"},
			},
			// This is not a valid config, just for test
			Filters: []*listener.Filter{tcpProxyFilter},
		},
		{
			FilterChainMatch: &listener.FilterChainMatch{},
			// This is not a valid config, just for test
			Filters: buildFallthroughNetworkFilters(push, node),
		},
	}
	l.FilterChains = filterChains
	listenerMap := map[string]*outboundListenerEntry{
		"0.0.0.0_443": {
			servicePort: svcPort,
			services: []*model.Service{{
				CreationTime: tnow,
				Hostname:     host.Name("foo.com"),
				Address:      "192.168.1.1",
				Ports:        []*model.Port{svcPort},
				Resolution:   model.DNSLB,
			}},
			listener: &l,
		},
	}

	insertFallthroughMetadata(listenerMap["0.0.0.0_443"].listener.FilterChains[2])

	incomingFilterChains := []*listener.FilterChain{
		{
			FilterChainMatch: &listener.FilterChainMatch{},
			// This is not a valid config, just for test
			Filters: []*listener.Filter{tcpProxyFilter2},
		},
	}

	svc := model.Service{
		Hostname: "bar.com",
	}

	params := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolTCP,
		Node:             node,
		Port:             svcPort,
		ServiceInstance:  &model.ServiceInstance{Service: &svc},
		Push:             push,
	}

	out := mergeTCPFilterChains(incomingFilterChains, params, "0.0.0.0_443", listenerMap, node)

	if len(out) != 3 {
		t.Errorf("Got %d filter chains, expected 3", len(out))
	}
	if !isMatchAllFilterChain(out[2]) {
		t.Errorf("The last filter chain  %#v is not wildcard matching", out[2])
	}

	if !reflect.DeepEqual(out[2].Filters, incomingFilterChains[0].Filters) {
		t.Errorf("got %v\nwant %v\ndiff %v", out[2].Filters, incomingFilterChains[0].Filters, cmp.Diff(out[2].Filters, incomingFilterChains[0].Filters))
	}
}

func TestOutboundRateLimitedThriftListenerConfig(t *testing.T) {
	svcName := "thrift-service-unlimited"
	svcIP := "127.0.22.2"
	limitedSvcName := "thrift-service"
	limitedSvcIP := "127.0.22.3"
	if err := os.Setenv("PILOT_ENABLE_THRIFT_FILTER", "true"); err != nil {
		t.Error(err.Error())
	}
	defer func() {
		_ = os.Unsetenv(features.EnableThriftFilter.Name)
	}()
	services := []*model.Service{
		buildService(svcName+".default.svc.cluster.local", svcIP, protocol.Thrift, tnow),
		buildService(limitedSvcName+".default.svc.cluster.local", limitedSvcIP, protocol.Thrift, tnow)}

	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					// None
					CaptureMode: networking.CaptureMode_NONE,
					Hosts:       []string{"*/*"},
				},
			},
		},
	}

	configgen := NewConfigGenerator([]plugin.Plugin{p})

	serviceDiscovery := new(fakes.ServiceDiscovery)
	serviceDiscovery.ServicesReturns(services, nil)

	quotaSpec := &client.Quota{
		Quota:  "test",
		Charge: 1,
	}

	configStore := &fakes.IstioConfigStore{
		ListStub: func(kind resource.GroupVersionKind, s string) (configs []model.Config, err error) {
			if kind.String() == collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind().String() {
				return []model.Config{
					{
						ConfigMeta: model.ConfigMeta{
							Type:      collections.IstioMixerV1ConfigClientQuotaspecs.Resource().Kind(),
							Version:   collections.IstioMixerV1ConfigClientQuotaspecs.Resource().Version(),
							Name:      limitedSvcName,
							Namespace: "default",
						},
						Spec: quotaSpec,
					},
				}, nil
			} else if kind.String() == collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind().String() {
				return []model.Config{
					{
						ConfigMeta: model.ConfigMeta{
							Type:      collections.IstioMixerV1ConfigClientQuotaspecs.Resource().Kind(),
							Version:   collections.IstioMixerV1ConfigClientQuotaspecs.Resource().Version(),
							Name:      limitedSvcName,
							Namespace: "default",
						},
						Spec: &mixerClient.QuotaSpecBinding{
							Services: []*mixerClient.IstioService{
								{
									Name:      "thrift-service",
									Namespace: "default",
									Domain:    "cluster.local",
									Service:   "thrift-service.default.svc.cluster.local",
								},
							},
							QuotaSpecs: []*mixerClient.QuotaSpecBinding_QuotaSpecReference{
								{
									Name:      "thrift-service",
									Namespace: "default",
								},
							},
						},
					},
				}, nil
			}
			return []model.Config{}, nil
		},
	}

	m := mesh.DefaultMeshConfig()
	m.ThriftConfig.RateLimitUrl = "ratelimit.svc.cluster.local"
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Watcher:          mesh.NewFixedWatcher(&m),
	}

	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		t.Error(err.Error())
	}

	proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	proxy.ServiceInstances = proxyInstances

	listeners := configgen.buildSidecarOutboundListeners(&proxy, env.PushContext)

	var thriftProxy thrift_proxy.ThriftProxy
	thriftListener := findListenerByAddress(listeners, svcIP)
	chains := thriftListener.GetFilterChains()
	filters := chains[len(chains)-1].Filters
	err := ptypes.UnmarshalAny(filters[len(filters)-1].GetTypedConfig(), &thriftProxy)
	if err != nil {
		t.Error(err.Error())
	}
	if len(thriftProxy.ThriftFilters) > 0 {
		t.Fatal("No thrift filters should have been applied")
	}
	thriftListener = findListenerByAddress(listeners, limitedSvcIP)
	chains = thriftListener.GetFilterChains()
	filters = chains[len(chains)-1].Filters
	err = ptypes.UnmarshalAny(filters[len(filters)-1].GetTypedConfig(), &thriftProxy)
	if err != nil {
		t.Error(err.Error())
	}
	if len(thriftProxy.ThriftFilters) == 0 {
		t.Fatal("Thrift rate limit filter should have been applied")
	}
	var rateLimitApplied bool
	for _, filter := range thriftProxy.ThriftFilters {
		if filter.Name == "envoy.filters.thrift.rate_limit" {
			rateLimitApplied = true
			break
		}
	}
	if !rateLimitApplied {
		t.Error("No rate limit applied when one should have been")
	}
}
