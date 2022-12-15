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

package ambientgen

import (
	"fmt"
	"time"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	any "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	"istio.io/istio/pilot/pkg/model"
	core2 "istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("ambientgen", "xDS Generator for ambient clients", 0)

const (
	ZTunnelOutboundCapturePort uint32 = 15001
)

var _ model.XdsResourceGenerator = &WaypointGenerator{}

/*
Listener waypoint_outbound, 0.0.0.0:15001:

	Single Chain:
	    -> Terminate CONNECT
	    -> Route per header to each cluster "<svc_vip>_<port>"
	CDS "<svc_vip>_<port>":
	    -> forward to internal listener "<svc_vip>_<port>"

Listener tunnel, internal:

	Single Chain:
	    -> Tunnel to cluster "outbound_tunnel_clus_<identity>"

Listener: all normal outbound listeners, but switched to internal listeners

Clusters: all normal outbound clusters
*/

type WaypointGenerator struct {
	ConfigGenerator core2.ConfigGenerator
}

func (p *WaypointGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	var out model.Resources
	switch w.TypeUrl {
	case v3.ListenerType:
		sidecarListeners := p.ConfigGenerator.BuildListeners(proxy, req.Push)
		resources := model.Resources{}
		for _, c := range sidecarListeners {
			resources = append(resources, &discovery.Resource{
				Name:     c.Name,
				Resource: protoconv.MessageToAny(c),
			})
		}
		out = resources
		// out = append(p.buildWaypointListeners(proxy, req.Push), resources...)
	case v3.ClusterType:
		sidecarClusters, _ := p.ConfigGenerator.BuildClusters(proxy, req)
		waypointClusters := p.buildClusters(proxy, req.Push)
		out = append(waypointClusters, sidecarClusters...)
	}
	return out, model.DefaultXdsLogDetails, nil
}

func getActualWildcardAndLocalHost(node *model.Proxy) string {
	if node.SupportsIPv4() {
		return v1alpha3.WildcardAddress // , v1alpha3.LocalhostAddress
	}
	return v1alpha3.WildcardIPv6Address //, v1alpha3.LocalhostIPv6Address
}

/*
func (p *WaypointGenerator) buildWaypointListeners(proxy *model.Proxy, push *model.PushContext) model.Resources {
	saWorkloads := push.AmbientIndex.Workloads.ByIdentity[proxy.VerifiedIdentity.String()]
	if len(saWorkloads) == 0 {
		log.Warnf("no workloads for sa %s (proxy %s)", proxy.VerifiedIdentity.String(), proxy.ID)
		return nil
	}
	wildcard := getActualWildcardAndLocalHost(proxy)
	vhost := &route.VirtualHost{
		Name:    "connect",
		Domains: []string{"*"},
	}
	for _, egressListener := range proxy.SidecarScope.EgressListeners {
		for _, service := range egressListener.Services() {
			for _, port := range service.Ports {
				if port.Protocol == protocol.UDP {
					continue
				}
				bind := wildcard
				if !port.Protocol.IsHTTP() {
					// TODO: this is not 100% accurate for custom cases
					bind = service.GetAddressForProxy(proxy)
				}

				// This essentially mirrors the sidecar case for serviceEntries have no VIP.  In the waypoint proxy, we
				// don't know the ServiceEntry's VIP, so instead we search for a matching ServiceEntry host
				// for any remaining unmatched outbund to *:<port>
				authorityHost := service.GetAddressForProxy(proxy)
				if authorityHost == "0.0.0.0" {
					authorityHost = "*"
				}
				name := fmt.Sprintf("%s_%d", bind, port.Port)
				vhost.Routes = append(vhost.Routes, &route.Route{
					Match: &route.RouteMatch{
						PathSpecifier: &route.RouteMatch_ConnectMatcher_{ConnectMatcher: &route.RouteMatch_ConnectMatcher{}},
						Headers: []*route.HeaderMatcher{
							istiomatcher.HeaderMatcher(":authority", fmt.Sprintf("%s:%d", authorityHost, port.Port)),
						},
					},
					Action: &route.Route_Route{Route: &route.RouteAction{
						UpgradeConfigs: []*route.RouteAction_UpgradeConfig{{
							UpgradeType:   "CONNECT",
							ConnectConfig: &route.RouteAction_UpgradeConfig_ConnectConfig{},
						}},

						ClusterSpecifier: &route.RouteAction_Cluster{Cluster: name},
					}},
				})
			}
		}
	}
	l := &listener.Listener{
		Name:    "waypoint_outbound l",
		Address: ipPortAddress("0.0.0.0", ZTunnelOutboundCapturePort),

		AccessLog: accessLogString("waypoint_outbound"),
		FilterChains: []*listener.FilterChain{
			{
				Name: "waypoint_outbound fc",

				TransportSocket: &core.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
						CommonTlsContext: buildCommonTLSContext(proxy, "", push, true),
					})},
				},
				Filters: []*listener.Filter{{
					Name: "envoy.filters.network.http_connection_manager",
					ConfigType: &listener.Filter_TypedConfig{
						TypedConfig: protoconv.MessageToAny(&httpconn.HttpConnectionManager{
							AccessLog:  accessLogString("waypoint hcm"),
							StatPrefix: "outbound_hcm",
							RouteSpecifier: &httpconn.HttpConnectionManager_RouteConfig{
								RouteConfig: &route.RouteConfiguration{
									Name:             "local_route",
									VirtualHosts:     []*route.VirtualHost{vhost},
									ValidateClusters: proto.BoolFalse,
								},
							},
							HttpFilters: []*httpconn.HttpFilter{{
								Name:       "envoy.filters.http.router",
								ConfigType: &httpconn.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(&routerfilter.Router{})},
							}},
							Http2ProtocolOptions: &core.Http2ProtocolOptions{
								AllowConnect: true,
							},
							UpgradeConfigs: []*httpconn.HttpConnectionManager_UpgradeConfig{{
								UpgradeType: "CONNECT",
							}},
						}),
					},
				}},
			},
		},
	}
	var out model.Resources
	for _, l := range []*listener.Listener{l} {
		out = append(out, &discovery.Resource{
			Name:     l.Name,
			Resource: protoconv.MessageToAny(l),
		})
	}
	return out
}*/

func (p *WaypointGenerator) buildClusters(node *model.Proxy, push *model.PushContext) model.Resources {
	// TODO passthrough and blackhole
	var clusters []*cluster.Cluster
	wildcard := getActualWildcardAndLocalHost(node)
	seen := sets.New[string]()
	for _, egressListener := range node.SidecarScope.EgressListeners {
		for _, service := range egressListener.Services() {
			for _, port := range service.Ports {
				if port.Protocol == protocol.UDP {
					continue
				}
				bind := wildcard
				if !port.Protocol.IsHTTP() {
					// TODO: this is not 100% accurate for custom cases
					bind = service.GetAddressForProxy(node)
				}
				name := fmt.Sprintf("%s_%d", bind, port.Port)
				if seen.Contains(name) {
					continue
				}
				seen.Insert(name)
				clusters = append(clusters, &cluster.Cluster{
					Name:                 name,
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
					LoadAssignment: &endpoint.ClusterLoadAssignment{
						ClusterName: name,
						Endpoints: []*endpoint.LocalityLbEndpoints{{
							LbEndpoints: []*endpoint.LbEndpoint{{
								HostIdentifier: &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{Address: util.BuildInternalAddress(name)}},
								Metadata:       nil, // TODO metadata for passthrough
							}},
						}},
					},
				})
			}
		}
	}

	clusters = append(clusters, outboundTunnelCluster(node, push, node.Metadata.ServiceAccount, ""))
	var out model.Resources
	for _, c := range clusters {
		out = append(out, &discovery.Resource{Name: c.Name, Resource: protoconv.MessageToAny(c)})
	}
	return out
}

func buildCommonTLSContext(proxy *model.Proxy, identityOverride string, push *model.PushContext, inbound bool) *tls.CommonTlsContext {
	ctx := &tls.CommonTlsContext{}
	// TODO san match
	security.ApplyToCommonTLSContext(ctx, proxy, nil, authn.TrustDomainsForValidation(push.Mesh), inbound)

	if identityOverride != "" {
		ctx.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
			security.ConstructSdsSecretConfig(identityOverride),
		}
	}
	ctx.AlpnProtocols = []string{"h2"}

	ctx.TlsParams = &tls.TlsParameters{
		// Ensure TLS 1.3 is used everywhere
		TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
		TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
	}

	return ctx
}

// outboundTunnelCluster is per-workload SA
func outboundTunnelCluster(proxy *model.Proxy, push *model.PushContext, sa string, identityOverride string) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                 outboundTunnelClusterName(sa),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:       durationpb.New(2 * time.Second),
		CleanupInterval:      durationpb.New(60 * time.Second),
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{},
		},
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: buildCommonTLSContext(proxy, identityOverride, push, false),
			})},
		},
	}
}

func outboundTunnelClusterName(sa string) string {
	return "outbound_tunnel_clus_" + sa
}

const EnvoyTextLogFormat = "[%START_TIME%] \"%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% " +
	"%PROTOCOL%\" %RESPONSE_CODE% %RESPONSE_FLAGS% " +
	"%RESPONSE_CODE_DETAILS% %CONNECTION_TERMINATION_DETAILS% " +
	"\"%UPSTREAM_TRANSPORT_FAILURE_REASON%\" %BYTES_RECEIVED% %BYTES_SENT% " +
	"%DURATION% %RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)% \"%REQ(X-FORWARDED-FOR)%\" " +
	"\"%REQ(USER-AGENT)%\" \"%REQ(X-REQUEST-ID)%\" \"%REQ(:AUTHORITY)%\" \"%UPSTREAM_HOST%\" " +
	"%UPSTREAM_CLUSTER% %UPSTREAM_LOCAL_ADDRESS% %DOWNSTREAM_LOCAL_ADDRESS% " +
	"%DOWNSTREAM_REMOTE_ADDRESS% %REQUESTED_SERVER_NAME% %ROUTE_NAME% "

func accessLogString(prefix string) []*accesslog.AccessLog {
	inlineString := EnvoyTextLogFormat + prefix + "\n"
	return []*accesslog.AccessLog{{
		Name: "envoy.access_loggers.file",
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(&fileaccesslog.FileAccessLog{
			Path: "/dev/stdout",
			AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{TextFormatSource: &core.DataSource{Specifier: &core.DataSource_InlineString{
					InlineString: inlineString,
				}}},
			}},
		})},
	}}
}

func h2connectUpgrade() map[string]*any.Any {
	return map[string]*any.Any{
		v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
			UpstreamProtocolOptions: &http.HttpProtocolOptions_ExplicitHttpConfig_{ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
					Http2ProtocolOptions: &core.Http2ProtocolOptions{
						AllowConnect: true,
					},
				},
			}},
		}),
	}
}

func ipPortAddress(ip string, port uint32) *core.Address {
	return &core.Address{Address: &core.Address_SocketAddress{
		SocketAddress: &core.SocketAddress{
			Address: ip,
			PortSpecifier: &core.SocketAddress_PortValue{
				PortValue: port,
			},
		},
	}}
}
