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

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	registrylabel "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
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
				env := buildEnvForClustersWithDistribute(t, tt.distribute)
				cluster := buildFakeCluster()
				ApplyLocalityLoadBalancer(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
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
		env := buildEnvForClustersWithFailover(t)
		cluster := buildFakeCluster()
		ApplyLocalityLoadBalancer(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
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
		env := buildEnvForClustersWithFailover(t)
		cluster := buildSmallCluster()
		ApplyLocalityLoadBalancer(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
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
		env := buildEnvForClustersWithFailover(t)
		cluster := buildSmallClusterWithNilLocalities()
		ApplyLocalityLoadBalancer(cluster.LoadAssignment, nil, locality, nil, env.Mesh().LocalityLbSetting, true)
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
		// lbsetting := &networking.LocalityLoadBalancerSetting{
		// 	Enabled: &wrappers.BoolValue{Value: false},
		// }
		// disabled locality lb setting should be converted to nil
		ApplyLocalityLoadBalancer(cluster.LoadAssignment, nil, locality, nil, nil, true)
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
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
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
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
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
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("2.3.4.5", "2001:1::2"),
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
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("2.3.4.5", "2001:1::2"),
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
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("2.3.4.5", "2001:1::2"), // match [key, network, cluster]
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
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"), // match no label
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
			{
				name:             "priority assignment as per the overridden value",
				failoverPriority: []string{"topology.istio.io/network=n1"},
				proxyLabels: map[string]string{
					"topology.istio.io/network": "n2",
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
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
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
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("2.3.4.5", "2001:1::2"),
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
				name:             "no endpoints with overridden value",
				failoverPriority: []string{"topology.istio.io/network=n3"},
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
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
							{
								HostIdentifier: buildEndpointWithMultipleAddresses("2.3.4.5", "2001:1::2"),
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
		}

		wrappedEndpoints := buildWrappedLocalityLbEndpoints()

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				env := buildEnvForClustersWithFailoverPriority(t, tt.failoverPriority)
				cluster := buildFakeCluster()
				ApplyLocalityLoadBalancer(cluster.LoadAssignment, wrappedEndpoints, locality, tt.proxyLabels, env.Mesh().LocalityLbSetting, true)

				if len(cluster.LoadAssignment.Endpoints) != len(tt.expected) {
					t.Fatalf("expected endpoints %d but got %d", len(cluster.LoadAssignment.Endpoints), len(tt.expected))
				}
				for i := range cluster.LoadAssignment.Endpoints {
					// TODO Below assertions are not robust to ordering changes in cluster.LoadAssignment.Endpoints[i]
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

	// NOTE: The test "FailoverPriority with DNS service instances" has been moved to
	// loadbalancer_simulation_test.go as TestFailoverPriorityWithDNSServiceEntry.
	// That test uses a real ServiceEntry converted through the full xDS pipeline
	// rather than manually constructed cluster structures.

	t.Run("FailoverPriority with Failover", func(t *testing.T) {
		tests := []struct {
			name             string
			failoverPriority []string
			proxyLabels      map[string]string
			expected         []*endpoint.LocalityLbEndpoints
		}{
			{
				name:             "match all labels",
				failoverPriority: []string{"topology.istio.io/network", "topology.istio.io/cluster"},
				proxyLabels: map[string]string{
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
								HostIdentifier: buildEndpoint("1.1.1.1"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 3, // does not match failoverPriority but match locality of the client.
					},
					{
						Locality: &core.Locality{
							Region:  "region1",
							Zone:    "zone2",
							SubZone: "subzone2",
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
						Priority: 0, // highest priority because of label matching and same region.
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
						Priority: 4, // does not match failoverPriority and locality but mentioned in locality failover settings for the client region.
					},
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
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
						Priority: 2, // match the first label of failoverPriority but not the second
					},
					{
						Locality: &core.Locality{
							Region:  "region2",
							Zone:    "zone2",
							SubZone: "subzone2",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("5.5.5.5"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 1, // matching the same failoverPriority but different locality.
					},
					{
						Locality: &core.Locality{
							Region:  "region3",
							Zone:    "zone3",
							SubZone: "subzone3",
						},
						LbEndpoints: []*endpoint.LbEndpoint{
							{
								HostIdentifier: buildEndpoint("6.6.6.6"),
								LoadBalancingWeight: &wrappers.UInt32Value{
									Value: 1,
								},
							},
						},
						LoadBalancingWeight: &wrappers.UInt32Value{
							Value: 1,
						},
						Priority: 5, // does not match any of the priority constructs so least priority.
					},
				},
			},
		}
		wrappedEndpoints := buildWrappedLocalityLbEndpointsForFailoverPriorityWithFailover()
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				env := buildEnvForClustersWithMixedFailoverPriorityAndLocalityFailover(t, tt.failoverPriority)
				cluster := buildFakeCluster()
				ApplyLocalityLoadBalancer(cluster.LoadAssignment, wrappedEndpoints, locality, tt.proxyLabels, env.Mesh().LocalityLbSetting, true)

				if len(cluster.LoadAssignment.Endpoints) != len(tt.expected) {
					t.Fatalf("expected endpoints %d but got %d", len(cluster.LoadAssignment.Endpoints), len(tt.expected))
				}
				for i := range cluster.LoadAssignment.Endpoints {
					// TODO Below assertions are not robust to ordering changes in cluster.LoadAssignment.Endpoints[i]
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

func TestGetEffectiveLbSetting(t *testing.T) {
	// dummy config for test
	preferSameZoneService := &model.Service{Attributes: model.ServiceAttributes{K8sAttributes: model.K8sAttributes{
		TrafficDistribution: model.TrafficDistributionPreferSameZone,
	}}}
	preferSameNodeService := &model.Service{Attributes: model.ServiceAttributes{K8sAttributes: model.K8sAttributes{
		TrafficDistribution: model.TrafficDistributionPreferSameNode,
	}}}
	failover := []*networking.LocalityLoadBalancerSetting_Failover{nil}
	zaFailover := []*networking.ZoneAwareLoadBalancerSetting_Failover{{From: "r1", To: "r2"}}
	drLB := func(l *networking.LocalityLoadBalancerSetting) *networking.LoadBalancerSettings {
		if l == nil {
			return nil
		}
		return &networking.LoadBalancerSettings{LocalityLbSetting: l}
	}
	drZA := func(z *networking.ZoneAwareLoadBalancerSetting) *networking.LoadBalancerSettings {
		if z == nil {
			return nil
		}
		return &networking.LoadBalancerSettings{ZoneAwareLbSetting: z}
	}
	preferSameZoneExpected := &networking.LocalityLoadBalancerSetting{
		FailoverPriority: []string{
			label.TopologyNetwork.Name,
			registrylabel.LabelTopologyRegion,
			registrylabel.LabelTopologyZone,
		},
		Enabled: &wrappers.BoolValue{Value: true},
	}
	preferSameNodeExpected := &networking.LocalityLoadBalancerSetting{
		FailoverPriority: []string{
			label.TopologyNetwork.Name,
			registrylabel.LabelTopologyRegion,
			registrylabel.LabelTopologyZone,
			label.TopologySubzone.Name,
			registrylabel.LabelHostname,
		},
		Enabled: &wrappers.BoolValue{Value: true},
	}
	cases := []struct {
		name              string
		meshLocality      *networking.LocalityLoadBalancerSetting
		meshZoneAware     *networking.ZoneAwareLoadBalancerSetting
		dr                *networking.LoadBalancerSettings
		svc               *model.Service
		expectedLocality  *networking.LocalityLoadBalancerSetting
		expectedZoneAware *networking.ZoneAwareLoadBalancerSetting
		expectedForce     bool
	}{
		{
			name: "all disabled",
		},
		{
			name:             "mesh locality only",
			meshLocality:     &networking.LocalityLoadBalancerSetting{},
			expectedLocality: &networking.LocalityLoadBalancerSetting{},
		},
		{
			name:             "dr locality only",
			dr:               drLB(&networking.LocalityLoadBalancerSetting{}),
			expectedLocality: &networking.LocalityLoadBalancerSetting{},
		},
		{
			name:             "dr locality only override",
			dr:               drLB(&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}}),
			expectedLocality: &networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
		},
		{
			name:             "dr locality wins over mesh locality",
			meshLocality:     &networking.LocalityLoadBalancerSetting{},
			dr:               drLB(&networking.LocalityLoadBalancerSetting{Failover: failover}),
			expectedLocality: &networking.LocalityLoadBalancerSetting{Failover: failover},
		},
		{
			name:             "prefer same zone service over mesh locality",
			meshLocality:     &networking.LocalityLoadBalancerSetting{},
			svc:              preferSameZoneService,
			expectedLocality: preferSameZoneExpected,
			expectedForce:    true,
		},
		{
			name:             "prefer same node service over mesh locality",
			meshLocality:     &networking.LocalityLoadBalancerSetting{},
			svc:              preferSameNodeService,
			expectedLocality: preferSameNodeExpected,
			expectedForce:    true,
		},
		{
			name:         "mesh locality disabled",
			meshLocality: &networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}},
		},
		{
			name:         "dr locality disabled overrides mesh locality enabled",
			meshLocality: &networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
			dr:           drLB(&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}}),
		},
		{
			name:             "dr locality enabled overrides mesh locality disabled",
			meshLocality:     &networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}},
			dr:               drLB(&networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}}),
			expectedLocality: &networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
		},

		// ZoneAware scenarios
		{
			name:              "mesh zone-aware only",
			meshZoneAware:     &networking.ZoneAwareLoadBalancerSetting{},
			expectedZoneAware: &networking.ZoneAwareLoadBalancerSetting{},
		},
		{
			name:              "dr zone-aware only",
			dr:                drZA(&networking.ZoneAwareLoadBalancerSetting{Failover: zaFailover}),
			expectedZoneAware: &networking.ZoneAwareLoadBalancerSetting{Failover: zaFailover},
		},
		{
			name:              "dr zone-aware wins over mesh zone-aware",
			meshZoneAware:     &networking.ZoneAwareLoadBalancerSetting{},
			dr:                drZA(&networking.ZoneAwareLoadBalancerSetting{Failover: zaFailover}),
			expectedZoneAware: &networking.ZoneAwareLoadBalancerSetting{Failover: zaFailover},
		},
		{
			name:              "dr zone-aware wins over mesh locality",
			meshLocality:      &networking.LocalityLoadBalancerSetting{Failover: failover},
			dr:                drZA(&networking.ZoneAwareLoadBalancerSetting{}),
			expectedZoneAware: &networking.ZoneAwareLoadBalancerSetting{},
		},
		{
			name:             "dr locality wins over mesh zone-aware",
			meshZoneAware:    &networking.ZoneAwareLoadBalancerSetting{},
			dr:               drLB(&networking.LocalityLoadBalancerSetting{Failover: failover}),
			expectedLocality: &networking.LocalityLoadBalancerSetting{Failover: failover},
		},
		{
			name:              "mesh zone-aware wins over mesh locality when both set",
			meshLocality:      &networking.LocalityLoadBalancerSetting{},
			meshZoneAware:     &networking.ZoneAwareLoadBalancerSetting{Failover: zaFailover},
			expectedZoneAware: &networking.ZoneAwareLoadBalancerSetting{Failover: zaFailover},
		},
		{
			name:          "mesh zone-aware disabled",
			meshZoneAware: &networking.ZoneAwareLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}},
		},
		{
			name:         "dr zone-aware disabled overrides mesh locality enabled",
			meshLocality: &networking.LocalityLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
			dr:           drZA(&networking.ZoneAwareLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}}),
		},
		{
			name:              "dr zone-aware enabled overrides mesh zone-aware disabled",
			meshZoneAware:     &networking.ZoneAwareLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: false}},
			dr:                drZA(&networking.ZoneAwareLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}}),
			expectedZoneAware: &networking.ZoneAwareLoadBalancerSetting{Enabled: &wrappers.BoolValue{Value: true}},
		},
		{
			name:             "service traffic distribution wins over mesh zone-aware",
			meshZoneAware:    &networking.ZoneAwareLoadBalancerSetting{},
			svc:              preferSameZoneService,
			expectedLocality: preferSameZoneExpected,
			expectedForce:    true,
		},
		{
			name:              "dr zone-aware wins over service traffic distribution",
			dr:                drZA(&networking.ZoneAwareLoadBalancerSetting{}),
			svc:               preferSameZoneService,
			expectedZoneAware: &networking.ZoneAwareLoadBalancerSetting{},
		},
		{
			name:             "dr locality wins over service traffic distribution",
			dr:               drLB(&networking.LocalityLoadBalancerSetting{Failover: failover}),
			svc:              preferSameZoneService,
			expectedLocality: &networking.LocalityLoadBalancerSetting{Failover: failover},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			gotLocality, gotZoneAware, gotForce := GetEffectiveLbSetting(tt.meshLocality, tt.meshZoneAware, tt.dr, tt.svc)
			if !reflect.DeepEqual(tt.expectedLocality, gotLocality) {
				t.Fatalf("locality: expected %v, got %v", tt.expectedLocality, gotLocality)
			}
			if !reflect.DeepEqual(tt.expectedZoneAware, gotZoneAware) {
				t.Fatalf("zone-aware: expected %v, got %v", tt.expectedZoneAware, gotZoneAware)
			}
			if tt.expectedForce != gotForce {
				t.Fatalf("forceFailover: expected %v, got %v", tt.expectedForce, gotForce)
			}
		})
	}
}

func TestApplyZoneAwareLoadBalancer(t *testing.T) {
	proxyLocality := &core.Locality{Region: "region1", Zone: "zone1", SubZone: "subzone1"}
	// fakeCluster has localities region1/zone1/subzone{1,2,3}, region1/zone2, region2, region3.
	// Envoy zone-aware LB operates only inside one priority level, so when no failover is
	// configured all LocalityLbEndpoints must remain flat at priority 0.

	t.Run("zone-aware without explicit failover still region-buckets", func(t *testing.T) {
		c := buildFakeCluster()
		za := &networking.ZoneAwareLoadBalancerSetting{Enabled: wrappers.Bool(true)}
		ApplyZoneAwareLoadBalancer(c.LoadAssignment, nil, proxyLocality, nil, za, true)
		// Default behavior mirrors locality LB: same-region endpoints at p0, others compacted to p1,
		// so Envoy zone-aware LB only balances zones inside the proxy's region.
		want := []uint32{0, 0, 0, 0, 0, 1, 1}
		for i, ep := range c.LoadAssignment.Endpoints {
			if ep.Priority != want[i] {
				t.Errorf("endpoint[%d] locality %v: got priority %d, want %d",
					i, ep.Locality, ep.Priority, want[i])
			}
		}
	})

	t.Run("enableFailover=false leaves CLA untouched", func(t *testing.T) {
		c := buildFakeCluster()
		za := &networking.ZoneAwareLoadBalancerSetting{
			Failover: []*networking.ZoneAwareLoadBalancerSetting_Failover{{From: "region1", To: "region2"}},
		}
		ApplyZoneAwareLoadBalancer(c.LoadAssignment, nil, proxyLocality, nil, za, false)
		for i, ep := range c.LoadAssignment.Endpoints {
			if ep.Priority != 0 {
				t.Fatalf("endpoint[%d] priority %d; want 0 when enableFailover=false", i, ep.Priority)
			}
		}
	})

	t.Run("zone-aware Failover keeps same-region endpoints at priority 0", func(t *testing.T) {
		c := buildFakeCluster()
		za := &networking.ZoneAwareLoadBalancerSetting{
			Failover: []*networking.ZoneAwareLoadBalancerSetting_Failover{{From: "region1", To: "region2"}},
		}
		ApplyZoneAwareLoadBalancer(c.LoadAssignment, nil, proxyLocality, nil, za, true)
		// All region1 endpoints (any zone/subzone) → 0, failover-matched region2 → 1, region3 → 2.
		// This is the shape Envoy zone-aware LB needs: priority 0 spans every same-region zone.
		want := []uint32{0, 0, 0, 0, 0, 1, 2}
		for i, ep := range c.LoadAssignment.Endpoints {
			if ep.Priority != want[i] {
				t.Errorf("endpoint[%d] locality %v: got priority %d, want %d",
					i, ep.Locality, ep.Priority, want[i])
			}
		}
	})

	t.Run("zone-aware Failover with no matching rule compacts to two priorities", func(t *testing.T) {
		c := buildFakeCluster()
		// No failover rule applies for proxy region1 — all non-r1 endpoints collapse to one bucket.
		za := &networking.ZoneAwareLoadBalancerSetting{
			Failover: []*networking.ZoneAwareLoadBalancerSetting_Failover{{From: "region9", To: "region2"}},
		}
		ApplyZoneAwareLoadBalancer(c.LoadAssignment, nil, proxyLocality, nil, za, true)
		// region1 → 0, region2 + region3 → compacted to 1.
		want := []uint32{0, 0, 0, 0, 0, 1, 1}
		for i, ep := range c.LoadAssignment.Endpoints {
			if ep.Priority != want[i] {
				t.Errorf("endpoint[%d] locality %v: got priority %d, want %d",
					i, ep.Locality, ep.Priority, want[i])
			}
		}
	})

	t.Run("zone-aware FailoverPriority delegates to label-based priority", func(t *testing.T) {
		c := buildSmallClusterForFailOverPriority()
		wrapped := buildWrappedLocalityLbEndpoints()
		za := &networking.ZoneAwareLoadBalancerSetting{
			FailoverPriority: []string{"topology.istio.io/network", "topology.istio.io/cluster"},
		}
		proxyLabels := map[string]string{
			"topology.istio.io/network": "n1",
			"topology.istio.io/cluster": "c1",
		}
		ApplyZoneAwareLoadBalancer(c.LoadAssignment, wrapped, proxyLocality, proxyLabels, za, true)
		// applyFailoverPriorities reshapes Endpoints. We just confirm the slice is non-empty
		// and the priority math ran (some endpoint at priority 0).
		if len(c.LoadAssignment.Endpoints) == 0 {
			t.Fatal("expected non-empty endpoints after FailoverPriority")
		}
		foundP0 := false
		for _, ep := range c.LoadAssignment.Endpoints {
			if ep.Priority == 0 {
				foundP0 = true
				break
			}
		}
		if !foundP0 {
			t.Fatal("expected at least one priority-0 endpoint after FailoverPriority")
		}
	})

	t.Run("nil CLA is a no-op", func(t *testing.T) {
		za := &networking.ZoneAwareLoadBalancerSetting{Enabled: wrappers.Bool(true)}
		ApplyZoneAwareLoadBalancer(nil, nil, proxyLocality, nil, za, true)
	})
}

func buildEnvForClustersWithDistribute(t *testing.T, distribute []*networking.LocalityLoadBalancerSetting_Distribute) *model.Environment {
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

	configStore := memory.NewController(memory.Make(collections.Pilot))

	env := model.NewEnvironment()
	env.ServiceDiscovery = serviceDiscovery
	env.ConfigStore = configStore
	env.Watcher = meshwatcher.NewTestWatcher(meshConfig)
	env.VirtualServiceController = model.NewVirtualServiceController(
		configStore,
		model.VSControllerOptions{KrtDebugger: krt.GlobalDebugHandler},
		env.Watcher,
	)
	stop := test.NewStop(t)
	go configStore.Run(stop)
	go env.VirtualServiceController.Run(stop)
	kube.WaitForCacheSync("test", stop, configStore.HasSynced)
	kube.WaitForCacheSync("test", stop, env.VirtualServiceController.HasSynced)

	pushContext := model.NewPushContext()
	env.Init()
	pushContext.InitContext(env, nil, nil)
	env.SetPushContext(pushContext)
	pushContext.SetDestinationRulesForTesting([]config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
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

func buildEnvForClustersWithFailover(t *testing.T) *model.Environment {
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

	configStore := memory.NewController(memory.Make(collections.Pilot))

	env := model.NewEnvironment()
	env.ServiceDiscovery = serviceDiscovery
	env.ConfigStore = configStore
	env.Watcher = meshwatcher.NewTestWatcher(meshConfig)
	env.VirtualServiceController = model.NewVirtualServiceController(
		configStore,
		model.VSControllerOptions{KrtDebugger: krt.GlobalDebugHandler},
		env.Watcher,
	)
	stop := test.NewStop(t)
	go configStore.Run(stop)
	go env.VirtualServiceController.Run(stop)
	kube.WaitForCacheSync("test", stop, configStore.HasSynced)
	kube.WaitForCacheSync("test", stop, env.VirtualServiceController.HasSynced)

	pushContext := model.NewPushContext()
	env.Init()
	pushContext.InitContext(env, nil, nil)
	env.SetPushContext(pushContext)
	pushContext.SetDestinationRulesForTesting([]config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
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

func buildEnvForClustersWithFailoverPriority(t *testing.T, failoverPriority []string) *model.Environment {
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

	configStore := memory.NewController(memory.Make(collections.Pilot))

	env := model.NewEnvironment()
	env.ServiceDiscovery = serviceDiscovery
	env.ConfigStore = configStore
	env.Watcher = meshwatcher.NewTestWatcher(meshConfig)
	env.VirtualServiceController = model.NewVirtualServiceController(
		configStore,
		model.VSControllerOptions{KrtDebugger: krt.GlobalDebugHandler},
		env.Watcher,
	)
	stop := test.NewStop(t)
	go configStore.Run(stop)
	go env.VirtualServiceController.Run(stop)
	kube.WaitForCacheSync("test", stop, configStore.HasSynced)
	kube.WaitForCacheSync("test", stop, env.VirtualServiceController.HasSynced)

	pushContext := model.NewPushContext()
	env.Init()
	pushContext.InitContext(env, nil, nil)
	env.SetPushContext(pushContext)
	pushContext.SetDestinationRulesForTesting([]config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
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

func buildEnvForClustersWithMixedFailoverPriorityAndLocalityFailover(t *testing.T, failoverPriority []string) *model.Environment {
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
			Failover: []*networking.LocalityLoadBalancerSetting_Failover{
				{
					From: "region1",
					To:   "region2",
				},
			},
		},
	}

	configStore := memory.NewController(memory.Make(collections.Pilot))

	env := model.NewEnvironment()
	env.ServiceDiscovery = serviceDiscovery
	env.ConfigStore = configStore
	env.Watcher = meshwatcher.NewTestWatcher(meshConfig)
	env.VirtualServiceController = model.NewVirtualServiceController(
		configStore,
		model.VSControllerOptions{KrtDebugger: krt.GlobalDebugHandler},
		env.Watcher,
	)
	stop := test.NewStop(t)
	go configStore.Run(stop)
	go env.VirtualServiceController.Run(stop)
	kube.WaitForCacheSync("test", stop, configStore.HasSynced)
	kube.WaitForCacheSync("test", stop, env.VirtualServiceController.HasSynced)

	pushContext := model.NewPushContext()
	env.Init()
	pushContext.InitContext(env, nil, nil)
	env.SetPushContext(pushContext)
	pushContext.SetDestinationRulesForTesting([]config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
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
							HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
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
				{
					Locality: &core.Locality{
						Region:  "region3",
						Zone:    "zone3",
						SubZone: "subzone3",
					},
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: buildEndpointWithMultipleAddresses("1.2.3.4", "2001:1::1"),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 1,
							},
						},
						{
							HostIdentifier: buildEndpointWithMultipleAddresses("2.3.4.5", "2001:1::2"),
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

func buildSmallClusterForFailOverPriorityWithFailover() *cluster.Cluster {
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
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone2",
						SubZone: "subzone2",
					},
					LbEndpoints: []*endpoint.LbEndpoint{
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
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region3",
						Zone:    "zone3",
						SubZone: "subzone3",
					},
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: buildEndpoint("4.4.4.4"),
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
							HostIdentifier: buildEndpoint("5.5.5.5"),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 1,
							},
						},
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region3",
						Zone:    "zone3",
						SubZone: "subzone3",
					},
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: buildEndpoint("6.6.6.6"),
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

func buildEndpointWithMultipleAddresses(ip, additionIP string) *endpoint.LbEndpoint_Endpoint {
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
			AdditionalAddresses: []*endpoint.Endpoint_AdditionalAddress{
				{
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address: additionIP,
								PortSpecifier: &core.SocketAddress_PortValue{
									PortValue: 10001,
								},
							},
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
					Addresses: []string{"1.1.1.1"},
				},
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c2",
					},
					Addresses: []string{"2.2.2.2"},
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
					Addresses: []string{"3.3.3.3"},
				},
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c4",
					},
					Addresses: []string{"4.4.4.4"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[1],
		},
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n1",
						"topology.istio.io/cluster": "c1",
					},
					Addresses: []string{"1.2.3.4", "2001:1::1"},
				},
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c2",
					},
					Addresses: []string{"2.3.4.5", "2001:1::2"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[2],
		},
	}
}

func buildWrappedLocalityLbEndpointsForFailoverPriorityWithFailover() []*WrappedLocalityLbEndpoints {
	cluster := buildSmallClusterForFailOverPriorityWithFailover()
	return []*WrappedLocalityLbEndpoints{
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"topology.istio.io/network": "n1",
						"topology.istio.io/cluster": "c1",
					},
					Addresses: []string{"1.1.1.1"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[0],
		},
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c2",
					},
					Addresses: []string{"2.2.2.2"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[1],
		},
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n1",
						"topology.istio.io/cluster": "c3",
					},
					Addresses: []string{"3.3.3.3"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[2],
		},
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c4",
					},
					Addresses: []string{"4.4.4.4"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[3],
		},
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"topology.istio.io/network": "n2",
						"topology.istio.io/cluster": "c2",
					},
					Addresses: []string{"5.5.5.5"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[4],
		},
		{
			IstioEndpoints: []*model.IstioEndpoint{
				{
					Labels: map[string]string{
						"key":                       "value",
						"topology.istio.io/network": "n1",
						"topology.istio.io/cluster": "c6",
					},
					Addresses: []string{"6.6.6.6"},
				},
			},
			LocalityLbEndpoints: cluster.LoadAssignment.Endpoints[5],
		},
	}
}
