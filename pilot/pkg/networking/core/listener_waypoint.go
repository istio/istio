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
	"regexp"
	"strconv"
	"time"

	xds "github.com/cncf/xds/go/xds/core/v3"
	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
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
	envoymatcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	googleproto "google.golang.org/protobuf/proto"
	any "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/extension"
	"istio.io/istio/pilot/pkg/networking/core/match"
	istio_route "istio.io/istio/pilot/pkg/networking/core/route"
	"istio.io/istio/pilot/pkg/networking/core/route/retry"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/plugin/authz"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
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
	// 3. Encapsulation CONNECT listener, originating the tunnel
	wls, wps := findWaypointResources(lb.node, lb.push)

	listeners = append(listeners,
		lb.buildWaypointInboundConnectTerminate(),
		lb.buildWaypointInternal(wls, wps.orderedServices),
		buildWaypointConnectOriginateListener(lb.push, lb.node))

	return listeners
}

func (lb *ListenerBuilder) buildHCMConnectTerminateChain(routes []*route.Route) []*listener.Filter {
	ph := GetProxyHeaders(lb.node, lb.push, istionetworking.ListenerClassSidecarInbound)
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

	// Protocol settings
	h.StreamIdleTimeout = istio_route.Notimeout
	h.UpgradeConfigs = []*hcm.HttpConnectionManager_UpgradeConfig{{
		UpgradeType: ConnectUpgradeType,
	}}
	h.Http2ProtocolOptions = &core.Http2ProtocolOptions{
		AllowConnect: true,
		// TODO(https://github.com/istio/istio/issues/43443)
		// All streams are bound to the same worker. Therefore, we need to limit for better fairness.
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

	// Filters needed to propagate the tunnel metadata to the inner streams.
	h.HttpFilters = []*hcm.HttpFilter{
		xdsfilters.WaypointDownstreamMetadataFilter,
		xdsfilters.ConnectAuthorityFilter,
		xdsfilters.BuildRouterFilter(xdsfilters.RouterFilterContext{
			StartChildSpan:       false,
			SuppressDebugHeaders: ph.SuppressDebugHeaders,
		}),
	}
	return []*listener.Filter{
		{
			Name:       wellknown.HTTPConnectionManager,
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(h)},
		},
	}
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

func (lb *ListenerBuilder) buildWaypointInternal(wls []model.WorkloadInfo, svcs []*model.Service) *listener.Listener {
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
			if len(svcAddresses) > 0 && features.EnableAmbientMultiNetwork {
				setOrigDstForCluster := []*listener.Filter{getOrigDstSfs(origDst, false)}
				tcpChain = &listener.FilterChain{
					Filters: append(setOrigDstForCluster, lb.buildInboundNetworkFilters(cc)...),
					Name:    cc.clusterName,
				}
				cc.clusterName = httpClusterName
				httpChain = &listener.FilterChain{
					Filters: append(setOrigDstForCluster, lb.buildWaypointInboundHTTPFilters(svc, cc)...),
					Name:    cc.clusterName,
				}
			} else {
				tcpChain = &listener.FilterChain{
					Filters: lb.buildInboundNetworkFilters(cc),
					Name:    cc.clusterName,
				}
				cc.clusterName = httpClusterName
				httpChain = &listener.FilterChain{
					Filters: lb.buildWaypointInboundHTTPFilters(svc, cc),
					Name:    cc.clusterName,
				}
			}
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

	{
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
				lb.buildInboundNetworkFilters(cc)...),
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
	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, l, istionetworking.ListenerClassSidecarInbound)
	return l
}

func buildWaypointConnectOriginateListener(push *model.PushContext, proxy *model.Proxy) *listener.Listener {
	return buildConnectOriginateListener(push, proxy, istionetworking.ListenerClassSidecarInbound)
}

func buildConnectOriginateListener(push *model.PushContext, proxy *model.Proxy, class istionetworking.ListenerClass) *listener.Listener {
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       ConnectOriginate,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: ConnectOriginate},
		TunnelingConfig: &tcp.TcpProxy_TunnelingConfig{
			Hostname: "%DOWNSTREAM_LOCAL_ADDRESS%",
		},
	}
	// Set access logs. These are filtered down to only connection establishment errors, to avoid double logs in most cases.
	accessLogBuilder.setHboneAccessLog(push, proxy, tcpProxy, class)
	l := &listener.Listener{
		Name:              ConnectOriginate,
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

// buildWaypointHTTPFilters augments the common chain of Waypoint-bound HTTP filters.
// Authn/authz filters are pre-pended. Telemetry filters are appended.
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
	ph := GetProxyHeaders(lb.node, lb.push, istionetworking.ListenerClassSidecarInbound)
	var filters []*listener.Filter
	httpOpts := &httpListenerOpts{
		routeConfig:      buildWaypointInboundHTTPRouteConfig(lb, svc, cc),
		rds:              "", // no RDS for inbound traffic
		useRemoteAddress: false,
		connectionManager: &hcm.HttpConnectionManager{
			ServerName:                 ph.ServerName,
			ServerHeaderTransformation: ph.ServerHeaderTransformation,
			GenerateRequestId:          ph.GenerateRequestID,
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

func buildWaypointInboundHTTPRouteConfig(lb *ListenerBuilder, svc *model.Service, cc inboundChainConfig) *route.RouteConfiguration {
	// TODO: Policy binding via VIP+Host is inapplicable for direct pod access.
	if svc == nil {
		return buildSidecarInboundHTTPRouteConfig(lb, cc)
	}
	vs := getVirtualServiceForWaypoint(lb.node.ConfigNamespace, svc, lb.node.SidecarScope.EgressListeners[0].VirtualServices())
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

	return &route.RouteConfiguration{
		Name:             cc.clusterName,
		VirtualHosts:     []*route.VirtualHost{inboundVHost},
		ValidateClusters: proto.BoolFalse,
	}
}

// Select the config pertaining to the service being processed.
func getVirtualServiceForWaypoint(configNamespace string, svc *model.Service, configs []config.Config) *config.Config {
	for _, cfg := range configs {
		if cfg.Namespace != configNamespace && cfg.Namespace != svc.Attributes.Namespace {
			// We only allow routes in the same namespace as the service or in the waypoint's own namespace
			continue
		}
		virtualService := cfg.Spec.(*networking.VirtualService)
		for _, vsHost := range virtualService.Hosts {
			if host.Name(vsHost).Matches(svc.Hostname) {
				return &cfg
			}
		}
	}
	return nil
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

func (lb *ListenerBuilder) translateWaypointRoute(
	virtualService config.Config,
	in *networking.HTTPRoute,
	match *networking.HTTPMatchRequest,
	listenPort int,
) *route.Route {
	gatewaySemantics := model.UseGatewaySemantics(virtualService)
	// When building routes, it's okay if the target cluster cannot be
	// resolved. Traffic to such clusters will blackhole.

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
		Match:    istio_route.TranslateRouteMatch(virtualService, match),
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
		istio_route.ApplyRedirect(out, in.Redirect, listenPort, false, model.UseGatewaySemantics(virtualService))
	} else if in.DirectResponse != nil {
		istio_route.ApplyDirectResponse(out, in.DirectResponse)
	} else {
		lb.waypointRouteDestination(out, in, authority, listenPort, gatewaySemantics)
	}

	out.Decorator = &route.Decorator{
		Operation: istio_route.GetRouteOperation(out, virtualService.Name, listenPort),
	}
	if in.Fault != nil || in.CorsPolicy != nil {
		out.TypedPerFilterConfig = make(map[string]*any.Any)
	}
	if in.Fault != nil {
		out.TypedPerFilterConfig[wellknown.Fault] = protoconv.MessageToAny(istio_route.TranslateFault(in.Fault))
	}
	if in.CorsPolicy != nil {
		out.TypedPerFilterConfig[wellknown.CORS] = protoconv.MessageToAny(istio_route.TranslateCORSPolicy(lb.node, in.CorsPolicy))
	}

	return out
}

func (lb *ListenerBuilder) waypointRouteDestination(
	out *route.Route,
	in *networking.HTTPRoute,
	authority string,
	listenerPort int,
	gatewaySemantics bool,
) {
	policy := in.Retries
	if policy == nil {
		// No VS policy set, use mesh defaults
		policy = lb.push.Mesh.GetDefaultHttpRetryPolicy()
	}
	action := &route.RouteAction{
		RetryPolicy: retry.ConvertPolicy(policy, false),
	}

	// Configure timeouts specified by Virtual Service if they are provided, otherwise set it to defaults.
	action.Timeout = istio_route.Notimeout
	if in.Timeout != nil {
		action.Timeout = in.Timeout
	}
	// Use deprecated value for now as the replacement MaxStreamDuration has some regressions.
	// TODO: check and see if the replacement has been fixed.
	// nolint: staticcheck
	action.MaxGrpcTimeout = action.Timeout

	if gatewaySemantics {
		// return 500 for invalid backends
		// https://github.com/kubernetes-sigs/gateway-api/blob/cea484e38e078a2c1997d8c7a62f410a1540f519/apis/v1beta1/httproute_types.go#L204
		action.ClusterNotFoundResponseCode = route.RouteAction_INTERNAL_SERVER_ERROR
	}
	out.Action = &route.Route_Route{Route: action}

	if in.Rewrite != nil {
		if regexRewrite := in.Rewrite.GetUriRegexRewrite(); regexRewrite != nil {
			action.RegexRewrite = &envoymatcher.RegexMatchAndSubstitute{
				Pattern: &envoymatcher.RegexMatcher{
					Regex: regexRewrite.Match,
				},
				Substitution: regexRewrite.Rewrite,
			}
		} else if uri := in.Rewrite.GetUri(); uri != "" {
			if gatewaySemantics && uri == "/" {
				// remove the prefix
				action.RegexRewrite = &envoymatcher.RegexMatchAndSubstitute{
					Pattern: &envoymatcher.RegexMatcher{
						Regex: fmt.Sprintf(`^%s(/?)(.*)`, regexp.QuoteMeta(out.Match.GetPathSeparatedPrefix())),
					},
					// hold `/` in case the entire path is removed
					Substitution: `/\2`,
				}
			} else {
				action.PrefixRewrite = uri
			}
		}
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
			cluster := lb.getWaypointDestinationCluster(in.Mirror, lb.serviceForHostname(host.Name(in.Mirror.Host)), listenerPort)
			action.RequestMirrorPolicies = append(action.RequestMirrorPolicies,
				istio_route.TranslateRequestMirrorPolicyCluster(cluster, mp))
		}
	}
	for _, mirror := range in.Mirrors {
		if mp := istio_route.MirrorPercentByPolicy(mirror); mp != nil && mirror.Destination != nil {
			cluster := lb.getWaypointDestinationCluster(mirror.Destination, lb.serviceForHostname(host.Name(mirror.Destination.Host)), listenerPort)
			action.RequestMirrorPolicies = append(action.RequestMirrorPolicies,
				istio_route.TranslateRequestMirrorPolicyCluster(cluster, mp))
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
		n := lb.getWaypointDestinationCluster(dst.Destination, lb.serviceForHostname(hostname), listenerPort)
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
	security.ApplyToCommonTLSContext(ctx, proxy, nil, "", nil, true)
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
