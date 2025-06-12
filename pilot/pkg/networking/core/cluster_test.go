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

package core

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
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	authn_beta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
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

func testMesh() *meshconfig.MeshConfig {
	return &meshconfig.MeshConfig{
		ConnectTimeout: &durationpb.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		EnableAutoMtls: &wrappers.BoolValue{
			Value: false,
		},
		InboundTrafficPolicy: &meshconfig.MeshConfig_InboundTrafficPolicy{},
	}
}

func TestConnectionPoolSettings(t *testing.T) {
	basicSettings := &networking.ConnectionPoolSettings{
		Http: &networking.ConnectionPoolSettings_HTTPSettings{
			Http1MaxPendingRequests:  1,
			Http2MaxRequests:         2,
			MaxRequestsPerConnection: 3,
			MaxRetries:               4,
		},
		Tcp: &networking.ConnectionPoolSettings_TCPSettings{
			MaxConnections: 1,
			ConnectTimeout: durationpb.New(2 * time.Second),
			TcpKeepalive: &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
				Probes:   3,
				Time:     durationpb.New(4 * time.Second),
				Interval: durationpb.New(5 * time.Second),
			},
			MaxConnectionDuration: durationpb.New(6 * time.Second),
		},
	}

	overrideSettings := &networking.ConnectionPoolSettings{
		Http: &networking.ConnectionPoolSettings_HTTPSettings{
			Http1MaxPendingRequests:  5,
			Http2MaxRequests:         6,
			MaxRequestsPerConnection: 7,
			MaxRetries:               8,
		},
		Tcp: &networking.ConnectionPoolSettings_TCPSettings{
			MaxConnections: 4,
			ConnectTimeout: durationpb.New(5 * time.Second),
			TcpKeepalive: &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
				Probes:   6,
				Time:     durationpb.New(7 * time.Second),
				Interval: durationpb.New(8 * time.Second),
			},
			MaxConnectionDuration: durationpb.New(9 * time.Second),
		},
	}

	cases := map[string]struct {
		sidecar  *networking.Sidecar
		destrule *networking.DestinationRule
		// port -> expected settings
		want map[string]*networking.ConnectionPoolSettings
	}{
		"no settings": {
			destrule: &networking.DestinationRule{
				Host: "*.example.org",
			},
			want: map[string]*networking.ConnectionPoolSettings{
				"outbound|8080||*.example.org": nil,
				"inbound|10001||":              nil,
				"inbound|10002||":              nil,
			},
		},
		"destination rule settings no sidecar": {
			destrule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: basicSettings,
				},
			},
			want: map[string]*networking.ConnectionPoolSettings{
				"outbound|8080||*.example.org": basicSettings,
				"inbound|10001||":              basicSettings,
				"inbound|10002||":              basicSettings,
			},
		},
		"sidecar settings no destination rule": {
			destrule: &networking.DestinationRule{
				Host: "*.example.org",
			},
			sidecar: &networking.Sidecar{
				InboundConnectionPool: basicSettings,
			},
			want: map[string]*networking.ConnectionPoolSettings{
				"outbound|8080||*.example.org": nil,
				"inbound|10001||":              basicSettings,
				"inbound|10002||":              basicSettings,
			},
		},
		"sidecar overrides destination rule for inbound listeners": {
			destrule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: basicSettings,
				},
			},
			sidecar: &networking.Sidecar{
				InboundConnectionPool: overrideSettings,
			},
			want: map[string]*networking.ConnectionPoolSettings{
				"outbound|8080||*.example.org": basicSettings,
				"inbound|10001||":              overrideSettings,
				"inbound|10002||":              overrideSettings,
			},
		},
		"sidecar per-port rules override top-level rules": {
			destrule: &networking.DestinationRule{
				Host: "*.example.org",
			},
			sidecar: &networking.Sidecar{
				InboundConnectionPool: basicSettings,
				Ingress: []*networking.IstioIngressListener{
					{
						Port: &networking.SidecarPort{
							Number:   10001,
							Protocol: string(protocol.HTTP),
						},
						ConnectionPool: overrideSettings,
					},
					{
						Port: &networking.SidecarPort{
							Number:   10002,
							Protocol: string(protocol.Unsupported),
						},
					},
				},
			},
			want: map[string]*networking.ConnectionPoolSettings{
				"outbound|8080||*.example.org": nil,
				"inbound|10001||":              overrideSettings,
				"inbound|10002||":              basicSettings,
			},
		},
		"sidecar per-port rules with destination rule default": {
			destrule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: basicSettings,
				},
			},
			sidecar: &networking.Sidecar{
				Ingress: []*networking.IstioIngressListener{
					{
						Port: &networking.SidecarPort{
							Number:   10001,
							Protocol: string(protocol.HTTP),
						},
						ConnectionPool: overrideSettings,
					},
					{
						Port: &networking.SidecarPort{
							Number:   10002,
							Protocol: string(protocol.Unsupported),
						},
					},
				},
			},
			want: map[string]*networking.ConnectionPoolSettings{
				"outbound|8080||*.example.org": basicSettings,
				"inbound|10001||":              overrideSettings,
				"inbound|10002||":              basicSettings,
			},
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			clusters := xdstest.ExtractClusters(buildTestClusters(clusterTest{
				t:               t,
				serviceHostname: "*.example.org",
				nodeType:        model.SidecarProxy,
				mesh:            testMesh(),
				destRule:        tt.destrule,
				sidecar:         tt.sidecar,
			}))

			for c, expected := range tt.want {
				cluster, ok := clusters[c]
				if !ok {
					names := make([]string, 0, len(clusters))
					for n := range clusters {
						names = append(names, n)
					}
					t.Fatalf("cluster %v not found; have: %s", c, strings.Join(names, ", "))
				}
				g.Expect(len(cluster.CircuitBreakers.Thresholds)).To(Equal(1))
				thresholds := cluster.CircuitBreakers.Thresholds[0]

				if expected == nil {
					g.Expect(thresholds).To(Equal(getDefaultCircuitBreakerThresholds()))
				} else {
					// verify TCP settings
					g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive).NotTo(BeNil())
					g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes.Value).
						To(Equal(expected.Tcp.TcpKeepalive.Probes))
					g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime.Value).
						To(Equal(uint32(expected.Tcp.TcpKeepalive.Time.Seconds)))
					g.Expect(cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval.Value).
						To(Equal(uint32(expected.Tcp.TcpKeepalive.Interval.Seconds)))
					g.Expect(cluster.ConnectTimeout).NotTo(BeNil())
					g.Expect(cluster.ConnectTimeout.Seconds).To(Equal(expected.Tcp.ConnectTimeout.Seconds))

					g.Expect(thresholds.MaxConnections).NotTo(BeNil())
					g.Expect(thresholds.MaxConnections.Value).To(Equal(uint32(expected.Tcp.MaxConnections)))

					// and HTTP settings
					g.Expect(thresholds.MaxPendingRequests.Value).To(Equal(uint32(expected.Http.Http1MaxPendingRequests)))
					g.Expect(thresholds.MaxRequests).NotTo(BeNil())
					g.Expect(thresholds.MaxRequests.Value).To(Equal(uint32(expected.Http.Http2MaxRequests)))
					g.Expect(cluster.TypedExtensionProtocolOptions).NotTo(BeNil())
					anyOptions := cluster.TypedExtensionProtocolOptions[v3.HttpProtocolOptionsType]
					g.Expect(anyOptions).NotTo(BeNil())
					httpProtocolOptions := &http.HttpProtocolOptions{}
					anyOptions.UnmarshalTo(httpProtocolOptions)
					g.Expect(httpProtocolOptions.CommonHttpProtocolOptions.MaxRequestsPerConnection.GetValue()).
						To(Equal(uint32(expected.Http.MaxRequestsPerConnection)))
					g.Expect(thresholds.MaxRetries).NotTo(BeNil())
					g.Expect(thresholds.MaxRetries.Value).To(Equal(uint32(expected.Http.MaxRetries)))
				}
			}
		})
	}
}

// TODO(liorlieberman) match the test with the changes after confirming the PR direction is LGTM
// func TestBuildClustersForInferencePoolServices(t *testing.T) {
// 	cases := []struct {
// 		testName             string
// 		clusterName          string
// 		proxyType            model.NodeType
// 		InferencePoolService bool
// 	}{
// 		// Add testcase for inbound clusters as well?
// 		{
// 			testName:             "InferencePool service should have lb_subset_config with the right selector",
// 			clusterName:          "outbound|8080||*.example.org",
// 			proxyType:            model.Router,
// 			InferencePoolService: true,
// 		},
// 		{
// 			testName:             "Regular service should NOT have lb_subset_config",
// 			clusterName:          "outbound|8080||*.example.org",
// 			proxyType:            model.Router,
// 			InferencePoolService: false,
// 		},
// 		{
// 			testName:             "Sidecar proxy should not have config",
// 			clusterName:          "outbound|8080||*.example.org",
// 			proxyType:            model.SidecarProxy,
// 			InferencePoolService: true,
// 		},
// 	}
// 	for _, tc := range cases {
// 		t.Run(tc.testName, func(t *testing.T) {
// 			g := NewWithT(t)
// 			clusters := buildTestClusters(clusterTest{
// 				t:                    t,
// 				serviceHostname:      "*.example.org",
// 				nodeType:             tc.proxyType,
// 				mesh:                 testMesh(),
// 				istioVersion:         model.MaxIstioVersion,
// 				inferencePoolCluster: tc.InferencePoolService,
// 			})
// 			c := xdstest.ExtractCluster("outbound|8080||*.example.org", clusters)
// 			if !tc.InferencePoolService || tc.InferencePoolService && tc.proxyType != model.Router {
// 				g.Expect(c.LbSubsetConfig).To(BeNil())
// 			} else if tc.InferencePoolService {
// 				g.Expect(c.LbSubsetConfig).NotTo(BeNil())
// 				g.Expect(c.LbSubsetConfig.SubsetSelectors).NotTo(BeEmpty())
// 				g.Expect(c.LbSubsetConfig.SubsetSelectors[0].Keys).To(Equal([]string{"x-gateway-destination-endpoint"}))
// 			}
// 		})
// 	}
// }

func TestCommonHttpProtocolOptions(t *testing.T) {
	cases := []struct {
		clusterName           string
		useDownStreamProtocol bool
		proxyType             model.NodeType
		clusters              int
	}{
		{
			clusterName:           "outbound|8080||*.example.org",
			useDownStreamProtocol: false,
			proxyType:             model.SidecarProxy,
			clusters:              7,
		},
		{
			clusterName:           "inbound|10001||",
			useDownStreamProtocol: false,
			proxyType:             model.SidecarProxy,
			clusters:              7,
		},
		{
			clusterName:           "outbound|9090||*.example.org",
			useDownStreamProtocol: true,
			proxyType:             model.SidecarProxy,
			clusters:              7,
		},
		{
			clusterName:           "inbound|10002||",
			useDownStreamProtocol: true,
			proxyType:             model.SidecarProxy,
			clusters:              7,
		},
		{
			clusterName:           "outbound|8080||*.example.org",
			useDownStreamProtocol: true,
			proxyType:             model.Router,
			clusters:              3,
		},
	}
	settings := &networking.ConnectionPoolSettings{
		Http: &networking.ConnectionPoolSettings_HTTPSettings{
			Http1MaxPendingRequests: 1,
			IdleTimeout:             &durationpb.Duration{Seconds: 15},
		},
	}

	test.SetForTest(t, &features.FilterGatewayClusterConfig, false)
	for _, tc := range cases {
		settingsName := "default"
		if settings != nil {
			settingsName = "override"
		}
		testName := fmt.Sprintf("%s-%s-%s", tc.clusterName, settingsName, tc.proxyType)
		t.Run(testName, func(t *testing.T) {
			g := NewWithT(t)
			clusters := xdstest.ExtractClusters(buildTestClusters(clusterTest{
				t: t, serviceHostname: "*.example.org", nodeType: tc.proxyType, mesh: testMesh(),
				destRule: &networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						ConnectionPool: settings,
					},
				},
			}))
			g.Expect(len(clusters)).To(Equal(tc.clusters))
			c := clusters[tc.clusterName]

			anyOptions := c.TypedExtensionProtocolOptions[v3.HttpProtocolOptionsType]
			httpProtocolOptions := &http.HttpProtocolOptions{}
			if anyOptions != nil {
				anyOptions.UnmarshalTo(httpProtocolOptions)
			}

			if tc.useDownStreamProtocol && tc.proxyType == model.SidecarProxy {
				if httpProtocolOptions.GetUseDownstreamProtocolConfig() == nil {
					t.Errorf("Expected cluster to use downstream protocol but got %v", httpProtocolOptions)
				}
			} else {
				if httpProtocolOptions.GetUseDownstreamProtocolConfig() != nil {
					t.Errorf("Expected cluster to not to use downstream protocol but got %v", httpProtocolOptions)
				}
			}

			// Verify that the values were set correctly.
			g.Expect(httpProtocolOptions.CommonHttpProtocolOptions.IdleTimeout).To(Not(BeNil()))
			g.Expect(httpProtocolOptions.CommonHttpProtocolOptions.IdleTimeout).To(Equal(durationpb.New(time.Duration(15000000000))))
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
	mesh              *meshconfig.MeshConfig
	destRule          proto.Message
	sidecar           *networking.Sidecar
	peerAuthn         *authn_beta.PeerAuthentication
	externalService   bool

	meta         *model.NodeMetadata
	istioVersion *model.IstioVersion
	proxyIps     []string

	inferencePoolCluster bool
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

	service := &model.Service{
		Hostname:     host.Name(c.serviceHostname),
		Ports:        servicePort,
		Resolution:   c.serviceResolution,
		MeshExternal: c.externalService,
		Attributes: model.ServiceAttributes{
			Namespace: TestServiceNamespace,
			Labels:    map[string]string{},
		},
	}

	if c.inferencePoolCluster {
		service.Attributes.Labels[constants.InternalServiceSemantics] = "inferencepool"
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort[0],
			Endpoint: &model.IstioEndpoint{
				Addresses:       []string{"6.6.6.6"},
				ServicePortName: servicePort[0].Name,
				EndpointPort:    10001,
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
				Addresses:       []string{"6.6.6.6"},
				ServicePortName: servicePort[0].Name,
				EndpointPort:    10001,
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
				Addresses:       []string{"6.6.6.6"},
				ServicePortName: servicePort[0].Name,
				EndpointPort:    10001,
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
				ServicePortName: servicePort[1].Name,
				Addresses:       []string{"6.6.6.6"},
				EndpointPort:    10002,
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
	if c.sidecar != nil {
		configs = append(configs, config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.Sidecar,
				Name:             "default",
			},
			Spec: c.sidecar,
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
		MeshConfig: c.mesh,
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
		{
			"use ring hash",
			2,
			2,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			test.SetForTest(t, &features.FilterGatewayClusterConfig, false)

			c := xdstest.ExtractCluster("outbound|8080||*.example.org",
				buildTestClusters(clusterTest{
					t: t, serviceHostname: "*.example.org", nodeType: model.Router, mesh: testMesh(),
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
												Ttl:  &durationpb.Duration{Nanos: 100},
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
			g.Expect(c.ConnectTimeout).To(Equal(durationpb.New(time.Duration(10000000001))))
		})
	}
}

func TestApplyRingHashLoadBalancer(t *testing.T) {
	cases := []struct {
		name     string
		lb       *networking.LoadBalancerSettings
		validate func(c *cluster.Cluster) error
	}{
		{
			"default consistent hash settings",
			&networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{},
				},
			},
			func(c *cluster.Cluster) error {
				if c.LbPolicy != cluster.Cluster_RING_HASH {
					return fmt.Errorf("unexpected load balancer. expected: %v, got: %v", cluster.Cluster_RING_HASH, c.LbPolicy)
				}
				if c.GetRingHashLbConfig().GetMinimumRingSize().Value != 1024 {
					return fmt.Errorf("unexpected minimum ring size. expected: %v, got: %v", 1024, c.GetRingHashLbConfig().GetMinimumRingSize().Value)
				}
				return nil
			},
		},
		{
			"consistent hash settings with deprecated minring size",
			&networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						MinimumRingSize: 10,
					},
				},
			},
			func(c *cluster.Cluster) error {
				if c.LbPolicy != cluster.Cluster_RING_HASH {
					return fmt.Errorf("unexpected load balancer. expected: %v, got: %v", cluster.Cluster_RING_HASH, c.LbPolicy)
				}
				if c.GetRingHashLbConfig().GetMinimumRingSize().Value != 10 {
					return fmt.Errorf("unexpected minimum ring size. expected: %v, got: %v", 10, c.GetRingHashLbConfig().GetMinimumRingSize().Value)
				}
				return nil
			},
		},
		{
			"consistent hash settings with Maglev",
			&networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashAlgorithm: &networking.LoadBalancerSettings_ConsistentHashLB_Maglev{
							Maglev: &networking.LoadBalancerSettings_ConsistentHashLB_MagLev{},
						},
					},
				},
			},
			func(c *cluster.Cluster) error {
				if c.LbPolicy != cluster.Cluster_MAGLEV {
					return fmt.Errorf("unexpected load balancer. expected: %v, got: %v", cluster.Cluster_MAGLEV, c.LbPolicy)
				}
				if c.GetMaglevLbConfig() != nil {
					return fmt.Errorf("unexpected maglev config. expected: %v, got: %v", nil, c.GetMaglevLbConfig())
				}
				return nil
			},
		},
		{
			"consistent hash settings with Maglev and table size defined",
			&networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashAlgorithm: &networking.LoadBalancerSettings_ConsistentHashLB_Maglev{
							Maglev: &networking.LoadBalancerSettings_ConsistentHashLB_MagLev{
								TableSize: 10,
							},
						},
					},
				},
			},
			func(c *cluster.Cluster) error {
				if c.LbPolicy != cluster.Cluster_MAGLEV {
					return fmt.Errorf("unexpected load balancer. expected: %v, got: %v", cluster.Cluster_MAGLEV, c.LbPolicy)
				}
				if c.GetMaglevLbConfig().TableSize.Value != 10 {
					return fmt.Errorf("unexpected maglev table size. expected: %v, got: %v", 10, c.GetMaglevLbConfig().TableSize.Value)
				}
				return nil
			},
		},
		{
			"consistent hash settings with Ringhash",
			&networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashAlgorithm: &networking.LoadBalancerSettings_ConsistentHashLB_RingHash_{
							RingHash: &networking.LoadBalancerSettings_ConsistentHashLB_RingHash{},
						},
					},
				},
			},
			func(c *cluster.Cluster) error {
				if c.LbPolicy != cluster.Cluster_RING_HASH {
					return fmt.Errorf("unexpected load balancer. expected: %v, got: %v", cluster.Cluster_RING_HASH, c.LbPolicy)
				}
				if c.GetRingHashLbConfig() != nil {
					return fmt.Errorf("unexpected ring hash config. expected: %v, got: %v", nil, c.GetRingHashLbConfig())
				}
				return nil
			},
		},
		{
			"consistent hash settings with RingHash and min ringsize size defined",
			&networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashAlgorithm: &networking.LoadBalancerSettings_ConsistentHashLB_RingHash_{
							RingHash: &networking.LoadBalancerSettings_ConsistentHashLB_RingHash{
								MinimumRingSize: 10,
							},
						},
					},
				},
			},
			func(c *cluster.Cluster) error {
				if c.LbPolicy != cluster.Cluster_RING_HASH {
					return fmt.Errorf("unexpected load balancer. expected: %v, got: %v", cluster.Cluster_RING_HASH, c.LbPolicy)
				}
				if c.GetRingHashLbConfig().MinimumRingSize.Value != 10 {
					return fmt.Errorf("unexpected min ring hash size. expected: %v, got: %v", 10, c.GetRingHashLbConfig().MinimumRingSize.Value)
				}
				return nil
			},
		},
		{
			"consistent hash settings with RingHash with min ringsize size defined along with deprecated minring size",
			&networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashAlgorithm: &networking.LoadBalancerSettings_ConsistentHashLB_RingHash_{
							RingHash: &networking.LoadBalancerSettings_ConsistentHashLB_RingHash{
								MinimumRingSize: 10,
							},
						},
						MinimumRingSize: 1000,
					},
				},
			},
			func(c *cluster.Cluster) error {
				if c.LbPolicy != cluster.Cluster_RING_HASH {
					return fmt.Errorf("unexpected load balancer. expected: %v, got: %v", cluster.Cluster_RING_HASH, c.LbPolicy)
				}
				if c.GetRingHashLbConfig().MinimumRingSize.Value != 10 {
					return fmt.Errorf("unexpected min ring hash size. expected: %v, got: %v", 10, c.GetRingHashLbConfig().MinimumRingSize.Value)
				}
				return nil
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := &cluster.Cluster{}
			ApplyRingHashLoadBalancer(c, tt.lb)
			if err := tt.validate(c); err != nil {
				t.Errorf("%s failed with error: %v", tt.name, err)
			}
		})
	}
}

func withClusterLocalHosts(m *meshconfig.MeshConfig, hosts ...string) *meshconfig.MeshConfig { // nolint:interfacer
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
		mesh:              testMesh(),
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
		g.Expect(rootSdsConfig.GetName()).To(Equal("file-root:/clientRootCertFromNodeMetadata.pem"))

		certSdsConfig := tlsContext.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()
		g.Expect(certSdsConfig).To(HaveLen(1))
		g.Expect(certSdsConfig[0].GetName()).To(Equal("file-cert:/clientCertFromNodeMetadata.pem~/clientKeyFromNodeMetadata.pem"))
	}
}

func buildSniTestClustersForSidecar(t *testing.T, sniValue string) []*cluster.Cluster {
	return buildSniTestClustersWithMetadata(t, sniValue, model.SidecarProxy, &model.NodeMetadata{})
}

func buildSniDnatTestClustersForGateway(t *testing.T, sniValue string) []*cluster.Cluster {
	return buildSniTestClustersWithMetadata(t, sniValue, model.Router, &model.NodeMetadata{})
}

func buildSniTestClustersWithMetadata(t testing.TB, sniValue string, typ model.NodeType, meta *model.NodeMetadata) []*cluster.Cluster {
	return buildTestClusters(clusterTest{
		t: t, serviceHostname: "foo.example.org", nodeType: typ, mesh: testMesh(),
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
	m := testMesh()
	if configType != None {
		m.TcpKeepalive = &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
			Time: &durationpb.Duration{
				Seconds: MeshWideTCPKeepaliveSeconds,
				Nanos:   0,
			},
		}
	}

	// Set DestinationRule override.
	var destinationRuleTCPKeepalive *networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive
	if configType == DestinationRule {
		destinationRuleTCPKeepalive = &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
			Time: &durationpb.Duration{
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

	clusters := buildTestClusters(clusterTest{t: t, serviceHostname: "*.example.org", nodeType: model.SidecarProxy, mesh: testMesh(), destRule: destRule})

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
			g.Expect(dr.GetStringValue()).To(Equal("/apis/networking.istio.io/v1/namespaces//destination-rule/acme"))
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
			g.Expect(dr.GetStringValue()).To(Equal("/apis/networking.istio.io/v1/namespaces//destination-rule/acme"))
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
				locality: &core.Locality{}, mesh: testMesh(),
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
				Consecutive_5XxErrors:    &wrappers.UInt32Value{Value: 4},
				ConsecutiveGatewayErrors: &wrappers.UInt32Value{Value: 3},
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
				ConsecutiveGatewayErrors: &wrappers.UInt32Value{Value: 3},
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
				Consecutive_5XxErrors: &wrappers.UInt32Value{Value: 3},
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
				ConsecutiveGatewayErrors: &wrappers.UInt32Value{Value: 0},
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
				Consecutive_5XxErrors: &wrappers.UInt32Value{Value: 0},
			},
			&cluster.OutlierDetection{
				Consecutive_5Xx:          &wrappers.UInt32Value{Value: 0},
				EnforcingConsecutive_5Xx: &wrappers.UInt32Value{Value: 0},
				EnforcingSuccessRate:     &wrappers.UInt32Value{Value: 0},
			},
		},
		{
			"Local origin errors is enabled",
			&networking.OutlierDetection{
				SplitExternalLocalOriginErrors: true,
				ConsecutiveLocalOriginFailures: &wrappers.UInt32Value{Value: 10},
			},
			&cluster.OutlierDetection{
				EnforcingSuccessRate:                   &wrappers.UInt32Value{Value: 0},
				SplitExternalLocalOriginErrors:         true,
				ConsecutiveLocalOriginFailure:          &wrappers.UInt32Value{Value: 10},
				EnforcingLocalOriginSuccessRate:        &wrappers.UInt32Value{Value: 0},
				EnforcingConsecutiveLocalOriginFailure: &wrappers.UInt32Value{Value: 100},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := xdstest.ExtractCluster("outbound|8080||*.example.org",
				buildTestClusters(clusterTest{
					t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
					locality: &core.Locality{}, mesh: testMesh(),
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

	statConfigMesh := &meshconfig.MeshConfig{
		ConnectTimeout: &durationpb.Duration{
			Seconds: 10,
			Nanos:   1,
		},
		InboundTrafficPolicy: &meshconfig.MeshConfig_InboundTrafficPolicy{},
		EnableAutoMtls: &wrappers.BoolValue{
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
	g.Expect(xdstest.ExtractCluster("outbound|8080||*.example.org", clusters).AltStatName).To(Equal("*.example.org_default_8080;"))
	g.Expect(xdstest.ExtractCluster("inbound|10001||", clusters).AltStatName).To(Equal("LocalService_*.example.org;"))
}

func TestDuplicateClusters(t *testing.T) {
	buildTestClusters(clusterTest{
		t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
		locality: &core.Locality{}, mesh: testMesh(),
		destRule: &networking.DestinationRule{
			Host: "*.example.org",
		},
	})
}

func TestSidecarLocalityLB(t *testing.T) {
	g := NewWithT(t)
	// Distribute locality loadbalancing setting
	mesh := testMesh()
	mesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{
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
			}, mesh: mesh,
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
	// failover locality loadbalancing setting
	mesh = testMesh()
	mesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{Failover: []*networking.LocalityLoadBalancerSetting_Failover{}}

	c = xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.SidecarProxy,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: mesh,
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
	mesh := testMesh()
	// Distribute locality loadbalancing setting
	mesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{
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
			}, mesh: mesh,
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

	// Test distribute
	// Distribute locality loadbalancing setting
	mesh := testMesh()
	mesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{
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

	test.SetForTest(t, &features.FilterGatewayClusterConfig, false)

	c := xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.Router,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: mesh,
			destRule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 10,
					},
				},
			},
			meta: &model.NodeMetadata{},
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
	mesh = testMesh()
	mesh.LocalityLbSetting = &networking.LocalityLoadBalancerSetting{Failover: []*networking.LocalityLoadBalancerSetting_Failover{}}

	c = xdstest.ExtractCluster("outbound|8080||*.example.org",
		buildTestClusters(clusterTest{
			t: t, serviceHostname: "*.example.org", serviceResolution: model.DNSLB, nodeType: model.Router,
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			}, mesh: mesh,
			destRule: &networking.DestinationRule{
				Host: "*.example.org",
				TrafficPolicy: &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 10,
					},
				},
			}, // peerAuthn
			meta: &model.NodeMetadata{},
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
		Hostname:   host.Name("*.example.org"),
		Ports:      model.PortList{servicePort},
		Resolution: model.ClientSideLB,
	}

	instances := []model.ServiceTarget{
		{
			Service: service,
			Port: model.ServiceInstancePort{
				ServicePort: servicePort,
				TargetPort:  7443,
			},
		},
	}

	ingress := &networking.IstioIngressListener{
		CaptureMode:     networking.CaptureMode_NONE,
		DefaultEndpoint: "127.0.0.1:7020",
		Port: &networking.SidecarPort{
			Number:   7443,
			Name:     "grpc-core",
			Protocol: "GRPC",
		},
	}
	svc := findOrCreateService(instances, ingress, "sidecar", "sidecarns")
	if svc == nil || svc.Hostname.Matches("sidecar.sidecarns") {
		t.Fatal("Expected to return a valid instance, but got nil/default instance")
	}
	if !reflect.DeepEqual(svc, service) {
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
		mesh:              testMesh(),
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

// Deprecated: use TestWarmup
func TestSlowStartConfig(t *testing.T) {
	g := NewWithT(t)
	testcases := []struct {
		name                string
		lbType              networking.LoadBalancerSettings_SimpleLB
		enableSlowStartMode bool
	}{
		{name: "roundrobin", lbType: networking.LoadBalancerSettings_ROUND_ROBIN, enableSlowStartMode: true},
		{name: "leastrequest", lbType: networking.LoadBalancerSettings_LEAST_REQUEST, enableSlowStartMode: true},
		{name: "passthrough", lbType: networking.LoadBalancerSettings_PASSTHROUGH, enableSlowStartMode: true},
		{name: "roundrobin-without-warmup", lbType: networking.LoadBalancerSettings_ROUND_ROBIN, enableSlowStartMode: false},
		{name: "leastrequest-without-warmup", lbType: networking.LoadBalancerSettings_LEAST_REQUEST, enableSlowStartMode: false},
		{name: "empty lb type", enableSlowStartMode: true},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			clusters := buildTestClusters(clusterTest{
				t:               t,
				serviceHostname: test.name,
				nodeType:        model.SidecarProxy,
				mesh:            testMesh(),
				destRule: &networking.DestinationRule{
					Host:          test.name,
					TrafficPolicy: getSlowStartTrafficPolicy(test.enableSlowStartMode, test.lbType),
				},
			})

			c := xdstest.ExtractCluster("outbound|8080||"+test.name,
				clusters)

			if !test.enableSlowStartMode {
				g.Expect(c.GetLbConfig()).To(BeNil())
			} else {
				switch c.LbPolicy {
				case cluster.Cluster_ROUND_ROBIN:
					g.Expect(c.GetRoundRobinLbConfig().GetSlowStartConfig().GetSlowStartWindow().Seconds).To(Equal(int64(15)))
				case cluster.Cluster_LEAST_REQUEST:
					g.Expect(c.GetLeastRequestLbConfig().GetSlowStartConfig().GetSlowStartWindow().Seconds).To(Equal(int64(15)))
				default:
					g.Expect(c.GetLbConfig()).To(BeNil())
				}
			}
		})
	}
}

// Deprecated: use TestWarmup
func getSlowStartTrafficPolicy(slowStartEnabled bool, lbType networking.LoadBalancerSettings_SimpleLB) *networking.TrafficPolicy {
	var warmupDurationSecs *durationpb.Duration
	if slowStartEnabled {
		warmupDurationSecs = &durationpb.Duration{Seconds: 15}
	}
	return &networking.TrafficPolicy{
		LoadBalancer: &networking.LoadBalancerSettings{
			LbPolicy: &networking.LoadBalancerSettings_Simple{
				Simple: lbType,
			},
			WarmupDurationSecs: warmupDurationSecs,
		},
	}
}

func TestWarmup(t *testing.T) {
	warmupConfig := &networking.WarmupConfiguration{
		Duration:       &durationpb.Duration{Seconds: 60},
		MinimumPercent: &wrappers.DoubleValue{Value: 2},
		Aggression:     &wrappers.DoubleValue{Value: 5.5},
	}
	g := NewWithT(t)
	testcases := []struct {
		name          string
		lbType        networking.LoadBalancerSettings_SimpleLB
		warmup        *networking.WarmupConfiguration
		defaultConfig bool
	}{
		{name: "roundrobin", lbType: networking.LoadBalancerSettings_ROUND_ROBIN, warmup: warmupConfig, defaultConfig: false},
		{name: "leastrequest", lbType: networking.LoadBalancerSettings_LEAST_REQUEST, warmup: warmupConfig, defaultConfig: false},
		{name: "passthrough", lbType: networking.LoadBalancerSettings_PASSTHROUGH, warmup: warmupConfig, defaultConfig: false},
		{name: "roundrobin without warmup", lbType: networking.LoadBalancerSettings_ROUND_ROBIN, warmup: nil, defaultConfig: false},
		{name: "leastrequest without warmup", lbType: networking.LoadBalancerSettings_LEAST_REQUEST, warmup: nil, defaultConfig: false},
		{name: "empty lb type", warmup: warmupConfig, defaultConfig: false},
		{
			name:   "roundrobin with default values",
			lbType: networking.LoadBalancerSettings_ROUND_ROBIN,
			warmup: &networking.WarmupConfiguration{
				Duration: &durationpb.Duration{Seconds: 60},
			},
			defaultConfig: true,
		},
		{
			name:   "leastrequest with default values",
			lbType: networking.LoadBalancerSettings_LEAST_REQUEST,
			warmup: &networking.WarmupConfiguration{
				Duration: &durationpb.Duration{Seconds: 60},
			},
			defaultConfig: true,
		},
		{
			name: "empty lb type with default values",
			warmup: &networking.WarmupConfiguration{
				Duration: &durationpb.Duration{Seconds: 60},
			},
			defaultConfig: true,
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			clusters := buildTestClusters(clusterTest{
				t:               t,
				serviceHostname: test.name,
				nodeType:        model.SidecarProxy,
				mesh:            testMesh(),
				destRule: &networking.DestinationRule{
					Host: test.name,
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: test.lbType,
							},
							Warmup: test.warmup,
						},
					},
				},
			})

			c := xdstest.ExtractCluster("outbound|8080||"+test.name,
				clusters)

			if test.warmup == nil {
				g.Expect(c.GetLbConfig()).To(BeNil())
			} else {
				// The warmup configuration only specify the duration window, we expect default values for aggression and minWeightPercent
				switch test.defaultConfig {
				case true:
					switch c.LbPolicy {
					case cluster.Cluster_ROUND_ROBIN:
						g.Expect(c.GetRoundRobinLbConfig().GetSlowStartConfig().GetSlowStartWindow().Seconds).To(Equal(int64(60)))
						g.Expect(c.GetRoundRobinLbConfig().GetSlowStartConfig().GetMinWeightPercent().GetValue()).To(Equal(float64(10)))
						g.Expect(c.GetRoundRobinLbConfig().GetSlowStartConfig().GetAggression().GetDefaultValue()).To(Equal(float64(1.0)))
					case cluster.Cluster_LEAST_REQUEST:
						g.Expect(c.GetLeastRequestLbConfig().GetSlowStartConfig().GetSlowStartWindow().Seconds).To(Equal(int64(60)))
						g.Expect(c.GetLeastRequestLbConfig().GetSlowStartConfig().GetMinWeightPercent().GetValue()).To(Equal(float64(10)))
						g.Expect(c.GetLeastRequestLbConfig().GetSlowStartConfig().GetAggression().GetDefaultValue()).To(Equal(float64(1.0)))
					default:
						g.Expect(c.GetLbConfig()).To(BeNil())
					}

				case false:
					// The warmup configuration provides all parameters
					switch c.LbPolicy {
					case cluster.Cluster_ROUND_ROBIN:
						g.Expect(c.GetRoundRobinLbConfig().GetSlowStartConfig().GetSlowStartWindow().Seconds).To(Equal(int64(60)))
						g.Expect(c.GetRoundRobinLbConfig().GetSlowStartConfig().GetMinWeightPercent().GetValue()).To(Equal(float64(2)))
						g.Expect(c.GetRoundRobinLbConfig().GetSlowStartConfig().GetAggression().GetDefaultValue()).To(Equal(float64(5.5)))
					case cluster.Cluster_LEAST_REQUEST:
						g.Expect(c.GetLeastRequestLbConfig().GetSlowStartConfig().GetSlowStartWindow().Seconds).To(Equal(int64(60)))
						g.Expect(c.GetLeastRequestLbConfig().GetSlowStartConfig().GetMinWeightPercent().GetValue()).To(Equal(float64(2)))
						g.Expect(c.GetLeastRequestLbConfig().GetSlowStartConfig().GetAggression().GetDefaultValue()).To(Equal(float64(5.5)))
					default:
						g.Expect(c.GetLbConfig()).To(BeNil())
					}
				}
			}
		})
	}
}

func TestClusterDiscoveryTypeAndLbPolicyPassthrough(t *testing.T) {
	g := NewWithT(t)

	clusters := buildTestClusters(clusterTest{
		t:                 t,
		serviceHostname:   "*.example.org",
		serviceResolution: model.ClientSideLB,
		nodeType:          model.SidecarProxy,
		mesh:              testMesh(),
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
		Hostname:   host.Name("backend.default.svc.cluster.local"),
		Ports:      model.PortList{servicePort},
		Resolution: model.Passthrough,
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"1.1.1.1"},
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
				TrackRemaining:     true,
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
				TrackRemaining:     true,
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

func TestInboundClustersPassThroughBindIPs(t *testing.T) {
	servicePort := &model.Port{
		Name:     "default",
		Port:     80,
		Protocol: protocol.HTTP,
	}

	service := &model.Service{
		Hostname:   host.Name("backend.default.svc.cluster.local"),
		Ports:      model.PortList{servicePort},
		Resolution: model.Passthrough,
	}
	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"1.1.1.1"},
				EndpointPort: 10001,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"2001:1::1"},
				EndpointPort: 10001,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"1.1.1.1", "2001:1::1"},
				EndpointPort: 10001,
			},
		},
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"2.2.2.2", "2001:1::2"},
				EndpointPort: 10001,
			},
		},
	}

	inboundFilter := func(c *cluster.Cluster) bool {
		return strings.HasPrefix(c.Name, "inbound|")
	}

	cases := []struct {
		name                 string
		proxy                *model.Proxy
		dualStack            bool
		expectedSrcAddr      string
		expectedExtraSrcAddr string
	}{
		{
			name:                 "all ipv4, dual stack disabled",
			proxy:                getProxy(),
			dualStack:            false,
			expectedSrcAddr:      InboundPassthroughBindIpv4,
			expectedExtraSrcAddr: "",
		},
		{
			name:                 "all ipv4, dual stack enabled",
			proxy:                getProxy(),
			dualStack:            true,
			expectedSrcAddr:      InboundPassthroughBindIpv4,
			expectedExtraSrcAddr: "",
		},
		{
			name:                 "all ipv6, dual stack disabled",
			proxy:                getIPv6Proxy(),
			dualStack:            false,
			expectedSrcAddr:      InboundPassthroughBindIpv6,
			expectedExtraSrcAddr: "",
		},
		{
			name:                 "all ipv6, dual stack enabled",
			proxy:                getIPv6Proxy(),
			dualStack:            true,
			expectedSrcAddr:      InboundPassthroughBindIpv6,
			expectedExtraSrcAddr: "",
		},
		{
			name:                 "ipv4 and ipv6, dual stack disabled",
			proxy:                &dualStackProxy,
			dualStack:            false,
			expectedSrcAddr:      InboundPassthroughBindIpv4,
			expectedExtraSrcAddr: "",
		},
		{
			name:                 "ipv4 and ipv6, dual stack enabled",
			proxy:                &dualStackProxy,
			dualStack:            true,
			expectedSrcAddr:      InboundPassthroughBindIpv4,
			expectedExtraSrcAddr: InboundPassthroughBindIpv6,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableDualStack, c.dualStack)
			g := NewWithT(t)
			cg := NewConfigGenTest(t, TestOptions{
				Services:  []*model.Service{service},
				Instances: instances,
				Configs:   []config.Config{},
			})
			clusters := cg.Clusters(cg.SetupProxy(c.proxy))
			xdstest.ValidateClusters(t, clusters)

			clusters = xdstest.FilterClusters(clusters, inboundFilter)
			g.Expect(len(clusters)).ShouldNot(Equal(0))

			for _, cluster := range clusters {
				g.Expect(cluster.UpstreamBindConfig.SourceAddress.Address).To(Equal(c.expectedSrcAddr))
				if c.expectedExtraSrcAddr != "" {
					g.Expect(len(cluster.UpstreamBindConfig.ExtraSourceAddresses)).To(Equal(1))
					g.Expect(cluster.UpstreamBindConfig.ExtraSourceAddresses[0].Address.Address).To(Equal(c.expectedExtraSrcAddr))
				} else {
					g.Expect(len(cluster.UpstreamBindConfig.ExtraSourceAddresses)).To(Equal(0))
				}

			}
		})
	}
}

func TestInboundClustersLocalhostDefaultEndpoint(t *testing.T) {
	ipv4Proxy := getProxy()
	ipv6Proxy := getIPv6Proxy()
	dsProxy := model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1", "1111:2222::1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
		},
		ConfigNamespace: "not-default",
	}

	inboundFilter := func(c *cluster.Cluster) bool {
		return strings.HasPrefix(c.Name, "inbound|")
	}

	cases := []struct {
		name            string
		proxy           *model.Proxy
		defaultEndpoint string
		expectedAddr    string
		expectedPort    uint32
	}{
		// ipv4 use cases
		{
			name:            "ipv4 host: defaultEndpoint set to 127.0.0.1:7073",
			proxy:           ipv4Proxy,
			defaultEndpoint: "127.0.0.1:7073",
			expectedAddr:    "127.0.0.1",
			expectedPort:    7073,
		},
		{
			name:            "ipv4 host: defaultEndpoint set to [::1]:7073",
			proxy:           ipv4Proxy,
			defaultEndpoint: "[::1]:7073",
			expectedAddr:    "127.0.0.1",
			expectedPort:    7073,
		},
		// ipv6 use cases
		{
			name:            "ipv6 host: defaultEndpoint set to 127.0.0.1:7073",
			proxy:           ipv6Proxy,
			defaultEndpoint: "127.0.0.1:7073",
			expectedAddr:    "::1",
			expectedPort:    7073,
		},
		{
			name:            "ipv6 host: defaultEndpoint set to [::1]:7073",
			proxy:           ipv6Proxy,
			defaultEndpoint: "[::1]:7073",
			expectedAddr:    "::1",
			expectedPort:    7073,
		}, // dual-stack use cases
		{
			name:            "dual-stack host: defaultEndpoint set to 127.0.0.1:7073",
			proxy:           &dsProxy,
			defaultEndpoint: "127.0.0.1:7073",
			expectedAddr:    "127.0.0.1",
			expectedPort:    7073,
		},
		{
			name:            "dual-stack host: defaultEndpoint set to [::1]:7073",
			proxy:           &dsProxy,
			defaultEndpoint: "[::1]:7073",
			expectedAddr:    "::1",
			expectedPort:    7073,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			cg := NewConfigGenTest(t, TestOptions{})
			proxy := cg.SetupProxy(c.proxy)
			proxy.Metadata.InterceptionMode = model.InterceptionNone
			proxy.SidecarScope = &model.SidecarScope{
				Sidecar: &networking.Sidecar{
					Ingress: []*networking.IstioIngressListener{
						{
							CaptureMode:     networking.CaptureMode_NONE,
							DefaultEndpoint: c.defaultEndpoint,
							Port: &networking.SidecarPort{
								Number:   7443,
								Name:     "http",
								Protocol: "HTTP",
							},
						},
					},
				},
			}

			clusters := cg.Clusters(proxy)
			xdstest.ValidateClusters(t, clusters)

			clusters = xdstest.FilterClusters(clusters, inboundFilter)
			g.Expect(len(clusters)).ShouldNot(Equal(0))

			for _, cluster := range clusters {
				socket := cluster.GetLoadAssignment().GetEndpoints()[0].LbEndpoints[0].GetEndpoint().GetAddress().GetSocketAddress()
				g.Expect(socket.GetAddress()).To(Equal(c.expectedAddr))
				g.Expect(socket.GetPortValue()).To(Equal(c.expectedPort))
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
		Hostname:   host.Name("redis.com"),
		Ports:      model.PortList{servicePort},
		Resolution: model.Passthrough,
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
			lbType:        defaultLBAlgorithm(),
			discoveryType: cluster.Cluster_EDS,
		},
		{
			name:          "redis disabled passthrough",
			redisEnabled:  false,
			resolution:    model.Passthrough,
			lbType:        defaultLBAlgorithm(),
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
			test.SetForTest(t, &features.FilterGatewayClusterConfig, false)
			test.SetForTest(t, &features.EnableRedisFilter, tt.redisEnabled)
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

	mesh := testMesh()
	mesh.EnableAutoMtls.Value = true

	clusters := buildTestClusters(clusterTest{t: t, serviceHostname: TestServiceNHostname, nodeType: model.SidecarProxy, mesh: mesh, destRule: destRule})

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

	mesh := testMesh()
	mesh.EnableAutoMtls.Value = true

	clusters := buildTestClusters(clusterTest{
		t:               t,
		serviceHostname: TestServiceNHostname,
		nodeType:        model.SidecarProxy,
		mesh:            mesh,
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
		name                               string
		lbSettings                         *networking.LoadBalancerSettings
		discoveryType                      cluster.Cluster_DiscoveryType
		port                               *model.Port
		sendUnhealthyEndpoints             bool
		expectedLbPolicy                   cluster.Cluster_LbPolicy
		expectedLocalityWeightedConfig     bool
		expectClusterLoadAssignmenttoBeNil bool
	}{
		{
			name:                           "ORIGINAL_DST discovery type is a no op",
			discoveryType:                  cluster.Cluster_ORIGINAL_DST,
			expectedLbPolicy:               cluster.Cluster_CLUSTER_PROVIDED,
			expectedLocalityWeightedConfig: false,
		},
		{
			name:                           "redis protocol",
			discoveryType:                  cluster.Cluster_EDS,
			port:                           &model.Port{Protocol: protocol.Redis},
			expectedLbPolicy:               cluster.Cluster_MAGLEV,
			expectedLocalityWeightedConfig: false,
		},
		{
			name: "Loadbalancer has distribute",
			lbSettings: &networking.LoadBalancerSettings{
				LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
					Enabled: &wrappers.BoolValue{Value: true},
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
			expectedLbPolicy:               defaultLBAlgorithm(),
			expectedLocalityWeightedConfig: true,
		},
		{
			name:          "DNS cluster with PASSTHROUGH in DR",
			discoveryType: cluster.Cluster_STRICT_DNS,
			lbSettings: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{Simple: networking.LoadBalancerSettings_PASSTHROUGH},
			},
			expectedLbPolicy:                   cluster.Cluster_CLUSTER_PROVIDED,
			expectClusterLoadAssignmenttoBeNil: true,
			expectedLocalityWeightedConfig:     false,
		},
		{
			name:                           "Send Unhealthy Endpoints enabled",
			discoveryType:                  cluster.Cluster_EDS,
			sendUnhealthyEndpoints:         true,
			expectedLbPolicy:               defaultLBAlgorithm(),
			expectedLocalityWeightedConfig: false,
		},
		// TODO: add more to cover all cases
	}

	proxy := model.Proxy{
		Type:         model.SidecarProxy,
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
		Metadata:     &model.NodeMetadata{},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			test.SetAtomicBoolForTest(t, features.GlobalSendUnhealthyEndpoints, tt.sendUnhealthyEndpoints)
			c := &cluster.Cluster{
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: tt.discoveryType},
				LoadAssignment:       &endpoint.ClusterLoadAssignment{},
				CommonLbConfig:       &cluster.Cluster_CommonLbConfig{},
			}

			if tt.discoveryType == cluster.Cluster_ORIGINAL_DST {
				c.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
			}

			if tt.port != nil && tt.port.Protocol == protocol.Redis {
				test.SetForTest(t, &features.EnableRedisFilter, true)
			}

			applyLoadBalancer(nil, c, tt.lbSettings, tt.port, proxy.Locality, nil, &meshconfig.MeshConfig{})

			if c.LbPolicy != tt.expectedLbPolicy {
				t.Errorf("cluster LbPolicy %s != expected %s", c.LbPolicy, tt.expectedLbPolicy)
			}

			if tt.sendUnhealthyEndpoints && c.CommonLbConfig.HealthyPanicThreshold.GetValue() != 0 {
				t.Errorf("panic threshold should be disabled when sendHealthyEndpoints is enabeld")
			}

			if tt.expectedLocalityWeightedConfig && c.CommonLbConfig.GetLocalityWeightedLbConfig() == nil {
				t.Errorf("cluster expected to have weighed config, but is nil")
			}

			if !tt.expectedLocalityWeightedConfig && c.CommonLbConfig.GetLocalityWeightedLbConfig() != nil {
				t.Errorf("cluster unexpected locality weighed config, but it is present")
			}

			if tt.expectClusterLoadAssignmenttoBeNil && c.LoadAssignment != nil {
				t.Errorf("cluster expected not to have load assignmentset, but is present")
			}
		})
	}
}

func TestBuildStaticClusterWithNoEndPoint(t *testing.T) {
	g := NewWithT(t)

	service := &model.Service{
		Hostname: host.Name("static.test"),
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
	g.Expect(xdstest.MapKeys(xdstest.ExtractClusters(clusters))).
		To(Equal([]string{"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster"}))
}

func TestEnvoyFilterPatching(t *testing.T) {
	service := &model.Service{
		Hostname: host.Name("static.test"),
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
			[]string{"outbound|8080||static.test", "BlackHoleCluster", "PassthroughCluster", "InboundPassthroughCluster"},
			nil,
			model.SidecarProxy,
			service,
		},
		{
			"add cluster",
			[]string{"outbound|8080||static.test", "BlackHoleCluster", "PassthroughCluster", "InboundPassthroughCluster", "new-cluster1"},
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
			[]string{"outbound|8080||static.test", "PassthroughCluster", "InboundPassthroughCluster"},
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
		svcInsts  []model.ServiceTarget
		service   *model.Service
		want      *core.Metadata
	}{
		{
			name:      "no cluster",
			direction: model.TrafficDirectionInbound,
			cluster:   nil,
			svcInsts: []model.ServiceTarget{
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
			svcInsts:  []model.ServiceTarget{},
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
			svcInsts: []model.ServiceTarget{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					Port: model.ServiceInstancePort{
						ServicePort: &model.Port{
							Port: 80,
						},
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
			svcInsts: []model.ServiceTarget{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					Port: model.ServiceInstancePort{
						ServicePort: &model.Port{
							Port: 80,
						},
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
			svcInsts: []model.ServiceTarget{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					Port: model.ServiceInstancePort{
						ServicePort: &model.Port{
							Port: 80,
						},
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
					Port: model.ServiceInstancePort{
						ServicePort: &model.Port{
							Port: 80,
						},
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
			svcInsts: []model.ServiceTarget{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					Port: model.ServiceInstancePort{
						ServicePort: &model.Port{
							Port: 80,
						},
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
			svcInsts: []model.ServiceTarget{
				{
					Service: &model.Service{
						Attributes: model.ServiceAttributes{
							Name:      "a",
							Namespace: "default",
						},
						Hostname: "a.default",
					},
					Port: model.ServiceInstancePort{
						ServicePort: &model.Port{
							Port: 80,
						},
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
					Port: model.ServiceInstancePort{
						ServicePort: &model.Port{
							Port: 80,
						},
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
				mutable: newClusterWrapper(tt.cluster),
				port:    &model.Port{Port: 80},
			}
			addTelemetryMetadata(tt.cluster, opt.port, tt.service, tt.direction, tt.svcInsts)
			if opt.mutable.cluster != nil && !reflect.DeepEqual(opt.mutable.cluster.Metadata, tt.want) {
				t.Errorf("cluster metadata does not match expectation want %+v, got %+v", tt.want, opt.mutable.cluster.Metadata)
			}
		})
	}
}

func TestBuildDeltaClusters(t *testing.T) {
	testService1 := &model.Service{
		Hostname: host.Name("test.com"),
		Ports: []*model.Port{
			{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		Resolution:   model.ClientSideLB,
		MeshExternal: false,
		Attributes: model.ServiceAttributes{
			Namespace: TestServiceNamespace,
		},
	}

	testService2 := &model.Service{
		Hostname: host.Name("testnew.com"),
		Ports: []*model.Port{
			{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		Resolution:   model.ClientSideLB,
		MeshExternal: false,
		Attributes: model.ServiceAttributes{
			Namespace: TestServiceNamespace,
		},
	}

	destRuleWithNewSubsets := &networking.DestinationRule{
		Host: "test.com",
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
				Name:   "subset-1",
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
			{
				Name:   "subset-2",
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

	destRuleWithRemovedSubsets := &networking.DestinationRule{
		Host: "test.com",
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
				Name:   "subset-2",
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

	destRuleWithMatchingWildcardHosts := &networking.DestinationRule{
		Host: "*.com",
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
				Name:   "subset-1",
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
			{
				Name:   "subset-2",
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

	destRuleWithNonMatchingWildcardHosts := &networking.DestinationRule{
		Host: "*.org",
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
				Name:   "subset-1",
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
			{
				Name:   "subset-2",
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

	destRuleWithNoSubsets := &networking.DestinationRule{
		Host: "test.com",
		TrafficPolicy: &networking.TrafficPolicy{
			Tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "/defaultCert.pem",
				PrivateKey:        "/defaultPrivateKey.pem",
				CaCertificates:    "/defaultCaCert.pem",
			},
		},
	}

	destRuleWithUpatedHost := &networking.DestinationRule{
		Host: "testnew.com",
		TrafficPolicy: &networking.TrafficPolicy{
			Tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "/defaultCert.pem",
				PrivateKey:        "/defaultPrivateKey.pem",
				CaCertificates:    "/defaultCaCert.pem",
			},
		},
	}

	// TODO: Add more test cases.
	testCases := []struct {
		name                 string
		services             []*model.Service
		instances            []*model.ServiceInstance
		configs              []config.Config
		prevConfigs          []config.Config
		configUpdated        sets.Set[model.ConfigKey]
		watchedResourceNames []string
		usedDelta            bool
		removedClusters      []string
		expectedClusters     []string
	}{
		{
			name:                 "service is added",
			services:             []*model.Service{testService1, testService2},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "testnew.com", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      nil,
			expectedClusters:     []string{"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", "outbound|8080||testnew.com"},
		},
		{
			name: "service and destination rule are added",
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithUpatedHost,
			}},
			services: []*model.Service{testService1, testService2},
			configUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "testnew.com", Namespace: TestServiceNamespace},
				model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      nil,
			expectedClusters:     []string{"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", "outbound|8080||testnew.com"},
		},
		{
			name:                 "service is removed",
			services:             []*model.Service{},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "test.com", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      []string{"outbound|8080||test.com"},
			expectedClusters:     []string{"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster"},
		},
		{
			name:          "service port is removed",
			services:      []*model.Service{testService1},
			configUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "test.com", Namespace: TestServiceNamespace}),
			instances: []*model.ServiceInstance{{
				Service:     testService1,
				ServicePort: &model.Port{Port: 8080},
				Endpoint:    &model.IstioEndpoint{Addresses: []string{"127.0.0.1"}, ServicePortName: "8080", EndpointPort: 8080},
			}},
			watchedResourceNames: []string{"outbound|7070||test.com", "inbound|7070||", "inbound|8080||"},
			usedDelta:            true,
			removedClusters:      []string{"inbound|7070||", "outbound|7070||test.com"},
			expectedClusters:     []string{"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", "inbound|8080||", "outbound|8080||test.com"},
		},
		{
			name:     "destination rule with no subsets is updated",
			services: []*model.Service{testService1},
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithNoSubsets,
			}},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      nil,
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", "outbound|8080||test.com",
			},
		},
		{
			name:     "destination rule is updated with new subset",
			services: []*model.Service{testService1},
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithNewSubsets,
			}},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com", "outbound|8080|subset-1|test.com"},
			usedDelta:            true,
			removedClusters:      []string{},
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster",
				"outbound|8080|subset-1|test.com", "outbound|8080|subset-2|test.com", "outbound|8080||test.com",
			},
		},
		{
			name:     "destination rule is updated with subset removal",
			services: []*model.Service{testService1},
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithRemovedSubsets,
			}},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080|subset-1|test.com", "outbound|8080|subset-2|test.com", "outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      []string{"outbound|8080|subset-1|test.com"},
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster",
				"outbound|8080|subset-2|test.com", "outbound|8080||test.com",
			},
		},
		{
			name:     "destination rule is removed",
			services: []*model.Service{testService1},
			prevConfigs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithRemovedSubsets,
			}},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080|subset-2|test.com", "outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      []string{"outbound|8080|subset-2|test.com"},
			expectedClusters:     []string{"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", "outbound|8080||test.com"},
		},
		{
			name:     "destination rule with wildcard matching hosts",
			services: []*model.Service{testService1},
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithMatchingWildcardHosts,
			}},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      nil,
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster",
				"outbound|8080|subset-1|test.com", "outbound|8080|subset-2|test.com", "outbound|8080||test.com",
			},
		},
		{
			name:     "destination rule with wildcard non matching hosts",
			services: []*model.Service{testService1},
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithNonMatchingWildcardHosts,
			}},
			prevConfigs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithNewSubsets,
			}},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      nil,
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", "outbound|8080||test.com",
			},
		},
		{
			name:     "destination rule with host updated",
			services: []*model.Service{testService1, testService2},
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithUpatedHost,
			}},
			prevConfigs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithNoSubsets,
			}},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      nil,
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", "outbound|8080||test.com", "outbound|8080||testnew.com",
			},
		},
		{
			name:     "service is added and destination rule is updated",
			services: []*model.Service{testService1, testService2},
			configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "test-desinationrule",
					Namespace:        TestServiceNamespace,
				},
				Spec: destRuleWithNewSubsets,
			}},
			configUpdated: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "testnew.com", Namespace: TestServiceNamespace},
				model.ConfigKey{Kind: kind.DestinationRule, Name: "test-desinationrule", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|8080||test.com"},
			usedDelta:            true,
			removedClusters:      nil,
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster",
				"outbound|8080|subset-1|test.com", "outbound|8080|subset-2|test.com", "outbound|8080||test.com",
				"outbound|8080||testnew.com",
			},
		},
		{
			name:                 "config update that is not delta aware",
			services:             []*model.Service{testService1, testService2},
			configUpdated:        sets.New(model.ConfigKey{Kind: kind.VirtualService, Name: "test.com", Namespace: TestServiceNamespace}),
			watchedResourceNames: []string{"outbound|7070||test.com"},
			usedDelta:            false,
			removedClusters:      nil,
			expectedClusters: []string{
				"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster",
				"outbound|8080||test.com", "outbound|8080||testnew.com",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{
				Services:  tc.services,
				Instances: tc.instances,
				Configs:   tc.configs,
			})
			proxy := cg.SetupProxy(&model.Proxy{IPAddresses: []string{"127.0.0.1"}})
			if tc.prevConfigs != nil {
				proxy.PrevSidecarScope = &model.SidecarScope{}
				proxy.PrevSidecarScope.SetDestinationRulesForTesting(tc.prevConfigs)
			}
			clusters, removed, delta := cg.DeltaClusters(proxy, tc.configUpdated,
				&model.WatchedResource{ResourceNames: sets.New(tc.watchedResourceNames...)})
			if delta != tc.usedDelta {
				t.Errorf("un expected delta, want %v got %v", tc.usedDelta, delta)
			}
			assert.Equal(t, removed, tc.removedClusters)
			assert.Equal(t, xdstest.MapKeys(xdstest.ExtractClusters(clusters)), tc.expectedClusters)
			assert.Equal(t, len(cg.env.PushContext().GetMetric(model.DuplicatedClusters.Name())), 0)
		})
	}
}

func TestBuildStaticClusterWithCredentialSocket(t *testing.T) {
	g := NewWithT(t)

	service := &model.Service{
		Hostname: host.Name("static.test"),
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
	proxy := cg.SetupProxy(nil)
	proxy.Metadata.Raw = map[string]any{
		security.CredentialMetaDataName: "true",
	}
	// Expect sds_external cluster be added if credentialSocket exists
	clusters := cg.Clusters(proxy)
	xdstest.ValidateClusters(t, clusters)
	g.Expect(xdstest.MapKeys(xdstest.ExtractClusters(clusters))).To(Equal([]string{
		"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster", security.SDSExternalClusterName,
	}))

	// Expect no sds_external cluster be added if if credentialSocket does NOT exists
	proxy = cg.SetupProxy(nil)
	clusters = cg.Clusters(proxy)
	xdstest.ValidateClusters(t, clusters)
	g.Expect(xdstest.MapKeys(xdstest.ExtractClusters(clusters))).To(Equal([]string{
		"BlackHoleCluster", "InboundPassthroughCluster", "PassthroughCluster",
	}))
}
