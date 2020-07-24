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
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	apiv2_cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoyEndpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	authn_beta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
)

type ConfigType int

const (
	None ConfigType = iota
	Mesh
	DestinationRule
	DestinationRuleForOsDefault
	MeshWideTCPKeepaliveSeconds        = 11
	DestinationRuleTCPKeepaliveSeconds = 21
	TestServiceNamespace               = "bar"
	// FQDN service name in namespace TestServiceNamespace. Note the mesh config domain is empty.
	TestServiceNHostname = "foo.bar"
)

var (
	testMesh = meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		EnableAutoMtls: &types.BoolValue{
			Value: false,
		},
	}
)

func TestHTTPCircuitBreakerThresholds(t *testing.T) {
	clusterIndexes := []int{0, 4}
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

	for _, s := range settings {
		testName := "default"
		if s != nil {
			testName = "override"
		}
		t.Run(testName, func(t *testing.T) {
			g := NewGomegaWithT(t)
			clusters, err := buildTestClusters("*.example.org", 0, model.SidecarProxy, nil, testMesh,
				&networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						ConnectionPool: s,
					},
				})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(clusters)).To(Equal(8))
			for _, index := range clusterIndexes {
				cluster := clusters[index]
				g.Expect(len(cluster.CircuitBreakers.Thresholds)).To(Equal(1))
				thresholds := cluster.CircuitBreakers.Thresholds[0]

				if s == nil {
					// Assume the correct defaults for this direction.
					g.Expect(thresholds).To(Equal(getDefaultCircuitBreakerThresholds()))
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
			}
		})
	}
}

func TestCommonHttpProtocolOptions(t *testing.T) {
	g := NewGomegaWithT(t)

	cases := []struct {
		direction                  model.TrafficDirection
		clusterIndex               int
		useDownStreamProtocol      bool
		sniffingEnabledForInbound  bool
		sniffingEnabledForOutbound bool
		proxyType                  model.NodeType
		clusters                   int
	}{
		{
			direction:                  model.TrafficDirectionOutbound,
			clusterIndex:               0,
			useDownStreamProtocol:      false,
			sniffingEnabledForInbound:  false,
			sniffingEnabledForOutbound: true,
			proxyType:                  model.SidecarProxy,
			clusters:                   8,
		}, {
			direction:                  model.TrafficDirectionInbound,
			clusterIndex:               4,
			useDownStreamProtocol:      false,
			sniffingEnabledForInbound:  false,
			sniffingEnabledForOutbound: true,
			proxyType:                  model.SidecarProxy,
			clusters:                   8,
		}, {
			direction:                  model.TrafficDirectionOutbound,
			clusterIndex:               1,
			useDownStreamProtocol:      true,
			sniffingEnabledForInbound:  false,
			sniffingEnabledForOutbound: true,
			proxyType:                  model.SidecarProxy,
			clusters:                   8,
		},
		{
			direction:                  model.TrafficDirectionInbound,
			clusterIndex:               5,
			useDownStreamProtocol:      true,
			sniffingEnabledForInbound:  true,
			sniffingEnabledForOutbound: true,
			proxyType:                  model.SidecarProxy,
			clusters:                   8,
		},
		{
			direction:                  model.TrafficDirectionInbound,
			clusterIndex:               0,
			useDownStreamProtocol:      true,
			sniffingEnabledForInbound:  true,
			sniffingEnabledForOutbound: true,
			proxyType:                  model.Router,
			clusters:                   3,
		},
	}
	settings := &networking.ConnectionPoolSettings{
		Http: &networking.ConnectionPoolSettings_HTTPSettings{
			Http1MaxPendingRequests: 1,
			IdleTimeout:             &types.Duration{Seconds: 15},
		},
	}

	for _, tc := range cases {
		defaultValue := features.EnableProtocolSniffingForInbound
		features.EnableProtocolSniffingForInbound = tc.sniffingEnabledForInbound
		defer func() { features.EnableProtocolSniffingForInbound = defaultValue }()

		settingsName := "default"
		if settings != nil {
			settingsName = "override"
		}
		testName := fmt.Sprintf("%s-%s", tc.direction, settingsName)
		t.Run(testName, func(t *testing.T) {
			clusters, err := buildTestClusters("*.example.org", 0, tc.proxyType, nil, testMesh,
				&networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						ConnectionPool: settings,
					},
				})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(clusters)).To(Equal(tc.clusters))
			cluster := clusters[tc.clusterIndex]
			g.Expect(cluster.CommonHttpProtocolOptions).To(Not(BeNil()))
			commonHTTPProtocolOptions := cluster.CommonHttpProtocolOptions

			if tc.useDownStreamProtocol && tc.proxyType == model.SidecarProxy {
				g.Expect(cluster.ProtocolSelection).To(Equal(apiv2.Cluster_USE_DOWNSTREAM_PROTOCOL))
			} else {
				g.Expect(cluster.ProtocolSelection).To(Equal(apiv2.Cluster_USE_CONFIGURED_PROTOCOL))
			}

			// Verify that the values were set correctly.
			g.Expect(commonHTTPProtocolOptions.IdleTimeout).To(Not(BeNil()))
			g.Expect(commonHTTPProtocolOptions.IdleTimeout).To(Equal(ptypes.DurationProto(time.Duration(15000000000))))
		})
	}
}

func buildTestClusters(serviceHostname string, serviceResolution model.Resolution,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithAuthnPolicy(
		serviceHostname,
		serviceResolution,
		false, // externalService
		nodeType,
		locality,
		mesh,
		destRule,
		nil, // peerAuthn
	)
}

func buildTestClustersWithAuthnPolicy(serviceHostname string, serviceResolution model.Resolution, externalService bool,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, peerAuthn *authn_beta.PeerAuthentication) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadata(
		serviceHostname,
		serviceResolution,
		externalService,
		nodeType,
		locality,
		mesh,
		destRule,
		peerAuthn,
		&model.NodeMetadata{},
		model.MaxIstioVersion)
}

func buildTestClustersWithProxyMetadata(serviceHostname string, serviceResolution model.Resolution, externalService bool,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, peerAuthn *authn_beta.PeerAuthentication,
	meta *model.NodeMetadata, istioVersion *model.IstioVersion) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadataWithIps(serviceHostname, serviceResolution, externalService,
		nodeType, locality, mesh,
		destRule, peerAuthn, meta, istioVersion,
		// Add default sidecar proxy meta
		[]string{"6.6.6.6", "::1"})
}

func buildTestClustersWithProxyMetadataWithIps(serviceHostname string, serviceResolution model.Resolution, externalService bool,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, peerAuthn *authn_beta.PeerAuthentication,
	meta *model.NodeMetadata, istioVersion *model.IstioVersion, proxyIps []string) ([]*apiv2.Cluster, error) {
	configgen := NewConfigGenerator([]plugin.Plugin{})

	serviceDiscovery := &fakes.ServiceDiscovery{}

	servicePort := model.PortList{
		&model.Port{
			Name:     "default",
			Port:     8080,
			Protocol: protocol.HTTP,
		},
		&model.Port{
			Name:     "auto",
			Port:     9090,
			Protocol: protocol.Unsupported,
		},
	}

	serviceAttribute := model.ServiceAttributes{
		Namespace: TestServiceNamespace,
	}
	service := &model.Service{
		Hostname:     host.Name(serviceHostname),
		Address:      "1.1.1.1",
		ClusterVIPs:  make(map[string]string),
		Ports:        servicePort,
		Resolution:   serviceResolution,
		MeshExternal: externalService,
		Attributes:   serviceAttribute,
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort[0],
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.1",
				EndpointPort: 10001,
				Locality: model.Locality{
					ClusterID: "",
					Label:     "region1/zone1/subzone1",
				},
				LbWeight: 40,
				TLSMode:  model.IstioMutualTLSModeLabel,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort[0],
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.2",
				EndpointPort: 10001,
				Locality: model.Locality{
					ClusterID: "",
					Label:     "region1/zone1/subzone2",
				},
				LbWeight: 20,
				TLSMode:  model.IstioMutualTLSModeLabel,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort[0],
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.3",
				EndpointPort: 10001,
				Locality: model.Locality{
					ClusterID: "",
					Label:     "region2/zone1/subzone1",
				},
				LbWeight: 40,
				TLSMode:  model.IstioMutualTLSModeLabel,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort[1],
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.1",
				EndpointPort: 10001,
				Locality: model.Locality{
					ClusterID: "",
					Label:     "region1/zone1/subzone1",
				},
				LbWeight: 0,
				TLSMode:  model.IstioMutualTLSModeLabel,
			},
		},
	}

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)
	serviceDiscovery.GetProxyServiceInstancesReturns(instances, nil)
	serviceDiscovery.InstancesByPortReturns(instances, nil)

	configStore := &fakes.IstioConfigStore{
		ListStub: func(typ resource.GroupVersionKind, namespace string) (configs []model.Config, e error) {
			if typ == collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind() {
				return []model.Config{
					{ConfigMeta: model.ConfigMeta{
						Type:    collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Kind(),
						Version: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Version(),
						Name:    "acme",
					},
						Spec: destRule,
					}}, nil
			}
			if typ == collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind() && peerAuthn != nil {
				policyName := "default"
				if peerAuthn.Selector != nil {
					policyName = "acme"
				}
				return []model.Config{
					{ConfigMeta: model.ConfigMeta{
						Type:      collections.IstioSecurityV1Beta1Peerauthentications.Resource().Kind(),
						Version:   collections.IstioSecurityV1Beta1Peerauthentications.Resource().Version(),
						Name:      policyName,
						Namespace: TestServiceNamespace,
					},
						Spec: peerAuthn,
					}}, nil

			}
			return nil, nil
		},
	}
	env := newTestEnvironment(serviceDiscovery, mesh, configStore)

	var proxy *model.Proxy
	switch nodeType {
	case model.SidecarProxy:
		proxy = &model.Proxy{
			ClusterID:    "some-cluster-id",
			Type:         model.SidecarProxy,
			IPAddresses:  proxyIps,
			Locality:     locality,
			DNSDomain:    "com",
			Metadata:     meta,
			IstioVersion: istioVersion,
		}
	case model.Router:
		proxy = &model.Proxy{
			ClusterID:    "some-cluster-id",
			Type:         model.Router,
			IPAddresses:  []string{"6.6.6.6"},
			Locality:     locality,
			DNSDomain:    "default.example.org",
			Metadata:     meta,
			IstioVersion: istioVersion,
		}
	default:
		panic(fmt.Sprintf("unsupported node type: %v", nodeType))
	}
	proxy.SetSidecarScope(env.PushContext)

	proxy.ServiceInstances, _ = serviceDiscovery.GetProxyServiceInstances(proxy)
	proxy.DiscoverIPVersions()

	clusters := configgen.BuildClusters(proxy, env.PushContext)

	for _, cluster := range clusters {
		// Validate Clusters so that generated clusters pass Envoy validation logic.
		if err := cluster.Validate(); err != nil {
			return nil, fmt.Errorf("cluster %s failed validation with error %s", cluster.Name, err.Error())
		}
	}
	if len(env.PushContext.ProxyStatus[model.DuplicatedClusters.Name()]) > 0 {
		return nil, fmt.Errorf("duplicate clusters detected %#v", env.PushContext.ProxyStatus[model.DuplicatedClusters.Name()])
	}
	return clusters, nil
}

func TestBuildGatewayClustersWithRingHashLb(t *testing.T) {
	g := NewGomegaWithT(t)

	ttl := types.Duration{Nanos: 100}
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
	g.Expect(cluster.ConnectTimeout).To(Equal(ptypes.DurationProto(time.Duration(10000000001))))
}

func TestBuildGatewayClustersWithRingHashLbDefaultMinRingSize(t *testing.T) {
	g := NewGomegaWithT(t)

	ttl := types.Duration{Nanos: 100}
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
	g.Expect(cluster.ConnectTimeout).To(Equal(ptypes.DurationProto(time.Duration(10000000001))))
}

func newTestEnvironment(serviceDiscovery model.ServiceDiscovery, meshConfig meshconfig.MeshConfig, configStore model.IstioConfigStore) *model.Environment {
	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Watcher:          mesh.NewFixedWatcher(&meshConfig),
	}

	env.PushContext = model.NewPushContext()
	_ = env.PushContext.InitContext(env, nil, nil)

	return env
}

func withClusterLocalHosts(m meshconfig.MeshConfig, hosts ...string) meshconfig.MeshConfig { // nolint:interfacer
	m.ServiceSettings = append(append(make([]*meshconfig.MeshConfig_ServiceSettings, 0), m.ServiceSettings...),
		&meshconfig.MeshConfig_ServiceSettings{
			Settings: &meshconfig.MeshConfig_ServiceSettings_Settings{
				ClusterLocal: true,
			},
			Hosts: hosts,
		})
	return m
}

func TestBuildSidecarClustersWithIstioMutualAndSNI(t *testing.T) {
	g := NewGomegaWithT(t)

	clusters, err := buildSniTestClustersForSidecar("foo.com")
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(len(clusters)).To(Equal(10))

	cluster := clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	g.Expect(getTLSContext(t, cluster).GetSni()).To(Equal("foo.com"))

	clusters, err = buildSniTestClustersForSidecar("")
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(len(clusters)).To(Equal(10))

	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	g.Expect(getTLSContext(t, cluster).GetSni()).To(Equal("outbound_.8080_.foobar_.foo.example.org"))
}

func TestBuildClustersWithMutualTlsAndNodeMetadataCertfileOverrides(t *testing.T) {
	expectedClientKeyPath := "/clientKeyFromNodeMetadata.pem"
	expectedClientCertPath := "/clientCertFromNodeMetadata.pem"
	expectedRootCertPath := "/clientRootCertFromNodeMetadata.pem"

	g := NewGomegaWithT(t)

	envoyMetadata := &model.NodeMetadata{
		TLSClientCertChain: expectedClientCertPath,
		TLSClientKey:       expectedClientKeyPath,
		TLSClientRootCert:  expectedRootCertPath,
	}

	destRule := &networking.DestinationRule{
		Host: "*.example.org",
		TrafficPolicy: &networking.TrafficPolicy{
			Tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "/defaultCert.pem",
				PrivateKey:        "/defaultPrivateKey.pem",
				CaCertificates:    "/defaultCaCert.pem",
			},
		},
		Subsets: []*networking.Subset{
			{
				Name:   "foobar",
				Labels: map[string]string{"foo": "bar"},
				TrafficPolicy: &networking.TrafficPolicy{
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
						{
							Port: &networking.PortSelector{
								Number: 8080,
							},
						},
					},
				},
			},
		},
	}

	clusters, err := buildTestClustersWithProxyMetadata("foo.example.org", model.ClientSideLB, false, model.SidecarProxy,
		nil, testMesh, destRule, nil, envoyMetadata, model.MaxIstioVersion)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(clusters).To(HaveLen(10))

	expectedOutboundClusterCount := 4
	actualOutboundClusterCount := 0

	for _, c := range clusters {
		if strings.Contains(c.Name, "outbound") {
			actualOutboundClusterCount++
			tlsContext := getTLSContext(t, c)
			g.Expect(tlsContext).NotTo(BeNil())

			tlsCerts := tlsContext.CommonTlsContext.TlsCertificates
			g.Expect(tlsCerts).To(HaveLen(1))

			g.Expect(tlsCerts[0].PrivateKey.GetFilename()).To(Equal(expectedClientKeyPath))
			g.Expect(tlsCerts[0].CertificateChain.GetFilename()).To(Equal(expectedClientCertPath))
			g.Expect(tlsContext.CommonTlsContext.GetValidationContext().TrustedCa.GetFilename()).To(Equal(expectedRootCertPath))
		}
	}
	g.Expect(actualOutboundClusterCount).To(Equal(expectedOutboundClusterCount))
}

func buildSniTestClustersForSidecar(sniValue string) ([]*apiv2.Cluster, error) {
	return buildSniTestClustersWithMetadata(sniValue, model.SidecarProxy, &model.NodeMetadata{})
}

func buildSniDnatTestClustersForGateway(sniValue string) ([]*apiv2.Cluster, error) {
	return buildSniTestClustersWithMetadata(sniValue, model.Router, &model.NodeMetadata{RouterMode: string(model.SniDnatRouter)})
}

func buildSniTestClustersWithMetadata(sniValue string, typ model.NodeType, meta *model.NodeMetadata) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadata("foo.example.org", 0, false, typ, nil, testMesh,
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
									Number: 8080,
								},
								Tls: &networking.ClientTLSSettings{
									Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
									Sni:  sniValue,
								},
							},
						},
					},
				},
			},
		},
		nil, // peerAuthn
		meta,
		model.MaxIstioVersion,
	)
}

func TestBuildSidecarClustersWithMeshWideTCPKeepalive(t *testing.T) {
	g := NewGomegaWithT(t)

	// Do not set tcp_keepalive anywhere
	clusters, err := buildTestClustersWithTCPKeepalive(None)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(10))
	cluster := clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// UpstreamConnectionOptions should be nil. TcpKeepalive is the only field in it currently.
	g.Expect(cluster.UpstreamConnectionOptions).To(BeNil())

	// Set mesh wide default for tcp_keepalive.
	clusters, err = buildTestClustersWithTCPKeepalive(Mesh)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(10))
	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// KeepaliveTime should be set but rest should be nil.
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes).To(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime.Value).To(Equal(uint32(MeshWideTCPKeepaliveSeconds)))
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval).To(BeNil())

	// Set DestinationRule override for tcp_keepalive.
	clusters, err = buildTestClustersWithTCPKeepalive(DestinationRule)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(10))
	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// KeepaliveTime should be set but rest should be nil.
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes).To(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime.Value).To(Equal(uint32(DestinationRuleTCPKeepaliveSeconds)))
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval).To(BeNil())

	// Set DestinationRule override for tcp_keepalive with empty value.
	clusters, err = buildTestClustersWithTCPKeepalive(DestinationRuleForOsDefault)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(clusters)).To(Equal(10))
	cluster = clusters[1]
	g.Expect(cluster.Name).To(Equal("outbound|8080|foobar|foo.example.org"))
	// TcpKeepalive should be present but with nil values.
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive).NotTo(BeNil())
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes).To(BeNil())
	// Time should inherit from Mesh config.
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime.Value).To(Equal(uint32(MeshWideTCPKeepaliveSeconds)))
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval).To(BeNil())
}

func buildTestClustersWithTCPKeepalive(configType ConfigType) ([]*apiv2.Cluster, error) {
	// Set mesh wide defaults.
	m := testMesh
	if configType != None {
		m.TcpKeepalive = &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
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

	return buildTestClusters("foo.example.org", 0, model.SidecarProxy, nil, m,
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
									Number: 8080,
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

	foundSubset := false
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
			if strings.Contains(cluster.Name, "Subset") {
				foundSubset = true
				sub := istio.Fields["subset"]
				g.Expect(sub.GetStringValue()).To(HavePrefix("Subset "))
			} else {
				_, ok := istio.Fields["subset"]
				g.Expect(ok).To(Equal(false))
			}
		} else {
			g.Expect(cluster.Metadata).To(BeNil())
		}
	}

	g.Expect(foundSubset).To(Equal(true))
	g.Expect(clustersWithMetadata).To(Equal(len(destRule.Subsets) + 6)) // outbound  outbound subsets  inbound

	sniClusters, err := buildSniDnatTestClustersForGateway("test-sni")
	g.Expect(err).NotTo(HaveOccurred())

	foundSNISubset := false
	for _, cluster := range sniClusters {
		if strings.HasPrefix(cluster.Name, "outbound") {
			g.Expect(cluster.Metadata).NotTo(BeNil())
			md := cluster.Metadata
			g.Expect(md.FilterMetadata["istio"]).NotTo(BeNil())
			istio := md.FilterMetadata["istio"]
			g.Expect(istio.Fields["config"]).NotTo(BeNil())
			dr := istio.Fields["config"]
			g.Expect(dr.GetStringValue()).To(Equal("/apis//v1alpha3/namespaces//destination-rule/acme"))
			if strings.Contains(cluster.Name, "foobar") {
				foundSNISubset = true
				sub := istio.Fields["subset"]
				g.Expect(sub.GetStringValue()).To(Equal("foobar"))
			} else {
				_, ok := istio.Fields["subset"]
				g.Expect(ok).To(Equal(false))
			}
		} else {
			g.Expect(cluster.Metadata).To(BeNil())
		}
	}

	g.Expect(foundSNISubset).To(Equal(true))
}

func TestConditionallyConvertToIstioMtls(t *testing.T) {
	tlsSettings := &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	tests := []struct {
		name                 string
		tls                  *networking.ClientTLSSettings
		sans                 []string
		sni                  string
		proxy                *model.Proxy
		autoMTLSEnabled      bool
		meshExternal         bool
		serviceMTLSMode      model.MutualTLSMode
		clusterDiscoveryType apiv2.Cluster_DiscoveryType
		want                 *networking.ClientTLSSettings
		wantCtxType          mtlsContextType
	}{
		{
			"Destination rule TLS sni and SAN override",
			tlsSettings,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			false, false, model.MTLSUnknown, apiv2.Cluster_EDS,
			tlsSettings,
			userSupplied,
		},
		{
			"Destination rule TLS sni and SAN override absent",
			&networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				CaCertificates:    constants.DefaultRootCert,
				ClientCertificate: constants.DefaultCertChain,
				PrivateKey:        constants.DefaultKey,
				SubjectAltNames:   []string{},
				Sni:               "",
			},
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			false, false, model.MTLSUnknown, apiv2.Cluster_EDS,
			&networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				CaCertificates:    constants.DefaultRootCert,
				ClientCertificate: constants.DefaultCertChain,
				PrivateKey:        constants.DefaultKey,
				SubjectAltNames:   []string{"spiffe://foo/serviceaccount/1"},
				Sni:               "foo.com",
			},
			userSupplied,
		},
		{
			"Cert path override",
			tlsSettings,
			[]string{},
			"",
			&model.Proxy{Metadata: &model.NodeMetadata{
				TLSClientCertChain: "/custom/chain.pem",
				TLSClientKey:       "/custom/key.pem",
				TLSClientRootCert:  "/custom/root.pem",
			}},
			false, false, model.MTLSUnknown, apiv2.Cluster_EDS,
			&networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				CaCertificates:    "/custom/root.pem",
				ClientCertificate: "/custom/chain.pem",
				PrivateKey:        "/custom/key.pem",
				SubjectAltNames:   []string{"custom.foo.com"},
				Sni:               "custom.foo.com",
			},
			userSupplied,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in strict mode",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSStrict, apiv2.Cluster_EDS,
			&networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				CaCertificates:    constants.DefaultRootCert,
				ClientCertificate: constants.DefaultCertChain,
				PrivateKey:        constants.DefaultKey,
				SubjectAltNames:   []string{"spiffe://foo/serviceaccount/1"},
				Sni:               "foo.com",
			},
			autoDetected,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in permissive mode",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSPermissive, apiv2.Cluster_EDS,
			&networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				CaCertificates:    constants.DefaultRootCert,
				ClientCertificate: constants.DefaultCertChain,
				PrivateKey:        constants.DefaultKey,
				SubjectAltNames:   []string{"spiffe://foo/serviceaccount/1"},
				Sni:               "foo.com",
			},
			autoDetected,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in plaintext mode",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSDisable, apiv2.Cluster_EDS,
			nil,
			userSupplied,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in unknown mode",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSUnknown, apiv2.Cluster_EDS,
			nil,
			userSupplied,
		},
		{
			"Do not auto fill nil settings for external",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, true, model.MTLSUnknown, apiv2.Cluster_EDS,
			nil,
			userSupplied,
		},
		{
			"Do not auto fill nil settings if server mTLS is disabled",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			false, false, model.MTLSDisable, apiv2.Cluster_EDS,
			nil,
			userSupplied,
		},
		{
			"Do not enable auto mtls when cluster type is `Cluster_ORIGINAL_DST`",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSPermissive, apiv2.Cluster_ORIGINAL_DST,
			nil,
			userSupplied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTLS, gotCtxType := conditionallyConvertToIstioMtls(tt.tls, tt.sans, tt.sni, tt.proxy,
				tt.autoMTLSEnabled, tt.meshExternal, tt.serviceMTLSMode, tt.clusterDiscoveryType)
			if !reflect.DeepEqual(gotTLS, tt.want) {
				t.Errorf("cluster TLS does not match exppected result want %#v, got %#v", tt.want, gotTLS)
			}
			if gotCtxType != tt.wantCtxType {
				t.Errorf("cluster TLS context type does not match expected result want %#v, got %#v", tt.wantCtxType, gotTLS)
			}
		})
	}
}

func TestDisablePanicThresholdAsDefault(t *testing.T) {
	g := NewGomegaWithT(t)

	outliers := []*networking.OutlierDetection{
		// Unset MinHealthPercent
		{},
		// Explicitly set MinHealthPercent to 0
		{
			MinHealthPercent: 0,
		},
	}

	for _, outlier := range outliers {
		clusters, err := buildTestClusters("*.example.org", model.DNSLB, model.SidecarProxy,
			&core.Locality{}, testMesh,
			&networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: outlier,
				},
			})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(clusters[0].CommonLbConfig.HealthyPanicThreshold).To(Not(BeNil()))
		g.Expect(clusters[0].CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(0)))
	}
}

func TestApplyOutlierDetection(t *testing.T) {
	g := NewGomegaWithT(t)

	tests := []struct {
		name string
		cfg  *networking.OutlierDetection
		o    *apiv2_cluster.OutlierDetection
	}{
		{
			"Nil outlier detection",
			nil,
			nil,
		},
		{
			"No outlier detection is set",
			&networking.OutlierDetection{},
			&apiv2_cluster.OutlierDetection{
				EnforcingSuccessRate: &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Deprecated consecutive errors is set",
			&networking.OutlierDetection{
				ConsecutiveErrors: 3,
			},
			&apiv2_cluster.OutlierDetection{
				EnforcingConsecutive_5Xx:           &wrappers.UInt32Value{Value: 0},
				ConsecutiveGatewayFailure:          &wrappers.UInt32Value{Value: 3},
				EnforcingConsecutiveGatewayFailure: &wrappers.UInt32Value{Value: 100},
				EnforcingSuccessRate:               &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Consecutive gateway and 5xx errors are set",
			&networking.OutlierDetection{
				Consecutive_5XxErrors:    &types.UInt32Value{Value: 4},
				ConsecutiveGatewayErrors: &types.UInt32Value{Value: 3},
			},
			&apiv2_cluster.OutlierDetection{
				Consecutive_5Xx:                    &wrappers.UInt32Value{Value: 4},
				EnforcingConsecutive_5Xx:           &wrappers.UInt32Value{Value: 100},
				ConsecutiveGatewayFailure:          &wrappers.UInt32Value{Value: 3},
				EnforcingConsecutiveGatewayFailure: &wrappers.UInt32Value{Value: 100},
				EnforcingSuccessRate:               &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Only consecutive gateway is set",
			&networking.OutlierDetection{
				ConsecutiveGatewayErrors: &types.UInt32Value{Value: 3},
			},
			&apiv2_cluster.OutlierDetection{
				ConsecutiveGatewayFailure:          &wrappers.UInt32Value{Value: 3},
				EnforcingConsecutiveGatewayFailure: &wrappers.UInt32Value{Value: 100},
				EnforcingSuccessRate:               &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Only consecutive 5xx is set",
			&networking.OutlierDetection{
				Consecutive_5XxErrors: &types.UInt32Value{Value: 3},
			},
			&apiv2_cluster.OutlierDetection{
				Consecutive_5Xx:          &wrappers.UInt32Value{Value: 3},
				EnforcingConsecutive_5Xx: &wrappers.UInt32Value{Value: 100},
				EnforcingSuccessRate:     &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Consecutive gateway is set to 0",
			&networking.OutlierDetection{
				ConsecutiveGatewayErrors: &types.UInt32Value{Value: 0},
			},
			&apiv2_cluster.OutlierDetection{
				ConsecutiveGatewayFailure:          &wrappers.UInt32Value{Value: 0},
				EnforcingConsecutiveGatewayFailure: &wrappers.UInt32Value{Value: 0},
				EnforcingSuccessRate:               &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Consecutive 5xx is set to 0",
			&networking.OutlierDetection{
				Consecutive_5XxErrors: &types.UInt32Value{Value: 0},
			},
			&apiv2_cluster.OutlierDetection{
				Consecutive_5Xx:          &wrappers.UInt32Value{Value: 0},
				EnforcingConsecutive_5Xx: &wrappers.UInt32Value{Value: 0},
				EnforcingSuccessRate:     &wrappers.UInt32Value{Value: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clusters, err := buildTestClusters("*.example.org", model.DNSLB, model.SidecarProxy,
				&core.Locality{}, testMesh,
				&networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: tt.cfg,
					},
				})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(clusters[0].OutlierDetection).To(Equal(tt.o))
		})
	}
}

func TestStatNamePattern(t *testing.T) {
	g := NewGomegaWithT(t)

	statConfigMesh := meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		EnableAutoMtls: &types.BoolValue{
			Value: false,
		},
		InboundClusterStatName:  "LocalService_%SERVICE%",
		OutboundClusterStatName: "%SERVICE%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
	}

	clusters, err := buildTestClusters("*.example.org", model.DNSLB, model.SidecarProxy,
		&core.Locality{}, statConfigMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
		})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clusters[0].AltStatName).To(Equal("*.example.org_default_8080"))
	g.Expect(clusters[4].AltStatName).To(Equal("LocalService_*.example.org"))
}

func TestDuplicateClusters(t *testing.T) {
	g := NewGomegaWithT(t)

	clusters, err := buildTestClusters("*.example.org", model.DNSLB, model.SidecarProxy,
		&core.Locality{}, testMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
		})
	g.Expect(len(clusters)).To(Equal(8))
	g.Expect(err).NotTo(HaveOccurred())
}

func TestSidecarLocalityLB(t *testing.T) {
	g := NewGomegaWithT(t)
	// Distribute locality loadbalancing setting
	testMesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{
		Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
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
					MinHealthPercent:  10,
				},
			},
		})
	g.Expect(err).NotTo(HaveOccurred())

	if clusters[0].CommonLbConfig == nil {
		t.Fatalf("CommonLbConfig should be set for cluster %+v", clusters[0])
	}
	g.Expect(clusters[0].CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

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

	// Test failover
	// Distribute locality loadbalancing setting
	testMesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{}

	clusters, err = buildTestClusters("*.example.org", model.DNSLB, model.SidecarProxy,
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
					MinHealthPercent:  10,
				},
			},
		})
	g.Expect(err).NotTo(HaveOccurred())
	if clusters[0].CommonLbConfig == nil {
		t.Fatalf("CommonLbConfig should be set for cluster %+v", clusters[0])
	}
	g.Expect(clusters[0].CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

	g.Expect(len(clusters[0].LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range clusters[0].LoadAssignment.Endpoints {
		locality := localityLbEndpoint.Locality
		if locality.Region == "region1" && locality.Zone == "zone1" && locality.SubZone == "subzone1" {
			g.Expect(localityLbEndpoint.Priority).To(Equal(uint32(0)))
		} else if locality.Region == "region1" && locality.Zone == "zone1" && locality.SubZone == "subzone2" {
			g.Expect(localityLbEndpoint.Priority).To(Equal(uint32(1)))
		} else if locality.Region == "region2" {
			g.Expect(localityLbEndpoint.Priority).To(Equal(uint32(2)))
		}
	}
}

func TestLocalityLBDestinationRuleOverride(t *testing.T) {
	g := NewGomegaWithT(t)
	// Distribute locality loadbalancing setting
	testMesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{
		Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
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
					MinHealthPercent:  10,
				},
				LoadBalancer: &networking.LoadBalancerSettings{LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
					Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
						{
							From: "region1/zone1/subzone1",
							To: map[string]uint32{
								"region1/zone1/*":        60,
								"region2/zone1/subzone1": 40,
							},
						},
					},
				}},
			},
		})
	g.Expect(err).NotTo(HaveOccurred())

	if clusters[0].CommonLbConfig == nil {
		t.Fatalf("CommonLbConfig should be set for cluster %+v", clusters[0])
	}
	g.Expect(clusters[0].CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

	g.Expect(len(clusters[0].LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range clusters[0].LoadAssignment.Endpoints {
		locality := localityLbEndpoint.Locality
		if locality.Region == "region1" && locality.SubZone == "subzone1" {
			g.Expect(localityLbEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
			g.Expect(localityLbEndpoint.LbEndpoints[0].LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
		} else if locality.Region == "region1" && locality.SubZone == "subzone2" {
			g.Expect(localityLbEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(20)))
			g.Expect(localityLbEndpoint.LbEndpoints[0].LoadBalancingWeight.GetValue()).To(Equal(uint32(20)))
		} else if locality.Region == "region2" {
			g.Expect(localityLbEndpoint.LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
			g.Expect(len(localityLbEndpoint.LbEndpoints)).To(Equal(1))
			g.Expect(localityLbEndpoint.LbEndpoints[0].LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
		}
	}
}

func TestGatewayLocalityLB(t *testing.T) {
	g := NewGomegaWithT(t)
	// Distribute locality loadbalancing setting
	testMesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{
		Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
			{
				From: "region1/zone1/subzone1",
				To: map[string]uint32{
					"region1/zone1/*":        50,
					"region2/zone1/subzone1": 50,
				},
			},
		},
	}

	clusters, err := buildTestClustersWithProxyMetadata("*.example.org", model.DNSLB, false, model.Router,
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
					MinHealthPercent:  10,
				},
			},
		},
		nil, // peerAuthn,
		&model.NodeMetadata{RouterMode: string(model.SniDnatRouter)},
		model.MaxIstioVersion)

	g.Expect(err).NotTo(HaveOccurred())

	for _, cluster := range clusters {
		if cluster.Name == util.BlackHoleCluster {
			continue
		}
		if cluster.CommonLbConfig == nil {
			t.Errorf("CommonLbConfig should be set for cluster %+v", cluster)
		}
		g.Expect(cluster.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

		g.Expect(len(cluster.LoadAssignment.Endpoints)).To(Equal(3))
		for _, localityLbEndpoint := range cluster.LoadAssignment.Endpoints {
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

	// Test failover
	testMesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{}

	clusters, err = buildTestClustersWithProxyMetadata("*.example.org", model.DNSLB, false, model.Router,
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
					MinHealthPercent:  10,
				},
			},
		},
		nil, // peerAuthn
		&model.NodeMetadata{RouterMode: string(model.SniDnatRouter)},
		model.MaxIstioVersion)

	g.Expect(err).NotTo(HaveOccurred())

	for _, cluster := range clusters {
		if cluster.Name == util.BlackHoleCluster {
			continue
		}
		if cluster.CommonLbConfig == nil {
			t.Fatalf("CommonLbConfig should be set for cluster %+v", cluster)
		}
		g.Expect(cluster.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

		g.Expect(len(cluster.LoadAssignment.Endpoints)).To(Equal(3))
		for _, localityLbEndpoint := range cluster.LoadAssignment.Endpoints {
			locality := localityLbEndpoint.Locality
			if locality.Region == "region1" && locality.Zone == "zone1" && locality.SubZone == "subzone1" {
				g.Expect(localityLbEndpoint.Priority).To(Equal(uint32(0)))
			} else if locality.Region == "region1" && locality.Zone == "zone1" && locality.SubZone == "subzone2" {
				g.Expect(localityLbEndpoint.Priority).To(Equal(uint32(1)))
			} else if locality.Region == "region2" {
				g.Expect(localityLbEndpoint.Priority).To(Equal(uint32(2)))
			}
		}
	}
}

func TestBuildLocalityLbEndpoints(t *testing.T) {
	proxy := &model.Proxy{
		ClusterID: "cluster-1",
	}
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: protocol.HTTP,
	}
	service := &model.Service{
		Hostname:    host.Name("*.example.org"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Resolution:  model.DNSLB,
		Attributes: model.ServiceAttributes{
			Name:      "TestService",
			Namespace: "test-ns",
		},
	}

	emptyMetadata := &core.Metadata{
		FilterMetadata: make(map[string]*structpb.Struct),
	}

	cases := []struct {
		name      string
		newEnv    func(model.ServiceDiscovery, model.IstioConfigStore) *model.Environment
		instances []*model.ServiceInstance
		expected  []*envoyEndpoint.LocalityLbEndpoints
	}{
		{
			name: "basics",
			newEnv: func(sd model.ServiceDiscovery, cs model.IstioConfigStore) *model.Environment {
				return newTestEnvironment(sd, testMesh, cs)
			},
			instances: []*model.ServiceInstance{
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.1",
						EndpointPort: 10001,
						Locality: model.Locality{
							ClusterID: "cluster-1",
							Label:     "region1/zone1/subzone1",
						},
						LbWeight: 30,
					},
				},
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.2",
						EndpointPort: 10001,
						Locality: model.Locality{
							ClusterID: "cluster-2",
							Label:     "region1/zone1/subzone1",
						},
						LbWeight: 30,
					},
				},
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.3",
						EndpointPort: 10001,
						Locality: model.Locality{
							ClusterID: "cluster-3",
							Label:     "region2/zone1/subzone1",
						},
						LbWeight: 40,
					},
				},
			},
			expected: []*envoyEndpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 60,
					},
					LbEndpoints: []*envoyEndpoint.LbEndpoint{
						{
							HostIdentifier: &envoyEndpoint.LbEndpoint_Endpoint{
								Endpoint: &envoyEndpoint.Endpoint{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address: "192.168.1.1",
												PortSpecifier: &core.SocketAddress_PortValue{
													PortValue: 10001,
												},
											},
										},
									},
								},
							},
							Metadata: emptyMetadata,
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 30,
							},
						},
						{
							HostIdentifier: &envoyEndpoint.LbEndpoint_Endpoint{
								Endpoint: &envoyEndpoint.Endpoint{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address: "192.168.1.2",
												PortSpecifier: &core.SocketAddress_PortValue{
													PortValue: 10001,
												},
											},
										},
									},
								},
							},
							Metadata: emptyMetadata,
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 30,
							},
						},
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 40,
					},
					LbEndpoints: []*envoyEndpoint.LbEndpoint{
						{
							HostIdentifier: &envoyEndpoint.LbEndpoint_Endpoint{
								Endpoint: &envoyEndpoint.Endpoint{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address: "192.168.1.3",
												PortSpecifier: &core.SocketAddress_PortValue{
													PortValue: 10001,
												},
											},
										},
									},
								},
							},
							Metadata: emptyMetadata,
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 40,
							},
						},
					},
				},
			},
		},
		{
			name: "cluster local",
			newEnv: func(sd model.ServiceDiscovery, cs model.IstioConfigStore) *model.Environment {
				return newTestEnvironment(sd, withClusterLocalHosts(testMesh, "*.example.org"), cs)
			},
			instances: []*model.ServiceInstance{
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.1",
						EndpointPort: 10001,
						Locality: model.Locality{
							ClusterID: "cluster-1",
							Label:     "region1/zone1/subzone1",
						},
						LbWeight: 30,
					},
				},
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.2",
						EndpointPort: 10001,
						Locality: model.Locality{
							ClusterID: "cluster-2",
							Label:     "region1/zone1/subzone1",
						},
						LbWeight: 30,
					},
				},
			},
			expected: []*envoyEndpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 30,
					},
					LbEndpoints: []*envoyEndpoint.LbEndpoint{
						{
							HostIdentifier: &envoyEndpoint.LbEndpoint_Endpoint{
								Endpoint: &envoyEndpoint.Endpoint{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address: "192.168.1.1",
												PortSpecifier: &core.SocketAddress_PortValue{
													PortValue: 10001,
												},
											},
										},
									},
								},
							},
							Metadata: emptyMetadata,
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 30,
							},
						},
					},
				},
			},
		},
	}

	sortEndpoints := func(endpoints []*envoyEndpoint.LocalityLbEndpoints) {
		sort.SliceStable(endpoints, func(i, j int) bool {
			if strings.Compare(endpoints[i].Locality.Region, endpoints[j].Locality.Region) < 0 {
				return true
			}
			if strings.Compare(endpoints[i].Locality.Zone, endpoints[j].Locality.Zone) < 0 {
				return true
			}
			return strings.Compare(endpoints[i].Locality.SubZone, endpoints[j].Locality.SubZone) < 0
		})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			configStore := &fakes.IstioConfigStore{}
			serviceDiscovery := &fakes.ServiceDiscovery{}
			serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)
			serviceDiscovery.InstancesByPortReturns(c.instances, nil)

			env := c.newEnv(serviceDiscovery, configStore)
			actual := buildLocalityLbEndpoints(proxy, env.PushContext, model.GetNetworkView(nil), service, 8080, nil)
			sortEndpoints(actual)
			if v := cmp.Diff(c.expected, actual); v != "" {
				t.Fatalf("Expected (-) != actual (+):\n%s", v)
			}
		})
	}
}

func TestFindServiceInstanceForIngressListener(t *testing.T) {
	servicePort := &model.Port{
		Name:     "default",
		Port:     7443,
		Protocol: protocol.HTTP,
	}
	service := &model.Service{
		Hostname:    host.Name("*.example.org"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Resolution:  model.ClientSideLB,
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.1",
				EndpointPort: 7443,
				Locality: model.Locality{
					ClusterID: "",
					Label:     "region1/zone1/subzone1",
				},
				LbWeight: 30,
			},
		},
	}

	ingress := &networking.IstioIngressListener{
		CaptureMode:     networking.CaptureMode_NONE,
		DefaultEndpoint: "127.0.0.1:7020",
		Port: &networking.Port{
			Number:   7443,
			Name:     "grpc-core",
			Protocol: "GRPC",
		},
	}
	configgen := NewConfigGenerator([]plugin.Plugin{})
	instance := configgen.findOrCreateServiceInstance(instances, ingress, "sidecar", "sidecarns")
	if instance == nil || instance.Service.Hostname.Matches("sidecar.sidecarns") {
		t.Fatal("Expected to return a valid instance, but got nil/default instance")
	}
	if instance == instances[0] {
		t.Fatal("Expected to return a copy of instance, but got the same instance")
	}
	if !reflect.DeepEqual(instance, instances[0]) {
		t.Fatal("Expected returned copy of instance to be equal, but they are different")
	}
}

func TestClusterDiscoveryTypeAndLbPolicyRoundRobin(t *testing.T) {
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
	g.Expect(clusters[0].LbPolicy).To(Equal(apiv2.Cluster_CLUSTER_PROVIDED))
	g.Expect(clusters[0].GetClusterDiscoveryType()).To(Equal(&apiv2.Cluster_Type{Type: apiv2.Cluster_ORIGINAL_DST}))
}

func TestClusterDiscoveryTypeAndLbPolicyPassthrough(t *testing.T) {
	g := NewGomegaWithT(t)

	clusters, err := buildTestClusters("*.example.org", model.ClientSideLB, model.SidecarProxy, nil, testMesh,
		&networking.DestinationRule{
			Host: "*.example.org",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_PASSTHROUGH,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 5,
				},
			},
		})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clusters[0].LbPolicy).To(Equal(apiv2.Cluster_CLUSTER_PROVIDED))
	g.Expect(clusters[0].GetClusterDiscoveryType()).To(Equal(&apiv2.Cluster_Type{Type: apiv2.Cluster_ORIGINAL_DST}))
	g.Expect(clusters[0].EdsClusterConfig).To(BeNil())
}

func TestBuildClustersDefaultCircuitBreakerThresholds(t *testing.T) {
	g := NewGomegaWithT(t)

	configgen := NewConfigGenerator([]plugin.Plugin{})
	serviceDiscovery := &fakes.ServiceDiscovery{}
	configStore := &fakes.IstioConfigStore{}
	env := newTestEnvironment(serviceDiscovery, testMesh, configStore)
	proxy := &model.Proxy{Metadata: &model.NodeMetadata{}}

	clusters := configgen.BuildClusters(proxy, env.PushContext)
	g.Expect(len(clusters)).ShouldNot(Equal(0))

	for _, cluster := range clusters {
		if err := cluster.Validate(); err != nil {
			t.Fatalf("Cluster %s validation failed with error %s", cluster.Name, err.Error())
		}
		if cluster.Name != "BlackHoleCluster" {
			g.Expect(cluster.CircuitBreakers).NotTo(BeNil())
			g.Expect(cluster.CircuitBreakers.Thresholds[0]).To(Equal(getDefaultCircuitBreakerThresholds()))
		}
	}
}

func TestBuildInboundClustersDefaultCircuitBreakerThresholds(t *testing.T) {
	g := NewGomegaWithT(t)

	configgen := NewConfigGenerator([]plugin.Plugin{})
	serviceDiscovery := &fakes.ServiceDiscovery{}
	configStore := &fakes.IstioConfigStore{}
	env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

	proxy := &model.Proxy{
		Metadata:     &model.NodeMetadata{},
		SidecarScope: &model.SidecarScope{},
	}

	servicePort := &model.Port{
		Name:     "default",
		Port:     80,
		Protocol: protocol.HTTP,
	}

	service := &model.Service{
		Hostname:    host.Name("backend.default.svc.cluster.local"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Resolution:  model.Passthrough,
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.1",
				EndpointPort: 10001,
			},
		},
	}

	clusters := configgen.buildInboundClusters(proxy, env.PushContext, instances, []*model.Port{servicePort})
	g.Expect(len(clusters)).ShouldNot(Equal(0))

	for _, cluster := range clusters {
		g.Expect(cluster.CircuitBreakers).NotTo(BeNil())
		g.Expect(cluster.CircuitBreakers.Thresholds[0]).To(Equal(getDefaultCircuitBreakerThresholds()))
	}
}

func TestBuildInboundClustersPortLevelCircuitBreakerThresholds(t *testing.T) {
	g := NewGomegaWithT(t)

	proxy := &model.Proxy{
		Metadata:     &model.NodeMetadata{},
		SidecarScope: &model.SidecarScope{},
	}

	servicePort := &model.Port{
		Name:     "default",
		Port:     80,
		Protocol: protocol.HTTP,
	}

	service := &model.Service{
		Hostname:    host.Name("backend.default.svc.cluster.local"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Resolution:  model.Passthrough,
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.1",
				EndpointPort: 10001,
			},
		},
	}

	cases := []struct {
		name     string
		newEnv   func(model.ServiceDiscovery, model.IstioConfigStore) *model.Environment
		destRule *networking.DestinationRule
		expected apiv2_cluster.CircuitBreakers_Thresholds
	}{
		{
			name: "port-level policy matched",
			newEnv: func(sd model.ServiceDiscovery, cs model.IstioConfigStore) *model.Environment {
				return newTestEnvironment(sd, testMesh, cs)
			},
			destRule: &networking.DestinationRule{
				Host: "backend.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Tcp: &networking.ConnectionPoolSettings_TCPSettings{
							MaxConnections: 1000,
						},
					},
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
						{
							Port: &networking.PortSelector{
								Number: 80,
							},
							ConnectionPool: &networking.ConnectionPoolSettings{
								Tcp: &networking.ConnectionPoolSettings_TCPSettings{
									MaxConnections: 100,
								},
							},
						},
					},
				},
			},
			expected: apiv2_cluster.CircuitBreakers_Thresholds{
				MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxConnections:     &wrappers.UInt32Value{Value: 100},
				MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
			},
		},
		{
			name: "port-level policy not matched",
			newEnv: func(sd model.ServiceDiscovery, cs model.IstioConfigStore) *model.Environment {
				return newTestEnvironment(sd, testMesh, cs)
			},
			destRule: &networking.DestinationRule{
				Host: "backend.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Tcp: &networking.ConnectionPoolSettings_TCPSettings{
							MaxConnections: 1000,
						},
					},
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
						{
							Port: &networking.PortSelector{
								Number: 8080,
							},
							ConnectionPool: &networking.ConnectionPoolSettings{
								Tcp: &networking.ConnectionPoolSettings_TCPSettings{
									MaxConnections: 100,
								},
							},
						},
					},
				},
			},
			expected: apiv2_cluster.CircuitBreakers_Thresholds{
				MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxConnections:     &wrappers.UInt32Value{Value: 1000},
				MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			configgen := NewConfigGenerator([]plugin.Plugin{})
			serviceDiscovery := &fakes.ServiceDiscovery{}

			configStore := &fakes.IstioConfigStore{
				ListStub: func(typ resource.GroupVersionKind, namespace string) (configs []model.Config, e error) {
					if typ == collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind() {
						return []model.Config{
							{ConfigMeta: model.ConfigMeta{
								Type:    collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Kind(),
								Version: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Version(),
								Name:    "acme",
							},
								Spec: c.destRule,
							}}, nil
					}
					return nil, nil
				},
			}

			env := c.newEnv(serviceDiscovery, configStore)
			clusters := configgen.buildInboundClusters(proxy, env.PushContext, instances, []*model.Port{servicePort})
			g.Expect(len(clusters)).ShouldNot(Equal(0))

			for _, cluster := range clusters {
				g.Expect(cluster.CircuitBreakers).NotTo(BeNil())
				if cluster.Name == "inbound|80|default|backend.default.svc.cluster.local" {
					g.Expect(cluster.CircuitBreakers.Thresholds[0]).To(Equal(&c.expected))
				}
			}
		})
	}
}

func TestRedisProtocolWithPassThroughResolutionAtGateway(t *testing.T) {
	g := NewGomegaWithT(t)

	configgen := NewConfigGenerator([]plugin.Plugin{})

	configStore := &fakes.IstioConfigStore{}

	proxy := &model.Proxy{Type: model.Router, Metadata: &model.NodeMetadata{}}

	serviceDiscovery := &fakes.ServiceDiscovery{}

	servicePort := &model.Port{
		Name:     "redis-port",
		Port:     6379,
		Protocol: protocol.Redis,
	}
	service := &model.Service{
		Hostname:    host.Name("redis.com"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Resolution:  model.Passthrough,
	}

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)

	env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

	clusters := configgen.BuildClusters(proxy, env.PushContext)
	g.Expect(len(clusters)).ShouldNot(Equal(0))
	for _, cluster := range clusters {
		if cluster.Name == "outbound|6379||redis.com" {
			g.Expect(cluster.LbPolicy).To(Equal(apiv2.Cluster_ROUND_ROBIN))
		}
	}
}

func TestRedisProtocolClusterAtGateway(t *testing.T) {
	g := NewGomegaWithT(t)

	configgen := NewConfigGenerator([]plugin.Plugin{})

	configStore := &fakes.IstioConfigStore{}

	proxy := &model.Proxy{Type: model.Router, Metadata: &model.NodeMetadata{}}

	serviceDiscovery := &fakes.ServiceDiscovery{}

	servicePort := &model.Port{
		Name:     "redis-port",
		Port:     6379,
		Protocol: protocol.Redis,
	}
	service := &model.Service{
		Hostname:    host.Name("redis.com"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Resolution:  model.ClientSideLB,
	}

	// enable redis filter to true
	defaultValue := features.EnableRedisFilter
	features.EnableRedisFilter = true
	defer func() { features.EnableRedisFilter = defaultValue }()

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)

	env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

	clusters := configgen.BuildClusters(proxy, env.PushContext)
	g.Expect(len(clusters)).ShouldNot(Equal(0))
	for _, cluster := range clusters {
		// Validate Clusters so that generated clusters pass Envoy validation logic.
		if err := cluster.Validate(); err != nil {
			t.Fatalf("Cluster %s failed validation with error %s", cluster.Name, err.Error())
		}
		if cluster.Name == "outbound|6379||redis.com" {
			g.Expect(cluster.GetClusterDiscoveryType()).To(Equal(&apiv2.Cluster_Type{Type: apiv2.Cluster_EDS}))
			g.Expect(cluster.LbPolicy).To(Equal(apiv2.Cluster_MAGLEV))
		}
	}
}

func TestAutoMTLSClusterSubsets(t *testing.T) {
	g := NewGomegaWithT(t)

	destRule := &networking.DestinationRule{
		Host: TestServiceNHostname,
		Subsets: []*networking.Subset{
			{
				Name: "foobar",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxRequestsPerConnection: 1,
						},
					},
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
						{
							Port: &networking.PortSelector{
								Number: 8080,
							},
							Tls: &networking.ClientTLSSettings{
								Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
								Sni:  "custom.sni.com",
							},
						},
					},
				},
			},
		},
	}

	testMesh.EnableAutoMtls.Value = true

	clusters, err := buildTestClusters(TestServiceNHostname, 0, model.SidecarProxy, nil, testMesh, destRule)
	g.Expect(err).NotTo(HaveOccurred())

	tlsContext := getTLSContext(t, clusters[1])
	g.Expect(tlsContext).ToNot(BeNil())
	g.Expect(tlsContext.GetSni()).To(Equal("custom.sni.com"))
	g.Expect(clusters[1].TransportSocketMatches).To(HaveLen(0))

	for _, i := range []int{0, 2, 3} {
		g.Expect(getTLSContext(t, clusters[i])).To(BeNil())
		g.Expect(clusters[i].TransportSocketMatches).To(HaveLen(2))
	}

}

func TestAutoMTLSClusterIgnoreWorkloadLevelPeerAuthn(t *testing.T) {
	g := NewGomegaWithT(t)

	destRule := &networking.DestinationRule{
		Host: TestServiceNHostname,
		TrafficPolicy: &networking.TrafficPolicy{
			ConnectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					MaxRequestsPerConnection: 1,
				},
			},
			PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
				{
					Port: &networking.PortSelector{
						Number: 9090,
					},
					Tls: &networking.ClientTLSSettings{
						Mode: networking.ClientTLSSettings_DISABLE,
					},
				},
			},
		},
	}

	// This workload-level beta policy doesn't affect CDS (yet).
	peerAuthn := &authn_beta.PeerAuthentication{
		Selector: &selectorpb.WorkloadSelector{
			MatchLabels: map[string]string{
				"version": "v1",
			},
		},
		Mtls: &authn_beta.PeerAuthentication_MutualTLS{
			Mode: authn_beta.PeerAuthentication_MutualTLS_STRICT,
		},
	}

	testMesh.EnableAutoMtls.Value = true

	clusters, err := buildTestClustersWithAuthnPolicy(TestServiceNHostname, 0, false, model.SidecarProxy, nil, testMesh, destRule, peerAuthn)
	g.Expect(err).NotTo(HaveOccurred())

	// No policy visible, auto-mTLS should set to PERMISSIVE.
	// For port 8080, (m)TLS settings is automatically added, thus its cluster should have TLS context.
	// TlsContext is nil because we use socket match instead
	g.Expect(getTLSContext(t, clusters[0])).To(BeNil())
	g.Expect(clusters[0].TransportSocketMatches).To(HaveLen(2))

	// For 9090, use the TLS settings are explicitly specified in DR (which disable TLS)
	g.Expect(getTLSContext(t, clusters[1])).To(BeNil())

	// Sanity check: make sure TLS is not accidentally added to other clusters.
	for i := 2; i < len(clusters); i++ {
		cluster := clusters[i]
		g.Expect(getTLSContext(t, cluster)).To(BeNil())
	}
}

func TestApplyLoadBalancer(t *testing.T) {
	testcases := []struct {
		name             string
		lbSettings       *networking.LoadBalancerSettings
		discoveryType    apiv2.Cluster_DiscoveryType
		port             *model.Port
		expectedLbPolicy apiv2.Cluster_LbPolicy
	}{
		{
			name:             "lb = nil ORIGINAL_DST discovery type",
			discoveryType:    apiv2.Cluster_ORIGINAL_DST,
			expectedLbPolicy: apiv2.Cluster_CLUSTER_PROVIDED,
		},
		{
			name:             "lb = nil redis protocol",
			discoveryType:    apiv2.Cluster_EDS,
			port:             &model.Port{Protocol: protocol.Redis},
			expectedLbPolicy: apiv2.Cluster_MAGLEV,
		},
		// TODO: add more to cover all cases
	}

	proxy := model.Proxy{
		Type:         model.SidecarProxy,
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			cluster := &apiv2.Cluster{
				ClusterDiscoveryType: &apiv2.Cluster_Type{Type: test.discoveryType},
			}

			if test.port != nil && test.port.Protocol == protocol.Redis {
				defaultValue := features.EnableRedisFilter
				features.EnableRedisFilter = true
				defer func() { features.EnableRedisFilter = defaultValue }()
			}

			applyLoadBalancer(cluster, test.lbSettings, test.port, &proxy, &meshconfig.MeshConfig{})

			if cluster.LbPolicy != test.expectedLbPolicy {
				t.Errorf("cluster LbPolicy %s != expected %s", cluster.LbPolicy, test.expectedLbPolicy)
			}
		})
	}

}

func TestApplyUpstreamTLSSettings(t *testing.T) {
	tlsSettings := &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	mutualTLSSettings := &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_MUTUAL,
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}

	http2ProtocolOptions := &core.Http2ProtocolOptions{
		AllowConnect:  true,
		AllowMetadata: true,
	}

	tests := []struct {
		name          string
		mtlsCtx       mtlsContextType
		discoveryType apiv2.Cluster_DiscoveryType
		tls           *networking.ClientTLSSettings

		expectTransportSocket      bool
		expectTransportSocketMatch bool
		http2ProtocolOptions       *core.Http2ProtocolOptions

		validateTLSContext func(t *testing.T, ctx *envoy_api_v2_auth.UpstreamTlsContext)
	}{
		{
			name:                       "user specified without tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              apiv2.Cluster_EDS,
			tls:                        nil,
			expectTransportSocket:      false,
			expectTransportSocketMatch: false,
		},
		{
			name:                       "user specified with istio_mutual tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              apiv2.Cluster_EDS,
			tls:                        tlsSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *envoy_api_v2_auth.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
		{
			name:                       "user specified with istio_mutual tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              apiv2.Cluster_EDS,
			tls:                        tlsSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *envoy_api_v2_auth.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshH2WithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshH2WithMxc, got)
				}
			},
		},
		{
			name:                       "user specified mutual tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              apiv2.Cluster_EDS,
			tls:                        mutualTLSSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *envoy_api_v2_auth.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettings.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettings.ClientCertificate, mutualTLSSettings.PrivateKey)
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()[0].GetName(); certName != got {
					t.Fatalf("expected cert name %v got %v", certName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); got != nil {
					t.Fatalf("expected alpn list nil as not h2 or Istio_Mutual TLS Setting; got %v", got)
				}
			},
		},
		{
			name:                       "user specified mutual tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              apiv2.Cluster_EDS,
			tls:                        mutualTLSSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *envoy_api_v2_auth.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettings.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettings.ClientCertificate, mutualTLSSettings.PrivateKey)
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()[0].GetName(); certName != got {
					t.Fatalf("expected cert name %v got %v", certName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNH2Only) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNH2Only, got)
				}
			},
		},
		{
			name:                       "auto detect with tls",
			mtlsCtx:                    autoDetected,
			discoveryType:              apiv2.Cluster_EDS,
			tls:                        tlsSettings,
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
			validateTLSContext: func(t *testing.T, ctx *envoy_api_v2_auth.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
		{
			name:                       "auto detect with tls and h2 options",
			mtlsCtx:                    autoDetected,
			discoveryType:              apiv2.Cluster_EDS,
			tls:                        tlsSettings,
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *envoy_api_v2_auth.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshH2WithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshH2WithMxc, got)
				}
			},
		},
		{
			name:                       "auto detect with tls",
			mtlsCtx:                    autoDetected,
			discoveryType:              apiv2.Cluster_ORIGINAL_DST,
			tls:                        tlsSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
		},
	}

	proxy := &model.Proxy{
		Type:         model.SidecarProxy,
		Metadata:     &model.NodeMetadata{SdsEnabled: true},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}
	push := model.NewPushContext()
	push.Mesh = &meshconfig.MeshConfig{SdsUdsPath: "foo"}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := &buildClusterOpts{
				cluster: &apiv2.Cluster{
					ClusterDiscoveryType: &apiv2.Cluster_Type{Type: test.discoveryType},
					Http2ProtocolOptions: test.http2ProtocolOptions,
				},
				proxy: proxy,
				push:  push,
			}
			applyUpstreamTLSSettings(opts, test.tls, test.mtlsCtx, proxy)

			if test.expectTransportSocket && opts.cluster.TransportSocket == nil ||
				!test.expectTransportSocket && opts.cluster.TransportSocket != nil {
				t.Errorf("Expected TransportSocket %v", test.expectTransportSocket)
			}
			if test.expectTransportSocketMatch && opts.cluster.TransportSocketMatches == nil ||
				!test.expectTransportSocketMatch && opts.cluster.TransportSocketMatches != nil {
				t.Errorf("Expected TransportSocketMatch %v", test.expectTransportSocketMatch)
			}

			if test.validateTLSContext != nil {
				ctx := &envoy_api_v2_auth.UpstreamTlsContext{}
				if test.expectTransportSocket {
					if err := ptypes.UnmarshalAny(opts.cluster.TransportSocket.GetTypedConfig(), ctx); err != nil {
						t.Fatal(err)
					}
				} else if test.expectTransportSocketMatch {
					if err := ptypes.UnmarshalAny(opts.cluster.TransportSocketMatches[0].TransportSocket.GetTypedConfig(), ctx); err != nil {
						t.Fatal(err)
					}
				}
				test.validateTLSContext(t, ctx)
			}
		})
	}

}

// Helper function to extract TLS context from a cluster
func getTLSContext(t *testing.T, c *apiv2.Cluster) *envoy_api_v2_auth.UpstreamTlsContext {
	t.Helper()
	if c.TransportSocket == nil {
		return nil
	}
	tlsContext := &envoy_api_v2_auth.UpstreamTlsContext{}
	err := ptypes.UnmarshalAny(c.TransportSocket.GetTypedConfig(), tlsContext)

	if err != nil {
		t.Fatalf("Failed to unmarshall tls context: %v", err)
	}
	return tlsContext
}

func TestBuildStaticClusterWithNoEndPoint(t *testing.T) {
	g := NewGomegaWithT(t)

	cfg := NewConfigGenerator([]plugin.Plugin{})
	serviceDiscovery := &fakes.ServiceDiscovery{}
	service := &model.Service{
		Hostname:    host.Name("static.test"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports: []*model.Port{
			{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		Resolution:   model.DNSLB,
		MeshExternal: true,
		Attributes: model.ServiceAttributes{
			Namespace: TestServiceNamespace,
		},
	}

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)
	serviceDiscovery.GetProxyServiceInstancesReturns([]*model.ServiceInstance{}, nil)
	serviceDiscovery.InstancesByPortReturns([]*model.ServiceInstance{}, nil)
	proxy.ServiceInstances = []*model.ServiceInstance{}

	configStore := &fakes.IstioConfigStore{}
	proxy := &model.Proxy{
		ClusterID: "some-cluster-id",
		Type:      model.SidecarProxy,
		DNSDomain: "com",
		Metadata:  &model.NodeMetadata{},
	}
	env := newTestEnvironment(serviceDiscovery, testMesh, configStore)
	proxy.SetSidecarScope(env.PushContext)
	clusters := cfg.BuildClusters(proxy, env.PushContext)

	for _, cluster := range clusters {
		// Validate Clusters so that generated clusters pass Envoy validation logic.
		if err := cluster.Validate(); err != nil {
			t.Fatalf("Cluster %s failed validation with error %s", cluster.Name, err.Error())
		}
	}

	// Expect to ignore STRICT_DNS cluster without endpoints.
	g.Expect(len(clusters)).To(Equal(2))
}

func TestShouldH2Upgrade(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		direction      model.TrafficDirection
		port           model.Port
		mesh           meshconfig.MeshConfig
		connectionPool networking.ConnectionPoolSettings

		upgrade bool
	}{
		{
			name:        "mesh upgrade - dr default",
			clusterName: "bar",
			direction:   model.TrafficDirectionOutbound,
			port:        model.Port{Protocol: protocol.HTTP},
			mesh:        meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT}},
			upgrade: true,
		},
		{
			name:        "mesh no_upgrade - dr default",
			clusterName: "bar",
			direction:   model.TrafficDirectionOutbound,
			port:        model.Port{Protocol: protocol.HTTP},
			mesh:        meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_DO_NOT_UPGRADE},
			connectionPool: networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT}},
			upgrade: false,
		},
		{
			name:        "mesh no_upgrade - dr upgrade",
			clusterName: "bar",
			direction:   model.TrafficDirectionOutbound,
			port:        model.Port{Protocol: protocol.HTTP},
			mesh:        meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_DO_NOT_UPGRADE},
			connectionPool: networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_UPGRADE}},
			upgrade: true,
		},
		{
			name:        "mesh upgrade - dr no_upgrade",
			clusterName: "bar",
			direction:   model.TrafficDirectionOutbound,
			port:        model.Port{Protocol: protocol.HTTP},
			mesh:        meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DO_NOT_UPGRADE}},
			upgrade: false,
		},
		{
			name:        "inbound ignore",
			clusterName: "bar",
			direction:   model.TrafficDirectionInbound,
			port:        model.Port{Protocol: protocol.HTTP},
			mesh:        meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT}},
			upgrade: false,
		},
		{
			name:        "non-http",
			clusterName: "bar",
			direction:   model.TrafficDirectionOutbound,
			port:        model.Port{Protocol: protocol.Unsupported},
			mesh:        meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT}},
			upgrade: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upgrade := shouldH2Upgrade(test.clusterName, test.direction, &test.port, &test.mesh, &test.connectionPool)

			if upgrade != test.upgrade {
				t.Fatalf("got: %t, want: %t (%v, %v)", upgrade, test.upgrade, test.mesh.H2UpgradePolicy, test.connectionPool.Http.H2UpgradePolicy)
			}
		})
	}

}
