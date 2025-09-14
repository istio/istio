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

package core

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	cares "github.com/envoyproxy/go-control-plane/envoy/extensions/network/dns_resolver/cares/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	istiocluster "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
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
	http2ServicePort := model.PortList{
		&model.Port{
			Name:     "default",
			Port:     8080,
			Protocol: protocol.HTTP2,
		},
		&model.Port{
			Name:     "auto",
			Port:     9090,
			Protocol: protocol.Unsupported,
		},
	}
	service := &model.Service{
		Hostname:   host.Name("foo.default.svc.cluster.local"),
		Ports:      servicePort,
		Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Namespace: TestServiceNamespace,
		},
	}
	http2Service := &model.Service{
		Hostname:   host.Name("foo.default.svc.cluster.local"),
		Ports:      http2ServicePort,
		Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Namespace: TestServiceNamespace,
		},
	}

	cases := []struct {
		name                   string
		cluster                *cluster.Cluster
		clusterMode            ClusterMode
		service                *model.Service
		port                   *model.Port
		proxyView              model.ProxyView
		destRule               *networking.DestinationRule
		meshConfig             *meshconfig.MeshConfig
		expectedSubsetClusters []*cluster.Cluster
	}{
		// TODO(ramaraochavali): Add more tests to cover additional conditions.
		{
			name:                   "nil destination rule",
			cluster:                &cluster.Cluster{},
			clusterMode:            DefaultClusterMode,
			service:                &model.Service{},
			port:                   &model.Port{},
			proxyView:              model.ProxyViewAll,
			destRule:               nil,
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with subsets",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
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
			proxyView:   model.ProxyViewAll,
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
			name: "destination rule static with pass",
			cluster: &cluster.Cluster{
				Name:                 "foo",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
				LoadAssignment:       &endpoint.ClusterLoadAssignment{},
			},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					LoadBalancer: &networking.LoadBalancerSettings{
						LbPolicy: &networking.LoadBalancerSettings_Simple{Simple: networking.LoadBalancerSettings_PASSTHROUGH},
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
			proxyView:   model.ProxyViewAll,
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
			proxyView:   model.ProxyViewAll,
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
			name:        "cluster with OutboundClusterStatName",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name:   "foobar",
						Labels: map[string]string{"foo": "bar"},
					},
				},
			},
			meshConfig: &meshconfig.MeshConfig{
				OutboundClusterStatName: "%SERVICE%_%SUBSET_NAME%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
				InboundTrafficPolicy:    &meshconfig.MeshConfig_InboundTrafficPolicy{},
				EnableAutoMtls: &wrappers.BoolValue{
					Value: false,
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{
				{
					Name:                 "outbound|8080|foobar|foo.default.svc.cluster.local",
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
					EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
						ServiceName: "outbound|8080|foobar|foo.default.svc.cluster.local",
					},
					AltStatName: "foo.default.svc.cluster.local_foobar_default_8080;",
				},
			},
		},
		{
			name:        "destination rule with subset traffic policy and alt statname",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
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
			meshConfig: &meshconfig.MeshConfig{
				OutboundClusterStatName: "%SERVICE%_%SUBSET_NAME%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
				InboundTrafficPolicy:    &meshconfig.MeshConfig_InboundTrafficPolicy{},
				EnableAutoMtls: &wrappers.BoolValue{
					Value: false,
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
					AltStatName: "foo.default.svc.cluster.local_foobar_default_8080;",
				},
			},
		},
		{
			name:        "destination rule with use client protocol traffic policy",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
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
		{
			name:        "destination rule with maxRequestsPerConnection",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxRetries:               10,
							MaxRequestsPerConnection: 10,
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with maxConcurrentStreams",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     http2Service,
			port:        http2ServicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxRetries:           10,
							MaxConcurrentStreams: 10,
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with http2UpgradePolicy and maxConcurrentStreams",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxConcurrentStreams: 200,
							H2UpgradePolicy:      networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with http2UpgradePolicy on existing http2 cluster and maxConcurrentStreams",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     http2Service,
			port:        http2ServicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxConcurrentStreams: 200,
							H2UpgradePolicy:      networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "subset without labels in both",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host:    "foo.example.com",
				Subsets: []*networking.Subset{{Name: "v1"}},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.example.com",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS},
			}},
		},
		{
			name:        "subset without labels in dest rule",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host:    "foo.example.com",
				Subsets: []*networking.Subset{{Name: "v1"}},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.example.com",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS},
			}},
		},
		{
			name:        "subset with labels in both",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.example.com",
				Subsets: []*networking.Subset{{
					Name:   "v1",
					Labels: map[string]string{"foo": "bar"},
				}},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.example.com",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS},
			}},
		},
		{
			name:        "subset with labels in both, not matching",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.example.com",
				Subsets: []*networking.Subset{{
					Name:   "v1",
					Labels: map[string]string{"foo": "not-match"},
				}},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "subset without labels in both and resolution of DNS_ROUND_ROBIN",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSRoundRobinLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host:    "foo.example.com",
				Subsets: []*networking.Subset{{Name: "v1"}},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.example.com",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS},
			}},
		},
		{
			name:        "subset without labels in dest rule and a resolution of DNS_ROUND_ROBIN",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSRoundRobinLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host:    "foo.example.com",
				Subsets: []*networking.Subset{{Name: "v1"}},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.example.com",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS},
			}},
		},
		{
			name:        "subset with labels in both",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSRoundRobinLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.example.com",
				Subsets: []*networking.Subset{{
					Name:   "v1",
					Labels: map[string]string{"foo": "bar"},
				}},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.example.com",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS},
			}},
		},
		{
			name:        "subset with labels in both, not matching",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS}},
			clusterMode: DefaultClusterMode,
			service: &model.Service{
				Hostname:   host.Name("foo.example.com"),
				Ports:      servicePort,
				Resolution: model.DNSRoundRobinLB,
				Attributes: model.ServiceAttributes{
					Namespace: TestServiceNamespace,
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			port:      servicePort[0],
			proxyView: model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.example.com",
				Subsets: []*networking.Subset{{
					Name:   "v1",
					Labels: map[string]string{"foo": "not-match"},
				}},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with tls mode SIMPLE",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with tls mode MUTUAL",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_MUTUAL},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "destination rule with tls mode ISTIO_MUTUAL",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "port level destination rule with tls mode SIMPLE",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
						{
							Port: &networking.PortSelector{Number: uint32(servicePort[0].Port)},
							Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "port level destination rule with tls mode MUTUAL",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
						{
							Port: &networking.PortSelector{Number: uint32(servicePort[0].Port)},
							Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_MUTUAL},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "port level destination rule with tls mode ISTIO_MUTUAL",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
						{
							Port: &networking.PortSelector{Number: uint32(servicePort[0].Port)},
							Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{},
		},
		{
			name:        "subset destination rule with tls mode SIMPLE",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name: "v1",
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.default.svc.cluster.local",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				EdsClusterConfig:     &cluster.Cluster_EdsClusterConfig{ServiceName: "outbound|8080|v1|foo.default.svc.cluster.local"},
			}},
		},
		{
			name:        "subset destination rule with tls mode MUTUAL",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name: "v1",
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_MUTUAL},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.default.svc.cluster.local",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				EdsClusterConfig:     &cluster.Cluster_EdsClusterConfig{ServiceName: "outbound|8080|v1|foo.default.svc.cluster.local"},
			}},
		},
		{
			name:        "subset destination rule with tls mode MUTUAL",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				Subsets: []*networking.Subset{
					{
						Name: "v1",
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
						},
					},
				},
			},
			expectedSubsetClusters: []*cluster.Cluster{{
				Name:                 "outbound|8080|v1|foo.default.svc.cluster.local",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				EdsClusterConfig:     &cluster.Cluster_EdsClusterConfig{ServiceName: "outbound|8080|v1|foo.default.svc.cluster.local"},
			}},
		},
		{
			name:        "destination rule with empty retry budget",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
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
							RetryBudget: &networking.TrafficPolicy_RetryBudget{},
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
								RetryBudget: &cluster.CircuitBreakers_Thresholds_RetryBudget{
									BudgetPercent:       &xdstype.Percent{Value: 0.2},
									MinRetryConcurrency: &wrappers.UInt32Value{Value: 3},
								},
							},
						},
					},
				},
			},
		},
		{
			name:        "destination rule with retry budget",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
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
							RetryBudget: &networking.TrafficPolicy_RetryBudget{
								Percent:             wrappers.Double(0.3),
								MinRetryConcurrency: uint32(4),
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
								RetryBudget: &cluster.CircuitBreakers_Thresholds_RetryBudget{
									BudgetPercent:       &xdstype.Percent{Value: 0.3},
									MinRetryConcurrency: &wrappers.UInt32Value{Value: 4},
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
			instances := []*model.ServiceInstance{
				{
					Service:     tt.service,
					ServicePort: tt.port,
					Endpoint: &model.IstioEndpoint{
						ServicePortName: tt.port.Name,
						Addresses:       []string{"192.168.1.1"},
						EndpointPort:    10001,
						Locality: model.Locality{
							ClusterID: "",
							Label:     "region1/zone1/subzone1",
						},
						Labels:  tt.service.Attributes.Labels,
						TLSMode: model.IstioMutualTLSModeLabel,
					},
				},
				{
					Service:     tt.service,
					ServicePort: tt.port,
					Endpoint: &model.IstioEndpoint{
						ServicePortName: tt.port.Name,
						Addresses:       []string{"192.168.1.2", "2001:1::2"},
						EndpointPort:    10001,
						Locality: model.Locality{
							ClusterID: "",
							Label:     "region1/zone1/subzone1",
						},
						Labels:  tt.service.Attributes.Labels,
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
				Instances:      instances,
				ConfigPointers: []*config.Config{cfg},
				Services:       []*model.Service{tt.service},
				MeshConfig:     tt.meshConfig,
			})
			proxy := cg.SetupProxy(nil)
			cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, nil)

			tt.cluster.CommonLbConfig = &cluster.Cluster_CommonLbConfig{}

			ec := newClusterWrapper(tt.cluster)
			// Set cluster wrapping with HTTP2 options if port protocol is HTTP2
			if tt.port.Protocol == protocol.HTTP2 {
				setH2Options(ec)
			}
			destRule := proxy.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, proxy, tt.service.Hostname)
			eb := endpoints.NewCDSEndpointBuilder(proxy, cb.req.Push, tt.cluster.Name,
				model.TrafficDirectionOutbound, "", tt.service.Hostname, tt.port.Port,
				tt.service, destRule)
			subsetClusters := cb.applyDestinationRule(ec, tt.clusterMode, tt.service, tt.port, eb, destRule.GetRule(), nil)
			if len(subsetClusters) != len(tt.expectedSubsetClusters) {
				t.Fatalf("Unexpected subset clusters want %v, got %v. keys=%v",
					len(tt.expectedSubsetClusters), len(subsetClusters), xdstest.MapKeys(xdstest.ExtractClusters(subsetClusters)))
			}
			if len(tt.expectedSubsetClusters) > 0 {
				compareClusters(t, tt.expectedSubsetClusters[0], subsetClusters[0])
			}
			// Validate that use client protocol configures cluster correctly.
			if tt.destRule != nil && tt.destRule.TrafficPolicy != nil && tt.destRule.TrafficPolicy.GetConnectionPool().GetHttp().GetUseClientProtocol() {
				if ec.httpProtocolOptions == nil {
					t.Errorf("Expected cluster %s to have http protocol options but not found", tt.cluster.Name)
				}
				if ec.httpProtocolOptions.UpstreamProtocolOptions == nil &&
					ec.httpProtocolOptions.GetUseDownstreamProtocolConfig() == nil {
					t.Errorf("Expected cluster %s to have downstream protocol options but not found", tt.cluster.Name)
				}
			}

			// Validate that max requests per connection configures cluster correctly.
			if tt.destRule != nil && tt.destRule.TrafficPolicy != nil && tt.destRule.TrafficPolicy.GetConnectionPool().GetHttp().GetMaxRequestsPerConnection() > 0 {
				if ec.httpProtocolOptions == nil {
					t.Errorf("Expected cluster %s to have http protocol options but not found", tt.cluster.Name)
				}
				if ec.httpProtocolOptions.CommonHttpProtocolOptions == nil {
					t.Errorf("Expected cluster %s to have common http protocol options but not found", tt.cluster.Name)
				}
				if ec.httpProtocolOptions.CommonHttpProtocolOptions.MaxRequestsPerConnection.GetValue() !=
					uint32(tt.destRule.TrafficPolicy.GetConnectionPool().GetHttp().MaxRequestsPerConnection) {
					t.Errorf("Unexpected max_requests_per_connection found")
				}
			}

			if tt.destRule.GetTrafficPolicy().GetConnectionPool().GetHttp().GetMaxConcurrentStreams() > 0 {
				if ec.httpProtocolOptions == nil {
					t.Errorf("Expected cluster %s to have http protocol options but not found", tt.cluster.Name)
				}
				if ec.httpProtocolOptions.GetExplicitHttpConfig() == nil {
					t.Errorf("Expected cluster %s to have explicit http config but not found", tt.cluster.Name)
				}
				if ec.httpProtocolOptions.GetExplicitHttpConfig().GetHttp2ProtocolOptions() == nil {
					t.Errorf("Expected cluster %s to have HTTP2 protocol options but not found", tt.cluster.Name)
				}
				if ec.httpProtocolOptions.GetExplicitHttpConfig().GetHttp2ProtocolOptions().GetMaxConcurrentStreams().GetValue() !=
					uint32(tt.destRule.TrafficPolicy.GetConnectionPool().GetHttp().MaxConcurrentStreams) {
					t.Errorf("Unexpected max_concurrent_streams found")
				}
			}

			// Validate that alpn_override is correctly configured on cluster given a TLS mode.
			if tt.destRule.GetTrafficPolicy().GetTls() != nil {
				verifyALPNOverride(t, tt.cluster.Metadata, tt.destRule.TrafficPolicy.Tls.Mode)
			}
			if len(tt.destRule.GetSubsets()) > 0 {
				for _, c := range subsetClusters {
					var subsetName string
					if tt.clusterMode == DefaultClusterMode {
						subsetName = strings.Split(c.Name, "|")[2]
					} else {
						subsetName = strings.Split(c.Name, ".")[2]
					}
					for _, subset := range tt.destRule.Subsets {
						if subset.Name == subsetName {
							if subset.GetTrafficPolicy().GetTls() != nil {
								verifyALPNOverride(t, c.Metadata, subset.TrafficPolicy.Tls.Mode)
							}
						}
					}
				}
			}

			// Validate that ORIGINAL_DST cluster does not have load assignments
			for _, subset := range subsetClusters {
				if subset.GetType() == cluster.Cluster_ORIGINAL_DST && subset.GetLoadAssignment() != nil {
					t.Errorf("Passthrough subsets should not have load assignments")
				}
			}
			if ec.cluster.GetType() == cluster.Cluster_ORIGINAL_DST && ec.cluster.GetLoadAssignment() != nil {
				t.Errorf("Passthrough should not have load assignments")
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
	if ec.AltStatName != "" {
		if ec.AltStatName != gc.AltStatName {
			t.Errorf("Unexpected alt stat name want %s, got %s", ec.AltStatName, gc.AltStatName)
		}
	}
}

func verifyALPNOverride(t *testing.T, md *core.Metadata, tlsMode networking.ClientTLSSettings_TLSmode) {
	istio, ok := md.FilterMetadata[util.IstioMetadataKey]
	if tlsMode == networking.ClientTLSSettings_SIMPLE || tlsMode == networking.ClientTLSSettings_MUTUAL {
		if !ok {
			t.Errorf("Istio metadata not found")
		}
		alpnOverride, found := istio.Fields[util.AlpnOverrideMetadataKey]
		if found {
			if alpnOverride.GetStringValue() != "false" {
				t.Errorf("alpn_override:%s tlsMode:%s, should be false for either TLS mode SIMPLE or MUTUAL", alpnOverride, tlsMode)
			}
		} else {
			t.Errorf("alpn_override metadata should be written for either TLS mode SIMPLE or MUTUAL")
		}
	} else if ok {
		alpnOverride, found := istio.Fields[util.AlpnOverrideMetadataKey]
		if found {
			t.Errorf("alpn_override:%s tlsMode:%s, alpn_override metadata should not be written if TLS mode is neither SIMPLE nor MUTUAL",
				alpnOverride.GetStringValue(), tlsMode)
		}
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
					InitialFetchTimeout: durationpb.New(0),
					ResourceApiVersion:  core.ApiVersion_V3,
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
				AltStatName:          "foo;",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				CommonLbConfig:       &cluster.Cluster_CommonLbConfig{},
				ConnectTimeout:       &durationpb.Duration{Seconds: 10, Nanos: 1},
				CircuitBreakers: &cluster.CircuitBreakers{
					Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
				},
				Filters:  []*cluster.Filter{xdsfilters.TCPClusterMx},
				LbPolicy: defaultLBAlgorithm(),
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
							},
						},
					},
				},
				EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
					ServiceName: "foo",
					EdsConfig: &core.ConfigSource{
						ConfigSourceSpecifier: &core.ConfigSource_Ads{
							Ads: &core.AggregatedConfigSource{},
						},
						InitialFetchTimeout: durationpb.New(0),
						ResourceApiVersion:  core.ApiVersion_V3,
					},
				},
			},
		},
		{
			name:        "static external cluster with . in the name",
			clusterName: "foo.bar.com",
			discovery:   cluster.Cluster_EDS,
			endpoints:   nil,
			direction:   model.TrafficDirectionOutbound,
			external:    false,
			expectedCluster: &cluster.Cluster{
				Name:                 "foo.bar.com",
				AltStatName:          "foo.bar.com;",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				CommonLbConfig:       &cluster.Cluster_CommonLbConfig{},
				ConnectTimeout:       &durationpb.Duration{Seconds: 10, Nanos: 1},
				CircuitBreakers: &cluster.CircuitBreakers{
					Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
				},
				Filters:  []*cluster.Filter{xdsfilters.TCPClusterMx},
				LbPolicy: defaultLBAlgorithm(),
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
							},
						},
					},
				},
				EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
					ServiceName: "foo.bar.com",
					EdsConfig: &core.ConfigSource{
						ConfigSourceSpecifier: &core.ConfigSource_Ads{
							Ads: &core.AggregatedConfigSource{},
						},
						InitialFetchTimeout: durationpb.New(0),
						ResourceApiVersion:  core.ApiVersion_V3,
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
				AltStatName:          "foo;",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
				CommonLbConfig:       &cluster.Cluster_CommonLbConfig{},
				ConnectTimeout:       &durationpb.Duration{Seconds: 10, Nanos: 1},
				Filters:              []*cluster.Filter{xdsfilters.TCPClusterMx},
				LbPolicy:             defaultLBAlgorithm(),
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
						}},
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mesh := testMesh()
			cg := NewConfigGenTest(t, TestOptions{MeshConfig: mesh})
			proxy := cg.SetupProxy(nil)
			cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, nil)
			service := &model.Service{
				Ports: model.PortList{
					servicePort,
				},
				Hostname:     "host",
				MeshExternal: false,
				Attributes:   model.ServiceAttributes{Name: "svc", Namespace: "default"},
			}
			defaultCluster := cb.buildCluster(tt.clusterName, tt.discovery, tt.endpoints, tt.direction, servicePort, service, nil, "")
			eb := endpoints.NewCDSEndpointBuilder(proxy, cb.req.Push, tt.clusterName,
				tt.direction, "", service.Hostname, servicePort.Port,
				service, nil)
			if defaultCluster != nil {
				_ = cb.applyDestinationRule(defaultCluster, DefaultClusterMode, service, servicePort, eb, nil, nil)
			}

			if diff := cmp.Diff(defaultCluster.build(), tt.expectedCluster, protocmp.Transform()); diff != "" {
				t.Errorf("Unexpected default cluster, diff: %v", diff)
			}
		})
	}
}

func TestClusterDnsConfig(t *testing.T) {
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: protocol.HTTP,
	}

	endpoints := []*endpoint.LocalityLbEndpoints{
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
	}

	cases := []struct {
		name          string
		udpMaxQueries uint32
		dnsJitter     time.Duration
		proxy         *model.Proxy
	}{
		{
			name:          "Dual stack proxy",
			udpMaxQueries: 99,
			dnsJitter:     0 * time.Millisecond,
			proxy:         &dualStackProxy,
		},
		{
			name:          "IPv4 proxy",
			udpMaxQueries: 0,
			dnsJitter:     1 * time.Millisecond,
			proxy:         getProxy(),
		},
		{
			name:          "IPv6 proxy",
			udpMaxQueries: 1,
			dnsJitter:     5 * time.Millisecond,
			proxy:         getIPv6Proxy(),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.PilotDNSCaresUDPMaxQueries, tt.udpMaxQueries)
			test.SetForTest(t, &features.PilotDNSJitterDurationEnv, tt.dnsJitter)
			mesh := testMesh()
			cg := NewConfigGenTest(t, TestOptions{MeshConfig: mesh})
			cb := NewClusterBuilder(cg.SetupProxy(tt.proxy), &model.PushRequest{Push: cg.PushContext()}, nil)
			service := &model.Service{
				Ports: model.PortList{
					servicePort,
				},
				Hostname:     "host",
				MeshExternal: false,
				Attributes:   model.ServiceAttributes{Name: "svc", Namespace: "default"},
			}
			defaultCluster := cb.buildCluster("my-cluster", cluster.Cluster_STRICT_DNS, endpoints, model.TrafficDirectionOutbound, servicePort, service, nil, "")
			c := defaultCluster.build()

			dnsConfig := new(cares.CaresDnsResolverConfig)
			if err := c.TypedDnsResolverConfig.TypedConfig.UnmarshalTo(dnsConfig); err != nil {
				t.Errorf("Unexpected TypedDnsResolverConfig type, expected cares dns resolver, got: %v", c.TypedDnsResolverConfig.TypedConfig.TypeUrl)
			}
			if dnsConfig.UdpMaxQueries.Value != tt.udpMaxQueries {
				t.Errorf("Unexpected UdpMaxQueries, expected : %v, got: %v", tt.udpMaxQueries, dnsConfig.UdpMaxQueries.Value)
			}
			//nolint:staticcheck // DnsJitter is deprecated
			if c.DnsJitter.AsDuration() != tt.dnsJitter {
				t.Errorf("Unexpected dnsJitter, expected : %v, got: %v", tt.dnsJitter, c.DnsJitter.AsDuration())
			}
		})
	}
}

func TestClusterDnsLookupFamily(t *testing.T) {
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: protocol.HTTP,
	}

	endpoints := []*endpoint.LocalityLbEndpoints{
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
	}

	cases := []struct {
		name           string
		clusterName    string
		discovery      cluster.Cluster_DiscoveryType
		proxy          *model.Proxy
		dualStack      bool
		expectedFamily cluster.Cluster_DnsLookupFamily
	}{
		{
			name:           "all ipv4, dual stack disabled",
			clusterName:    "foo",
			discovery:      cluster.Cluster_STRICT_DNS,
			proxy:          getProxy(),
			dualStack:      false,
			expectedFamily: cluster.Cluster_V4_ONLY,
		},
		{
			name:           "all ipv4, dual stack enabled",
			clusterName:    "foo",
			discovery:      cluster.Cluster_STRICT_DNS,
			proxy:          getProxy(),
			dualStack:      true,
			expectedFamily: cluster.Cluster_V4_ONLY,
		},
		{
			name:           "all ipv6, dual stack disabled",
			clusterName:    "foo",
			discovery:      cluster.Cluster_STRICT_DNS,
			proxy:          getIPv6Proxy(),
			dualStack:      false,
			expectedFamily: cluster.Cluster_V6_ONLY,
		},
		{
			name:           "all ipv6, dual stack enabled",
			clusterName:    "foo",
			discovery:      cluster.Cluster_STRICT_DNS,
			proxy:          getIPv6Proxy(),
			dualStack:      true,
			expectedFamily: cluster.Cluster_V6_ONLY,
		},
		{
			name:           "ipv4 and ipv6, dual stack disabled",
			clusterName:    "foo",
			discovery:      cluster.Cluster_STRICT_DNS,
			proxy:          &dualStackProxy,
			dualStack:      false,
			expectedFamily: cluster.Cluster_V4_ONLY,
		},
		{
			name:           "ipv4 and ipv6, dual stack enabled",
			clusterName:    "foo",
			discovery:      cluster.Cluster_STRICT_DNS,
			proxy:          &dualStackProxy,
			dualStack:      true,
			expectedFamily: cluster.Cluster_ALL,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableDualStack, tt.dualStack)
			mesh := testMesh()
			cg := NewConfigGenTest(t, TestOptions{MeshConfig: mesh})
			cb := NewClusterBuilder(cg.SetupProxy(tt.proxy), &model.PushRequest{Push: cg.PushContext()}, nil)
			service := &model.Service{
				Ports: model.PortList{
					servicePort,
				},
				Hostname:     "host",
				MeshExternal: false,
				Attributes:   model.ServiceAttributes{Name: "svc", Namespace: "default"},
			}
			defaultCluster := cb.buildCluster(tt.clusterName, tt.discovery, endpoints, model.TrafficDirectionOutbound, servicePort, service, nil, "")
			c := defaultCluster.build()

			if c.DnsLookupFamily != tt.expectedFamily {
				t.Errorf("Unexpected DnsLookupFamily, got: %v, want: %v", c.DnsLookupFamily, tt.expectedFamily)
			}
		})
	}
}

func TestBuildLocalityLbEndpoints(t *testing.T) {
	test.SetForTest(t, &features.EnableDualStack, true)
	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID:            "cluster-1",
			RequestedNetworkView: []string{"nw-0", "nw-1"},
		},
	}
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: protocol.HTTP,
	}
	service := &model.Service{
		Hostname: host.Name("*.example.org"),
		Ports:    model.PortList{servicePort},
		Attributes: model.ServiceAttributes{
			Name:      "TestService",
			Namespace: "test-ns",
		},
	}

	buildMetadata := func(networkID network.ID, tlsMode, workloadname, namespace string,
		clusterID istiocluster.ID, lbls labels.Instance,
	) *core.Metadata {
		newmeta := &core.Metadata{}
		util.AppendLbEndpointMetadata(&model.EndpointMetadata{
			Network:      networkID,
			TLSMode:      tlsMode,
			WorkloadName: workloadname,
			Namespace:    namespace,
			ClusterID:    clusterID,
			Labels:       lbls,
		}, newmeta)
		return newmeta
	}

	cases := []struct {
		name      string
		mesh      *meshconfig.MeshConfig
		labels    labels.Instance
		instances []*model.ServiceInstance
		expected  []*endpoint.LocalityLbEndpoints
	}{
		{
			name: "basics",
			mesh: testMesh(),
			instances: []*model.ServiceInstance{
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Addresses:    []string{"192.168.1.1"},
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
						Addresses:    []string{"192.168.1.2", "2001:1::2"},
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
						Addresses:    []string{"192.168.1.3"},
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
						Addresses:    []string{"192.168.1.4"},
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
							Metadata: buildMetadata("nw-0", "", "workload-1", "namespace-1", "cluster-1", map[string]string{}),
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
									AdditionalAddresses: []*endpoint.Endpoint_AdditionalAddress{
										{
											Address: &core.Address{
												Address: &core.Address_SocketAddress{
													SocketAddress: &core.SocketAddress{
														Address: "2001:1::2",
														PortSpecifier: &core.SocketAddress_PortValue{
															PortValue: 10001,
														},
													},
												},
											},
										},
									},
								},
							},
							Metadata: buildMetadata("nw-1", "", "workload-2", "namespace-2", "cluster-2", map[string]string{}),
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
							Metadata: buildMetadata("", "", "workload-3", "namespace-3", "cluster-3", map[string]string{}),
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
			mesh: withClusterLocalHosts(testMesh(), "*.example.org"),
			instances: []*model.ServiceInstance{
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Addresses:    []string{"192.168.1.1"},
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
						Addresses:    []string{"192.168.1.2"},
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
							Metadata: buildMetadata("", "", "", "", "cluster-1", map[string]string{}),
							LoadBalancingWeight: &wrappers.UInt32Value{
								Value: 30,
							},
						},
					},
				},
			},
		},
		{
			name:   "subset cluster endpoints with labels",
			mesh:   testMesh(),
			labels: labels.Instance{"version": "v1"},
			instances: []*model.ServiceInstance{
				{
					Service:     service,
					ServicePort: servicePort,
					Endpoint: &model.IstioEndpoint{
						Addresses:    []string{"192.168.1.1"},
						EndpointPort: 10001,
						WorkloadName: "workload-1",
						Namespace:    "namespace-1",
						Labels: map[string]string{
							"version": "v1",
							"app":     "example",
						},
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
						Addresses:    []string{"192.168.1.2"},
						EndpointPort: 10001,
						WorkloadName: "workload-2",
						Namespace:    "namespace-2",
						Labels: map[string]string{
							"version": "v2",
							"app":     "example",
						},
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
						Addresses:    []string{"192.168.1.3"},
						EndpointPort: 10001,
						WorkloadName: "workload-3",
						Namespace:    "namespace-3",
						Labels: map[string]string{
							"version": "v3",
							"app":     "example",
						},
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
						Addresses:    []string{"192.168.1.4"},
						EndpointPort: 10001,
						WorkloadName: "workload-1",
						Namespace:    "namespace-1",
						Labels: map[string]string{
							"version": "v4",
							"app":     "example",
						},
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
							Metadata: buildMetadata("nw-0", "", "workload-1", "namespace-1", "cluster-1", map[string]string{
								"version": "v1",
								"app":     "example",
							}),
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
		for _, resolution := range []model.Resolution{model.DNSLB, model.DNSRoundRobinLB} {
			t.Run(fmt.Sprintf("%s_%s", tt.name, resolution), func(t *testing.T) {
				service.Resolution = resolution
				cg := NewConfigGenTest(t, TestOptions{
					MeshConfig: tt.mesh,
					Services:   []*model.Service{service},
					Instances:  tt.instances,
				})

				cb := NewClusterBuilder(cg.SetupProxy(proxy), &model.PushRequest{Push: cg.PushContext()}, nil)
				eb := endpoints.NewCDSEndpointBuilder(
					proxy, cb.req.Push,
					"outbound|8080|v1|foo.com",
					model.TrafficDirectionOutbound, "v1", "foo.com", 8080,
					service, drWithLabels(tt.labels),
				)
				actual := eb.FromServiceEndpoints()
				sortEndpoints(actual)
				if v := cmp.Diff(tt.expected, actual, protocmp.Transform()); v != "" {
					t.Fatalf("Expected (-) != actual (+):\n%s", v)
				}
			})
		}
	}
}

func drWithLabels(lbls labels.Instance) *model.ConsolidatedDestRule {
	return model.ConvertConsolidatedDestRule(&config.Config{
		Meta: config.Meta{},
		Spec: &networking.DestinationRule{
			Subsets: []*networking.Subset{{
				Name:   "v1",
				Labels: lbls,
			}},
		},
	}, nil)
}

func TestConcurrentBuildLocalityLbEndpoints(t *testing.T) {
	test.SetForTest(t, &features.CanonicalServiceForMeshExternalServiceEntry, true)
	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID:            "cluster-1",
			RequestedNetworkView: []string{"nw-0", "nw-1"},
		},
	}
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: protocol.HTTP,
	}
	service := &model.Service{
		Hostname: host.Name("*.example.org"),
		Ports:    model.PortList{servicePort},
		Attributes: model.ServiceAttributes{
			Name:      "TestService",
			Namespace: "test-ns",
			Labels:    map[string]string{"service.istio.io/canonical-name": "example-service"},
		},
		MeshExternal: true,
		Resolution:   model.DNSLB,
	}
	dr := drWithLabels(labels.Instance{"version": "v1"})

	buildMetadata := func(networkID network.ID, tlsMode, workloadname, namespace string,
		clusterID istiocluster.ID, lbls labels.Instance,
	) *core.Metadata {
		newmeta := &core.Metadata{}
		util.AppendLbEndpointMetadata(&model.EndpointMetadata{
			Network:      networkID,
			TLSMode:      tlsMode,
			WorkloadName: workloadname,
			Namespace:    namespace,
			ClusterID:    clusterID,
			Labels:       lbls,
		}, newmeta)
		return newmeta
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"192.168.1.1"},
				EndpointPort: 10001,
				WorkloadName: "workload-1",
				Namespace:    "namespace-1",
				Labels: map[string]string{
					"version": "v1",
					"app":     "example",
				},
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
				Addresses:    []string{"192.168.1.2"},
				EndpointPort: 10001,
				WorkloadName: "workload-2",
				Namespace:    "namespace-2",
				Labels: map[string]string{
					"version": "v2",
					"app":     "example",
				},
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
				Addresses:    []string{"192.168.1.3"},
				EndpointPort: 10001,
				WorkloadName: "workload-3",
				Namespace:    "namespace-3",
				Labels: map[string]string{
					"version": "v3",
					"app":     "example",
				},
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
				Addresses:    []string{"192.168.1.4"},
				EndpointPort: 10001,
				WorkloadName: "workload-1",
				Namespace:    "namespace-1",
				Labels: map[string]string{
					"version": "v4",
					"app":     "example",
				},
				Locality: model.Locality{
					ClusterID: "cluster-1",
					Label:     "region1/zone1/subzone1",
				},
				LbWeight: 30,
				Network:  "filtered-out",
			},
		},
	}

	updatedLbls := labels.Instance{
		"app":                                "example",
		model.IstioCanonicalServiceLabelName: "example-service",
	}
	expected := []*endpoint.LocalityLbEndpoints{
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
					Metadata: buildMetadata("nw-0", "", "workload-1", "test-ns", "cluster-1", updatedLbls),
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 30,
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

	cg := NewConfigGenTest(t, TestOptions{
		MeshConfig: testMesh(),
		Services:   []*model.Service{service},
		Instances:  instances,
	})

	cb := NewClusterBuilder(cg.SetupProxy(proxy), &model.PushRequest{Push: cg.PushContext()}, nil)
	wg := sync.WaitGroup{}
	wg.Add(5)
	var actual []*endpoint.LocalityLbEndpoints
	mu := sync.Mutex{}
	for i := 0; i < 5; i++ {
		go func() {
			eb := endpoints.NewCDSEndpointBuilder(
				proxy, cb.req.Push,
				"outbound|8080|v1|foo.com",
				model.TrafficDirectionOutbound, "v1", "foo.com", 8080,
				service, dr,
			)
			eps := eb.FromServiceEndpoints()
			mu.Lock()
			actual = eps
			mu.Unlock()
			wg.Done()
		}()
	}
	wg.Wait()
	sortEndpoints(actual)
	if v := cmp.Diff(expected, actual, protocmp.Transform()); v != "" {
		t.Fatalf("Expected (-) != actual (+):\n%s", v)
	}
}

func TestConcurrentBuildLocalityLbEndpointsWithMulAddrs(t *testing.T) {
	test.SetForTest(t, &features.CanonicalServiceForMeshExternalServiceEntry, true)
	test.SetForTest(t, &features.EnableDualStack, true)
	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID:            "cluster-1",
			RequestedNetworkView: []string{"nw-0", "nw-1"},
		},
	}
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: protocol.HTTP,
	}
	service := &model.Service{
		Hostname: host.Name("*.example.org"),
		Ports:    model.PortList{servicePort},
		Attributes: model.ServiceAttributes{
			Name:      "TestService",
			Namespace: "test-ns",
			Labels:    map[string]string{"service.istio.io/canonical-name": "example-service"},
		},
		MeshExternal: true,
		Resolution:   model.DNSLB,
	}
	dr := drWithLabels(labels.Instance{"version": "v1"})

	buildMetadata := func(networkID network.ID, tlsMode, workloadname, namespace string,
		clusterID istiocluster.ID, lbls labels.Instance,
	) *core.Metadata {
		newmeta := &core.Metadata{}
		util.AppendLbEndpointMetadata(&model.EndpointMetadata{
			Network:      networkID,
			TLSMode:      tlsMode,
			WorkloadName: workloadname,
			Namespace:    namespace,
			ClusterID:    clusterID,
			Labels:       lbls,
		}, newmeta)
		return newmeta
	}

	instances := []*model.ServiceInstance{
		{
			Service:     service,
			ServicePort: servicePort,
			Endpoint: &model.IstioEndpoint{
				Addresses:    []string{"192.168.1.1", "2001:1::1"},
				EndpointPort: 10001,
				WorkloadName: "workload-1",
				Namespace:    "namespace-1",
				Labels: map[string]string{
					"version": "v1",
					"app":     "example",
				},
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
				Addresses:    []string{"192.168.1.2"},
				EndpointPort: 10001,
				WorkloadName: "workload-2",
				Namespace:    "namespace-2",
				Labels: map[string]string{
					"version": "v2",
					"app":     "example",
				},
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
				Addresses:    []string{"192.168.1.3"},
				EndpointPort: 10001,
				WorkloadName: "workload-3",
				Namespace:    "namespace-3",
				Labels: map[string]string{
					"version": "v3",
					"app":     "example",
				},
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
				Addresses:    []string{"192.168.1.4"},
				EndpointPort: 10001,
				WorkloadName: "workload-1",
				Namespace:    "namespace-1",
				Labels: map[string]string{
					"version": "v4",
					"app":     "example",
				},
				Locality: model.Locality{
					ClusterID: "cluster-1",
					Label:     "region1/zone1/subzone1",
				},
				LbWeight: 30,
				Network:  "filtered-out",
			},
		},
	}

	updatedLbls := labels.Instance{
		"app":                                "example",
		model.IstioCanonicalServiceLabelName: "example-service",
	}
	expected := []*endpoint.LocalityLbEndpoints{
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
							AdditionalAddresses: []*endpoint.Endpoint_AdditionalAddress{
								{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address: "2001:1::1",
												PortSpecifier: &core.SocketAddress_PortValue{
													PortValue: 10001,
												},
											},
										},
									},
								},
							},
						},
					},
					Metadata: buildMetadata("nw-0", "", "workload-1", "test-ns", "cluster-1", updatedLbls),
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 30,
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

	cg := NewConfigGenTest(t, TestOptions{
		MeshConfig: testMesh(),
		Services:   []*model.Service{service},
		Instances:  instances,
	})

	cb := NewClusterBuilder(cg.SetupProxy(proxy), &model.PushRequest{Push: cg.PushContext()}, nil)
	wg := sync.WaitGroup{}
	wg.Add(5)
	var actual []*endpoint.LocalityLbEndpoints
	mu := sync.Mutex{}
	for i := 0; i < 5; i++ {
		go func() {
			eb := endpoints.NewCDSEndpointBuilder(
				proxy, cb.req.Push,
				"outbound|8080|v1|foo.com",
				model.TrafficDirectionOutbound, "v1", "foo.com", 8080,
				service, dr,
			)
			eps := eb.FromServiceEndpoints()
			mu.Lock()
			actual = eps
			mu.Unlock()
			wg.Done()
		}()
	}
	wg.Wait()
	sortEndpoints(actual)
	if v := cmp.Diff(expected, actual, protocmp.Transform()); v != "" {
		t.Fatalf("Expected (-) != actual (+):\n%s", v)
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

			cb := NewClusterBuilder(cg.SetupProxy(proxy), &model.PushRequest{Push: cg.PushContext()}, nil)
			passthrough := cb.buildInboundPassthroughCluster()
			ips := sets.New[string]()
			ips.Insert(passthrough.GetUpstreamBindConfig().GetSourceAddress().Address)
			for _, extra := range passthrough.GetUpstreamBindConfig().GetExtraSourceAddresses() {
				ips.Insert(extra.GetAddress().GetAddress())
			}
			want := sets.New[string]()
			if tt.ipv4Expected {
				want.Insert("127.0.0.6")
			}
			if tt.ipv6Expected {
				want.Insert("::6")
			}
			assert.Equal(t, want, ips)
			// Validate that Passthrough Cluster LB Policy is set correctly.
			if passthrough.GetType() != cluster.Cluster_ORIGINAL_DST || passthrough.GetLbPolicy() != cluster.Cluster_CLUSTER_PROVIDED {
				t.Errorf("Unexpected Discovery type or Lb policy, got Discovery type: %v, Lb Policy: %v", passthrough.GetType(), passthrough.GetLbPolicy())
			}
		})
	}
}

func newTestCluster() *clusterWrapper {
	return newClusterWrapper(&cluster.Cluster{
		Name: "test-cluster",
	})
}

func newH2TestCluster() *clusterWrapper {
	mc := newClusterWrapper(&cluster.Cluster{
		Name: "test-cluster",
	})
	setH2Options(mc)
	return mc
}

func newDownstreamTestCluster() *clusterWrapper {
	cb := NewClusterBuilder(newSidecarProxy(), nil, model.DisabledCache{})
	mc := newClusterWrapper(&cluster.Cluster{
		Name: "test-cluster",
	})
	cb.setUseDownstreamProtocol(mc)
	return mc
}

func newSidecarProxy() *model.Proxy {
	return &model.Proxy{Type: model.SidecarProxy, Metadata: &model.NodeMetadata{}}
}

func newGatewayProxy() *model.Proxy {
	return &model.Proxy{Type: model.Router, Metadata: &model.NodeMetadata{}}
}

// Helper function to extract TLS context from a cluster
func getTLSContext(t *testing.T, c *cluster.Cluster) *tls.UpstreamTlsContext {
	t.Helper()
	if c.TransportSocket == nil {
		return nil
	}
	tlsContext := &tls.UpstreamTlsContext{}
	err := c.TransportSocket.GetTypedConfig().UnmarshalTo(tlsContext)
	if err != nil {
		t.Fatalf("Failed to unmarshall tls context: %v", err)
	}
	return tlsContext
}

func TestShouldH2Upgrade(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		port           *model.Port
		mesh           *meshconfig.MeshConfig
		connectionPool *networking.ConnectionPoolSettings

		upgrade bool
	}{
		{
			name:        "mesh upgrade - dr default",
			clusterName: "bar",
			port:        &model.Port{Protocol: protocol.HTTP},
			mesh:        &meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT,
				},
			},
			upgrade: true,
		},
		{
			name:        "mesh default - dr upgrade non http port",
			clusterName: "bar",
			port:        &model.Port{Protocol: protocol.Unsupported},
			mesh:        &meshconfig.MeshConfig{},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
				},
			},
			upgrade: true,
		},
		{
			name:        "mesh no_upgrade - dr default",
			clusterName: "bar",
			port:        &model.Port{Protocol: protocol.HTTP},
			mesh:        &meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_DO_NOT_UPGRADE},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT,
				},
			},
			upgrade: false,
		},
		{
			name:        "mesh no_upgrade - dr upgrade",
			clusterName: "bar",
			port:        &model.Port{Protocol: protocol.HTTP},
			mesh:        &meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_DO_NOT_UPGRADE},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
				},
			},
			upgrade: true,
		},
		{
			name:        "mesh upgrade - dr no_upgrade",
			clusterName: "bar",
			port:        &model.Port{Protocol: protocol.HTTP},
			mesh:        &meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DO_NOT_UPGRADE,
				},
			},
			upgrade: false,
		},
		{
			name:        "mesh upgrade - dr useClientProtocol",
			clusterName: "bar",
			port:        &model.Port{Protocol: protocol.HTTP},
			mesh:        &meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					UseClientProtocol: true,
				},
			},
			upgrade: false,
		},
		{
			name:        "non-http",
			clusterName: "bar",
			port:        &model.Port{Protocol: protocol.Unsupported},
			mesh:        &meshconfig.MeshConfig{H2UpgradePolicy: meshconfig.MeshConfig_UPGRADE},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					H2UpgradePolicy: networking.ConnectionPoolSettings_HTTPSettings_DEFAULT,
				},
			},
			upgrade: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upgrade := shouldH2Upgrade(test.clusterName, test.port, test.mesh, test.connectionPool)

			if upgrade != test.upgrade {
				t.Fatalf("got: %t, want: %t (%v, %v)", upgrade, test.upgrade, test.mesh.H2UpgradePolicy, test.connectionPool.Http.H2UpgradePolicy)
			}
		})
	}
}

// nolint
func TestIsHttp2Cluster(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *clusterWrapper
		isHttp2Cluster bool // revive:disable-line
	}{
		{
			name:           "with no h2 options",
			cluster:        newTestCluster(),
			isHttp2Cluster: false,
		},
		{
			name:           "with h2 options",
			cluster:        newH2TestCluster(),
			isHttp2Cluster: true,
		},
		{
			name:           "with downstream config and h2 options",
			cluster:        newDownstreamTestCluster(),
			isHttp2Cluster: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isHttp2Cluster := isHttp2Cluster(tt.cluster) // revive:disable-line
			if isHttp2Cluster != tt.isHttp2Cluster {
				t.Errorf("got: %t, want: %t", isHttp2Cluster, tt.isHttp2Cluster)
			}
		})
	}
}

func TestApplyDestinationRuleOSCACert(t *testing.T) {
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
		Hostname:   host.Name("foo.default.svc.cluster.local"),
		Ports:      servicePort,
		Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Namespace: TestServiceNamespace,
		},
	}

	cases := []struct {
		name                      string
		cluster                   *cluster.Cluster
		clusterMode               ClusterMode
		service                   *model.Service
		port                      *model.Port
		proxyView                 model.ProxyView
		destRule                  *networking.DestinationRule
		expectedCaCertificateName string
	}{
		{
			name:        "destination rule with empty string CaCertificates",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxRetries:        10,
							UseClientProtocol: true,
						},
					},
					Tls: &networking.ClientTLSSettings{
						CaCertificates: "",
						Mode:           networking.ClientTLSSettings_SIMPLE,
					},
				},
			},
			expectedCaCertificateName: "system",
		},
		{
			name:        "destination rule with CaCertificates",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxRetries:        10,
							UseClientProtocol: true,
						},
					},
					Tls: &networking.ClientTLSSettings{
						CaCertificates: "root-cert.pem",
						Mode:           networking.ClientTLSSettings_SIMPLE,
					},
				},
			},
			expectedCaCertificateName: "root-cert.pem",
		},
		{
			name:        "destination rule without CaCertificates",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							MaxRetries:        10,
							UseClientProtocol: true,
						},
					},
					Tls: &networking.ClientTLSSettings{
						Mode: networking.ClientTLSSettings_SIMPLE,
					},
				},
			},
			expectedCaCertificateName: "system",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
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
			cl := xdstest.ExtractCluster("outbound|8080||foo.default.svc.cluster.local", cg.Clusters(cg.SetupProxy(nil)))
			_, ca, _ := strings.Cut(xdstest.ExtractClusterSecretResources(t, cl)[0], "file-root:")
			assert.Equal(t, ca, tt.expectedCaCertificateName)
		})
	}
}

func TestApplyTCPKeepalive(t *testing.T) {
	cases := []struct {
		name           string
		mesh           *meshconfig.MeshConfig
		connectionPool *networking.ConnectionPoolSettings
		wantConnOpts   *cluster.UpstreamConnectionOptions
	}{
		{
			name:           "no tcp alive",
			mesh:           &meshconfig.MeshConfig{},
			connectionPool: &networking.ConnectionPoolSettings{},
			wantConnOpts:   nil,
		},
		{
			name: "destination rule tcp alive",
			mesh: &meshconfig.MeshConfig{},
			connectionPool: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{
					TcpKeepalive: &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
						Time: &durationpb.Duration{Seconds: 10},
					},
				},
			},
			wantConnOpts: &cluster.UpstreamConnectionOptions{
				TcpKeepalive: &core.TcpKeepalive{
					KeepaliveTime: &wrappers.UInt32Value{Value: uint32(10)},
				},
			},
		},
		{
			name: "mesh tcp alive",
			mesh: &meshconfig.MeshConfig{
				TcpKeepalive: &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
					Time: &durationpb.Duration{Seconds: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{},
			wantConnOpts: &cluster.UpstreamConnectionOptions{
				TcpKeepalive: &core.TcpKeepalive{
					KeepaliveTime: &wrappers.UInt32Value{Value: uint32(10)},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{})
			proxy := cg.SetupProxy(nil)
			cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, nil)
			mc := &clusterWrapper{
				cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			}

			cb.applyConnectionPool(tt.mesh, mc, tt.connectionPool, nil)

			if !reflect.DeepEqual(tt.wantConnOpts, mc.cluster.UpstreamConnectionOptions) {
				t.Errorf("unexpected tcp keepalive settings, want %v, got %v", tt.wantConnOpts,
					mc.cluster.UpstreamConnectionOptions)
			}
		})
	}
}

func TestApplyConnectionPool(t *testing.T) {
	cases := []struct {
		name                    string
		cluster                 *cluster.Cluster
		httpProtocolOptions     *http.HttpProtocolOptions
		connectionPool          *networking.ConnectionPoolSettings
		retryBudget             *networking.TrafficPolicy_RetryBudget
		expectedHTTPPOpt        *http.HttpProtocolOptions
		expectedCircuitBreakers *cluster.CircuitBreakers
	}{
		{
			name:    "only update IdleTimeout",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			httpProtocolOptions: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					IdleTimeout: &durationpb.Duration{
						Seconds: 22,
					},
				},
			},
			expectedHTTPPOpt: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 22,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			expectedCircuitBreakers: &cluster.CircuitBreakers{
				Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
			},
		},
		{
			name:    "set TCP idle timeout",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			httpProtocolOptions: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
				},
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					IdleTimeout: nil,
				},
			},
			expectedHTTPPOpt: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			expectedCircuitBreakers: &cluster.CircuitBreakers{
				Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
			},
		},
		{
			name:    "ignore TCP idle timeout when HTTP idle timeout is specified",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			httpProtocolOptions: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
				},
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					IdleTimeout: &durationpb.Duration{
						Seconds: 20,
					},
				},
			},
			expectedHTTPPOpt: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 20,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			expectedCircuitBreakers: &cluster.CircuitBreakers{
				Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
			},
		},
		{
			name:    "only update MaxRequestsPerConnection ",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			httpProtocolOptions: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					MaxRequestsPerConnection: 22,
				},
			},
			expectedHTTPPOpt: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 22},
				},
			},
			expectedCircuitBreakers: &cluster.CircuitBreakers{
				Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
			},
		},
		{
			name:    "update multiple fields",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			httpProtocolOptions: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					IdleTimeout: &durationpb.Duration{
						Seconds: 22,
					},
					MaxRequestsPerConnection: 22,
				},
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{
					MaxConnectionDuration: &durationpb.Duration{
						Seconds: 500,
					},
				},
			},
			expectedHTTPPOpt: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 22,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 22},
					MaxConnectionDuration: &durationpb.Duration{
						Seconds: 500,
					},
				},
			},
			expectedCircuitBreakers: &cluster.CircuitBreakers{
				Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
			},
		},
		{
			name:    "default retry budget",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			httpProtocolOptions: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					MaxRequestsPerConnection: 22,
				},
			},
			retryBudget: &networking.TrafficPolicy_RetryBudget{},
			expectedHTTPPOpt: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 22},
				},
			},
			expectedCircuitBreakers: &cluster.CircuitBreakers{
				Thresholds: []*cluster.CircuitBreakers_Thresholds{
					{
						MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
						MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
						MaxConnections:     &wrappers.UInt32Value{Value: math.MaxUint32},
						MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
						TrackRemaining:     true,
						RetryBudget: &cluster.CircuitBreakers_Thresholds_RetryBudget{
							BudgetPercent:       &xdstype.Percent{Value: 0.2},
							MinRetryConcurrency: &wrappers.UInt32Value{Value: 3},
						},
					},
				},
			},
		},
		{
			name:    "retry budget",
			cluster: &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			httpProtocolOptions: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 10},
				},
			},
			connectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					MaxRequestsPerConnection: 22,
				},
			},
			retryBudget: &networking.TrafficPolicy_RetryBudget{
				Percent:             wrappers.Double(0.3),
				MinRetryConcurrency: uint32(4),
			},
			expectedHTTPPOpt: &http.HttpProtocolOptions{
				CommonHttpProtocolOptions: &core.HttpProtocolOptions{
					IdleTimeout: &durationpb.Duration{
						Seconds: 10,
					},
					MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 22},
				},
			},
			expectedCircuitBreakers: &cluster.CircuitBreakers{
				Thresholds: []*cluster.CircuitBreakers_Thresholds{
					{
						MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
						MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
						MaxConnections:     &wrappers.UInt32Value{Value: math.MaxUint32},
						MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
						TrackRemaining:     true,
						RetryBudget: &cluster.CircuitBreakers_Thresholds_RetryBudget{
							BudgetPercent:       &xdstype.Percent{Value: 0.3},
							MinRetryConcurrency: &wrappers.UInt32Value{Value: 4},
						},
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{})
			proxy := cg.SetupProxy(nil)
			cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, nil)
			mc := &clusterWrapper{
				cluster:             tt.cluster,
				httpProtocolOptions: tt.httpProtocolOptions,
			}

			opts := buildClusterOpts{
				mesh:    cb.req.Push.Mesh,
				mutable: mc,
			}
			cb.applyConnectionPool(opts.mesh, opts.mutable, tt.connectionPool, tt.retryBudget)
			// assert httpProtocolOptions
			assert.Equal(t, opts.mutable.httpProtocolOptions.CommonHttpProtocolOptions.IdleTimeout,
				tt.expectedHTTPPOpt.CommonHttpProtocolOptions.IdleTimeout)
			assert.Equal(t, opts.mutable.httpProtocolOptions.CommonHttpProtocolOptions.MaxRequestsPerConnection,
				tt.expectedHTTPPOpt.CommonHttpProtocolOptions.MaxRequestsPerConnection)
			assert.Equal(t, opts.mutable.httpProtocolOptions.CommonHttpProtocolOptions.MaxConnectionDuration,
				tt.expectedHTTPPOpt.CommonHttpProtocolOptions.MaxConnectionDuration)
			assert.Equal(t, opts.mutable.cluster.CircuitBreakers, tt.expectedCircuitBreakers)
		})
	}
}

func TestBuildExternalSDSClusters(t *testing.T) {
	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{
			Raw: map[string]any{
				security.CredentialMetaDataName: "true",
			},
		},
	}

	cases := []struct {
		name         string
		expectedName string
		expectedPath string
	}{
		{
			name:         "uds",
			expectedName: security.SDSExternalClusterName,
			expectedPath: security.CredentialNameSocketPath,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{})
			cb := NewClusterBuilder(cg.SetupProxy(proxy), &model.PushRequest{Push: cg.PushContext()}, nil)
			cluster := cb.buildExternalSDSCluster(security.CredentialNameSocketPath)
			path := cluster.LoadAssignment.Endpoints[0].LbEndpoints[0].GetEndpoint().Address.GetPipe().Path
			anyOptions := cluster.TypedExtensionProtocolOptions[v3.HttpProtocolOptionsType]
			if anyOptions == nil {
				t.Errorf("cluster has no httpProtocolOptions")
			}
			if cluster.Name != tt.expectedName {
				t.Errorf("Unexpected cluster name, got: %v, want: %v", cluster.Name, tt.expectedName)
			}
			if path != tt.expectedPath {
				t.Errorf("Unexpected path, got: %v, want: %v", path, tt.expectedPath)
			}
		})
	}
}

func TestInsecureSkipVerify(t *testing.T) {
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
		Hostname:   host.Name("foo.default.svc.cluster.local"),
		Ports:      servicePort,
		Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Namespace:       TestServiceNamespace,
			ServiceRegistry: provider.External,
		},
	}

	cases := []struct {
		name             string
		cluster          *cluster.Cluster
		clusterMode      ClusterMode
		service          *model.Service
		port             *model.Port
		proxyView        model.ProxyView
		destRule         *networking.DestinationRule
		serviceAcct      []string // SE SAN values
		expectTLSContext *tls.UpstreamTlsContext
	}{
		{
			name:        "With tls mode simple, InsecureSkipVerify is not specified and ca cert is supplied",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:            networking.ClientTLSSettings_SIMPLE,
						CaCertificates:  constants.RootCertFilename,
						Sni:             "foo.default.svc.cluster.local",
						SubjectAltNames: []string{"foo.default.svc.cluster.local"},
					},
				},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"foo.default.svc.cluster.local"})},
							ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
								Name: "file-root:" + constants.RootCertFilename,
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
				Sni: "foo.default.svc.cluster.local",
			},
		},
		{
			name:        "With tls mode simple, InsecureSkipVerify is set false and ca cert is supplied",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:               networking.ClientTLSSettings_SIMPLE,
						CaCertificates:     constants.RootCertFilename,
						Sni:                "foo.default.svc.cluster.local",
						SubjectAltNames:    []string{"foo.default.svc.cluster.local"},
						InsecureSkipVerify: &wrappers.BoolValue{Value: false},
					},
				},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"foo.default.svc.cluster.local"})},
							ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
								Name: "file-root:" + constants.RootCertFilename,
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
				Sni: "foo.default.svc.cluster.local",
			},
		},
		{
			name:        "With tls mode simple, InsecureSkipVerify is set true",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:               networking.ClientTLSSettings_SIMPLE,
						SubjectAltNames:    []string{"foo.default.svc.cluster.local"},
						InsecureSkipVerify: &wrappers.BoolValue{Value: true},
					},
				},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					ValidationContextType: &tls.CommonTlsContext_ValidationContext{},
				},
			},
		},
		{
			name:        "With tls mode simple and CredentialName, InsecureSkipVerify is set true and env",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:               networking.ClientTLSSettings_SIMPLE,
						CredentialName:     "ca-cert",
						Sni:                "foo.default.svc.cluster.local",
						SubjectAltNames:    []string{"foo.default.svc.cluster.local"},
						InsecureSkipVerify: &wrappers.BoolValue{Value: true},
					},
				},
				WorkloadSelector: &v1beta1.WorkloadSelector{},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
				},
				Sni: "foo.default.svc.cluster.local",
			},
		},
		{
			name:        "With tls mode mutual, InsecureSkipVerify is not specified and ca cert is supplied",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:              networking.ClientTLSSettings_MUTUAL,
						ClientCertificate: "cert",
						PrivateKey:        "key",
						CaCertificates:    constants.RootCertFilename,
						Sni:               "foo.default.svc.cluster.local",
						SubjectAltNames:   []string{"foo.default.svc.cluster.local"},
					},
				},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
						{
							Name: "file-cert:cert~key",
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
							DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"foo.default.svc.cluster.local"})},
							ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
								Name: "file-root:" + constants.RootCertFilename,
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
				Sni: "foo.default.svc.cluster.local",
			},
		},
		{
			name:        "With tls mode mutual, InsecureSkipVerify is set false and ca cert is supplied",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:               networking.ClientTLSSettings_MUTUAL,
						ClientCertificate:  "cert",
						PrivateKey:         "key",
						CaCertificates:     constants.RootCertFilename,
						Sni:                "foo.default.svc.cluster.local",
						SubjectAltNames:    []string{"foo.default.svc.cluster.local"},
						InsecureSkipVerify: &wrappers.BoolValue{Value: false},
					},
				},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
						{
							Name: "file-cert:cert~key",
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
							DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"foo.default.svc.cluster.local"})},
							ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
								Name: "file-root:" + constants.RootCertFilename,
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
				Sni: "foo.default.svc.cluster.local",
			},
		},
		{
			name:        "With tls mode mutual, InsecureSkipVerify is set true",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:               networking.ClientTLSSettings_MUTUAL,
						ClientCertificate:  "cert",
						PrivateKey:         "key",
						SubjectAltNames:    []string{"foo.default.svc.cluster.local"},
						InsecureSkipVerify: &wrappers.BoolValue{Value: true},
					},
				},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
						{
							Name: "file-cert:cert~key",
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
			},
		},
		{
			name:        "With tls mode mutual and CredentialName, InsecureSkipVerify is set true",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:               networking.ClientTLSSettings_MUTUAL,
						CredentialName:     "server-cert",
						Sni:                "foo.default.svc.cluster.local",
						SubjectAltNames:    []string{"foo.default.svc.cluster.local"},
						InsecureSkipVerify: &wrappers.BoolValue{Value: true},
					},
				},
				WorkloadSelector: &v1beta1.WorkloadSelector{},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
						{
							Name: "kubernetes://server-cert",
							SdsConfig: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_Ads{
									Ads: &core.AggregatedConfigSource{},
								},
								ResourceApiVersion: core.ApiVersion_V3,
							},
						},
					},
				},
				Sni: "foo.default.svc.cluster.local",
			},
		},
		{
			name:        "With tls mode istio mutual, InsecureSkipVerify is set true",
			cluster:     &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}},
			clusterMode: DefaultClusterMode,
			service:     service,
			port:        servicePort[0],
			proxyView:   model.ProxyViewAll,
			destRule: &networking.DestinationRule{
				Host: "foo.default.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:               networking.ClientTLSSettings_ISTIO_MUTUAL,
						Sni:                "foo.default.svc.cluster.local",
						SubjectAltNames:    []string{"foo.default.svc.cluster.local"},
						InsecureSkipVerify: &wrappers.BoolValue{Value: true},
					},
				},
			},
			expectTLSContext: &tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					TlsParams: &tls.TlsParameters{
						// if not specified, envoy use TLSv1_2 as default for client.
						TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
						TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
					},
					TlsCertificateSdsSecretConfigs: []*tls.SdsSecretConfig{
						{
							Name: authn_model.SDSDefaultResourceName,
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
								InitialFetchTimeout: durationpb.New(time.Second * 0),
							},
						},
					},
					ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &tls.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{"foo.default.svc.cluster.local"})},
							ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
								Name: authn_model.SDSRootResourceName,
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
									InitialFetchTimeout: durationpb.New(time.Second * 0),
								},
							},
						},
					},
					AlpnProtocols: util.ALPNInMeshWithMxc,
				},
				Sni: "foo.default.svc.cluster.local",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targets := []model.ServiceTarget{
				{
					Service: tc.service,
					Port: model.ServiceInstancePort{
						ServicePort: tc.port,
						TargetPort:  10001,
					},
				},
			}

			var cfg *config.Config
			if tc.destRule != nil {
				cfg = &config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "default",
					},
					Spec: tc.destRule,
				}
			}

			cg := NewConfigGenTest(t, TestOptions{
				ConfigPointers: []*config.Config{cfg},
				Services:       []*model.Service{tc.service},
			})

			cg.MemRegistry.WantGetProxyServiceTargets = targets
			proxy := cg.SetupProxy(nil)
			cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, nil)
			ec := newClusterWrapper(tc.cluster)
			tc.cluster.CommonLbConfig = &cluster.Cluster_CommonLbConfig{}
			destRule := proxy.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, proxy, tc.service.Hostname)
			eb := endpoints.NewCDSEndpointBuilder(proxy, cb.req.Push, tc.cluster.Name,
				model.TrafficDirectionOutbound, "", service.Hostname, tc.port.Port,
				service, destRule)

			_ = cb.applyDestinationRule(ec, tc.clusterMode, tc.service, tc.port, eb, destRule.GetRule(), tc.serviceAcct)

			result := getTLSContext(t, ec.cluster)
			if diff := cmp.Diff(result, tc.expectTLSContext, protocmp.Transform()); diff != "" {
				t.Errorf("got diff: `%v", diff)
			}

			if tc.destRule.GetTrafficPolicy().GetTls().Sni == "" {
				assert.Equal(t, ec.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSni, true)
			}

			if tc.destRule.GetTrafficPolicy().GetTls().GetInsecureSkipVerify().GetValue() {
				if ec.httpProtocolOptions != nil {
					assert.Equal(t, ec.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSanValidation, false)
				}
			} else if tc.destRule.GetTrafficPolicy().GetTls().SubjectAltNames != nil && len(tc.destRule.GetTrafficPolicy().GetTls().SubjectAltNames) == 0 {
				assert.Equal(t, ec.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSanValidation, true)
			}
		})
	}
}
