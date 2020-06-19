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
	"reflect"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
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
		Hostname:    host.Name("foo"),
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
		proxy                  *model.Proxy
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
			proxy:                  &model.Proxy{},
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
			proxy:       &model.Proxy{},
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo",
				Subsets: []*networking.Subset{
					{
						Name:   "foobar",
						Labels: map[string]string{"foo": "bar"},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{
				{
					Name:                 "outbound|8080|foobar|foo",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
					EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
						ServiceName: "outbound|8080|foobar|foo",
					},
				},
			},
		},
		{
			name:        "destination rule with subsets for SniDnat cluster",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: SniDnatClusterMode,
			service:     service,
			port:        servicePort[0],
			proxy:       &model.Proxy{},
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo",
				Subsets: []*networking.Subset{
					{
						Name:   "foobar",
						Labels: map[string]string{"foo": "bar"},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{
				{
					Name:                 "outbound_.8080_.foobar_.foo",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
					EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
						ServiceName: "outbound_.8080_.foobar_.foo",
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
			proxy:       &model.Proxy{},
			networkView: map[string]bool{},
			destRule: &networking.DestinationRule{
				Host: "foo",
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
					Name:                 "outbound|8080|foobar|foo",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
					EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
						ServiceName: "outbound|8080|foobar|foo",
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
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {

			serviceDiscovery := &fakes.ServiceDiscovery{}

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

			serviceDiscovery.ServicesReturns([]*model.Service{tt.service}, nil)
			serviceDiscovery.GetProxyServiceInstancesReturns(instances, nil)
			serviceDiscovery.InstancesByPortReturns(instances, nil)

			configStore := &fakes.IstioConfigStore{
				ListStub: func(typ resource.GroupVersionKind, namespace string) (configs []model.Config, e error) {
					if typ == collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind() {
						if tt.destRule != nil {
							return []model.Config{
								{ConfigMeta: model.ConfigMeta{
									GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
									Name:             "acme",
								},
									Spec: tt.destRule,
								}}, nil
						}
					}
					return nil, nil
				},
			}
			env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

			proxy.SetSidecarScope(env.PushContext)

			cb := NewClusterBuilder(tt.proxy, env.PushContext)

			subsetClusters := cb.applyDestinationRule(tt.proxy, tt.cluster, tt.clusterMode, tt.service, tt.port, tt.networkView)
			if len(subsetClusters) != len(tt.expectedSubsetClusters) {
				t.Errorf("Unexpected subset clusters want %v, got %v", len(tt.expectedSubsetClusters), len(subsetClusters))
			}
			if len(tt.expectedSubsetClusters) > 0 {
				compareClusters(t, tt.expectedSubsetClusters[0], subsetClusters[0])
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

func TestApplyEdsConfig(t *testing.T) {
	cases := []struct {
		name       string
		cluster    *cluster.Cluster
		cdsVersion string
		edsConfig  *cluster.Cluster_EdsClusterConfig
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
					InitialFetchTimeout: features.InitialFetchTimeout,
				},
			},
		},
		{
			name:    "eds type of cluster v3",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			edsConfig: &cluster.Cluster_EdsClusterConfig{
				ServiceName: "foo",
				EdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
					InitialFetchTimeout: features.InitialFetchTimeout,
					ResourceApiVersion:  core.ApiVersion_V3,
				},
			},
			cdsVersion: v3.ClusterType,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			maybeApplyEdsConfig(tt.cluster, tt.cdsVersion)
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
					Thresholds: []*cluster.CircuitBreakers_Thresholds{&defaultCircuitBreakerThresholds},
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
					Thresholds: []*cluster.CircuitBreakers_Thresholds{&defaultCircuitBreakerThresholds},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			serviceDiscovery := &fakes.ServiceDiscovery{}
			configStore := &fakes.IstioConfigStore{}
			env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

			proxy.SetSidecarScope(env.PushContext)

			cb := NewClusterBuilder(&model.Proxy{}, env.PushContext)

			defaultCluster := cb.buildDefaultCluster(tt.clusterName, tt.discovery,
				tt.endpoints, tt.direction, servicePort, tt.external)

			if !reflect.DeepEqual(defaultCluster, tt.expectedCluster) {
				t.Errorf("Unexpected default cluster, want %v got %v", tt.expectedCluster, defaultCluster)
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
			serviceDiscovery := &fakes.ServiceDiscovery{}
			configStore := &fakes.IstioConfigStore{}
			env := newTestEnvironment(serviceDiscovery, testMesh, configStore)

			proxy := &model.Proxy{IPAddresses: tt.ips}
			proxy.SetSidecarScope(env.PushContext)
			proxy.DiscoverIPVersions()

			cb := NewClusterBuilder(proxy, env.PushContext)

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
			// Validate that Passthrough Cluster LB Policy is set correctly.
			if clusters[0].GetType() != cluster.Cluster_ORIGINAL_DST || clusters[0].GetLbPolicy() != cluster.Cluster_CLUSTER_PROVIDED {
				t.Errorf("Unexpected Discovery type or Lb policy, got Discovery type: %v, Lb Policy: %v", clusters[0].GetType(), clusters[0].GetLbPolicy())
			}
		})
	}
}
