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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestBuildRedisFilter(t *testing.T) {
	node := getProxy()
	redisFilter := buildRedisFilter(node, "redis", "redis-cluster")
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

func TestBuildRedisFilterCustomTimeout(t *testing.T) {
	node := getProxy()
	node.Metadata.RedisOpTimeout = "15s"
	redisFilter := buildRedisFilter(node, "redis", "redis-cluster")
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
		if redisProxy.Settings.OpTimeout.Seconds != 15 {
			t.Errorf("redis proxy's Settings.OpTimeout is %s", redisProxy.Settings.OpTimeout)
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

			instance := &model.ServiceInstance{
				Service: &model.Service{
					Hostname:       "v0.default.example.org",
					DefaultAddress: "9.9.9.9",
					CreationTime:   tnow,
					Attributes: model.ServiceAttributes{
						Namespace: "not-default",
					},
				},
				ServicePort: &model.Port{
					Port: 9999,
					Name: "http",
				},
				Endpoint: &model.IstioEndpoint{
					EndpointPort: 8888,
				},
			}

			listenerFilters := buildInboundNetworkFilters(cg.PushContext(), cg.SetupProxy(nil),
				instance, model.BuildInboundSubsetKey(int(instance.Endpoint.EndpointPort)))
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

			instance := &model.ServiceInstance{
				Service: &model.Service{
					Hostname:       "v0.default.example.org",
					DefaultAddress: "9.9.9.9",
					CreationTime:   tnow,
					Attributes: model.ServiceAttributes{
						Namespace: "not-default",
					},
				},
				ServicePort: &model.Port{
					Port: 9999,
					Name: "http",
				},
				Endpoint: &model.IstioEndpoint{
					EndpointPort: 8888,
				},
			}
			node := &model.Proxy{Metadata: &model.NodeMetadata{IdleTimeout: tt.idleTimeout}}
			listenerFilters := buildInboundNetworkFilters(cg.PushContext(), node,
				instance, model.BuildInboundSubsetKey(int(instance.Endpoint.EndpointPort)))
			tcp := &tcp.TcpProxy{}
			listenerFilters[len(listenerFilters)-1].GetTypedConfig().UnmarshalTo(tcp)
			if !reflect.DeepEqual(tcp.IdleTimeout, tt.expected) {
				t.Fatalf("Unexpected IdleTimeout, Expecting %s, Got %s", tt.expected, tcp.IdleTimeout)
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
