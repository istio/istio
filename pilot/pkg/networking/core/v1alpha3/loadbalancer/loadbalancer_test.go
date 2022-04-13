// Copyright Istio Authors. All Rights Reserved.
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

package loadbalancer

import (
	"reflect"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestApplyLocalitySetting(t *testing.T) {
	locality := &core.Locality{
		Region:  "region1",
		Zone:    "zone1",
		SubZone: "subzone1",
	}

	t.Run("Distribute", func(t *testing.T) {
		tests := []struct {
			name       string
			distribute []*networking.LocalityLoadBalancerSetting_Distribute
			expected   []int
		}{
			{
				name: "distribution between subzones",
				distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "region1/zone1/subzone1",
						To: map[string]uint32{
							"region1/zone1/subzone1": 80,
							"region1/zone1/subzone2": 15,
							"region1/zone1/subzone3": 5,
						},
					},
				},
				expected: []int{40, 40, 15, 5, 0, 0, 0},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				env := buildEnvForClustersWithDistribute(tt.distribute)
				cluster := buildFakeCluster()
				ApplyLocalityLBSetting(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
				weights := make([]int, 0)
				for _, localityEndpoint := range cluster.LoadAssignment.Endpoints {
					weights = append(weights, int(localityEndpoint.LoadBalancingWeight.GetValue()))
				}
				if !reflect.DeepEqual(weights, tt.expected) {
					t.Errorf("Got weights %v expected %v", weights, tt.expected)
				}
			})
		}
	})

	t.Run("Failover: all priorities", func(t *testing.T) {
		g := NewWithT(t)
		env := buildEnvForClustersWithFailover()
		cluster := buildFakeCluster()
		ApplyLocalityLBSetting(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
		for _, localityEndpoint := range cluster.LoadAssignment.Endpoints {
			if localityEndpoint.Locality.Region == locality.Region {
				if localityEndpoint.Locality.Zone == locality.Zone {
					if localityEndpoint.Locality.SubZone == locality.SubZone {
						g.Expect(localityEndpoint.Priority).To(Equal(uint32(0)))
						continue
					}
					g.Expect(localityEndpoint.Priority).To(Equal(uint32(1)))
					continue
				}
				g.Expect(localityEndpoint.Priority).To(Equal(uint32(2)))
				continue
			}
			if localityEndpoint.Locality.Region == "region2" {
				g.Expect(localityEndpoint.Priority).To(Equal(uint32(3)))
			} else {
				g.Expect(localityEndpoint.Priority).To(Equal(uint32(4)))
			}
		}
	})

	t.Run("Failover: priorities with gaps", func(t *testing.T) {
		g := NewWithT(t)
		env := buildEnvForClustersWithFailover()
		cluster := buildSmallCluster()
		ApplyLocalityLBSetting(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
		for _, localityEndpoint := range cluster.LoadAssignment.Endpoints {
			if localityEndpoint.Locality.Region == locality.Region {
				if localityEndpoint.Locality.Zone == locality.Zone {
					if localityEndpoint.Locality.SubZone == locality.SubZone {
						t.Errorf("Should not exist")
						continue
					}
					g.Expect(localityEndpoint.Priority).To(Equal(uint32(0)))
					continue
				}
				t.Errorf("Should not exist")
				continue
			}
			if localityEndpoint.Locality.Region == "region2" {
				g.Expect(localityEndpoint.Priority).To(Equal(uint32(1)))
			} else {
				t.Errorf("Should not exist")
			}
		}
	})

	t.Run("Failover: priorities with some nil localities", func(t *testing.T) {
		g := NewWithT(t)
		env := buildEnvForClustersWithFailover()
		cluster := buildSmallClusterWithNilLocalities()
		ApplyLocalityLBSetting(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
		for _, localityEndpoint := range cluster.LoadAssignment.Endpoints {
			if localityEndpoint.Locality == nil {
				g.Expect(localityEndpoint.Priority).To(Equal(uint32(2)))
			} else if localityEndpoint.Locality.Region == locality.Region {
				if localityEndpoint.Locality.Zone == locality.Zone {
					if localityEndpoint.Locality.SubZone == locality.SubZone {
						t.Errorf("Should not exist")
						continue
					}
					g.Expect(localityEndpoint.Priority).To(Equal(uint32(0)))
					continue
				}
				t.Errorf("Should not exist")
				continue
			} else if localityEndpoint.Locality.Region == "region2" {
				g.Expect(localityEndpoint.Priority).To(Equal(uint32(1)))
			} else {
				t.Errorf("Should not exist")
			}
		}
	})

	t.Run("Failover: with locality lb disabled", func(t *testing.T) {
		g := NewWithT(t)
		cluster := buildSmallClusterWithNilLocalities()
		lbsetting := &networking.LocalityLoadBalancerSetting{
			Enabled: &wrappers.BoolValue{Value: false},
		}
		ApplyLocalityLBSetting(cluster.LoadAssignment, nil, locality, nil, lbsetting, true)
		for _, localityEndpoint := range cluster.LoadAssignment.Endpoints {
			g.Expect(localityEndpoint.Priority).To(Equal(uint32(0)))
		}
	})

	t.Run("FailoverPriority", func(t *testing.T) {
		tests := []struct {
			name             string
			failoverPriority []string
			proxyLabels      map[string]string
			expected         []*endpoint.LocalityLbEndpoints
		}{
			{
				name:             "match none label",
				failoverPriority: []string{"topology.istio.io/network", "topology.istio.io/cluster"},
				proxyLabels: map[string]string{
					"topology.istio.io/network": "test",
					"topology.istio.io/cluster": "test",
				},
				expected: []*endpoint.LocalityLbEndpoints{
					{
						Locality: &core.Locality{
							Region:  "region1",
							Zone:    "zone1",
							SubZone: "subzone1",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("1.1.1.1"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
							{
								HostIdentifier: buildEndpoint("2.2.2.2"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 2,
						},
						Priority: 0,
					},
					{
						Locality: &core.Locality{
							Region:  "region2",
							Zone:    "zone2",
							SubZone: "subzone2",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("3.3.3.3"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
							{
								HostIdentifier: buildEndpoint("4.4.4.4"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 2,
						},
						Priority: 0,
					},
				},
			},
			{
				name:             "match network label",
				failoverPriority: []string{"topology.istio.io/network", "topology.istio.io/cluster"},
				proxyLabels: map[string]string{
					"topology.istio.io/network": "n1",
					"topology.istio.io/cluster": "test",
				},
				expected: []*endpoint.LocalityLbEndpoints{
					{
						Locality: &core.Locality{
							Region:  "region1",
							Zone:    "zone1",
							SubZone: "subzone1",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("1.1.1.1"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 0,
					},
					{
						Locality: &core.Locality{
							Region:  "region1",
							Zone:    "zone1",
							SubZone: "subzone1",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("2.2.2.2"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 1,
					},
					{
						Locality: &core.Locality{
							Region:  "region2",
							Zone:    "zone2",
							SubZone: "subzone2",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("3.3.3.3"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 0,
					},
					{
						Locality: &core.Locality{
							Region:  "region2",
							Zone:    "zone2",
							SubZone: "subzone2",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("4.4.4.4"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 1,
					},
				},
			},
			{
				name:             "not match the first n label",
				failoverPriority: []string{"topology.istio.io/network", "topology.istio.io/cluster"},
				proxyLabels: map[string]string{
					"topology.istio.io/network": "test",
					"topology.istio.io/cluster": "c1",
				},
				expected: []*endpoint.LocalityLbEndpoints{
					{
						Locality: &core.Locality{
							Region:  "region1",
							Zone:    "zone1",
							SubZone: "subzone1",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("1.1.1.1"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
							{
								HostIdentifier: buildEndpoint("2.2.2.2"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 2,
						},
						Priority: 0,
					},
					{
						Locality: &core.Locality{
							Region:  "region2",
							Zone:    "zone2",
							SubZone: "subzone2",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("3.3.3.3"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
							{
								HostIdentifier: buildEndpoint("4.4.4.4"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 2,
						},
						Priority: 0,
					},
				},
			},
			{
				name:             "match all labels",
				failoverPriority: []string{"key", "topology.istio.io/network", "topology.istio.io/cluster"},
				proxyLabels: map[string]string{
					"key":                       "value",
					"topology.istio.io/network": "n2",
					"topology.istio.io/cluster": "c2",
				},
				expected: []*endpoint.LocalityLbEndpoints{
					{
						Locality: &core.Locality{
							Region:  "region1",
							Zone:    "zone1",
							SubZone: "subzone1",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("2.2.2.2"), // match [key, network, cluster]
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 0,
					},
					{
						Locality: &core.Locality{
							Region:  "region1",
							Zone:    "zone1",
							SubZone: "subzone1",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("1.1.1.1"), // match no label
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 3,
					},
					{
						Locality: &core.Locality{
							Region:  "region2",
							Zone:    "zone2",
							SubZone: "subzone2",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("4.4.4.4"), // match [key, network]
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 1,
					},
					{
						Locality: &core.Locality{
							Region:  "region2",
							Zone:    "zone2",
							SubZone: "subzone2",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("3.3.3.3"), // match [key]
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 2,
					},
				},
			},
		}

		wrappedEndpoints := buildWrappedLocalityLbEndpoints()

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				env := buildEnvForClustersWithFailoverPriority(tt.failoverPriority)
				cluster := buildFakeCluster()
				ApplyLocalityLBSetting(cluster.LoadAssignment, wrappedEndpoints, locality, tt.proxyLabels, env.Mesh().LocalityLbSetting, true)

				if len(cluster.LoadAssignment.Endpoints) != len(tt.expected) {
					t.Fatalf("expected endpoints %d but got %d", len(cluster.LoadAssignment.Endpoints), len(tt.expected))
				}
				for i := range cluster.LoadAssignment.Endpoints {
					if cluster.LoadAssignment.Endpoints[i].LoadBalancingWeight.GetValue() != tt.expected[i].LoadBalancingWeight.GetValue() {
						t.Errorf("Got unexpected lb weight %v expected %v", cluster.LoadAssignment.Endpoints[i].LoadBalancingWeight, tt.expected[i].LoadBalancingWeight)
					}
					if cluster.LoadAssignment.Endpoints[i].Priority != tt.expected[i].Priority {
						t.Errorf("Got unexpected priority %v expected %v", cluster.LoadAssignment.Endpoints[i].Priority, tt.expected[i].Priority)
					}
				}
			})
		}
	})
}

func TestGetLocalityLbSetting(t *testing.T) {
	// dummy config for test
	failover := []*networking.LocalityLoadBalancerSetting_Failover{nil}
	cases := []struct {
		name     string
		mesh     *networking.LocalityLoadBalancerSetting
		dr       *networking.LocalityLoadBalancerSetting
		expected *networking.LocalityLoadBalancerSetting
	}{
		{
			"all disabled",
			nil,
			nil,
			nil,
		},
		{
			"mesh only",
			&networking.LocalityLoadBalancerSetting{},
			nil,
			&networking.LocalityLoadBalancerSetting{},
		},
		{
			"dr only",
			nil,
			&networking.LocalityLoadBalancerSetting{},
			&networking.LocalityLoadBalancerSetting{},
		},
		{
			"dr only override",
			nil,
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
		},
		{
			"both",
			&networking.LocalityLoadBalancerSetting{},
			&networking.LocalityLoadBalancerSetting{Failover: failover},
			&networking.LocalityLoadBalancerSetting{Failover: failover},
		},
		{
			"mesh disabled",
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}},
			nil,
			nil,
		},
		{
			"dr disabled",
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}},
			nil,
		},
		{
			"dr enabled override mesh disabled",
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}},
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
			&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLocalityLbSetting(tt.mesh, tt.dr)
			if !reflect.DeepEqual(tt.expected, got) {
				t.Fatalf("Expected: %v, got: %v", tt.expected, got)
			}
		})
	}
}

func buildEnvForClustersWithDistribute(distribute []*networking.LocalityLoadBalancerSetting_Distribute) *model.Environment {
	serviceDiscovery := memregistry.NewServiceDiscovery(&model.Service{
		Hostname:       "test.example.org",
		DefaultAddress: "1.1.1.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
	})

	meshConfig := &meshconfig.MeshConfig{
		ConnectTimeout: &durationpb.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
			Distribute: distribute,
		},
	}

	configStore := model.MakeIstioStore(memory.Make(collections.Pilot))

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ConfigStore:      configStore,
		Watcher:          mesh.NewFixedWatcher(meshConfig),
	}

	env.PushContext = model.NewPushContext()
	env.Init()
	_ = env.PushContext.InitContext(env, nil, nil)
	env.PushContext.SetDestinationRules([]config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
				Name:             "acme",
			},
			Spec: &networking.DestinationRule{
				Host: "test.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{},
				},
			},
		},
	})

	return env
}

func buildEnvForClustersWithFailover() *model.Environment {
	serviceDiscovery := memregistry.NewServiceDiscovery(&model.Service{
		Hostname:       "test.example.org",
		DefaultAddress: "1.1.1.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
	})

	meshConfig := &meshconfig.MeshConfig{
		ConnectTimeout: &durationpb.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
			Failover: []*networking.LocalityLoadBalancerSetting_Failover{
				{
					From: "region1",
					To:   "region2",
				},
			},
		},
	}

	configStore := model.MakeIstioStore(memory.Make(collections.Pilot))

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ConfigStore:      configStore,
		Watcher:          mesh.NewFixedWatcher(meshConfig),
	}

	env.PushContext = model.NewPushContext()
	env.Init()
	_ = env.PushContext.InitContext(env, nil, nil)
	env.PushContext.SetDestinationRules([]config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
				Name:             "acme",
			},
			Spec: &networking.DestinationRule{
				Host: "test.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{},
				},
			},
		},
	})

	return env
}

func buildEnvForClustersWithFailoverPriority(failoverPriority []string) *model.Environment {
	serviceDiscovery := memregistry.NewServiceDiscovery(&model.Service{
		Hostname:       "test.example.org",
		DefaultAddress: "1.1.1.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
	})

	meshConfig := &meshconfig.MeshConfig{
		ConnectTimeout: &durationpb.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
			FailoverPriority: failoverPriority,
		},
	}

	configStore := model.MakeIstioStore(memory.Make(collections.Pilot))

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ConfigStore:      configStore,
		Watcher:          mesh.NewFixedWatcher(meshConfig),
	}

	env.PushContext = model.NewPushContext()
	env.Init()
	_ = env.PushContext.InitContext(env, nil, nil)
	env.PushContext.SetDestinationRules([]config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
				Name:             "acme",
			},
			Spec: &networking.DestinationRule{
				Host: "test.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{},
				},
			},
		},
	})

	return env
}

func buildFakeCluster() *cluster.Cluster {
	return &cluster.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone3",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone2",
						SubZone: "",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "",
						SubZone: "",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region3",
						Zone:    "",
						SubZone: "",
					},
				},
			},
		},
	}
}

func buildSmallCluster() *cluster.Cluster {
	return &cluster.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
			},
		},
	}
}

func buildSmallClusterWithNilLocalities() *cluster.Cluster {
	return &cluster.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
			},
		},
	}
}

func buildSmallClusterForFailOverPriority() *cluster.Cluster {
	return &cluster.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: buildEndpoint("1.1.1.1"),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 1,
							},
						},
						{
							HostIdentifier: buildEndpoint("2.2.2.2"),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 1,
							},
						},
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "zone2",
						SubZone: "subzone2",
					},
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: buildEndpoint("3.3.3.3"),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 1,
							},
						},
						{
							HostIdentifier: buildEndpoint("4.4.4.4"),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 1,
							},
						},
					},
				},
			},
		},
	}
}

func buildEndpoint(ip string) *endpoint.LbEndpoint_Endpoint {
	return &endpoint.LbEndpoint_Endpoint{
		Endpoint: &endpoint.Endpoint{
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: ip,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 10001,
						},
					},
				},
			},
		},
	}
}

func buildWrappedLocalityLbEndpoints() []*WrappedLocalityLbEndpoints {
	cluster := buildSmallClusterForFailOverPriority()
	return []*WrappedLocalityLbEndpoints{
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"topology.istio.io/network": "n1",
						"topology.istio.io/cluster": "c1",
					},
					Address: "1.1.1.1",
				},
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c2",
					},
					Address: "2.2.2.2",
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[0],
		},
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n1",
						"topology.istio.io/cluster": "c3",
					},
					Address: "3.3.3.3",
				},
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c4",
					},
					Address: "4.4.4.4",
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[1],
		},
	}
}
