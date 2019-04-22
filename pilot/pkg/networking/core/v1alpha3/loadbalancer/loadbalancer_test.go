package loadbalancer

import (
	"reflect"
	"testing"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
)

func TestApplyLocalitySetting(t *testing.T) {
	locality := &envoycore.Locality{
		Region:  "region1",
		Zone:    "zone1",
		SubZone: "subzone1",
	}

	t.Run("Distribute", func(t *testing.T) {
		tests := []struct {
			name       string
			distribute []*meshconfig.LocalityLoadBalancerSetting_Distribute
			expected   []int
		}{
			{
				name: "distribution between subzones",
				distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
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
				ApplyLocalityLBSetting(locality, cluster.LoadAssignment, env.Mesh.LocalityLbSetting)
				weights := []int{}
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
		g := NewGomegaWithT(t)
		env := buildEnvForClustersWithFailover()
		cluster := buildFakeCluster()
		ApplyLocalityLBSetting(locality, cluster.LoadAssignment, env.Mesh.LocalityLbSetting)
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
		g := NewGomegaWithT(t)
		env := buildEnvForClustersWithFailover()
		cluster := buildSmallCluster()
		ApplyLocalityLBSetting(locality, cluster.LoadAssignment, env.Mesh.LocalityLbSetting)
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
		g := NewGomegaWithT(t)
		env := buildEnvForClustersWithFailover()
		cluster := buildSmallClusterWithNilLocalities()
		ApplyLocalityLBSetting(locality, cluster.LoadAssignment, env.Mesh.LocalityLbSetting)
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
}

func buildEnvForClustersWithDistribute(distribute []*meshconfig.LocalityLoadBalancerSetting_Distribute) *model.Environment {
	serviceDiscovery := &fakes.ServiceDiscovery{}

	serviceDiscovery.ServicesReturns([]*model.Service{
		{
			Hostname:    "test.example.org",
			Address:     "1.1.1.1",
			ClusterVIPs: make(map[string]string),
			Ports: model.PortList{
				&model.Port{
					Name:     "default",
					Port:     8080,
					Protocol: model.ProtocolHTTP,
				},
			},
		},
	}, nil)

	meshConfig := &meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		LocalityLbSetting: &meshconfig.LocalityLoadBalancerSetting{
			Distribute: distribute,
		},
	}

	configStore := &fakes.IstioConfigStore{}

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             meshConfig,
		MixerSAN:         []string{},
	}

	env.PushContext = model.NewPushContext()
	env.PushContext.InitContext(env)
	env.PushContext.SetDestinationRules([]model.Config{
		{ConfigMeta: model.ConfigMeta{
			Type:    model.DestinationRule.Type,
			Version: model.DestinationRule.Version,
			Name:    "acme",
		},
			Spec: &networking.DestinationRule{
				Host: "test.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						ConsecutiveErrors: 5,
					},
				},
			},
		}})

	return env
}

func buildEnvForClustersWithFailover() *model.Environment {
	serviceDiscovery := &fakes.ServiceDiscovery{}

	serviceDiscovery.ServicesReturns([]*model.Service{
		{
			Hostname:    "test.example.org",
			Address:     "1.1.1.1",
			ClusterVIPs: make(map[string]string),
			Ports: model.PortList{
				&model.Port{
					Name:     "default",
					Port:     8080,
					Protocol: model.ProtocolHTTP,
				},
			},
		},
	}, nil)

	meshConfig := &meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		LocalityLbSetting: &meshconfig.LocalityLoadBalancerSetting{
			Failover: []*meshconfig.LocalityLoadBalancerSetting_Failover{
				{
					From: "region1",
					To:   "region2",
				},
			},
		},
	}

	configStore := &fakes.IstioConfigStore{}

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             meshConfig,
		MixerSAN:         []string{},
	}

	env.PushContext = model.NewPushContext()
	env.PushContext.InitContext(env)
	env.PushContext.SetDestinationRules([]model.Config{
		{ConfigMeta: model.ConfigMeta{
			Type:    model.DestinationRule.Type,
			Version: model.DestinationRule.Version,
			Name:    "acme",
		},
			Spec: &networking.DestinationRule{
				Host: "test.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						ConsecutiveErrors: 5,
					},
				},
			},
		}})

	return env
}

func buildFakeCluster() *apiv2.Cluster {
	return &apiv2.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &apiv2.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []endpoint.LocalityLbEndpoints{
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone3",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone2",
						SubZone: "",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region2",
						Zone:    "",
						SubZone: "",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region3",
						Zone:    "",
						SubZone: "",
					},
				},
			},
		},
	}

}

func buildSmallCluster() *apiv2.Cluster {
	return &apiv2.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &apiv2.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []endpoint.LocalityLbEndpoints{
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
			},
		},
	}
}

func buildSmallClusterWithNilLocalities() *apiv2.Cluster {
	return &apiv2.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &apiv2.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []endpoint.LocalityLbEndpoints{
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{},
				{
					Locality: &envoycore.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
			},
		},
	}
}
