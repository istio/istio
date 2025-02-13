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
	"fmt"
	"net"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"

	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/wellknown"
	"istio.io/istio/pkg/workloadapi"
)

type mockDeltaXdsServer struct {
	handler func(stream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer)
	ctx     context.Context
}

func (t *mockDeltaXdsServer) StreamAggregatedResources(discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return nil
}

func (t *mockDeltaXdsServer) DeltaAggregatedResources(delta discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	t.handler(delta)
	<-t.ctx.Done()
	return nil
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
	add := func(typ, name string) string {
		return fmt.Sprintf("add/%s/%s", typ, name)
	}
	cases := []struct {
		desc            string
		serverResponses []*discovery.DeltaDiscoveryResponse
		expectedRecv    []string
		expectedTree    string
		expectSynced    bool
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
			expectedRecv: []string{add(v3.ClusterType, "test-eds"), add(v3.EndpointType, "test-eds")},
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
			desc:         "put things together",
			expectSynced: true,
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
	for _, tt := range cases {
		t.Run(tt.desc, func(t *testing.T) {
			dh := func(delta discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) {
				for _, response := range tt.serverResponses {
					_ = delta.Send(response)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deltaHandler := &mockDeltaXdsServer{handler: dh, ctx: ctx}
			l, err := net.Listen("tcp", ":0")
			if err != nil {
				t.Errorf("Unable to listen with tcp err %v", err)
				return
			}
			xds := grpc.NewServer()
			discovery.RegisterAggregatedDiscoveryServiceServer(xds, deltaHandler)
			go func() {
				err = xds.Serve(l)
				if err != nil {
					log.Error(err)
				}
			}()
			defer l.Close()
			if err != nil {
				t.Errorf("Could not start serving ads server %v", err)
				return
			}

			tracker := assert.NewTracker[string](t)
			handlers := buildHandlers(t, tracker)

			client := NewDelta(l.Addr().String(), &DeltaADSConfig{}, handlers...)
			go client.Run(ctx)
			wantRecv := slices.Flatten(slices.Map(tt.serverResponses, func(e *discovery.DeltaDiscoveryResponse) []string {
				res := []string{}
				for _, r := range e.Resources {
					res = append(res, "add/"+e.TypeUrl+"/"+r.Name)
				}
				for _, r := range e.RemovedResources {
					res = append(res, "delete/"+e.TypeUrl+"/"+r)
				}
				return res
			}))
			tracker.WaitUnordered(wantRecv...)
			tracker.Empty()
			if tt.expectSynced {
				assert.ChannelIsClosed(t, client.synced)
			}
			// Close the listener and wait for things to gracefully close down
			cancel()
			assert.NoError(t, l.Close())
			assert.EventuallyEqual(t, client.closed.Load, true)
			assert.Equal(t, client.dumpTree(), tt.expectedTree)
		})
	}
}

func buildHandlers(t *testing.T, tracker *assert.Tracker[string]) []Option {
	clusterHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *cluster.Cluster, event Event) {
		tracker.Record(fmt.Sprintf("%v/%v/%v", event, v3.ClusterType, resourceName))
		if event == EventDelete {
			return
		}
		ctx.RegisterDependency(v3.SecretType, xdstest.ExtractClusterSecretResources(t, resourceEntity)...)
		ctx.RegisterDependency(v3.EndpointType, xdstest.ExtractEdsClusterNames([]*cluster.Cluster{resourceEntity})...)
	})
	endpointsHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *endpoint.ClusterLoadAssignment,
		event Event,
	) {
		tracker.Record(fmt.Sprintf("%v/%v/%v", event, v3.EndpointType, resourceName))
	})
	listenerHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *listener.Listener, event Event) {
		tracker.Record(fmt.Sprintf("%v/%v/%v", event, v3.ListenerType, resourceName))
		if event == EventDelete {
			return
		}
		ctx.RegisterDependency(v3.SecretType, xdstest.ExtractListenerSecretResources(t, resourceEntity)...)
		ctx.RegisterDependency(v3.RouteType, xdstest.ExtractRoutesFromListeners([]*listener.Listener{resourceEntity})...)
		// TODO: ECDS
	})
	routesHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *route.RouteConfiguration, event Event) {
		tracker.Record(fmt.Sprintf("%v/%v/%v", event, v3.RouteType, resourceName))
	})
	secretsHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *tls.Secret, event Event) {
		tracker.Record(fmt.Sprintf("%v/%v/%v", event, v3.SecretType, resourceName))
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
	return handlers
}

type fakeClient struct{}

func (f fakeClient) CloseSend() error {
	return nil
}

func (f fakeClient) Send(request *discovery.DeltaDiscoveryRequest) error {
	return nil
}

func (f fakeClient) Recv() (*discovery.DeltaDiscoveryResponse, error) {
	// TODO implement me
	panic("implement me")
}

func TestState(t *testing.T) {
	svcHandler := Register(func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity *workloadapi.Workload, event Event) {
	})
	handlers := []Option{
		svcHandler,
		Watch[*workloadapi.Workload]("*"),
	}
	c := NewDeltaWithBackoffPolicy("", &DeltaADSConfig{}, nil, handlers...)
	c.xdsClient = fakeClient{}
	makeRes := func(s *workloadapi.Workload) *discovery.Resource {
		return &discovery.Resource{
			Name:     s.Uid,
			Resource: protoconv.MessageToAny(s),
		}
	}
	send := func(removes []string, res ...*workloadapi.Workload) {
		t.Log(c.handleDeltaResponse(&discovery.DeltaDiscoveryResponse{
			SystemVersionInfo: "",
			Resources:         slices.Map(res, makeRes),
			RemovedResources:  removes,
			TypeUrl:           v3.WorkloadType,
		}))
		t.Log(c.dumpTree())
	}
	assertResources := func(want ...string) {
		t.Helper()
		child := c.tree[resourceKey{Name: "", TypeURL: v3.WorkloadType}].Children
		have := slices.Sort(slices.Map(child.UnsortedList(), func(e resourceKey) string {
			return e.Name
		}))
		assert.Equal(t, want, have)
	}
	// Create a resource
	send(nil, &workloadapi.Workload{Uid: "a"})
	assertResources("a")

	// Remove it
	send([]string{"a"})
	assertResources()

	// Remove it again; should be a NO-OP
	send([]string{"a"})
	assertResources()

	// Add multiple resources
	send(nil, &workloadapi.Workload{Uid: "a"}, &workloadapi.Workload{Uid: "b"})
	assertResources("a", "b")
	// Add one, update one
	send([]string{"a"}, &workloadapi.Workload{Uid: "b"})
	assertResources("b")
}
