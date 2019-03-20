// Copyright 2018 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

// Package resource creates test xDS resources
package resource

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/types"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	als "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	alf "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	tcp "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/envoyproxy/go-control-plane/pkg/util"
)

const (
	localhost = "127.0.0.1"

	// XdsCluster is the cluster name for the control server (used by non-ADS set-up)
	XdsCluster = "xds_cluster"

	// Ads mode for resources: one aggregated xDS service
	Ads = "ads"

	// Xds mode for resources: individual xDS services
	Xds = "xds"

	// Rest mode for resources: polling using Fetch
	Rest = "rest"
)

var (
	// RefreshDelay for the polling config source
	RefreshDelay = 500 * time.Millisecond
)

// MakeEndpoint creates a localhost endpoint on a given port.
func MakeEndpoint(clusterName string, port uint32) *v2.ClusterLoadAssignment {
	return &v2.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints: []endpoint.LocalityLbEndpoints{{
			LbEndpoints: []endpoint.LbEndpoint{{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &core.Address{
							Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									Protocol: core.TCP,
									Address:  localhost,
									PortSpecifier: &core.SocketAddress_PortValue{
										PortValue: port,
									},
								},
							},
						},
					},
				},
			}},
		}},
	}
}

// MakeCluster creates a cluster using either ADS or EDS.
func MakeCluster(mode string, clusterName string) *v2.Cluster {
	var edsSource *core.ConfigSource
	switch mode {
	case Ads:
		edsSource = &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
		}
	case Xds:
		edsSource = &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType: core.ApiConfigSource_GRPC,
					GrpcServices: []*core.GrpcService{{
						TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: XdsCluster},
						},
					}},
				},
			},
		}
	case Rest:
		edsSource = &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:      core.ApiConfigSource_REST,
					ClusterNames: []string{XdsCluster},
					RefreshDelay: &RefreshDelay,
				},
			},
		}
	}

	return &v2.Cluster{
		Name:                 clusterName,
		ConnectTimeout:       5 * time.Second,
		ClusterDiscoveryType: &v2.Cluster_Type{Type: v2.Cluster_EDS},
		EdsClusterConfig: &v2.Cluster_EdsClusterConfig{
			EdsConfig: edsSource,
		},
	}
}

// MakeRoute creates an HTTP route that routes to a given cluster.
func MakeRoute(routeName, clusterName string) *v2.RouteConfiguration {
	return &v2.RouteConfiguration{
		Name: routeName,
		VirtualHosts: []route.VirtualHost{{
			Name:    routeName,
			Domains: []string{"*"},
			Routes: []route.Route{{
				Match: route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{
						Prefix: "/",
					},
				},
				Action: &route.Route_Route{
					Route: &route.RouteAction{
						ClusterSpecifier: &route.RouteAction_Cluster{
							Cluster: clusterName,
						},
					},
				},
			}},
		}},
	}
}

// data source configuration
func configSource(mode string) *core.ConfigSource {
	source := &core.ConfigSource{}
	switch mode {
	case Ads:
		source.ConfigSourceSpecifier = &core.ConfigSource_Ads{
			Ads: &core.AggregatedConfigSource{},
		}
	case Xds:
		source.ConfigSourceSpecifier = &core.ConfigSource_ApiConfigSource{
			ApiConfigSource: &core.ApiConfigSource{
				ApiType: core.ApiConfigSource_GRPC,
				GrpcServices: []*core.GrpcService{{
					TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: XdsCluster},
					},
				}},
			},
		}
	case Rest:
		source.ConfigSourceSpecifier = &core.ConfigSource_ApiConfigSource{
			ApiConfigSource: &core.ApiConfigSource{
				ApiType:      core.ApiConfigSource_REST,
				ClusterNames: []string{XdsCluster},
				RefreshDelay: &RefreshDelay,
			},
		}
	}
	return source
}

// MakeHTTPListener creates a listener using either ADS or RDS for the route.
func MakeHTTPListener(mode string, listenerName string, port uint32, route string) *v2.Listener {
	rdsSource := configSource(mode)

	// access log service configuration
	alsConfig := &als.HttpGrpcAccessLogConfig{
		CommonConfig: &als.CommonGrpcAccessLogConfig{
			LogName: "echo",
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: XdsCluster,
					},
				},
			},
		},
	}
	alsConfigPbst, err := types.MarshalAny(alsConfig)
	if err != nil {
		panic(err)
	}

	// HTTP filter configuration
	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_Rds{
			Rds: &hcm.Rds{
				ConfigSource:    *rdsSource,
				RouteConfigName: route,
			},
		},
		HttpFilters: []*hcm.HttpFilter{{
			Name: util.Router,
		}},
		AccessLog: []*alf.AccessLog{{
			Name: util.HTTPGRPCAccessLog,
			ConfigType: &alf.AccessLog_TypedConfig{
				TypedConfig: alsConfigPbst,
			},
		}},
	}
	pbst, err := types.MarshalAny(manager)
	if err != nil {
		panic(err)
	}

	return &v2.Listener{
		Name: listenerName,
		Address: core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Protocol: core.TCP,
					Address:  localhost,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		FilterChains: []listener.FilterChain{{
			Filters: []listener.Filter{{
				Name: util.HTTPConnectionManager,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: pbst,
				},
			}},
		}},
	}
}

// MakeTCPListener creates a TCP listener for a cluster.
func MakeTCPListener(listenerName string, port uint32, clusterName string) *v2.Listener {
	// TCP filter configuration
	config := &tcp.TcpProxy{
		StatPrefix: "tcp",
		ClusterSpecifier: &tcp.TcpProxy_Cluster{
			Cluster: clusterName,
		},
	}
	pbst, err := types.MarshalAny(config)
	if err != nil {
		panic(err)
	}
	return &v2.Listener{
		Name: listenerName,
		Address: core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Protocol: core.TCP,
					Address:  localhost,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		FilterChains: []listener.FilterChain{{
			Filters: []listener.Filter{{
				Name: util.TCPProxy,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: pbst,
				},
			}},
		}},
	}
}

// TestSnapshot holds parameters for a synthetic snapshot.
type TestSnapshot struct {
	// Xds indicates snapshot mode: ads, xds, or rest
	Xds string
	// Version for the snapshot.
	Version string
	// UpstreamPort for the single endpoint on the localhost.
	UpstreamPort uint32
	// BasePort is the initial port for the listeners.
	BasePort uint32
	// NumClusters is the total number of clusters to generate.
	NumClusters int
	// NumHTTPListeners is the total number of HTTP listeners to generate.
	NumHTTPListeners int
	// NumTCPListeners is the total number of TCP listeners to generate.
	// Listeners are assigned clusters in a round-robin fashion.
	NumTCPListeners int
	// TLS enables SDS-enabled TLS mode on all listeners
	TLS bool
}

// Generate produces a snapshot from the parameters.
func (ts TestSnapshot) Generate() cache.Snapshot {
	clusters := make([]cache.Resource, ts.NumClusters)
	endpoints := make([]cache.Resource, ts.NumClusters)
	for i := 0; i < ts.NumClusters; i++ {
		name := fmt.Sprintf("cluster-%s-%d", ts.Version, i)
		clusters[i] = MakeCluster(ts.Xds, name)
		endpoints[i] = MakeEndpoint(name, ts.UpstreamPort)
	}

	routes := make([]cache.Resource, ts.NumHTTPListeners)
	for i := 0; i < ts.NumHTTPListeners; i++ {
		name := fmt.Sprintf("route-%s-%d", ts.Version, i)
		routes[i] = MakeRoute(name, cache.GetResourceName(clusters[i%ts.NumClusters]))
	}

	total := ts.NumHTTPListeners + ts.NumTCPListeners
	listeners := make([]cache.Resource, total)
	for i := 0; i < total; i++ {
		port := ts.BasePort + uint32(i)
		// listener name must be same since ports are shared and previous listener is drained
		name := fmt.Sprintf("listener-%d", port)
		var listener *v2.Listener
		if i < ts.NumHTTPListeners {
			listener = MakeHTTPListener(ts.Xds, name, port, cache.GetResourceName(routes[i]))
		} else {
			listener = MakeTCPListener(name, port, cache.GetResourceName(clusters[i%ts.NumClusters]))
		}

		if ts.TLS {
			for i, chain := range listener.FilterChains {
				chain.TlsContext = &auth.DownstreamTlsContext{
					CommonTlsContext: &auth.CommonTlsContext{
						TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{{
							Name:      tlsName,
							SdsConfig: configSource(ts.Xds),
						}},
						ValidationContextType: &auth.CommonTlsContext_ValidationContextSdsSecretConfig{
							ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
								Name:      rootName,
								SdsConfig: configSource(ts.Xds),
							},
						},
					},
				}
				listener.FilterChains[i] = chain
			}
		}

		listeners[i] = listener
	}

	out := cache.Snapshot{
		Endpoints: cache.NewResources(ts.Version, endpoints),
		Clusters:  cache.NewResources(ts.Version, clusters),
		Routes:    cache.NewResources(ts.Version, routes),
		Listeners: cache.NewResources(ts.Version, listeners),
	}

	if ts.TLS {
		out.Secrets = cache.NewResources(ts.Version, MakeSecrets(tlsName, rootName))
	}

	return out
}
