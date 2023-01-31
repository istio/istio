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
	"strconv"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	any "google.golang.org/protobuf/types/known/anypb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/match"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route/retry"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	istiomatcher "istio.io/istio/pilot/pkg/security/authz/matcher"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

type WorkloadAndServices struct {
	WorkloadInfo ambient.Workload
	Services     []*model.Service
}

func FindAssociatedResources(node *model.Proxy, push *model.PushContext) ([]WorkloadAndServices, map[host.Name]*model.Service) {
	wls := []WorkloadAndServices{}
	var workloads []ambient.Workload
	if sa, f := node.Metadata.Annotations["istio.io/service-account"]; f {
		ident := spiffe.MustGenSpiffeURI(node.ConfigNamespace, sa)
		workloads = push.AmbientIndex.Workloads.ByIdentity[ident]
	} else {
		workloads = push.AmbientIndex.Workloads.ByNamespace[node.ConfigNamespace]
	}
	for _, wl := range workloads {
		if wl.Labels[ambient.LabelType] != ambient.TypeWorkload {
			continue
		}
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
	// We create 4 listeners:
	// 1. Our top level terminating CONNECT listener, `inbound TERMINATE`. This has a route per destination and decapsulates the CONNECT,
	//    forwarding to the VIP or Pod internal listener.
	// 2. (many) VIP listeners, `inbound-vip||hostname|port`. This will apply service policies. For typical case (not redirecting to external service),
	//    this will end up forwarding to a cluster for the same VIP, which will have endpoints for each Pod internal listener
	// 3. (many) Pod listener, `inbound-pod||podip|port`. This is one per inbound pod. Will go through HCM if needed, in order to apply L7 policies (authz)
	//    Note: we need both a pod listener and a VIP listener since we need to apply policies at different levels (routing vs authz).
	// 4. Our final CONNECT listener, originating the tunnel
	wls, svcs := FindAssociatedResources(lb.node, lb.push)

	listeners = append(listeners, lb.buildWaypointInboundTerminateConnect(svcs, wls))

	// VIP listeners
	listeners = append(listeners, lb.buildWaypointInboundVIP(svcs)...)

	// Pod listeners
	listeners = append(listeners, lb.buildWaypointInboundPod(wls)...)

	listeners = append(listeners, lb.buildWaypointInboundOriginateConnect())

	return listeners
}

// Our top level terminating CONNECT listener, `inbound TERMINATE`. This has a route per destination and decapsulates the CONNECT,
// forwarding to the VIP or Pod internal listener.
func (lb *ListenerBuilder) buildWaypointInboundTerminateConnect(svcs map[host.Name]*model.Service, wls []WorkloadAndServices) *listener.Listener {
	actualWildcard, _ := getActualWildcardAndLocalHost(lb.node)
	// CONNECT listener
	vhost := &route.VirtualHost{
		Name:    "connect",
		Domains: []string{"*"},
	}
	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "internal", svc.Hostname, port.Port)
			vhost.Routes = append(vhost.Routes, &route.Route{
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_ConnectMatcher_{ConnectMatcher: &route.RouteMatch_ConnectMatcher{}},
					Headers: []*route.HeaderMatcher{
						istiomatcher.HeaderMatcher(":authority", fmt.Sprintf("%s:%d", svc.GetAddressForProxy(lb.node), port.Port)),
					},
				},
				Action: &route.Route_Route{Route: &route.RouteAction{
					UpgradeConfigs: []*route.RouteAction_UpgradeConfig{{
						UpgradeType:   "CONNECT",
						ConnectConfig: &route.RouteAction_UpgradeConfig_ConnectConfig{},
					}},
					ClusterSpecifier: &route.RouteAction_Cluster{Cluster: clusterName},
				}},
			})
		}
	}

	// it's possible for us to hit this listener and target a Pod directly; route through the inbound-pod internal listener
	// TODO: this shouldn't match on port; we should accept traffic to any port.
	for _, wlx := range wls {
		wl := wlx.WorkloadInfo
		// TODO: fake proxy is really bad. Should have these take in Workload or similar
		instances := lb.Discovery.GetProxyServiceInstances(&model.Proxy{
			Type:            model.SidecarProxy,
			IPAddresses:     []string{wl.PodIP},
			ConfigNamespace: wl.Namespace,
			Metadata: &model.NodeMetadata{
				Namespace: wl.Namespace,
				Labels:    wl.Labels,
			},
		})
		// For each port, setup a route
		for _, port := range getPorts(instances) {
			clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundPod, "internal", host.Name(wl.PodIP), port.Port)
			vhost.Routes = append(vhost.Routes, &route.Route{
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_ConnectMatcher_{ConnectMatcher: &route.RouteMatch_ConnectMatcher{}},
					Headers: []*route.HeaderMatcher{
						istiomatcher.HeaderMatcher(":authority", fmt.Sprintf("%s:%d", wl.PodIP, port.Port)),
					},
				},
				Action: &route.Route_Route{Route: &route.RouteAction{
					UpgradeConfigs: []*route.RouteAction_UpgradeConfig{{
						UpgradeType:   "CONNECT",
						ConnectConfig: &route.RouteAction_UpgradeConfig_ConnectConfig{},
					}},
					ClusterSpecifier: &route.RouteAction_Cluster{Cluster: clusterName},
				}},
			})
		}
	}

	httpOpts := &httpListenerOpts{
		routeConfig: &route.RouteConfiguration{
			Name:             "local_route",
			VirtualHosts:     []*route.VirtualHost{vhost},
			ValidateClusters: proto.BoolFalse,
		},
		statPrefix:           "inbound_hcm",
		protocol:             protocol.HTTP2,
		class:                istionetworking.ListenerClassSidecarInbound,
		skipTelemetryFilters: true, // do not include telemetry filters on the CONNECT termination chain
		skipRBACFilters:      true,
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
	}

	h := lb.buildHTTPConnectionManager(httpOpts)

	h.UpgradeConfigs = []*hcm.HttpConnectionManager_UpgradeConfig{{
		UpgradeType: "CONNECT",
	}}
	h.Http2ProtocolOptions = &core.Http2ProtocolOptions{
		AllowConnect: true,
	}
	h.HttpFilters = append([]*hcm.HttpFilter{xdsfilters.ConnectBaggageFilter}, h.HttpFilters...)
	name := "inbound_CONNECT_terminate"
	l := &listener.Listener{
		Name:    name,
		Address: util.BuildAddress(actualWildcard, model.HBoneInboundListenPort),
		FilterChains: []*listener.FilterChain{
			{
				Name: name,
				TransportSocket: &core.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
						CommonTlsContext: buildCommonTLSContext(lb.node, nil, lb.push, true),
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

func (lb *ListenerBuilder) buildWaypointInboundOriginateConnect() *listener.Listener {
	name := "inbound_CONNECT_originate"
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
							HeadersToAdd: []*core.HeaderValueOption{
								{Header: &core.HeaderValue{Key: "x-envoy-original-dst-host", Value: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
							},
						},
					}),
				},
			}},
		}},
	}
	return l
}

// VIP listeners, `inbound||hostname|port`. This will apply service policies. For typical case (not redirecting to external service),
// this will end up forwarding to a cluster for the same VIP, which will have endpoints for each Pod internal listener
func (lb *ListenerBuilder) buildWaypointInboundVIP(svcs map[host.Name]*model.Service) []*listener.Listener {
	listeners := []*listener.Listener{}
	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			cc := inboundChainConfig{
				clusterName: model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "tcp", svc.Hostname, port.Port),
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
				Filters: lb.buildWaypointInboundVIPHTTPFilters(svc, cc),
				Name:    httpName,
			}
			l := &listener.Listener{
				Name:              name,
				ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
				TrafficDirection:  core.TrafficDirection_INBOUND,
				FilterChains:      []*listener.FilterChain{},
				ListenerFilters: []*listener.ListenerFilter{
					util.InternalListenerSetAddressFilter(),
				},
			}
			if port.Protocol.IsUnsupported() {
				// If we need to sniff, insert two chains and the protocol detector
				l.FilterChains = append(l.FilterChains, tcpChain, httpChain)
				l.FilterChainMatcher = match.NewAppProtocol(match.ProtocolMatch{
					TCP:  match.ToChain(tcpName),
					HTTP: match.ToChain(httpName),
				})
			} else if port.Protocol.IsHTTP() {
				// Otherwise, just insert HTTP/TCP
				l.FilterChains = append(l.FilterChains, httpChain)
			} else {
				l.FilterChains = append(l.FilterChains, tcpChain)
			}
			listeners = append(listeners, l)
		}
	}
	return listeners
}

// (many) Pod listener, `inbound||podip|port`. This is one per inbound pod. Will go through HCM if needed, in order to apply L7 policies (authz)
// Note: we need both a pod listener and a VIP listener since we need to apply policies at different levels (routing vs authz).
func (lb *ListenerBuilder) buildWaypointInboundPod(wls []WorkloadAndServices) []*listener.Listener {
	listeners := []*listener.Listener{}
	for _, wlx := range wls {
		// Follow same logic as today, but no mTLS ever
		wl := wlx.WorkloadInfo

		// For each port, setup a match
		// TODO: fake proxy is really bad. Should have these take in Workload or similar
		instances := lb.Discovery.GetProxyServiceInstances(&model.Proxy{
			Type:            model.SidecarProxy,
			IPAddresses:     []string{wl.PodIP},
			ConfigNamespace: wl.Namespace,
			Metadata: &model.NodeMetadata{
				Namespace: wl.Namespace,
				Labels:    wl.Labels,
			},
		})
		if len(instances) == 0 {
			// TODO: Don't we need some passthrough mechanism? We will need ORIG_PORT but custom IP to implement that though
			continue
		}
		wlBuilder := lb.WithWorkload(wl)
		for _, port := range getPorts(instances) {
			if port.Protocol == protocol.UDP {
				continue
			}
			cc := inboundChainConfig{
				clusterName: model.BuildSubsetKey(model.TrafficDirectionInboundPod, "", host.Name(wl.PodIP), port.Port),
				port: ServiceInstancePort{
					Name:       port.Name,
					Port:       uint32(port.Port),
					TargetPort: uint32(port.Port),
					Protocol:   port.Protocol,
				},
				bind:  "0.0.0.0",
				hbone: true,
			}
			name := cc.clusterName

			tcpName := name + "-tcp"
			tcpChain := &listener.FilterChain{
				Filters: wlBuilder.buildInboundNetworkFilters(cc),
				Name:    tcpName,
			}

			httpName := name + "-http"
			httpChain := &listener.FilterChain{
				Filters: wlBuilder.buildInboundNetworkFiltersForHTTP(cc),
				Name:    httpName,
			}
			l := &listener.Listener{
				Name:              name,
				ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
				ListenerFilters:   []*listener.ListenerFilter{util.InternalListenerSetAddressFilter()},
				TrafficDirection:  core.TrafficDirection_INBOUND,
				FilterChains:      []*listener.FilterChain{},
			}
			if port.Protocol.IsUnsupported() {
				// If we need to sniff, insert two chains and the protocol detector
				l.FilterChains = append(l.FilterChains, tcpChain, httpChain)
				l.FilterChainMatcher = match.NewAppProtocol(match.ProtocolMatch{
					TCP:  match.ToChain(tcpName),
					HTTP: match.ToChain(httpName),
				})
			} else if port.Protocol.IsHTTP() {
				// Otherwise, just insert HTTP/TCP
				l.FilterChains = append(l.FilterChains, httpChain)
			} else {
				l.FilterChains = append(l.FilterChains, tcpChain)
			}
			listeners = append(listeners, l)
		}
	}
	return listeners
}

func getPorts(services []*model.ServiceInstance) []model.Port {
	p := map[int]model.Port{}
	for _, s := range services {
		p[int(s.Endpoint.EndpointPort)] = model.Port{
			Port:     int(s.Endpoint.EndpointPort),
			Protocol: s.ServicePort.Protocol,
		}
	}
	pl := []model.Port{}
	for _, m := range p {
		pl = append(pl, m)
	}
	return pl
}

// buildWaypointInboundVIPHTTPFilters builds the network filters that should be inserted before an HCM.
// This should only be used with HTTP; see buildInboundNetworkFilters for TCP
func (lb *ListenerBuilder) buildWaypointInboundVIPHTTPFilters(svc *model.Service, cc inboundChainConfig) []*listener.Filter {
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
		protocol:        cc.port.Protocol,
		class:           istionetworking.ListenerClassSidecarInbound,
		statPrefix:      cc.StatPrefix(),
		skipRBACFilters: true, // Handled by pod listener
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

	filters = append(filters, &listener.Filter{
		Name:       wellknown.HTTPConnectionManager,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(h)},
	})
	return filters
}

func buildWaypointInboundHTTPRouteConfig(lb *ListenerBuilder, svc *model.Service, cc inboundChainConfig) *route.RouteConfiguration {
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
		istio_route.ApplyRedirect(out, in.Redirect, listenPort)
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
func buildCommonTLSContext(proxy *model.Proxy, workload *ambient.Workload, push *model.PushContext, inbound bool) *tls.CommonTlsContext {
	ctx := &tls.CommonTlsContext{}
	security.ApplyToCommonTLSContext(ctx, proxy, nil, authn.TrustDomainsForValidation(push.Mesh), inbound)

	// TODO always use the below flow, always specify which workload
	if workload != nil {
		// present the workload cert if possible
		workloadSecret := workload.Identity()
		if workload.UID != "" {
			workloadSecret += "~" + workload.Name + "~" + workload.UID
		}
		ctx.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
			security.ConstructSdsSecretConfig(workloadSecret),
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
			HeadersToAdd: []*core.HeaderValueOption{
				{Header: &core.HeaderValue{Key: "x-envoy-original-dst-host", Value: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
			},
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
