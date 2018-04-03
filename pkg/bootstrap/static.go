// Copyright 2018 Istio Authors
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

package bootstrap

import (
	"fmt"
	"strconv"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	trace "github.com/envoyproxy/go-control-plane/envoy/config/trace/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	"istio.io/api/mixer/v1"
	"istio.io/api/mixer/v1/config/client"
)

// Default constants
const (
	ZipkinCluster    = "zipkin"
	TelemetryCluster = "mixer_report_server"
	TelemetryAddress = "istio-telemetry"
	TelemetryPort    = 9091
	PolicyAddress    = "istio-policy"
	PolicyPort       = 9091
)

// CircuitBreakers settings
var CircuitBreakers = &cluster.CircuitBreakers{
	Thresholds: []*cluster.CircuitBreakers_Thresholds{{
		Priority:           core.RoutingPriority_DEFAULT,
		MaxConnections:     &types.UInt32Value{Value: 100000},
		MaxPendingRequests: &types.UInt32Value{Value: 100000},
		MaxRequests:        &types.UInt32Value{Value: 100000},
		MaxRetries:         &types.UInt32Value{Value: 3},
	}},
}

// BuildBootstrap creates a static config for the sidecar HTTP proxy.
func BuildBootstrap(
	upstreams []Upstream,
	telemetry *Upstream, // set to one of the upstreams to route telemetry locally
	zipkin string, // set to non-empty host:port zipkin address
	adminPort int32,
) (*bootstrap.Bootstrap, error) {
	out := bootstrap.Bootstrap{
		Admin: bootstrap.Admin{
			AccessLogPath: "/dev/stdout",
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:       "127.0.0.1",
						PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(adminPort)},
					},
				},
			},
		},
		StaticResources: &bootstrap.Bootstrap_StaticResources{
			Clusters: BuildClusters(upstreams),
		},
	}

	if zipkin != "" {
		host, portStr, err := GetHostPort("zipkin", zipkin)
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, err
		}

		out.StaticResources.Clusters = append(out.StaticResources.Clusters,
			v2.Cluster{
				Name:           ZipkinCluster,
				ConnectTimeout: 1 * time.Second,
				Type:           v2.Cluster_STRICT_DNS,
				LbPolicy:       v2.Cluster_ROUND_ROBIN,
				Hosts: []*core.Address{{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address:       host,
							PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(port)},
						},
					},
				}},
			})
		out.Tracing = &trace.Tracing{
			Http: &trace.Tracing_Http{
				Name: util.Zipkin,
				Config: toStruct(&trace.ZipkinConfig{
					CollectorCluster:  ZipkinCluster,
					CollectorEndpoint: "/api/v1/spans",
				}),
			},
		}
	}

	// telemetry bootstrap should route telemetry directly to the local upstream
	telemetryCluster := TelemetryCluster
	if telemetry == nil {
		out.StaticResources.Clusters = append(out.StaticResources.Clusters, v2.Cluster{
			Name:           TelemetryCluster,
			ConnectTimeout: 1 * time.Second,
			Type:           v2.Cluster_STRICT_DNS,
			LbPolicy:       v2.Cluster_ROUND_ROBIN,
			Hosts: []*core.Address{{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:       TelemetryAddress,
						PortSpecifier: &core.SocketAddress_PortValue{PortValue: TelemetryPort},
					},
				},
			}},
			CircuitBreakers:      CircuitBreakers,
			Http2ProtocolOptions: &core.Http2ProtocolOptions{},
		})
	} else {
		telemetryCluster = clusterName(*telemetry)
	}

	out.StaticResources.Listeners = BuildListeners(upstreams, telemetryCluster)

	return &out, nil
}

func toStruct(msg proto.Message) *types.Struct {
	out, err := util.MessageToStruct(msg)
	if err != nil {
		panic(err)
	}
	return out
}

// Upstream declaration for a local HTTP service instance.
type Upstream struct {
	ListenPort   uint32
	UpstreamPort uint32
	GRPC         bool
	Auth         bool
	Service      string
	UID          string
	Operation    string
}

// BuildServiceConfig generates a mixer client config.
func BuildServiceConfig(service, uid, telemetryCluster string) *client.HttpClientConfig {
	return &client.HttpClientConfig{
		DefaultDestinationService: service,
		ServiceConfigs: map[string]*client.ServiceConfig{
			service: {
				DisableCheckCalls:  true,
				DisableReportCalls: false,
				MixerAttributes: &v1.Attributes{
					Attributes: map[string]*v1.Attributes_AttributeValue{
						"destination.service": {Value: &v1.Attributes_AttributeValue_StringValue{
							StringValue: service,
						}},
						"destination.uid": {Value: &v1.Attributes_AttributeValue_StringValue{
							StringValue: uid,
						}},
					},
				},
			},
		},
		Transport: &client.TransportConfig{
			CheckCluster:  "mixer_check_server",
			ReportCluster: telemetryCluster,
		},
	}
}

// BuildListeners generates HTTP listeners from upstreams
func BuildListeners(upstreams []Upstream, telemetryCluster string) []v2.Listener {
	out := make([]v2.Listener, 0, len(upstreams))
	for _, upstream := range upstreams {
		config := &hcm.HttpConnectionManager{
			CodecType:         hcm.AUTO,
			StatPrefix:        fmt.Sprintf("%d", upstream.ListenPort),
			GenerateRequestId: &types.BoolValue{Value: true}, // required for zipkin tracing
			AccessLog: []*accesslog.AccessLog{{
				Name: util.FileAccessLog,
				Config: toStruct(&accesslog.FileAccessLog{
					Path: "/dev/stdout",
				}),
			}},
			Tracing: &hcm.HttpConnectionManager_Tracing{
				OperationName: hcm.INGRESS,
			},
			HttpFilters: []*hcm.HttpFilter{
				{
					Name:   "mixer",
					Config: toStruct(BuildServiceConfig(upstream.Service, upstream.UID, telemetryCluster)),
				},
				{
					Name: util.Router,
				},
			},
			RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
				RouteConfig: &v2.RouteConfiguration{
					Name: fmt.Sprintf("%d", upstream.ListenPort),
					VirtualHosts: []route.VirtualHost{{
						Name:    upstream.Service,
						Domains: []string{"*"},
						Routes: []route.Route{{
							Match:     route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
							Decorator: &route.Decorator{Operation: upstream.Operation},
							Action: &route.Route_Route{
								Route: &route.RouteAction{
									ClusterSpecifier: &route.RouteAction_Cluster{Cluster: clusterName(upstream)},
									Timeout:          (func(t time.Duration) *time.Duration { return &t })(0),
								},
							},
						}},
					}},
				},
			},
		}
		if upstream.GRPC {
			config.CodecType = hcm.HTTP2
		}
		lst := v2.Listener{
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:       "0.0.0.0",
						PortSpecifier: &core.SocketAddress_PortValue{PortValue: upstream.ListenPort},
					},
				},
			},
			Name: fmt.Sprintf("%d", upstream.ListenPort),
			FilterChains: []listener.FilterChain{{
				Filters: []listener.Filter{{
					Name:   util.HTTPConnectionManager,
					Config: toStruct(config),
				}},
			}},
		}
		if upstream.Auth {
			ctx := &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					TlsCertificates: []*auth.TlsCertificate{
						{
							CertificateChain: &core.DataSource{
								Specifier: &core.DataSource_Filename{Filename: "/etc/certs/cert-chain.pem"},
							},
							PrivateKey: &core.DataSource{
								Specifier: &core.DataSource_Filename{Filename: "/etc/certs/key.pem"},
							},
						},
					},
					ValidationContext: &auth.CertificateValidationContext{
						TrustedCa: &core.DataSource{
							Specifier: &core.DataSource_Filename{Filename: "/etc/certs/root-cert.pem"},
						},
					},
				},
				RequireClientCertificate: &types.BoolValue{Value: true},
			}
			if upstream.GRPC {
				ctx.CommonTlsContext.AlpnProtocols = []string{"h2"}
			} else {
				ctx.CommonTlsContext.AlpnProtocols = []string{"http/1.1"}
			}
			lst.FilterChains[0].TlsContext = ctx
		}
		out = append(out, lst)
	}
	return out
}

func clusterName(upstream Upstream) string {
	return fmt.Sprintf("in.%d", upstream.UpstreamPort)
}

// BuildClusters generates clusters from upstreams
func BuildClusters(upstreams []Upstream) []v2.Cluster {
	out := make([]v2.Cluster, 0, len(upstreams))
	for _, upstream := range upstreams {
		cluster := v2.Cluster{
			Name:           clusterName(upstream),
			ConnectTimeout: 1 * time.Second,
			Type:           v2.Cluster_STATIC,
			LbPolicy:       v2.Cluster_ROUND_ROBIN,
			Hosts: []*core.Address{{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:       "127.0.0.1",
						PortSpecifier: &core.SocketAddress_PortValue{PortValue: upstream.UpstreamPort},
					},
				},
			}},
			CircuitBreakers: CircuitBreakers,
		}
		if upstream.GRPC {
			cluster.Http2ProtocolOptions = &core.Http2ProtocolOptions{}
		}
		out = append(out, cluster)
	}
	return out
}
