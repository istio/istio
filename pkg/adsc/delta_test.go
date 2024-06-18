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

	"istio.io/api/label"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
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

func TestDeltaClient_handleMCP(t *testing.T) {
	rev := "test-rev"
	store := memory.Make(collections.Pilot)
	respCh := make(chan *discovery.DeltaDiscoveryResponse)
	deltaHandler = func(delta discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
		for response := range respCh {
			_ = delta.Send(response)
		}
		return nil
	}

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Errorf("Unable to listen with tcp err %v", err)
		return
	}
	cfg := &DeltaADSConfig{
		Config: Config{
			Address: l.Addr().String(),
		},
	}
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewDeltaWithBackoffPolicy(cfg.Address, cfg, nil, WatchMcpOptions(rev, WithConfigStore(store))...)
	if err := client.Run(ctx); err != nil {
		t.Errorf("ADSC: failed running %v", err)
		return
	}

	tests := []struct {
		desc                 string
		serverResponses      []*discovery.DeltaDiscoveryResponse
		expectedLastReceived map[string]*discovery.DeltaDiscoveryResponse
		expectedResources    [][]string
		run                  bool
	}{
		{
			run:  true,
			desc: "create-resources",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: gvk.ServiceEntry.String(),
					Resources: []*discovery.Resource{
						{
							Name:     "default/foo1",
							Resource: constructMcpResourceWithOptions("foo1", "foo1.bar.com", "192.1.1.1", "1"),
						},
						{
							Name:     "default/foo2",
							Resource: constructMcpResourceWithOptions("foo2", "foo2.bar.com", "192.1.1.2", "1"),
						},
					},
				},
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo2", "foo2.bar.com", "192.1.1.2"},
			},
		},
		{
			run:  true,
			desc: "create-resources-rev-1",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: gvk.ServiceEntry.String(),
					Resources: []*discovery.Resource{
						{
							Name: "default/foo2",
							Resource: constructMcpResourceWithOptions("foo2", "foo2.bar.com", "192.1.1.2", "1", func(resource *mcp.Resource) {
								resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev+"wrong") // to del
							}),
						},
						{
							Name: "default/foo3",
							Resource: constructMcpResourceWithOptions("foo3", "foo3.bar.com", "192.1.1.3", "1", func(resource *mcp.Resource) {
								resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev) // to add
							}),
						},
					},
				},
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo3", "foo3.bar.com", "192.1.1.3"},
			},
		},
		{
			desc: "create-resources-rev-2",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: gvk.ServiceEntry.String(),
					Resources: []*discovery.Resource{
						{
							Name: "default/foo2",
							Resource: constructMcpResourceWithOptions("foo2", "foo2.bar.com", "192.1.1.2", "1", func(resource *mcp.Resource) {
								resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev) // to add back
							}),
						},
						{
							Name: "default/foo3",
							Resource: constructMcpResourceWithOptions("foo3", "foo3.bar.com", "192.1.1.3", "1", func(resource *mcp.Resource) {
								resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev+"wrong") // to del
							}),
						},
					},
				},
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo2", "foo2.bar.com", "192.1.1.2"},
			},
		},
		{
			desc: "update-and-create-resources",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: gvk.ServiceEntry.String(),
					Resources: []*discovery.Resource{
						{
							Name:     "default/foo1",
							Resource: constructMcpResourceWithOptions("foo1", "foo1.bar.com", "192.1.1.11", "2"),
						},
						{
							Name:     "default/foo2",
							Resource: constructMcpResourceWithOptions("foo2", "foo2.bar.com", "192.1.1.22", "1"),
						},
						{
							Name:     "default/foo3",
							Resource: constructMcpResourceWithOptions("foo3", "foo3.bar.com", "192.1.1.3", ""),
						},
					},
				},
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.11"},
				{"foo2", "foo2.bar.com", "192.1.1.2"},
				{"foo3", "foo3.bar.com", "192.1.1.3"},
			},
		},
		{
			desc: "update-delete-and-create-resources",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: gvk.ServiceEntry.String(),
					Resources: []*discovery.Resource{
						{
							Name:     "default/foo2",
							Resource: constructMcpResourceWithOptions("foo2", "foo2.bar.com", "192.1.1.222", "4"),
						},
						{
							Name:     "default/foo4",
							Resource: constructMcpResourceWithOptions("foo4", "foo4.bar.com", "192.1.1.4", "1"),
						},
					},
					RemovedResources: []string{"default/foo1", "default/foo3"},
				},
			},
			expectedResources: [][]string{
				{"foo2", "foo2.bar.com", "192.1.1.222"},
				{"foo4", "foo4.bar.com", "192.1.1.4"},
			},
		},
		{
			desc: "update-delete-and-create-resources",
			serverResponses: []*discovery.DeltaDiscoveryResponse{
				{
					TypeUrl: gvk.ServiceEntry.String(),
					Resources: []*discovery.Resource{
						{
							Name:     "default/foo2",
							Resource: constructMcpResourceWithOptions("foo2", "foo2.bar.com", "192.2.2.22", "3"),
						},
						{
							Name:     "default/foo3",
							Resource: constructMcpResourceWithOptions("foo3", "foo3.bar.com", "192.1.1.33", ""),
						},
					},
					RemovedResources: []string{"default/foo4"},
				},
			},
			expectedResources: [][]string{
				{"foo2", "foo2.bar.com", "192.2.2.22"},
				{"foo3", "foo3.bar.com", "192.1.1.33"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			expectedLastReceived := make(map[string]*discovery.DeltaDiscoveryResponse)
			for _, response := range tt.serverResponses {
				expectedLastReceived[response.TypeUrl] = response
				respCh <- response
			}

			assert.EventuallyEqual(t, func() bool {
				client.mutex.Lock()
				defer client.mutex.Unlock()
				rec := client.lastReceived

				if rec == nil && len(rec) != len(expectedLastReceived) {
					return false
				}
				for tpe, rsrcs := range expectedLastReceived {
					if _, ok := rec[tpe]; !ok {
						return false
					}
					if len(rsrcs.Resources) != len(rec[tpe].Resources) {
						return false
					}
				}
				return true
			}, true, retry.Timeout(time.Second), retry.Delay(time.Millisecond))

			// wait for the callback updates the store
			time.Sleep(1 * time.Millisecond)

			configs := store.List(gvk.ServiceEntry, "")
			if len(configs) != len(tt.expectedResources) {
				t.Errorf("expected %v got %v", len(tt.expectedResources), len(configs))
			}
			configMap := make(map[string][]string)
			for _, conf := range configs {
				service, _ := conf.Spec.(*networking.ServiceEntry)
				configMap[conf.Name] = []string{conf.Name, service.Hosts[0], service.Addresses[0]}
			}
			for _, expected := range tt.expectedResources {
				got, ok := configMap[expected[0]]
				if !ok {
					t.Errorf("expected %v got none", expected)
				} else {
					for i, value := range expected {
						if value != got[i] {
							t.Errorf("expected %v got %v", value, got[i])
						}
					}
				}
			}
		})
	}
	close(respCh)
}
