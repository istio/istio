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
	"testing"
	"time"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	"istio.io/api/mesh/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
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
					g.Expect(thresholds).To(Equal(GetDefaultCircuitBreakerThresholds(directionInfo.direction)))
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
	configgen := NewConfigGenerator([]plugin.Plugin{})

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

func TestBuildDefaultTrafficPolicy(t *testing.T) {
	defaultEnv := &model.Environment{Mesh: &v1alpha1.MeshConfig{ConnectTimeout: &types.Duration{Seconds: 1, Nanos: 1}}}
	var tests = []struct {
		name          string
		discoveryType apiv2.Cluster_DiscoveryType
		direction     model.TrafficDirection
		protocol      model.Protocol
		wantOutDet    bool
		wantLBPolicy  networking.LoadBalancerSettings_SimpleLB
	}{
		{
			name:      "Inbound HTTP Traffic Policy has no OutlierDetection",
			direction: model.TrafficDirectionInbound,
			protocol:  model.ProtocolHTTP,
		},
		{
			name:      "Inbound HTTP2 Traffic Policy has no OutlierDetection",
			direction: model.TrafficDirectionInbound,
			protocol:  model.ProtocolHTTP2,
		},
		{
			name:      "Inbound HTTPS Traffic Policy has no OutlierDetection",
			direction: model.TrafficDirectionInbound,
			protocol:  model.ProtocolHTTPS,
		},
		{
			name:      "Inbound TCP Traffic Policy has no OutlierDetection",
			direction: model.TrafficDirectionInbound,
			protocol:  model.ProtocolTCP,
		},
		{
			name:      "Inbound UDP Traffic Policy has no OutlierDetection",
			direction: model.TrafficDirectionInbound,
			protocol:  model.ProtocolUDP,
		},
		{
			name:       "Outbound HTTP Traffic Policy has OutlierDetection",
			direction:  model.TrafficDirectionOutbound,
			protocol:   model.ProtocolHTTP,
			wantOutDet: true,
		},
		{
			name:       "Outbound HTTP2 Traffic Policy has OutlierDetection",
			direction:  model.TrafficDirectionOutbound,
			protocol:   model.ProtocolHTTP2,
			wantOutDet: true,
		},
		{
			name:      "Outbound HTTPS Traffic Policy has OutlierDetection",
			direction: model.TrafficDirectionOutbound,
			protocol:  model.ProtocolHTTPS,
		},
		{
			name:      "Outbound TCP Traffic Policy has no OutlierDetection",
			direction: model.TrafficDirectionOutbound,
			protocol:  model.ProtocolTCP,
		},
		{
			name:      "Outbound UDP Traffic Policy has no OutlierDetection",
			direction: model.TrafficDirectionOutbound,
			protocol:  model.ProtocolUDP,
		},
		// TODO: Work out whether GRPC and GRPC should be set or not
		{
			name:          "Static Cluster Discovery uses round robin LB",
			discoveryType: apiv2.Cluster_STATIC,
			wantLBPolicy:  networking.LoadBalancerSettings_ROUND_ROBIN,
		},
		{
			name:          "Strict DNS Cluster Discovery uses round robin LB",
			discoveryType: apiv2.Cluster_STRICT_DNS,
			wantLBPolicy:  networking.LoadBalancerSettings_ROUND_ROBIN,
		},
		{
			name:          "Logical DNS Cluster Discovery uses round robin LB",
			discoveryType: apiv2.Cluster_LOGICAL_DNS,
			wantLBPolicy:  networking.LoadBalancerSettings_ROUND_ROBIN,
		},
		{
			name:          "EDS Cluster Discovery uses round robin LB",
			discoveryType: apiv2.Cluster_EDS,
			wantLBPolicy:  networking.LoadBalancerSettings_ROUND_ROBIN,
		},
		{
			name:          "Original DST Cluster Discovery uses passthrough LB",
			discoveryType: apiv2.Cluster_ORIGINAL_DST,
			wantLBPolicy:  networking.LoadBalancerSettings_PASSTHROUGH,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			got := buildDefaultTrafficPolicy(defaultEnv, tt.discoveryType, tt.direction, &model.Port{Protocol: tt.protocol})
			if tt.wantOutDet {
				g.Expect(got.OutlierDetection).To(Not(BeNil()))
			} else {
				g.Expect(got.OutlierDetection).To(BeNil())
			}
			g.Expect(got.LoadBalancer.LbPolicy).To(Equal(&networking.LoadBalancerSettings_Simple{Simple: tt.wantLBPolicy}))
			g.Expect(got.ConnectionPool.Tcp.ConnectTimeout).To(Equal(defaultEnv.Mesh.ConnectTimeout))
		})
	}

}
