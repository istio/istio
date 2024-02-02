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

package adsc

import (
	"context"
	"net"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/testing/protocmp"

	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

type mockDeltaXdsServer struct{}

var deltaHandler func(stream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error

func (t *mockDeltaXdsServer) StreamAggregatedResources(discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return nil
}

func (t *mockDeltaXdsServer) DeltaAggregatedResources(delta discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return deltaHandler(delta)
}

var testCluster = &cluster.Cluster{
	Name:                 "test-eds",
	ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
	EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
		},
	},
	LbPolicy: cluster.Cluster_ROUND_ROBIN,
	TransportSocket: &core.TransportSocket{
		Name: wellknown.TransportSocketTLS,
		ConfigType: &core.TransportSocket_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
							ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
								Name:      "kubernetes://test",
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
					},
				},
			}),
		},
	},
}

var testClusterNoSecret = &cluster.Cluster{
	Name:                 "test-eds",
	ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
	EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
		},
	},
	LbPolicy: cluster.Cluster_ROUND_ROBIN,
}

var testListener = &listener.Listener{
	Name: "test-listener",
	Address: &core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Protocol: core.SocketAddress_TCP,
				Address:  "0.0.0.0",
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: 8080,
				},
			},
		},
	},
	FilterChains: []*listener.FilterChain{
		{
			Filters: []*listener.Filter{
				{
					Name: wellknown.HTTPConnectionManager,
					ConfigType: &listener.Filter_TypedConfig{
						TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
							RouteSpecifier: &hcm.HttpConnectionManager_Rds{
								Rds: &hcm.Rds{
									RouteConfigName: "test-rds-config",
									ConfigSource: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_Ads{
											Ads: &core.AggregatedConfigSource{},
										},
									},
								},
							},
						}),
					},
				},
			},
			TransportSocket: &core.TransportSocket{
				Name: wellknown.TransportSocketTLS,
				ConfigType: &core.TransportSocket_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
						CommonTlsContext: &tls.CommonTlsContext{
							ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
								CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
									ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
										Name:      "kubernetes://test",
										SdsConfig: authn_model.SDSAdsConfig,
									},
								},
							},
						},
					}),
				},
			},
		},
	},
}

var testListenerNoSecret = &listener.Listener{
	Name: "test-listener",
	Address: &core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Protocol: core.SocketAddress_TCP,
				Address:  "0.0.0.0",
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: 8080,
				},
			},
		},
	},
	FilterChains: []*listener.FilterChain{
		{
			Filters: []*listener.Filter{
				{
					Name: wellknown.HTTPConnectionManager,
					ConfigType: &listener.Filter_TypedConfig{
						TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
							RouteSpecifier: &hcm.HttpConnectionManager_Rds{
								Rds: &hcm.Rds{
									RouteConfigName: "test-rds-config",
									ConfigSource: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_Ads{
											Ads: &core.AggregatedConfigSource{},
										},
									},
								},
							},
						}),
					},
				},
			},
		},
	},
}

var testRouteConfig = &route.RouteConfiguration{
	Name: "test-route",
	// Define the route entries here
	VirtualHosts: []*route.VirtualHost{
		{
			Name:    "test-vhost",
			Domains: []string{"*"},
			Routes: []*route.Route{
				{
					Match: &route.RouteMatch{
						PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &route.Route_Route{
						Route: &route.RouteAction{
							ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "test-cluster"},
						},
					},
				},
			},
		},
	},
}

func TestDeltaClient(t *testing.T) {
	type testCase struct {
		desc                   string
		deltaHandler           func(server discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error
		inClient               *Client
		expectedDeltaResources *Client
		expectedTree           string
	}

	var tests []testCase

	clusterHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *cluster.Cluster, event Event) {
		if event == EventDelete {
			return
		}
		ctx.RegisterDependency(v3.SecretType, xdstest.ExtractClusterSecretResources(t, resourceEntity)...)
		ctx.RegisterDependency(v3.EndpointType, xdstest.ExtractEdsClusterNames([]*cluster.Cluster{resourceEntity})...)
	})
	endpointsHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *endpoint.ClusterLoadAssignment,
		event Event) {
	})
	listenerHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *listener.Listener, event Event) {
		if event == EventDelete {
			return
		}
		ctx.RegisterDependency(v3.SecretType, xdstest.ExtractListenerSecretResources(t, resourceEntity)...)
		ctx.RegisterDependency(v3.RouteType, xdstest.ExtractRoutesFromListeners([]*listener.Listener{resourceEntity})...)
		// TODO: ECDS
	})
	routesHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *route.RouteConfiguration, event Event) {
	})
	secretsHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *tls.Secret, event Event) {
	})

	handlers := []Option{
		clusterHandler,
		Watch[*cluster.Cluster]("*"),
		listenerHandler,
		Watch[*listener.Listener]("*"),
		endpointsHandler,
		routesHandler,
		secretsHandler,
	}

	descs := []struct {
		desc            string
		inClient        *Client
		serverResponses []*discovery.DeltaDiscoveryResponse
		expectedTree    string
	}{
		{
			desc: "initial request cluster with no secret",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: v3.ClusterType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-eds",
							Resource: protoconv.MessageToAny(testClusterNoSecret),
						},
					},
				},
			},
			expectedTree: `CDS/:
  CDS/test-eds:
    EDS/test-eds:
LDS/:
`,
		},
		{
			desc: "initial request cluster with secret",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: v3.ClusterType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-eds",
							Resource: protoconv.MessageToAny(testCluster),
						},
					},
				},
			},
			expectedTree: `CDS/:
  CDS/test-eds:
    EDS/test-eds:
    SDS/kubernetes://test:
LDS/:
`,
		},
		{
			desc: "initial request listener with no secret",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: v3.ListenerType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-listener",
							Resource: protoconv.MessageToAny(testListenerNoSecret),
						},
					},
				},
			},
			expectedTree: `CDS/:
LDS/:
  LDS/test-listener:
    RDS/test-rds-config:
`,
		},
		{
			desc: "initial request listener with secret",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: v3.ListenerType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-listener",
							Resource: protoconv.MessageToAny(testListener),
						},
					},
				},
			},
			expectedTree: `CDS/:
LDS/:
  LDS/test-listener:
    RDS/test-rds-config:
    SDS/kubernetes://test:
`,
		},
		{
			desc: "put things together",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: v3.ClusterType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-eds",
							Resource: protoconv.MessageToAny(testCluster),
						},
					},
				},
				{
					TypeUrl: v3.ListenerType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-listener",
							Resource: protoconv.MessageToAny(testListener),
						},
					},
				},
				{
					TypeUrl: v3.RouteType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-route",
							Resource: protoconv.MessageToAny(testRouteConfig),
						},
					},
				},
			},
			expectedTree: `CDS/:
  CDS/test-eds:
    EDS/test-eds:
    SDS/kubernetes://test:
LDS/:
  LDS/test-listener:
    RDS/test-rds-config:
    SDS/kubernetes://test:
RDS/test-route:
`,
		},

		{
			desc: "begin two clusters then remove one",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: v3.ClusterType,
					Resources: []*discovery.Resource{
						{
							Name:     "test-cluster1",
							Resource: protoconv.MessageToAny(testCluster),
						},
						{
							Name:     "test-cluster2",
							Resource: protoconv.MessageToAny(testClusterNoSecret),
						},
					},
				},
				{
					TypeUrl:          v3.ClusterType,
					Resources:        []*discovery.Resource{},
					RemovedResources: []string{"test-cluster2"},
				},
			},
			expectedTree: `CDS/:
  CDS/test-cluster1:
    EDS/test-eds:
    SDS/kubernetes://test:
LDS/:
`,
		},
	}
	for _, item := range descs {
		desc := item // avoid refer to on-stack-var
		expected := make(map[string]*discovery.DeltaDiscoveryResponse)
		for _, response := range item.serverResponses {
			expected[response.TypeUrl] = response
		}
		tc := testCase{
			desc:     desc.desc,
			inClient: NewDeltaWithBackoffPolicy("", &DeltaADSConfig{}, nil),
			deltaHandler: func(delta discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
				for _, response := range desc.serverResponses {
					_ = delta.Send(response)
				}
				return nil
			},
			expectedDeltaResources: &Client{
				lastReceived: expected,
			},
			expectedTree: desc.expectedTree,
		}
		tests = append(tests, tc)
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			deltaHandler = tt.deltaHandler
			l, err := net.Listen("tcp", ":0")
			if err != nil {
				t.Errorf("Unable to listen with tcp err %v", err)
				return
			}
			tt.inClient.cfg.Address = l.Addr().String()
			xds := grpc.NewServer()
			discovery.RegisterAggregatedDiscoveryServiceServer(xds, new(mockDeltaXdsServer))
			go func() {
				err = xds.Serve(l)
				if err != nil {
					log.Error(err)
				}
			}()
			defer xds.GracefulStop()
			if err != nil {
				t.Errorf("Could not start serving ads server %v", err)
				return
			}

			tt.inClient = NewDeltaWithBackoffPolicy(tt.inClient.cfg.Address, tt.inClient.cfg, nil, handlers...)
			if err := tt.inClient.Run(context.TODO()); err != nil {
				t.Errorf("ADSC: failed running %v", err)
				return
			}
			assert.EventuallyEqual(t, func() bool {
				tt.inClient.mutex.Lock()
				defer tt.inClient.mutex.Unlock()
				rec := tt.inClient.lastReceived

				if rec == nil && len(rec) != len(tt.expectedDeltaResources.lastReceived) {
					return false
				}
				for tpe, rsrcs := range tt.expectedDeltaResources.lastReceived {
					if _, ok := rec[tpe]; !ok {
						return false
					}
					if len(rsrcs.Resources) != len(rec[tpe].Resources) {
						return false
					}
				}
				return true
			}, true, retry.Timeout(time.Second), retry.Delay(time.Millisecond))

			if !cmp.Equal(tt.inClient.lastReceived, tt.expectedDeltaResources.lastReceived, protocmp.Transform()) {
				t.Errorf("%s: expected recv %v got %v", tt.desc, tt.expectedDeltaResources.lastReceived, tt.inClient.lastReceived)
			}

			tree := tt.inClient.dumpTree()
			if diff := cmp.Diff(tt.expectedTree, tree); diff != "" {
				t.Errorf("%s: expected tree %v got %v", tt.desc, tt.expectedTree, tree)
			}
		})
	}
}
