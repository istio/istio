package loadbalancer

import (
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
	"istio.io/istio/pilot/pkg/networking/util"
)

func TestApplyLocalitySetting(t *testing.T) {
	g := NewGomegaWithT(t)
	locality := &envoycore.Locality{
		Region:  "region1",
		Zone:    "zone1",
		SubZone: "subzone1",
	}
	env := buildEnvForClustersWithDistribute()
	cluster := buildFakeCluster()
	ApplyLocalityLBSetting(locality, cluster.LoadAssignment, env.Mesh.LocalityLbSetting)

	for _, localityEndpoint := range cluster.LoadAssignment.Endpoints {
		if util.LocalityMatch(localityEndpoint.Locality, "region1/zone1/subzone1") {
			g.Expect(localityEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(90)))
			continue
		}
		if util.LocalityMatch(localityEndpoint.Locality, "region1/zone1") {
			g.Expect(localityEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(5)))
			continue
		}
		g.Expect(localityEndpoint.LbEndpoints).To(BeNil())
	}

	env = buildEnvForClustersWithFailover()
	cluster = buildFakeCluster()
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
}

func buildEnvForClustersWithDistribute() *model.Environment {
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
			Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
				{
					From: "region1/zone1/subzone1",
					To: map[string]uint32{
						"region1/zone1/subzone1": 90,
						"region1/zone1/subzone2": 5,
						"region1/zone1/subzone3": 5,
					},
				},
			},
		},
	}

	configStore := &fakes.IstioConfigStore{}

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ServiceAccounts:  &fakes.ServiceAccounts{},
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
		ServiceAccounts:  &fakes.ServiceAccounts{},
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
					LbEndpoints: []endpoint.LbEndpoint{},
					LoadBalancingWeight: &types.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
					LbEndpoints: []endpoint.LbEndpoint{},
					LoadBalancingWeight: &types.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone3",
					},
					LbEndpoints: []endpoint.LbEndpoint{},
					LoadBalancingWeight: &types.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region1",
						Zone:    "zone2",
						SubZone: "",
					},
					LbEndpoints: []endpoint.LbEndpoint{},
					LoadBalancingWeight: &types.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region2",
						Zone:    "",
						SubZone: "",
					},
					LbEndpoints: []endpoint.LbEndpoint{},
					LoadBalancingWeight: &types.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
				{
					Locality: &envoycore.Locality{
						Region:  "region3",
						Zone:    "",
						SubZone: "",
					},
					LbEndpoints: []endpoint.LbEndpoint{},
					LoadBalancingWeight: &types.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
			},
		},
	}
}
