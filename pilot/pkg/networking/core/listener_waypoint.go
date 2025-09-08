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

package core

import (
	"fmt"
	"net/netip"
	"strconv"
	"time"

	xds "github.com/cncf/xds/go/xds/core/v3"
	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	sfsvalue "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/common/set_filter_state/v3"
	sfs "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/set_filter_state/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	sfsnetwork "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/set_filter_state/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	celformatter "github.com/envoyproxy/go-control-plane/envoy/extensions/formatter/cel/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	googleproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/extension"
	"istio.io/istio/pilot/pkg/networking/core/match"
	istio_route "istio.io/istio/pilot/pkg/networking/core/route"
	"istio.io/istio/pilot/pkg/networking/core/tunnelingconfig"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/plugin/authz"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

// These are both the current defaults used by the ztunnel hyper http2 server
const (
	h2KeepaliveInterval = 10 * time.Second
	h2KeepaliveTimeout  = 20 * time.Second
)

func (lb *ListenerBuilder) serviceForHostname(name host.Name) *model.Service {
	return lb.push.ServiceForHostname(lb.node, name)
}

func (lb *ListenerBuilder) buildWaypointInbound() []*listener.Listener {
	listeners := []*listener.Listener{}
	// We create 3 listeners:
	// 1. Decapsulation CONNECT listener.
	// 2. IP dispatch listener, handling both VIPs and direct pod IPs.
	// 3. One of two options based on the type of waypoint:
	//    a. East-West Gateway: Forward the inner CONNECT to the backend ztunnel or waypoint.
	//    b. Regular Waypoint: Encapsulate the inner CONNECT and forward to the ztunnel.
	var forwarder *listener.Listener
	var orderedWPS []*model.Service
	wls, wps := findWaypointResources(lb.node, lb.push)
	if wps != nil {
		orderedWPS = wps.orderedServices
	}
	if features.EnableAmbientMultiNetwork && isEastWestGateway(lb.node) {
		forwarder = buildWaypointForwardInnerConnectListener(lb.push, lb.node)
	} else {
		forwarder = buildWaypointConnectOriginateListener(lb.push, lb.node)
	}
	listeners = append(listeners,
		lb.buildWaypointInboundConnectTerminate(),
		lb.buildWaypointInternal(wls, orderedWPS),
		forwarder)

	return listeners
}

func (lb *ListenerBuilder) buildHCMConnectTerminateChain(routes []*route.Route) []*listener.Filter {
	ph := util.GetProxyHeaders(lb.node, lb.push, istionetworking.ListenerClassSidecarInbound)
	h := &hcm.HttpConnectionManager{
		StatPrefix: ConnectTerminate,
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &route.RouteConfiguration{
				Name: "default",
				VirtualHosts: []*route.VirtualHost{{
					Name:    "default",
					Domains: []string{"*"},
					Routes:  routes,
				}},
			},
		},
		// Append and forward client cert to backend, if configured
		ForwardClientCertDetails: ph.ForwardedClientCert,
		SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
			Subject: proto.BoolTrue,
			Uri:     true,
			Dns:     true,
		},
		ServerName:                 ph.ServerName,
		ServerHeaderTransformation: ph.ServerHeaderTransformation,
		GenerateRequestId:          ph.GenerateRequestID,
		UseRemoteAddress:           proto.BoolFalse,
	}
	accessLogBuilder.setHboneTerminationAccessLog(lb.push, lb.node, h, istionetworking.ListenerClassSidecarInbound)

	// Protocol settings
	h.StreamIdleTimeout = istio_route.Notimeout
	h.UpgradeConfigs = []*hcm.HttpConnectionManager_UpgradeConfig{{
		UpgradeType: ConnectUpgradeType,
	}}
	h.Http2ProtocolOptions = &core.Http2ProtocolOptions{
		AllowConnect: true,
		// TODO(https://github.com/istio/istio/issues/43443)
		// All streams are bound to the same worker. Therefore, we need to limit for better fairness.
		// TODO: This is probably too low for east/west gateways; maybe need to let this be configurable.
		MaxConcurrentStreams: &wrappers.UInt32Value{Value: 100},
		// well behaved clients should close connections.
		// not all clients are well-behaved. This will prune
		// connections when the client is not responding, to keep
		// us from holding many stale conns from deceased clients
		//
		// Also TODO(https://github.com/hyperium/hyper/pull/3647)
		ConnectionKeepalive: &core.KeepaliveSettings{
			Interval: durationpb.New(h2KeepaliveInterval),
			Timeout:  durationpb.New(h2KeepaliveTimeout),
		},
	}

	var filters []*hcm.HttpFilter

	if features.WaypointLayeredAuthorizationPolicies {
		authzBuilder := authz.NewWaypointTerminationBuilder(authz.Local, lb.push, lb.node)
		// We want L4 semantics, but applied to an HTTP filter.
		// If we put them as a network filter, we get poor logs and cannot return an error at the CONNECT level
		filters = authzBuilder.BuildTCPRulesAsHTTPFilter()
	}
	// Filters needed to propagate the tunnel metadata to the inner streams.
	h.HttpFilters = append(filters,
		xdsfilters.WaypointDownstreamMetadataFilter,
		xdsfilters.ConnectAuthorityFilter,
		xdsfilters.BuildRouterFilter(xdsfilters.RouterFilterContext{
			StartChildSpan:       false,
			SuppressDebugHeaders: ph.SuppressDebugHeaders,
		}),
	)
	return []*listener.Filter{{
		Name:       wellknown.HTTPConnectionManager,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(h)},
	}}
}

func (lb *ListenerBuilder) buildConnectTerminateListener(routes []*route.Route) *listener.Listener {
	actualWildcard, _ := getWildcardsAndLocalHost(lb.node.GetIPMode())
	bind := actualWildcard

	l := &listener.Listener{
		Name:    ConnectTerminate,
		Address: util.BuildAddress(bind[0], model.HBoneInboundListenPort),
		FilterChains: []*listener.FilterChain{
			{
				Name: "default",
				TransportSocket: &core.TransportSocket{
					Name: "tls",
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
						CommonTlsContext:         buildCommonConnectTLSContext(lb.node, lb.push),
						RequireClientCertificate: &wrappers.BoolValue{Value: true},
					})},
				},
				Filters: lb.buildHCMConnectTerminateChain(routes),
			},
		},
		// for HBONE inbound specifically, we want to prefer exact balance.
		// This is because by definition we have fewer, longer-lived connections,
		// and want to avoid issues where (for instance) 2 HBONE tunnel connections
		// (each with ~100 connections multiplexed) both end up on the same envoy worker thread,
		// which effectively defeats the HBONE pooling ztunnel does.
		//
		// With sandwiching, this isn't a concern, as ztunnel handles HBONE decap, and this listener
		// wouldn't be used anyway.
		ConnectionBalanceConfig: &listener.Listener_ConnectionBalanceConfig{
			BalanceType: &listener.Listener_ConnectionBalanceConfig_ExactBalance_{
				ExactBalance: &listener.Listener_ConnectionBalanceConfig_ExactBalance{},
			},
		},
	}
	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, l, istionetworking.ListenerClassSidecarInbound)
	if len(actualWildcard) > 1 {
		l.AdditionalAddresses = util.BuildAdditionalAddresses(bind[1:], model.HBoneInboundListenPort)
	}
	return l
}

func (lb *ListenerBuilder) buildWaypointInboundConnectTerminate() *listener.Listener {
	routes := []*route.Route{{
		Match: &route.RouteMatch{
			PathSpecifier: &route.RouteMatch_ConnectMatcher_{ConnectMatcher: &route.RouteMatch_ConnectMatcher{}},
		},
		Action: &route.Route_Route{Route: &route.RouteAction{
			UpgradeConfigs: []*route.RouteAction_UpgradeConfig{{
				UpgradeType:   ConnectUpgradeType,
				ConnectConfig: &route.RouteAction_UpgradeConfig_ConnectConfig{},
			}},
			ClusterSpecifier: &route.RouteAction_Cluster{Cluster: MainInternalName},
		}},
	}}
	return lb.buildConnectTerminateListener(routes)
}

// This is the regular waypoint flow, where we terminate the tunnel, and then re-encap.
func (lb *ListenerBuilder) buildWaypointInternal(wls []model.WorkloadInfo, svcs []*model.Service) *listener.Listener {
	isEastWestGateway := isEastWestGateway(lb.node)
	ipMatcher := &matcher.IPMatcher{}
	svcHostnameMap := &matcher.Matcher_MatcherTree_MatchMap{
		Map: make(map[string]*matcher.Matcher_OnMatch),
	}
	chains := []*listener.FilterChain{}
	portProtocols := map[int]protocol.Instance{}
	getOrigDstSfs := func(ipAndPort string, isHttp bool) *listener.Filter {
		celTemplate := `%%CEL('%s' in filter_state ? filter_state['%s']: r'%s')%%`
		celEval := fmt.Sprintf(celTemplate, xdsfilters.OriginalDstFilterStateKey, xdsfilters.OriginalDstFilterStateKey, ipAndPort)
		filterStateValue := &sfsvalue.FilterStateValue{
			Key: &sfsvalue.FilterStateValue_ObjectKey{
				ObjectKey: "envoy.network.transport_socket.original_dst_address",
			},
			Value: &sfsvalue.FilterStateValue_FormatString{
				FormatString: &core.SubstitutionFormatString{
					Formatters: []*core.TypedExtensionConfig{
						{
							Name:        "envoy.formatter.cel",
							TypedConfig: protoconv.MessageToAny(&celformatter.Cel{}),
						},
					},
					Format: &core.SubstitutionFormatString_TextFormatSource{
						TextFormatSource: &core.DataSource{
							Specifier: &core.DataSource_InlineString{
								// If we have a valid original destination in filter state, use it. Else,
								// fall back to the original destination address.
								InlineString: celEval,
							},
						},
					},
				},
			},
		}
		var msg googleproto.Message
		if isHttp {
			msg = &sfs.Config{
				OnRequestHeaders: []*sfsvalue.FilterStateValue{filterStateValue},
			}
		} else {
			msg = &sfsnetwork.Config{
				OnNewConnection: []*sfsvalue.FilterStateValue{filterStateValue},
			}
		}
		return &listener.Filter{
			Name: "set_dst_address",
			ConfigType: &listener.Filter_TypedConfig{
				TypedConfig: protoconv.MessageToAny(msg),
			},
		}
	}
	for _, svc := range svcs {
		svcAddresses := svc.GetAllAddressesForProxy(lb.node)
		portMapper := match.NewDestinationPort()
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			portString := strconv.Itoa(port.Port)
			authorityKey := fmt.Sprintf("%s:%d", svc.Hostname, port.Port)
			tcpClusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "tcp", svc.Hostname, port.Port)

			cc := inboundChainConfig{
				clusterName:   tcpClusterName,
				policyService: svc,
				port: model.ServiceInstancePort{
					ServicePort: port,
					TargetPort:  uint32(port.Port),
				},
				bind:  "0.0.0.0",
				hbone: true,
				telemetryMetadata: telemetry.FilterChainMetadata{
					InstanceHostname:           svc.Hostname,
					KubernetesServiceNamespace: svc.Attributes.Namespace,
					KubernetesServiceName:      svc.Attributes.Name,
				},
			}
			var tcpChain, httpChain *listener.FilterChain
			origDst := svc.GetAddressForProxy(lb.node) + ":" + portString
			httpClusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", svc.Hostname, port.Port)
			var filters []*listener.Filter
			if len(svcAddresses) > 0 && features.EnableAmbientMultiNetwork && !isEastWestGateway {
				filters = []*listener.Filter{getOrigDstSfs(origDst, false)}
			}
			tcpChain = &listener.FilterChain{
				Filters: append(slices.Clone(filters), lb.buildWaypointNetworkFilters(svc, cc)...),
				Name:    cc.clusterName,
			}
			cc.clusterName = httpClusterName
			httpChain = &listener.FilterChain{
				Filters: append(slices.Clone(filters), lb.buildWaypointInboundHTTPFilters(svc, cc)...),
				Name:    cc.clusterName,
			}
			if isEastWestGateway && features.EnableAmbientMultiNetwork {
				// We want to send to all ports regardless of protocol, but we want the filter chains to tcp proxy no matter what
				// (since we're expecting double-hbone). There's no point in sniffing, so we just send to the TCP chain.
				chains = append(chains, tcpChain)
				// Pick the TCP chain; as long as the cluster exists (and we'll ensure it does if we're terminating),
				// traffic should end up at the correct destination
				portMapper.Map[portString] = match.ToChain(tcpClusterName)
				svcHostnameMap.Map[authorityKey] = match.ToChain(tcpChain.Name)
				portProtocols[port.Port] = protocol.TCP // Inner HBONE with no SNI == TCP protocol for listener purposes
			} else {
				if port.Protocol.IsUnsupported() {
					// If we need to sniff, insert two chains and the protocol detector
					chains = append(chains, tcpChain, httpChain)
					protocolMatcher := match.ToMatcher(match.NewAppProtocol(match.ProtocolMatch{
						TCP:  match.ToChain(tcpClusterName),
						HTTP: match.ToChain(httpClusterName),
					}))
					portMapper.Map[portString] = protocolMatcher
					svcHostnameMap.Map[authorityKey] = protocolMatcher
					portProtocols[port.Port] = protocol.Unsupported
				} else if port.Protocol.IsHTTP() {
					// Otherwise, just insert HTTP/TCP
					chains = append(chains, httpChain)
					portMapper.Map[portString] = match.ToChain(httpChain.Name)
					svcHostnameMap.Map[authorityKey] = match.ToChain(httpChain.Name)
					// TCP and HTTP on the same port, mark it as requiring sniffing
					if portProtocols[port.Port] != "" && portProtocols[port.Port] != protocol.HTTP {
						portProtocols[port.Port] = protocol.Unsupported
					} else {
						portProtocols[port.Port] = protocol.HTTP
					}
				} else {
					// Note that this could include double hbone
					chains = append(chains, tcpChain)
					portMapper.Map[portString] = match.ToChain(tcpChain.Name)
					svcHostnameMap.Map[authorityKey] = match.ToChain(tcpChain.Name)
					// TCP and HTTP on the same port, mark it as requiring sniffing
					if portProtocols[port.Port] != "" && !portProtocols[port.Port].IsTCP() {
						portProtocols[port.Port] = protocol.Unsupported
					} else {
						portProtocols[port.Port] = port.Protocol
					}
				}
			}

			// If the service has no addresses, we don't want this waypoint to be able to serve its hostname
			if len(svcAddresses) == 0 {
				delete(svcHostnameMap.Map, authorityKey)
			}
		}
		if len(portMapper.Map) > 0 {
			ranges := slices.Map(svcAddresses, func(vip string) *xds.CidrRange {
				cidr := util.ConvertAddressToCidr(vip)
				return &xds.CidrRange{
					AddressPrefix: cidr.AddressPrefix,
					PrefixLen:     cidr.PrefixLen,
				}
			})
			rangeMatcher := &matcher.IPMatcher_IPRangeMatcher{
				Ranges:  ranges,
				OnMatch: match.ToMatcher(portMapper.Matcher),
			}
			ipMatcher.RangeMatchers = append(ipMatcher.RangeMatchers, rangeMatcher)
		}
	}

	// TODO: remove when e/w gateway supports workload addressing
	if !isEastWestGateway {
		// Direct pod access chain.
		cc := inboundChainConfig{
			clusterName: EncapClusterName,
			port: model.ServiceInstancePort{
				ServicePort: &model.Port{
					Name:     "unknown",
					Protocol: protocol.TCP,
				},
			},
			bind:  "0.0.0.0",
			hbone: true,
		}
		tcpChain := &listener.FilterChain{
			Filters: append([]*listener.Filter{
				xdsfilters.ConnectAuthorityNetworkFilter,
			},
				lb.buildWaypointNetworkFilters(nil, cc)...),
			Name: "direct-tcp",
		}
		// TODO: maintains undesirable persistent HTTP connections to "encap"
		httpChain := &listener.FilterChain{
			Filters: append([]*listener.Filter{
				xdsfilters.ConnectAuthorityNetworkFilter,
			},
				lb.buildWaypointInboundHTTPFilters(nil, cc)...),
			Name: "direct-http",
		}

		chains = append(chains, tcpChain, httpChain)
		if len(wls) > 0 {
			// Workload IP filtering happens here.
			ipRange := []*xds.CidrRange{}
			for _, wl := range wls {
				for _, ip := range wl.Workload.Addresses {
					addr, _ := netip.AddrFromSlice(ip)
					cidr := util.ConvertAddressToCidr(addr.String())
					ipRange = append(ipRange, &xds.CidrRange{
						AddressPrefix: cidr.AddressPrefix,
						PrefixLen:     cidr.PrefixLen,
					})
				}
			}
			if len(ipRange) > 0 {
				// Empty can happen if we have workloads, but none have an Address (DNS)
				ipMatcher.RangeMatchers = append(ipMatcher.RangeMatchers,
					&matcher.IPMatcher_IPRangeMatcher{
						Ranges: ipRange,
						OnMatch: match.ToMatcher(match.NewAppProtocol(match.ProtocolMatch{
							TCP:  match.ToChain(tcpChain.Name),
							HTTP: match.ToChain(httpChain.Name),
						})),
					})
			}
		}
	}
	// This may affect the data path due to the server-first protocols triggering a time-out.
	// Currently, we attempt to exclude ports where we can, but it's not perfect.
	// https://github.com/envoyproxy/envoy/issues/35958 is likely required for an optimal solution
	httpInspector := xdsfilters.HTTPInspector
	// If there are workloads, any port is accepted on them, so we cannot opt out
	if len(wls) == 0 {
		// Otherwise, find all the ports that have only TCP or only HTTP
		nonInspectorPorts := []int{}
		for p, proto := range portProtocols {
			if !proto.IsUnsupported() {
				nonInspectorPorts = append(nonInspectorPorts, p)
			}
		}
		if len(nonInspectorPorts) > 0 {
			// Sort for stable output, then replace the filter with one disabling the ports.
			slices.Sort(nonInspectorPorts)
			httpInspector = &listener.ListenerFilter{
				Name:           wellknown.HTTPInspector,
				ConfigType:     httpInspector.ConfigType,
				FilterDisabled: listenerPredicateExcludePorts(nonInspectorPorts),
			}
		}
	}
	tlsInspector := func() *listener.ListenerFilter {
		tlsPorts := sets.New[int]()
		nonTLSPorts := sets.New[int]()
		for _, s := range svcs {
			for _, p := range s.Ports {
				if p.Protocol.IsTLS() {
					tlsPorts.Insert(p.Port)
				} else {
					nonTLSPorts.Insert(p.Port)
				}
			}
		}
		nonInspectorPorts := nonTLSPorts.DeleteAll(tlsPorts.UnsortedList()...).UnsortedList()
		if len(nonInspectorPorts) > 0 {
			slices.Sort(nonInspectorPorts)
			return &listener.ListenerFilter{
				Name:           wellknown.TLSInspector,
				ConfigType:     xdsfilters.TLSInspector.ConfigType,
				FilterDisabled: listenerPredicateExcludePorts(nonInspectorPorts),
			}
		}
		return nil
	}()

	l := &listener.Listener{
		Name:              MainInternalName,
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		ListenerFilters: []*listener.ListenerFilter{
			xdsfilters.OriginalDestination,
			httpInspector,
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

	if len(svcHostnameMap.Map) > 0 && features.EnableAmbientMultiNetwork {
		l.FilterChainMatcher.OnNoMatch = &matcher.Matcher_OnMatch{
			OnMatch: &matcher.Matcher_OnMatch_Matcher{
				Matcher: &matcher.Matcher{
					MatcherType: &matcher.Matcher_MatcherTree_{
						MatcherTree: &matcher.Matcher_MatcherTree{
							Input: match.AuthorityFilterStateInput,
							TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
								ExactMatchMap: svcHostnameMap,
							},
						},
					},
				},
			},
		}
	}
	if tlsInspector != nil {
		l.ListenerFilters = append(l.ListenerFilters, tlsInspector)
	}

	if features.EnableAmbientMultiNetwork && isEastWestGateway {
		// If there are no filter chains, populate a dummy one that never matches
		if len(l.FilterChains) == 0 {
			l.FilterChains = []*listener.FilterChain{{
				Name: model.VirtualInboundBlackholeFilterChainName,
				Filters: []*listener.Filter{{
					Name: wellknown.TCPProxy,
					ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(&tcp.TcpProxy{
						StatPrefix:       util.BlackHoleCluster,
						ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
					})},
				}},
			}}
		}
	}

	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, l, istionetworking.ListenerClassSidecarInbound)
	return l
}

func buildWaypointConnectOriginateListener(push *model.PushContext, proxy *model.Proxy) *listener.Listener {
	return buildConnectOriginateListener(push, proxy, istionetworking.ListenerClassSidecarInbound)
}

func buildWaypointForwardInnerConnectListener(push *model.PushContext, proxy *model.Proxy) *listener.Listener {
	return buildForwardInnerConnectListener(push, proxy, istionetworking.ListenerClassSidecarInbound)
}

func buildForwardInnerConnectListener(push *model.PushContext, proxy *model.Proxy, class istionetworking.ListenerClass) *listener.Listener {
	return buildConnectForwarder(push, proxy, class, ForwardInnerConnect, false)
}

func buildConnectForwarder(push *model.PushContext, proxy *model.Proxy, class istionetworking.ListenerClass,
	clusterName string, tunnel bool,
) *listener.Listener {
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       clusterName,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: clusterName},
	}
	if tunnel {
		tcpProxy.TunnelingConfig = &tcp.TcpProxy_TunnelingConfig{
			Hostname: "%DOWNSTREAM_LOCAL_ADDRESS%",
		}
		// Set access logs. These are filtered down to only connection establishment errors, to avoid double logs in most cases.
		accessLogBuilder.setHboneOriginationAccessLog(push, proxy, tcpProxy, class)
	} else {
		accessLogBuilder.setTCPAccessLogWithFilter(push, proxy, tcpProxy, class, nil, &accesslog.AccessLogFilter{
			FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
				// UF: upstream failure, we couldn't connect. This is important to log at this layer, since the error details
				// are lost otherwise.
				ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"UF"}},
			},
		})
	}

	l := &listener.Listener{
		Name:              clusterName,
		UseOriginalDst:    wrappers.Bool(false),
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		ListenerFilters: []*listener.ListenerFilter{
			xdsfilters.OriginalDestination,
		},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name: wellknown.TCPProxy,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(tcpProxy),
				},
			}},
		}},
	}
	accessLogBuilder.setListenerAccessLog(push, proxy, l, class)
	return l
}

func buildConnectOriginateListener(push *model.PushContext, proxy *model.Proxy, class istionetworking.ListenerClass) *listener.Listener {
	return buildConnectForwarder(push, proxy, class, ConnectOriginate, true)
}

// buildWaypointHTTPFilters augments the common chain of Waypoint-bound HTTP filters.
// Authn/authz filters are prepended. Telemetry filters are appended.
func (lb *ListenerBuilder) buildWaypointHTTPFilters(svc *model.Service) (pre []*hcm.HttpFilter, post []*hcm.HttpFilter) {
	authzCustomBuilder := lb.authzCustomBuilder
	authzBuilder := lb.authzBuilder
	authnBuilder := lb.authnBuilder
	if svc != nil {
		authnBuilder = authn.NewBuilderForService(lb.push, lb.node, svc)
		authzBuilder = authz.NewBuilderForService(authz.Local, lb.push, lb.node, true, svc)
		authzCustomBuilder = authz.NewBuilderForService(authz.Custom, lb.push, lb.node, true, svc)
	}

	// TODO: consider dedicated listener class for waypoint filters
	cls := istionetworking.ListenerClassSidecarInbound
	wasm := lb.push.WasmPluginsByListenerInfo(lb.node,
		model.WasmPluginListenerInfo{Class: cls}.WithService(svc),
		model.WasmPluginTypeHTTP,
	)
	// TODO: how to deal with ext-authz? It will be in the ordering twice
	// TODO policies here will need to be different per-chain (service attached)
	pre = append(pre, authzCustomBuilder.BuildHTTP(cls)...)
	pre = extension.PopAppendHTTP(pre, wasm, extensions.PluginPhase_AUTHN)
	pre = append(pre, authnBuilder.BuildHTTP(cls)...)
	pre = extension.PopAppendHTTP(pre, wasm, extensions.PluginPhase_AUTHZ)
	pre = append(pre, authzBuilder.BuildHTTP(cls)...)
	// TODO: these feel like the wrong place to insert, but this retains backwards compatibility with the original implementation
	post = extension.PopAppendHTTP(post, wasm, extensions.PluginPhase_STATS)
	post = extension.PopAppendHTTP(post, wasm, extensions.PluginPhase_UNSPECIFIED_PHASE)
	post = append(post, xdsfilters.WaypointUpstreamMetadataFilter)
	post = append(post, lb.push.Telemetry.HTTPFilters(lb.node, cls, svc)...)
	return
}

// buildWaypointInboundHTTPFilters builds the network filters that should be inserted before an HCM.
// This should only be used with HTTP; see buildInboundNetworkFilters for TCP
func (lb *ListenerBuilder) buildWaypointInboundHTTPFilters(svc *model.Service, cc inboundChainConfig) []*listener.Filter {
	pre, post := lb.buildWaypointHTTPFilters(svc)
	ph := util.GetProxyHeaders(lb.node, lb.push, istionetworking.ListenerClassSidecarInbound)
	var filters []*listener.Filter
	httpOpts := &httpListenerOpts{
		routeConfig:      buildWaypointInboundHTTPRouteConfig(lb, svc, cc),
		rds:              "", // no RDS for inbound traffic
		useRemoteAddress: false,
		connectionManager: &hcm.HttpConnectionManager{
			ServerName:                 ph.ServerName,
			ServerHeaderTransformation: ph.ServerHeaderTransformation,
			GenerateRequestId:          ph.GenerateRequestID,
			AppendXForwardedPort:       ph.XForwardedPort,
		},
		suppressEnvoyDebugHeaders: ph.SuppressDebugHeaders,
		protocol:                  cc.port.Protocol,
		class:                     istionetworking.ListenerClassSidecarInbound,
		statPrefix:                cc.StatPrefix(),
		isWaypoint:                true,
		policySvc:                 svc,
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

// buildInboundNetworkFilters generates a TCP proxy network filter on the inbound path
func (lb *ListenerBuilder) buildWaypointNetworkFilters(svc *model.Service, fcc inboundChainConfig) []*listener.Filter {
	var svcHostname host.Name
	var subsetName string

	statPrefix := fcc.clusterName
	// If stat name is configured, build the stat prefix from configured pattern.
	if len(lb.push.Mesh.InboundClusterStatName) != 0 {
		statPrefix = telemetry.BuildInboundStatPrefix(lb.push.Mesh.InboundClusterStatName, fcc.telemetryMetadata, "", uint32(fcc.port.Port), fcc.port.Name)
	}
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: fcc.clusterName},
	}
	var destinationRule *networking.DestinationRule
	if svc != nil {
		svcHostname = svc.Hostname
		virtualServices := getVirtualServiceForWaypoint(lb.node.ConfigNamespace, svc, lb.node.SidecarScope.EgressListeners[0].VirtualServices())
		routes := getWaypointTCPRoutes(virtualServices, svcHostname.String(), fcc.port.Port)
		if len(routes) > 0 {
			// Existing (slightly incorrect, but best we can do) semantics.
			// We are routing to multiple destinations, but want TCPProxy level configuration which is shared.
			// We have to just pick one to use as the settings.
			svcHostname = host.Name(routes[0].Destination.Host)
		}
		if len(routes) == 1 {
			// Similar to above, we have to pick one subset. Not sure why this logic is different but this aligns with sidecars.
			subsetName = routes[0].Destination.Subset
		}
		destinationRule = CastDestinationRule(lb.node.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, lb.node, svcHostname).GetRule())

		if len(routes) == 1 {
			route := routes[0]
			service := lb.push.ServiceForHostname(lb.node, host.Name(route.Destination.Host))
			clusterName := lb.getWaypointDestinationCluster(route.Destination, service, fcc.port.Port)
			tcpProxy.ClusterSpecifier = &tcp.TcpProxy_Cluster{Cluster: clusterName}
		} else if len(routes) > 1 {
			clusterSpecifier := &tcp.TcpProxy_WeightedClusters{
				WeightedClusters: &tcp.TcpProxy_WeightedCluster{},
			}
			for _, route := range routes {
				service := lb.push.ServiceForHostname(lb.node, host.Name(route.Destination.Host))
				if route.Weight > 0 {
					clusterName := lb.getWaypointDestinationCluster(route.Destination, service, fcc.port.Port)
					clusterSpecifier.WeightedClusters.Clusters = append(clusterSpecifier.WeightedClusters.Clusters, &tcp.TcpProxy_WeightedCluster_ClusterWeight{
						Name:   clusterName,
						Weight: uint32(route.Weight),
					})
				}
			}
			tcpProxy.ClusterSpecifier = clusterSpecifier
		}
	}

	// To align with sidecars, subsets are ignored here...?
	conPool := destinationRule.GetTrafficPolicy().GetConnectionPool().GetTcp()
	tcpProxy.IdleTimeout = conPool.GetIdleTimeout()
	if tcpProxy.IdleTimeout == nil {
		tcpProxy.IdleTimeout = parseDuration(lb.node.Metadata.IdleTimeout)
	}
	tcpProxy.MaxDownstreamConnectionDuration = conPool.GetMaxConnectionDuration()

	maybeSetHashPolicy(destinationRule, tcpProxy, subsetName)
	tunnelingconfig.Apply(tcpProxy, destinationRule, subsetName)

	tcpFilter := setAccessLogAndBuildTCPFilter(lb.push, lb.node, tcpProxy, istionetworking.ListenerClassSidecarInbound, fcc.policyService)
	networkFilterstack := buildNetworkFiltersStack(fcc.port.Protocol, tcpFilter, statPrefix, fcc.clusterName)
	return lb.buildCompleteNetworkFilters(istionetworking.ListenerClassSidecarInbound, fcc.port.Port, networkFilterstack, true, fcc.policyService)
}

var meshGateways = sets.New(constants.IstioMeshGateway)

func getWaypointTCPRoutes(configs []config.Config, svcHostname string, port int) []*networking.RouteDestination {
	for _, vs := range configs {
		// Per https://gateway-api.sigs.k8s.io/geps/gep-1294/?h=xroute#route-types, respect TLS routes before TCP routes
		if match := getTLSRouteMatch(vs.Spec.(*networking.VirtualService).GetTls(), svcHostname, port); match != nil {
			return match
		}
	}
	for _, vs := range configs {
		if match := getTCPRouteMatch(vs.Spec.(*networking.VirtualService).GetTcp(), port); match != nil {
			return match
		}
	}
	return nil
}

func getTLSRouteMatch(tls []*networking.TLSRoute, svcHostname string, port int) []*networking.RouteDestination {
	for _, rule := range tls {
		if len(rule.Match) == 0 {
			return rule.Route
		}
		for _, m := range rule.Match {
			// Waypoint does not currently support SNI matches. This could be done, but would require more extensive refactoring
			// of filter chains. Given TLSRoute does not support a config *not* matching this check (though VirtualService does)
			// there is still some value in respecting the routes
			if len(m.SniHosts) != 1 || m.SniHosts[0] != svcHostname {
				continue
			}
			if matchTLS(
				m,
				nil, // No source labels support
				meshGateways,
				port,
				"", // No source namespace support
			) {
				return rule.Route
			}
		}
	}
	return nil
}

func getTCPRouteMatch(tcp []*networking.TCPRoute, port int) []*networking.RouteDestination {
	for _, rule := range tcp {
		if len(rule.Match) == 0 {
			return rule.Route
		}
		for _, m := range rule.Match {
			if matchTCP(
				m,
				nil, // No source labels support
				meshGateways,
				port,
				"", // No source namespace support
			) {
				return rule.Route
			}
		}
	}
	return nil
}

func buildWaypointInboundHTTPRouteConfig(lb *ListenerBuilder, svc *model.Service, cc inboundChainConfig) *route.RouteConfiguration {
	// TODO: Policy binding via VIP+Host is inapplicable for direct pod access.
	if svc == nil {
		return buildSidecarInboundHTTPRouteConfig(lb, cc)
	}

	virtualServices := getVirtualServiceForWaypoint(lb.node.ConfigNamespace, svc, lb.node.SidecarScope.EgressListeners[0].VirtualServices())
	vs := slices.FindFunc(virtualServices, func(c config.Config) bool {
		// Find the first HTTP virtual service
		return c.Spec.(*networking.VirtualService).Http != nil
	})
	if vs == nil {
		return buildSidecarInboundHTTPRouteConfig(lb, cc)
	}

	// Typically we setup routes with the Host header match. However, for waypoint inbound we are actually using
	// hostname purely to match to the Service VIP. So we only need a single VHost, with routes compute based on the VS.
	// For destinations, we need to hit the inbound clusters if it is an internal destination, otherwise outbound.
	routes, err := lb.waypointInboundRoute(*vs, cc.port.Port)
	if err != nil {
		return buildSidecarInboundHTTPRouteConfig(lb, cc)
	}

	inboundVHost := &route.VirtualHost{
		Name:    inboundVirtualHostPrefix + strconv.Itoa(cc.port.Port), // Format: "inbound|http|%d"
		Domains: []string{"*"},
		Routes:  routes,
	}

	// Serviceentries should be patched when virtualservice exists.
	return envoyfilter.ApplyRouteConfigurationPatches(networking.EnvoyFilter_SIDECAR_INBOUND, lb.node,
		lb.push.EnvoyFilters(lb.node),
		&route.RouteConfiguration{
			Name:             cc.clusterName,
			VirtualHosts:     []*route.VirtualHost{inboundVHost},
			ValidateClusters: proto.BoolFalse,
		})
}

// Select the config pertaining to the service being processed.
func getVirtualServiceForWaypoint(configNamespace string, svc *model.Service, configs []config.Config) []config.Config {
	var matching []config.Config
	for _, cfg := range configs {
		if cfg.Namespace != configNamespace && cfg.Namespace != svc.Attributes.Namespace {
			// We only allow routes in the same namespace as the service or in the waypoint's own namespace
			continue
		}
		virtualService := cfg.Spec.(*networking.VirtualService)

		for _, vsHost := range virtualService.Hosts {
			if host.Name(vsHost).Matches(svc.Hostname) {
				matching = append(matching, cfg)
				break
			}
		}
	}
	return matching
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
			if r := lb.translateWaypointRoute(virtualService, http, nil, listenPort); r != nil {
				out = append(out, r)
			}
			// This is a catchall route, so we can stop processing the rest of the routes.
			break
		}
		for _, match := range http.Match {
			// Source label matching is not supported in ambient mode, and thus not supported in
			// waypoint routes. Any matchers containing source labels should be dropped.
			// See https://github.com/istio/istio/issues/51565 for more details and discussion.
			if len(match.SourceLabels) > 0 {
				continue
			}

			if r := lb.translateWaypointRoute(virtualService, http, match, listenPort); r != nil {
				out = append(out, r)
				// This is a catch all path. Routes are matched in order, so we will never go beyond this match
				// As an optimization, we can just stop sending any more routes here.
				if istio_route.IsCatchAllRoute(r) {
					catchall = true
					break
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

var meshGateway = sets.New("mesh")

func (lb *ListenerBuilder) translateWaypointRoute(
	virtualService config.Config,
	in *networking.HTTPRoute,
	match *networking.HTTPMatchRequest,
	listenPort int,
) *route.Route {
	opts := istio_route.RouteOptions{
		IsTLS:                     false,
		IsHTTP3AltSvcHeaderNeeded: false,
		Mesh:                      lb.push.Mesh,
		Push:                      lb.push,
		LookupService:             lb.serviceForHostname,
		LookupDestinationCluster:  lb.getWaypointDestinationCluster,
		LookupHash: func(destination *networking.HTTPRouteDestination) *networking.LoadBalancerSettings_ConsistentHashLB {
			lb, _ := istio_route.HashForHTTPDestination(lb.push, lb.node, destination)
			return lb
		},
	}
	return istio_route.TranslateRoute(
		lb.node,
		in,
		match,
		listenPort,
		virtualService,
		meshGateway,
		opts,
	)
}

// getWaypointDestinationCluster generates a cluster name for the route. If the destination is invalid
// or cannot be found, "UnknownService" is returned.
func (lb *ListenerBuilder) getWaypointDestinationCluster(destination *networking.Destination, service *model.Service, listenerPort int) string {
	if len(destination.GetHost()) == 0 {
		// only happens when the gateway-api BackendRef is invalid
		return "UnknownService"
	}
	dir, port := model.TrafficDirectionInboundVIP, listenerPort

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

	subset := portToSubset(service, port, destination)
	if service != nil {
		_, wps := findWaypointResources(lb.node, lb.push)
		_, f := wps.services[service.Hostname]
		if !f {
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

// portToSubset helps translate a port to the waypoint subset to use
func portToSubset(service *model.Service, port int, destination *networking.Destination) string {
	var p *model.Port
	var ok bool
	if service != nil {
		p, ok = service.Ports.GetByPort(port)
	}
	if !ok {
		// Port is unknown.
		if destination != nil && destination.Subset != "" {
			return "http/" + destination.Subset
		}
		return "http"
	}
	// Ambient will have the subset as <protocol>[/subset]. Pick that based on the service information
	subset := "tcp"
	if p.Protocol.IsHTTP() || p.Protocol.IsUnsupported() {
		subset = "http"
	}
	if destination.Subset != "" {
		subset += "/" + destination.Subset
	}
	return subset
}

// NB: Un-typed SAN validation is ignored when typed is used, so only typed version must be used with this function.
func buildCommonConnectTLSContext(proxy *model.Proxy, push *model.PushContext) *tls.CommonTlsContext {
	ctx := &tls.CommonTlsContext{}
	security.ApplyToCommonTLSContext(ctx, proxy, nil, "", nil, true, nil)
	aliases := authn.TrustDomainsForValidation(push.Mesh)
	validationCtx := ctx.GetCombinedValidationContext().DefaultValidationContext
	if len(aliases) > 0 {
		matchers := util.StringToPrefixMatch(security.AppendURIPrefixToTrustDomain(aliases))
		for _, matcher := range matchers {
			validationCtx.MatchTypedSubjectAltNames = append(validationCtx.MatchTypedSubjectAltNames, &tls.SubjectAltNameMatcher{
				SanType: tls.SubjectAltNameMatcher_URI,
				Matcher: matcher,
			})
		}
	}
	ctx.AlpnProtocols = []string{"h2"}
	ctx.TlsParams = &tls.TlsParameters{
		// Ensure TLS 1.3 is used everywhere
		TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
		TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
	}
	// Compliance for Envoy tunnel TLS contexts.
	security.EnforceCompliance(ctx)
	return ctx
}
