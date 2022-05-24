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

//go:build !agent
// +build !agent

package uproxygen

import (
	"fmt"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	httpconn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"

	"istio.io/istio/pilot/pkg/model"
	core2 "istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	istiomatcher "istio.io/istio/pilot/pkg/security/authz/matcher"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/util/sets"
)

var _ model.XdsResourceGenerator = &PEPGenerator{}

/*
Listener pep_outbound, 0.0.0.0:15001:

	Single Chain:
	    -> Terminate CONNECT
	    -> Route per header to each cluster "<svc_vip>_<port>"
	CDS "<svc_vip>_<port>":
	    -> forward to internal listener "<svc_vip>_<port>"

Listener pep_tunnel, internal:

	Single Chain:
	    -> Tunnel to cluster "outbound_tunnel_clus_<identity>"

Listener: all normal outbound listeners, but switched to internal listeners

Clusters: all normal outbound clusters
*/

type PEPGenerator struct {
	ConfigGenerator core2.ConfigGenerator
}

func (p *PEPGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	var out model.Resources
	switch w.TypeUrl {
	case v3.ListenerType:
		sidecarListeners := p.ConfigGenerator.BuildListeners(proxy, req.Push)
		resources := model.Resources{}
		for _, c := range sidecarListeners {
			resources = append(resources, &discovery.Resource{
				Name:     c.Name,
				Resource: util.MessageToAny(c),
			})
		}
		out = append(p.buildPEPListeners(proxy, req.Push), resources...)
		out = append(out, outboundTunnelListener("pep_tunnel", proxy.Metadata.ServiceAccount))
	case v3.ClusterType:
		sidecarClusters, _ := p.ConfigGenerator.BuildClusters(proxy, req)
		remoteClusters := p.buildClusters(proxy, req.Push)
		out = append(remoteClusters, sidecarClusters...)
		out = append(out)
	}
	return out, model.DefaultXdsLogDetails, nil
}

func getActualWildcardAndLocalHost(node *model.Proxy) (string, string) {
	if node.SupportsIPv4() {
		return v1alpha3.WildcardAddress, v1alpha3.LocalhostAddress
	}
	return v1alpha3.WildcardIPv6Address, v1alpha3.LocalhostIPv6Address
}

func (p *PEPGenerator) buildPEPListeners(proxy *model.Proxy, push *model.PushContext) model.Resources {
	saWorkloads := push.SidecarlessIndex.Workloads.ByIdentity[proxy.VerifiedIdentity.String()]
	if len(saWorkloads) == 0 {
		log.Warnf("no workloads for sa %s (proxy %s)", proxy.Metadata.ServiceAccount, proxy.ID)
		return nil
	}
	wildcard, _ := getActualWildcardAndLocalHost(proxy)
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
				name := fmt.Sprintf("%s_%d", bind, port.Port)
				vhost.Routes = append(vhost.Routes, &route.Route{
					Match: &route.RouteMatch{
						// PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
						PathSpecifier: &route.RouteMatch_ConnectMatcher_{ConnectMatcher: &route.RouteMatch_ConnectMatcher{}},
						Headers: []*route.HeaderMatcher{
							istiomatcher.HeaderMatcher("x-original-ip", service.GetAddressForProxy(proxy)),
							istiomatcher.HeaderMatcher("x-original-port", fmt.Sprint(port.Port)),
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
		Name:    "pep_outbound l",
		Address: ipPortAddress("0.0.0.0", UproxyOutboundCapturePort),

		AccessLog: accessLogString("pep_outbound"),
		FilterChains: []*listener.FilterChain{
			{
				Name: "pep_outbound fc",

				TransportSocket: &core.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.DownstreamTlsContext{
						CommonTlsContext: buildCommonTLSContext(proxy, nil, push, true),
					})},
				},
				Filters: []*listener.Filter{{
					Name: "envoy.filters.network.http_connection_manager",
					ConfigType: &listener.Filter_TypedConfig{
						TypedConfig: util.MessageToAny(&httpconn.HttpConnectionManager{
							AccessLog:  accessLogString("pep hcm"),
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
								ConfigType: &httpconn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(&routerfilter.Router{})},
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
			Resource: util.MessageToAny(l),
		})
	}
	return out
}

func routePortToInternalOutbound(*model.Proxy, *model.PushContext) []*route.Route {
	var out []*route.Route
	out = append(out, &route.Route{
		Match: &route.RouteMatch{
			PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
			Headers:       []*route.HeaderMatcher{istiomatcher.HeaderMatcher("x-direction", "outbound")},
		},
		Action: &route.Route_Route{Route: &route.RouteAction{
			UpgradeConfigs: []*route.RouteAction_UpgradeConfig{{
				UpgradeType:   "CONNECT",
				ConnectConfig: &route.RouteAction_UpgradeConfig_ConnectConfig{},
			}},
			ClusterSpecifier: &route.RouteAction_ClusterHeader{ClusterHeader: "x-original-port"},
		}},
	})
	return out
}

func stringMatch(s string) *route.HeaderMatcher_StringMatch {
	return &route.HeaderMatcher_StringMatch{StringMatch: &matcher.StringMatcher{
		MatchPattern: &matcher.StringMatcher_Exact{Exact: s},
	}}
}

func (p *PEPGenerator) buildClusters(node *model.Proxy, push *model.PushContext) model.Resources {
	// TODO passthrough and blackhole
	var clusters []*cluster.Cluster
	wildcard, _ := getActualWildcardAndLocalHost(node)
	seen := sets.New()
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
								HostIdentifier: &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{Address: internalAddress(name)}},
								Metadata:       nil, // TODO metadata for passthrough
							}},
						}},
					},
				})
			}
		}
	}

	clusters = append(clusters, outboundTunnelCluster(node, push, node.Metadata.ServiceAccount, nil))
	var out model.Resources
	for _, c := range clusters {
		out = append(out, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
	}
	return out
}
