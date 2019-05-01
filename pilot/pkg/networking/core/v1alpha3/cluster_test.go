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
	"reflect"
	"strings"
	"testing"
	"time"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

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
				clusters, err := buildTestClusters("*.example.org", 0, model.SidecarProxy, nil, testMesh,
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
					g.Expect(thresholds).To(Equal(getDefaultCircuitBreakerThresholds(directionInfo.direction)))
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

func TestCommonHttpProtocolOptions(t *testing.T) {
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
	settings := &networking.ConnectionPoolSettings{
		Http: &networking.ConnectionPoolSettings_HTTPSettings{
			Http1MaxPendingRequests: 1,
			IdleTimeout:             &types.Duration{Seconds: 15},
		},
	}

	for _, directionInfo := range directionInfos {
		settingsName := "default"
		if settings != nil {
			settingsName = "override"
		}
		testName := fmt.Sprintf("%s-%s", directionInfo.direction, settingsName)
		t.Run(testName, func(t *testing.T) {
			clusters, err := buildTestClusters("*.example.org", 0, model.SidecarProxy, nil, testMesh,
				&networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						ConnectionPool: settings,
					},
				})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(clusters)).To(Equal(4))
			cluster := clusters[directionInfo.clusterIndex]
			g.Expect(cluster.CommonHttpProtocolOptions).To(Not(BeNil()))
			commonHTTPProtocolOptions := cluster.CommonHttpProtocolOptions

			// Verify that the values were set correctly.
			g.Expect(commonHTTPProtocolOptions.IdleTimeout).To(Not(BeNil()))
			g.Expect(*commonHTTPProtocolOptions.IdleTimeout).To(Equal(time.Duration(15000000000)))
		})
	}
}

func buildTestClusters(serviceHostname string, serviceResolution model.Resolution,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadata(serviceHostname, serviceResolution, nodeType, locality, mesh, destRule, make(map[string]string))
}

func buildTestClustersWithProxyMetadata(serviceHostname string, serviceResolution model.Resolution,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, meta map[string]string) ([]*apiv2.Cluster, error) {
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
		Resolution:  serviceResolution,
	}

	instances := []*model.ServiceInstance{
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.1",
				Port:        10001,
				ServicePort: servicePort,
				Locality:    "region1/zone1/subzone1",
				LbWeight:    40,
			},
		},
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.2",
				Port:        10001,
				ServicePort: servicePort,
				Locality:    "region1/zone1/subzone2",
				LbWeight:    20,
			},
		},
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.3",
				Port:        10001,
				ServicePort: servicePort,
				Locality:    "region2/zone1/subzone1",
				LbWeight:    40,
			},
		},
	}

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)
	serviceDiscovery.GetProxyServiceInstancesReturns(instances, nil)
	serviceDiscovery.InstancesByPortReturns(instances, nil)

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
			Locality:    locality,
			DNSDomain:   "com",
			Metadata:    meta,
		}
	case model.Router:
		proxy = &model.Proxy{
			ClusterID:   "some-cluster-id",
			Type:        model.Router,
			IPAddresses: []string{"6.6.6.6"},
			Locality:    locality,
			DNSDomain:   "default.example.org",
			Metadata:    meta,
		}
	default:
		panic(fmt.Sprintf("unsupported node type: %v", nodeType))
	}

	proxy.ServiceInstances, _ = serviceDiscovery.GetProxyServiceInstances(proxy)

	return configgen.BuildClusters(env, proxy, env.PushContext)
}

func TestBuildGatewayClustersWithRingHashLb(t *testing.T) {
	g := NewGomegaWithT(t)

	ttl := time.Nanosecond * 100
	clusters, err := buildTestClusters("*.example.org", 0, model.Router, nil, testMesh,
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
	g.Expect(cluster.ConnectTimeout).To(Equal(time.Duration(10000000001)))
}

func TestBuildGatewayClustersWithRingHashLbDefaultMinRingSize(t *testing.T) {
	g := NewGomegaWithT(t)

	ttl := time.Nanosecond * 100
	clusters, err := buildTestClusters("*.example.org", 0, model.Router, nil, testMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
						ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
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
	g.Expect(cluster.GetRingHashLbConfig().GetMinimumRingSize().GetValue()).To(Equal(uint64(1024)))
	g.Expect(cluster.Name).To(Equal("outbound|8080||*.example.org"))
	g.Expect(cluster.ConnectTimeout).To(Equal(time.Duration(10000000001)))
}

func newTestEnvironment(serviceDiscovery model.ServiceDiscovery, mesh meshconfig.MeshConfig) *model.Environment {
	configStore := &fakes.IstioConfigStore{}

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             &mesh,
	}

	env.PushContext = model.NewPushContext()
	_ = env.PushContext.InitContext(env)

	return env
}

func TestBuildSidecarClustersWithIstioMutualAndSNI(t *testing.T) {
	g := NewGomegaWithT(t)

	clusters, err := buildSniTestClusters("foo.com")
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(len(clusters)).To(Equal(4))

	cluster := clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	g.Expect(cluster.TlsContext.GetSni()).To(Equal("foo.com"))

	clusters, err = buildSniTestClusters("")
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(len(clusters)).To(Equal(4))

	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	g.Expect(cluster.TlsContext.GetSni()).To(Equal("outbound_.8080_.foobar_.foo.example.org"))
}

func buildSniTestClusters(sniValue string) ([]*apiv2.Cluster, error) {
	return buildSniTestClustersWithMetadata(sniValue, make(map[string]string))
}

func buildSniDnatTestClusters(sniValue string) ([]*apiv2.Cluster, error) {
	return buildSniTestClustersWithMetadata(sniValue, map[string]string{"ROUTER_MODE": string(model.SniDnatRouter)})
}

func buildSniTestClustersWithMetadata(sniValue string, meta map[string]string) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadata("foo.example.org", 0, model.Router, nil, testMesh,
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
		},
		meta,
	)
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

	return buildTestClusters("foo.example.org", 0, model.SidecarProxy, nil, mesh,
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

func TestClusterMetadata(t *testing.T) {
	g := NewGomegaWithT(t)

	destRule := &networking.DestinationRule{
		Host: "*.example.org",
		Subsets: []*networking.Subset{
			{Name: "Subset 1"},
			{Name: "Subset 2"},
		},
		TrafficPolicy: &networking.TrafficPolicy{
			ConnectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					MaxRequestsPerConnection: 1,
				},
			},
		},
	}

	clusters, err := buildTestClusters("*.example.org", 0, model.SidecarProxy, nil, testMesh, destRule)
	g.Expect(err).NotTo(HaveOccurred())

	clustersWithMetadata := 0

	for _, cluster := range clusters {
		if strings.HasPrefix(cluster.Name, "outbound") || strings.HasPrefix(cluster.Name, "inbound") {
			clustersWithMetadata++
			g.Expect(cluster.Metadata).NotTo(BeNil())
			md := cluster.Metadata
			g.Expect(md.FilterMetadata["istio"]).NotTo(BeNil())
			istio := md.FilterMetadata["istio"]
			g.Expect(istio.Fields["config"]).NotTo(BeNil())
			dr := istio.Fields["config"]
			g.Expect(dr.GetStringValue()).To(Equal("/apis//v1alpha3/namespaces//destination-rule/acme"))
		} else {
			g.Expect(cluster.Metadata).To(BeNil())
		}
	}

	g.Expect(clustersWithMetadata).To(Equal(len(destRule.Subsets) + 2)) // outbound  outbound subsets  inbound

	sniClusters, err := buildSniDnatTestClusters("test-sni")
	g.Expect(err).NotTo(HaveOccurred())

	for _, cluster := range sniClusters {
		if strings.HasPrefix(cluster.Name, "outbound") {
			g.Expect(cluster.Metadata).NotTo(BeNil())
			md := cluster.Metadata
			g.Expect(md.FilterMetadata["istio"]).NotTo(BeNil())
			istio := md.FilterMetadata["istio"]
			g.Expect(istio.Fields["config"]).NotTo(BeNil())
			dr := istio.Fields["config"]
			g.Expect(dr.GetStringValue()).To(Equal("/apis//v1alpha3/namespaces//destination-rule/acme"))
		} else {
			g.Expect(cluster.Metadata).To(BeNil())
		}
	}
}

func TestConditionallyConvertToIstioMtls(t *testing.T) {
	tlsSettings := &networking.TLSSettings{
		Mode:              networking.TLSSettings_ISTIO_MUTUAL,
		CaCertificates:    model.DefaultRootCert,
		ClientCertificate: model.DefaultCertChain,
		PrivateKey:        model.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	tests := []struct {
		name  string
		tls   *networking.TLSSettings
		sans  []string
		sni   string
		proxy *model.Proxy
		want  *networking.TLSSettings
	}{
		{
			"Destination rule TLS sni and SAN override",
			tlsSettings,
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: map[string]string{}},
			tlsSettings,
		},
		{
			"Destination rule TLS sni and SAN override absent",
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
				CaCertificates:    model.DefaultRootCert,
				ClientCertificate: model.DefaultCertChain,
				PrivateKey:        model.DefaultKey,
				SubjectAltNames:   []string{},
				Sni:               "",
			},
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: map[string]string{}},
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
				CaCertificates:    model.DefaultRootCert,
				ClientCertificate: model.DefaultCertChain,
				PrivateKey:        model.DefaultKey,
				SubjectAltNames:   []string{"spiffee://foo/serviceaccount/1"},
				Sni:               "foo.com",
			},
		},
		{
			"Cert path override",
			tlsSettings,
			[]string{},
			"",
			&model.Proxy{Metadata: map[string]string{
				model.NodeMetadataTLSClientCertChain: "/custom/chain.pem",
				model.NodeMetadataTLSClientKey:       "/custom/key.pem",
				model.NodeMetadataTLSClientRootCert:  "/custom/root.pem",
			}},
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
				CaCertificates:    "/custom/root.pem",
				ClientCertificate: "/custom/chain.pem",
				PrivateKey:        "/custom/key.pem",
				SubjectAltNames:   []string{"custom.foo.com"},
				Sni:               "custom.foo.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conditionallyConvertToIstioMtls(tt.tls, tt.sans, tt.sni, tt.proxy)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality empty result %#v, but got %#v", tt.want, got)
			}
		})
	}
}

func TestLocalityLB(t *testing.T) {
	g := NewGomegaWithT(t)
	// Distribute locality loadbalancing setting
	testMesh.LocalityLbSetting = &meshconfig.LocalityLoadBalancerSetting{
		Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
			{
				From: "region1/zone1/subzone1",
				To: map[string]uint32{
					"region1/zone1/*":        50,
					"region2/zone1/subzone1": 50,
				},
			},
		},
	}

	clusters, err := buildTestClusters("*.example.org", model.DNSLB, model.SidecarProxy,
		&core.Locality{
			Region:  "region1",
			Zone:    "zone1",
			SubZone: "subzone1",
		}, testMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
			TrafficPolicy: &networking.TrafficPolicy{
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 5,
				},
			},
		})
	g.Expect(err).NotTo(HaveOccurred())

	if clusters[0].CommonLbConfig == nil {
		t.Errorf("CommonLbConfig should be set for cluster %+v", clusters[0])
	}

	g.Expect(len(clusters[0].LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range clusters[0].LoadAssignment.Endpoints {
		locality := localityLbEndpoint.Locality
		if locality.Region == "region1" && locality.SubZone == "subzone1" {
			g.Expect(localityLbEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(34)))
			g.Expect(localityLbEndpoint.LbEndpoints[0].LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
		} else if locality.Region == "region1" && locality.SubZone == "subzone2" {
			g.Expect(localityLbEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(17)))
			g.Expect(localityLbEndpoint.LbEndpoints[0].LoadBalancingWeight.GetValue()).To(Equal(uint32(20)))
		} else if locality.Region == "region2" {
			g.Expect(localityLbEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(50)))
			g.Expect(len(localityLbEndpoint.LbEndpoints)).To(Equal(1))
			g.Expect(localityLbEndpoint.LbEndpoints[0].LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
		}

	}
}

func TestBuildLocalityLbEndpoints(t *testing.T) {
	g := NewGomegaWithT(t)
	serviceDiscovery := &fakes.ServiceDiscovery{}

	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: model.ProtocolHTTP,
	}
	service := &model.Service{
		Hostname:    model.Hostname("*.example.org"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Resolution:  model.DNSLB,
	}
	instances := []*model.ServiceInstance{
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.1",
				Port:        10001,
				ServicePort: servicePort,
				Locality:    "region1/zone1/subzone1",
				LbWeight:    30,
			},
		},
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.2",
				Port:        10001,
				ServicePort: servicePort,
				Locality:    "region1/zone1/subzone1",
				LbWeight:    30,
			},
		},
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.3",
				Port:        10001,
				ServicePort: servicePort,
				Locality:    "region2/zone1/subzone1",
				LbWeight:    40,
			},
		},
	}

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)
	serviceDiscovery.InstancesByPortReturns(instances, nil)

	env := newTestEnvironment(serviceDiscovery, testMesh)

	localityLbEndpoints := buildLocalityLbEndpoints(env, model.GetNetworkView(nil), service, 8080, nil)
	g.Expect(len(localityLbEndpoints)).To(Equal(2))
	for _, ep := range localityLbEndpoints {
		if ep.Locality.Region == "region1" {
			g.Expect(ep.LoadBalancingWeight.GetValue()).To(Equal(uint32(60)))
		} else if ep.Locality.Region == "region2" {
			g.Expect(ep.LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
		}
	}
}

func TestClusterDiscoveryTypeAndLbPolicy(t *testing.T) {
	g := NewGomegaWithT(t)

	clusters, err := buildTestClusters("*.example.org", model.Passthrough, model.SidecarProxy, nil, testMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 5,
				},
			},
		})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clusters[0].LbPolicy).To(Equal(apiv2.Cluster_ORIGINAL_DST_LB))
	g.Expect(clusters[0].GetClusterDiscoveryType()).To(Equal(&apiv2.Cluster_Type{Type: apiv2.Cluster_ORIGINAL_DST}))
}
