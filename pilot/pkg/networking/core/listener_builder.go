// Copyright Istio Authors. All Rights Reserved.
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
	"time"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/extension"
	istio_route "istio.io/istio/pilot/pkg/networking/core/route"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/plugin/authz"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/pkg/xds/requestidextension"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/wellknown"
)

// A stateful listener builder
// Support the below intentions
// 1. Use separate inbound capture listener(:15006) and outbound capture listener(:15001)
// 2. The above listeners use bind_to_port sub listeners or filter chains.
type ListenerBuilder struct {
	node              *model.Proxy
	push              *model.PushContext
	gatewayListeners  []*listener.Listener
	inboundListeners  []*listener.Listener
	outboundListeners []*listener.Listener
	// HttpProxyListener is a specialize outbound listener. See MeshConfig.proxyHttpPort
	httpProxyListener       *listener.Listener
	virtualOutboundListener *listener.Listener
	virtualInboundListener  *listener.Listener

	envoyFilterWrapper *model.MergedEnvoyFilterWrapper

	// authnBuilder provides access to authn (mTLS) configuration for the given proxy.
	authnBuilder *authn.Builder
	// authzBuilder provides access to authz configuration for the given proxy.
	authzBuilder *authz.Builder
	// authzCustomBuilder provides access to CUSTOM authz configuration for the given proxy.
	authzCustomBuilder *authz.Builder
}

// enabledInspector captures if for a given listener, listener filter inspectors are added
type enabledInspector struct {
	HTTPInspector bool
	TLSInspector  bool
}

func NewListenerBuilder(node *model.Proxy, push *model.PushContext) *ListenerBuilder {
	builder := &ListenerBuilder{
		node: node,
		push: push,
	}
	builder.authnBuilder = authn.NewBuilder(push, node)
	builder.authzBuilder = authz.NewBuilder(authz.Local, push, node, node.Type == model.Waypoint)
	builder.authzCustomBuilder = authz.NewBuilder(authz.Custom, push, node, node.Type == model.Waypoint)
	return builder
}

func maxConnectionsToAcceptPerSocketEvent() *wrappers.UInt32Value {
	if features.MaxConnectionsToAcceptPerSocketEvent > 0 {
		return &wrappers.UInt32Value{Value: uint32(features.MaxConnectionsToAcceptPerSocketEvent)}
	}
	return nil
}

func (lb *ListenerBuilder) appendSidecarInboundListeners() *ListenerBuilder {
	lb.inboundListeners = lb.buildInboundListeners()
	if lb.node.EnableHBONEListen() {
		lb.inboundListeners = append(lb.inboundListeners, lb.buildInboundHBONEListeners()...)
	}

	return lb
}

func (lb *ListenerBuilder) appendSidecarOutboundListeners() *ListenerBuilder {
	lb.outboundListeners = lb.buildSidecarOutboundListeners(lb.node, lb.push)
	return lb
}

func (lb *ListenerBuilder) buildHTTPProxyListener() *ListenerBuilder {
	httpProxy := lb.buildHTTPProxy(lb.node, lb.push)
	if httpProxy == nil {
		return lb
	}
	lb.httpProxyListener = httpProxy
	return lb
}

func (lb *ListenerBuilder) buildVirtualOutboundListener() *ListenerBuilder {
	if lb.node.GetInterceptionMode() == model.InterceptionNone {
		// virtual listener is not necessary since workload is not using IPtables for traffic interception
		return lb
	}

	var isTransparentProxy *wrappers.BoolValue
	if lb.node.GetInterceptionMode() == model.InterceptionTproxy {
		isTransparentProxy = proto.BoolTrue
	}

	filterChains := buildOutboundCatchAllNetworkFilterChains(lb.node, lb.push)

	actualWildcards, _ := getWildcardsAndLocalHost(lb.node.GetIPMode())
	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	ipTablesListener := &listener.Listener{
		Name:                                 model.VirtualOutboundListenerName,
		Address:                              util.BuildAddress(actualWildcards[0], uint32(lb.push.Mesh.ProxyListenPort)),
		Transparent:                          isTransparentProxy,
		UseOriginalDst:                       proto.BoolTrue,
		FilterChains:                         filterChains,
		TrafficDirection:                     core.TrafficDirection_OUTBOUND,
		MaxConnectionsToAcceptPerSocketEvent: maxConnectionsToAcceptPerSocketEvent(),
	}
	// add extra addresses for the listener
	if features.EnableDualStack && len(actualWildcards) > 1 {
		ipTablesListener.AdditionalAddresses = util.BuildAdditionalAddresses(actualWildcards[1:], uint32(lb.push.Mesh.ProxyListenPort))
	} else if features.EnableAdditionalIpv4OutboundListenerForIpv6Only && (lb.node.GetIPMode() == model.IPv6) {
		// add an additional IPv4 outbound listener for IPv6 only clusters
		ipv4Wildcards, _ := getWildcardsAndLocalHost(model.IPv4) // get the IPv4 based wildcards
		ipTablesListener.AdditionalAddresses = util.BuildAdditionalAddresses(ipv4Wildcards[0:], uint32(lb.push.Mesh.ProxyListenPort))
	}

	class := model.OutboundListenerClass(lb.node.Type)
	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, ipTablesListener, class)
	lb.virtualOutboundListener = ipTablesListener
	return lb
}

func (lb *ListenerBuilder) patchOneListener(l *listener.Listener, ctx networking.EnvoyFilter_PatchContext) *listener.Listener {
	if l == nil {
		return nil
	}
	tempArray := []*listener.Listener{l}
	tempArray = envoyfilter.ApplyListenerPatches(ctx, lb.envoyFilterWrapper, tempArray, true)
	// temp array will either be empty [if virtual listener was removed] or will have a modified listener
	if len(tempArray) == 0 {
		return nil
	}
	return tempArray[0]
}

func (lb *ListenerBuilder) patchListeners() {
	lb.envoyFilterWrapper = lb.push.EnvoyFilters(lb.node)
	if lb.envoyFilterWrapper == nil {
		return
	}

	if lb.node.Type == model.Router {
		lb.gatewayListeners = envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_GATEWAY, lb.envoyFilterWrapper,
			lb.gatewayListeners, false)
		return
	}

	lb.virtualOutboundListener = lb.patchOneListener(lb.virtualOutboundListener, networking.EnvoyFilter_SIDECAR_OUTBOUND)
	lb.virtualInboundListener = lb.patchOneListener(lb.virtualInboundListener, networking.EnvoyFilter_SIDECAR_INBOUND)
	lb.httpProxyListener = lb.patchOneListener(lb.httpProxyListener, networking.EnvoyFilter_SIDECAR_OUTBOUND)
	lb.inboundListeners = envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_SIDECAR_INBOUND, lb.envoyFilterWrapper, lb.inboundListeners, false)
	lb.outboundListeners = envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_SIDECAR_OUTBOUND, lb.envoyFilterWrapper, lb.outboundListeners, false)
}

func (lb *ListenerBuilder) getListeners() []*listener.Listener {
	if lb.node.Type == model.Router {
		return lb.gatewayListeners
	}
	nInbound, nOutbound := len(lb.inboundListeners), len(lb.outboundListeners)
	nHTTPProxy, nVirtual := 0, 0
	if lb.httpProxyListener != nil {
		nHTTPProxy = 1
	}
	if lb.virtualOutboundListener != nil {
		nVirtual = 1
	}

	nListener := nInbound + nOutbound + nHTTPProxy + nVirtual

	listeners := make([]*listener.Listener, 0, nListener)
	listeners = append(listeners, lb.outboundListeners...)
	if lb.httpProxyListener != nil {
		listeners = append(listeners, lb.httpProxyListener)
	}
	if lb.virtualOutboundListener != nil {
		listeners = append(listeners, lb.virtualOutboundListener)
	}
	listeners = append(listeners, lb.inboundListeners...)

	log.Debugf("Build %d listeners for node %s including %d outbound, %d http proxy, "+
		"%d virtual outbound",
		nListener,
		lb.node.ID,
		nOutbound,
		nHTTPProxy,
		nVirtual,
	)
	return listeners
}

func buildOutboundCatchAllNetworkFiltersOnly(push *model.PushContext, node *model.Proxy) []*listener.Filter {
	var egressCluster string

	if util.IsAllowAnyOutbound(node) {
		// We need a passthrough filter to fill in the filter stack for orig_dst listener
		egressCluster = util.PassthroughCluster

		// no need to check for nil value as the previous if check has checked
		if node.SidecarScope.OutboundTrafficPolicy.EgressProxy != nil {
			// user has provided an explicit destination for all the unknown traffic.
			// build a cluster out of this destination
			egressCluster = istio_route.GetDestinationCluster(node.SidecarScope.OutboundTrafficPolicy.EgressProxy,
				nil, 0)
		}
	} else {
		egressCluster = util.BlackHoleCluster
	}

	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       egressCluster,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: egressCluster},
		IdleTimeout:      parseDuration(node.Metadata.IdleTimeout),
	}

	filterStack := buildMetricsNetworkFilters(push, node, istionetworking.ListenerClassSidecarOutbound, nil)
	accessLogBuilder.setTCPAccessLog(push, node, tcpProxy, istionetworking.ListenerClassSidecarOutbound, nil)
	filterStack = append(filterStack, &listener.Filter{
		Name:       wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(tcpProxy)},
	})

	return filterStack
}

func parseDuration(s string) *durationpb.Duration {
	if s == "" {
		return nil
	}
	t, err := time.ParseDuration(s)
	if err != nil {
		return nil
	}
	return durationpb.New(t)
}

// TODO: This code is still insufficient. Ideally we should be parsing all the virtual services
// with TLS blocks and build the appropriate filter chain matches and routes here. And then finally
// evaluate the left over unmatched TLS traffic using allow_any or registry_only.
// See https://github.com/istio/istio/issues/21170
func buildOutboundCatchAllNetworkFilterChains(node *model.Proxy, push *model.PushContext) []*listener.FilterChain {
	filterStack := buildOutboundCatchAllNetworkFiltersOnly(push, node)
	chains := make([]*listener.FilterChain, 0, 2)
	chains = append(chains, blackholeFilterChain(push, node), &listener.FilterChain{
		Name:    model.VirtualOutboundCatchAllTCPFilterChainName,
		Filters: filterStack,
	})
	return chains
}

func blackholeFilterChain(push *model.PushContext, node *model.Proxy) *listener.FilterChain {
	return &listener.FilterChain{
		Name: model.VirtualOutboundBlackholeFilterChainName,
		FilterChainMatch: &listener.FilterChainMatch{
			// We should not allow requests to the listen port directly. Requests must be
			// sent to some other original port and iptables redirected to 15001. This
			// ensures we do not passthrough back to the listen port.
			DestinationPort: &wrappers.UInt32Value{Value: uint32(push.Mesh.ProxyListenPort)},
		},
		Filters: append(
			buildMetricsNetworkFilters(push, node, istionetworking.ListenerClassSidecarOutbound, nil),
			&listener.Filter{
				Name: wellknown.TCPProxy,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(&tcp.TcpProxy{
					StatPrefix:       util.BlackHoleCluster,
					ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
				})},
			},
		),
	}
}

func (lb *ListenerBuilder) buildHTTPConnectionManager(httpOpts *httpListenerOpts) *hcm.HttpConnectionManager {
	if httpOpts.connectionManager == nil {
		httpOpts.connectionManager = &hcm.HttpConnectionManager{}
	}

	connectionManager := httpOpts.connectionManager
	if httpOpts.http3Only {
		connectionManager.CodecType = hcm.HttpConnectionManager_HTTP3
		connectionManager.Http3ProtocolOptions = &core.Http3ProtocolOptions{}
	} else {
		connectionManager.CodecType = hcm.HttpConnectionManager_AUTO
	}

	ph := lb.node.Metadata.ProxyConfigOrDefault(lb.push.Mesh.GetDefaultConfig()).GetProxyHeaders()

	// Preserve HTTP/1.x traffic header case
	if shouldPreserveHeaderCase(lb.node.Metadata, lb.push) {
		// This value only affects HTTP/1.x traffic
		connectionManager.HttpProtocolOptions = defaultPreserveCaseFormatterConfig
	}

	connectionManager.AccessLog = []*accesslog.AccessLog{}
	connectionManager.StatPrefix = httpOpts.statPrefix
	connectionManager.AppendXForwardedPort = ph.GetXForwardedPort().GetEnabled().GetValue()

	// Setup normalization
	connectionManager.PathWithEscapedSlashesAction = hcm.HttpConnectionManager_KEEP_UNCHANGED
	switch lb.push.Mesh.GetPathNormalization().GetNormalization() {
	case meshconfig.MeshConfig_ProxyPathNormalization_NONE:
		connectionManager.NormalizePath = proto.BoolFalse
	case meshconfig.MeshConfig_ProxyPathNormalization_BASE, meshconfig.MeshConfig_ProxyPathNormalization_DEFAULT:
		connectionManager.NormalizePath = proto.BoolTrue
	case meshconfig.MeshConfig_ProxyPathNormalization_MERGE_SLASHES:
		connectionManager.NormalizePath = proto.BoolTrue
		connectionManager.MergeSlashes = true
	case meshconfig.MeshConfig_ProxyPathNormalization_DECODE_AND_MERGE_SLASHES:
		connectionManager.NormalizePath = proto.BoolTrue
		connectionManager.MergeSlashes = true
		connectionManager.PathWithEscapedSlashesAction = hcm.HttpConnectionManager_UNESCAPE_AND_FORWARD
	}

	if httpOpts.useRemoteAddress {
		connectionManager.UseRemoteAddress = proto.BoolTrue
	} else {
		connectionManager.UseRemoteAddress = proto.BoolFalse
	}

	// Allow websocket upgrades
	websocketUpgrade := &hcm.HttpConnectionManager_UpgradeConfig{UpgradeType: "websocket"}
	connectionManager.UpgradeConfigs = []*hcm.HttpConnectionManager_UpgradeConfig{websocketUpgrade}

	if idleTimeout := parseDuration(lb.node.Metadata.IdleTimeout); idleTimeout != nil {
		connectionManager.CommonHttpProtocolOptions = &core.HttpProtocolOptions{
			IdleTimeout: idleTimeout,
		}
	}

	connectionManager.StreamIdleTimeout = durationpb.New(0 * time.Second)

	if httpOpts.rds != "" {
		rds := &hcm.HttpConnectionManager_Rds{
			Rds: &hcm.Rds{
				ConfigSource: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
					InitialFetchTimeout: durationpb.New(0),
					ResourceApiVersion:  core.ApiVersion_V3,
				},
				RouteConfigName: httpOpts.rds,
			},
		}
		connectionManager.RouteSpecifier = rds
	} else {
		connectionManager.RouteSpecifier = &hcm.HttpConnectionManager_RouteConfig{RouteConfig: httpOpts.routeConfig}
	}

	accessLogBuilder.setHTTPAccessLog(lb.push, lb.node, connectionManager, httpOpts.class, httpOpts.policySvc)

	reqIDExtensionCtx := configureTracing(lb.push, lb.node, connectionManager, httpOpts.class, httpOpts.policySvc)

	filters := []*hcm.HttpFilter{}
	if !httpOpts.isWaypoint {
		wasm := lb.push.WasmPluginsByListenerInfo(lb.node, model.WasmPluginListenerInfo{
			Port:  httpOpts.port,
			Class: httpOpts.class,
		}, model.WasmPluginTypeHTTP)

		// Metadata exchange filter needs to be added before any other HTTP filters are added. This is done to
		// ensure that mx filter comes before HTTP RBAC filter. This is related to https://github.com/istio/istio/issues/41066
		filters = appendMxFilter(httpOpts, filters)
		// TODO: how to deal with ext-authz? It will be in the ordering twice
		filters = append(filters, lb.authzCustomBuilder.BuildHTTP(httpOpts.class)...)
		filters = extension.PopAppendHTTP(filters, wasm, extensions.PluginPhase_AUTHN)
		filters = append(filters, lb.authnBuilder.BuildHTTP(httpOpts.class)...)
		filters = extension.PopAppendHTTP(filters, wasm, extensions.PluginPhase_AUTHZ)
		filters = append(filters, lb.authzBuilder.BuildHTTP(httpOpts.class)...)
		// TODO: these feel like the wrong place to insert, but this retains backwards compatibility with the original implementation
		filters = extension.PopAppendHTTP(filters, wasm, extensions.PluginPhase_STATS)
		filters = extension.PopAppendHTTP(filters, wasm, extensions.PluginPhase_UNSPECIFIED_PHASE)
		// Add ExtProc per listener only if the Gateway has any inferencePool attached to it
		if kubeGwName, ok := lb.node.Labels[label.IoK8sNetworkingGatewayGatewayName.Name]; ok {
			if lb.push.GatewayAPIController.HasInferencePool(types.NamespacedName{Name: kubeGwName, Namespace: lb.node.GetNamespace()}) {
				filters = append(filters, xdsfilters.InferencePoolExtProc)
			}
		}
	}

	if httpOpts.protocol == protocol.GRPCWeb {
		// TODO: because we share an HCM between many services, this check is broken; it will only work if the first
		// GRPCWeb is probably only used for Gateways though, which don't have this concern.
		filters = append(filters, xdsfilters.GrpcWeb)
	}

	filters = append(filters, xdsfilters.GrpcStats)

	// append ALPN HTTP filter in HTTP connection manager for outbound listener only.
	if features.ALPNFilter {
		if httpOpts.class != istionetworking.ListenerClassSidecarInbound {
			filters = append(filters, xdsfilters.Alpn)
		}
	}

	// TypedPerFilterConfig in route needs these filters.
	filters = append(filters, xdsfilters.Fault, xdsfilters.Cors)
	if !httpOpts.isWaypoint {
		filters = append(filters, lb.push.Telemetry.HTTPFilters(lb.node, httpOpts.class, nil)...)
	}
	// Add EmptySessionFilter so that it can be overridden at route level per service.
	if features.EnablePersistentSessionFilter.Load() && httpOpts.class != istionetworking.ListenerClassSidecarInbound {
		filters = append(filters, xdsfilters.EmptySessionFilter)
	}
	filters = append(filters, xdsfilters.BuildRouterFilter(xdsfilters.RouterFilterContext{
		SuppressDebugHeaders: httpOpts.suppressEnvoyDebugHeaders,
	}))

	connectionManager.HttpFilters = filters
	connectionManager.RequestIdExtension = requestidextension.BuildUUIDRequestIDExtension(reqIDExtensionCtx)

	// If UseRemoteAddress is set, we must set the internal address config to preserve internal headers.
	// As of Envoy 1.33, the default internalAddressConfig is set to an empty set. In previous versions
	// the default was all private IPs. To preserve internal headers when useRemoteAddress is set, we must
	// explicitly set MeshNetworks to configure Envoy's internal_address_config.
	// MeshNetwork configuration docs can be found here: https://istio.io/latest/docs/reference/config/istio.mesh.v1alpha1/#MeshNetworks
	if (features.EnableHCMInternalNetworks || httpOpts.useRemoteAddress) && lb.push.Networks != nil {
		connectionManager.InternalAddressConfig = util.MeshNetworksToEnvoyInternalAddressConfig(lb.push.Networks)
	}
	connectionManager.Proxy_100Continue = features.Enable100ContinueHeaders
	return connectionManager
}

func appendMxFilter(httpOpts *httpListenerOpts, filters []*hcm.HttpFilter) []*hcm.HttpFilter {
	if !features.MetadataExchange || httpOpts.hbone {
		return filters
	}
	if httpOpts.class == istionetworking.ListenerClassSidecarInbound {
		return append(filters, xdsfilters.SidecarInboundMetadataFilter)
	}

	if httpOpts.skipIstioMXHeaders {
		return append(filters, xdsfilters.SidecarOutboundMetadataFilterSkipHeaders)
	}
	return append(filters, xdsfilters.SidecarOutboundMetadataFilter)
}
