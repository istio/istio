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
	"reflect"
	"testing"
	"time"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	redis "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/redis_proxy/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/durationpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestBuildRedisFilter(t *testing.T) {
	redisFilter := buildRedisFilter("redis", "redis-cluster")
	if redisFilter.Name != wellknown.RedisProxy {
		t.Errorf("redis filter name is %s not %s", redisFilter.Name, wellknown.RedisProxy)
	}
	if config, ok := redisFilter.ConfigType.(*listener.Filter_TypedConfig); ok {
		redisProxy := redis.RedisProxy{}
		if err := config.TypedConfig.UnmarshalTo(&redisProxy); err != nil {
			t.Errorf("unmarshal failed: %v", err)
		}
		if redisProxy.StatPrefix != "redis" {
			t.Errorf("redis proxy statPrefix is %s", redisProxy.StatPrefix)
		}
		if !redisProxy.LatencyInMicros {
			t.Errorf("redis proxy latency stat is not configured for microseconds")
		}
		if redisProxy.PrefixRoutes.CatchAllRoute.Cluster != "redis-cluster" {
			t.Errorf("redis proxy's PrefixRoutes.CatchAllCluster is %s", redisProxy.PrefixRoutes.CatchAllRoute.Cluster)
		}
	} else {
		t.Errorf("redis filter type is %T not listener.Filter_TypedConfig ", redisFilter.ConfigType)
	}
}

func TestInboundNetworkFilterStatPrefix(t *testing.T) {
	cases := []struct {
		name               string
		statPattern        string
		expectedStatPrefix string
	}{
		{
			"no pattern",
			"",
			"inbound|8888||",
		},
		{
			"service only pattern",
			"%SERVICE%",
			"v0.default.example.org",
		},
	}

	services := []*model.Service{
		buildService("test.com", "10.10.0.0/24", protocol.TCP, tnow),
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := mesh.DefaultMeshConfig()
			m.InboundClusterStatName = tt.statPattern
			cg := NewConfigGenTest(t, TestOptions{
				Services:   services,
				MeshConfig: m,
			})

			fcc := inboundChainConfig{
				telemetryMetadata: telemetry.FilterChainMetadata{InstanceHostname: "v0.default.example.org"},
				clusterName:       "inbound|8888||",
			}

			listenerFilters := NewListenerBuilder(cg.SetupProxy(nil), cg.PushContext()).buildInboundNetworkFilters(fcc)
			tcp := &tcp.TcpProxy{}
			listenerFilters[len(listenerFilters)-1].GetTypedConfig().UnmarshalTo(tcp)
			if tcp.StatPrefix != tt.expectedStatPrefix {
				t.Fatalf("Unexpected Stat Prefix, Expecting %s, Got %s", tt.expectedStatPrefix, tcp.StatPrefix)
			}
		})
	}
}

func TestInboundNetworkFilterIdleTimeout(t *testing.T) {
	cases := []struct {
		name        string
		idleTimeout string
		expected    *durationpb.Duration
	}{
		{
			"no idle timeout",
			"",
			nil,
		},
		{
			"invalid timeout",
			"invalid-30s",
			nil,
		},
		{
			"valid idle timeout 30s",
			"30s",
			durationpb.New(30 * time.Second),
		},
	}

	services := []*model.Service{
		buildService("test.com", "10.10.0.0/24", protocol.TCP, tnow),
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{Services: services})

			fcc := inboundChainConfig{}
			node := &model.Proxy{Metadata: &model.NodeMetadata{IdleTimeout: tt.idleTimeout}}
			listenerFilters := NewListenerBuilder(cg.SetupProxy(node), cg.PushContext()).buildInboundNetworkFilters(fcc)
			tcp := &tcp.TcpProxy{}
			listenerFilters[len(listenerFilters)-1].GetTypedConfig().UnmarshalTo(tcp)
			if !reflect.DeepEqual(tcp.IdleTimeout, tt.expected) {
				t.Fatalf("Unexpected IdleTimeout, Expecting %s, Got %s", tt.expected, tcp.IdleTimeout)
			}
		})
	}
}

func TestBuildOutboundNetworkFiltersTunnelingConfig(t *testing.T) {
	type tunnelingConfig struct {
		hostname string
		usePost  bool
	}

	ns := "not-default"
	tunnelingEnabled := &networking.DestinationRule{
		Host: "tunnel-proxy.com",
		TrafficPolicy: &networking.TrafficPolicy{
			Tunnel: &networking.TrafficPolicy_TunnelSettings{
				Protocol:   "CONNECT",
				TargetHost: "example.com",
				TargetPort: 8443,
			},
		},
	}
	tunnelingEnabledWithoutProtocol := &networking.DestinationRule{
		Host: "tunnel-proxy.com",
		TrafficPolicy: &networking.TrafficPolicy{
			Tunnel: &networking.TrafficPolicy_TunnelSettings{
				TargetHost: "example.com",
				TargetPort: 8443,
			},
		},
	}
	tunnelingEnabledForSubset := &networking.DestinationRule{
		Host: "tunnel-proxy.com",
		Subsets: []*networking.Subset{
			{
				Name: "example-com-8443",
				TrafficPolicy: &networking.TrafficPolicy{
					Tunnel: &networking.TrafficPolicy_TunnelSettings{
						Protocol:   "POST",
						TargetHost: "example.com",
						TargetPort: 8443,
					},
				},
			},
		},
	}
	weightedRouteDestinations := []*networking.RouteDestination{
		{
			Destination: &networking.Destination{
				Host:   "tunnel-proxy.com",
				Port:   &networking.PortSelector{Number: 3128},
				Subset: "v1",
			},
			Weight: 25,
		},
		{
			Destination: &networking.Destination{
				Host:   "tunnel-proxy.com",
				Port:   &networking.PortSelector{Number: 3128},
				Subset: "v2",
			},
			Weight: 75,
		},
	}
	tunnelProxyDestination := []*networking.RouteDestination{
		{
			Destination: &networking.Destination{
				Host: "tunnel-proxy.com",
				Port: &networking.PortSelector{Number: 3128},
			},
		},
	}

	testCases := []struct {
		name                    string
		routeDestinations       []*networking.RouteDestination
		destinationRule         *networking.DestinationRule
		expectedTunnelingConfig *tunnelingConfig
	}{
		{
			name: "tunneling_config should not be applied when destination rule and listener subsets do not match",
			routeDestinations: []*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host:   "tunnel-proxy.com",
						Port:   &networking.PortSelector{Number: 3128},
						Subset: "random-subset",
					},
				},
			},
			destinationRule:         tunnelingEnabledForSubset,
			expectedTunnelingConfig: nil,
		},
		{
			name:              "tunneling_config should be applied when destination rule has specified tunnel settings",
			routeDestinations: tunnelProxyDestination,
			destinationRule:   tunnelingEnabled,
			expectedTunnelingConfig: &tunnelingConfig{
				hostname: "example.com:8443",
				usePost:  false,
			},
		},
		{
			name:              "tunneling_config should be applied with disabled usePost property when tunneling settings does not specify protocol",
			routeDestinations: tunnelProxyDestination,
			destinationRule:   tunnelingEnabledWithoutProtocol,
			expectedTunnelingConfig: &tunnelingConfig{
				hostname: "example.com:8443",
				usePost:  false,
			},
		},
		{
			name:              "tunneling_config should be applied when destination rule has specified tunnel settings and the target host is an IPv4 address",
			routeDestinations: tunnelProxyDestination,
			destinationRule: &networking.DestinationRule{
				Host: "tunnel-proxy.com",
				TrafficPolicy: &networking.TrafficPolicy{
					Tunnel: &networking.TrafficPolicy_TunnelSettings{
						Protocol:   "CONNECT",
						TargetHost: "192.168.1.2",
						TargetPort: 8443,
					},
				},
			},
			expectedTunnelingConfig: &tunnelingConfig{
				hostname: "192.168.1.2:8443",
				usePost:  false,
			},
		},
		{
			name:              "tunneling_config should be applied when destination rule has specified tunnel settings and the target host is an IPv6 address",
			routeDestinations: tunnelProxyDestination,
			destinationRule: &networking.DestinationRule{
				Host: "tunnel-proxy.com",
				TrafficPolicy: &networking.TrafficPolicy{
					Tunnel: &networking.TrafficPolicy_TunnelSettings{
						Protocol:   "CONNECT",
						TargetHost: "2001:db8:1234::",
						TargetPort: 8443,
					},
				},
			},
			expectedTunnelingConfig: &tunnelingConfig{
				hostname: "[2001:db8:1234::]:8443",
				usePost:  false,
			},
		},
		{
			name: "tunneling_config should be applied when destination rule has specified tunnel settings for a subset matching the destination route subset",
			routeDestinations: []*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host:   "tunnel-proxy.com",
						Port:   &networking.PortSelector{Number: 3128},
						Subset: "example-com-8443",
					},
				},
			},
			destinationRule: tunnelingEnabledForSubset,
			expectedTunnelingConfig: &tunnelingConfig{
				hostname: "example.com:8443",
				usePost:  true,
			},
		},
		{
			name: "tunneling_config should be applied when multiple destination routes with weights are specified" +
				" and destination rule with tunnel settings has no subset",
			routeDestinations: weightedRouteDestinations,
			destinationRule:   tunnelingEnabled,
			expectedTunnelingConfig: &tunnelingConfig{
				hostname: "example.com:8443",
				usePost:  false,
			},
		},
		{
			name: "tunneling_config should not be applied when multiple destination routes with weights are specified " +
				"and destination rule has tunnel settings for a subset",
			routeDestinations:       weightedRouteDestinations,
			destinationRule:         tunnelingEnabledForSubset,
			expectedTunnelingConfig: nil,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			destinationRuleConfig := config.Config{
				Meta: config.Meta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "tunnel-config",
					Namespace:        ns,
				},
				Spec: tt.destinationRule,
			}
			cg := NewConfigGenTest(t, TestOptions{
				ConfigPointers: []*config.Config{&destinationRuleConfig},
				Services: []*model.Service{
					buildServiceWithPort("example.com", 443, protocol.TLS, tnow),
					buildServiceWithPort("tunnel-proxy.com", 3128, protocol.HTTP, tnow),
				},
			})
			proxy := cg.SetupProxy(&model.Proxy{ConfigNamespace: ns})

			filters := buildOutboundNetworkFilters(proxy, tt.routeDestinations, cg.PushContext(),
				&model.Port{Port: 443}, config.Meta{Name: "routing-config-for-example-com", Namespace: ns})

			tcpProxy := xdstest.ExtractTCPProxy(t, &listener.FilterChain{Filters: filters})
			if tt.expectedTunnelingConfig == nil {
				if tcpProxy.TunnelingConfig != nil {
					t.Fatalf("Unexpected tunneling config in TcpProxy filter: %s", filters[0].String())
				}
			} else {
				if tcpProxy.TunnelingConfig.GetHostname() != tt.expectedTunnelingConfig.hostname {
					t.Fatalf("Expected to get tunneling_config.hostname: %s, but got: %s",
						tt.expectedTunnelingConfig.hostname, tcpProxy.TunnelingConfig.GetHostname())
				}
				if tcpProxy.TunnelingConfig.GetUsePost() != tt.expectedTunnelingConfig.usePost {
					t.Fatalf("Expected to get tunneling_config.use_post: %t, but got: %t",
						tt.expectedTunnelingConfig.usePost, tcpProxy.TunnelingConfig.GetUsePost())
				}
			}
		})
	}
}

func TestOutboundNetworkFilterStatPrefix(t *testing.T) {
	cases := []struct {
		name               string
		statPattern        string
		routes             []*networking.RouteDestination
		expectedStatPrefix string
	}{
		{
			"no pattern, single route",
			"",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "test.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
					},
				},
			},
			"outbound|9999||test.com",
		},
		{
			"service only pattern, single route",
			"%SERVICE%",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "test.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
					},
				},
			},
			"test.com",
		},
		{
			"no pattern, multiple routes",
			"",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "test.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
					},
					Weight: 50,
				},
				{
					Destination: &networking.Destination{
						Host: "test.com",
						Port: &networking.PortSelector{
							Number: 8888,
						},
					},
					Weight: 50,
				},
			},
			"test.com.ns", // No stat pattern will be applied for multiple routes, as it will be always be name.namespace.
		},
		{
			"service pattern, multiple routes",
			"%SERVICE%",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "test.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
					},
					Weight: 50,
				},
				{
					Destination: &networking.Destination{
						Host: "test.com",
						Port: &networking.PortSelector{
							Number: 8888,
						},
					},
					Weight: 50,
				},
			},
			"test.com.ns", // No stat pattern will be applied for multiple routes, as it will be always be name.namespace.
		},
	}

	services := []*model.Service{
		buildService("test.com", "10.10.0.0/24", protocol.TCP, tnow),
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := mesh.DefaultMeshConfig()
			m.OutboundClusterStatName = tt.statPattern
			cg := NewConfigGenTest(t, TestOptions{MeshConfig: m, Services: services})

			listeners := buildOutboundNetworkFilters(
				cg.SetupProxy(nil), tt.routes, cg.PushContext(),
				&model.Port{Port: 9999}, config.Meta{Name: "test.com", Namespace: "ns"})
			tcp := &tcp.TcpProxy{}
			listeners[0].GetTypedConfig().UnmarshalTo(tcp)
			if tcp.StatPrefix != tt.expectedStatPrefix {
				t.Fatalf("Unexpected Stat Prefix, Expecting %s, Got %s", tt.expectedStatPrefix, tcp.StatPrefix)
			}
		})
	}
}

func TestOutboundNetworkFilterWithSourceIPHashing(t *testing.T) {
	services := []*model.Service{
		buildService("test.com", "10.10.0.0/24", protocol.TCP, tnow),
		buildService("testsimple.com", "10.10.0.0/24", protocol.TCP, tnow),
		buildService("subsettest.com", "10.10.0.0/24", protocol.TCP, tnow),
		buildService("subsettestdifferent.com", "10.10.0.0/24", protocol.TCP, tnow),
	}

	simpleDestinationRuleSpec := &networking.DestinationRule{
		Host: "testsimple.com",
		TrafficPolicy: &networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{},
			},
		},
	}

	simpleDestinationRule := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:             "acme-v3-0",
			Namespace:        "not-default",
		},
		Spec: simpleDestinationRuleSpec,
	}

	destinationRuleSpec := &networking.DestinationRule{
		Host: "test.com",
		TrafficPolicy: &networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_UseSourceIp{UseSourceIp: true},
					},
				},
			},
		},
	}

	destinationRule := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:             "acme-v3-1",
			Namespace:        "not-default",
		},
		Spec: destinationRuleSpec,
	}

	subsetdestinationRuleSpec := &networking.DestinationRule{
		Host: "subsettest.com",
		TrafficPolicy: &networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_UseSourceIp{UseSourceIp: true},
					},
				},
			},
		},
		Subsets: []*networking.Subset{{Name: "v1", Labels: map[string]string{"version": "v1"}}},
	}

	subsetdestinationRule := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:             "acme-v3-2",
			Namespace:        "not-default",
		},
		Spec: subsetdestinationRuleSpec,
	}

	subsetdestinationRuleDifferentSpec := &networking.DestinationRule{
		Host: "subsettestdifferent.com",
		TrafficPolicy: &networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_UseSourceIp{UseSourceIp: true},
					},
				},
			},
		},
		Subsets: []*networking.Subset{
			{
				Name:   "v1",
				Labels: map[string]string{"version": "v1"},
				TrafficPolicy: &networking.TrafficPolicy{
					LoadBalancer: &networking.LoadBalancerSettings{
						LbPolicy: &networking.LoadBalancerSettings_Simple{},
					},
				},
			},
		},
	}

	subsetDifferentdestinationRule := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:             "acme-v3-3",
			Namespace:        "not-default",
		},
		Spec: subsetdestinationRuleDifferentSpec,
	}

	destinationRules := []*config.Config{&destinationRule, &simpleDestinationRule, &subsetdestinationRule, &subsetDifferentdestinationRule}

	cg := NewConfigGenTest(t, TestOptions{
		ConfigPointers: destinationRules,
		Services:       services,
	})

	proxy := cg.SetupProxy(&model.Proxy{ConfigNamespace: "not-default"})
	cases := []struct {
		name        string
		routes      []*networking.RouteDestination
		configMeta  config.Meta
		useSourceIP bool
	}{
		{
			"destination rule without sourceip",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "testsimple.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
					},
				},
			},
			config.Meta{Name: "testsimple.com", Namespace: "ns"},
			false,
		},
		{
			"destination rule has sourceip",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "test.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
					},
				},
			},
			config.Meta{Name: "test.com", Namespace: "ns"},
			true,
		},
		{
			"subset destination rule does not have traffic policy",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "subsettest.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
						Subset: "v1",
					},
				},
			},
			config.Meta{Name: "subsettest.com", Namespace: "ns"},
			true,
		},
		{
			"subset destination rule overrides traffic policy",
			[]*networking.RouteDestination{
				{
					Destination: &networking.Destination{
						Host: "subsettestdifferent.com",
						Port: &networking.PortSelector{
							Number: 9999,
						},
						Subset: "v1",
					},
				},
			},
			config.Meta{Name: "subsettestdifferent.com", Namespace: "ns"},
			false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			listeners := buildOutboundNetworkFilters(proxy, tt.routes, cg.PushContext(), &model.Port{Port: 9999}, tt.configMeta)
			tcp := &tcp.TcpProxy{}
			listeners[0].GetTypedConfig().UnmarshalTo(tcp)
			hasSourceIP := tcp.HashPolicy != nil && len(tcp.HashPolicy) == 1 && tcp.HashPolicy[0].GetSourceIp() != nil
			if hasSourceIP != tt.useSourceIP {
				t.Fatalf("Unexpected SourceIp hash policy. expected: %v, got: %v", tt.useSourceIP, hasSourceIP)
			}
		})
	}
}
