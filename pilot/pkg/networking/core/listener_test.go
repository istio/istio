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
	"log"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	tracing "github.com/envoyproxy/go-control-plane/envoy/type/tracing/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	telemetry "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/listenertest"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

const (
	wildcardIPv4 = "0.0.0.0"
	wildcardIPv6 = "::"
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

func getIPv6Proxy() *model.Proxy {
	pr := &model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"2001:1::1"},
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
		},
		ConfigNamespace: "not-default",
	}
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
	// this proxy is a dual stack proxy which holds ipv4 and ipv6 addresses
	dualStackProxy = model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1", "1111:2222::1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
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
	for _, p := range []*model.Proxy{getProxy(), &proxyHTTP10, &dualStackProxy} {
		t.Run("multiple services", func(t *testing.T) {
			testInboundListenerConfig(t, p,
				buildServiceWithPort("test1.com", 15021, protocol.HTTP, tnow.Add(1*time.Second)),
				buildService("test2.com", wildcardIPv4, "unknown", tnow),
				buildService("test3.com", wildcardIPv4, protocol.HTTP, tnow.Add(2*time.Second)))
		})
		t.Run("no service", func(t *testing.T) {
			testInboundListenerConfigWithoutService(t, p)
		})
		t.Run("sidecar", func(t *testing.T) {
			testInboundListenerConfigWithSidecar(t, p,
				buildService("test.com", wildcardIPv4, protocol.HTTP, tnow))
		})
		t.Run("sidecar with service", func(t *testing.T) {
			testInboundListenerConfigWithSidecarWithoutServices(t, p)
		})
	}

	t.Run("services target port conflict with static listener", func(t *testing.T) {
		p := getProxy()
		p.Metadata.EnvoyStatusPort = 15021
		testInboundListenerConfigWithConflictPort(t, p,
			buildServiceWithPort("test1.com", 15021, protocol.HTTP, tnow.Add(1*time.Second)),
			buildService("test2.com", wildcardIPv4, "unknown", tnow),
			buildService("test3.com", wildcardIPv4, protocol.HTTP, tnow.Add(2*time.Second)))
	})

	t.Run("sidecar conflict port", func(t *testing.T) {
		p := getProxy()
		p.Metadata.EnvoyStatusPort = 15021
		p.Metadata.EnvoyPrometheusPort = 15090
		testInboundListenerConfigWithSidecarConflictPort(t, p,
			buildService("test.com", wildcardIPv4, protocol.HTTP, tnow))
	})

	t.Run("grpc", func(t *testing.T) {
		testInboundListenerConfigWithGrpc(t, getProxy(),
			buildService("test1.com", wildcardIPv4, protocol.GRPC, tnow.Add(1*time.Second)))
	})

	t.Run("merge sidecar ingress ports and service ports", func(t *testing.T) {
		test.SetForTest(t, &features.EnableSidecarServiceInboundListenerMerge, true)
		testInboundListenerConfigWithSidecarIngressPortMergeServicePort(t, getProxy(),
			buildServiceWithPort("test1.com", 80, protocol.HTTP, tnow.Add(1*time.Second)))
	})
	t.Run("merge sidecar ingress and service ports, same port in both sidecar and service", func(t *testing.T) {
		test.SetForTest(t, &features.EnableSidecarServiceInboundListenerMerge, true)
		testInboundListenerConfigWithSidecar(t, getProxy(),
			buildService("test.com", wildcardIPv4, protocol.HTTP, tnow))
	})

	t.Run("wasm, stats, authz", func(t *testing.T) {
		tcp := buildService("tcp.example.com", wildcardIPv4, protocol.TCP, tnow)
		tcp.Ports[0].Port = 1234
		tcp.Ports[0].Name = "tcp"
		services := []*model.Service{
			tcp,
			buildService("http.example.com", wildcardIPv4, protocol.HTTP, tnow),
		}
		mc := mesh.DefaultMeshConfig()
		mc.ExtensionProviders = append(mc.ExtensionProviders, &meshconfig.MeshConfig_ExtensionProvider{
			Name: "extauthz",
			Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc{
				EnvoyExtAuthzGrpc: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider{
					Service: "default/http.example.com",
					Port:    8080,
				},
			},
		})
		o := TestOptions{
			Services:   services,
			MeshConfig: mc,
			Configs:    filterTestConfigs,
		}

		httpFilters := []string{
			xdsfilters.MxFilterName,
			// Ext auth makes 2 filters
			wellknown.HTTPRoleBasedAccessControl,
			wellknown.HTTPExternalAuthorization,
			"extensions.istio.io/wasmplugin/istio-system.wasm-authn",
			"extensions.istio.io/wasmplugin/istio-system.wasm-authz",
			wellknown.HTTPRoleBasedAccessControl,
			"extensions.istio.io/wasmplugin/istio-system.wasm-stats",
			wellknown.HTTPGRPCStats,
			xdsfilters.Fault.Name,
			xdsfilters.Cors.Name,
			xds.StatsFilterName,
			wellknown.Router,
		}
		httpNetworkFilters := []string{
			xdsfilters.MxFilterName,
			"extensions.istio.io/wasmplugin/istio-system.wasm-network-authn",
			"extensions.istio.io/wasmplugin/istio-system.wasm-network-authz",
			"extensions.istio.io/wasmplugin/istio-system.wasm-network-stats",
			wellknown.HTTPConnectionManager,
		}
		tcpNetworkFilters := []string{
			xdsfilters.MxFilterName,
			// Ext auth makes 2 filters
			wellknown.RoleBasedAccessControl,
			wellknown.ExternalAuthorization,
			"extensions.istio.io/wasmplugin/istio-system.wasm-network-authn",
			"extensions.istio.io/wasmplugin/istio-system.wasm-network-authz",
			wellknown.RoleBasedAccessControl,
			"extensions.istio.io/wasmplugin/istio-system.wasm-network-stats",
			xds.StatsFilterName,
			wellknown.TCPProxy,
		}
		cg := NewConfigGenTest(t, o)
		p := getProxy()
		for _, s := range o.Services {
			i := &model.ServiceInstance{
				Service: s,
				Endpoint: &model.IstioEndpoint{
					Addresses:    []string{"1.1.1.1"},
					EndpointPort: uint32(s.Ports[0].Port),
				},
				ServicePort: s.Ports[0],
			}
			cg.MemRegistry.AddInstance(i)
		}
		listeners := cg.Listeners(cg.SetupProxy(p))
		xdstest.ValidateListeners(t, listeners)
		l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
		verifyInboundFilterChains(t, l, httpFilters, httpNetworkFilters, tcpNetworkFilters)
		// verifyInboundFilterChains only checks the passthrough. Ensure the main filters get created as expected, too.
		listenertest.VerifyListener(t, l, listenertest.ListenerTest{
			FilterChains: []listenertest.FilterChainTest{
				{
					Name:           "0.0.0.0_8080",
					Type:           listenertest.MTLSHTTP,
					HTTPFilters:    httpFilters,
					NetworkFilters: httpNetworkFilters,
					TotalMatch:     true,
				},
				{
					Name:           "0.0.0.0_8080",
					Type:           listenertest.PlainTCP,
					HTTPFilters:    httpFilters,
					NetworkFilters: httpNetworkFilters,
					TotalMatch:     true,
				},
				{
					Name:           "0.0.0.0_1234",
					Type:           listenertest.StandardTLS,
					HTTPFilters:    []string{},
					NetworkFilters: tcpNetworkFilters,
					TotalMatch:     true,
				},
				{
					Name:           "0.0.0.0_1234",
					Type:           listenertest.PlainTCP,
					HTTPFilters:    []string{},
					NetworkFilters: tcpNetworkFilters,
					TotalMatch:     true,
				},
			},
		})

		// test instance with multiple addresses
		cg = NewConfigGenTest(t, o)
		p = getProxy()
		for _, s := range o.Services {
			i := &model.ServiceInstance{
				Service: s,
				Endpoint: &model.IstioEndpoint{
					Addresses:    []string{"1.1.1.1", "2001:1::1"},
					EndpointPort: uint32(s.Ports[0].Port),
				},
				ServicePort: s.Ports[0],
			}
			cg.MemRegistry.AddInstance(i)
		}
		listeners = cg.Listeners(cg.SetupProxy(p))
		xdstest.ValidateListeners(t, listeners)
		l = xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
		verifyInboundFilterChains(t, l, httpFilters, httpNetworkFilters, tcpNetworkFilters)
		// verifyInboundFilterChains only checks the passthrough. Ensure the main filters get created as expected, too.
		listenertest.VerifyListener(t, l, listenertest.ListenerTest{
			FilterChains: []listenertest.FilterChainTest{
				{
					Name:           "0.0.0.0_8080",
					Type:           listenertest.MTLSHTTP,
					HTTPFilters:    httpFilters,
					NetworkFilters: httpNetworkFilters,
					TotalMatch:     true,
				},
				{
					Name:           "0.0.0.0_8080",
					Type:           listenertest.PlainTCP,
					HTTPFilters:    httpFilters,
					NetworkFilters: httpNetworkFilters,
					TotalMatch:     true,
				},
				{
					Name:           "0.0.0.0_1234",
					Type:           listenertest.StandardTLS,
					HTTPFilters:    []string{},
					NetworkFilters: tcpNetworkFilters,
					TotalMatch:     true,
				},
				{
					Name:           "0.0.0.0_1234",
					Type:           listenertest.PlainTCP,
					HTTPFilters:    []string{},
					NetworkFilters: tcpNetworkFilters,
					TotalMatch:     true,
				},
			},
		})
	})
}

func TestOutboundListenerConflict_HTTPWithCurrentUnknown(t *testing.T) {
	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIPv4, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIPv4, "unknown", tnow),
		buildService("test3.com", wildcardIPv4, protocol.HTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_WellKnowPorts(t *testing.T) {
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
	// The oldest service port is unknown.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIPv4, protocol.TCP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIPv4, "unknown", tnow),
		buildService("test3.com", wildcardIPv4, protocol.TCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentTCP(t *testing.T) {
	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIPv4, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIPv4, protocol.TCP, tnow),
		buildService("test3.com", wildcardIPv4, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_UnknownWithCurrentHTTP(t *testing.T) {
	// The oldest service port is Auto.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIPv4, "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIPv4, protocol.HTTP, tnow),
		buildService("test3.com", wildcardIPv4, "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerRoute(t *testing.T) {
	testOutboundListenerRoute(t,
		buildService("test1.com", "1.2.3.4", "unknown", tnow.Add(1*time.Second)),
		buildService("test2.com", "2.3.4.5", protocol.HTTP, tnow),
		buildService("test3.com", "3.4.5.6", "unknown", tnow.Add(2*time.Second)))
}

func TestOutboundListenerConfig_WithSidecar(t *testing.T) {
	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIPv4, protocol.HTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIPv4, protocol.TCP, tnow),
		buildService("test3.com", wildcardIPv4, "unknown", tnow.Add(2*time.Second)),
	}
	service4 := &model.Service{
		CreationTime:   tnow.Add(1 * time.Second),
		Hostname:       host.Name("test4.com"),
		DefaultAddress: wildcardIPv4,
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

func TestOutboundListenerConflictWithReservedListener(t *testing.T) {
	// Add a service and verify it's config
	servicesConflictWithStaticListener := []*model.Service{
		buildServiceWithPort("test1.com", 15021, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test2.com", 15090, protocol.TCP, tnow),
		buildServiceWithPort("test3.com", 8080, protocol.HTTP, tnow.Add(2*time.Second)),
	}
	servicesConflictWithVirtualListener := []*model.Service{
		buildServiceWithPort("test4.com", 15001, protocol.HTTP, tnow.Add(1*time.Second)),
		buildServiceWithPort("test5.com", 15006, protocol.TCP, tnow),
		buildServiceWithPort("test6.com", 8081, protocol.HTTP, tnow.Add(2*time.Second)),
	}
	services := append([]*model.Service(nil), servicesConflictWithStaticListener...)
	services = append(services, servicesConflictWithVirtualListener...)

	// with sidecar
	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Number:   15021,
						Protocol: "GRPC",
						Name:     "http",
					},
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   15001,
						Protocol: "HTTP",
						Name:     "http",
					},
					Hosts: []string{"*/*"},
				},
			},
		},
	}
	testcases := []struct {
		name             string
		services         []*model.Service
		sidecar          *config.Config
		expectedListener []int
	}{
		{
			name:             "service port conflict with proxy static listener",
			services:         servicesConflictWithStaticListener,
			sidecar:          nil,
			expectedListener: []int{15090, 8080},
		},
		{
			name:             "service port conflict with proxy virtual listener",
			services:         servicesConflictWithVirtualListener,
			sidecar:          nil,
			expectedListener: []int{15006, 8081},
		},
		{
			name:             "sidecar listener port conflict with proxy reserved listener",
			services:         services,
			sidecar:          sidecarConfig,
			expectedListener: []int{},
		},
	}

	proxy := getProxy()
	proxy.Metadata.EnvoyStatusPort = 15021
	proxy.Metadata.EnvoyPrometheusPort = 15090

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			listeners := buildOutboundListeners(t, proxy, nil, tc.sidecar, tc.services...)
			if len(listeners) != len(tc.expectedListener) {
				t.Logf("listeners: %v", listeners[0].GetAddress().GetSocketAddress().GetPortValue())
				t.Fatalf("expected %d listeners, found %d", len(tc.expectedListener), len(listeners))
			}
			for _, port := range tc.expectedListener {
				l := findListenerByPort(listeners, uint32(port))
				if l == nil {
					t.Fatalf("found no listener with port %d", port)
				}
			}
		})
	}
}

func TestOutboundListenerDualStackWildcard(t *testing.T) {
	test.SetForTest(t, &features.EnableDualStack, true)
	service := buildService("test1.com", "0.0.0.0", protocol.TCP, tnow.Add(1*time.Second))
	service.Attributes.ServiceRegistry = provider.External // Imitate a ServiceEntry with no addresses
	services := []*model.Service{service}
	for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
		p.DiscoverIPMode()
		listeners := buildOutboundListeners(t, p, nil, nil, services...)
		if len(listeners) != 1 {
			t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
		}
		if p.IsDualStack() {
			if len(listeners[0].AdditionalAddresses) != 1 {
				t.Fatalf("expected %d additional addresses, found %d", 1, len(listeners[0].AdditionalAddresses))
			}
			if listeners[0].AdditionalAddresses[0].GetAddress().GetSocketAddress().GetAddress() != wildcardIPv6 {
				t.Fatalf("expected additional address %s, found %s", wildcardIPv6, listeners[0].AdditionalAddresses[0].String())
			}
		}
	}
}

func TestOutboundListenerConflict(t *testing.T) {
	run := func(t *testing.T, s []*model.Service) {
		for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
			p.DiscoverIPMode()
			listeners := buildOutboundListeners(t, p, nil, nil, s...)
			if len(listeners) != 1 {
				t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
			}
		}
	}
	// Iterate over all protocol pairs and generate listeners
	// ValidateListeners will be called on all of them ensuring they are valid
	protos := []protocol.Instance{protocol.TCP, protocol.TLS, protocol.HTTP, protocol.Unsupported}
	for _, older := range protos {
		for _, newer := range protos {
			t.Run(fmt.Sprintf("%v then %v", older, newer), func(t *testing.T) {
				run(t, []*model.Service{
					buildService("test1.com", wildcardIPv4, older, tnow.Add(-1*time.Second)),
					buildService("test2.com", wildcardIPv4, newer, tnow),
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
	for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
		listeners := buildOutboundListeners(t, p, nil, nil, services...)
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
					GroupVersionKind: gvk.VirtualService,
					Name:             "test_vs",
					Namespace:        "default",
				},
				Spec: virtualServiceSpec,
			}
			for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
				listeners := buildOutboundListeners(t, p, nil, &virtualService, services...)

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
					GroupVersionKind: gvk.VirtualService,
					Name:             "test_vs",
					Namespace:        "default",
				},
				Spec: virtualServiceSpec,
			}
			for _, proxy := range []*model.Proxy{getProxy(), &dualStackProxy} {
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

			listeners := NewListenerBuilder(proxy, cg.env.PushContext()).buildSidecarOutboundListeners(proxy, cg.env.PushContext())
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

func TestOutboundListenerForExternalServices(t *testing.T) {
	svc := buildServiceWithPort("test.com", 9999, protocol.TCP, tnow)
	svc.Attributes.ServiceRegistry = provider.Kubernetes

	autoSvc := buildServiceWithPort("test.com", 9999, protocol.Unsupported, tnow)
	autoSvc.Attributes.ServiceRegistry = provider.External

	extSvc := buildServiceWithPort("example1.com", 9999, protocol.TCP, tnow)
	extSvc.Attributes.ServiceRegistry = provider.External

	tests := []struct {
		name        string
		instances   []*model.ServiceInstance
		services    []*model.Service
		listenersOn string
	}{
		{
			name: "internal k8s service with ipv4 & ipv6 endpoint for Kubernetes TCP protocol",
			instances: []*model.ServiceInstance{
				buildServiceInstance(svc, "10.10.10.10"),
				buildServiceInstance(svc, "fd00:10:244:1::11"),
			},
			services:    []*model.Service{svc},
			listenersOn: "0.0.0.0_9999",
		},
		{
			name: "external service with ipv4 & ipv6 endpoints for Kubernetes auto protocol",
			instances: []*model.ServiceInstance{
				buildServiceInstance(autoSvc, "10.10.10.10"),
				buildServiceInstance(autoSvc, "fd00:10:244:1::11"),
			},
			services:    []*model.Service{autoSvc},
			listenersOn: "::_9999",
		},
		{
			name: "external service with ipv4 & ipv6 endpoints for Kubernetes TCP protocol",
			instances: []*model.ServiceInstance{
				buildServiceInstance(extSvc, "10.10.10.10"),
				buildServiceInstance(extSvc, "fd00:10:244:1::11"),
			},
			services:    []*model.Service{extSvc},
			listenersOn: "::_9999",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{
				Services:  tt.services,
				Instances: tt.instances,
			})

			proxy := cg.SetupProxy(getIPv6Proxy())

			listeners := NewListenerBuilder(proxy, cg.env.PushContext()).buildSidecarOutboundListeners(proxy, cg.env.PushContext())
			for _, l := range listeners {
				if l.Address.GetSocketAddress().GetPortValue() == 9999 {
					if l.Name != tt.listenersOn {
						t.Errorf("Expected listeners on %s, got %s", tt.listenersOn, l.Name)
					}
				}
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
					Port: &networking.SidecarPort{
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
	svc := buildService("test.com", wildcardIPv4, protocol.HTTP, tnow)
	for _, p := range []*model.Proxy{getProxy(), &proxyHTTP10, &dualStackProxy} {
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
							TotalMatch: true,
							Port:       8080,
							HTTPFilters: []string{
								xdsfilters.MxFilterName, xdsfilters.GrpcStats.Name, xdsfilters.Fault.Name,
								xdsfilters.Cors.Name, wellknown.Router,
							},
							ValidateHCM: func(t test.Failer, hcm *hcm.HttpConnectionManager) {
								assert.Equal(t, "istio-envoy", hcm.GetServerName(), "server name")
								statPrefixDelimeter := constants.StatPrefixDelimiter
								if len(tt.cfg) == 0 {
									assert.Equal(t, "inbound_0.0.0.0_8080"+statPrefixDelimeter, hcm.GetStatPrefix(), "stat prefix")
								} else {
									// Sidecar impacts stat prefix
									assert.Equal(t, "inbound_1.1.1.1_8080"+statPrefixDelimeter, hcm.GetStatPrefix(), "stat prefix")
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

func TestOutboundTlsTrafficWithoutTimeout(t *testing.T) {
	services := []*model.Service{
		{
			CreationTime:   tnow,
			Hostname:       host.Name("test.com"),
			DefaultAddress: wildcardIPv4,
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
			DefaultAddress: wildcardIPv4,
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

var filterTestConfigs = []config.Config{
	{
		Meta: config.Meta{Name: "wasm-network-authz", Namespace: "istio-system", GroupVersionKind: gvk.WasmPlugin},
		Spec: &extensions.WasmPlugin{
			Phase: extensions.PluginPhase_AUTHZ,
			Type:  extensions.PluginType_NETWORK,
		},
	},
	{
		Meta: config.Meta{Name: "wasm-network-authn", Namespace: "istio-system", GroupVersionKind: gvk.WasmPlugin},
		Spec: &extensions.WasmPlugin{
			Phase: extensions.PluginPhase_AUTHN,
			Type:  extensions.PluginType_NETWORK,
		},
	},
	{
		Meta: config.Meta{Name: "wasm-network-stats", Namespace: "istio-system", GroupVersionKind: gvk.WasmPlugin},
		Spec: &extensions.WasmPlugin{
			Phase: extensions.PluginPhase_STATS,
			Type:  extensions.PluginType_NETWORK,
		},
	},
	{
		Meta: config.Meta{Name: "wasm-authz", Namespace: "istio-system", GroupVersionKind: gvk.WasmPlugin},
		Spec: &extensions.WasmPlugin{
			Phase: extensions.PluginPhase_AUTHZ,
		},
	},
	{
		Meta: config.Meta{Name: "wasm-authn", Namespace: "istio-system", GroupVersionKind: gvk.WasmPlugin},
		Spec: &extensions.WasmPlugin{
			Phase: extensions.PluginPhase_AUTHN,
		},
	},
	{
		Meta: config.Meta{Name: "wasm-stats", Namespace: "istio-system", GroupVersionKind: gvk.WasmPlugin},
		Spec: &extensions.WasmPlugin{
			Phase: extensions.PluginPhase_STATS,
		},
	},
	{
		Meta: config.Meta{Name: uuid.NewString(), Namespace: "istio-system", GroupVersionKind: gvk.AuthorizationPolicy},
		Spec: &security.AuthorizationPolicy{},
	},
	{
		Meta: config.Meta{Name: uuid.NewString(), Namespace: "istio-system", GroupVersionKind: gvk.AuthorizationPolicy},
		Spec: &security.AuthorizationPolicy{
			Selector:     nil,
			TargetRef:    nil,
			Rules:        nil,
			Action:       security.AuthorizationPolicy_CUSTOM,
			ActionDetail: &security.AuthorizationPolicy_Provider{Provider: &security.AuthorizationPolicy_ExtensionProvider{Name: "extauthz"}},
		},
	},
	{
		Meta: config.Meta{Name: uuid.NewString(), Namespace: "istio-system", GroupVersionKind: gvk.Telemetry},
		Spec: &telemetry.Telemetry{
			Metrics: []*telemetry.Metrics{{Providers: []*telemetry.ProviderRef{{Name: "prometheus"}}}},
		},
	},
}

func TestOutboundFilters(t *testing.T) {
	mc := mesh.DefaultMeshConfig()
	mc.ExtensionProviders = append(mc.ExtensionProviders, &meshconfig.MeshConfig_ExtensionProvider{
		Name: "extauthz",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc{
			EnvoyExtAuthzGrpc: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider{
				Service: "foo/example.local",
				Port:    1234,
			},
		},
	})

	t.Run("HTTP", func(t *testing.T) {
		cg := NewConfigGenTest(t, TestOptions{
			Services:   []*model.Service{buildService("test.com", wildcardIPv4, protocol.HTTP, tnow)},
			Configs:    filterTestConfigs,
			MeshConfig: mc,
		})
		proxy := cg.SetupProxy(getProxy())
		listeners := NewListenerBuilder(proxy, cg.env.PushContext()).buildSidecarOutboundListeners(proxy, cg.env.PushContext())
		xdstest.ValidateListeners(t, listeners)
		l := xdstest.ExtractListener("0.0.0.0_8080", listeners)
		listenertest.VerifyListener(t, l, listenertest.ListenerTest{
			FilterChains: []listenertest.FilterChainTest{
				{
					TotalMatch: true,
					HTTPFilters: []string{
						xdsfilters.MxFilterName,
						"extensions.istio.io/wasmplugin/istio-system.wasm-authn",
						"extensions.istio.io/wasmplugin/istio-system.wasm-authz",
						"extensions.istio.io/wasmplugin/istio-system.wasm-stats",
						wellknown.HTTPGRPCStats,
						xdsfilters.AlpnFilterName,
						xdsfilters.Fault.Name,
						xdsfilters.Cors.Name,
						xds.StatsFilterName,
						wellknown.Router,
					},
					NetworkFilters: []string{
						"extensions.istio.io/wasmplugin/istio-system.wasm-network-authn",
						"extensions.istio.io/wasmplugin/istio-system.wasm-network-authz",
						"extensions.istio.io/wasmplugin/istio-system.wasm-network-stats",
						wellknown.HTTPConnectionManager,
					},
				},
			},
		})
	})

	t.Run("TCP", func(t *testing.T) {
		cg := NewConfigGenTest(t, TestOptions{
			Services:   []*model.Service{buildService("test.com", wildcardIPv4, protocol.TCP, tnow)},
			Configs:    filterTestConfigs,
			MeshConfig: mc,
		})
		proxy := cg.SetupProxy(getProxy())
		listeners := NewListenerBuilder(proxy, cg.env.PushContext()).buildSidecarOutboundListeners(proxy, cg.env.PushContext())
		xdstest.ValidateListeners(t, listeners)
		l := xdstest.ExtractListener("0.0.0.0_8080", listeners)
		listenertest.VerifyListener(t, l, listenertest.ListenerTest{
			FilterChains: []listenertest.FilterChainTest{
				{
					TotalMatch: true,
					NetworkFilters: []string{
						"extensions.istio.io/wasmplugin/istio-system.wasm-network-authn",
						"extensions.istio.io/wasmplugin/istio-system.wasm-network-authz",
						"extensions.istio.io/wasmplugin/istio-system.wasm-network-stats",
						xds.StatsFilterName,
						wellknown.TCPProxy,
					},
				},
			},
		})
	})
}

func TestOutboundTls(t *testing.T) {
	services := []*model.Service{
		{
			CreationTime:   tnow,
			Hostname:       host.Name("test.com"),
			DefaultAddress: wildcardIPv4,
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
	for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
		buildOutboundListeners(t, p, &virtualService2, &virtualService, services...)
	}
}

// Checks for scenarios like https://github.com/istio/istio/issues/49476
func TestOutboundTLSIPv6Only(t *testing.T) {
	services := []*model.Service{
		{
			CreationTime:   tnow,
			Hostname:       host.Name("test.com"),
			DefaultAddress: wildcardIPv4,
			Ports: model.PortList{
				&model.Port{
					Name:     "https",
					Port:     8080,
					Protocol: protocol.HTTPS,
				},
			},
			Resolution: model.Passthrough,
			Attributes: model.ServiceAttributes{
				Namespace:       "default",
				ServiceRegistry: provider.External,
			},
		},
	}

	p := getIPv6Proxy()
	listeners := buildOutboundListeners(t, p, nil, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	l := listeners[0]
	for _, fc := range l.FilterChains {
		if fc.FilterChainMatch == nil {
			t.Fatalf("expected filter chain match for chain %s to be set, found nil", fc.Name)
		}
		if len(fc.FilterChainMatch.ServerNames) == 0 {
			t.Fatalf("expected SNI to be set, found %v", fc.FilterChainMatch.ServerNames)
		}
	}
	log.Printf("Listeners: %v", listeners)
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
					Port: &networking.SidecarPort{
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
	services := []*model.Service{buildService("httpbin.com", wildcardIPv4, protocol.HTTP, tnow.Add(1*time.Second))}

	for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
		listeners := buildOutboundListeners(t, p, sidecarConfig, nil, services...)

		if expected := 1; len(listeners) != expected {
			t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
		}
		l := findListenerByPort(listeners, 15080)
		if l == nil {
			t.Fatalf("expected listener on port %d, but not found", 15080)
		}
		if len(l.FilterChains) != 1 {
			t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
		} else {
			if !isHTTPFilterChain(l.FilterChains[0]) {
				t.Fatalf("expected http filter chain, found %s", l.FilterChains[1].Filters[0].Name)
			}
			if len(l.ListenerFilters) > 0 {
				t.Fatalf("expected %d listener filter, found %d", 0, len(l.ListenerFilters))
			}
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

func TestGetDualStackActualWildcard(t *testing.T) {
	tests := []struct {
		name     string
		proxy    *model.Proxy
		expected []string
	}{
		{
			name: "ipv4 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1.1.1.1", "127.0.0.1", "2.2.2.2"},
			},
			expected: []string{WildcardAddress},
		},
		{
			name: "ipv6 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "2222:3333::1"},
			},
			expected: []string{WildcardIPv6Address},
		},
		{
			name: "mixed ipv4 and ipv6",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "127.0.0.1", "2.2.2.2", "2222:3333::1"},
			},
			expected: []string{WildcardAddress, WildcardIPv6Address},
		},
	}
	for _, tt := range tests {
		tt.proxy.DiscoverIPMode()
		actualWildcards, _ := getWildcardsAndLocalHost(tt.proxy.GetIPMode())
		if len(actualWildcards) != len(tt.expected) {
			t.Errorf("Test %s failed, expected: %v got: %v", tt.name, tt.expected, actualWildcards)
		}
	}
}

func TestGetDualStackLocalHost(t *testing.T) {
	tests := []struct {
		name     string
		proxy    *model.Proxy
		expected []string
	}{
		{
			name: "ipv4 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1.1.1.1", "127.0.0.1", "2.2.2.2"},
			},
			expected: []string{LocalhostAddress},
		},
		{
			name: "ipv6 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "2222:3333::1"},
			},
			expected: []string{LocalhostIPv6Address},
		},
		{
			name: "mixed ipv4 and ipv6",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "127.0.0.1", "2.2.2.2", "2222:3333::1"},
			},
			expected: []string{LocalhostAddress, LocalhostIPv6Address},
		},
	}
	for _, tt := range tests {
		tt.proxy.DiscoverIPMode()
		_, actualLocalHosts := getWildcardsAndLocalHost(tt.proxy.GetIPMode())
		if len(actualLocalHosts) != len(tt.expected) {
			t.Errorf("Test %s failed, expected: %v got: %v", tt.name, tt.expected, actualLocalHosts)
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
						Port: &networking.SidecarPort{
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
							Port: &networking.SidecarPort{
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
					for _, proxy := range []*model.Proxy{getProxy(), &dualStackProxy} {
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
					}
				})
			}
		})
	}
}

func testOutboundListenerRoute(t *testing.T, services ...*model.Service) {
	t.Helper()
	for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
		listeners := buildOutboundListeners(t, p, nil, nil, services...)
		if len(listeners) != 3 {
			t.Fatalf("expected %d listeners, found %d", 3, len(listeners))
		}

		l := findListenerByAddress(listeners, wildcardIPv4)
		if l == nil {
			t.Fatalf("expect listener %s", "0.0.0.0_8080")
		}

		f := l.FilterChains[0].Filters[0]
		cfg, _ := protomarshal.MessageToStructSlow(f.GetTypedConfig())
		rds := cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
		if rds != "8080" {
			t.Fatalf("expect routes %s, found %s", "8080", rds)
		}

		l = findListenerByAddress(listeners, "1.2.3.4")
		if l == nil {
			t.Fatalf("expect listener %s", "1.2.3.4_8080")
		}
		f = l.FilterChains[0].Filters[0]
		cfg, _ = protomarshal.MessageToStructSlow(f.GetTypedConfig())
		rds = cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
		if rds != "test1.com:8080" {
			t.Fatalf("expect routes %s, found %s", "test1.com:8080", rds)
		}

		l = findListenerByAddress(listeners, "3.4.5.6")
		if l == nil {
			t.Fatalf("expect listener %s", "3.4.5.6_8080")
		}
		f = l.FilterChains[0].Filters[0]
		cfg, _ = protomarshal.MessageToStructSlow(f.GetTypedConfig())
		rds = cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
		if rds != "test3.com:8080" {
			t.Fatalf("expect routes %s, found %s", "test3.com:8080", rds)
		}
	}
}

func testOutboundListenerFilterTimeout(t *testing.T, services ...*model.Service) {
	for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
		listeners := buildOutboundListeners(t, p, nil, nil, services...)
		if len(listeners) != 2 {
			t.Fatalf("expected %d listeners, found %d", 2, len(listeners))
		}

		explicit := xdstest.ExtractListener("0.0.0.0_8080", listeners)
		if explicit.ListenerFiltersTimeout == nil {
			t.Fatalf("expected timeout disabled, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
				explicit.ContinueOnListenerFiltersTimeout,
				explicit.ListenerFiltersTimeout)
		}

		auto := xdstest.ExtractListener("0.0.0.0_9090", listeners)
		if !auto.ContinueOnListenerFiltersTimeout || auto.ListenerFiltersTimeout == nil {
			t.Fatalf("expected timeout enabled, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
				auto.ContinueOnListenerFiltersTimeout,
				auto.ListenerFiltersTimeout)
		}
	}
}

func testOutboundListenerConflict(t *testing.T, services ...*model.Service) {
	oldestService := getOldestService(services...)
	for _, proxy := range []*model.Proxy{getProxy(), &dualStackProxy} {
		proxy.DiscoverIPMode()
		listeners := buildOutboundListeners(t, proxy, nil, nil, services...)
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
				t.Fatalf("expected %d filter chains, found %d", 1, len(listeners[0].FilterChains))
			}
			if !isHTTPFilterChain(listeners[0].FilterChains[0]) {
				t.Fatalf("expected http filter chain, found %s", listeners[0].FilterChains[0].Filters[0].Name)
			}

			if !isTCPFilterChain(listeners[0].DefaultFilterChain) {
				t.Fatalf("expected tcp filter chain, found %s", listeners[0].DefaultFilterChain.Filters[0].Name)
			}

			verifyHTTPFilterChainMatch(t, listeners[0].FilterChains[0])
			verifyHTTPListenerFilters(t, listeners[0].ListenerFilters)

			if listeners[0].ListenerFiltersTimeout.GetSeconds() != 5 {
				t.Fatalf("expected timeout 5s, found  ListenerFiltersTimeout %v",
					listeners[0].ListenerFiltersTimeout)
			}

			f := listeners[0].FilterChains[0].Filters[0]
			cfg, _ := protomarshal.MessageToStructSlow(f.GetTypedConfig())
			rds := cfg.Fields["rds"].GetStructValue().Fields["route_config_name"].GetStringValue()
			expect := fmt.Sprintf("%d", oldestService.Ports[0].Port)
			if rds != expect {
				t.Fatalf("expect routes %s, found %s", expect, rds)
			}
		} else {
			if len(listeners[0].FilterChains) != 1 {
				t.Fatalf("expected %d filter chains, found %d", 1, len(listeners[0].FilterChains))
			}
			if listeners[0].DefaultFilterChain == nil {
				t.Fatal("expected default filter chains, found none")
			}

			_ = getTCPFilterChain(t, listeners[0])
			http := getHTTPFilterChain(t, listeners[0])

			verifyHTTPFilterChainMatch(t, http)
			verifyHTTPListenerFilters(t, listeners[0].ListenerFilters)

			if listeners[0].ListenerFiltersTimeout == nil {
				t.Fatalf("expected timeout, found ContinueOnListenerFiltersTimeout %v, ListenerFiltersTimeout %v",
					listeners[0].ContinueOnListenerFiltersTimeout,
					listeners[0].ListenerFiltersTimeout)
			}
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
	t.Fatal("tcp filter chain not found")
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
	t.Fatal("tcp filter chain not found")
	return nil
}

func testInboundListenerConfig(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	listeners := buildListeners(t, TestOptions{Services: services}, proxy)
	verifyFilterChainMatch(t, xdstest.ExtractListener(model.VirtualInboundListenerName, listeners))
}

func testInboundListenerConfigWithConflictPort(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
	t.Helper()
	listeners := buildListeners(t, TestOptions{Services: services}, proxy)
	virtualListener := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	for _, fc := range virtualListener.FilterChains {
		if fc.FilterChainMatch.DestinationPort.GetValue() == 15021 {
			t.Fatal("port 15021 should not be included in inbound listener")
		}
	}
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

func testInboundListenerConfigWithSidecarIngressPortMergeServicePort(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
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
					Port: &networking.SidecarPort{
						Number:   8083,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:8083",
				},
				{
					Port: &networking.SidecarPort{
						Number:   8084,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:8084",
				},
				{
					// not conflict with service port
					Port: &networking.SidecarPort{
						Number:   80,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:80",
				},
				{
					// conflict with service target port
					Port: &networking.SidecarPort{
						Number:   8080,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:8080",
				},
			},
		},
	}
	listeners := buildListeners(t, TestOptions{
		Services: services,
		Configs:  []config.Config{sidecarConfig},
	}, proxy)
	l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	if len(l.FilterChains) != 14 {
		t.Fatalf("expected %d listener filter chains, found %d", 14, len(l.FilterChains))
	}
	verifyFilterChainMatch(t, l)
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
					Port: &networking.SidecarPort{
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

func testInboundListenerConfigWithSidecarConflictPort(t *testing.T, proxy *model.Proxy, services ...*model.Service) {
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
					Port: &networking.SidecarPort{
						Number:   15021,
						Protocol: "unknown",
						Name:     "uds",
					},
					CaptureMode:     2, // None
					Bind:            "0.0.0.0",
					DefaultEndpoint: "127.0.0.1:80",
				},
			},
		},
	}
	listeners := buildListeners(t, TestOptions{
		Services: services,
		Configs:  []config.Config{sidecarConfig},
	}, proxy)
	for _, l := range listeners {
		if l.Name == "1.1.1.1_15021" {
			t.Fatalf("unexpected listener with name %s", l.Name)
		}
	}
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
					Port: &networking.SidecarPort{
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

func verifyHTTPListenerFilters(t *testing.T, lfilters []*listener.ListenerFilter) {
	t.Helper()
	if len(lfilters) != 1 {
		t.Fatalf("expected %d listener filter, found %d", 1, len(lfilters))
	}
	if lfilters[0].Name != wellknown.HTTPInspector {
		t.Fatalf("expected listener filters not found, got %v", lfilters)
	}
}

func verifyHTTPFilterChainMatch(t *testing.T, fc *listener.FilterChain) {
	t.Helper()
	if fc.FilterChainMatch.TransportProtocol != xdsfilters.RawBufferTransportProtocol {
		t.Fatalf("expect %q transport protocol, found %q", xdsfilters.RawBufferTransportProtocol, fc.FilterChainMatch.TransportProtocol)
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
					Port: &networking.SidecarPort{
						Number:   9000,
						Protocol: "GRPC",
						Name:     "uds",
					},
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   3306,
						Protocol: string(protocol.MySQL),
						Name:     "MySQL",
					},
					Bind:  "8.8.8.8",
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.SidecarPort{
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
	test.SetForTest(t, &features.EnableMysqlFilter, true)
	for _, p := range []*model.Proxy{getProxy(), &dualStackProxy} {
		listeners := buildOutboundListeners(t, p, sidecarConfig, nil, services...)
		if len(listeners) != 4 {
			t.Fatalf("expected %d listeners, found %d", 4, len(listeners))
		}

		l := findListenerByPort(listeners, 8080)
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
		verifyHTTPListenerFilters(t, l.ListenerFilters)

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
		verifyHTTPListenerFilters(t, l.ListenerFilters)
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
				t.Fatal("did not generate virtual inbound listener")
			}

			if l := xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners); l == nil {
				t.Fatal("did not generate virtual outbound listener")
			}
		})
	}
}

func TestVirtualListeners_TrafficRedirectionDisabled(t *testing.T) {
	listeners := buildListeners(t, TestOptions{}, &model.Proxy{Metadata: &model.NodeMetadata{InterceptionMode: model.InterceptionNone}})
	if l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners); l != nil {
		t.Fatal("unexpectedly generated virtual inbound listener")
	}

	if l := xdstest.ExtractListener(model.VirtualOutboundListenerName, listeners); l != nil {
		t.Fatal("unexpectedly generated virtual outbound listener")
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
		t.Fatal("nil listener")
	}

	fc := &tcp.TcpProxy{}
	if err := getFilterConfig(xdstest.ExtractFilterChain("virtualOutbound-catchall-tcp", l).Filters[0], fc); err != nil {
		t.Fatalf("failed to get TCP Proxy config: %s", err)
	}
	if fc.AccessLog == nil {
		t.Fatal("expected access log configuration")
	}
	cfg, _ := protomarshal.MessageToStructSlow(fc.AccessLog[0].GetTypedConfig())
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
	cfg, _ := protomarshal.MessageToStructSlow(f.GetTypedConfig())

	if httpProxy.Address.GetSocketAddress().GetPortValue() != 15007 {
		t.Log(xdstest.Dump(t, httpProxy))
		t.Fatalf("expected http proxy is not listening on %d, but on port %d", 15007,
			httpProxy.Address.GetSocketAddress().GetPortValue())
	}
	if !strings.HasPrefix(cfg.Fields["stat_prefix"].GetStringValue(), "outbound_") {
		t.Log(xdstest.Dump(t, httpProxy))
		t.Fatalf("expected http proxy stat prefix to have outbound, %s", cfg.Fields["stat_prefix"].GetStringValue())
	}
}

func TestHttpProxyListenerPerWorkload(t *testing.T) {
	listeners := buildListeners(t, TestOptions{}, &model.Proxy{Metadata: &model.NodeMetadata{HTTPProxyPort: "15007"}})
	httpProxy := xdstest.ExtractListener("127.0.0.1_15007", listeners)
	f := httpProxy.FilterChains[0].Filters[0]
	cfg, _ := protomarshal.MessageToStructSlow(f.GetTypedConfig())

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
				test.SetForTest(t, &features.TraceSampling, tc.envPilotSampling)
			}

			m := mesh.DefaultMeshConfig()
			m.ProxyHttpPort = 15007
			m.EnableTracing = true
			m.DefaultConfig = &meshconfig.ProxyConfig{
				Tracing: &meshconfig.Tracing{
					CustomTags:       tc.in.CustomTags,
					MaxPathTagLength: tc.in.MaxPathTagLength,
					Sampling:         tc.in.Sampling,
				},
				DiscoveryAddress: "istiod.istio-system.svc:15012",
			}
			listeners := buildListeners(t, TestOptions{MeshConfig: m}, nil)
			httpProxy := xdstest.ExtractListener("127.0.0.1_15007", listeners)
			f := httpProxy.FilterChains[0].Filters[0]
			verifyHTTPConnectionManagerFilter(t, f, tc.out, tc.name)
		})
	}
}

func customTracingTags() []*tracing.CustomTag {
	return append(slices.Clone(optionalPolicyTags),
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
			Tag: "istio.cluster_id",
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
		buildService("test1.com", wildcardIPv4, protocol.HTTP, tnow),
	}
	listeners := buildListeners(t, TestOptions{Services: services}, nil)
	l := xdstest.ExtractListener("0.0.0.0_8080", listeners)
	if l == nil {
		t.Fatal("failed to find listener")
	}
	if len(l.FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
	}

	verifyHTTPFilterChainMatch(t, l.FilterChains[0])
	verifyPassThroughTCPFilterChain(t, l.DefaultFilterChain)
	verifyHTTPListenerFilters(t, l.ListenerFilters)
}

func verifyPassThroughTCPFilterChain(t *testing.T, fc *listener.FilterChain) {
	t.Helper()
	f := fc.Filters[0]
	expectedStatPrefix := util.PassthroughCluster
	cfg, _ := protomarshal.MessageToStructSlow(f.GetTypedConfig())
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
		t.Fatal("expected TCP filters, found none")
	}
	expectedStatPrefix := fmt.Sprintf("outbound|8080||%s", hostname)
	cfg, _ := protomarshal.MessageToStructSlow(f.GetTypedConfig())
	statPrefix := cfg.Fields["stat_prefix"].GetStringValue()
	if statPrefix != expectedStatPrefix {
		t.Fatalf("expected listener to contain stat_prefix %s, found %s", expectedStatPrefix, statPrefix)
	}
}

func verifyFilterChainMatch(t *testing.T, listener *listener.Listener) {
	t.Helper()
	httpFilters := []string{
		xdsfilters.MxFilterName,
		xdsfilters.GrpcStats.Name,
		xdsfilters.Fault.Name,
		xdsfilters.Cors.Name,
		wellknown.Router,
	}
	httpNetworkFilters := []string{xdsfilters.MxFilterName, wellknown.HTTPConnectionManager}
	tcpNetworkFilters := []string{xdsfilters.MxFilterName, wellknown.TCPProxy}
	verifyInboundFilterChains(t, listener, httpFilters, httpNetworkFilters, tcpNetworkFilters)
}

func verifyInboundFilterChains(t *testing.T, listener *listener.Listener, httpFilters []string, httpNetworkFilters []string, tcpNetworkFilters []string) {
	t.Helper()
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
				NetworkFilters: httpNetworkFilters,
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundCatchAllHTTPFilterChainName,
				Type:           listenertest.PlainHTTP,
				HTTPFilters:    httpFilters,
				NetworkFilters: httpNetworkFilters,
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundListenerName,
				Type:           listenertest.MTLSTCP,
				HTTPFilters:    []string{},
				NetworkFilters: tcpNetworkFilters,
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundListenerName,
				Type:           listenertest.PlainTCP,
				HTTPFilters:    []string{},
				NetworkFilters: tcpNetworkFilters,
				TotalMatch:     true,
			},
			{
				Name:           model.VirtualInboundListenerName,
				Type:           listenertest.StandardTLS,
				HTTPFilters:    []string{},
				NetworkFilters: tcpNetworkFilters,
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
	m := mesh.DefaultMeshConfig()
	m.ProtocolDetectionTimeout = durationpb.New(5 * time.Second)
	cg := NewConfigGenTest(t, TestOptions{
		Services:       services,
		ConfigPointers: []*config.Config{sidecarConfig, virtualService},
		MeshConfig:     m,
	})
	listeners := NewListenerBuilder(proxy, cg.env.PushContext()).buildSidecarOutboundListeners(cg.SetupProxy(proxy), cg.env.PushContext())
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
		DefaultAddress: wildcardIPv4,
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
			Addresses:       []string{instanceIP},
			ServicePortName: service.Ports[0].Name,
		},
		ServicePort: service.Ports[0],
		Service:     service,
	}
}

func TestOutboundListenerConfig_WithAutoAllocatedAddress(t *testing.T) {
	const tcpPort = 79
	services := []*model.Service{
		{
			CreationTime:             tnow.Add(1 * time.Second),
			Hostname:                 host.Name("test1.com"),
			DefaultAddress:           wildcardIPv4,
			AutoAllocatedIPv4Address: "240.240.0.100",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp",
					Port:     tcpPort,
					Protocol: protocol.TCP,
				},
			},
			Resolution: model.DNSLB,
			Attributes: model.ServiceAttributes{
				Namespace: "default",
			},
		},
	}

	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "sidecar-with-tcp",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"default/*"},
				},
			},
		},
	}

	sidecarConfigWithPort := &config.Config{
		Meta: config.Meta{
			Name:             "sidecar-with-tcp-port",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"default/*"},
					Port: &networking.SidecarPort{
						Number:   tcpPort,
						Protocol: "TCP",
						Name:     "tcp",
					},
				},
			},
		},
	}

	tests := []struct {
		name                      string
		services                  []*model.Service
		sidecar                   *config.Config
		numListenersOnServicePort int
		useAutoAllocatedAddress   bool
	}{
		{
			name:                      "egress tcp with auto allocated address",
			services:                  services,
			sidecar:                   sidecarConfig,
			numListenersOnServicePort: 1,
			useAutoAllocatedAddress:   true,
		},
		{
			name:                      "egress tcp and port with auto allocated address",
			services:                  services,
			sidecar:                   sidecarConfigWithPort,
			numListenersOnServicePort: 1,
			useAutoAllocatedAddress:   true,
		},
	}

	proxy := getProxy()
	proxy.Metadata.DNSCapture = true
	proxy.Metadata.DNSAutoAllocate = true

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listeners := buildOutboundListeners(t, proxy, tt.sidecar, nil, services...)

			listenersToCheck := make([]string, 0)
			for _, l := range listeners {
				if l.Address.GetSocketAddress().GetPortValue() == tcpPort {
					listenersToCheck = append(listenersToCheck, l.Address.GetSocketAddress().GetAddress())
				}
			}

			if len(listenersToCheck) != tt.numListenersOnServicePort {
				t.Errorf("Expected %d listeners, got %d (%v)", tt.numListenersOnServicePort, len(listenersToCheck), listenersToCheck)
			}

			if tt.useAutoAllocatedAddress {
				for _, addr := range listenersToCheck {
					if !strings.HasPrefix(addr, "240.240") {
						t.Errorf("Expected %d listeners on service port 79, got %d (%v)", tt.numListenersOnServicePort, len(listenersToCheck), listenersToCheck)
					}
				}
			}
		})
	}
}

func TestListenerTransportSocketConnectTimeoutForSidecar(t *testing.T) {
	cases := []struct {
		name            string
		expectedTimeout int64
		services        []*model.Service
	}{
		{
			name:            "should set timeout",
			expectedTimeout: durationpb.New(defaultGatewayTransportSocketConnectTimeout).GetSeconds(),
			services: []*model.Service{
				buildService("test.com", "1.2.3.4", protocol.TCP, tnow.Add(1*time.Second)),
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := getProxy()
			listeners := buildOutboundListeners(t, p, nil, nil, tt.services...)
			for _, l := range listeners {
				for _, fc := range l.FilterChains {
					if fc.TransportSocketConnectTimeout == nil || fc.TransportSocketConnectTimeout.Seconds != tt.expectedTimeout {
						t.Errorf("expected transport socket connect timeout to be %v for listener %s filter chain %s, got %v",
							tt.expectedTimeout, l.Name, fc.Name, fc.TransportSocketConnectTimeout)
					}
				}
				if l.DefaultFilterChain != nil {
					fc := l.DefaultFilterChain
					if fc.TransportSocketConnectTimeout == nil || fc.TransportSocketConnectTimeout.Seconds != tt.expectedTimeout {
						t.Errorf("expected transport socket connect timeout to be %v for listener %s default filter chain, got %v",
							tt.expectedTimeout, l.Name, fc.TransportSocketConnectTimeout)
					}
				}
			}
		})
	}
}

func TestBuildListenerTLSContext(t *testing.T) {
	tests := []struct {
		name                    string
		serverTLSSettings       *networking.ServerTLSSettings
		proxy                   *model.Proxy
		push                    *model.PushContext
		transportProtocol       istionetworking.TransportProtocol
		gatewayTCPServerWithTLS bool
		expectedCertCount       int
		expectedValidation      bool
	}{
		{
			name: "single certificate with credential name",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "test-cert",
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push:                    &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       1,
			expectedValidation:      false,
		},
		{
			name: "multiple certificates with credential names",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:            networking.ServerTLSSettings_SIMPLE,
				CredentialNames: []string{"rsa-cert", "ecdsa-cert"},
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push:                    &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       2,
			expectedValidation:      false,
		},
		{
			name: "multiple certificates with mutual TLS",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:            networking.ServerTLSSettings_MUTUAL,
				CredentialNames: []string{"rsa-cert", "ecdsa-cert"},
				SubjectAltNames: []string{"test.com"},
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push:                    &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       2,
			expectedValidation:      true,
		},
		{
			name: "SIMPLE and Server certificate",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "/path/to/cert.pem",
				PrivateKey:        "/path/to/key.pem",
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push:                    &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       1,
			expectedValidation:      false,
		},
		{
			name: "SIMPLE and TLS certificates",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode: networking.ServerTLSSettings_SIMPLE,
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "/path/to/cert.pem",
						PrivateKey:        "/path/to/key.pem",
					},
					{
						ServerCertificate: "/path/to/cert2.pem",
						PrivateKey:        "/path/to/key2.pem",
					},
				},
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push:                    &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       2,
			expectedValidation:      false,
		},
		{
			name: "MUTUAL and server certificate",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				CaCertificates:    "/path/to/ca.pem",
				ServerCertificate: "/path/to/cert.pem",
				PrivateKey:        "/path/to/key.pem",
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push:                    &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       1,
			expectedValidation:      true,
		},
		{
			name: "MUTUAL and TLS certificates",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_MUTUAL,
				CaCertificates: "/path/to/ca.pem",
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "/path/to/cert.pem",
						PrivateKey:        "/path/to/key.pem",
					},
					{
						ServerCertificate: "/path/to/cert2.pem",
						PrivateKey:        "/path/to/key2.pem",
					},
				},
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push:                    &model.PushContext{Mesh: &meshconfig.MeshConfig{}},
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       2,
			expectedValidation:      true,
		},
		{
			name: "external SDS provider with credential name",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "sds://provider-cert",
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push: func() *model.PushContext {
				pc := &model.PushContext{
					Mesh: &meshconfig.MeshConfig{
						ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
							{
								Name: "provider-cert",
								Provider: &meshconfig.MeshConfig_ExtensionProvider_Sds{
									Sds: &meshconfig.MeshConfig_ExtensionProvider_SDSProvider{
										Name:    "provider-cert",
										Service: "sds-provider-service",
										Port:    8080,
									},
								},
							},
						},
					},
				}
				pc.ServiceIndex.HostnameAndNamespace = map[host.Name]map[string]*model.Service{
					"sds-provider-service": {
						"": &model.Service{
							Hostname: "sds-provider-service",
							Ports: []*model.Port{
								{
									Name:     "grpc",
									Port:     8080,
									Protocol: protocol.GRPC,
								},
							},
						},
					},
				}
				return pc
			}(),
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       1,
			expectedValidation:      false,
		},
		{
			name: "external SDS provider with mutual TLS",
			serverTLSSettings: &networking.ServerTLSSettings{
				Mode:            networking.ServerTLSSettings_MUTUAL,
				CredentialName:  "sds://provider-cert",
				SubjectAltNames: []string{"test.com"},
			},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			push: func() *model.PushContext {
				pc := &model.PushContext{
					Mesh: &meshconfig.MeshConfig{
						ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
							{
								Name: "provider-cert",
								Provider: &meshconfig.MeshConfig_ExtensionProvider_Sds{
									Sds: &meshconfig.MeshConfig_ExtensionProvider_SDSProvider{
										Name:    "provider-cert",
										Service: "sds-provider-service",
										Port:    8080,
									},
								},
							},
						},
					},
				}
				pc.ServiceIndex.HostnameAndNamespace = map[host.Name]map[string]*model.Service{
					"sds-provider-service": {
						"": &model.Service{
							Hostname: "sds-provider-service",
							Ports: []*model.Port{
								{
									Name:     "grpc",
									Port:     8080,
									Protocol: protocol.GRPC,
								},
							},
						},
					},
				}
				return pc
			}(),
			transportProtocol:       istionetworking.TransportProtocolTCP,
			gatewayTCPServerWithTLS: false,
			expectedCertCount:       1,
			expectedValidation:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := BuildListenerTLSContext(tt.serverTLSSettings, tt.proxy, tt.push, tt.transportProtocol, tt.gatewayTCPServerWithTLS)

			// Check certificate count
			if len(ctx.CommonTlsContext.TlsCertificateSdsSecretConfigs) != tt.expectedCertCount {
				t.Errorf("expected %d certificates, got %d", tt.expectedCertCount, len(ctx.CommonTlsContext.TlsCertificateSdsSecretConfigs))
			}

			// Check validation context
			if tt.expectedValidation {
				if ctx.CommonTlsContext.ValidationContextType == nil {
					t.Error("expected validation context to be set")
				}
				combinedCtx, ok := ctx.CommonTlsContext.ValidationContextType.(*tls.CommonTlsContext_CombinedValidationContext)
				if !ok {
					t.Error("expected CombinedValidationContext")
				}
				if combinedCtx.CombinedValidationContext == nil {
					t.Error("expected CombinedValidationContext to be set")
				}
			} else if ctx.CommonTlsContext.ValidationContextType != nil {
				t.Error("unexpected validation context")
			}
		})
	}
}
