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

package v1alpha3

import (
	"sort"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	envoytype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	golangproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/extension"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/pkg/log"
)

var dummyServiceInstance = &model.ServiceInstance{
	Service:     &model.Service{},
	ServicePort: &model.Port{},
	Endpoint: &model.IstioEndpoint{
		EndpointPort: 15006,
	},
}

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

	envoyFilterWrapper *model.EnvoyFilterWrapper
}

// Setup the filter chain match so that the match should work under both
// - bind_to_port == false listener
// - virtual inbound listener
func amendFilterChainMatchFromInboundListener(chain *listener.FilterChain, l *listener.Listener) enabledInspector {
	if chain.FilterChainMatch == nil {
		chain.FilterChainMatch = &listener.FilterChainMatch{}
	}
	listenerAddress := l.Address
	if sockAddr := listenerAddress.GetSocketAddress(); sockAddr != nil {
		chain.FilterChainMatch.DestinationPort = &wrappers.UInt32Value{Value: sockAddr.GetPortValue()}
		chain.Name = l.Name
	}

	res := enabledInspector{}
	for _, filter := range l.ListenerFilters {
		if filter.Name == wellknown.TlsInspector {
			res.TLSInspector = true
		}
		if filter.Name == wellknown.HttpInspector {
			res.HTTPInspector = true
		}
	}
	return res
}

func isBindtoPort(l *listener.Listener) bool {
	v1 := l.GetBindToPort()
	if v1 == nil {
		// Default is true
		return true
	}
	return v1.GetValue()
}

// enabledInspector captures if for a given listener, listener filter inspectors are added
type enabledInspector struct {
	HTTPInspector bool
	TLSInspector  bool
}

// Accumulate the filter chains from per proxy service listeners
func reduceInboundListenerToFilterChains(listeners []*listener.Listener) ([]*listener.FilterChain, map[int]enabledInspector) {
	inspectorsMap := map[int]enabledInspector{}
	chains := make([]*listener.FilterChain, 0)
	for _, l := range listeners {
		// default bindToPort is true and these listener should be skipped
		if isBindtoPort(l) {
			// A listener on real port should not be intercepted by virtual inbound listener
			continue
		}
		for _, c := range l.FilterChains {
			chain := golangproto.Clone(c).(*listener.FilterChain)
			inspectors := amendFilterChainMatchFromInboundListener(chain, l)
			chains = append(chains, chain)
			// Aggregate the inspector options. If any listener on the port needs inspector, we should add it
			// Generally there is 1 listener per port anyways.
			port := int(l.Address.GetSocketAddress().GetPortValue())
			if port > 0 {
				prev := inspectorsMap[port]
				prev.HTTPInspector = prev.HTTPInspector || inspectors.HTTPInspector
				prev.TLSInspector = prev.TLSInspector || inspectors.TLSInspector
				inspectorsMap[port] = prev
			}
		}
	}
	return chains, inspectorsMap
}

func needsTLS(inspectors map[int]enabledInspector) bool {
	for _, i := range inspectors {
		if i.TLSInspector {
			return true
		}
	}
	return false
}

func mergeInspectors(a, b map[int]enabledInspector) map[int]enabledInspector {
	result := map[int]enabledInspector{}
	for p, i := range a {
		result[p] = i
	}
	for p, i := range b {
		result[p] = i
	}
	return result
}

func (lb *ListenerBuilder) aggregateVirtualInboundListener(passthroughInspectors map[int]enabledInspector) *ListenerBuilder {
	// Deprecated by envoyproxy. Replaced
	// 1. filter chains in this listener
	// 2. explicit original_dst listener filter
	// UseOriginalDst: proto.BoolTrue,
	lb.virtualInboundListener.UseOriginalDst = nil
	lb.virtualInboundListener.ListenerFilters = append(lb.virtualInboundListener.ListenerFilters,
		xdsfilters.OriginalDestination,
	)
	if lb.node.GetInterceptionMode() == model.InterceptionTproxy {
		lb.virtualInboundListener.ListenerFilters = append(lb.virtualInboundListener.ListenerFilters, xdsfilters.OriginalSrc)
	}
	// TODO: Trim the inboundListeners properly. Those that have been added to filter chains should
	// be removed while those that haven't been added need to remain in the inboundListeners list.
	filterChains, inspectors := reduceInboundListenerToFilterChains(lb.inboundListeners)
	sort.SliceStable(filterChains, func(i, j int) bool {
		return filterChains[i].Name < filterChains[j].Name
	})

	lb.virtualInboundListener.FilterChains = append(lb.virtualInboundListener.FilterChains, filterChains...)

	tlsInspectors := mergeInspectors(inspectors, passthroughInspectors)
	if needsTLS(tlsInspectors) {
		lb.virtualInboundListener.ListenerFilters = append(lb.virtualInboundListener.ListenerFilters, buildTLSInspector(tlsInspectors))
	}

	// Note: the HTTP inspector should be after TLS inspector.
	// If TLS inspector sets transport protocol to tls, the http inspector
	// won't inspect the packet.
	if features.EnableProtocolSniffingForInbound {
		lb.virtualInboundListener.ListenerFilters = append(lb.virtualInboundListener.ListenerFilters, buildHTTPInspector(inspectors))
	}

	timeout := lb.push.Mesh.GetProtocolDetectionTimeout()
	if features.InboundProtocolDetectionTimeoutSet {
		timeout = durationpb.New(features.InboundProtocolDetectionTimeout)
	}
	lb.virtualInboundListener.ListenerFiltersTimeout = timeout
	lb.virtualInboundListener.ContinueOnListenerFiltersTimeout = true

	// All listeners except bind_to_port=true listeners are now a part of virtual inbound and not needed
	// we can filter these ones out.
	bindToPortInbound := make([]*listener.Listener, 0, len(lb.inboundListeners))
	for _, i := range lb.inboundListeners {
		if isBindtoPort(i) {
			bindToPortInbound = append(bindToPortInbound, i)
		}
	}
	lb.inboundListeners = bindToPortInbound

	return lb
}

// buildTLSInspector creates a tls inspector filter. Based on the configured ports, this may be enabled
// for only some ports.
func buildTLSInspector(inspectors map[int]enabledInspector) *listener.ListenerFilter {
	defaultEnabled := inspectors[0].TLSInspector

	// We have a split path here based on if the passthrough inspector is enabled
	// If it is, then we need to explicitly opt ports out of the inspector
	// If it isn't, then we need to explicitly opt ports into the inspector
	if defaultEnabled {
		ports := make([]int, 0, len(inspectors))
		// Collect all ports where TLS inspector is disabled.
		for p, i := range inspectors {
			if p == 0 {
				continue
			}
			if !i.TLSInspector {
				ports = append(ports, p)
			}
		}
		// No need to filter, return the cached version enabled for all ports
		if len(ports) == 0 {
			return xdsfilters.TLSInspector
		}
		// Ensure consistent ordering as we are looping over a map
		sort.Ints(ports)
		filter := &listener.ListenerFilter{
			Name:           wellknown.TlsInspector,
			ConfigType:     xdsfilters.TLSInspector.ConfigType,
			FilterDisabled: listenerPredicateExcludePorts(ports),
		}
		return filter
	}
	ports := make([]int, 0, len(inspectors))
	// Collect all ports where TLS inspector is disabled.
	for p, i := range inspectors {
		if p == 0 {
			continue
		}
		if i.TLSInspector {
			ports = append(ports, p)
		}
	}
	// No need to filter, return the cached version enabled for all ports
	if len(ports) == 0 {
		return xdsfilters.TLSInspector
	}
	// Ensure consistent ordering as we are looping over a map
	sort.Ints(ports)
	filter := &listener.ListenerFilter{
		Name:           wellknown.TlsInspector,
		ConfigType:     xdsfilters.TLSInspector.ConfigType,
		FilterDisabled: listenerPredicateIncludePorts(ports),
	}
	return filter
}

// buildHTTPInspector creates an http inspector filter. Based on the configured ports, this may be enabled
// for only some ports.
func buildHTTPInspector(inspectors map[int]enabledInspector) *listener.ListenerFilter {
	ports := make([]int, 0, len(inspectors))
	// Collect all ports where HTTP inspector is disabled.
	for p, i := range inspectors {
		if !i.HTTPInspector {
			ports = append(ports, p)
		}
	}
	// No need to filter, return the cached version enabled for all ports
	if len(ports) == 0 {
		return xdsfilters.HTTPInspector
	}
	// Ensure consistent ordering as we are looping over a map
	sort.Ints(ports)
	filter := &listener.ListenerFilter{
		Name:           wellknown.HttpInspector,
		ConfigType:     xdsfilters.HTTPInspector.ConfigType,
		FilterDisabled: listenerPredicateExcludePorts(ports),
	}
	return filter
}

// listenerPredicateExcludePorts returns a listener filter predicate that will
// match everything except the passed in ports. This is useful, for example, to
// enable protocol sniffing on every port except port X and Y, because X and Y
// are explicitly declared.
func listenerPredicateExcludePorts(ports []int) *listener.ListenerFilterChainMatchPredicate {
	ranges := []*listener.ListenerFilterChainMatchPredicate{}
	for _, p := range ports {
		ranges = append(ranges, &listener.ListenerFilterChainMatchPredicate{Rule: &listener.ListenerFilterChainMatchPredicate_DestinationPortRange{
			// Range is [start, end)
			DestinationPortRange: &envoytype.Int32Range{
				Start: int32(p),
				End:   int32(p + 1),
			},
		}})
	}
	if len(ranges) > 1 {
		return &listener.ListenerFilterChainMatchPredicate{Rule: &listener.ListenerFilterChainMatchPredicate_OrMatch{
			OrMatch: &listener.ListenerFilterChainMatchPredicate_MatchSet{
				Rules: ranges,
			},
		}}
	}
	return &listener.ListenerFilterChainMatchPredicate{Rule: ranges[0].GetRule()}
}

func listenerPredicateIncludePorts(ports []int) *listener.ListenerFilterChainMatchPredicate {
	rule := listenerPredicateExcludePorts(ports)
	return &listener.ListenerFilterChainMatchPredicate{Rule: &listener.ListenerFilterChainMatchPredicate_NotMatch{
		NotMatch: rule,
	}}
}

func NewListenerBuilder(node *model.Proxy, push *model.PushContext) *ListenerBuilder {
	builder := &ListenerBuilder{
		node: node,
		push: push,
	}
	return builder
}

func (lb *ListenerBuilder) buildSidecarInboundListeners(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	lb.inboundListeners = configgen.buildSidecarInboundListeners(lb.node, lb.push)
	return lb
}

func (lb *ListenerBuilder) buildSidecarOutboundListeners(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	lb.outboundListeners = configgen.buildSidecarOutboundListeners(lb.node, lb.push)
	return lb
}

func (lb *ListenerBuilder) buildHTTPProxyListener(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	httpProxy := configgen.buildHTTPProxy(lb.node, lb.push)
	if httpProxy == nil {
		return lb
	}
	removeListenerFilterTimeout([]*listener.Listener{httpProxy})
	lb.patchOneListener(httpProxy, networking.EnvoyFilter_SIDECAR_OUTBOUND)
	lb.httpProxyListener = httpProxy
	return lb
}

func (lb *ListenerBuilder) buildVirtualOutboundListener(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	if lb.node.GetInterceptionMode() == model.InterceptionNone {
		// virtual listener is not necessary since workload is not using IPtables for traffic interception
		return lb
	}

	var isTransparentProxy *wrappers.BoolValue
	if lb.node.GetInterceptionMode() == model.InterceptionTproxy {
		isTransparentProxy = proto.BoolTrue
	}

	filterChains := buildOutboundCatchAllNetworkFilterChains(configgen, lb.node, lb.push)

	actualWildcard, _ := getActualWildcardAndLocalHost(lb.node)

	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	ipTablesListener := &listener.Listener{
		Name:             model.VirtualOutboundListenerName,
		Address:          util.BuildAddress(actualWildcard, uint32(lb.push.Mesh.ProxyListenPort)),
		Transparent:      isTransparentProxy,
		UseOriginalDst:   proto.BoolTrue,
		FilterChains:     filterChains,
		TrafficDirection: core.TrafficDirection_OUTBOUND,
	}
	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, ipTablesListener)
	lb.virtualOutboundListener = ipTablesListener
	return lb
}

// TProxy uses only the virtual outbound listener on 15001 for both directions
// but we still ship the no-op virtual inbound listener, so that the code flow is same across REDIRECT and TPROXY.
func (lb *ListenerBuilder) buildVirtualInboundListener(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	if lb.node.GetInterceptionMode() == model.InterceptionNone {
		// virtual listener is not necessary since workload is not using IPtables for traffic interception
		return lb
	}

	var isTransparentProxy *wrappers.BoolValue
	if lb.node.GetInterceptionMode() == model.InterceptionTproxy {
		isTransparentProxy = proto.BoolTrue
	}

	actualWildcard, _ := getActualWildcardAndLocalHost(lb.node)
	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	filterChains, passthroughInspector, usesQUIC := buildInboundCatchAllFilterChains(configgen, lb.node, lb.push)

	// exact balance used in Envoy is only supported over TCP connections
	var connectionBalance *listener.Listener_ConnectionBalanceConfig
	if !usesQUIC && bool(lb.node.Metadata.InboundListenerExactBalance) {
		connectionBalance = &listener.Listener_ConnectionBalanceConfig{
			BalanceType: &listener.Listener_ConnectionBalanceConfig_ExactBalance_{
				ExactBalance: &listener.Listener_ConnectionBalanceConfig_ExactBalance{},
			},
		}
	}
	lb.virtualInboundListener = &listener.Listener{
		Name:                    model.VirtualInboundListenerName,
		Address:                 util.BuildAddress(actualWildcard, ProxyInboundListenPort),
		Transparent:             isTransparentProxy,
		UseOriginalDst:          proto.BoolTrue,
		TrafficDirection:        core.TrafficDirection_INBOUND,
		FilterChains:            filterChains,
		ConnectionBalanceConfig: connectionBalance,
	}
	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, lb.virtualInboundListener)
	lb.aggregateVirtualInboundListener(passthroughInspector)

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
	lb.inboundListeners = envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_SIDECAR_INBOUND, lb.envoyFilterWrapper, lb.inboundListeners, false)
	lb.outboundListeners = envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_SIDECAR_OUTBOUND, lb.envoyFilterWrapper, lb.outboundListeners, false)
}

func (lb *ListenerBuilder) getListeners() []*listener.Listener {
	if lb.node.Type == model.SidecarProxy {
		nInbound, nOutbound := len(lb.inboundListeners), len(lb.outboundListeners)
		nHTTPProxy, nVirtual, nVirtualInbound := 0, 0, 0
		if lb.httpProxyListener != nil {
			nHTTPProxy = 1
		}
		if lb.virtualOutboundListener != nil {
			nVirtual = 1
		}
		if lb.virtualInboundListener != nil {
			nVirtualInbound = 1
		}

		nListener := nInbound + nOutbound + nHTTPProxy + nVirtual + nVirtualInbound

		listeners := make([]*listener.Listener, 0, nListener)
		listeners = append(listeners, lb.inboundListeners...)
		listeners = append(listeners, lb.outboundListeners...)
		if lb.httpProxyListener != nil {
			listeners = append(listeners, lb.httpProxyListener)
		}
		if lb.virtualOutboundListener != nil {
			listeners = append(listeners, lb.virtualOutboundListener)
		}
		if lb.virtualInboundListener != nil {
			listeners = append(listeners, lb.virtualInboundListener)
		}

		log.Debugf("Build %d listeners for node %s including %d outbound, %d http proxy, "+
			"%d virtual outbound and %d virtual inbound listeners",
			nListener,
			lb.node.ID,
			nOutbound,
			nHTTPProxy,
			nVirtual,
			nVirtualInbound)
		return listeners
	}

	return lb.gatewayListeners
}

func getMtlsSettings(configgen *ConfigGeneratorImpl, in *plugin.InputParams, passthrough bool) []plugin.MTLSSettings {
	for _, p := range configgen.Plugins {
		cfg := p.InboundMTLSConfiguration(in, passthrough)
		if cfg != nil {
			return cfg
		}
	}
	// If no plugin configures mtls, set it to disabled
	if passthrough {
		return []plugin.MTLSSettings{{Mode: model.MTLSDisable}}
	}
	return []plugin.MTLSSettings{{
		Port: in.ServiceInstance.Endpoint.EndpointPort,
		Mode: model.MTLSDisable,
	}}
}

func buildInboundCatchAllFilterChains(configgen *ConfigGeneratorImpl,
	node *model.Proxy, push *model.PushContext) ([]*listener.FilterChain, map[int]enabledInspector, bool) {
	usesQUIC := false

	// ipv4 and ipv6 feature detect
	ipVersions := make([]string, 0, 2)
	if node.SupportsIPv4() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv4)
	}
	if node.SupportsIPv6() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv6)
	}

	var filters []*listener.Filter
	filters = append(filters, buildMetadataExchangeNetworkFilters(istionetworking.ListenerClassSidecarInbound)...)
	filters = append(filters, buildMetricsNetworkFilters(push, node, istionetworking.ListenerClassSidecarInbound)...)
	filters = append(filters, &listener.Filter{
		Name: wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
			StatPrefix:       util.BlackHoleCluster,
			ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
		})},
	})
	// Setup enough slots for common max size (permissive mode is 5 filter chains). This is not
	// exact, just best effort optimization
	filterChains := make([]*listener.FilterChain, 0, 1+5*len(ipVersions))
	filterChains = append(filterChains, &listener.FilterChain{
		Name: model.VirtualInboundBlackholeFilterChainName,
		FilterChainMatch: &listener.FilterChainMatch{
			DestinationPort: &wrappers.UInt32Value{Value: ProxyInboundListenPort},
		},
		Filters: filters,
	})

	inspectors := map[int]enabledInspector{}
	for _, clusterName := range ipVersions {
		matchingIP := ""
		if clusterName == util.InboundPassthroughClusterIpv4 {
			matchingIP = "0.0.0.0/0"
		} else if clusterName == util.InboundPassthroughClusterIpv6 {
			matchingIP = "::0/0"
		}

		in := &plugin.InputParams{
			Node:            node,
			ServiceInstance: dummyServiceInstance,
			Push:            push,
		}
		listenerOpts := buildListenerOpts{
			push:  push,
			proxy: node,
			bind:  matchingIP,
			port: &model.Port{
				Name:     "virtualInbound",
				Port:     15006,
				Protocol: protocol.HTTP,
			},
			protocol: istionetworking.ListenerProtocolAuto,
		}
		// Call plugins to get mtls policies.
		fcOpts := configgen.buildInboundFilterchains(in, listenerOpts, matchingIP, clusterName, true)
		for _, opt := range fcOpts {
			filterChain := &listener.FilterChain{
				FilterChainMatch: opt.match,
				Name:             opt.filterChainName,
			}
			if opt.httpOpts != nil {
				opt.httpOpts.statPrefix = clusterName
				if opt.httpOpts.http3Only {
					usesQUIC = true
				}
				connectionManager := buildHTTPConnectionManager(listenerOpts, opt.httpOpts, opt.filterChain.HTTP)
				filter := &listener.Filter{
					Name:       wellknown.HTTPConnectionManager,
					ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(connectionManager)},
				}
				filterChain.Filters = append(opt.filterChain.TCP, filter)
			} else {
				filterChain.Filters = append(opt.filterChain.TCP, opt.networkFilters...)
			}
			port := int(opt.match.DestinationPort.GetValue())
			inspector := inspectors[port]
			if opt.tlsContext != nil {
				inspector.TLSInspector = true
				// Update transport socket from the TLS context configured by the plugin.
				filterChain.TransportSocket = &core.TransportSocket{
					Name:       util.EnvoyTLSSocketName,
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(opt.tlsContext)},
				}
			}
			inspectors[port] = inspector
			filterChains = append(filterChains, filterChain)
		}
	}

	return filterChains, inspectors, usesQUIC
}

func (configgen *ConfigGeneratorImpl) buildInboundFilterchains(in *plugin.InputParams, listenerOpts buildListenerOpts,
	matchingIP string, clusterName string, passthrough bool) []*filterChainOpts {
	newOpts := []*fcOpts{}

	// unless the PeerAuthentication is set to "DISABLE",
	// TLS settings won't take effect
	hasMTLs := false

	mtlsConfigs := getMtlsSettings(configgen, in, passthrough)
	for _, mtlsConfig := range mtlsConfigs {
		hasMTLs = hasMTLs || mtlsConfig.Mode != model.MTLSDisable
		for _, match := range getFilterChainMatchOptions(mtlsConfig, listenerOpts.protocol) {
			opt := fcOpts{matchOpts: match}.populateFilterChain(mtlsConfig, mtlsConfig.Port, matchingIP)
			newOpts = append(newOpts, &opt)
		}
	}

	if listenerOpts.tlsSettings != nil && !hasMTLs {
		newOpts = []*fcOpts{}
		opt := fcOpts{matchOpts: FilterChainMatchOptions{IsCustomTLS: true}}
		opt.fc.FilterChainMatch = &listener.FilterChainMatch{
			TransportProtocol: xdsfilters.TLSTransportProtocol,
			DestinationPort:   &wrappers.UInt32Value{Value: uint32(listenerOpts.port.Port)},
		}
		opt.fc.ListenerProtocol = listenerOpts.protocol
		listenerOpts.tlsSettings.CipherSuites = filteredSidecarCipherSuites(listenerOpts.tlsSettings.CipherSuites)
		opt.fc.TLSContext = configgen.BuildListenerTLSContext(listenerOpts.tlsSettings, in.Node, istionetworking.TransportProtocolTCP)
		newOpts = append(newOpts, &opt)
	}

	// Run our filter chains through the plugin
	fcs := make([]istionetworking.FilterChain, 0, len(newOpts))
	for _, o := range newOpts {
		fcs = append(fcs, o.fc)
	}
	mutable := &istionetworking.MutableObjects{
		FilterChains: fcs,
	}
	for _, p := range configgen.Plugins {
		if passthrough {
			if err := p.OnInboundPassthrough(in, mutable); err != nil {
				log.Errorf("Build inbound passthrough filter chains error: %v", err)
			}
		} else {
			if err := p.OnInboundListener(in, mutable); err != nil {
				log.Errorf("Build inbound filter chains error: %v", err)
			}
		}
	}
	extension.AddWasmPluginsToMutableObjects(mutable, in.Push.WasmPlugins(in.Node))
	// Merge the results back into our struct
	for i, fc := range mutable.FilterChains {
		newOpts[i].fc = fc
	}

	fcOpts := listenerOpts.filterChainOpts
	for _, opt := range newOpts {
		fcOpt := &filterChainOpts{
			match: opt.fc.FilterChainMatch,
		}
		if len(opt.matchOpts.SNIHosts) > 0 {
			fcOpt.sniHosts = opt.matchOpts.SNIHosts
		}
		if (opt.matchOpts.MTLS || opt.matchOpts.IsCustomTLS) && opt.fc.TLSContext != nil {
			// Update transport socket from the TLS context configured by the plugin.
			fcOpt.tlsContext = opt.fc.TLSContext
		}
		fcOpt.filterChain = opt.fc
		switch opt.fc.ListenerProtocol {
		case istionetworking.ListenerProtocolHTTP:
			fcOpt.httpOpts = configgen.buildSidecarInboundHTTPListenerOptsForPortOrUDS(in.Node, in, clusterName)
			fcOpt.filterChain.TCP = append(
				buildMetadataExchangeNetworkFilters(istionetworking.ListenerClassSidecarInbound),
				fcOpt.filterChain.TCP...)
		case istionetworking.ListenerProtocolTCP:
			fcOpt.networkFilters = buildInboundNetworkFilters(in.Push, in.Node, in.ServiceInstance, clusterName)
		case istionetworking.ListenerProtocolAuto:
			fcOpt.httpOpts = configgen.buildSidecarInboundHTTPListenerOptsForPortOrUDS(in.Node, in, clusterName)
			fcOpt.networkFilters = buildInboundNetworkFilters(in.Push, in.Node, in.ServiceInstance, clusterName)
		}
		fcOpt.filterChainName = model.VirtualInboundListenerName
		if opt.fc.ListenerProtocol == istionetworking.ListenerProtocolHTTP {
			fcOpt.filterChainName = model.VirtualInboundCatchAllHTTPFilterChainName
		}
		fcOpts = append(fcOpts, fcOpt)
	}
	return fcOpts
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
	idleTimeoutDuration, err := time.ParseDuration(node.Metadata.IdleTimeout)
	if err != nil {
		idleTimeoutDuration = 0
	}

	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       egressCluster,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: egressCluster},
		IdleTimeout:      durationpb.New(idleTimeoutDuration),
	}
	filterStack := buildMetricsNetworkFilters(push, node, istionetworking.ListenerClassSidecarOutbound)
	accessLogBuilder.setTCPAccessLog(push, node, tcpProxy)
	filterStack = append(filterStack, &listener.Filter{
		Name:       wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(tcpProxy)},
	})

	return filterStack
}

// TODO: This code is still insufficient. Ideally we should be parsing all the virtual services
// with TLS blocks and build the appropriate filter chain matches and routes here. And then finally
// evaluate the left over unmatched TLS traffic using allow_any or registry_only.
// See https://github.com/istio/istio/issues/21170
func buildOutboundCatchAllNetworkFilterChains(_ *ConfigGeneratorImpl,
	node *model.Proxy, push *model.PushContext) []*listener.FilterChain {
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
			buildMetricsNetworkFilters(push, node, istionetworking.ListenerClassSidecarOutbound),
			&listener.Filter{
				Name: wellknown.TCPProxy,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
					StatPrefix:       util.BlackHoleCluster,
					ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
				})},
			},
		),
	}
}
