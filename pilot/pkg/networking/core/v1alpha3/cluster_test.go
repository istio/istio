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
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	envoy_api_v2_auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	apiv2_cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	networking "istio.io/api/networking/v1alpha3"
	authn_beta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
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
		if tc.sniffingEnabledForInbound {
			_ = os.Setenv(features.EnableProtocolSniffingForInbound.Name, "true")
		} else {
			_ = os.Setenv(features.EnableProtocolSniffingForInbound.Name, "false")
		}

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
		nil, // authnPolicy
		nil, // peerAuthn
	)
}

func buildTestClustersWithAuthnPolicy(serviceHostname string, serviceResolution model.Resolution, externalService bool,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, authnPolicy *authn.Policy, peerAuthn *authn_beta.PeerAuthentication) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadata(
		serviceHostname,
		serviceResolution,
		externalService,
		nodeType,
		locality,
		mesh,
		destRule,
		authnPolicy,
		peerAuthn,
		&model.NodeMetadata{},
		model.MaxIstioVersion)
}

func buildTestClustersWithIstioVersion(serviceHostname string, serviceResolution model.Resolution,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, authnPolicy *authn.Policy, peerAuthn *authn_beta.PeerAuthentication,
	istioVersion *model.IstioVersion) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadata(serviceHostname, serviceResolution, false /* externalService */, nodeType, locality, mesh, destRule,
		authnPolicy, peerAuthn, &model.NodeMetadata{}, istioVersion)
}

func buildTestClustersWithProxyMetadata(serviceHostname string, serviceResolution model.Resolution, externalService bool,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, authnPolicy *authn.Policy, peerAuthn *authn_beta.PeerAuthentication,
	meta *model.NodeMetadata, istioVersion *model.IstioVersion) ([]*apiv2.Cluster, error) {
	return buildTestClustersWithProxyMetadataWithIps(serviceHostname, serviceResolution, externalService,
		nodeType, locality, mesh,
		destRule, authnPolicy, peerAuthn, meta, istioVersion,
		// Add default sidecar proxy meta
		[]string{"6.6.6.6", "::1"})
}

func buildTestClustersWithProxyMetadataWithIps(serviceHostname string, serviceResolution model.Resolution, externalService bool,
	nodeType model.NodeType, locality *core.Locality, mesh meshconfig.MeshConfig,
	destRule proto.Message, authnPolicy *authn.Policy, peerAuthn *authn_beta.PeerAuthentication,
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
				Locality:     "region1/zone1/subzone1",
				LbWeight:     40,
				TLSMode:      model.IstioMutualTLSModeLabel,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort[0],
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.2",
				EndpointPort: 10001,
				Locality:     "region1/zone1/subzone2",
				LbWeight:     20,
				TLSMode:      model.IstioMutualTLSModeLabel,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort[0],
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.3",
				EndpointPort: 10001,
				Locality:     "region2/zone1/subzone1",
				LbWeight:     40,
				TLSMode:      model.IstioMutualTLSModeLabel,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort[1],
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.1",
				EndpointPort: 10001,
				Locality:     "region1/zone1/subzone1",
				LbWeight:     0,
				TLSMode:      model.IstioMutualTLSModeLabel,
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
			if typ == collections.IstioAuthenticationV1Alpha1Policies.Resource().GroupVersionKind() && authnPolicy != nil {
				// Set the policy name conforming to the authentication rule:
				// - namespace wide policy (i.e has not target selector) must be name "default"
				// - service-specific policy can be named anything but 'default'
				policyName := "default"
				if authnPolicy.Targets != nil {
					policyName = "acme"
				}
				return []model.Config{
					{ConfigMeta: model.ConfigMeta{
						Type:      collections.IstioAuthenticationV1Alpha1Policies.Resource().Kind(),
						Version:   collections.IstioAuthenticationV1Alpha1Policies.Resource().Version(),
						Name:      policyName,
						Namespace: TestServiceNamespace,
					},
						Spec: authnPolicy,
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

	clusters := configgen.BuildClusters(proxy, env.PushContext)
	var err error
	if len(env.PushContext.ProxyStatus[model.DuplicatedClusters.Name()]) > 0 {
		err = fmt.Errorf("duplicate clusters detected %#v", env.PushContext.ProxyStatus[model.DuplicatedClusters.Name()])
	}
	return clusters, err
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
			Tls: &networking.TLSSettings{
				Mode:              networking.TLSSettings_MUTUAL,
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
		nil, testMesh, destRule, nil, nil, envoyMetadata, model.MaxIstioVersion)
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
		nil, // authnPolicy
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
	g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime).To(BeNil())
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

	g.Expect(clustersWithMetadata).To(Equal(len(destRule.Subsets) + 6)) // outbound  outbound subsets  inbound

	sniClusters, err := buildSniDnatTestClustersForGateway("test-sni")
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
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	tests := []struct {
		name            string
		tls             *networking.TLSSettings
		sans            []string
		sni             string
		proxy           *model.Proxy
		autoMTLSEnabled bool
		meshExternal    bool
		serviceMTLSMode model.MutualTLSMode
		want            *networking.TLSSettings
		wantCtxType     mtlsContextType
	}{
		{
			"Destination rule TLS sni and SAN override",
			tlsSettings,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			false, false, model.MTLSUnknown,
			tlsSettings,
			userSupplied,
		},
		{
			"Destination rule TLS sni and SAN override absent",
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
				CaCertificates:    constants.DefaultRootCert,
				ClientCertificate: constants.DefaultCertChain,
				PrivateKey:        constants.DefaultKey,
				SubjectAltNames:   []string{},
				Sni:               "",
			},
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			false, false, model.MTLSUnknown,
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
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
			false, false, model.MTLSUnknown,
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
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
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSStrict,
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
				CaCertificates:    constants.DefaultRootCert,
				ClientCertificate: constants.DefaultCertChain,
				PrivateKey:        constants.DefaultKey,
				SubjectAltNames:   []string{"spiffee://foo/serviceaccount/1"},
				Sni:               "foo.com",
			},
			autoDetected,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in permissive mode",
			nil,
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSPermissive,
			&networking.TLSSettings{
				Mode:              networking.TLSSettings_ISTIO_MUTUAL,
				CaCertificates:    constants.DefaultRootCert,
				ClientCertificate: constants.DefaultCertChain,
				PrivateKey:        constants.DefaultKey,
				SubjectAltNames:   []string{"spiffee://foo/serviceaccount/1"},
				Sni:               "foo.com",
			},
			autoDetected,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in plaintext mode",
			nil,
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSDisable,
			nil,
			userSupplied,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in unknown mode",
			nil,
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSUnknown,
			nil,
			userSupplied,
		},
		{
			"Do not auto fill nil settings for external",
			nil,
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, true, model.MTLSUnknown,
			nil,
			userSupplied,
		},
		{
			"Do not auto fill nil settings if server mTLS is disabled",
			nil,
			[]string{"spiffee://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			false, false, model.MTLSDisable,
			nil,
			userSupplied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTLS, gotCtxType := conditionallyConvertToIstioMtls(tt.tls, tt.sans, tt.sni, tt.proxy, tt.autoMTLSEnabled, tt.meshExternal, tt.serviceMTLSMode)
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
			"No outlier detection is set",
			&networking.OutlierDetection{},
			&apiv2_cluster.OutlierDetection{},
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
		nil, // authnPolicy
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
		nil, // authnPolicy
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
	g := NewGomegaWithT(t)
	serviceDiscovery := &fakes.ServiceDiscovery{}

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
	}
	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.1",
				EndpointPort: 10001,
				Locality:     "region1/zone1/subzone1",
				LbWeight:     30,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.2",
				EndpointPort: 10001,
				Locality:     "region1/zone1/subzone1",
				LbWeight:     30,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Address:      "192.168.1.3",
				EndpointPort: 10001,
				Locality:     "region2/zone1/subzone1",
				LbWeight:     40,
			},
		},
	}

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)
	serviceDiscovery.InstancesByPortReturns(instances, nil)

	configStore := &fakes.IstioConfigStore{}
	env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

	localityLbEndpoints := buildLocalityLbEndpoints(env.PushContext, model.GetNetworkView(nil), service, 8080, nil)
	g.Expect(len(localityLbEndpoints)).To(Equal(2))
	for _, ep := range localityLbEndpoints {
		if ep.Locality.Region == "region1" {
			g.Expect(ep.LoadBalancingWeight.GetValue()).To(Equal(uint32(60)))
		} else if ep.Locality.Region == "region2" {
			g.Expect(ep.LoadBalancingWeight.GetValue()).To(Equal(uint32(40)))
		}
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
				Locality:     "region1/zone1/subzone1",
				LbWeight:     30,
			},
		},
	}

	ingress := &networking.IstioIngressListener{
		CaptureMode:     v1alpha3.CaptureMode_NONE,
		DefaultEndpoint: "127.0.0.1:7020",
		Port: &v1alpha3.Port{
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

func TestClusterDiscoveryTypeAndLbPolicyPassthroughIstioVersion12(t *testing.T) {
	g := NewGomegaWithT(t)

	clusters, err := buildTestClustersWithIstioVersion("*.example.org", model.ClientSideLB, model.SidecarProxy, nil, testMesh,
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
		},
		nil, // authnPolicy
		nil, // peerAuthn
		&model.IstioVersion{Major: 1, Minor: 2})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clusters[0].LbPolicy).To(Equal(apiv2.Cluster_ORIGINAL_DST_LB))
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
	_ = os.Setenv(features.EnableRedisFilter.Name, "true")

	defer func() { _ = os.Unsetenv(features.EnableRedisFilter.Name) }()

	serviceDiscovery.ServicesReturns([]*model.Service{service}, nil)

	env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

	clusters := configgen.BuildClusters(proxy, env.PushContext)
	g.Expect(len(clusters)).ShouldNot(Equal(0))
	for _, cluster := range clusters {
		if cluster.Name == "outbound|6379||redis.com" {
			g.Expect(cluster.GetClusterDiscoveryType()).To(Equal(&apiv2.Cluster_Type{Type: apiv2.Cluster_EDS}))
			g.Expect(cluster.LbPolicy).To(Equal(apiv2.Cluster_MAGLEV))
		}
	}
}

func TestAltStatName(t *testing.T) {
	tests := []struct {
		name        string
		statPattern string
		host        string
		subsetName  string
		port        *model.Port
		attributes  model.ServiceAttributes
		want        string
	}{
		{
			"Service only pattern",
			"%SERVICE%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default",
		},
		{
			"Service only pattern from different namespace",
			"%SERVICE%",
			"reviews.namespace1.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "namespace1",
			},
			"reviews.namespace1",
		},
		{
			"Service with port pattern from different namespace",
			"%SERVICE%.%SERVICE_PORT%",
			"reviews.namespace1.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "namespace1",
			},
			"reviews.namespace1.7443",
		},
		{
			"Service from non k8s registry",
			"%SERVICE%.%SERVICE_PORT%",
			"reviews.hostname.consul",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Consul),
				Name:            "foo",
				Namespace:       "bar",
			},
			"reviews.hostname.consul.7443",
		},
		{
			"Service FQDN only pattern",
			"%SERVICE_FQDN%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local",
		},
		{
			"Service With Port pattern",
			"%SERVICE%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_7443",
		},
		{
			"Service With Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_grpc-svc",
		},
		{
			"Service With Port and Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_grpc-svc_7443",
		},
		{
			"Service FQDN With Port pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_7443",
		},
		{
			"Service FQDN With Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc",
		},
		{
			"Service FQDN With Port and Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Empty Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%%SUBSET_NAME%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%.%SUBSET_NAME%.%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"v1",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local.v1.grpc-svc_7443",
		},
		{
			"Service FQDN With Unknown Pattern",
			"%SERVICE_FQDN%.%DUMMY%",
			"reviews.default.svc.cluster.local",
			"v1",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local.%DUMMY%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := altStatName(tt.statPattern, tt.host, tt.subsetName, tt.port, tt.attributes)
			if got != tt.want {
				t.Errorf("Expected alt statname %s, but got %s", tt.want, got)
			}
		})
	}
}

func TestPassthroughClustersBuildUponProxyIpVersions(t *testing.T) {

	validation := func(clusters []*apiv2.Cluster) []bool {
		hasIpv4, hasIpv6 := false, false
		for _, c := range clusters {
			hasIpv4 = hasIpv4 || c.Name == util.InboundPassthroughClusterIpv4
			hasIpv6 = hasIpv6 || c.Name == util.InboundPassthroughClusterIpv6
		}
		return []bool{hasIpv4, hasIpv6}
	}
	for _, inAndOut := range []struct {
		ips      []string
		features []bool
	}{
		{[]string{"6.6.6.6", "::1"}, []bool{true, true}},
		{[]string{"6.6.6.6"}, []bool{true, false}},
		{[]string{"::1"}, []bool{false, true}},
	} {
		clusters, err := buildTestClustersWithProxyMetadataWithIps("*.example.org", 0, false, model.SidecarProxy, nil, testMesh,
			&networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							Http1MaxPendingRequests: 1,
							IdleTimeout:             &types.Duration{Seconds: 15},
						},
					},
				},
			},
			nil, // authnPolicy
			nil, // peerAuthn
			&model.NodeMetadata{},
			model.MaxIstioVersion,
			inAndOut.ips,
		)
		g := NewGomegaWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(validation(clusters)).To(Equal(inAndOut.features))
	}
}

func TestAutoMTLSClusterPlaintextMode(t *testing.T) {
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
					Tls: &networking.TLSSettings{
						Mode: networking.TLSSettings_DISABLE,
					},
				},
			},
		},
	}

	authnPolicy := &authn.Policy{
		Peers: []*authn.PeerAuthenticationMethod{},
	}

	clusters, err := buildTestClustersWithAuthnPolicy(TestServiceNHostname, 0, false, model.SidecarProxy, nil, testMesh, destRule, authnPolicy, nil)
	g.Expect(err).NotTo(HaveOccurred())

	// mTLS is disabled by authN policy so autoMTLS does not kick in. No cluster should have TLS context.
	for _, cluster := range clusters {
		g.Expect(getTLSContext(t, cluster)).To(BeNil())
	}
}

func TestAutoMTLSClusterStrictMode(t *testing.T) {
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
					Tls: &networking.TLSSettings{
						Mode: networking.TLSSettings_DISABLE,
					},
				},
			},
		},
	}

	authnPolicy := &authn.Policy{
		Peers: []*authn.PeerAuthenticationMethod{
			{
				Params: &authn.PeerAuthenticationMethod_Mtls{
					Mtls: &authn.MutualTls{
						Mode: authn.MutualTls_STRICT,
					},
				},
			},
		},
	}

	testMesh.EnableAutoMtls.Value = true

	clusters, err := buildTestClustersWithAuthnPolicy(TestServiceNHostname, 0, false, model.SidecarProxy, nil, testMesh, destRule, authnPolicy, nil)
	g.Expect(err).NotTo(HaveOccurred())

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

func TestAutoMTLSClusterStrictMode_SkipForExternal(t *testing.T) {
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
					Tls: &networking.TLSSettings{
						Mode: networking.TLSSettings_DISABLE,
					},
				},
			},
		},
	}

	authnPolicy := &authn.Policy{
		Peers: []*authn.PeerAuthenticationMethod{
			{
				Params: &authn.PeerAuthenticationMethod_Mtls{
					Mtls: &authn.MutualTls{
						Mode: authn.MutualTls_STRICT,
					},
				},
			},
		},
	}

	clusters, err := buildTestClustersWithAuthnPolicy(
		TestServiceNHostname,
		0,
		true,
		model.SidecarProxy,
		nil,
		testMesh,
		destRule,
		authnPolicy,
		nil, // peerAuthn
	)
	g.Expect(err).NotTo(HaveOccurred())

	// Service is external, use the TLS settings specified in DR.
	for _, cluster := range clusters {
		g.Expect(getTLSContext(t, cluster)).To(BeNil())
	}
}

func TestAutoMTLSClusterPerPortStrictMode(t *testing.T) {
	g := NewGomegaWithT(t)

	destRule := &networking.DestinationRule{
		Host: TestServiceNHostname,
		TrafficPolicy: &networking.TrafficPolicy{
			ConnectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					MaxRequestsPerConnection: 1,
				},
			},
		},
	}

	authnPolicy := &authn.Policy{
		Targets: []*authn.TargetSelector{
			{
				Name: "foo",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 8080,
						},
					},
				},
			},
		},
		Peers: []*authn.PeerAuthenticationMethod{
			{
				Params: &authn.PeerAuthenticationMethod_Mtls{
					Mtls: &authn.MutualTls{
						Mode: authn.MutualTls_STRICT,
					},
				},
			},
		},
	}

	testMesh.EnableAutoMtls.Value = true

	clusters, err := buildTestClustersWithAuthnPolicy(
		TestServiceNHostname,
		0, false, model.SidecarProxy, nil, testMesh, destRule, authnPolicy,
		nil, // peerAuthn
	)
	g.Expect(err).NotTo(HaveOccurred())

	// For port 8080, (m)TLS settings is automatically added, thus its cluster should have TLS context.
	// TlsContext is nil because we use socket match instead
	g.Expect(getTLSContext(t, clusters[0])).To(BeNil())
	g.Expect(clusters[0].TransportSocketMatches).To(HaveLen(2))

	// For 9090, authn policy disable mTLS, so it should not have TLS context.
	g.Expect(getTLSContext(t, clusters[1])).To(BeNil())

	// Sanity check: make sure TLS is not accidentally added to other clusters.
	for i := 2; i < len(clusters); i++ {
		cluster := clusters[i]
		g.Expect(getTLSContext(t, cluster)).To(BeNil())
	}
}

func TestAutoMTLSClusterWithPeerAuthnStrictMode(t *testing.T) {
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
					Tls: &networking.TLSSettings{
						Mode: networking.TLSSettings_DISABLE,
					},
				},
			},
		},
	}

	// This alpha policy will be ignored.
	authnPolicy := &authn.Policy{
		Peers: []*authn.PeerAuthenticationMethod{
			{
				Params: &authn.PeerAuthenticationMethod_Mtls{
					Mtls: &authn.MutualTls{
						Mode: authn.MutualTls_PERMISSIVE,
					},
				},
			},
		},
	}

	peerAuthn := &authn_beta.PeerAuthentication{
		Mtls: &authn_beta.PeerAuthentication_MutualTLS{
			Mode: authn_beta.PeerAuthentication_MutualTLS_STRICT,
		},
	}

	testMesh.EnableAutoMtls.Value = true

	clusters, err := buildTestClustersWithAuthnPolicy(TestServiceNHostname, 0, false, model.SidecarProxy, nil, testMesh, destRule, authnPolicy, peerAuthn)
	g.Expect(err).NotTo(HaveOccurred())

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
					Tls: &networking.TLSSettings{
						Mode: networking.TLSSettings_DISABLE,
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

	clusters, err := buildTestClustersWithAuthnPolicy(TestServiceNHostname, 0, false, model.SidecarProxy, nil, testMesh, destRule, nil, peerAuthn)
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
				os.Setenv("PILOT_ENABLE_REDIS_FILTER", "true")
				defer os.Unsetenv("PILOT_ENABLE_REDIS_FILTER")
			}

			applyLoadBalancer(cluster, test.lbSettings, test.port, &proxy, &meshconfig.MeshConfig{})

			if cluster.LbPolicy != test.expectedLbPolicy {
				t.Errorf("cluster LbPolicy %s != expected %s", cluster.LbPolicy, test.expectedLbPolicy)
			}
		})
	}

}

func TestApplyUpstreamTLSSettings(t *testing.T) {
	tlsSettings := &networking.TLSSettings{
		Mode:              networking.TLSSettings_ISTIO_MUTUAL,
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}

	tests := []struct {
		name     string
		mtlsCtx  mtlsContextType
		LbPolicy apiv2.Cluster_LbPolicy
		tls      *networking.TLSSettings

		expectTransportSocket      bool
		expectTransportSocketMatch bool
	}{
		{
			name:                       "user specified without tls",
			mtlsCtx:                    userSupplied,
			LbPolicy:                   apiv2.Cluster_ROUND_ROBIN,
			tls:                        nil,
			expectTransportSocket:      false,
			expectTransportSocketMatch: false,
		},
		{
			name:                       "user specified with tls",
			mtlsCtx:                    userSupplied,
			LbPolicy:                   apiv2.Cluster_ROUND_ROBIN,
			tls:                        tlsSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
		},
		{
			name:                       "auto detect with tls",
			mtlsCtx:                    autoDetected,
			LbPolicy:                   apiv2.Cluster_ROUND_ROBIN,
			tls:                        tlsSettings,
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
		},
		{
			name:                       "auto detect with tls",
			mtlsCtx:                    autoDetected,
			LbPolicy:                   apiv2.Cluster_CLUSTER_PROVIDED,
			tls:                        tlsSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
		},
	}

	proxy := &model.Proxy{
		Type:         model.SidecarProxy,
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}
	push := model.NewPushContext()
	push.Mesh = &meshconfig.MeshConfig{}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := &buildClusterOpts{
				cluster: &apiv2.Cluster{
					LbPolicy: test.LbPolicy,
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

	// Expect to ignore STRICT_DNS cluster without endpoints.
	g.Expect(len(clusters)).To(Equal(2))
}
