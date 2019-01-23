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

package v1alpha3_test

import (
	"fmt"
	"testing"
	"time"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	core "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
)

type ConfigType int

const (
	None ConfigType = iota
	Mesh
	DestinationRule
	DestinationRuleForOsDefault
	MeshWideTCPKeepaliveSeconds        = 11
	DestinationRuleTCPKeepaliveSeconds = 21
)

var (
	testMesh = meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
	}
)

func TestHTTPCircuitBreakerThresholds(t *testing.T) {
	g := NewGomegaWithT(t)

	directionInfos := []struct {
		direction    model.TrafficDirection
		clusterIndex int
	}{
		{
			direction:    model.TrafficDirectionOutbound,
			clusterIndex: 0,
		}, {
			direction:    model.TrafficDirectionInbound,
			clusterIndex: 1,
		},
	}
	settings := []*networking.ConnectionPoolSettings{
		nil,
		{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{
				Http1MaxPendingRequests:  1,
				Http2MaxRequests:         2,
				MaxRequestsPerConnection: 3,
				MaxRetries:               4,
			},
		}}

	for _, directionInfo := range directionInfos {
		for _, s := range settings {
			settingsName := "default"
			if s != nil {
				settingsName = "override"
			}
			testName := fmt.Sprintf("%s-%s", directionInfo.direction, settingsName)
			t.Run(testName, func(t *testing.T) {
				clusters, err := buildTestClusters("*.example.org", model.SidecarProxy, testMesh,
					&networking.DestinationRule{
						Host: "*.example.org",
						TrafficPolicy: &networking.TrafficPolicy{
							ConnectionPool: s,
						},
					})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(clusters)).To(Equal(4))
				cluster := clusters[directionInfo.clusterIndex]
				g.Expect(len(cluster.CircuitBreakers.Thresholds)).To(Equal(1))
				thresholds := cluster.CircuitBreakers.Thresholds[0]

				if s == nil {
					// Assume the correct defaults for this direction.
					g.Expect(thresholds).To(Equal(core.GetDefaultCircuitBreakerThresholds(directionInfo.direction)))
				} else {
					// Verify that the values were set correctly.
					g.Expect(thresholds.MaxPendingRequests).To(Not(BeNil()))
					g.Expect(thresholds.MaxPendingRequests.Value).To(Equal(uint32(s.Http.Http1MaxPendingRequests)))
					g.Expect(thresholds.MaxRequests).To(Not(BeNil()))
					g.Expect(thresholds.MaxRequests.Value).To(Equal(uint32(s.Http.Http2MaxRequests)))
					g.Expect(cluster.MaxRequestsPerConnection).To(Not(BeNil()))
					g.Expect(cluster.MaxRequestsPerConnection.Value).To(Equal(uint32(s.Http.MaxRequestsPerConnection)))
					g.Expect(thresholds.MaxRetries).To(Not(BeNil()))
					g.Expect(thresholds.MaxRetries.Value).To(Equal(uint32(s.Http.MaxRetries)))
				}
			})
		}
	}
}

func buildTestClusters(serviceHostname string, nodeType model.NodeType, mesh meshconfig.MeshConfig,
	destRule proto.Message) ([]*apiv2.Cluster, error) {
	configgen := core.NewConfigGenerator([]plugin.Plugin{})

	serviceDiscovery := &fakes.ServiceDiscovery{}

	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: model.ProtocolHTTP,
	}
	service := &model.Service{
		Hostname:    model.Hostname(serviceHostname),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
	}
	instance := &model.ServiceInstance{
		Service: service,
		Endpoint: model.NetworkEndpoint{
			Address:     "192.168.1.1",
			Port:        10001,
			ServicePort: servicePort,
		},
	}
	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)
	serviceDiscovery.GetProxyServiceInstancesReturns([]*model.ServiceInstance{instance}, nil)

	env := newTestEnvironment(serviceDiscovery, mesh)
	env.PushContext.SetDestinationRules([]model.Config{
		{ConfigMeta: model.ConfigMeta{
			Type:    model.DestinationRule.Type,
			Version: model.DestinationRule.Version,
			Name:    "acme",
		},
			Spec: destRule,
		}})

	var proxy *model.Proxy
	switch nodeType {
	case model.SidecarProxy:
		proxy = &model.Proxy{
			ClusterID:   "some-cluster-id",
			Type:        model.SidecarProxy,
			IPAddresses: []string{"6.6.6.6"},
			DNSDomain:   "com",
			Metadata:    make(map[string]string),
		}
	case model.Router:
		proxy = &model.Proxy{
			ClusterID:   "some-cluster-id",
			Type:        model.Router,
			IPAddresses: []string{"6.6.6.6"},
			DNSDomain:   "default.example.org",
			Metadata:    make(map[string]string),
		}
	default:
		panic(fmt.Sprintf("unsupported node type: %v", nodeType))
	}

	return configgen.BuildClusters(env, proxy, env.PushContext)
}

func TestBuildGatewayClustersWithRingHashLb(t *testing.T) {
	g := NewGomegaWithT(t)

	ttl := time.Nanosecond * 100
	clusters, err := buildTestClusters("*.example.org", model.Router, testMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
						ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
							MinimumRingSize: uint64(2),
							HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
								HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
									Name: "hash-cookie",
									Ttl:  &ttl,
								},
							},
						},
					},
				},
			},
		})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(len(clusters)).To(Equal(3))

	cluster := clusters[0]
	g.Expect(cluster.LbPolicy).To(Equal(apiv2.Cluster_RING_HASH))
	g.Expect(cluster.GetRingHashLbConfig().GetMinimumRingSize().GetValue()).To(Equal(uint64(2)))
	g.Expect(cluster.Name).To(Equal("outbound|8080||*.example.org"))
	g.Expect(cluster.Type).To(Equal(apiv2.Cluster_EDS))
	g.Expect(cluster.ConnectTimeout).To(Equal(time.Duration(10000000001)))
}

func newTestEnvironment(serviceDiscovery model.ServiceDiscovery, mesh meshconfig.MeshConfig) *model.Environment {
	configStore := &fakes.IstioConfigStore{}

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ServiceAccounts:  &fakes.ServiceAccounts{},
		IstioConfigStore: configStore,
		Mesh:             &mesh,
		MixerSAN:         []string{},
	}

	env.PushContext = model.NewPushContext()
	_ = env.PushContext.InitContext(env)

	return env
}

func TestBuildSidecarClustersWithIstioMutualAndSNI(t *testing.T) {
	g := NewGomegaWithT(t)

	clusters, err := buildSniTestClusters("foo.com")
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(len(clusters)).To(Equal(5))

	cluster := clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	g.Expect(cluster.TlsContext.GetSni()).To(Equal("foo.com"))

	clusters, err = buildSniTestClusters("")
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(len(clusters)).To(Equal(5))

	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	g.Expect(cluster.TlsContext.GetSni()).To(Equal("outbound_.8080_.foobar_.foo.example.org"))
}

func buildSniTestClusters(sniValue string) ([]*apiv2.Cluster, error) {
	return buildTestClusters("foo.example.org", model.SidecarProxy, testMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
			Subsets: []*networking.Subset{
				{
					Name:   "foobar",
					Labels: map[string]string{"foo": "bar"},
					TrafficPolicy: &networking.TrafficPolicy{
						PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
							{
								Port: &networking.PortSelector{
									Port: &networking.PortSelector_Number{Number: 8080},
								},
								Tls: &networking.TLSSettings{
									Mode: networking.TLSSettings_ISTIO_MUTUAL,
									Sni:  sniValue,
								},
							},
						},
					},
				},
			},
		})
}

func TestBuildSidecarClustersWithMeshWideTCPKeepalive(t *testing.T) {
	g := NewGomegaWithT(t)

	// Do not set tcp_keepalive anywhere
	clusters, err := buildTestClustersWithTCPKeepalive(None)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(5))
	cluster := clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// UpstreamConnectionOptions should be nil. TcpKeepalive is the only field in it currently.
	g.Expect(cluster.UpstreamConnectionOptions).To(BeNil())

	// Set mesh wide default for tcp_keepalive.
	clusters, err = buildTestClustersWithTCPKeepalive(Mesh)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(5))
	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// KeepaliveTime should be set but rest should be nil.
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes).To(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime.Value).To(Equal(uint32(MeshWideTCPKeepaliveSeconds)))
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval).To(BeNil())

	// Set DestinationRule override for tcp_keepalive.
	clusters, err = buildTestClustersWithTCPKeepalive(DestinationRule)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(5))
	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// KeepaliveTime should be set but rest should be nil.
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes).To(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime.Value).To(Equal(uint32(DestinationRuleTCPKeepaliveSeconds)))
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval).To(BeNil())

	// Set DestinationRule override for tcp_keepalive with empty value.
	clusters, err = buildTestClustersWithTCPKeepalive(DestinationRuleForOsDefault)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(5))
	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// TcpKeepalive should be present but with nil values.
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive).NotTo(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes).To(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime).To(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval).To(BeNil())
}

func buildTestClustersWithTCPKeepalive(configType ConfigType) ([]*apiv2.Cluster, error) {
	// Set mesh wide defaults.
	mesh := testMesh
	if configType != None {
		mesh.TcpKeepalive = &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
			Time: &types.Duration{
				Seconds: MeshWideTCPKeepaliveSeconds,
				Nanos:   0,
			},
		}
	}

	// Set DestinationRule override.
	var destinationRuleTCPKeepalive *networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive
	if configType == DestinationRule {
		destinationRuleTCPKeepalive = &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
			Time: &types.Duration{
				Seconds: DestinationRuleTCPKeepaliveSeconds,
				Nanos:   0,
			},
		}
	}

	// Set empty tcp_keepalive.
	if configType == DestinationRuleForOsDefault {
		destinationRuleTCPKeepalive = &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{}
	}

	return buildTestClusters("foo.example.org", model.SidecarProxy, mesh,
		&networking.DestinationRule{
			Host: "*.example.org",
			Subsets: []*networking.Subset{
				{
					Name:   "foobar",
					Labels: map[string]string{"foo": "bar"},
					TrafficPolicy: &networking.TrafficPolicy{
						PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
							{
								Port: &networking.PortSelector{
									Port: &networking.PortSelector_Number{Number: 8080},
								},
								ConnectionPool: &networking.ConnectionPoolSettings{
									Tcp: &networking.ConnectionPoolSettings_TCPSettings{
										TcpKeepalive: destinationRuleTCPKeepalive,
									},
								},
							},
						},
					},
				},
			},
		})
}

func TestApplyLocalitySetting(t *testing.T) {
	g := NewGomegaWithT(t)

	proxy := &model.Proxy{
		ClusterID:   "some-cluster-id",
		Type:        model.SidecarProxy,
		IPAddresses: []string{"6.6.6.6"},
		DNSDomain:   "com",
		Metadata:    make(map[string]string),
		Locality: model.Locality{
			Region:  "region1",
			Zone:    "zone1",
			SubZone: "subzone1",
		},
	}

	env := buildEnvForClustersWithDistribute()
	cluster := buildFakeCluster()
	core.ApplyLocalityLBSetting(proxy, cluster, env.PushContext)

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
	core.ApplyLocalityLBSetting(proxy, cluster, env.PushContext)
	for _, localityEndpoint := range cluster.LoadAssignment.Endpoints {
		if localityEndpoint.Locality.Region == proxy.Locality.Region {
			if localityEndpoint.Locality.Zone == proxy.Locality.Zone {
				if localityEndpoint.Locality.SubZone == proxy.Locality.SubZone {
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
