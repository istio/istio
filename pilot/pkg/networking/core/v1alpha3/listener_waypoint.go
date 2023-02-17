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

package v1alpha3

import (
	"fmt"
	"net/netip"
	"strconv"
	"time"

	xds "github.com/cncf/xds/go/xds/core/v3"
	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	any "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/extension"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/match"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route/retry"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/pkg/log"
)

type WorkloadAndServices struct {
	WorkloadInfo *model.WorkloadInfo
	Services     []*model.Service
}

func FindAssociatedResources(node *model.Proxy, push *model.PushContext) ([]WorkloadAndServices, map[host.Name]*model.Service) {
	wls := []WorkloadAndServices{}
	scope := node.WaypointScope()
	workloads := push.WorkloadsForWaypoint(scope)
	for _, wl := range workloads {
		wls = append(wls, WorkloadAndServices{WorkloadInfo: wl})
	}
	svcs := map[host.Name]*model.Service{}
	for i, wl := range wls {
		for _, ns := range push.ServiceIndex.HostnameAndNamespace {
			svc := ns[wl.WorkloadInfo.Namespace]
			if svc == nil {
				continue
			}
			if labels.Instance(svc.Attributes.LabelSelectors).SubsetOf(wl.WorkloadInfo.Labels) {
				svcs[svc.Hostname] = svc
				wl.Services = append(wl.Services, svc)
			}
		}
		wls[i] = wl
	}
	return wls, svcs
}

func (lb *ListenerBuilder) serviceForHostname(name host.Name) *model.Service {
	return lb.push.ServiceForHostname(lb.node, name)
}

func (lb *ListenerBuilder) buildWaypointInbound() []*listener.Listener {
	listeners := []*listener.Listener{}
	// We create 3 listeners:
	// 1. Decapsulation CONNECT listener, `inbound TERMINATE`.
	// 2. IP dispatch listener, handling both VIPs and direct pod IPs.
	// 3. Encapsulation CONNECT listener, originating the tunnel
	wls, svcs := FindAssociatedResources(lb.node, lb.push)

	listeners = append(listeners,
		lb.buildWaypointInboundTerminateConnect(),
		lb.buildWaypointInternal(wls, svcs),
		lb.buildWaypointInboundOriginateConnect())

	return listeners
}

func (lb *ListenerBuilder) buildWaypointInboundTerminateConnect() *listener.Listener {
	name := "connect_terminate"
	vhost := &route.VirtualHost{
		Name:    "default",
		Domains: []string{"*"},
		Routes: []*route.Route{{
			Match: &route.RouteMatch{
				PathSpecifier: &route.RouteMatch_ConnectMatcher_{ConnectMatcher: &route.RouteMatch_ConnectMatcher{}},
			},
			Action: &route.Route_Route{Route: &route.RouteAction{
				UpgradeConfigs: []*route.RouteAction_UpgradeConfig{{
					UpgradeType:   "CONNECT",
					ConnectConfig: &route.RouteAction_UpgradeConfig_ConnectConfig{},
				}},
				ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "internal"},
			}},
			TypedPerFilterConfig: map[string]*any.Any{
				xdsfilters.ConnectAuthorityFilter.Name: xdsfilters.ConnectAuthorityEnabled,
			},
		}},
	}

	actualWildcard, _ := getActualWildcardAndLocalHost(lb.node)
	h := &hcm.HttpConnectionManager{
		StatPrefix: name,
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &route.RouteConfiguration{
				Name:         "default",
				VirtualHosts: []*route.VirtualHost{vhost},
			},
		},
		// Append and forward client cert to backend.
		ForwardClientCertDetails: hcm.HttpConnectionManager_APPEND_FORWARD,
		SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
			Subject: proto.BoolTrue,
			Uri:     true,
			Dns:     true,
		},
		ServerName:       EnvoyServerName,
		UseRemoteAddress: proto.BoolFalse,
	}

	// Protocol settings
	notimeout := durationpb.New(0 * time.Second)
	h.StreamIdleTimeout = notimeout
	h.UpgradeConfigs = []*hcm.HttpConnectionManager_UpgradeConfig{{
		UpgradeType: "CONNECT",
	}}
	h.Http2ProtocolOptions = &core.Http2ProtocolOptions{
		AllowConnect: true,
	}

	// Filters needed to propagate the tunnel metadata to the inner streams.
	h.HttpFilters = []*hcm.HttpFilter{
		xdsfilters.ConnectBaggageFilter,
		xdsfilters.ConnectAuthorityFilter,
		xdsfilters.Router,
	}

	l := &listener.Listener{
		Name:    name,
		Address: util.BuildAddress(actualWildcard, model.HBoneInboundListenPort),
		FilterChains: []*listener.FilterChain{
			{
				Name: "default",
				TransportSocket: &core.TransportSocket{
					Name: "tls",
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
						CommonTlsContext: buildCommonTLSContext(lb.node, lb.push),
					})},
				},
				Filters: []*listener.Filter{
					xdsfilters.IstioNetworkAuthenticationFilter,
					{
						Name:       wellknown.HTTPConnectionManager,
						ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(h)},
					},
				},
			},
		},
	}
	return l
}

func (lb *ListenerBuilder) buildWaypointInternal(wls []WorkloadAndServices, svcs map[host.Name]*model.Service) *listener.Listener {
	ipMatcher := &matcher.IPMatcher{}
	chains := []*listener.FilterChain{}
	pre, post := lb.buildWaypointHTTPFilters()
	for _, svc := range svcs {
		portMapper := match.NewDestinationPort()
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			portString := fmt.Sprintf("%d", port.Port)
			cc := inboundChainConfig{
				clusterName: model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "tcp", svc.Hostname, port.Port),
				// clusterName: model.BuildSubsetKey(model.TrafficDirectionInboundPod, "", host.Name(wl.PodIP), port.Port),
				port: ServiceInstancePort{
					Name:       port.Name,
					Port:       uint32(port.Port),
					TargetPort: uint32(port.Port),
					Protocol:   port.Protocol,
				},
				bind:  "0.0.0.0",
				hbone: true,
			}
			name := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "", svc.Hostname, port.Port)
			tcpName := name + "-tcp"
			tcpChain := &listener.FilterChain{
				Filters: lb.buildInboundNetworkFilters(cc),
				Name:    tcpName,
			}
			cc.clusterName = model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", svc.Hostname, port.Port)
			httpName := name + "-http"
			httpChain := &listener.FilterChain{
				Filters: lb.buildWaypointInboundHTTPFilters(svc, cc, pre, post),
				Name:    httpName,
			}
			if port.Protocol.IsUnsupported() {
				// If we need to sniff, insert two chains and the protocol detector
				chains = append(chains, tcpChain, httpChain)
				portMapper.Map[portString] = match.ToMatcher(match.NewAppProtocol(match.ProtocolMatch{
					TCP:  match.ToChain(tcpName),
					HTTP: match.ToChain(httpName),
				}))
			} else if port.Protocol.IsHTTP() {
				// Otherwise, just insert HTTP/TCP
				chains = append(chains, httpChain)
				portMapper.Map[portString] = match.ToChain(httpChain.Name)
			} else {
				chains = append(chains, tcpChain)
				portMapper.Map[portString] = match.ToChain(tcpChain.Name)
			}
		}
		if len(portMapper.Map) > 0 {
			cidr := util.ConvertAddressToCidr(svc.GetAddressForProxy(lb.node))
			rangeMatcher := &matcher.IPMatcher_IPRangeMatcher{
				Ranges: []*xds.CidrRange{{
					AddressPrefix: cidr.AddressPrefix,
					PrefixLen:     cidr.PrefixLen,
				}},
				OnMatch: match.ToMatcher(portMapper.Matcher),
			}
			ipMatcher.RangeMatchers = append(ipMatcher.RangeMatchers, rangeMatcher)
		}
	}

	{
		// Direct pod access chain.
		cc := inboundChainConfig{
			clusterName: "encap",
			port: ServiceInstancePort{
				Name:     "unknown",
				Protocol: protocol.TCP,
			},
			bind:  "0.0.0.0",
			hbone: true,
		}
		tcpChain := &listener.FilterChain{
			Filters: append([]*listener.Filter{
				xdsfilters.ConnectAuthorityNetworkFilter,
			},
				lb.buildInboundNetworkFilters(cc)...),
			Name: "direct-tcp",
		}
		// TODO: maintains undesirable persistent HTTP connections to "encap"
		httpChain := &listener.FilterChain{
			Filters: append([]*listener.Filter{
				xdsfilters.ConnectAuthorityNetworkFilter,
			},
				lb.buildWaypointInboundHTTPFilters(nil, cc, pre, post)...),
			Name: "direct-http",
		}
		// Workload IP filtering happens here.
		ipRange := []*xds.CidrRange{}
		for _, wlx := range wls {
			addr, _ := netip.AddrFromSlice(wlx.WorkloadInfo.Address)
			cidr := util.ConvertAddressToCidr(addr.String())
			ipRange = append(ipRange, &xds.CidrRange{
				AddressPrefix: cidr.AddressPrefix,
				PrefixLen:     cidr.PrefixLen,
			})
		}
		chains = append(chains, tcpChain, httpChain)
		ipMatcher.RangeMatchers = append(ipMatcher.RangeMatchers,
			&matcher.IPMatcher_IPRangeMatcher{
				Ranges: ipRange,
				OnMatch: match.ToMatcher(match.NewAppProtocol(match.ProtocolMatch{
					TCP:  match.ToChain(tcpChain.Name),
					HTTP: match.ToChain(httpChain.Name),
				})),
			})
	}
	l := &listener.Listener{
		Name:              "internal",
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		ListenerFilters: []*listener.ListenerFilter{
			util.InternalListenerSetAddressFilter(),
			// TODO: This may affect the data path due to the server-first protocols triggering a time-out. Need exception filter.
			xdsfilters.HTTPInspector,
		},
		TrafficDirection: core.TrafficDirection_INBOUND,
		FilterChains:     chains,
		FilterChainMatcher: &matcher.Matcher{
			MatcherType: &matcher.Matcher_MatcherTree_{
				MatcherTree: &matcher.Matcher_MatcherTree{
					Input: match.DestinationIP,
					TreeType: &matcher.Matcher_MatcherTree_CustomMatch{
						CustomMatch: &xds.TypedExtensionConfig{
							Name:        "ip",
							TypedConfig: protoconv.MessageToAny(ipMatcher),
						},
					},
				},
			},
		},
	}
	return l
}

func (lb *ListenerBuilder) buildWaypointInboundOriginateConnect() *listener.Listener {
	name := "connect_originate"
	l := &listener.Listener{
		Name:              name,
		UseOriginalDst:    wrappers.Bool(false),
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		ListenerFilters:   []*listener.ListenerFilter{util.InternalListenerSetAddressFilter()},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name: wellknown.TCPProxy,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&tcp.TcpProxy{
						StatPrefix:       name,
						ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: name},
						TunnelingConfig: &tcp.TcpProxy_TunnelingConfig{
							Hostname: "%DOWNSTREAM_LOCAL_ADDRESS%",
						},
					}),
				},
			}},
		}},
	}
	return l
}

// buildWaypointHTTPFilters augments the common chain of Waypoint-bound HTTP filters.
// Authn/authz filters are pre-pended. Telemetry filters are appended.
func (lb *ListenerBuilder) buildWaypointHTTPFilters() (pre []*hcm.HttpFilter, post []*hcm.HttpFilter) {
	// TODO: consider dedicated listener class for waypoint filters
	cls := istionetworking.ListenerClassSidecarInbound
	wasm := lb.push.WasmPluginsByListenerInfo(lb.node, model.WasmPluginListenerInfo{
		Class: cls,
	})
	// TODO: how to deal with ext-authz? It will be in the ordering twice
	pre = append(pre, lb.authzCustomBuilder.BuildHTTP(cls)...)
	pre = extension.PopAppend(pre, wasm, extensions.PluginPhase_AUTHN)
	pre = append(pre, lb.authnBuilder.BuildHTTP(cls)...)
	pre = extension.PopAppend(pre, wasm, extensions.PluginPhase_AUTHZ)
	pre = append(pre, lb.authzBuilder.BuildHTTP(cls)...)
	// TODO: these feel like the wrong place to insert, but this retains backwards compatibility with the original implementation
	post = extension.PopAppend(post, wasm, extensions.PluginPhase_STATS)
	post = extension.PopAppend(post, wasm, extensions.PluginPhase_UNSPECIFIED_PHASE)
	post = append(post, lb.push.Telemetry.HTTPFilters(lb.node, cls)...)
	return
}

// buildWaypointInboundHTTPFilters builds the network filters that should be inserted before an HCM.
// This should only be used with HTTP; see buildInboundNetworkFilters for TCP
func (lb *ListenerBuilder) buildWaypointInboundHTTPFilters(svc *model.Service, cc inboundChainConfig, pre, post []*hcm.HttpFilter) []*listener.Filter {
	var filters []*listener.Filter
	if !lb.node.IsAmbient() {
		filters = append(filters, buildMetadataExchangeNetworkFilters(istionetworking.ListenerClassSidecarInbound)...)
	}

	httpOpts := &httpListenerOpts{
		routeConfig:      buildWaypointInboundHTTPRouteConfig(lb, svc, cc),
		rds:              "", // no RDS for inbound traffic
		useRemoteAddress: false,
		connectionManager: &hcm.HttpConnectionManager{
			// Append and forward client cert to backend.
			ForwardClientCertDetails: hcm.HttpConnectionManager_APPEND_FORWARD,
			SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
				Subject: proto.BoolTrue,
				Uri:     true,
				Dns:     true,
			},
			ServerName: EnvoyServerName,
		},
		protocol:   cc.port.Protocol,
		class:      istionetworking.ListenerClassSidecarInbound,
		statPrefix: cc.StatPrefix(),
		isWaypoint: true,
	}
	// See https://github.com/grpc/grpc-web/tree/master/net/grpc/gateway/examples/helloworld#configure-the-proxy
	if cc.port.Protocol.IsHTTP2() {
		httpOpts.connectionManager.Http2ProtocolOptions = &core.Http2ProtocolOptions{}
	}

	if features.HTTP10 || enableHTTP10(lb.node.Metadata.HTTP10) {
		httpOpts.connectionManager.HttpProtocolOptions = &core.Http1ProtocolOptions{
			AcceptHttp_10: true,
		}
	}
	h := lb.buildHTTPConnectionManager(httpOpts)

	// Last filter must be router.
	router := h.HttpFilters[len(h.HttpFilters)-1]
	h.HttpFilters = append(pre, h.HttpFilters[:len(h.HttpFilters)-1]...)
	h.HttpFilters = append(h.HttpFilters, post...)
	h.HttpFilters = append(h.HttpFilters, router)

	filters = append(filters, &listener.Filter{
		Name:       wellknown.HTTPConnectionManager,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(h)},
	})
	return filters
}

func buildWaypointInboundHTTPRouteConfig(lb *ListenerBuilder, svc *model.Service, cc inboundChainConfig) *route.RouteConfiguration {
	// TODO: Policy binding via VIP+Host is inapplicable for direct pod access.
	if svc == nil {
		return buildSidecarInboundHTTPRouteConfig(lb, cc)
	}
	vss := getConfigsForHost(svc.Hostname, lb.node.SidecarScope.EgressListeners[0].VirtualServices())
	if len(vss) == 0 {
		return buildSidecarInboundHTTPRouteConfig(lb, cc)
	}
	if len(vss) > 1 {
		log.Warnf("multiple virtual services for one service: %v", svc.Hostname)
	}
	vs := vss[0]

	// Typically we setup routes with the Host header match. However, for waypoint inbound we are actually using
	// hostname purely to match to the Service VIP. So we only need a single VHost, with routes compute based on the VS.
	// For destinations, we need to hit the inbound clusters if it is an internal destination, otherwise outbound.
	routes, err := lb.waypointInboundRoute(vs, int(cc.port.Port))
	if err != nil {
		return buildSidecarInboundHTTPRouteConfig(lb, cc)
	}

	inboundVHost := &route.VirtualHost{
		Name:    inboundVirtualHostPrefix + strconv.Itoa(int(cc.port.Port)), // Format: "inbound|http|%d"
		Domains: []string{"*"},
		Routes:  routes,
	}

	return &route.RouteConfiguration{
		Name:             cc.clusterName,
		VirtualHosts:     []*route.VirtualHost{inboundVHost},
		ValidateClusters: proto.BoolFalse,
	}
}

func (lb *ListenerBuilder) waypointInboundRoute(virtualService config.Config, listenPort int) ([]*route.Route, error) {
	vs, ok := virtualService.Spec.(*networking.VirtualService)
	if !ok { // should never happen
		return nil, fmt.Errorf("in not a virtual service: %#v", virtualService)
	}

	out := make([]*route.Route, 0, len(vs.Http))

	catchall := false
	for _, http := range vs.Http {
		if len(http.Match) == 0 {
			if r := lb.translateRoute(virtualService, http, nil, listenPort); r != nil {
				out = append(out, r)
			}
			catchall = true
		} else {
			for _, match := range http.Match {
				if r := lb.translateRoute(virtualService, http, match, listenPort); r != nil {
					out = append(out, r)
					// This is a catch all path. Routes are matched in order, so we will never go beyond this match
					// As an optimization, we can just top sending any more routes here.
					//if isCatchAllMatch(match) {
					//	catchall = true
					//	break
					//}
				}
			}
		}
		if catchall {
			break
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no routes matched")
	}
	return out, nil
}

func (lb *ListenerBuilder) translateRoute(
	virtualService config.Config,
	in *networking.HTTPRoute,
	match *networking.HTTPMatchRequest,
	listenPort int,
) *route.Route {
	// When building routes, it's okay if the target cluster cannot be
	// resolved Traffic to such clusters will blackhole.

	// Match by the destination port specified in the match condition
	if match != nil && match.Port != 0 && match.Port != uint32(listenPort) {
		return nil
	}

	routeName := in.Name
	if match != nil && match.Name != "" {
		routeName = routeName + "." + match.Name
	}

	out := &route.Route{
		Name:     routeName,
		Match:    istio_route.TranslateRouteMatch(lb.node, virtualService, match),
		Metadata: util.BuildConfigInfoMetadata(virtualService.Meta),
	}
	authority := ""
	if in.Headers != nil {
		operations := istio_route.TranslateHeadersOperations(in.Headers)
		out.RequestHeadersToAdd = operations.RequestHeadersToAdd
		out.ResponseHeadersToAdd = operations.ResponseHeadersToAdd
		out.RequestHeadersToRemove = operations.RequestHeadersToRemove
		out.ResponseHeadersToRemove = operations.ResponseHeadersToRemove
		authority = operations.Authority
	}

	if in.Redirect != nil {
		istio_route.ApplyRedirect(out, in.Redirect, listenPort, model.UseGatewaySemantics(virtualService))
	} else {
		lb.routeDestination(out, in, authority, listenPort)
	}

	out.Decorator = &route.Decorator{
		Operation: istio_route.GetRouteOperation(out, virtualService.Name, listenPort),
	}
	if in.Fault != nil {
		out.TypedPerFilterConfig = make(map[string]*any.Any)
		out.TypedPerFilterConfig[wellknown.Fault] = protoconv.MessageToAny(istio_route.TranslateFault(in.Fault))
	}

	return out
}

func (lb *ListenerBuilder) routeDestination(out *route.Route, in *networking.HTTPRoute, authority string, listenerPort int) {
	policy := in.Retries
	if policy == nil {
		// No VS policy set, use mesh defaults
		policy = lb.push.Mesh.GetDefaultHttpRetryPolicy()
	}
	action := &route.RouteAction{
		Cors:        istio_route.TranslateCORSPolicy(in.CorsPolicy),
		RetryPolicy: retry.ConvertPolicy(policy),
	}

	// Configure timeouts specified by Virtual Service if they are provided, otherwise set it to defaults.
	action.Timeout = features.DefaultRequestTimeout
	if in.Timeout != nil {
		action.Timeout = in.Timeout
	}
	// Use deprecated value for now as the replacement MaxStreamDuration has some regressions.
	// nolint: staticcheck
	action.MaxGrpcTimeout = action.Timeout

	out.Action = &route.Route_Route{Route: action}

	if in.Rewrite != nil {
		action.PrefixRewrite = in.Rewrite.GetUri()
		if in.Rewrite.GetAuthority() != "" {
			authority = in.Rewrite.GetAuthority()
		}
	}
	if authority != "" {
		action.HostRewriteSpecifier = &route.RouteAction_HostRewriteLiteral{
			HostRewriteLiteral: authority,
		}
	}

	if in.Mirror != nil {
		if mp := istio_route.MirrorPercent(in); mp != nil {
			action.RequestMirrorPolicies = []*route.RouteAction_RequestMirrorPolicy{{
				Cluster:         lb.GetDestinationCluster(in.Mirror, lb.serviceForHostname(host.Name(in.Mirror.Host)), listenerPort),
				RuntimeFraction: mp,
				TraceSampled:    &wrappers.BoolValue{Value: false},
			}}
		}
	}

	// TODO: eliminate this logic and use the total_weight option in envoy route
	weighted := make([]*route.WeightedCluster_ClusterWeight, 0)
	for _, dst := range in.Route {
		weight := &wrappers.UInt32Value{Value: uint32(dst.Weight)}
		if dst.Weight == 0 {
			// Ignore 0 weighted clusters if there are other clusters in the route.
			// But if this is the only cluster in the route, then add it as a cluster with weight 100
			if len(in.Route) == 1 {
				weight.Value = uint32(100)
			} else {
				continue
			}
		}
		hostname := host.Name(dst.GetDestination().GetHost())
		n := lb.GetDestinationCluster(dst.Destination, lb.serviceForHostname(hostname), listenerPort)
		clusterWeight := &route.WeightedCluster_ClusterWeight{
			Name:   n,
			Weight: weight,
		}
		if dst.Headers != nil {
			operations := istio_route.TranslateHeadersOperations(dst.Headers)
			clusterWeight.RequestHeadersToAdd = operations.RequestHeadersToAdd
			clusterWeight.RequestHeadersToRemove = operations.RequestHeadersToRemove
			clusterWeight.ResponseHeadersToAdd = operations.ResponseHeadersToAdd
			clusterWeight.ResponseHeadersToRemove = operations.ResponseHeadersToRemove
			if operations.Authority != "" {
				clusterWeight.HostRewriteSpecifier = &route.WeightedCluster_ClusterWeight_HostRewriteLiteral{
					HostRewriteLiteral: operations.Authority,
				}
			}
		}

		weighted = append(weighted, clusterWeight)
	}

	// rewrite to a single cluster if there is only weighted cluster
	if len(weighted) == 1 {
		action.ClusterSpecifier = &route.RouteAction_Cluster{Cluster: weighted[0].Name}
		out.RequestHeadersToAdd = append(out.RequestHeadersToAdd, weighted[0].RequestHeadersToAdd...)
		out.RequestHeadersToRemove = append(out.RequestHeadersToRemove, weighted[0].RequestHeadersToRemove...)
		out.ResponseHeadersToAdd = append(out.ResponseHeadersToAdd, weighted[0].ResponseHeadersToAdd...)
		out.ResponseHeadersToRemove = append(out.ResponseHeadersToRemove, weighted[0].ResponseHeadersToRemove...)
		if weighted[0].HostRewriteSpecifier != nil && action.HostRewriteSpecifier == nil {
			// Ideally, if the weighted cluster overwrites authority, it has precedence. This mirrors behavior of headers,
			// because for headers we append the weighted last which allows it to Set and wipe out previous Adds.
			// However, Envoy behavior is different when we set at both cluster level and route level, and we want
			// behavior to be consistent with a single cluster and multiple clusters.
			// As a result, we only override if the top level rewrite is not set
			action.HostRewriteSpecifier = &route.RouteAction_HostRewriteLiteral{
				HostRewriteLiteral: weighted[0].GetHostRewriteLiteral(),
			}
		}
	} else {
		action.ClusterSpecifier = &route.RouteAction_WeightedClusters{
			WeightedClusters: &route.WeightedCluster{
				Clusters: weighted,
			},
		}
	}
}

// GetDestinationCluster generates a cluster name for the route, or error if no cluster
// can be found. Called by translateRule to determine if
func (lb *ListenerBuilder) GetDestinationCluster(destination *networking.Destination, service *model.Service, listenerPort int) string {
	dir, subset, port := model.TrafficDirectionInboundVIP, "http", listenerPort
	if destination.Subset != "" {
		subset += "/" + destination.Subset
	}
	if destination.GetPort() != nil {
		port = int(destination.GetPort().GetNumber())
	} else if service != nil && len(service.Ports) == 1 {
		// if service only has one port defined, use that as the port, otherwise use default listenerPort
		port = service.Ports[0].Port

		// Do not return blackhole cluster for service==nil case as there is a legitimate use case for
		// calling this function with nil service: to route to a pre-defined statically configured cluster
		// declared as part of the bootstrap.
		// If blackhole cluster is needed, do the check on the caller side. See gateway and tls.go for examples.
	}

	if service != nil {
		_, svcs := FindAssociatedResources(lb.node, lb.push)
		_, f := svcs[service.Hostname]
		if !f || service.MeshExternal {
			// this waypoint proxy isn't responsible for this service so we use outbound; TODO quicker lookup
			dir, subset = model.TrafficDirectionOutbound, destination.Subset
		}
	}

	return model.BuildSubsetKey(
		dir,
		subset,
		host.Name(destination.Host),
		port,
	)
}

// TODO remove dupe with ztunnelgen
func buildCommonTLSContext(proxy *model.Proxy, push *model.PushContext) *tls.CommonTlsContext {
	ctx := &tls.CommonTlsContext{}
	security.ApplyToCommonTLSContext(ctx, proxy, nil, authn.TrustDomainsForValidation(push.Mesh), true)
	ctx.AlpnProtocols = []string{"h2"}
	ctx.TlsParams = &tls.TlsParameters{
		// Ensure TLS 1.3 is used everywhere
		TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
		TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
	}
	return ctx
}

// outboundTunnelListener is built for each ServiceAccount from pods on the node.
// This listener adds the original destination headers from the dynamic EDS metadata pass through.
// We build the listener per-service account so that it can point to the corresponding cluster that presents the correct cert.
func outboundTunnelListener(push *model.PushContext, proxy *model.Proxy) *listener.Listener {
	name := util.OutboundTunnel
	p := &tcp.TcpProxy{
		StatPrefix: name,
		// TODO
		// AccessLog:        accessLogString("outbound tunnel"),
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: name},
		TunnelingConfig: &tcp.TcpProxy_TunnelingConfig{
			Hostname: "%DOWNSTREAM_LOCAL_ADDRESS%",
		},
	}

	l := &listener.Listener{
		Name:              name,
		UseOriginalDst:    wrappers.Bool(false),
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		ListenerFilters:   []*listener.ListenerFilter{util.InternalListenerSetAddressFilter()},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{setAccessLogAndBuildTCPFilter(push, proxy, p, istionetworking.ListenerClassSidecarOutbound)},
		}},
	}
	accessLogBuilder.setListenerAccessLog(push, proxy, l, istionetworking.ListenerClassSidecarOutbound)
	return l
}
