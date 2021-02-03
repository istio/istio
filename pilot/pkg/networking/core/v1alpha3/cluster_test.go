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
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/testing/protocmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	authn_beta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
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

var testMesh = meshconfig.MeshConfig{
	ConnectTimeout: &types.Duration{
		Seconds: 10,
		Nanos:   1,
	},
	EnableAutoMtls: &types.BoolValue{
		Value: false,
	},
}

func TestHTTPCircuitBreakerThresholds(t *testing.T) {
	checkClusters := []string{"outbound|8080||*.example.org", "inbound|10001||"}
	settings := []*networking.ConnectionPoolSettings{
		nil,
		{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{
				Http1MaxPendingRequests:  1,
				Http2MaxRequests:         2,
				MaxRequestsPerConnection: 3,
				MaxRetries:               4,
			},
		},
	}

	for _, s := range settings {
		testName := "default"
		if s != nil {
			testName = "override"
		}
		t.Run(testName, func(t *testing.T) {
			g := NewWithT(t)
			clusters := xdstest.ExtractClusters(buildTestClusters(clusterTest{
				t:               t,
				serviceHostname: "*.example.org",
				nodeType:        model.SidecarProxy,
				mesh:            testMesh,
				destRule: &networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						ConnectionPool: s,
					},
				},
			}))

			for _, c := range checkClusters {
				cluster := clusters[c]
				if cluster == nil {
					t.Fatalf("cluster %v not found", c)
				}
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
	cases := []struct {
		clusterName               string
		useDownStreamProtocol     bool
		sniffingEnabledForInbound bool
		proxyType                 model.NodeType
		clusters                  int
	}{
		{
			clusterName:               "outbound|8080||*.example.org",
			useDownStreamProtocol:     false,
			sniffingEnabledForInbound: false,
			proxyType:                 model.SidecarProxy,
			clusters:                  8,
		},
		{
			clusterName:               "inbound|10001||",
			useDownStreamProtocol:     false,
			sniffingEnabledForInbound: false,
			proxyType:                 model.SidecarProxy,
			clusters:                  8,
		},
		{
			clusterName:               "outbound|9090||*.example.org",
			useDownStreamProtocol:     true,
			sniffingEnabledForInbound: false,
			proxyType:                 model.SidecarProxy,
			clusters:                  8,
		},
		{
			clusterName:               "inbound|10002||",
			useDownStreamProtocol:     true,
			sniffingEnabledForInbound: true,
			proxyType:                 model.SidecarProxy,
			clusters:                  8,
		},
		{
			clusterName:               "outbound|8080||*.example.org",
			useDownStreamProtocol:     true,
			sniffingEnabledForInbound: true,
			proxyType:                 model.Router,
			clusters:                  3,
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

		gwClusters := features.FilterGatewayClusterConfig
		features.FilterGatewayClusterConfig = false
		defer func() { features.FilterGatewayClusterConfig = gwClusters }()

		settingsName := "default"
		if settings != nil {
			settingsName = "override"
		}
		testName := fmt.Sprintf("%s-%s-%s", tc.clusterName, settingsName, tc.proxyType)
		// TODO(https://github.com/istio/istio/issues/29735) remove nolint
		// nolint: staticcheck
		t.Run(testName, func(t *testing.T) {
			g := NewWithT(t)
			clusters := xdstest.ExtractClusters(buildTestClusters(clusterTest{
				t: t, serviceHostname: "*.example.org", nodeType: tc.proxyType, mesh: testMesh,
				destRule: &networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						ConnectionPool: settings,
					},
				},
			}))
			g.Expect(len(clusters)).To(Equal(tc.clusters))
			c := clusters[tc.clusterName]

			g.Expect(c.GetCommonHttpProtocolOptions()).To(Not(BeNil()))
			commonHTTPProtocolOptions := c.CommonHttpProtocolOptions

			if tc.useDownStreamProtocol && tc.proxyType == model.SidecarProxy {
				g.Expect(c.ProtocolSelection).To(Equal(cluster.Cluster_USE_DOWNSTREAM_PROTOCOL))
			} else {
				g.Expect(c.ProtocolSelection).To(Equal(cluster.Cluster_USE_CONFIGURED_PROTOCOL))
			}

			// Verify that the values were set correctly.
			g.Expect(commonHTTPProtocolOptions.IdleTimeout).To(Not(BeNil()))
			g.Expect(commonHTTPProtocolOptions.IdleTimeout).To(Equal(ptypes.DurationProto(time.Duration(15000000000))))
		})
	}
}

// clusterTest defines a structure containing all information needed to build a cluster for tests
type clusterTest struct {
	// Required
	t                 testing.TB
	serviceHostname   string
	serviceResolution model.Resolution
	nodeType          model.NodeType
	locality          *core.Locality
	mesh              meshconfig.MeshConfig
	destRule          proto.Message
	peerAuthn         *authn_beta.PeerAuthentication
	externalService   bool

	meta         *model.NodeMetadata
	istioVersion *model.IstioVersion
	proxyIps     []string
}

func (c clusterTest) fillDefaults() clusterTest {
	if c.proxyIps == nil {
		c.proxyIps = []string{"6.6.6.6", "::1"}
	}
	if c.istioVersion == nil {
		c.istioVersion = model.MaxIstioVersion
	}
	if c.meta == nil {
		c.meta = &model.NodeMetadata{}
	}
	return c
}

func buildTestClusters(c clusterTest) []*cluster.Cluster {
	c = c.fillDefaults()

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
		Hostname:     host.Name(c.serviceHostname),
		Address:      "1.1.1.1",
		ClusterVIPs:  make(map[string]string),
		Ports:        servicePort,
		Resolution:   c.serviceResolution,
		MeshExternal: c.externalService,
		Attributes:   serviceAttribute,
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort[0],
			Endpoint: &model.IstioEndpoint{
				Address:      "6.6.6.6",
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
				Address:      "6.6.6.6",
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
				Address:      "6.6.6.6",
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
				Address:      "6.6.6.6",
				EndpointPort: 10002,
				Locality: model.Locality{
					ClusterID: "",
					Label:     "region1/zone1/subzone1",
				},
				LbWeight: 0,
				TLSMode:  model.IstioMutualTLSModeLabel,
			},
		},
	}

	configs := []config.Config{}
	if c.destRule != nil {
		configs = append(configs, config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
				Name:             "acme",
			},
			Spec: c.destRule,
		})
	}
	if c.peerAuthn != nil {
		policyName := "default"
		if c.peerAuthn.Selector != nil {
			policyName = "acme"
		}
		configs = append(configs, config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.PeerAuthentication,
				Name:             policyName,
				Namespace:        TestServiceNamespace,
			},
			Spec: c.peerAuthn,
		})
	}
	cg := NewConfigGenTest(c.t, TestOptions{
		Services:   []*model.Service{service},
		Instances:  instances,
		Configs:    configs,
		MeshConfig: &c.mesh,
	})

	var proxy *model.Proxy
	switch c.nodeType {
	case model.SidecarProxy:
		proxy = &model.Proxy{
			Type:         model.SidecarProxy,
			IPAddresses:  c.proxyIps,
			Locality:     c.locality,
			DNSDomain:    "com",
			Metadata:     c.meta,
			IstioVersion: c.istioVersion,
		}
	case model.Router:
		proxy = &model.Proxy{
			Type:         model.Router,
			IPAddresses:  []string{"6.6.6.6"},
			Locality:     c.locality,
			DNSDomain:    "default.example.org",
			Metadata:     c.meta,
			IstioVersion: c.istioVersion,
		}
	default:
		panic(fmt.Sprintf("unsupported node type: %v", c.nodeType))
	}
	clusters := cg.Clusters(cg.SetupProxy(proxy))
	xdstest.ValidateClusters(c.t, clusters)
	if len(cg.PushContext().ProxyStatus[model.DuplicatedClusters.Name()]) > 0 {
		c.t.Fatalf("duplicate clusters detected %#v", cg.PushContext().ProxyStatus[model.DuplicatedClusters.Name()])
	}
	return clusters
}

func TestBuildGatewayClustersWithRingHashLb(t *testing.T) {
	cases := []struct {
		name             string
		ringSize         int
		expectedRingSize int
	}{
		{
			"default",
			0,
			1024,
		},
		{
			"ring size",
			2,
			2,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			gwClusters := features.FilterGatewayClusterConfig
			features.FilterGatewayClusterConfig = false
			defer func() { features.FilterGatewayClusterConfig = gwClusters }()

			c := xdstest.ExtractCluster("outbound|8080||*.example.org",
				buildTestClusters(clusterTest{
					t: t, serviceHostname: "*.example.org", nodeType: model.Router, mesh: testMesh,
					destRule: &networking.DestinationRule{
						Host: "*.example.org",
						TrafficPolicy: &networking.TrafficPolicy{
							LoadBalancer: &networking.LoadBalancerSettings{
								LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
									ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
										MinimumRingSize: uint64(tt.ringSize),
										HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
											HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
												Name: "hash-cookie",
												Ttl:  &types.Duration{Nanos: 100},
											},
										},
									},
								},
							},
						},
					},
				}))

			g.Expect(c.LbPolicy).To(Equal(cluster.Cluster_RING_HASH))
			g.Expect(c.GetRingHashLbConfig().GetMinimumRingSize().GetValue()).To(Equal(uint64(tt.expectedRingSize)))
			g.Expect(c.ConnectTimeout).To(Equal(ptypes.DurationProto(time.Duration(10000000001))))
		})
	}
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
	cases := []struct {
		sni      string
		expected string
	}{
		{"foo.com", "foo.com"},
		{"", "outbound_.8080_.foobar_.foo.example.org"},
	}
	for _, tt := range cases {
		t.Run(tt.sni, func(t *testing.T) {
			g := NewWithT(t)

			c := xdstest.ExtractCluster("outbound|8080|foobar|foo.example.org", buildSniTestClustersForSidecar(t, tt.sni))
			g.Expect(getTLSContext(t, c).GetSni()).To(Equal(tt.expected))
		})
	}
}

func TestBuildClustersWithMutualTlsAndNodeMetadataCertfileOverrides(t *testing.T) {
	expectedClientKeyPath := "/clientKeyFromNodeMetadata.pem"
	expectedClientCertPath := "/clientCertFromNodeMetadata.pem"
	expectedRootCertPath := "/clientRootCertFromNodeMetadata.pem"

	g := NewWithT(t)

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

	clusters := buildTestClusters(clusterTest{
		t:                 t,
		serviceHostname:   "foo.example.org",
		serviceResolution: model.ClientSideLB,
		nodeType:          model.SidecarProxy,
		mesh:              testMesh,
		destRule:          destRule,
		meta:              envoyMetadata,
		istioVersion:      model.MaxIstioVersion,
	})

	// per the docs: default values will be applied to fields omitted in port-level traffic policies rather than inheriting
	// settings specified at the destination level
	g.Expect(getTLSContext(t, xdstest.ExtractCluster("outbound|8080|foobar|foo.example.org", clusters))).To(BeNil())

	expected := []string{
		"outbound|8080||foo.example.org",
		"outbound|9090||foo.example.org",
		"outbound|9090|foobar|foo.example.org",
	}
	for _, e := range expected {
		c := xdstest.ExtractCluster(e, clusters)
		tlsContext := getTLSContext(t, c)
		g.Expect(tlsContext).NotTo(BeNil())

		rootSdsConfig := tlsContext.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig()
		g.Expect(rootSdsConfig.GetName()).To(Equal("file-root:/defaultCaCert.pem"))

		certSdsConfig := tlsContext.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()
		g.Expect(certSdsConfig).To(HaveLen(1))
		g.Expect(certSdsConfig[0].GetName()).To(Equal("file-cert:/defaultCert.pem~/defaultPrivateKey.pem"))
	}
}

func buildSniTestClustersForSidecar(t *testing.T, sniValue string) []*cluster.Cluster {
	return buildSniTestClustersWithMetadata(t, sniValue, model.SidecarProxy, &model.NodeMetadata{})
}

func buildSniDnatTestClustersForGateway(t *testing.T, sniValue string) []*cluster.Cluster {
	return buildSniTestClustersWithMetadata(t, sniValue, model.Router, &model.NodeMetadata{RouterMode: string(model.SniDnatRouter)})
}

func buildSniTestClustersWithMetadata(t testing.TB, sniValue string, typ model.NodeType, meta *model.NodeMetadata) []*cluster.Cluster {
	return buildTestClusters(clusterTest{
		t: t, serviceHostname: "foo.example.org", nodeType: typ, mesh: testMesh,
		destRule: &networking.DestinationRule{
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
		meta:         meta,
		istioVersion: model.MaxIstioVersion,
	})
}

func TestBuildSidecarClustersWithMeshWideTCPKeepalive(t *testing.T) {
	cases := []struct {
		name      string
		rule      ConfigType
		keepalive *core.TcpKeepalive
	}{
		{
			// Do not set tcp_keepalive anywhere
			"unset",
			None,
			nil,
		},
		{
			// Set mesh wide default for tcp_keepalive.
			"mesh",
			Mesh,
			&core.TcpKeepalive{KeepaliveTime: &wrappers.UInt32Value{Value: uint32(MeshWideTCPKeepaliveSeconds)}},
		},
		{
			// Set DestinationRule override for tcp_keepalive.
			"destination rule",
			DestinationRule,
			&core.TcpKeepalive{KeepaliveTime: &wrappers.UInt32Value{Value: uint32(DestinationRuleTCPKeepaliveSeconds)}},
		},
		{
			// Set DestinationRule override for tcp_keepalive with empty value.
			"destination rule default",
			DestinationRuleForOsDefault,
			&core.TcpKeepalive{KeepaliveTime: &wrappers.UInt32Value{Value: uint32(MeshWideTCPKeepaliveSeconds)}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := xdstest.ExtractCluster("outbound|8080|foobar|foo.example.org", buildTestClustersWithTCPKeepalive(t, tt.rule))
			if diff := cmp.Diff(c.GetUpstreamConnectionOptions().GetTcpKeepalive(), tt.keepalive, protocmp.Transform()); diff != "" {
				t.Fatalf("got diff: %v", diff)
			}
		})
	}
}

func buildTestClustersWithTCPKeepalive(t testing.TB, configType ConfigType) []*cluster.Cluster {
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

	return buildTestClusters(clusterTest{
		t: t, serviceHostname: "foo.example.org", nodeType: model.SidecarProxy, mesh: m,
		destRule: &networking.DestinationRule{
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
		},
	})
}

func TestClusterMetadata(t *testing.T) {
	g := NewWithT(t)

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

	clusters := buildTestClusters(clusterTest{t: t, serviceHostname: "*.example.org", nodeType: model.SidecarProxy, mesh: testMesh, destRule: destRule})

	clustersWithMetadata := 0

	foundSubset := false
	for _, cluster := range clusters {
		if strings.HasPrefix(cluster.Name, "outbound") || strings.HasPrefix(cluster.Name, "inbound") {
			clustersWithMetadata++
			g.Expect(cluster.Metadata).NotTo(BeNil())
			md := cluster.Metadata
			g.Expect(md.FilterMetadata[util.IstioMetadataKey]).NotTo(BeNil())
			istio := md.FilterMetadata[util.IstioMetadataKey]
			g.Expect(istio.Fields["config"]).NotTo(BeNil())
			dr := istio.Fields["config"]
			g.Expect(dr.GetStringValue()).To(Equal("/apis/networking.istio.io/v1alpha3/namespaces//destination-rule/acme"))
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

	sniClusters := buildSniDnatTestClustersForGateway(t, "test-sni")

	foundSNISubset := false
	for _, cluster := range sniClusters {
		if strings.HasPrefix(cluster.Name, "outbound") {
			g.Expect(cluster.Metadata).NotTo(BeNil())
			md := cluster.Metadata
			g.Expect(md.FilterMetadata[util.IstioMetadataKey]).NotTo(BeNil())
			istio := md.FilterMetadata[util.IstioMetadataKey]
			g.Expect(istio.Fields["config"]).NotTo(BeNil())
			dr := istio.Fields["config"]
			g.Expect(dr.GetStringValue()).To(Equal("/apis/networking.istio.io/v1alpha3/namespaces//destination-rule/acme"))
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

func TestBuildAutoMtlsSettings(t *testing.T) {
	tlsSettings := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}
	tests := []struct {
		name            string
		tls             *networking.ClientTLSSettings
		sans            []string
		sni             string
		proxy           *model.Proxy
		autoMTLSEnabled bool
		meshExternal    bool
		serviceMTLSMode model.MutualTLSMode
		want            *networking.ClientTLSSettings
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
			false, false, model.MTLSUnknown,
			&networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"spiffe://foo/serviceaccount/1"},
				Sni:             "foo.com",
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
			true, false, model.MTLSStrict,
			&networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"spiffe://foo/serviceaccount/1"},
				Sni:             "foo.com",
			},
			autoDetected,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in permissive mode",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSPermissive,
			&networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
				SubjectAltNames: []string{"spiffe://foo/serviceaccount/1"},
				Sni:             "foo.com",
			},
			autoDetected,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in plaintext mode",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSDisable,
			nil,
			userSupplied,
		},
		{
			"Auto fill nil settings when mTLS nil for internal service in unknown mode",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, false, model.MTLSUnknown,
			nil,
			userSupplied,
		},
		{
			"Do not auto fill nil settings for external",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			true, true, model.MTLSUnknown,
			nil,
			userSupplied,
		},
		{
			"Do not auto fill nil settings if server mTLS is disabled",
			nil,
			[]string{"spiffe://foo/serviceaccount/1"},
			"foo.com",
			&model.Proxy{Metadata: &model.NodeMetadata{}},
			false, false, model.MTLSDisable,
			nil,
			userSupplied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTLS, gotCtxType := buildAutoMtlsSettings(tt.tls, tt.sans, tt.sni, tt.proxy,
				tt.autoMTLSEnabled, tt.meshExternal, tt.serviceMTLSMode)
			if !reflect.DeepEqual(gotTLS, tt.want) {
				t.Errorf("cluster TLS does not match expected result want %#v, got %#v", tt.want, gotTLS)
			}
			if gotCtxType != tt.wantCtxType {
				t.Errorf("cluster TLS context type does not match expected result want %#v, got %#v", tt.wantCtxType, gotTLS)
			}
		})
	}
}

func TestDisablePanicThresholdAsDefault(t *testing.T) {
	g := NewWithT(t)

	outliers := []*networking.OutlierDetection{
		// Unset MinHealthPercent
		{},
		// Explicitly set MinHealthPercent to 0
		{
			MinHealthPercent: 0,
		},
	}

	for _, outlier := range outliers {
		c := xdstest.ExtractCluster("outbound|8080||*.example.org",
			buildTestClusters(clusterTest{
				t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
				locality: &core.Locality{}, mesh: testMesh,
				destRule: &networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: outlier,
					},
				},
			}))
		g.Expect(c.CommonLbConfig.HealthyPanicThreshold).To(Not(BeNil()))
		g.Expect(c.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(0)))
	}
}

func TestApplyOutlierDetection(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name string
		cfg  *networking.OutlierDetection
		o    *cluster.OutlierDetection
	}{
		{
			"Nil outlier detection",
			nil,
			nil,
		},
		{
			"No outlier detection is set",
			&networking.OutlierDetection{},
			&cluster.OutlierDetection{
				EnforcingSuccessRate: &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Consecutive gateway and 5xx errors are set",
			&networking.OutlierDetection{
				Consecutive_5XxErrors:    &types.UInt32Value{Value: 4},
				ConsecutiveGatewayErrors: &types.UInt32Value{Value: 3},
			},
			&cluster.OutlierDetection{
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
			&cluster.OutlierDetection{
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
			&cluster.OutlierDetection{
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
			&cluster.OutlierDetection{
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
			&cluster.OutlierDetection{
				Consecutive_5Xx:          &wrappers.UInt32Value{Value: 0},
				EnforcingConsecutive_5Xx: &wrappers.UInt32Value{Value: 0},
				EnforcingSuccessRate:     &wrappers.UInt32Value{Value: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := xdstest.ExtractCluster("outbound|8080||*.example.org",
				buildTestClusters(clusterTest{
					t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
					locality: &core.Locality{}, mesh: testMesh,
					destRule: &networking.DestinationRule{
						Host: "*.example.org",
						TrafficPolicy: &networking.TrafficPolicy{
							OutlierDetection: tt.cfg,
						},
					},
				}))
			g.Expect(c.OutlierDetection).To(Equal(tt.o))
		})
	}
}

func TestStatNamePattern(t *testing.T) {
	g := NewWithT(t)

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

	clusters := buildTestClusters(clusterTest{
		t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
		locality: &core.Locality{}, mesh: statConfigMesh,
		destRule: &networking.DestinationRule{
			Host: "*.example.org",
		},
	})
	g.Expect(xdstest.ExtractCluster("outbound|8080||*.example.org", clusters).AltStatName).To(Equal("*.example.org_default_8080"))
	g.Expect(xdstest.ExtractCluster("inbound|10001||", clusters).AltStatName).To(Equal("LocalService_*.example.org"))
}

func TestDuplicateClusters(t *testing.T) {
	buildTestClusters(clusterTest{
		t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
		locality: &core.Locality{}, mesh: testMesh,
		destRule: &networking.DestinationRule{
			Host: "*.example.org",
		},
	})
}

func TestSidecarLocalityLB(t *testing.T) {
	g := NewWithT(t)
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

	c := xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: testMesh,
			destRule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 10,
					},
				},
			},
		}))

	if c.CommonLbConfig == nil {
		t.Fatalf("CommonLbConfig should be set for cluster %+v", c)
	}
	g.Expect(c.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

	g.Expect(len(c.LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range c.LoadAssignment.Endpoints {
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

	c = xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: testMesh,
			destRule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 10,
					},
				},
			},
		}))
	if c.CommonLbConfig == nil {
		t.Fatalf("CommonLbConfig should be set for cluster %+v", c)
	}
	g.Expect(c.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

	g.Expect(len(c.LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range c.LoadAssignment.Endpoints {
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
	g := NewWithT(t)
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

	c := xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: testMesh,
			destRule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 10,
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
			},
		}))

	if c.CommonLbConfig == nil {
		t.Fatalf("CommonLbConfig should be set for cluster %+v", c)
	}
	g.Expect(c.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

	g.Expect(len(c.LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range c.LoadAssignment.Endpoints {
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
	g := NewWithT(t)
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

	gwClusters := features.FilterGatewayClusterConfig
	features.FilterGatewayClusterConfig = false
	defer func() { features.FilterGatewayClusterConfig = gwClusters }()

	c := xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.Router,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: testMesh,
			destRule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 10,
					},
				},
			},
			meta: &model.NodeMetadata{RouterMode: string(model.SniDnatRouter)},
		}))

	if c.CommonLbConfig == nil {
		t.Errorf("CommonLbConfig should be set for cluster %+v", c)
	}
	g.Expect(c.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))
	g.Expect(len(c.LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range c.LoadAssignment.Endpoints {
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
	testMesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{}

	c = xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.Router,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: testMesh,
			destRule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 10,
					},
				},
			}, // peerAuthn
			meta: &model.NodeMetadata{RouterMode: string(model.SniDnatRouter)},
		}))

	if c.CommonLbConfig == nil {
		t.Fatalf("CommonLbConfig should be set for cluster %+v", c)
	}
	g.Expect(c.CommonLbConfig.HealthyPanicThreshold.GetValue()).To(Equal(float64(10)))

	g.Expect(len(c.LoadAssignment.Endpoints)).To(Equal(3))
	for _, localityLbEndpoint := range c.LoadAssignment.Endpoints {
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
	configgen := NewConfigGenerator([]plugin.Plugin{}, &model.DisabledCache{})
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
	g := NewWithT(t)

	clusters := buildTestClusters(clusterTest{
		t:                 t,
		serviceHostname:   "*.example.org",
		serviceResolution: model.Passthrough,
		nodeType:          model.SidecarProxy,
		mesh:              testMesh,
		destRule: &networking.DestinationRule{
			Host: "*.example.org",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
			},
		},
	})

	c := xdstest.ExtractCluster("outbound|8080||*.example.org",
		clusters)
	g.Expect(c.LbPolicy).To(Equal(cluster.Cluster_CLUSTER_PROVIDED))
	g.Expect(c.GetClusterDiscoveryType()).To(Equal(&cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST}))
}

func TestClusterDiscoveryTypeAndLbPolicyPassthrough(t *testing.T) {
	g := NewWithT(t)

	clusters := buildTestClusters(clusterTest{
		t:                 t,
		serviceHostname:   "*.example.org",
		serviceResolution: model.ClientSideLB,
		nodeType:          model.SidecarProxy,
		mesh:              testMesh,
		destRule: &networking.DestinationRule{
			Host: "*.example.org",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_PASSTHROUGH,
					},
				},
			},
		},
	})

	c := xdstest.ExtractCluster("outbound|8080||*.example.org", clusters)
	g.Expect(c.LbPolicy).To(Equal(cluster.Cluster_CLUSTER_PROVIDED))
	g.Expect(c.GetClusterDiscoveryType()).To(Equal(&cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST}))
	g.Expect(c.EdsClusterConfig).To(BeNil())
}

func TestBuildInboundClustersPortLevelCircuitBreakerThresholds(t *testing.T) {
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
				Address:      "1.1.1.1",
				EndpointPort: 10001,
			},
		},
	}
	inboundFilter := func(c *cluster.Cluster) bool {
		return strings.HasPrefix(c.Name, "inbound|")
	}

	cases := []struct {
		name     string
		filter   func(c *cluster.Cluster) bool
		destRule *networking.DestinationRule
		expected *cluster.CircuitBreakers_Thresholds
	}{
		{
			name: "defaults",
			filter: func(c *cluster.Cluster) bool {
				return strings.HasPrefix(c.Name, "inbound|") || strings.HasPrefix(c.Name, "outbound|")
			},
			destRule: nil,
			expected: getDefaultCircuitBreakerThresholds(),
		},
		{
			name:   "port-level policy matched",
			filter: inboundFilter,
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
			expected: &cluster.CircuitBreakers_Thresholds{
				MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxConnections:     &wrappers.UInt32Value{Value: 100},
				MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
			},
		},
		{
			name:   "port-level policy not matched",
			filter: inboundFilter,
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
			expected: &cluster.CircuitBreakers_Thresholds{
				MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
				MaxConnections:     &wrappers.UInt32Value{Value: 1000},
				MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			cfgs := []config.Config{}
			if c.destRule != nil {
				cfgs = append(cfgs, config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "default",
					},
					Spec: c.destRule,
				})
			}
			cg := NewConfigGenTest(t, TestOptions{
				Services:  []*model.Service{service},
				Instances: instances,
				Configs:   cfgs,
			})
			clusters := cg.Clusters(cg.SetupProxy(nil))
			xdstest.ValidateClusters(t, clusters)
			if c.filter != nil {
				clusters = xdstest.FilterClusters(clusters, c.filter)
			}
			g.Expect(len(clusters)).ShouldNot(Equal(0))

			for _, cluster := range clusters {
				g.Expect(cluster.CircuitBreakers).NotTo(BeNil())
				g.Expect(cluster.CircuitBreakers.Thresholds[0]).To(Equal(c.expected))
			}
		})
	}
}

func TestRedisProtocolWithPassThroughResolutionAtGateway(t *testing.T) {
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

	cases := []struct {
		name          string
		redisEnabled  bool
		resolution    model.Resolution
		lbType        cluster.Cluster_LbPolicy
		discoveryType cluster.Cluster_DiscoveryType
	}{
		{
			name:          "redis disabled",
			redisEnabled:  false,
			resolution:    model.ClientSideLB,
			lbType:        cluster.Cluster_ROUND_ROBIN,
			discoveryType: cluster.Cluster_EDS,
		},
		{
			name:          "redis disabled passthrough",
			redisEnabled:  false,
			resolution:    model.Passthrough,
			lbType:        cluster.Cluster_ROUND_ROBIN,
			discoveryType: cluster.Cluster_EDS,
		},
		{
			name:          "redis enabled",
			redisEnabled:  true,
			resolution:    model.ClientSideLB,
			lbType:        cluster.Cluster_MAGLEV,
			discoveryType: cluster.Cluster_EDS,
		},
		{
			name:          "redis enabled passthrough",
			redisEnabled:  true,
			resolution:    model.Passthrough,
			lbType:        cluster.Cluster_MAGLEV,
			discoveryType: cluster.Cluster_EDS,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			gwClusters := features.FilterGatewayClusterConfig
			features.FilterGatewayClusterConfig = false
			defer func() { features.FilterGatewayClusterConfig = gwClusters }()

			if tt.redisEnabled {
				defaultValue := features.EnableRedisFilter
				features.EnableRedisFilter = true
				defer func() { features.EnableRedisFilter = defaultValue }()
			}
			cg := NewConfigGenTest(t, TestOptions{Services: []*model.Service{service}})
			clusters := cg.Clusters(cg.SetupProxy(&model.Proxy{Type: model.Router}))
			xdstest.ValidateClusters(t, clusters)

			c := xdstest.ExtractCluster("outbound|6379||redis.com", clusters)
			g.Expect(c.LbPolicy).To(Equal(tt.lbType))
			g.Expect(c.GetClusterDiscoveryType()).To(Equal(&cluster.Cluster_Type{Type: tt.discoveryType}))
		})
	}
}

func TestAutoMTLSClusterSubsets(t *testing.T) {
	g := NewWithT(t)

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

	clusters := buildTestClusters(clusterTest{t: t, serviceHostname: TestServiceNHostname, nodeType: model.SidecarProxy, mesh: testMesh, destRule: destRule})

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
	g := NewWithT(t)

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

	clusters := buildTestClusters(clusterTest{
		t:               t,
		serviceHostname: TestServiceNHostname,
		nodeType:        model.SidecarProxy,
		mesh:            testMesh,
		destRule:        destRule,
		peerAuthn:       peerAuthn,
	})

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
		name                           string
		lbSettings                     *networking.LoadBalancerSettings
		discoveryType                  cluster.Cluster_DiscoveryType
		port                           *model.Port
		expectedLbPolicy               cluster.Cluster_LbPolicy
		expectedLocalityWeightedConfig bool
	}{
		{
			name:             "lb = nil ORIGINAL_DST discovery type",
			discoveryType:    cluster.Cluster_ORIGINAL_DST,
			expectedLbPolicy: cluster.Cluster_CLUSTER_PROVIDED,
		},
		{
			name:             "lb = nil redis protocol",
			discoveryType:    cluster.Cluster_EDS,
			port:             &model.Port{Protocol: protocol.Redis},
			expectedLbPolicy: cluster.Cluster_MAGLEV,
		},
		{
			name: "Loadbalancer has distribute",
			lbSettings: &networking.LoadBalancerSettings{
				LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
					Enabled: &types.BoolValue{Value: true},
					Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
						{
							From: "region1/zone1/subzone1",
							To: map[string]uint32{
								"region1/zone1/subzone1": 80,
								"region1/zone1/subzone2": 15,
								"region1/zone1/subzone3": 5,
							},
						},
					},
				},
			},
			discoveryType:                  cluster.Cluster_EDS,
			port:                           &model.Port{Protocol: protocol.HTTP},
			expectedLbPolicy:               cluster.Cluster_ROUND_ROBIN,
			expectedLocalityWeightedConfig: true,
		},
		// TODO: add more to cover all cases
	}

	proxy := model.Proxy{
		Type:         model.SidecarProxy,
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			cluster := &cluster.Cluster{
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: test.discoveryType},
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

			if test.expectedLocalityWeightedConfig && cluster.CommonLbConfig.GetLocalityWeightedLbConfig() == nil {
				t.Errorf("cluster expected to have weighed config, but is nil")
			}
		})
	}
}

func TestApplyUpstreamTLSSettings(t *testing.T) {
	istioMutualTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	istioMutualTLSSettings := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}
	mutualTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_MUTUAL,
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	simpleTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_SIMPLE,
		CaCertificates:  constants.DefaultRootCert,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}

	http2ProtocolOptions := &core.Http2ProtocolOptions{
		AllowConnect:  true,
		AllowMetadata: true,
	}

	expectedNodeMetadataClientKeyPath := "/clientKeyFromNodeMetadata.pem"
	expectedNodeMetadataClientCertPath := "/clientCertFromNodeMetadata.pem"
	expectedNodeMetadataRootCertPath := "/clientRootCertFromNodeMetadata.pem"

	tests := []struct {
		name                       string
		mtlsCtx                    mtlsContextType
		discoveryType              cluster.Cluster_DiscoveryType
		tls                        *networking.ClientTLSSettings
		customMetadata             *model.NodeMetadata
		allowCustomMetadataMutual  bool
		expectTransportSocket      bool
		expectTransportSocketMatch bool
		http2ProtocolOptions       *core.Http2ProtocolOptions

		validateTLSContext func(t *testing.T, ctx *tls.UpstreamTlsContext)
	}{
		{
			name:                       "user specified without tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        nil,
			expectTransportSocket:      false,
			expectTransportSocketMatch: false,
		},
		{
			name:                       "user specified with istio_mutual metadata certs tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); len(got) != 0 {
					t.Fatalf("expected empty alpn list got %v", got)
				}
			},
		},
		{
			name:                       "user specified with istio_mutual metadata certs tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNH2Only) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNH2Only, got)
				}
			},
		},
		{
			name:                       "user specified with istio_mutual tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
		{
			name:                       "user specified with istio_mutual tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshH2WithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshH2WithMxc, got)
				}
			},
		},
		{
			name:                       "user specified simple tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        simpleTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); got != nil {
					t.Fatalf("expected alpn list nil as not h2 or Istio_Mutual TLS Setting; got %v", got)
				}
				if got := ctx.GetSni(); got != simpleTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", simpleTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "user specified simple tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        simpleTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNH2Only) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNH2Only, got)
				}
				if got := ctx.GetSni(); got != simpleTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", simpleTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "user specified mutual tls",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        mutualTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettingsWithCerts.ClientCertificate, mutualTLSSettingsWithCerts.PrivateKey)
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()[0].GetName(); certName != got {
					t.Fatalf("expected cert name %v got %v", certName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); got != nil {
					t.Fatalf("expected alpn list nil as not h2 or Istio_Mutual TLS Setting; got %v", got)
				}
				if got := ctx.GetSni(); got != mutualTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", mutualTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "user specified mutual tls with h2",
			mtlsCtx:                    userSupplied,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        mutualTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettingsWithCerts.ClientCertificate, mutualTLSSettingsWithCerts.PrivateKey)
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()[0].GetName(); certName != got {
					t.Fatalf("expected cert name %v got %v", certName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNH2Only) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNH2Only, got)
				}
				if got := ctx.GetSni(); got != mutualTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", mutualTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:                       "auto detect with tls",
			mtlsCtx:                    autoDetected,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
		{
			name:                       "auto detect with tls and h2 options",
			mtlsCtx:                    autoDetected,
			discoveryType:              cluster.Cluster_EDS,
			tls:                        istioMutualTLSSettings,
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
			http2ProtocolOptions:       http2ProtocolOptions,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshH2WithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshH2WithMxc, got)
				}
			},
		},
		{
			name:                       "auto detect with tls",
			mtlsCtx:                    autoDetected,
			discoveryType:              cluster.Cluster_ORIGINAL_DST,
			tls:                        istioMutualTLSSettingsWithCerts,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
		},
		{
			name:          "user specified mutual tls with overridden certs from node metadata allowed",
			mtlsCtx:       userSupplied,
			discoveryType: cluster.Cluster_EDS,
			tls:           mutualTLSSettingsWithCerts,
			customMetadata: &model.NodeMetadata{
				TLSClientCertChain: expectedNodeMetadataClientCertPath,
				TLSClientKey:       expectedNodeMetadataClientKeyPath,
				TLSClientRootCert:  expectedNodeMetadataRootCertPath,
			},
			allowCustomMetadataMutual:  true,
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + expectedNodeMetadataRootCertPath
				certName := fmt.Sprintf("file-cert:%s~%s", expectedNodeMetadataClientCertPath, expectedNodeMetadataClientKeyPath)
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
			name:          "user specified mutual tls with overridden certs from node metadata not allowed",
			mtlsCtx:       userSupplied,
			discoveryType: cluster.Cluster_EDS,
			tls:           mutualTLSSettingsWithCerts,
			customMetadata: &model.NodeMetadata{
				TLSClientCertChain: expectedNodeMetadataClientCertPath,
				TLSClientKey:       expectedNodeMetadataClientKeyPath,
				TLSClientRootCert:  expectedNodeMetadataRootCertPath,
			},
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettingsWithCerts.ClientCertificate, mutualTLSSettingsWithCerts.PrivateKey)
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
	}

	proxy := &model.Proxy{
		Type:         model.SidecarProxy,
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}
	push := model.NewPushContext()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			customMetadataMutual := features.AllowMetadataCertsInMutualTLS
			if test.allowCustomMetadataMutual {
				features.AllowMetadataCertsInMutualTLS = true
			}
			defer func() { features.AllowMetadataCertsInMutualTLS = customMetadataMutual }()
			if test.customMetadata != nil {
				proxy.Metadata = test.customMetadata
			} else {
				proxy.Metadata = &model.NodeMetadata{}
			}
			opts := &buildClusterOpts{
				cluster: &cluster.Cluster{
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: test.discoveryType},
					Http2ProtocolOptions: test.http2ProtocolOptions,
				},
				proxy: proxy,
				mesh:  push.Mesh,
			}
			applyUpstreamTLSSettings(opts, test.tls, test.mtlsCtx)

			if test.expectTransportSocket && opts.cluster.TransportSocket == nil ||
				!test.expectTransportSocket && opts.cluster.TransportSocket != nil {
				t.Errorf("Expected TransportSocket %v", test.expectTransportSocket)
			}
			if test.expectTransportSocketMatch && opts.cluster.TransportSocketMatches == nil ||
				!test.expectTransportSocketMatch && opts.cluster.TransportSocketMatches != nil {
				t.Errorf("Expected TransportSocketMatch %v", test.expectTransportSocketMatch)
			}

			if test.validateTLSContext != nil {
				ctx := &tls.UpstreamTlsContext{}
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

type expectedResult struct {
	tlsContext *tls.UpstreamTlsContext
	err        error
}

// TestBuildUpstreamClusterTLSContext tests the buildUpstreamClusterTLSContext function
func TestBuildUpstreamClusterTLSContext(t *testing.T) {
	metadataClientCert := "/path/to/metadata/cert"
	metadataRootCert := "/path/to/metadata/root-cert"
	metadataClientKey := "/path/to/metadata/key"

	clientCert := "/path/to/cert"
	rootCert := "path/to/cacert"
	clientKey := "/path/to/key"

	credentialName := "some-fake-credential"

	testCases := []struct {
		name   string
		opts   *buildClusterOpts
		tls    *networking.ClientTLSSettings
		result expectedResult
	}{
		{
			name: "tls mode disabled",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_DISABLE,
			},
			result: expectedResult{nil, nil},
		},
		{
			name: "tls mode ISTIO_MUTUAL with metadata certs",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				ClientCertificate: metadataClientCert,
				PrivateKey:        metadataClientKey,
				CaCertificates:    metadataRootCert,
				SubjectAltNames:   []string{"SAN"},
				Sni:               "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name: "file-cert:/path/to/metadata/cert~/path/to/metadata/key",
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									ResourceApiVersion: core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: "file-root:/path/to/metadata/root-cert",
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										ResourceApiVersion: core.ApiVersion_V3,
									},
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode ISTIO_MUTUAL with metadata certs and H2",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name:                 "test-cluster",
					Http2ProtocolOptions: &core.Http2ProtocolOptions{},
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				ClientCertificate: metadataClientCert,
				PrivateKey:        metadataClientKey,
				CaCertificates:    metadataRootCert,
				SubjectAltNames:   []string{"SAN"},
				Sni:               "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name: "file-cert:/path/to/metadata/cert~/path/to/metadata/key",
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									ResourceApiVersion: core.ApiVersion_V3,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: "file-root:/path/to/metadata/root-cert",
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										ResourceApiVersion: core.ApiVersion_V3,
									},
								},
							},
						},
						AlpnProtocols: util.ALPNH2Only,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with no certs specified in tls",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						ValidationContextType: &tls.CommonTlsContext_ValidationContext{},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with certs specified in tls",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CaCertificates:  rootCert,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: features.InitialFetchTimeout,
									},
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with certs specified in tls with h2",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name:                 "test-cluster",
					Http2ProtocolOptions: &core.Http2ProtocolOptions{},
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CaCertificates:  rootCert,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: features.InitialFetchTimeout,
									},
								},
							},
						},
						AlpnProtocols: util.ALPNH2Only,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with certs specified in tls with overridden metadata certs",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{
						TLSClientRootCert: metadataRootCert,
					},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CaCertificates:  rootCert,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", metadataRootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: features.InitialFetchTimeout,
									},
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with no client certificate",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "",
				PrivateKey:        "some-fake-key",
			},
			result: expectedResult{
				nil,
				fmt.Errorf("client cert must be provided"),
			},
		},
		{
			name: "tls mode MUTUAL, with no client key",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "some-fake-cert",
				PrivateKey:        "",
			},
			result: expectedResult{
				nil,
				fmt.Errorf("client key must be provided"),
			},
		},
		{
			name: "tls mode MUTUAL, with node metadata sdsEnabled true no root CA specified",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: clientCert,
				PrivateKey:        clientKey,
				SubjectAltNames:   []string{"SAN"},
				Sni:               "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name: fmt.Sprintf("file-cert:%s~%s", clientCert, clientKey),
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									ResourceApiVersion:  core.ApiVersion_V3,
									InitialFetchTimeout: features.InitialFetchTimeout,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_ValidationContext{},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with node metadata sdsEnabled true",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: clientCert,
				PrivateKey:        clientKey,
				CaCertificates:    rootCert,
				SubjectAltNames:   []string{"SAN"},
				Sni:               "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name: fmt.Sprintf("file-cert:%s~%s", clientCert, clientKey),
								SdsConfig: &core.ConfigSource{
									ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
										ApiConfigSource: &core.ApiConfigSource{
											ApiType:                   core.ApiConfigSource_GRPC,
											SetNodeOnFirstMessageOnly: true,
											TransportApiVersion:       core.ApiVersion_V3,
											GrpcServices: []*core.GrpcService{
												{
													TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
														EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
													},
												},
											},
										},
									},
									ResourceApiVersion:  core.ApiVersion_V3,
									InitialFetchTimeout: features.InitialFetchTimeout,
								},
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"})},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name: fmt.Sprintf("file-root:%s", rootCert),
									SdsConfig: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:                   core.ApiConfigSource_GRPC,
												SetNodeOnFirstMessageOnly: true,
												TransportApiVersion:       core.ApiVersion_V3,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "sds-grpc"},
														},
													},
												},
											},
										},
										ResourceApiVersion:  core.ApiVersion_V3,
										InitialFetchTimeout: features.InitialFetchTimeout,
									},
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with CredentialName specified",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Type:     model.Router,
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with CredentialName specified",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Type:     model.Router,
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_SIMPLE,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode SIMPLE, with CredentialName specified with h2 and no SAN",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name:                 "test-cluster",
					Http2ProtocolOptions: &core.Http2ProtocolOptions{},
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Type:     model.Router,
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_SIMPLE,
				CredentialName: credentialName,
				Sni:            "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
						AlpnProtocols: util.ALPNH2Only,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with CredentialName specified",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Type:     model.Router,
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:            networking.ClientTLSSettings_MUTUAL,
				CredentialName:  credentialName,
				SubjectAltNames: []string{"SAN"},
				Sni:             "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name:      "kubernetes://" + credentialName,
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{
									MatchSubjectAltNames: util.StringToExactMatch([]string{"SAN"}),
								},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, with CredentialName specified with h2 and no SAN",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name:                 "test-cluster",
					Http2ProtocolOptions: &core.Http2ProtocolOptions{},
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Type:     model.Router,
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_MUTUAL,
				CredentialName: credentialName,
				Sni:            "some-sni.com",
			},
			result: expectedResult{
				tlsContext: &tls.UpstreamTlsContext{
					CommonTlsContext: &tls.CommonTlsContext{
						TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
							{
								Name:      "kubernetes://" + credentialName,
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
						ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
							CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
								DefaultValidationContext: &tls.CertificateValidationContext{},
								ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
									Name:      "kubernetes://" + credentialName + authn_model.SdsCaSuffix,
									SdsConfig: authn_model.SDSAdsConfig,
								},
							},
						},
						AlpnProtocols: util.ALPNH2Only,
					},
					Sni: "some-sni.com",
				},
				err: nil,
			},
		},
		{
			name: "tls mode MUTUAL, credentialName is set with proxy type Sidecar",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Type:     model.SidecarProxy,
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_MUTUAL,
				CredentialName: "fake-cred",
			},
			result: expectedResult{
				nil,
				nil,
			},
		},
		{
			name: "tls mode SIMPLE, credentialName is set with proxy type Sidecar",
			opts: &buildClusterOpts{
				cluster: &cluster.Cluster{
					Name: "test-cluster",
				},
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Type:     model.SidecarProxy,
				},
			},
			tls: &networking.ClientTLSSettings{
				Mode:           networking.ClientTLSSettings_SIMPLE,
				CredentialName: "fake-cred",
			},
			result: expectedResult{
				nil,
				nil,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret, err := buildUpstreamClusterTLSContext(tc.opts, tc.tls)
			if err != nil && tc.result.err == nil || err == nil && tc.result.err != nil {
				t.Errorf("expecting:\n err=%v but got err=%v", tc.result.err, err)
			} else if diff := cmp.Diff(tc.result.tlsContext, ret, protocmp.Transform()); diff != "" {
				t.Errorf("got diff: `%v", diff)
			}
		})
	}
}

// Helper function to extract TLS context from a cluster
func getTLSContext(t *testing.T, c *cluster.Cluster) *tls.UpstreamTlsContext {
	t.Helper()
	if c.TransportSocket == nil {
		return nil
	}
	tlsContext := &tls.UpstreamTlsContext{}
	err := ptypes.UnmarshalAny(c.TransportSocket.GetTypedConfig(), tlsContext)
	if err != nil {
		t.Fatalf("Failed to unmarshall tls context: %v", err)
	}
	return tlsContext
}

func TestBuildStaticClusterWithNoEndPoint(t *testing.T) {
	g := NewWithT(t)

	service := &model.Service{
		Hostname:    host.Name("static.test"),
		Address:     "1.1.1.2",
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
	cg := NewConfigGenTest(t, TestOptions{
		Services: []*model.Service{service},
	})
	clusters := cg.Clusters(cg.SetupProxy(nil))
	xdstest.ValidateClusters(t, clusters)

	// Expect to ignore STRICT_DNS cluster without endpoints.
	g.Expect(xdstest.MapKeys(xdstest.ExtractClusters(clusters))).To(Equal([]string{"BlackHoleCluster", "InboundPassthroughClusterIpv4", "PassthroughCluster"}))
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
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT,
				},
			},
			upgrade: true,
		},
		{
			name:        "mesh default - dr upgrade non http port",
			clusterName: "bar",
			direction:   model.TrafficDirectionOutbound,
			port:        model.Port{Protocol: protocol.Unsupported},
			mesh:        meshconfig.MeshConfig{},
			connectionPool: networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
				},
			},
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
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT,
				},
			},
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
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
				},
			},
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
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DO_NOT_UPGRADE,
				},
			},
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
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT,
				},
			},
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
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT,
				},
			},
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

func TestEnvoyFilterPatching(t *testing.T) {
	service := &model.Service{
		Hostname: host.Name("static.test"),
		Address:  "1.1.1.1",
		Ports: []*model.Port{
			{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: model.Passthrough,
	}

	cases := []struct {
		name  string
		want  []string
		efs   []*networking.EnvoyFilter
		proxy model.NodeType
		svc   *model.Service
	}{
		{
			"no config",
			[]string{"outbound|8080||static.test", "BlackHoleCluster", "PassthroughCluster", "InboundPassthroughClusterIpv4"},
			nil,
			model.SidecarProxy,
			service,
		},
		{
			"add cluster",
			[]string{"outbound|8080||static.test", "BlackHoleCluster", "PassthroughCluster", "InboundPassthroughClusterIpv4", "new-cluster1"},
			[]*networking.EnvoyFilter{{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
						Value:     buildPatchStruct(`{"name":"new-cluster1"}`),
					},
				}},
			}},
			model.SidecarProxy,
			service,
		},
		{
			"remove cluster",
			[]string{"outbound|8080||static.test", "PassthroughCluster", "InboundPassthroughClusterIpv4"},
			[]*networking.EnvoyFilter{{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{
								Name: "BlackHoleCluster",
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				}},
			}},
			model.SidecarProxy,
			service,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfgs := []config.Config{}
			for i, c := range tt.efs {
				cfgs = append(cfgs, config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.EnvoyFilter,
						Name:             fmt.Sprint(i),
						Namespace:        "default",
					},
					Spec: c,
				})
			}
			cg := NewConfigGenTest(t, TestOptions{Configs: cfgs, Services: []*model.Service{tt.svc}})
			clusters := cg.Clusters(cg.SetupProxy(nil))
			clusterNames := xdstest.MapKeys(xdstest.ExtractClusters(clusters))
			sort.Strings(tt.want)
			if !cmp.Equal(clusterNames, tt.want) {
				t.Fatalf("want %v got %v", tt.want, clusterNames)
			}
		})
	}
}

func TestTelemetryMetadata(t *testing.T) {
	cases := []struct {
		name      string
		direction model.TrafficDirection
		cluster   *cluster.Cluster
		svcInsts  []*model.ServiceInstance
		service   *model.Service
		want      *core.Metadata
	}{
		{
			name:      "no cluster",
			direction: model.TrafficDirectionInbound,
			cluster:   nil,
			svcInsts: []*model.ServiceInstance{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
				},
			},
			want: nil,
		},
		{
			name:      "inbound no service",
			direction: model.TrafficDirectionInbound,
			cluster:   &cluster.Cluster{},
			svcInsts:  []*model.ServiceInstance{},
			want:      nil,
		},
		{
			name:      "inbound existing metadata",
			direction: model.TrafficDirectionInbound,
			cluster: &cluster.Cluster{
				Metadata: &core.Metadata{
					FilterMetadata: map[string]*structpb.Struct{
						"some-metadata": {
							Fields: map[string]*structpb.Value{
								"some-key": {Kind: &structpb.Value_StringValue{StringValue: "some-val"}},
							},
						},
					},
				},
			},
			svcInsts: []*model.ServiceInstance{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					ServicePort: &model.Port{
						Port: 80,
					},
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					"some-metadata": {
						Fields: map[string]*structpb.Value{
							"some-key": {Kind: &structpb.Value_StringValue{StringValue: "some-val"}},
						},
					},
					util.IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"services": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"host": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a.default",
																},
															},
															"name": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a",
																},
															},
															"namespace": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "default",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "inbound existing istio metadata",
			direction: model.TrafficDirectionInbound,
			cluster: &cluster.Cluster{
				Metadata: &core.Metadata{
					FilterMetadata: map[string]*structpb.Struct{
						util.IstioMetadataKey: {
							Fields: map[string]*structpb.Value{
								"some-key": {Kind: &structpb.Value_StringValue{StringValue: "some-val"}},
							},
						},
					},
				},
			},
			svcInsts: []*model.ServiceInstance{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					ServicePort: &model.Port{
						Port: 80,
					},
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					util.IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"some-key": {Kind: &structpb.Value_StringValue{StringValue: "some-val"}},
							"services": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"host": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a.default",
																},
															},
															"name": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a",
																},
															},
															"namespace": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "default",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "inbound multiple services",
			direction: model.TrafficDirectionInbound,
			cluster:   &cluster.Cluster{},
			svcInsts: []*model.ServiceInstance{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					ServicePort: &model.Port{
						Port: 80,
					},
				},
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "b",
							Namespace: "default",
						},
						Hostname: "b.default",
					},
					ServicePort: &model.Port{
						Port: 80,
					},
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					util.IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"services": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"host": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a.default",
																},
															},
															"name": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a",
																},
															},
															"namespace": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "default",
																},
															},
														},
													},
												},
											},
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"host": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "b.default",
																},
															},
															"name": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "b",
																},
															},
															"namespace": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "default",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "inbound existing services metadata",
			direction: model.TrafficDirectionInbound,
			cluster: &cluster.Cluster{
				Metadata: &core.Metadata{
					FilterMetadata: map[string]*structpb.Struct{
						util.IstioMetadataKey: {
							Fields: map[string]*structpb.Value{
								"services": {Kind: &structpb.Value_StringValue{StringValue: "some-val"}},
							},
						},
					},
				},
			},
			svcInsts: []*model.ServiceInstance{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					ServicePort: &model.Port{
						Port: 80,
					},
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					util.IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"services": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"host": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a.default",
																},
															},
															"name": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a",
																},
															},
															"namespace": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "default",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "outbound service metadata",
			direction: model.TrafficDirectionOutbound,
			cluster:   &cluster.Cluster{},
			service: &model.Service{
				Attributes: model.ServiceAttributes{
					Name:      "a",
					Namespace: "default",
				},
				Hostname: "a.default",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					util.IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"services": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"host": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a.default",
																},
															},
															"name": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a",
																},
															},
															"namespace": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "default",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "inbound duplicated metadata",
			direction: model.TrafficDirectionInbound,
			cluster:   &cluster.Cluster{},
			svcInsts: []*model.ServiceInstance{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					ServicePort: &model.Port{
						Port: 80,
					},
				},
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					ServicePort: &model.Port{
						Port: 80,
					},
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					util.IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"services": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"host": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a.default",
																},
															},
															"name": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "a",
																},
															},
															"namespace": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "default",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			opt := buildClusterOpts{
				cluster: tt.cluster,
				port:    &model.Port{Port: 80},
				proxy: &model.Proxy{
					ServiceInstances: tt.svcInsts,
				},
			}
			addTelemetryMetadata(opt, tt.service, tt.direction, tt.svcInsts)
			if opt.cluster != nil && !reflect.DeepEqual(opt.cluster.Metadata, tt.want) {
				t.Errorf("cluster metadata does not match expectation want %+v, got %+v", tt.want, opt.cluster.Metadata)
			}
		})
	}
}
