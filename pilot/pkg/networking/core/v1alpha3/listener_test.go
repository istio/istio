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
	"strconv"
	"strings"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	tracing "github.com/envoyproxy/go-control-plane/envoy/type/tracing/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/listenertest"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	wildcardIP = "0.0.0.0"
)

func getProxy() *model.Proxy {
	pr := &model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
		},
		ConfigNamespace: "not-default",
	}
	pr.DiscoverIPMode()
	return pr
}

var (
	tnow        = time.Now()
	proxyHTTP10 = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
			HTTP10:    "1",
		},
		ConfigNamespace: "not-default",
	}
	proxyGateway = model.Proxy{
		Type:            model.Router,
		IPAddresses:     []string{"1.1.1.1"},
		ID:              "v0.default",
		DNSDomain:       "default.example.org",
		Labels:          proxyGatewayMetadata.Labels,
		Metadata:        &proxyGatewayMetadata,
		ConfigNamespace: "not-default",
	}
	proxyGatewayMetadata = model.NodeMetadata{
		Namespace: "not-default",
		Labels: map[string]string{
			"istio": "ingressgateway",
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

func TestInboundListenerConfig(t *testing.T) {
	for _, p := range []*model.Proxy{getProxy(), &proxyHTTP10} {
		t.Run("multiple services", func(t *testing.T) {
			testInboundListenerConfig(t, p,
				buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
				buildService("test2.com", wildcardIP, "unknown", tnow),
				buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
		})
		t.Run("no service", func(t *testing.T) {
			testInboundListenerConfigWithoutService(t, p)
		})
		t.Run("sidecar", func(t *testing.T) {
			testInboundListenerConfigWithSidecar(t, p,
				buildService("test.com", wildcardIP, protocol.HTTP, tnow))
		})
		t.Run("sidecar with service", func(t *testing.T) {
			testInboundListenerConfigWithSidecarWithoutServices(t, p)
		})
	}

	t.Run("grpc", func(t *testing.T) {
		testInboundListenerConfigWithGrpc(t, getProxy(),
			buildService("test1.com", wildcardIP, protocol.GRPC, tnow.Add(1*time.Second)))
	})
}

func TestOutboundListenerConflict_HTTPWithCurrentUnknown(t *testing.T) {
	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, true)

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, "unknown", tnow),
		buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_WellKnowPorts(t *testing.T) {
	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, true)

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildServiceWithPort("test1.com", 3306, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test2.com", 3306, protocol.MySQL, tnow))
	testOutboundListenerConflict(t,
		buildServiceWithPort("test1.com", 9999, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test2.com", 9999, protocol.MySQL, tnow))
}

func TestOutboundListenerConflict_TCPWithCurrentUnknown(t *testing.T) {
	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, true)

	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, "unknown", tnow),
		buildService("test3.com", wildcardIP, protocol.TCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentTCP(t *testing.T) {
	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, true)

	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentHTTP(t *testing.T) {
	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, true)

	// The oldest service port is Auto.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.HTTP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerRoute(t *testing.T) {
	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, true)

	testOutboundListenerRoute(t,
		buildService("test1.com", "1.2.3.4", "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", "2.3.4.5", protocol.HTTP, tnow),
		buildService("test3.com", "3.4.5.6", "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConfig_WithSidecar(t *testing.T) {
	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, "unknown", tnow.Add(2*time.Second)),
	}
	service4 := &model.Service{
		CreationTime:   tnow.Add(1 * time.Second),
		Hostname:       host.Name("test4.com"),
		DefaultAddress: wildcardIP,
		Ports: model.PortList{
			&model.Port{
				Name:     "udp",
				Port:     9000,
				Protocol: protocol.GRPC,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
	services = append(services, service4)
	service5 := &model.Service{
		CreationTime:   tnow.Add(1 * time.Second),
		Hostname:       host.Name("test5.com"),
		DefaultAddress: "8.8.8.8",
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
		CreationTime:   tnow.Add(1 * time.Second),
		Hostname:       host.Name("test6.com"),
		DefaultAddress: "2.2.2.2",
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
	testOutboundListenerConfigWithSidecar(t, services...)
}

func TestOutboundListenerConflict_HTTPWithCurrentTCP(t *testing.T) {
	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictWithSniffingDisabled(t,
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_TCPWithCurrentHTTP(t *testing.T) {
	// The oldest service port is HTTP.  We should encounter conflicts when attempting to add the TCP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflictWithSniffingDisabled(t,
		buildService("test1.com", wildcardIP, protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.HTTP, tnow),
		buildService("test3.com", wildcardIP, protocol.TCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict(t *testing.T) {
	run := func(t *testing.T, s []*model.Service) {
		proxy := getProxy()
		proxy.DiscoverIPMode()
		listeners := buildOutboundListeners(t, getProxy(), nil, nil, s...)
		if len(listeners) != 1 {
			t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
		}
	}
	// Iterate over all protocol pairs and generate listeners
	// ValidateListeners will be called on all of them ensuring they are valid
	protos := []protocol.Instance{protocol.TCP, protocol.TLS, protocol.HTTP, protocol.Unsupported}
	for _, older := range protos {
		for _, newer := range protos {
			t.Run(fmt.Sprintf("%v then %v", older, newer), func(t *testing.T) {
				run(t, []*model.Service{
					buildService("test1.com", wildcardIP, older, tnow.Add(-1*time.Second)),
					buildService("test2.com", wildcardIP, newer, tnow),
				})
			})
		}
	}
}

func TestOutboundListenerConflict_TCPWithCurrentTCP(t *testing.T) {
	services := []*model.Service{
		buildService("test1.com", "1.2.3.4", protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", "1.2.3.4", protocol.TCP, tnow),
		buildService("test3.com", "1.2.3.4", protocol.TCP, tnow.Add(2*time.Second)),
	}
	listeners := buildOutboundListeners(t, getProxy(), nil, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	// The filter chains should all be merged into one.
	if len(listeners[0].FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(listeners[0].FilterChains))
	}

	oldestService := getOldestService(services...)
	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != protocol.HTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == protocol.HTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}

	// Validate that listener conflict preserves the listener of oldest service.
	verifyOutboundTCPListenerHostname(t, listeners[0], oldestService.Hostname)
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
			services := []*model.Service{
				buildService("test.com", tt.CIDR, protocol.TCP, tnow),
			}

			virtualService := config.Config{
				Meta: config.Meta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
					Name:             "test_vs",
					Namespace:        "default",
				},
				Spec: virtualServiceSpec,
			}
			listeners := buildOutboundListeners(t, getProxy(), nil, &virtualService, services...)

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

			if listeners[0].ConnectionBalanceConfig != nil {
				t.Fatalf("expected connection balance config to be set to empty, found %v", listeners[0].ConnectionBalanceConfig)
			}

			for _, l := range listeners {
				for _, fc := range l.GetFilterChains() {
					listenertest.VerifyFilterChain(t, fc, listenertest.FilterChainTest{
						NetworkFilters: []string{wellknown.TCPProxy},
						TotalMatch:     true,
					})
				}
			}
		})
	}
}

func TestOutboundListenerTCPWithVSExactBalance(t *testing.T) {
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
			services := []*model.Service{
				buildService("test.com", tt.CIDR, protocol.TCP, tnow),
			}

			virtualService := config.Config{
				Meta: config.Meta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
					Name:             "test_vs",
					Namespace:        "default",
				},
				Spec: virtualServiceSpec,
			}
			proxy := getProxy()
			proxy.Metadata.InboundListenerExactBalance = true
			proxy.Metadata.OutboundListenerExactBalance = true
			listeners := buildOutboundListeners(t, proxy, nil, &virtualService, services...)

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

			if listeners[0].ConnectionBalanceConfig == nil || listeners[0].ConnectionBalanceConfig.GetExactBalance() == nil {
				t.Fatalf("expected connection balance config to be set to exact_balance, found %v", listeners[0].ConnectionBalanceConfig)
			}
		})
	}
}

func TestOutboundListenerForHeadlessServices(t *testing.T) {
	svc := buildServiceWithPort("test.com", 9999, protocol.TCP, tnow)
	svc.Resolution = model.Passthrough
	svc.Attributes.ServiceRegistry = provider.Kubernetes
	services := []*model.Service{svc}

	autoSvc := buildServiceWithPort("test.com", 9999, protocol.Unsupported, tnow)
	autoSvc.Resolution = model.Passthrough
	autoSvc.Attributes.ServiceRegistry = provider.Kubernetes

	extSvc := buildServiceWithPort("example1.com", 9999, protocol.TCP, tnow)
	extSvc.Resolution = model.Passthrough
	extSvc.Attributes.ServiceRegistry = provider.External

	extSvcSelector := buildServiceWithPort("example2.com", 9999, protocol.TCP, tnow)
	extSvcSelector.Resolution = model.Passthrough
	extSvcSelector.Attributes.ServiceRegistry = provider.External
	extSvcSelector.Attributes.LabelSelectors = map[string]string{"foo": "bar"}

	tests := []struct {
		name                      string
		instances                 []*model.ServiceInstance
		services                  []*model.Service
		numListenersOnServicePort int
	}{
		{
			name: "gen a listener per IP instance",
			instances: []*model.ServiceInstance{
				// This instance is the proxy itself, will not gen a outbound listener for it.
				buildServiceInstance(services[0], "1.1.1.1"),
				buildServiceInstance(services[0], "10.10.10.10"),
				buildServiceInstance(services[0], "11.11.11.11"),
				buildServiceInstance(services[0], "12.11.11.11"),
			},
			services:                  []*model.Service{svc},
			numListenersOnServicePort: 3,
		},
		{
			name:                      "no listeners for empty services",
			instances:                 []*model.ServiceInstance{},
			services:                  []*model.Service{svc},
			numListenersOnServicePort: 0,
		},
		{
			name: "no listeners for DNS instance",
			instances: []*model.ServiceInstance{
				buildServiceInstance([]*model.Service{svc}[0], "example.com"),
			},
			services:                  services,
			numListenersOnServicePort: 0,
		},
		{
			name:                      "external service",
			instances:                 []*model.ServiceInstance{},
			services:                  []*model.Service{extSvc},
			numListenersOnServicePort: 1,
		},
		{
			name:                      "external service with selector",
			instances:                 []*model.ServiceInstance{},
			services:                  []*model.Service{extSvcSelector},
			numListenersOnServicePort: 0,
		},
		{
			name: "external service with selector and endpoints",
			instances: []*model.ServiceInstance{
				buildServiceInstance(extSvcSelector, "10.10.10.10"),
				buildServiceInstance(extSvcSelector, "11.11.11.11"),
			},
			services:                  []*model.Service{extSvcSelector},
			numListenersOnServicePort: 2,
		},
		{
			name:                      "no listeners for empty Kubernetes auto protocol",
			instances:                 []*model.ServiceInstance{},
			services:                  []*model.Service{autoSvc},
			numListenersOnServicePort: 0,
		},
		{
			name: "listeners per instance for Kubernetes auto protocol",
			instances: []*model.ServiceInstance{
				buildServiceInstance(autoSvc, "10.10.10.10"),
				buildServiceInstance(autoSvc, "11.11.11.11"),
			},
			services:                  []*model.Service{autoSvc},
			numListenersOnServicePort: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{
				Services:  tt.services,
				Instances: tt.instances,
			})

			proxy := cg.SetupProxy(nil)
			proxy.Metadata.InboundListenerExactBalance = true
			proxy.Metadata.OutboundListenerExactBalance = true

			listeners := NewListenerBuilder(proxy, cg.env.PushContext).buildSidecarOutboundListeners(proxy, cg.env.PushContext)
			listenersToCheck := make([]string, 0)
			for _, l := range listeners {
				if l.Address.GetSocketAddress().GetPortValue() == 9999 {
					listenersToCheck = append(listenersToCheck, l.Name)
				}

				if l.ConnectionBalanceConfig == nil || l.ConnectionBalanceConfig.GetExactBalance() == nil {
					t.Fatalf("expected connection balance config to be set to exact_balance, found %v", listeners[0].ConnectionBalanceConfig)
				}
			}

			if len(listenersToCheck) != tt.numListenersOnServicePort {
				t.Errorf("Expected %d listeners on service port 9999, got %d (%v)", tt.numListenersOnServicePort, len(listenersToCheck), listenersToCheck)
			}
		})
	}
}

func TestInboundHTTPListenerConfig(t *testing.T) {
	sidecarConfig := config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
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
	svc := buildService("test.com", wildcardIP, protocol.HTTP, tnow)
	for _, p := range []*model.Proxy{getProxy(), &proxyHTTP10} {
		cases := []struct {
			name     string
			p        *model.Proxy
			cfg      []config.Config
			services []*model.Service
		}{
			{
				name:     "simple",
				p:        p,
				services: []*model.Service{svc},
			},
			{
				name:     "sidecar with service",
				p:        p,
				services: []*model.Service{svc},
				cfg:      []config.Config{sidecarConfig},
			},
			{
				name:     "sidecar",
				p:        p,
				services: []*model.Service{svc},
				cfg:      []config.Config{sidecarConfig},
			},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				t.Helper()
				listeners := buildListeners(t, TestOptions{
					Services: tt.services,
					Configs:  tt.cfg,
				}, tt.p)
				l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
				listenertest.VerifyListener(t, l, listenertest.ListenerTest{
					FilterChains: []listenertest.FilterChainTest{
						{
							TotalMatch:  true,
							Port:        8080,
							HTTPFilters: []string{xdsfilters.MxFilterName, xdsfilters.Fault.Name, xdsfilters.Cors.Name, xdsfilters.Router.Name},
							ValidateHCM: func(t test.Failer, hcm *hcm.HttpConnectionManager) {
								assert.Equal(t, "istio-envoy", hcm.GetServerName(), "server name")
								if len(tt.cfg) == 0 {
									assert.Equal(t, "inbound_0.0.0.0_8080", hcm.GetStatPrefix(), "stat prefix")
								} else {
									// Sidecar impacts stat prefix
									assert.Equal(t, "inbound_1.1.1.1_8080", hcm.GetStatPrefix(), "stat prefix")
								}
								assert.Equal(t, "APPEND_FORWARD", hcm.GetForwardClientCertDetails().String(), "forward client cert details")
								assert.Equal(t, true, hcm.GetSetCurrentClientCertDetails().GetSubject().GetValue(), "subject")
								assert.Equal(t, true, hcm.GetSetCurrentClientCertDetails().GetDns(), "dns")
								assert.Equal(t, true, hcm.GetSetCurrentClientCertDetails().GetUri(), "uri")
								assert.Equal(t, true, hcm.GetNormalizePath().GetValue(), "normalize path")
								assert.Equal(t, enableHTTP10(tt.p.Metadata.HTTP10), hcm.GetHttpProtocolOptions().GetAcceptHttp_10(), "http/1.0")
							},
						},
					},
				})
			})
		}
	}
}

func TestOutboundListenerConfig_WithDisabledSniffing_WithSidecar(t *testing.T) {
	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, false)

	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, protocol.TCP, tnow),
		buildService("test3.com", wildcardIP, protocol.HTTP, tnow.Add(2*time.Second)),
	}
	service4 := &model.Service{
		CreationTime:   tnow.Add(1 * time.Second),
		Hostname:       host.Name("test4.com"),
		DefaultAddress: wildcardIP,
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
	testOutboundListenerConfigWithSidecarWithSniffingDisabled(t, services...)
	services = append(services, service4)
	testOutboundListenerConfigWithSidecarWithCaptureModeNone(t, services...)
	testOutboundListenerConfigWithSidecarWithUseRemoteAddress(t, services...)
}

func TestOutboundTlsTrafficWithoutTimeout(t *testing.T) {
	services := []*model.Service{
		{
			CreationTime:   tnow,
			Hostname:       host.Name("test.com"),
			DefaultAddress: wildcardIP,
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
			CreationTime:   tnow,
			Hostname:       host.Name("test1.com"),
			DefaultAddress: wildcardIP,
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
	testOutboundListenerFilterTimeout(t, services...)
}

func TestOutboundTls(t *testing.T) {
	services := []*model.Service{
		{
			CreationTime:   tnow,
			Hostname:       host.Name("test.com"),
			DefaultAddress: wildcardIP,
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
	}
	virtualService := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "test",
			Namespace:        "default",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"test.com"},
			Gateways: []string{"mesh"},
			Tls: []*networking.TLSRoute{
				{
					Match: []*networking.TLSMatchAttributes{
						{
							DestinationSubnets: []string{"10.10.0.0/24", "11.10.0.0/24"},
							Port:               8080,
							SniHosts:           []string{"a", "b", "c"},
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
		},
	}
	virtualService2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "test2",
			Namespace:        "default",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"test.com"},
			Gateways: []string{"mesh"},
			Tls: []*networking.TLSRoute{
				{
					Match: []*networking.TLSMatchAttributes{
						{
							DestinationSubnets: []string{"12.10.0.0/24", "13.10.0.0/24"},
							Port:               8080,
							SniHosts:           []string{"e", "f", "g"},
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
		},
	}
	buildOutboundListeners(t, getProxy(), &virtualService2, &virtualService, services...)
}

func TestOutboundListenerConfigWithSidecarHTTPProxy(t *testing.T) {
	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "sidecar-with-http-proxy",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
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
					CaptureMode: networking.CaptureMode_NONE,
				},
			},
		},
	}
	services := []*model.Service{buildService("httpbin.com", wildcardIP, protocol.HTTP, tnow.Add(1*time.Second))}

	listeners := buildOutboundListeners(t, getProxy(), sidecarConfig, nil, services...)

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
		tt.proxy.DiscoverIPMode()
		wm, lh := getActualWildcardAndLocalHost(tt.proxy)
		if wm != tt.expected[0] && lh != tt.expected[1] {
			t.Errorf("Test %s failed, expected: %s / %s got: %s / %s", tt.name, tt.expected[0], tt.expected[1], wm, lh)
		}
	}
}

// Test to catch new fields in FilterChainMatch message.
func TestFilterChainMatchFields(t *testing.T) {
	fcm := listener.FilterChainMatch{}
	e := reflect.ValueOf(&fcm).Elem()
	// If this fails, that means new fields have been added to FilterChainMatch, filterChainMatchEqual function needs to be updated.
	if e.NumField() != 14 {
		t.Fatalf("Expected 14 fields, got %v. This means we need to update filterChainMatchEqual implementation", e.NumField())
	}
}

func TestInboundListener_PrivilegedPorts(t *testing.T) {
	// Verify that an explicit ingress listener will not bind to privileged ports
	// if proxy is not using Iptables and cannot bind to privileged ports (1-1023).
	//
	// Even if a user explicitly created a Sidecar config with an ingress listener for a privileged port,
	// it is still not worth it creating such a listener if we already known that a proxy will end up
	// rejecting it.
	testPrivilegedPorts(t, func(t *testing.T, proxy *model.Proxy, port uint32) []*listener.Listener {
		// simulate user-defined Sidecar config with an ingress listener for a given port
		sidecarConfig := config.Config{
			Meta: config.Meta{
				Name:             "sidecar-with-ingress-listener",
				Namespace:        proxy.ConfigNamespace,
				GroupVersionKind: gvk.Sidecar,
			},
			Spec: &networking.Sidecar{
				Ingress: []*networking.IstioIngressListener{
					{
						Port: &networking.Port{
							Number:   port,
							Protocol: "HTTP",
							Name:     strconv.Itoa(int(port)),
						},
						DefaultEndpoint: "127.0.0.1:8080",
					},
				},
			},
		}
		return buildListeners(t, TestOptions{
			Configs: []config.Config{sidecarConfig},
		}, proxy)
	})
}

func TestOutboundListener_PrivilegedPorts(t *testing.T) {
	// Verify that an implicit catch all egress listener will not bind to privileged ports
	// if proxy is not using Iptables and cannot bind to privileged ports (1-1023).
	//
	// It is very common for the catch all egress listener to match services on ports 80 and 443.
	// Therefore, the default behavior should not force users to start from looking for a workaround.
	t.Run("implicit catch all egress listener", func(t *testing.T) {
		testPrivilegedPorts(t, func(t *testing.T, proxy *model.Proxy, port uint32) []*listener.Listener {
			return buildListeners(t, TestOptions{
				Services: []*model.Service{buildServiceWithPort("httpbin.com", int(port), protocol.HTTP, tnow)},
			}, proxy)
		})
	})

	// Verify that an explicit per-port egress listener will not bind to privileged ports
	// if proxy is not using Iptables and cannot bind to privileged ports (1-1023).
	//
	// Even if a user explicitly created a Sidecar config with an egress listener for a privileged port,
	// it is still not worth it creating such a listener if we already known that a proxy will end up
	// rejecting it.
	t.Run("explicit per-port egress listener", func(t *testing.T) {
		testPrivilegedPorts(t, func(t *testing.T, proxy *model.Proxy, port uint32) []*listener.Listener {
			// simulate user-defined Sidecar config with an egress listener for a given port
			sidecarConfig := config.Config{
				Meta: config.Meta{
					Name:             "sidecar-with-per-port-egress-listener",
					Namespace:        proxy.ConfigNamespace,
					GroupVersionKind: gvk.Sidecar,
				},
				Spec: &networking.Sidecar{
					Egress: []*networking.IstioEgressListener{
						{
							Hosts: []string{"default/*"},
							Port: &networking.Port{
								Number:   port,
								Protocol: "HTTP",
								Name:     strconv.Itoa(int(port)),
							},
						},
					},
				},
			}
			return buildListeners(t, TestOptions{
				Services: []*model.Service{buildServiceWithPort("httpbin.com", int(port), protocol.HTTP, tnow)},
				Configs:  []config.Config{sidecarConfig},
			}, proxy)
		})
	})
}

func testPrivilegedPorts(t *testing.T, buildListeners func(t *testing.T, proxy *model.Proxy, port uint32) []*listener.Listener) {
	privilegedPorts := []uint32{1, 80, 443, 1023}
	unprivilegedPorts := []uint32{1024, 8080, 8443, 15443}
	anyPorts := append(privilegedPorts, unprivilegedPorts...)

	// multiple test cases to ensure that privileged ports get treated differently
	// only under certain conditions, namely
	// 1) proxy explicitly indicated it is not using Iptables
	// 2) proxy explicitly indicated it is not a privileged process (cannot bind to  1-1023)
	cases := []struct {
		name           string
		unprivileged   bool // whether proxy is unprivileged
		mode           model.TrafficInterceptionMode
		ports          []uint32
		expectListener bool // expect listener to be generated
	}{
		{
			name:           "privileged proxy; implicit REDIRECT mode; any ports",
			unprivileged:   false,
			mode:           "",
			ports:          anyPorts,
			expectListener: true,
		},
		{
			name:           "privileged proxy; explicit REDIRECT mode; any ports",
			unprivileged:   false,
			mode:           model.InterceptionRedirect,
			ports:          anyPorts,
			expectListener: true,
		},
		{
			name:           "privileged proxy; explicit TPROXY mode; any ports",
			unprivileged:   false,
			mode:           model.InterceptionTproxy,
			ports:          anyPorts,
			expectListener: true,
		},
		{
			name:           "privileged proxy; explicit NONE mode; any ports",
			unprivileged:   false,
			mode:           model.InterceptionNone,
			ports:          anyPorts,
			expectListener: true,
		},
		{
			name:           "unprivileged proxy; implicit REDIRECT mode; any ports",
			unprivileged:   true,
			mode:           "",
			ports:          anyPorts,
			expectListener: true,
		},
		{
			name:           "unprivileged proxy; explicit REDIRECT mode; any ports",
			unprivileged:   true,
			mode:           model.InterceptionRedirect,
			ports:          anyPorts,
			expectListener: true,
		},
		{
			name:           "unprivileged proxy; explicit TPROXY mode; any ports",
			unprivileged:   true,
			mode:           model.InterceptionTproxy,
			ports:          anyPorts,
			expectListener: true,
		},
		{
			name:           "unprivileged proxy; explicit NONE mode; privileged ports",
			unprivileged:   true,
			mode:           model.InterceptionNone,
			ports:          privilegedPorts,
			expectListener: false,
		},
		{
			name:           "unprivileged proxy; explicit NONE mode; unprivileged ports",
			unprivileged:   true,
			mode:           model.InterceptionNone,
			ports:          unprivilegedPorts,
			expectListener: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, port := range tc.ports {
				t.Run(strconv.Itoa(int(port)), func(t *testing.T) {
					proxy := getProxy()
					proxy.Metadata.UnprivilegedPod = strconv.FormatBool(tc.unprivileged)
					proxy.Metadata.InterceptionMode = tc.mode

					listeners := buildListeners(t, proxy, port)
					found := hasListenerOrFilterChainForPort(listeners, port)
					if tc.expectListener {
						if !found {
							t.Fatalf("expected listener on port %d, but not found", port)
						}
					} else {
						if found {
							t.Fatalf("expected no listener on port %d, but found found one", port)
						}
					}
				})
			}
		})
	}
}

func testOutboundListenerConflictWithSniffingDisabled(t *testing.T, services ...*model.Service) {
	t.Helper()

	test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, false)

	oldestService := getOldestService(services...)

	listeners := buildOutboundListeners(t, getProxy(), nil, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != protocol.HTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == protocol.HTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}
}

func testOutboundListenerRoute(t *testing.T, services ...*model.Service) {
	t.Helper()
	listeners := buildOutboundListeners(t, getProxy(), nil, nil, services...)
	if len(listeners) != 3 {
		t.Fatalf("expected %d listeners, found %d", 3, len(listeners))
	}

	l := findListenerByAddress(listeners, wildcardIP)
	if l == nil {
		t.Fatalf("expect listener %s", "0.0.0.0_8080")
	}

	f := l.FilterChains[0].Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	rds := cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
	if rds != "8080" {
		t.Fatalf("expect routes %s, found %s", "8080", rds)
	}

	l = findListenerByAddress(listeners, "1.2.3.4")
	if l == nil {
		t.Fatalf("expect listener %s", "1.2.3.4_8080")
	}
	f = l.FilterChains[0].Filters[0]
	cfg, _ = conversion.MessageToStruct(f.GetTypedConfig())
	rds = cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
	if rds != "test1.com:8080" {
		t.Fatalf("expect routes %s, found %s", "test1.com:8080", rds)
	}

	l = findListenerByAddress(listeners, "3.4.5.6")
	if l == nil {
		t.Fatalf("expect listener %s", "3.4.5.6_8080")
	}
	f = l.FilterChains[0].Filters[0]
	cfg, _ = conversion.MessageToStruct(f.GetTypedConfig())
	rds = cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
	if rds != "test3.com:8080" {
		t.Fatalf("expect routes %s, found %s", "test3.com:8080", rds)
	}
}

func testOutboundListenerFilterTimeout(t *testing.T, services ...*model.Service) {
	listeners := buildOutboundListeners(t, getProxy(), nil, nil, services...)
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

func testOutboundListenerConflict(t *testing.T, services ...*model.Service) {
	oldestService := getOldestService(services...)
	proxy := getProxy()
	proxy.DiscoverIPMode()
	listeners := buildOutboundListeners(t, getProxy(), nil, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol == protocol.MySQL {
		if len(listeners[0].FilterChains) != 1 {
			t.Fatalf("expected %d filter chains, found %d", 1, len(listeners[0].FilterChains))
		} else if !isTCPFilterChain(listeners[0].FilterChains[0]) {
			t.Fatalf("expected tcp filter chain, found %s", listeners[0].FilterChains[1].Filters[0].Name)
		}
	} else if oldestProtocol != protocol.HTTP && oldestProtocol != protocol.TCP {
		if len(listeners[0].FilterChains) != 1 {
			t.Fatalf("expectd %d filter chains, found %d", 1, len(listeners[0].FilterChains))
		}
		if !isHTTPFilterChain(listeners[0].FilterChains[0]) {
			t.Fatalf("expected http filter chain, found %s", listeners[0].FilterChains[0].Filters[0].Name)
		}

		if !isTCPFilterChain(listeners[0].DefaultFilterChain) {
			t.Fatalf("expected tcp filter chain, found %s", listeners[0].DefaultFilterChain.Filters[0].Name)
		}

		verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[0])
		verifyListenerFilters(t, listeners[0].ListenerFilters)

		if !listeners[0].ContinueOnListenerFiltersTimeout || listeners[0].ListenerFiltersTimeout == nil {
			t.Fatalf("exptected timeout, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
				listeners[0].ContinueOnListenerFiltersTimeout,
				listeners[0].ListenerFiltersTimeout)
		}

		f := listeners[0].FilterChains[0].Filters[0]
		cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
		rds := cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
		expect := fmt.Sprintf("%d", oldestService.Ports[0].Port)
		if rds != expect {
			t.Fatalf("expect routes %s, found %s", expect, rds)
		}
	} else {
		if len(listeners[0].FilterChains) != 1 {
			t.Fatalf("expectd %d filter chains, found %d", 1, len(listeners[0].FilterChains))
		}
		if listeners[0].DefaultFilterChain == nil {
			t.Fatalf("expected default filter chains, found none")
		}

		_ = getTCPFilterChain(t, listeners[0])
		http := getHTTPFilterChain(t, listeners[0])

		verifyHTTPFilterChainMatch(t, http)
		verifyListenerFilters(t, listeners[0].ListenerFilters)

		if !listeners[0].ContinueOnListenerFiltersTimeout || listeners[0].ListenerFiltersTimeout == nil {
			t.Fatalf("exptected timeout, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
				listeners[0].ContinueOnListenerFiltersTimeout,
				listeners[0].ListenerFiltersTimeout)
		}
	}
}

func getFilterChains(l *listener.Listener) []*listener.FilterChain {
	res := l.FilterChains
	if l.DefaultFilterChain != nil {
		res = append(res, l.DefaultFilterChain)
	}
	return res
}

func getTCPFilterChain(t *testing.T, l *listener.Listener) *listener.FilterChain {
	t.Helper()
	for _, fc := range getFilterChains(l) {
		for _, f := range fc.Filters {
			if f.Name == wellknown.TCPProxy {
				return fc
			}
		}
	}
	t.Fatalf("tcp filter chain not found")
	return nil
}

func getTCPFilter(fc *listener.FilterChain) *listener.Filter {
	for _, f := range fc.Filters {
		if f.Name == wellknown.TCPProxy {
			return f
		}
	}
	return nil
}

func getHTTPFilter(fc *listener.FilterChain) *listener.Filter {
	for _, f := range fc.Filters {
		if f.Name == wellknown.HTTPConnectionManager {
			return f
		}
	}
	return nil
}

func getHTTPFilterChain(t *testing.T, l *listener.Listener) *listener.FilterChain {
	t.Helper()
	for _, fc := range getFilterChains(l) {
		for _, f := range fc.Filters {
			if f.Name == wellknown.HTTPConnectionManager {
				return fc
			}
		}
	}
	t.Fatalf("tcp filter chain not found")
	return nil
}

func testInboundListenerConfig(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	listeners := buildListeners(t, TestOptions{Services: services}, proxy)
	verifyFilterChainMatch(t, xdstest.ExtractListener(model.VirtualInboundListenerName, listeners))
}

func testInboundListenerConfigWithGrpc(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	listeners := buildListeners(t, TestOptions{Services: services}, proxy)
	l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	listenertest.VerifyListener(t, l, listenertest.ListenerTest{
		FilterChains: []listenertest.FilterChainTest{
			{
				Port:        8080,
				HTTPFilters: []string{wellknown.HTTPGRPCStats},
			},
		},
	})
}

func testInboundListenerConfigWithSidecar(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	sidecarConfig := config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
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
	listeners := buildListeners(t, TestOptions{
		Services: services,
		Configs:  []config.Config{sidecarConfig},
	}, proxy)
	l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	verifyFilterChainMatch(t, l)
}

func testInboundListenerConfigWithSidecarWithoutServices(t *testing.T, proxy *model.Proxy) {
	t.Helper()

	sidecarConfig := config.Config{
		Meta: config.Meta{
			Name:             "foo-without-service",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
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
	listeners := buildListeners(t, TestOptions{
		Configs: []config.Config{sidecarConfig},
	}, proxy)
	l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	verifyFilterChainMatch(t, l)
}

func testInboundListenerConfigWithoutService(t *testing.T, proxy *model.Proxy) {
	t.Helper()
	listeners := buildListeners(t, TestOptions{}, proxy)
	l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	verifyFilterChainMatch(t, l)
}

func verifyListenerFilters(t *testing.T, lfilters []*listener.ListenerFilter) {
	t.Helper()
	if len(lfilters) != 2 {
		t.Fatalf("expected %d listener filter, found %d", 2, len(lfilters))
	}
	if lfilters[0].Name != wellknown.TlsInspector ||
		lfilters[1].Name != wellknown.HttpInspector {
		t.Fatalf("expected listener filters not found, got %v", lfilters)
	}
}

func verifyHTTPFilterChainMatch(t *testing.T, fc *listener.FilterChain) {
	t.Helper()
	if fc.FilterChainMatch.TransportProtocol != xdsfilters.RawBufferTransportProtocol {
		t.Fatalf("exepct %q transport protocol, found %q", xdsfilters.RawBufferTransportProtocol, fc.FilterChainMatch.TransportProtocol)
	}

	if !reflect.DeepEqual(plaintextHTTPALPNs, fc.FilterChainMatch.ApplicationProtocols) {
		t.Fatalf("expected %d application protocols, %v got %v",
			len(plaintextHTTPALPNs), plaintextHTTPALPNs, fc.FilterChainMatch.ApplicationProtocols)
	}

	hcm := &hcm.HttpConnectionManager{}
	if err := getFilterConfig(getHTTPFilter(fc), hcm); err != nil {
		t.Fatalf("failed to get HCM, config %v", hcm)
	}

	hasAlpn := hasAlpnFilter(hcm.HttpFilters)

	if !hasAlpn {
		t.Fatal("ALPN filter is not found")
	}
}

func hasAlpnFilter(filters []*hcm.HttpFilter) bool {
	for _, f := range filters {
		if f.Name == xdsfilters.AlpnFilterName {
			return true
		}
	}
	return false
}

func isHTTPFilterChain(fc *listener.FilterChain) bool {
	return getHTTPFilter(fc) != nil
}

func isTCPFilterChain(fc *listener.FilterChain) bool {
	return getTCPFilter(fc) != nil
}

func testOutboundListenerConfigWithSidecar(t *testing.T, services ...*model.Service) {
	t.Helper()
	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   9000,
						Protocol: "GRPC",
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
	test.SetBoolForTest(t, &features.EnableMysqlFilter, true)

	listeners := buildOutboundListeners(t, getProxy(), sidecarConfig, nil, services...)
	if len(listeners) != 4 {
		t.Fatalf("expected %d listeners, found %d", 4, len(listeners))
	}

	l := findListenerByPort(listeners, 8080)
	if len(l.FilterChains) != 1 {
		t.Fatalf("expectd %d filter chains, found %d", 1, len(l.FilterChains))
	}
	if !isHTTPFilterChain(l.FilterChains[0]) {
		t.Fatalf("expected http filter chain, found %s", l.FilterChains[0].Filters[0].Name)
	}

	if !isTCPFilterChain(l.DefaultFilterChain) {
		t.Fatalf("expected tcp filter chain, found %s", l.DefaultFilterChain.Filters[0].Name)
	}

	verifyHTTPFilterChainMatch(t, l.FilterChains[0])
	verifyListenerFilters(t, l.ListenerFilters)

	if l := findListenerByPort(listeners, 3306); !isMysqlListener(l) {
		t.Fatalf("expected MySQL listener on port 3306, found %v", l)
	}

	if l := findListenerByPort(listeners, 9000); !isHTTPListener(l) {
		t.Fatalf("expected HTTP listener on port 9000, found TCP\n%v", l)
	}

	l = findListenerByPort(listeners, 8888)
	if len(l.FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
	}
	if !isHTTPFilterChain(l.FilterChains[0]) {
		t.Fatalf("expected http filter chain, found %s", l.FilterChains[0].Filters[0].Name)
	}

	if !isTCPFilterChain(l.DefaultFilterChain) {
		t.Fatalf("expected tcp filter chain, found %s", l.DefaultFilterChain.Filters[0].Name)
	}

	verifyHTTPFilterChainMatch(t, l.FilterChains[0])
	verifyListenerFilters(t, l.ListenerFilters)
}

func testOutboundListenerConfigWithSidecarWithSniffingDisabled(t *testing.T, services ...*model.Service) {
	t.Helper()
	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
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
	test.SetBoolForTest(t, &features.EnableMysqlFilter, true)

	listeners := buildOutboundListeners(t, getProxy(), sidecarConfig, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	if l := findListenerByPort(listeners, 8080); isHTTPListener(l) {
		t.Fatalf("expected TCP listener on port 8080, found HTTP: %v", l)
	}
}

func testOutboundListenerConfigWithSidecarWithUseRemoteAddress(t *testing.T, services ...*model.Service) {
	t.Helper()
	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
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
	test.SetBoolForTest(t, &features.UseRemoteAddress, true)

	listeners := buildOutboundListeners(t, getProxy(), sidecarConfig, nil, services...)

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
	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					// Bind + Port
					CaptureMode: networking.CaptureMode_NONE,
					Port: &networking.Port{
						Number:   9000,
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
						Number:   9000,
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
	listeners := buildOutboundListeners(t, getProxy(), sidecarConfig, nil, services...)
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
		if l.ConnectionBalanceConfig != nil {
			t.Fatalf("expected connection balance config to be nil, found %v", l.ConnectionBalanceConfig)
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

func TestVirtualListeners_TrafficRedirectionEnabled(t *testing.T) {
	cases := []struct {
		name string
		mode model.TrafficInterceptionMode
	}{
		{
			name: "empty value",
			mode: "",
		},
		{
			name: "unknown value",
			mode: model.TrafficInterceptionMode("UNKNOWN_VALUE"),
		},
		{
			name: string(model.InterceptionTproxy),
			mode: model.InterceptionTproxy,
		},
		{
			name: string(model.InterceptionRedirect),
			mode: model.InterceptionRedirect,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listeners := buildListeners(t, TestOptions{}, &model.Proxy{Metadata: &model.NodeMetadata{InterceptionMode: tc.mode}})

			if l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners); l == nil {
				t.Fatalf("did not generate virtual inbound listener")
			}

			if l := xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners); l == nil {
				t.Fatalf("did not generate virtual outbound listener")
			}
		})
	}
}

func TestVirtualListeners_TrafficRedirectionDisabled(t *testing.T) {
	listeners := buildListeners(t, TestOptions{}, &model.Proxy{Metadata: &model.NodeMetadata{InterceptionMode: model.InterceptionNone}})
	if l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners); l != nil {
		t.Fatalf("unexpectedly generated virtual inbound listener")
	}

	if l := xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners); l != nil {
		t.Fatalf("unexpectedly generated virtual outbound listener")
	}
}

func TestOutboundListenerAccessLogs(t *testing.T) {
	m := mesh.DefaultMeshConfig()
	m.AccessLogFile = "foo"
	listeners := buildListeners(t, TestOptions{MeshConfig: m}, nil)
	validateAccessLog(t, xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners), "")

	// Update MeshConfig
	m.AccessLogFormat = "format modified"
	// Trigger MeshConfig change and validate that access log is recomputed.
	accessLogBuilder.reset()
	listeners = buildListeners(t, TestOptions{MeshConfig: m}, nil)

	// Validate that access log filter uses the new format.
	validateAccessLog(t, xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners), "format modified\n")
}

func TestListenerAccessLogs(t *testing.T) {
	t.Helper()
	m := mesh.DefaultMeshConfig()
	m.AccessLogFile = "foo"
	listeners := buildListeners(t, TestOptions{MeshConfig: m}, nil)
	for _, l := range listeners {
		if l.AccessLog == nil {
			t.Fatalf("expected access log configuration for %v", l)
		}
		if l.AccessLog[0].Filter == nil {
			t.Fatal("expected filter config in listener access log configuration")
		}
	}
}

func validateAccessLog(t *testing.T, l *listener.Listener, format string) {
	t.Helper()
	if l == nil {
		t.Fatalf("nil listener")
	}

	fc := &tcp.TcpProxy{}
	if err := getFilterConfig(xdstest.ExtractFilterChain("virtualOutbound-catchall-tcp", l).Filters[0], fc); err != nil {
		t.Fatalf("failed to get TCP Proxy config: %s", err)
	}
	if fc.AccessLog == nil {
		t.Fatal("expected access log configuration")
	}
	cfg, _ := conversion.MessageToStruct(fc.AccessLog[0].GetTypedConfig())
	textFormat := cfg.GetFields()["log_format"].GetStructValue().GetFields()["text_format_source"].GetStructValue().
		GetFields()["inline_string"].GetStringValue()
	if format != "" && textFormat != format {
		t.Fatalf("expected format to be %s, but got %s", format, textFormat)
	}
}

func TestHttpProxyListener(t *testing.T) {
	m := mesh.DefaultMeshConfig()
	m.ProxyHttpPort = 15007
	listeners := buildListeners(t, TestOptions{MeshConfig: m}, nil)
	httpProxy := xdstest.ExtractListener("127.0.0.1_15007", listeners)
	f := httpProxy.FilterChains[0].Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())

	if httpProxy.Address.GetSocketAddress().GetPortValue() != 15007 {
		t.Fatalf("expected http proxy is not listening on %d, but on port %d", 15007,
			httpProxy.Address.GetSocketAddress().GetPortValue())
	}
	if !strings.HasPrefix(cfg.Fields["stat_prefix"].GetStringValue(), "outbound_") {
		t.Fatalf("expected http proxy stat prefix to have outbound, %s", cfg.Fields["stat_prefix"].GetStringValue())
	}
}

func TestHttpProxyListenerPerWorkload(t *testing.T) {
	listeners := buildListeners(t, TestOptions{}, &model.Proxy{Metadata: &model.NodeMetadata{HTTPProxyPort: "15007"}})
	httpProxy := xdstest.ExtractListener("127.0.0.1_15007", listeners)
	f := httpProxy.FilterChains[0].Filters[0]
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())

	if httpProxy.Address.GetSocketAddress().GetPortValue() != 15007 {
		t.Fatalf("expected http proxy is not listening on %d, but on port %d", 15007,
			httpProxy.Address.GetSocketAddress().GetPortValue())
	}
	if !strings.HasPrefix(cfg.Fields["stat_prefix"].GetStringValue(), "outbound_") {
		t.Fatalf("expected http proxy stat prefix to have outbound, %s", cfg.Fields["stat_prefix"].GetStringValue())
	}
}

func TestHttpProxyListener_Tracing(t *testing.T) {
	customTagsTest := []struct {
		name             string
		in               *meshconfig.Tracing
		out              *hcm.HttpConnectionManager_Tracing
		tproxy           *model.Proxy
		envPilotSampling float64
		disableIstioTags bool
	}{
		{
			name:             "random-sampling-env",
			tproxy:           getProxy(),
			envPilotSampling: 80.0,
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 0,
				Sampling:         0,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: nil,
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 80.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: customTracingTags(),
			},
		},
		{
			name:             "random-sampling-env-without-istio-tags",
			disableIstioTags: true,
			tproxy:           getProxy(),
			envPilotSampling: 80.0,
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 0,
				Sampling:         0,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: nil,
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 80.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
			},
		},
		{
			name:             "random-sampling-env-and-meshconfig",
			tproxy:           getProxy(),
			envPilotSampling: 80.0,
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 0,
				Sampling:         10,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: nil,
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 10.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: customTracingTags(),
			},
		},
		{
			name:             "random-sampling-too-low-env",
			tproxy:           getProxy(),
			envPilotSampling: -1,
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 0,
				Sampling:         300,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: nil,
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: customTracingTags(),
			},
		},
		{
			name:             "random-sampling-too-high-meshconfig",
			tproxy:           getProxy(),
			envPilotSampling: 80.0,
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 0,
				Sampling:         300,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: nil,
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: customTracingTags(),
			},
		},
		{
			name:             "random-sampling-too-high-env",
			tproxy:           getProxy(),
			envPilotSampling: 2000.0,
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 0,
				Sampling:         300,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: nil,
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: customTracingTags(),
			},
		},
		{
			// upstream will set the default to 256 per
			// its documentation
			name:   "tag-max-path-length-not-set-default",
			tproxy: getProxy(),
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 0,
				Sampling:         0,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: nil,
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: customTracingTags(),
			},
		},
		{
			name:   "tag-max-path-length-set-to-1024",
			tproxy: getProxy(),
			in: &meshconfig.Tracing{
				Tracer:           nil,
				CustomTags:       nil,
				MaxPathTagLength: 1024,
				Sampling:         0,
			},
			out: &hcm.HttpConnectionManager_Tracing{
				MaxPathTagLength: &wrappers.UInt32Value{
					Value: 1024,
				},
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: customTracingTags(),
			},
		},
		{
			name:   "custom-tags-sidecar",
			tproxy: getProxy(),
			in: &meshconfig.Tracing{
				CustomTags: map[string]*meshconfig.Tracing_CustomTag{
					"custom_tag_env": {
						Type: &meshconfig.Tracing_CustomTag_Environment{
							Environment: &meshconfig.Tracing_Environment{
								Name:         "custom_tag_env-var",
								DefaultValue: "custom-tag-env-default",
							},
						},
					},
					"custom_tag_request_header": {
						Type: &meshconfig.Tracing_CustomTag_Header{
							Header: &meshconfig.Tracing_RequestHeader{
								Name:         "custom_tag_request_header_name",
								DefaultValue: "custom-defaulted-value-request-header",
							},
						},
					},
					// leave this in non-alphanumeric order to verify
					// the stable sorting doing when creating the custom tag filter
					"custom_tag_literal": {
						Type: &meshconfig.Tracing_CustomTag_Literal{
							Literal: &meshconfig.Tracing_Literal{
								Value: "literal-value",
							},
						},
					},
				},
			},
			out: &hcm.HttpConnectionManager_Tracing{
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: append([]*tracing.CustomTag{
					{
						Tag: "custom_tag_env",
						Type: &tracing.CustomTag_Environment_{
							Environment: &tracing.CustomTag_Environment{
								Name:         "custom_tag_env-var",
								DefaultValue: "custom-tag-env-default",
							},
						},
					},
					{
						Tag: "custom_tag_literal",
						Type: &tracing.CustomTag_Literal_{
							Literal: &tracing.CustomTag_Literal{
								Value: "literal-value",
							},
						},
					},
					{
						Tag: "custom_tag_request_header",
						Type: &tracing.CustomTag_RequestHeader{
							RequestHeader: &tracing.CustomTag_Header{
								Name:         "custom_tag_request_header_name",
								DefaultValue: "custom-defaulted-value-request-header",
							},
						},
					},
				}, customTracingTags()...),
			},
		},
		{
			name:             "custom-tags-sidecar-without-istio-tags",
			disableIstioTags: true,
			tproxy:           getProxy(),
			in: &meshconfig.Tracing{
				CustomTags: map[string]*meshconfig.Tracing_CustomTag{
					"custom_tag_env": {
						Type: &meshconfig.Tracing_CustomTag_Environment{
							Environment: &meshconfig.Tracing_Environment{
								Name:         "custom_tag_env-var",
								DefaultValue: "custom-tag-env-default",
							},
						},
					},
					"custom_tag_request_header": {
						Type: &meshconfig.Tracing_CustomTag_Header{
							Header: &meshconfig.Tracing_RequestHeader{
								Name:         "custom_tag_request_header_name",
								DefaultValue: "custom-defaulted-value-request-header",
							},
						},
					},
					// leave this in non-alphanumeric order to verify
					// the stable sorting doing when creating the custom tag filter
					"custom_tag_literal": {
						Type: &meshconfig.Tracing_CustomTag_Literal{
							Literal: &meshconfig.Tracing_Literal{
								Value: "literal-value",
							},
						},
					},
				},
			},
			out: &hcm.HttpConnectionManager_Tracing{
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				CustomTags: []*tracing.CustomTag{
					{
						Tag: "custom_tag_env",
						Type: &tracing.CustomTag_Environment_{
							Environment: &tracing.CustomTag_Environment{
								Name:         "custom_tag_env-var",
								DefaultValue: "custom-tag-env-default",
							},
						},
					},
					{
						Tag: "custom_tag_literal",
						Type: &tracing.CustomTag_Literal_{
							Literal: &tracing.CustomTag_Literal{
								Value: "literal-value",
							},
						},
					},
					{
						Tag: "custom_tag_request_header",
						Type: &tracing.CustomTag_RequestHeader{
							RequestHeader: &tracing.CustomTag_Header{
								Name:         "custom_tag_request_header_name",
								DefaultValue: "custom-defaulted-value-request-header",
							},
						},
					},
				},
			},
		},
		{
			name:   "custom-tracing-gateways",
			tproxy: &proxyGateway,
			in: &meshconfig.Tracing{
				MaxPathTagLength: 100,
				CustomTags: map[string]*meshconfig.Tracing_CustomTag{
					"custom_tag_request_header": {
						Type: &meshconfig.Tracing_CustomTag_Header{
							Header: &meshconfig.Tracing_RequestHeader{
								Name:         "custom_tag_request_header_name",
								DefaultValue: "custom-defaulted-value-request-header",
							},
						},
					},
				},
			},
			out: &hcm.HttpConnectionManager_Tracing{
				ClientSampling: &xdstype.Percent{
					Value: 100.0,
				},
				RandomSampling: &xdstype.Percent{
					Value: 1.0,
				},
				OverallSampling: &xdstype.Percent{
					Value: 100.0,
				},
				MaxPathTagLength: &wrappers.UInt32Value{
					Value: 100,
				},
				CustomTags: append([]*tracing.CustomTag{
					{
						Tag: "custom_tag_request_header",
						Type: &tracing.CustomTag_RequestHeader{
							RequestHeader: &tracing.CustomTag_Header{
								Name:         "custom_tag_request_header_name",
								DefaultValue: "custom-defaulted-value-request-header",
							},
						},
					},
				}, customTracingTags()...),
			},
		},
	}
	for _, tc := range customTagsTest {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envPilotSampling != 0.0 {
				test.SetFloatForTest(t, &features.TraceSampling, tc.envPilotSampling)
			}

			test.SetBoolForTest(t, &features.EnableIstioTags, !tc.disableIstioTags)

			m := mesh.DefaultMeshConfig()
			m.ProxyHttpPort = 15007
			m.EnableTracing = true
			m.DefaultConfig = &meshconfig.ProxyConfig{
				Tracing: &meshconfig.Tracing{
					CustomTags:       tc.in.CustomTags,
					MaxPathTagLength: tc.in.MaxPathTagLength,
					Sampling:         tc.in.Sampling,
				},
			}
			listeners := buildListeners(t, TestOptions{MeshConfig: m}, nil)
			httpProxy := xdstest.ExtractListener("127.0.0.1_15007", listeners)
			f := httpProxy.FilterChains[0].Filters[0]
			verifyHTTPConnectionManagerFilter(t, f, tc.out, tc.name)
		})
	}
}

func customTracingTags() []*tracing.CustomTag {
	return append(buildOptionalPolicyTags(),
		&tracing.CustomTag{
			Tag: "istio.canonical_revision",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "latest",
				},
			},
		},
		&tracing.CustomTag{
			Tag: "istio.canonical_service",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "unknown",
				},
			},
		},
		&tracing.CustomTag{
			Tag: "istio.mesh_id",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "unknown",
				},
			},
		},
		&tracing.CustomTag{
			Tag: "istio.namespace",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "default",
				},
			},
		})
}

func verifyHTTPConnectionManagerFilter(t *testing.T, f *listener.Filter, expected *hcm.HttpConnectionManager_Tracing, name string) {
	t.Helper()
	if f.Name == wellknown.HTTPConnectionManager {
		cmgr := &hcm.HttpConnectionManager{}
		err := getFilterConfig(f, cmgr)
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(cmgr.GetTracing(), expected, protocmp.Transform()); diff != "" {
			t.Fatalf("Testcase failure: %s custom tags did match not expected output; diff: %v", name, diff)
		}
	}
}

func TestOutboundListenerConfig_TCPFailThrough(t *testing.T) {
	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIP, protocol.HTTP, tnow),
	}
	listeners := buildListeners(t, TestOptions{Services: services}, nil)
	l := xdstest.ExtractListener("0.0.0.0_8080", listeners)
	if l == nil {
		t.Fatalf("failed to find listener")
	}
	if len(l.FilterChains) != 1 {
		t.Fatalf("expectd %d filter chains, found %d", 1, len(l.FilterChains))
	}

	verifyHTTPFilterChainMatch(t, l.FilterChains[0])
	verifyPassThroughTCPFilterChain(t, l.DefaultFilterChain)
	verifyListenerFilters(t, l.ListenerFilters)
}

func verifyPassThroughTCPFilterChain(t *testing.T, fc *listener.FilterChain) {
	t.Helper()
	f := fc.Filters[0]
	expectedStatPrefix := util.PassthroughCluster
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	statPrefix := cfg.Fields["stat_prefix"].GetStringValue()
	if statPrefix != expectedStatPrefix {
		t.Fatalf("expected listener to contain stat_prefix %s, found %s", expectedStatPrefix, statPrefix)
	}
}

func verifyOutboundTCPListenerHostname(t *testing.T, l *listener.Listener, hostname host.Name) {
	t.Helper()
	if len(l.FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	f := getTCPFilter(fc)
	if f == nil {
		t.Fatalf("expected TCP filters, found none")
	}
	expectedStatPrefix := fmt.Sprintf("outbound|8080||%s", hostname)
	cfg, _ := conversion.MessageToStruct(f.GetTypedConfig())
	statPrefix := cfg.Fields["stat_prefix"].GetStringValue()
	if statPrefix != expectedStatPrefix {
		t.Fatalf("expected listener to contain stat_prefix %s, found %s", expectedStatPrefix, statPrefix)
	}
}

func verifyFilterChainMatch(t *testing.T, listener *listener.Listener) {
	httpFilters := []string{
		xdsfilters.MxFilterName,
		xdsfilters.Fault.Name,
		xdsfilters.Cors.Name,
		xdsfilters.Router.Name,
	}
	listenertest.VerifyListener(t, listener, listenertest.ListenerTest{
		FilterChains: []listenertest.FilterChainTest{
			{
				Name:       model.VirtualInboundBlackholeFilterChainName,
				Port:       15006,
				TotalMatch: true,
			},
			{
				Name:           model.VirtualInboundCatchAllHTTPFilterChainName,
				Type:           listenertest.MTLSHTTP,
				HTTPFilters:    httpFilters,
				NetworkFilters: []string{xdsfilters.MxFilterName, wellknown.HTTPConnectionManager},
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundCatchAllHTTPFilterChainName,
				Type:           listenertest.PlainHTTP,
				HTTPFilters:    httpFilters,
				NetworkFilters: []string{xdsfilters.MxFilterName, wellknown.HTTPConnectionManager},
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundListenerName,
				Type:           listenertest.MTLSTCP,
				HTTPFilters:    []string{},
				NetworkFilters: []string{xdsfilters.MxFilterName, wellknown.TCPProxy},
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundListenerName,
				Type:           listenertest.PlainTCP,
				HTTPFilters:    []string{},
				NetworkFilters: []string{xdsfilters.MxFilterName, wellknown.TCPProxy},
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundListenerName,
				Type:           listenertest.StandardTLS,
				HTTPFilters:    []string{},
				NetworkFilters: []string{xdsfilters.MxFilterName, wellknown.TCPProxy},
				TotalMatch:     true,
			},
		},
	})
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

func getFilterConfig(filter *listener.Filter, out proto.Message) error {
	switch c := filter.ConfigType.(type) {
	case *listener.Filter_TypedConfig:
		if err := c.TypedConfig.UnmarshalTo(out); err != nil {
			return err
		}
	}
	return nil
}

func buildOutboundListeners(t *testing.T, proxy *model.Proxy, sidecarConfig *config.Config,
	virtualService *config.Config, services ...*model.Service,
) []*listener.Listener {
	t.Helper()
	cg := NewConfigGenTest(t, TestOptions{
		Services:       services,
		ConfigPointers: []*config.Config{sidecarConfig, virtualService},
	})
	listeners := NewListenerBuilder(proxy, cg.env.PushContext).buildSidecarOutboundListeners(cg.SetupProxy(proxy), cg.env.PushContext)
	xdstest.ValidateListeners(t, listeners)
	return listeners
}

func isHTTPListener(listener *listener.Listener) bool {
	for _, fc := range listener.GetFilterChains() {
		if isHTTPFilterChain(fc) {
			return true
		}
	}
	return false
}

func isMysqlListener(listener *listener.Listener) bool {
	if len(listener.FilterChains) > 0 && len(listener.FilterChains[0].Filters) > 0 {
		return listener.FilterChains[0].Filters[0].Name == wellknown.MySQLProxy
	}
	return false
}

func hasListenerOrFilterChainForPort(listeners []*listener.Listener, port uint32) bool {
	for _, l := range listeners {
		if port == l.Address.GetSocketAddress().GetPortValue() {
			return true
		}
		for _, fc := range l.GetFilterChains() {
			if fc.GetFilterChainMatch().GetDestinationPort().GetValue() == port {
				return true
			}
		}
	}

	return false
}

func findListenerByPort(listeners []*listener.Listener, port uint32) *listener.Listener {
	for _, l := range listeners {
		if port == l.Address.GetSocketAddress().GetPortValue() {
			return l
		}
	}

	return nil
}

func findListenerByAddress(listeners []*listener.Listener, address string) *listener.Listener {
	for _, l := range listeners {
		if address == l.Address.GetSocketAddress().Address {
			return l
		}
	}

	return nil
}

func buildService(hostname string, ip string, protocol protocol.Instance, creationTime time.Time) *model.Service {
	return &model.Service{
		CreationTime:   creationTime,
		Hostname:       host.Name(hostname),
		DefaultAddress: ip,
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     8080,
				Protocol: protocol,
			},
		},
		Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
}

func buildServiceWithPort(hostname string, port int, protocol protocol.Instance, creationTime time.Time) *model.Service {
	return &model.Service{
		CreationTime:   creationTime,
		Hostname:       host.Name(hostname),
		DefaultAddress: wildcardIP,
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     port,
				Protocol: protocol,
			},
		},
		Resolution: model.ClientSideLB,
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
		ServicePort: service.Ports[0],
		Service:     service,
	}
}

func TestAppendListenerFallthroughRouteForCompleteListener(t *testing.T) {
	tests := []struct {
		name        string
		node        *model.Proxy
		hostname    string
		idleTimeout *durationpb.Duration
	}{
		{
			name: "Registry_Only",
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
			name: "Allow_Any",
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
		{
			name: "idle_timeout",
			node: &model.Proxy{
				ID: "foo.bar",
				Metadata: &model.NodeMetadata{
					IdleTimeout: "15s",
				},
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
					},
				},
			},
			hostname:    util.PassthroughCluster,
			idleTimeout: durationpb.New(15 * time.Second),
		},
		{
			name: "invalid_idle_timeout",
			node: &model.Proxy{
				ID: "foo.bar",
				Metadata: &model.NodeMetadata{
					IdleTimeout: "s15s",
				},
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
					},
				},
			},
			hostname:    util.PassthroughCluster,
			idleTimeout: durationpb.New(0 * time.Second),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{})
			l := &listener.Listener{}
			appendListenerFallthroughRouteForCompleteListener(l, tt.node, cg.PushContext())
			if len(l.FilterChains) != 0 {
				t.Errorf("Expected exactly 0 filter chain")
			}
			if len(l.DefaultFilterChain.Filters) != 1 {
				t.Errorf("Expected exactly 1 network filter in the chain")
			}
			filter := l.DefaultFilterChain.Filters[0]
			var tcpProxy tcp.TcpProxy
			cfg := filter.GetTypedConfig()
			_ = cfg.UnmarshalTo(&tcpProxy)
			if tcpProxy.StatPrefix != tt.hostname {
				t.Errorf("Expected stat prefix %s but got %s\n", tt.hostname, tcpProxy.StatPrefix)
			}
			if tcpProxy.GetCluster() != tt.hostname {
				t.Errorf("Expected cluster %s but got %s\n", tt.hostname, tcpProxy.GetCluster())
			}
			if tt.idleTimeout != nil && !reflect.DeepEqual(tcpProxy.IdleTimeout, tt.idleTimeout) {
				t.Errorf("Expected IdleTimeout %s but got %s\n", tt.idleTimeout, tcpProxy.IdleTimeout)
			}
		})
	}
}

func TestMergeTCPFilterChains(t *testing.T) {
	cg := NewConfigGenTest(t, TestOptions{})

	node := &model.Proxy{
		ID:       "foo.bar",
		Metadata: &model.NodeMetadata{},
		SidecarScope: &model.SidecarScope{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
	}

	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       "outbound|443||foo.com",
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: "outbound|443||foo.com"},
	}

	tcpProxyFilter := &listener.Filter{
		Name:       wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(tcpProxy)},
	}

	tcpProxy = &tcp.TcpProxy{
		StatPrefix:       "outbound|443||bar.com",
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: "outbound|443||bar.com"},
	}

	tcpProxyFilter2 := &listener.Filter{
		Name:       wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(tcpProxy)},
	}

	svcPort := &model.Port{
		Name:     "https",
		Port:     443,
		Protocol: protocol.HTTPS,
	}
	var l listener.Listener
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
			Filters: buildOutboundCatchAllNetworkFiltersOnly(cg.PushContext(), node),
		},
	}
	l.FilterChains = filterChains
	listenerMap := map[string]*outboundListenerEntry{
		"0.0.0.0_443": {
			servicePort: svcPort,
			services: []*model.Service{{
				CreationTime:   tnow,
				Hostname:       host.Name("foo.com"),
				DefaultAddress: "192.168.1.1",
				Ports:          []*model.Port{svcPort},
				Resolution:     model.DNSLB,
			}},
			listener: &l,
		},
	}

	incomingFilterChains := []*listener.FilterChain{
		{
			FilterChainMatch: &listener.FilterChainMatch{
				ServerNames: []string{"bar.com"},
			}, // This is not a valid config, just for test
			Filters: []*listener.Filter{tcpProxyFilter2},
		},
	}

	svc := model.Service{
		Hostname: "bar.com",
	}

	opts := buildListenerOpts{
		proxy:   node,
		push:    cg.PushContext(),
		service: &svc,
	}

	out := mergeTCPFilterChains(incomingFilterChains, opts, "0.0.0.0_443", listenerMap)

	if len(out) != 4 {
		t.Errorf("Got %d filter chains, expected 3", len(out))
	}
	if !isMatchAllFilterChain(out[2]) {
		t.Errorf("The last filter chain  %#v is not wildcard matching", out[2])
	}

	if !reflect.DeepEqual(out[3].Filters, incomingFilterChains[0].Filters) {
		t.Errorf("got %v\nwant %v\ndiff %v", out[2].Filters, incomingFilterChains[0].Filters, cmp.Diff(out[2].Filters, incomingFilterChains[0].Filters))
	}
}

func TestFilterChainMatchEqual(t *testing.T) {
	cases := []struct {
		name   string
		first  *listener.FilterChainMatch
		second *listener.FilterChainMatch
		want   bool
	}{
		{
			name:   "both nil",
			first:  nil,
			second: nil,
			want:   true,
		},
		{
			name:   "one of them nil",
			first:  nil,
			second: &listener.FilterChainMatch{},
			want:   false,
		},
		{
			name:   "both empty",
			first:  &listener.FilterChainMatch{},
			second: &listener.FilterChainMatch{},
			want:   true,
		},
		{
			name: "with equal values",
			first: &listener.FilterChainMatch{
				TransportProtocol:    "TCP",
				ApplicationProtocols: mtlsHTTPALPNs,
			},
			second: &listener.FilterChainMatch{
				TransportProtocol:    "TCP",
				ApplicationProtocols: mtlsHTTPALPNs,
			},
			want: true,
		},
		{
			name: "with not equal values",
			first: &listener.FilterChainMatch{
				TransportProtocol:    "TCP",
				ApplicationProtocols: mtlsHTTPALPNs,
			},
			second: &listener.FilterChainMatch{
				TransportProtocol:    "TCP",
				ApplicationProtocols: plaintextHTTPALPNs,
			},
			want: false,
		},
		{
			name: "equal with all values",
			first: &listener.FilterChainMatch{
				TransportProtocol:    "TCP",
				ApplicationProtocols: mtlsHTTPALPNs,
				DestinationPort:      &wrappers.UInt32Value{Value: 1999},
				AddressSuffix:        "suffix",
				SourceType:           listener.FilterChainMatch_ANY,
				SuffixLen:            &wrappers.UInt32Value{Value: 3},
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
				SourcePrefixRanges: []*core.CidrRange{
					{
						AddressPrefix: "10.244.0.18",
						PrefixLen:     &wrappers.UInt32Value{Value: 32},
					},
					{
						AddressPrefix: "fe80::1c97:c3ff:fed7:5940",
						PrefixLen:     &wrappers.UInt32Value{Value: 128},
					},
				},
				SourcePorts: []uint32{2000},
				ServerNames: []string{"foo"},
			},
			second: &listener.FilterChainMatch{
				TransportProtocol:    "TCP",
				ApplicationProtocols: plaintextHTTPALPNs,
				DestinationPort:      &wrappers.UInt32Value{Value: 1999},
				AddressSuffix:        "suffix",
				SourceType:           listener.FilterChainMatch_ANY,
				SuffixLen:            &wrappers.UInt32Value{Value: 3},
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
				SourcePrefixRanges: []*core.CidrRange{
					{
						AddressPrefix: "10.244.0.18",
						PrefixLen:     &wrappers.UInt32Value{Value: 32},
					},
					{
						AddressPrefix: "fe80::1c97:c3ff:fed7:5940",
						PrefixLen:     &wrappers.UInt32Value{Value: 128},
					},
				},
				SourcePorts: []uint32{2000},
				ServerNames: []string{"foo"},
			},
			want: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := filterChainMatchEqual(tt.first, tt.second); got != tt.want {
				t.Fatalf("Expected filter chain match to return %v, but got %v", tt.want, got)
			}
		})
	}
}
