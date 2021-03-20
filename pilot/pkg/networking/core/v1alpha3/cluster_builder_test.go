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

package v1alpha3

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestApplyDestinationRule(t *testing.T) {
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
		Hostname:    host.Name("foo.default.svc.cluster.local"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       servicePort,
		Resolution:  model.ClientSideLB,
		Attributes:  serviceAttribute,
	}

	cases := []struct {
		name                   string
		cluster                *cluster.Cluster
		clusterMode            ClusterMode
		service                *model.Service
		port                   *model.Port
		networkView            map[string]bool
		destRule               *networking.DestinationRule
		expectedSubsetClusters []*cluster.Cluster
	}{
		// TODO(ramaraochavali): Add more tests to cover additional conditions.
		{
			name:                   "nil destination rule",
			cluster:                &cluster.Cluster{},
			clusterMode:            DefaultClusterMode,
			service:                &model.Service{},
			port:                   &model.Port{},
			networkView:            map[string]bool{},
			destRule:               nil,
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with subsets",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name:   "foobar",
						Labels: map[string]string{"foo": "bar"},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{
				{
					Name:                 "outbound|8080|foobar|foo.default.svc.cluster.local",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
					EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
						ServiceName: "outbound|8080|foobar|foo.default.svc.cluster.local",
					},
				},
			},
		},
		{
			name:        "destination rule with pass through subsets",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name:   "foobar",
						Labels: map[string]string{"foo": "bar"},
						TrafficPolicy: &networking.TrafficPolicy{
							LoadBalancer: &networking.LoadBalancerSettings{
								LbPolicy: &networking.LoadBalancerSettings_Simple{Simple: networking.LoadBalancerSettings_PASSTHROUGH},
							},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{
				{
					Name:                 "outbound|8080|foobar|foo.default.svc.cluster.local",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
				},
			},
		},
		{
			name:        "destination rule with subsets for SniDnat cluster",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: SniDnatClusterMode,
			service:     service,
			port:        servicePort[0],
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name:   "foobar",
						Labels: map[string]string{"foo": "bar"},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{
				{
					Name:                 "outbound_.8080_.foobar_.foo.default.svc.cluster.local",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
					EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
						ServiceName: "outbound_.8080_.foobar_.foo.default.svc.cluster.local",
					},
				},
			},
		},
		{
			name:        "destination rule with subset traffic policy",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name:   "foobar",
						Labels: map[string]string{"foo": "bar"},
						TrafficPolicy: &networking.TrafficPolicy{
							ConnectionPool: &networking.ConnectionPoolSettings{
								Http: &networking.ConnectionPoolSettings_HTTPSettings{
									MaxRetries: 10,
								},
							},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{
				{
					Name:                 "outbound|8080|foobar|foo.default.svc.cluster.local",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
					EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
						ServiceName: "outbound|8080|foobar|foo.default.svc.cluster.local",
					},
					CircuitBreakers: &cluster.CircuitBreakers{
						Thresholds: []*cluster.CircuitBreakers_Thresholds{
							{
								MaxRetries: &wrappers.UInt32Value{
									Value: 10,
								},
							},
						},
					},
				},
			},
		},
		{
			name:        "destination rule with use client protocol traffic policy",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxRetries:        10,
							UseClientProtocol: true,
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			instances := []*model.ServiceInstance{
				{
					Service:     tt.service,
					ServicePort: tt.port,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.1",
						EndpointPort: 10001,
						Locality: model.Locality{
							ClusterID: "",
							Label:     "region1/zone1/subzone1",
						},
						TLSMode: model.IstioMutualTLSModeLabel,
					},
				},
			}

			var cfg *config.Config
			if tt.destRule != nil {
				cfg = &config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "default",
					},
					Spec: tt.destRule,
				}
			}
			cg := NewConfigGenTest(t, TestOptions{
				ConfigPointers: []*config.Config{cfg},
				Services:       []*model.Service{tt.service},
			})
			cg.MemRegistry.WantGetProxyServiceInstances = instances
			cb := NewClusterBuilder(cg.SetupProxy(nil), cg.PushContext())

			ec := NewMutableCluster(tt.cluster)
			subsetClusters := cb.applyDestinationRule(ec, tt.clusterMode, tt.service, tt.port, tt.networkView)
			if len(subsetClusters) != len(tt.expectedSubsetClusters) {
				t.Errorf("Unexpected subset clusters want %v, got %v", len(tt.expectedSubsetClusters), len(subsetClusters))
			}
			if len(tt.expectedSubsetClusters) > 0 {
				compareClusters(t, tt.expectedSubsetClusters[0], subsetClusters[0])
			}
			// Validate that use client protocol configures cluster correctly.
			if tt.destRule != nil && tt.destRule.TrafficPolicy != nil && tt.destRule.TrafficPolicy.GetConnectionPool().GetHttp().UseClientProtocol {
				if ec.httpProtocolOptions == nil {
					t.Errorf("Expected cluster %s to have http protocol options but not found", tt.cluster.Name)
				}
				if ec.httpProtocolOptions.UpstreamProtocolOptions == nil &&
					ec.httpProtocolOptions.GetUseDownstreamProtocolConfig() == nil {
					t.Errorf("Expected cluster %s to have downstream protocol options but not found", tt.cluster.Name)
				}
			}

			// Validate that ORIGINAL_DST cluster does not have load assignments
			for _, subset := range subsetClusters {
				if subset.GetType() == cluster.Cluster_ORIGINAL_DST && subset.GetLoadAssignment() != nil {
					t.Errorf("Passthrough subsets should not have load assignments")
				}
			}
		})
	}
}

func compareClusters(t *testing.T, ec *cluster.Cluster, gc *cluster.Cluster) {
	// TODO(ramaraochavali): Expand the comparison to more fields.
	t.Helper()
	if ec.Name != gc.Name {
		t.Errorf("Unexpected cluster name want %s, got %s", ec.Name, gc.Name)
	}
	if ec.GetType() != gc.GetType() {
		t.Errorf("Unexpected cluster discovery type want %v, got %v", ec.GetType(), gc.GetType())
	}
	if ec.GetType() == cluster.Cluster_EDS && ec.EdsClusterConfig.ServiceName != gc.EdsClusterConfig.ServiceName {
		t.Errorf("Unexpected service name in EDS config want %v, got %v", ec.EdsClusterConfig.ServiceName, gc.EdsClusterConfig.ServiceName)
	}
	if ec.CircuitBreakers != nil {
		if ec.CircuitBreakers.Thresholds[0].MaxRetries.Value != gc.CircuitBreakers.Thresholds[0].MaxRetries.Value {
			t.Errorf("Unexpected circuit breaker thresholds want %v, got %v", ec.CircuitBreakers.Thresholds[0].MaxRetries, gc.CircuitBreakers.Thresholds[0].MaxRetries)
		}
	}
}

func TestMergeTrafficPolicy(t *testing.T) {
	cases := []struct {
		name     string
		original *networking.TrafficPolicy
		subset   *networking.TrafficPolicy
		port     *model.Port
		expected *networking.TrafficPolicy
	}{
		{
			name:     "all nil policies",
			original: nil,
			subset:   nil,
			port:     nil,
			expected: nil,
		},
		{
			name: "no subset policy",
			original: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
			subset: nil,
			port:   nil,
			expected: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
		},
		{
			name:     "no parent policy",
			original: nil,
			subset: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
			port: nil,
			expected: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
		},
		{
			name: "merge non-conflicting fields",
			original: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				},
			},
			subset: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
			port: nil,
			expected: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				},
			},
		},
		{
			name: "subset overwrite top-level fields",
			original: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
			subset: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
			port: nil,
			expected: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
		},
		{
			name:     "merge port level policy, and do not inherit top-level fields",
			original: nil,
			subset: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_LEAST_CONN,
							},
						},
					},
				},
			},
			port: &model.Port{Port: 8080},
			expected: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_LEAST_CONN,
					},
				},
			},
		},
		{
			name: "merge port level policy, and do not inherit top-level fields",
			original: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 20,
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 15,
						},
					},
				},
			},
			subset: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 13,
						},
					},
				},
			},
			port: &model.Port{Port: 8080},
			expected: &networking.TrafficPolicy{
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 13,
				},
			},
		},
		{
			name: "default cluster, non-matching port selector",
			original: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 20,
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 15,
						},
					},
				},
			},
			subset: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 13,
						},
					},
				},
			},
			port: &model.Port{Port: 9090},
			expected: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 20,
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			policy := MergeTrafficPolicy(tt.original, tt.subset, tt.port)
			if !reflect.DeepEqual(policy, tt.expected) {
				t.Errorf("Unexpected merged TrafficPolicy. want %v, got %v", tt.expected, policy)
			}
		})
	}
}

func TestApplyEdsConfig(t *testing.T) {
	cases := []struct {
		name      string
		cluster   *cluster.Cluster
		edsConfig *cluster.Cluster_EdsClusterConfig
	}{
		{
			name:      "non eds type of cluster",
			cluster:   &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS}},
			edsConfig: nil,
		},
		{
			name:    "eds type of cluster",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			edsConfig: &cluster.Cluster_EdsClusterConfig{
				ServiceName: "foo",
				EdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
					ResourceApiVersion: core.ApiVersion_V3,
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			maybeApplyEdsConfig(tt.cluster)
			if !reflect.DeepEqual(tt.cluster.EdsClusterConfig, tt.edsConfig) {
				t.Errorf("Unexpected Eds config in cluster. want %v, got %v", tt.edsConfig, tt.cluster.EdsClusterConfig)
			}
		})
	}
}

func TestBuildDefaultCluster(t *testing.T) {
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: protocol.HTTP,
	}

	cases := []struct {
		name            string
		clusterName     string
		discovery       cluster.Cluster_DiscoveryType
		endpoints       []*endpoint.LocalityLbEndpoints
		direction       model.TrafficDirection
		external        bool
		expectedCluster *cluster.Cluster
	}{
		{
			name:        "default EDS cluster",
			clusterName: "foo",
			discovery:   cluster.Cluster_EDS,
			endpoints:   nil,
			direction:   model.TrafficDirectionOutbound,
			external:    false,
			expectedCluster: &cluster.Cluster{
				Name:                 "foo",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				ConnectTimeout:       &duration.Duration{Seconds: 10, Nanos: 1},
				CircuitBreakers: &cluster.CircuitBreakers{
					Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
				},
				Metadata: &core.Metadata{
					FilterMetadata: map[string]*structpb.Struct{
						util.IstioMetadataKey: {
							Fields: map[string]*structpb.Value{
								"services": {Kind: &structpb.Value_ListValue{ListValue: &structpb.ListValue{Values: []*structpb.Value{
									{Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
										"host": {
											Kind: &structpb.Value_StringValue{
												StringValue: "host",
											},
										},
										"name": {
											Kind: &structpb.Value_StringValue{
												StringValue: "svc",
											},
										},
										"namespace": {
											Kind: &structpb.Value_StringValue{
												StringValue: "default",
											},
										},
									}}}},
								}}}},
								"default_original_port": {
									Kind: &structpb.Value_NumberValue{
										NumberValue: float64(8080),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:            "static cluster with no endpoints",
			clusterName:     "foo",
			discovery:       cluster.Cluster_STATIC,
			endpoints:       nil,
			direction:       model.TrafficDirectionOutbound,
			external:        false,
			expectedCluster: nil,
		},
		{
			name:            "strict DNS cluster with no endpoints",
			clusterName:     "foo",
			discovery:       cluster.Cluster_STRICT_DNS,
			endpoints:       nil,
			direction:       model.TrafficDirectionOutbound,
			external:        false,
			expectedCluster: nil,
		},
		{
			name:        "static cluster with endpoints",
			clusterName: "foo",
			discovery:   cluster.Cluster_STATIC,
			endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LbEndpoints: []*endpoint.LbEndpoint{},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
			},
			direction: model.TrafficDirectionOutbound,
			external:  false,
			expectedCluster: &cluster.Cluster{
				Name:                 "foo",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
				ConnectTimeout:       &duration.Duration{Seconds: 10, Nanos: 1},
				LoadAssignment: &endpoint.ClusterLoadAssignment{
					ClusterName: "foo",
					Endpoints: []*endpoint.LocalityLbEndpoints{
						{
							Locality: &core.Locality{
								Region:  "region1",
								Zone:    "zone1",
								SubZone: "subzone1",
							},
							LbEndpoints: []*endpoint.LbEndpoint{},
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 1,
							},
							Priority: 0,
						},
					},
				},
				CircuitBreakers: &cluster.CircuitBreakers{
					Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
				},
				Metadata: &core.Metadata{
					FilterMetadata: map[string]*structpb.Struct{
						util.IstioMetadataKey: {Fields: map[string]*structpb.Value{
							"services": {Kind: &structpb.Value_ListValue{ListValue: &structpb.ListValue{Values: []*structpb.Value{
								{Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
									"host": {
										Kind: &structpb.Value_StringValue{
											StringValue: "host",
										},
									},
									"name": {
										Kind: &structpb.Value_StringValue{
											StringValue: "svc",
										},
									},
									"namespace": {
										Kind: &structpb.Value_StringValue{
											StringValue: "default",
										},
									},
								}}}},
							}}}},
							"default_original_port": {
								Kind: &structpb.Value_NumberValue{
									NumberValue: float64(8080),
								},
							},
						}},
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{MeshConfig: &testMesh})
			cb := NewClusterBuilder(cg.SetupProxy(nil), cg.PushContext())

			defaultCluster := cb.buildDefaultCluster(tt.clusterName, tt.discovery, tt.endpoints, tt.direction, servicePort, &model.Service{
				Ports: model.PortList{
					servicePort,
				},
				Hostname: "host", MeshExternal: false, Attributes: model.ServiceAttributes{Name: "svc", Namespace: "default"},
			}, nil)

			if diff := cmp.Diff(defaultCluster.build(), tt.expectedCluster, protocmp.Transform()); diff != "" {
				t.Errorf("Unexpected default cluster, diff: %v", diff)
			}
		})
	}
}

func TestBuildLocalityLbEndpoints(t *testing.T) {
	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID: "cluster-1",
		},
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

	cases := []struct {
		name      string
		mesh      meshconfig.MeshConfig
		instances []*model.ServiceInstance
		expected  []*endpoint.LocalityLbEndpoints
	}{
		{
			name: "basics",
			mesh: testMesh,
			instances: []*model.ServiceInstance{
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.1",
						EndpointPort: 10001,
						WorkloadName: "workload-1",
						Namespace:    "namespace-1",
						Locality: model.Locality{
							ClusterID: "cluster-1",
							Label:     "region1/zone1/subzone1",
						},
						LbWeight: 30,
						Network:  "nw-0",
					},
				},
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.2",
						EndpointPort: 10001,
						WorkloadName: "workload-2",
						Namespace:    "namespace-2",
						Locality: model.Locality{
							ClusterID: "cluster-2",
							Label:     "region1/zone1/subzone1",
						},
						LbWeight: 30,
						Network:  "nw-1",
					},
				},
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.3",
						EndpointPort: 10001,
						WorkloadName: "workload-3",
						Namespace:    "namespace-3",
						Locality: model.Locality{
							ClusterID: "cluster-3",
							Label:     "region2/zone1/subzone1",
						},
						LbWeight: 40,
						Network:  "",
					},
				},
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Address:      "192.168.1.4",
						EndpointPort: 10001,
						WorkloadName: "workload-1",
						Namespace:    "namespace-1",
						Locality: model.Locality{
							ClusterID: "cluster-1",
							Label:     "region1/zone1/subzone1",
						},
						LbWeight: 30,
						Network:  "filtered-out",
					},
				},
			},
			expected: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 60,
					},
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: &endpoint.LbEndpoint_Endpoint{
								Endpoint: &endpoint.Endpoint{
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
							Metadata: util.BuildLbEndpointMetadata("nw-0", "", "workload-1", "namespace-1", "cluster-1", map[string]string{}),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 30,
							},
						},
						{
							HostIdentifier: &endpoint.LbEndpoint_Endpoint{
								Endpoint: &endpoint.Endpoint{
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
							Metadata: util.BuildLbEndpointMetadata("nw-1", "", "workload-2", "namespace-2", "cluster-2", map[string]string{}),
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
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: &endpoint.LbEndpoint_Endpoint{
								Endpoint: &endpoint.Endpoint{
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
							Metadata: util.BuildLbEndpointMetadata("", "", "workload-3", "namespace-3", "cluster-3", map[string]string{}),
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
			mesh: withClusterLocalHosts(testMesh, "*.example.org"),
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
			expected: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 30,
					},
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: &endpoint.LbEndpoint_Endpoint{
								Endpoint: &endpoint.Endpoint{
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
							Metadata: util.BuildLbEndpointMetadata("", "", "", "", "cluster-1", map[string]string{}),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 30,
							},
						},
					},
				},
			},
		},
	}

	sortEndpoints := func(endpoints []*endpoint.LocalityLbEndpoints) {
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

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{
				MeshConfig: &tt.mesh,
				Services:   []*model.Service{service},
				Instances:  tt.instances,
			})

			cb := NewClusterBuilder(cg.SetupProxy(proxy), cg.PushContext())
			nv := map[string]bool{
				"nw-0":               true,
				"nw-1":               true,
				model.UnnamedNetwork: true,
			}
			actual := cb.buildLocalityLbEndpoints(nv, service, 8080, nil)
			sortEndpoints(actual)
			if v := cmp.Diff(tt.expected, actual, protocmp.Transform()); v != "" {
				t.Fatalf("Expected (-) != actual (+):\n%s", v)
			}
		})
	}
}

func TestBuildPassthroughClusters(t *testing.T) {
	cases := []struct {
		name         string
		ips          []string
		ipv4Expected bool
		ipv6Expected bool
	}{
		{
			name:         "both ipv4 and ipv6",
			ips:          []string{"6.6.6.6", "::1"},
			ipv4Expected: true,
			ipv6Expected: true,
		},
		{
			name:         "ipv4 only",
			ips:          []string{"6.6.6.6"},
			ipv4Expected: true,
			ipv6Expected: false,
		},
		{
			name:         "ipv6 only",
			ips:          []string{"::1"},
			ipv4Expected: false,
			ipv6Expected: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{IPAddresses: tt.ips}
			cg := NewConfigGenTest(t, TestOptions{})

			cb := NewClusterBuilder(cg.SetupProxy(proxy), cg.PushContext())
			clusters := cb.buildInboundPassthroughClusters()

			var hasIpv4, hasIpv6 bool
			for _, c := range clusters {
				hasIpv4 = hasIpv4 || c.Name == util.InboundPassthroughClusterIpv4
				hasIpv6 = hasIpv6 || c.Name == util.InboundPassthroughClusterIpv6
			}
			if hasIpv4 != tt.ipv4Expected {
				t.Errorf("Unexpected Ipv4 Passthrough Cluster, want %v got %v", tt.ipv4Expected, hasIpv4)
			}
			if hasIpv6 != tt.ipv6Expected {
				t.Errorf("Unexpected Ipv6 Passthrough Cluster, want %v got %v", tt.ipv6Expected, hasIpv6)
			}

			passthrough := xdstest.ExtractCluster(util.InboundPassthroughClusterIpv4, clusters)
			if passthrough == nil {
				passthrough = xdstest.ExtractCluster(util.InboundPassthroughClusterIpv6, clusters)
			}
			// Validate that Passthrough Cluster LB Policy is set correctly.
			if passthrough.GetType() != cluster.Cluster_ORIGINAL_DST || passthrough.GetLbPolicy() != cluster.Cluster_CLUSTER_PROVIDED {
				t.Errorf("Unexpected Discovery type or Lb policy, got Discovery type: %v, Lb Policy: %v", passthrough.GetType(), passthrough.GetLbPolicy())
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
		h2                         bool
		expectTransportSocket      bool
		expectTransportSocketMatch bool

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
			h2:                         true,
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
			h2:                         true,
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
			h2:                         true,
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
			h2:                         true,
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
			h2:                         true,
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
			cb := NewClusterBuilder(proxy, push)
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
				mutable: NewMutableCluster(&cluster.Cluster{
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: test.discoveryType},
				}),
				proxy: proxy,
				mesh:  push.Mesh,
			}
			if test.h2 {
				cb.setH2Options(opts.mutable)
			}
			cb.applyUpstreamTLSSettings(opts, test.tls, test.mtlsCtx)

			if test.expectTransportSocket && opts.mutable.cluster.TransportSocket == nil ||
				!test.expectTransportSocket && opts.mutable.cluster.TransportSocket != nil {
				t.Errorf("Expected TransportSocket %v", test.expectTransportSocket)
			}
			if test.expectTransportSocketMatch && opts.mutable.cluster.TransportSocketMatches == nil ||
				!test.expectTransportSocketMatch && opts.mutable.cluster.TransportSocketMatches != nil {
				t.Errorf("Expected TransportSocketMatch %v", test.expectTransportSocketMatch)
			}

			if test.validateTLSContext != nil {
				ctx := &tls.UpstreamTlsContext{}
				if test.expectTransportSocket {
					if err := ptypes.UnmarshalAny(opts.mutable.cluster.TransportSocket.GetTypedConfig(), ctx); err != nil {
						t.Fatal(err)
					}
				} else if test.expectTransportSocketMatch {
					if err := ptypes.UnmarshalAny(opts.mutable.cluster.TransportSocketMatches[0].TransportSocket.GetTypedConfig(), ctx); err != nil {
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
		h2     bool
		result expectedResult
	}{
		{
			name: "tls mode disabled",
			opts: &buildClusterOpts{
				mutable: NewMutableCluster(&cluster.Cluster{
					Name: "test-cluster",
				}),
			},
			tls: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_DISABLE,
			},
			result: expectedResult{nil, nil},
		},
		{
			name: "tls mode ISTIO_MUTUAL with metadata certs",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
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
				mutable: newTestCluster(),
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
			h2: true,
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
				mutable: newTestCluster(),
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
				mutable: newTestCluster(),
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
			name: "tls mode SIMPLE, with certs specified in tls with h2",
			opts: &buildClusterOpts{
				mutable: newH2TestCluster(),
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
			h2: true,
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
			name: "tls mode SIMPLE, with certs specified in tls with overridden metadata certs",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
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
			name: "tls mode MUTUAL, with no client certificate",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
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
				mutable: newTestCluster(),
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
				mutable: newTestCluster(),
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
									ResourceApiVersion: core.ApiVersion_V3,
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
				mutable: newTestCluster(),
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
									ResourceApiVersion: core.ApiVersion_V3,
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
			name: "tls mode SIMPLE, with CredentialName specified",
			opts: &buildClusterOpts{
				mutable: newTestCluster(),
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
				mutable: newTestCluster(),
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
				mutable: newH2TestCluster(),
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
			h2: true,
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
				mutable: newTestCluster(),
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
				mutable: newH2TestCluster(),
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
			h2: true,
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
				mutable: newTestCluster(),
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
				mutable: newTestCluster(),
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
			cb := NewClusterBuilder(nil, nil)
			if tc.h2 {
				cb.setH2Options(tc.opts.mutable)
			}
			ret, err := cb.buildUpstreamClusterTLSContext(tc.opts, tc.tls)
			if err != nil && tc.result.err == nil || err == nil && tc.result.err != nil {
				t.Errorf("expecting:\n err=%v but got err=%v", tc.result.err, err)
			} else if diff := cmp.Diff(tc.result.tlsContext, ret, protocmp.Transform()); diff != "" {
				t.Errorf("got diff: `%v", diff)
			}
		})
	}
}

func newTestCluster() *MutableCluster {
	return NewMutableCluster(&cluster.Cluster{
		Name: "test-cluster",
	})
}

func newH2TestCluster() *MutableCluster {
	return NewMutableCluster(&cluster.Cluster{
		Name:                 "test-cluster",
		Http2ProtocolOptions: &core.Http2ProtocolOptions{},
	})
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

	cb := NewClusterBuilder(nil, nil)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upgrade := cb.shouldH2Upgrade(test.clusterName, test.direction, &test.port, &test.mesh, &test.connectionPool)

			if upgrade != test.upgrade {
				t.Fatalf("got: %t, want: %t (%v, %v)", upgrade, test.upgrade, test.mesh.H2UpgradePolicy, test.connectionPool.Http.H2UpgradePolicy)
			}
		})
	}
}
