// Copyright 2020 Istio Authors. All Rights Reserved.
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

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
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
		cluster                *apiv2.Cluster
		clusterMode            ClusterMode
		service                *model.Service
		port                   *model.Port
		proxy                  *model.Proxy
		networkView            map[string]bool
		destRule               *networking.DestinationRule
		expectedSubsetClusters []*apiv2.Cluster
	}{
		// TODO(ramaraochavali): Add more tests to cover additional conditions.
		{
			name:                   "nil destination rule",
			cluster:                &apiv2.Cluster{},
			clusterMode:            DefaultClusterMode,
			service:                &model.Service{},
			port:                   &model.Port{},
			proxy:                  &model.Proxy{},
			networkView:            map[string]bool{},
			destRule:               nil,
			expectedSubsetClusters: []*apiv2.Cluster{},
		},
		{
			name:        "destination rule with subsets",
			cluster:     &apiv2.Cluster{Name: "foo", ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_EDS}},
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
			expectedSubsetClusters: []*apiv2.Cluster{
				{
					Name:                 "outbound|8080|foobar|foo",
					ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_EDS},
					EdsClusterConfig: &apiv2.Cluster_EdsClusterConfig{
						ServiceName: "outbound|8080|foobar|foo",
					},
				},
			},
		},
		{
			name:        "destination rule with subsets for SniDnat cluster",
			cluster:     &apiv2.Cluster{Name: "foo", ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_EDS}},
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
			expectedSubsetClusters: []*apiv2.Cluster{
				{
					Name:                 "outbound_.8080_.foobar_.foo",
					ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_EDS},
					EdsClusterConfig: &apiv2.Cluster_EdsClusterConfig{
						ServiceName: "outbound_.8080_.foobar_.foo",
					},
				},
			},
		},
		{
			name:        "destination rule with subset traffic policy",
			cluster:     &apiv2.Cluster{Name: "foo", ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_EDS}},
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
			expectedSubsetClusters: []*apiv2.Cluster{
				{
					Name:                 "outbound|8080|foobar|foo",
					ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_EDS},
					EdsClusterConfig: &apiv2.Cluster_EdsClusterConfig{
						ServiceName: "outbound|8080|foobar|foo",
					},
					CircuitBreakers: &envoy_api_v2_cluster.CircuitBreakers{
						Thresholds: []*envoy_api_v2_cluster.CircuitBreakers_Thresholds{
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
									Type:    collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Kind(),
									Version: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Version(),
									Name:    "acme",
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

			subsetClusters := applyDestinationRule(tt.cluster, tt.clusterMode, tt.service, tt.port, tt.proxy, tt.networkView, env.PushContext)
			if len(subsetClusters) != len(tt.expectedSubsetClusters) {
				t.Errorf("Unexpected subset clusters want %v, got %v", len(tt.expectedSubsetClusters), len(subsetClusters))
			}
			if len(tt.expectedSubsetClusters) > 0 {
				compareClusters(t, tt.expectedSubsetClusters[0], subsetClusters[0])
			}
		})
	}
}

func compareClusters(t *testing.T, ec *apiv2.Cluster, gc *apiv2.Cluster) {
	// TODO(ramaraochavali): Expand the comparison to more fields.
	t.Helper()
	if ec.Name != gc.Name {
		t.Errorf("Unexpected cluster name want %s, got %s", ec.Name, gc.Name)
	}
	if ec.GetType() != gc.GetType() {
		t.Errorf("Unexpected cluster discovery type want %v, got %v", ec.GetType(), gc.GetType())
	}
	if ec.GetType() == apiv2.Cluster_EDS && ec.EdsClusterConfig.ServiceName != gc.EdsClusterConfig.ServiceName {
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
		name      string
		cluster   *apiv2.Cluster
		edsConfig *apiv2.Cluster_EdsClusterConfig
	}{
		{
			name:      "non eds type of cluster",
			cluster:   &apiv2.Cluster{Name: "foo", ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_STRICT_DNS}},
			edsConfig: nil,
		},
		{
			name:    "eds type of cluster",
			cluster: &apiv2.Cluster{Name: "foo", ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_EDS}},
			edsConfig: &apiv2.Cluster_EdsClusterConfig{
				ServiceName: "foo",
				EdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
					InitialFetchTimeout: features.InitialFetchTimeout,
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
