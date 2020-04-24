// Copyright 2019 Istio Authors. All Rights Reserved.
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
	"istio.io/istio/pilot/pkg/serviceregistry"
	"sort"
	"strconv"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
)

var (
	dummyServiceInstance = &model.ServiceInstance{
		Service:     &model.Service{},
		ServicePort: &model.Port{},
		Endpoint: &model.IstioEndpoint{
			EndpointPort: 15006,
		},
	}
)

// A stateful listener builder
// Support the below intentions
// 1. Use separate inbound capture listener(:15006) and outbound capture listener(:15001)
// 2. The above listeners use bind_to_port sub listeners or filter chains.
type ListenerBuilder struct {
	node              *model.Proxy
	push              *model.PushContext
	gatewayListeners  []*xdsapi.Listener
	inboundListeners  []*xdsapi.Listener
	outboundListeners []*xdsapi.Listener
	// HttpProxyListener is a specialize outbound listener. See MeshConfig.proxyHttpPort
	httpProxyListener       *xdsapi.Listener
	virtualOutboundListener *xdsapi.Listener
	virtualInboundListener  *xdsapi.Listener
	voutBuilder             VirtualOutboundListenerInterface
}

type VirtualOutboundListenerInterface interface {
	// Insert an outbound service into the virtual outbound listener.
	// This function should resolve the conflict if a service is inserted repeatedly.
	insertService(configgen *ConfigGeneratorImpl, node *model.Proxy, listenerOpts buildListenerOpts,
		pluginParams *plugin.InputParams,
		virtualServices []model.Config, actualWildcard string)
	// Invoked when build the catch rest egress listener
	prepareDefaultEgressListener()

	getAllServiceListeners(configgen *ConfigGeneratorImpl, node *model.Proxy, push *model.PushContext) []*xdsapi.Listener

	getAllOutboundListener(configgen *ConfigGeneratorImpl, node *model.Proxy, push *model.PushContext, outboundListener []*xdsapi.Listener) ([]*xdsapi.Listener, *xdsapi.Listener)
}

type PerListenerShimBuilder struct {
	tcpListeners, httpListeners []*xdsapi.Listener
	// For conflict resolution
	listenerMap map[string]*outboundListenerEntry
}

func (shim *PerListenerShimBuilder) insertService(configgen *ConfigGeneratorImpl, node *model.Proxy, listenerOpts buildListenerOpts,
	pluginParams *plugin.InputParams,
	virtualServices []model.Config, actualWildcard string) {
	configgen.buildSidecarOutboundListenerForPortOrUDS(node, listenerOpts, pluginParams, shim.listenerMap, virtualServices, actualWildcard)
}

func (shim *PerListenerShimBuilder) prepareDefaultEgressListener() {
	for _, e := range shim.listenerMap {
		e.locked = true
	}
}

func (shim *PerListenerShimBuilder) getAllServiceListeners(configgen *ConfigGeneratorImpl, node *model.Proxy, push *model.PushContext) []*xdsapi.Listener{
	// Now validate all the listeners. Collate the tcp listeners first and then the HTTP listeners
	// TODO: This is going to be bad for caching as the order of listeners in tcpListeners or httpListeners is not
	// guaranteed.
	invalid := 0.0
	for name, l := range shim.listenerMap {
		if err := l.listener.Validate(); err != nil {
			log.Warnf("buildSidecarOutboundListeners: error validating listener %s (type %v): %v", name, l.servicePort.Protocol, err)
			invalid++
			invalidOutboundListeners.Record(invalid)
			continue
		}
		if l.servicePort.Protocol.IsTCP() {
			shim.tcpListeners = append(shim.tcpListeners, l.listener)
		} else {
			shim.httpListeners = append(shim.httpListeners, l.listener)
		}
	}
	shim.tcpListeners = append(shim.tcpListeners, shim.httpListeners...)
	// Build pass through filter chains now that all the non-passthrough filter chains are ready.
	for _, listener := range shim.tcpListeners {
		configgen.appendListenerFallthroughRouteForCompleteListener(listener, node, push)
	}
	removeListenerFilterTimeout(shim.tcpListeners)
	return shim.tcpListeners
}

func (shim *PerListenerShimBuilder) getAllOutboundListener(configgen *ConfigGeneratorImpl,
	node *model.Proxy, push *model.PushContext, perServiceOutboundListener []*xdsapi.Listener) ([]*xdsapi.Listener, *xdsapi.Listener) {

	var isTransparentProxy *wrappers.BoolValue
	if node.GetInterceptionMode() == model.InterceptionTproxy {
		isTransparentProxy = proto.BoolTrue
	}

	filterChains := buildOutboundCatchAllNetworkFilterChains(configgen, node, push)

	actualWildcard, _ := getActualWildcardAndLocalHost(node)

	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	ipTablesListener := &xdsapi.Listener{
		Name:             VirtualOutboundListenerName,
		Address:          util.BuildAddress(actualWildcard, uint32(push.Mesh.ProxyListenPort)),
		Transparent:      isTransparentProxy,
		UseOriginalDst:   proto.BoolTrue,
		FilterChains:     filterChains,
		TrafficDirection: core.TrafficDirection_OUTBOUND,
	}
	configgen.onVirtualOutboundListener(node, push, ipTablesListener)
	return perServiceOutboundListener, ipTablesListener
}

// Setup the filter chain match so that the match should work under both
// - bind_to_port == false listener
// - virtual inbound listener
func amendFilterChainMatchFromInboundListener(chain *listener.FilterChain, l *xdsapi.Listener, needTLS bool) (*listener.FilterChain, bool) {
	if chain.FilterChainMatch == nil {
		chain.FilterChainMatch = &listener.FilterChainMatch{}
	}
	listenerAddress := l.Address
	if sockAddr := listenerAddress.GetSocketAddress(); sockAddr != nil {
		chain.FilterChainMatch.DestinationPort = &wrappers.UInt32Value{Value: sockAddr.GetPortValue()}
		if cidr := util.ConvertAddressToCidr(sockAddr.GetAddress()); cidr != nil {
			if chain.FilterChainMatch.PrefixRanges != nil && len(chain.FilterChainMatch.PrefixRanges) != 1 {
				log.Debugf("Intercepted inbound listener %s have neither 0 or 1 prefix ranges. Actual:  %d",
					l.Name, len(chain.FilterChainMatch.PrefixRanges))
			}
			chain.FilterChainMatch.PrefixRanges = []*core.CidrRange{util.ConvertAddressToCidr(sockAddr.GetAddress())}
		}
		chain.Name = l.Name
	}
	for _, filter := range l.ListenerFilters {
		if needTLS = needTLS || filter.Name == xdsutil.TlsInspector; needTLS {
			break
		}
	}
	return chain, needTLS
}

func isBindtoPort(l *xdsapi.Listener) bool {
	v1 := l.GetDeprecatedV1()
	if v1 == nil {
		// Default is true
		return true
	}
	bp := v1.BindToPort
	if bp == nil {
		// Default is true
		return true
	}
	return bp.Value
}

// Accumulate the filter chains from per proxy service listeners
func reduceInboundListenerToFilterChains(listeners []*xdsapi.Listener) ([]*listener.FilterChain, bool) {
	needTLS := false
	chains := make([]*listener.FilterChain, 0)
	for _, l := range listeners {
		// default bindToPort is true and these listener should be skipped
		if isBindtoPort(l) {
			// A listener on real port should not be intercepted by virtual inbound listener
			continue
		}
		for _, c := range l.FilterChains {
			newChain, needTLSLocal := amendFilterChainMatchFromInboundListener(gogoproto.Clone(c).(*listener.FilterChain), l, needTLS)
			chains = append(chains, newChain)
			needTLS = needTLS || needTLSLocal
		}
	}
	return chains, needTLS
}

func (lb *ListenerBuilder) aggregateVirtualInboundListener(needTLSForPassThroughFilterChain bool) *ListenerBuilder {
	// Deprecated by envoyproxy. Replaced
	// 1. filter chains in this listener
	// 2. explicit original_dst listener filter
	// UseOriginalDst: proto.BoolTrue,
	lb.virtualInboundListener.UseOriginalDst = nil
	lb.virtualInboundListener.ListenerFilters = append(lb.virtualInboundListener.ListenerFilters,
		originalDestinationFilter,
	)
	// TODO: Trim the inboundListeners properly. Those that have been added to filter chains should
	// be removed while those that haven't been added need to remain in the inboundListeners list.
	filterChains, needTLS := reduceInboundListenerToFilterChains(lb.inboundListeners)
	sort.SliceStable(filterChains, func(i, j int) bool {
		return filterChains[i].Name < filterChains[j].Name
	})

	lb.virtualInboundListener.FilterChains =
		append(lb.virtualInboundListener.FilterChains, filterChains...)

	if needTLS || needTLSForPassThroughFilterChain {
		lb.virtualInboundListener.ListenerFilters =
			append(lb.virtualInboundListener.ListenerFilters, tlsInspectorFilter)
	}

	// Note: the HTTP inspector should be after TLS inspector.
	// If TLS inspector sets transport protocol to tls, the http inspector
	// won't inspect the packet.
	if features.EnableProtocolSniffingForInbound {
		lb.virtualInboundListener.ListenerFilters =
			append(lb.virtualInboundListener.ListenerFilters, httpInspectorFilter)
	}

	timeout := features.InboundProtocolDetectionTimeout
	lb.virtualInboundListener.ListenerFiltersTimeout = ptypes.DurationProto(timeout)
	lb.virtualInboundListener.ContinueOnListenerFiltersTimeout = true

	// All listeners except bind_to_port=true listeners are now a part of virtual inbound and not needed
	// we can filter these ones out.
	bindToPortInbound := make([]*xdsapi.Listener, 0, len(lb.inboundListeners))
	for _, i := range lb.inboundListeners {
		if isBindtoPort(i) {
			bindToPortInbound = append(bindToPortInbound, i)
		}
	}
	lb.inboundListeners = bindToPortInbound

	return lb
}

func NewListenerBuilder(node *model.Proxy, push *model.PushContext) *ListenerBuilder {
	builder := &ListenerBuilder{
		node: node,
		push: push,
		// TODO(lambdai): impl,verify and migrate to PerPortListenerBuilder
		voutBuilder: &PerListenerShimBuilder{
			listenerMap: make(map[string]*outboundListenerEntry),
		},
	}
	return builder
}

func (lb *ListenerBuilder) buildSidecarInboundListeners(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	lb.inboundListeners = configgen.buildSidecarInboundListeners(lb.node, lb.push)
	return lb
}

func (lb *ListenerBuilder) buildSidecarOutboundListeners(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	vout := lb.voutBuilder

	node := lb.node
	push := lb.push
	noneMode := node.GetInterceptionMode() == model.InterceptionNone
	actualWildcard, actualLocalHostAddress := getActualWildcardAndLocalHost(node)

	//var tcpListeners, httpListeners []*xdsapi.Listener
	// For conflict resolution
	//listenerMap := make(map[string]*outboundListenerEntry)

	// The sidecarConfig if provided could filter the list of
	// services/virtual services that we need to process. It could also
	// define one or more listeners with specific ports. Once we generate
	// listeners for these user specified ports, we will auto generate
	// configs for other ports if and only if the sidecarConfig has an
	// egressListener on wildcard port.
	//
	// Validation will ensure that we have utmost one wildcard egress listener
	// occurring in the end

	// Add listeners based on the config in the sidecar.EgressListeners if
	// no Sidecar CRD is provided for this config namespace,
	// push.SidecarScope will generate a default catch all egress listener.
	for _, egressListener := range node.SidecarScope.EgressListeners {

		services := egressListener.Services()
		virtualServices := egressListener.VirtualServices()

		// determine the bindToPort setting for listeners
		bindToPort := false
		if noneMode {
			// do not care what the listener's capture mode setting is. The proxy does not use iptables
			bindToPort = true
		} else if egressListener.IstioListener != nil &&
			// proxy uses iptables redirect or tproxy. IF mode is not set
			// for older proxies, it defaults to iptables redirect.  If the
			// listener's capture mode specifies NONE, then the proxy wants
			// this listener alone to be on a physical port. If the
			// listener's capture mode is default, then its same as
			// iptables i.e. bindToPort is false.
			egressListener.IstioListener.CaptureMode == networking.CaptureMode_NONE {
			bindToPort = true
		}

		if egressListener.IstioListener != nil &&
			egressListener.IstioListener.Port != nil {
			// We have a non catch all listener on some user specified port
			// The user specified port may or may not match a service port.
			// If it does not match any service port and the service has only
			// one port, then we pick a default service port. If service has
			// multiple ports, we expect the user to provide a virtualService
			// that will route to a proper Service.

			listenPort := &model.Port{
				Port:     int(egressListener.IstioListener.Port.Number),
				Protocol: protocol.Parse(egressListener.IstioListener.Port.Protocol),
				Name:     egressListener.IstioListener.Port.Name,
			}

			// If capture mode is NONE i.e., bindToPort is true, and
			// Bind IP + Port is specified, we will bind to the specified IP and Port.
			// This specified IP is ideally expected to be a loopback IP.
			//
			// If capture mode is NONE i.e., bindToPort is true, and
			// only Port is specified, we will bind to the default loopback IP
			// 127.0.0.1 and the specified Port.
			//
			// If capture mode is NONE, i.e., bindToPort is true, and
			// only Bind IP is specified, we will bind to the specified IP
			// for each port as defined in the service registry.
			//
			// If captureMode is not NONE, i.e., bindToPort is false, then
			// we will bind to user specified IP (if any) or to the VIPs of services in
			// this egress listener.
			bind := egressListener.IstioListener.Bind
			if bindToPort && bind == "" {
				bind = actualLocalHostAddress
			} else if len(bind) == 0 {
				bind = actualWildcard
			}

			// Build ListenerOpts and PluginParams once and reuse across all Services to avoid unnecessary allocations.
			listenerOpts := buildListenerOpts{
				push:       push,
				proxy:      node,
				bind:       bind,
				port:       listenPort.Port,
				bindToPort: bindToPort,
			}

			// The listener protocol is determined by the protocol of egress listener port.
			pluginParams := &plugin.InputParams{
				ListenerProtocol: istionetworking.ModelProtocolToListenerProtocol(node, listenPort.Protocol,
					core.TrafficDirection_OUTBOUND),
				ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				Node:             node,
				Push:             push,
				Bind:             bind,
				Port:             listenPort,
			}

			for _, service := range services {
				// Set service specific attributes here.
				pluginParams.Service = service
				vout.insertService(configgen, node, listenerOpts, pluginParams,
					virtualServices, actualWildcard)
			}
		} else {
			// This is a catch all egress listener with no port. This
			// should be the last egress listener in the sidecar
			// Scope. Construct a listener for each service and service
			// port, if and only if this port was not specified in any of
			// the preceding listeners from the sidecarScope. This allows
			// users to specify a trimmed set of services for one or more
			// listeners and then add a catch all egress listener for all
			// other ports. Doing so allows people to restrict the set of
			// services exposed on one or more listeners, and avoid hard
			// port conflicts like tcp taking over http or http taking over
			// tcp, or simply specify that of all the listeners that Istio
			// generates, the user would like to have only specific sets of
			// services exposed on a particular listener.
			//
			// To ensure that we do not add anything to listeners we have
			// already generated, run through the outboundListenerEntry map and set
			// the locked bit to true.
			// buildSidecarOutboundListenerForPortOrUDS will not add/merge
			// any HTTP/TCP listener if there is already a outboundListenerEntry
			// with locked bit set to true

			vout.prepareDefaultEgressListener()

			bind := ""
			if egressListener.IstioListener != nil && egressListener.IstioListener.Bind != "" {
				bind = egressListener.IstioListener.Bind
			}
			if bindToPort && bind == "" {
				bind = actualLocalHostAddress
			}

			// Build ListenerOpts and PluginParams once and reuse across all Services to avoid unnecessary allocations.
			listenerOpts := buildListenerOpts{
				push:       push,
				proxy:      node,
				bindToPort: bindToPort,
			}

			pluginParams := &plugin.InputParams{
				ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				Node:             node,
				Push:             push,
				Bind:             bind,
			}

			for _, service := range services {
				for _, servicePort := range service.Ports {
					// bind might have been modified by below code, so reset it for every Service.
					listenerOpts.bind = bind
					// port depends on servicePort.
					listenerOpts.port = servicePort.Port

					// Set service specific attributes here.
					pluginParams.Port = servicePort
					pluginParams.Service = service
					// The listener protocol is determined by the protocol of service port.
					pluginParams.ListenerProtocol = istionetworking.ModelProtocolToListenerProtocol(node, servicePort.Protocol,
						core.TrafficDirection_OUTBOUND)

					// Support Kubernetes statefulsets/headless services with TCP ports only.
					// Instead of generating a single 0.0.0.0:Port listener, generate a listener
					// for each instance. HTTP services can happily reside on 0.0.0.0:PORT and use the
					// wildcard route match to get to the appropriate pod through original dst clusters.
					if features.EnableHeadlessService && bind == "" && service.Resolution == model.Passthrough &&
						service.Attributes.ServiceRegistry == string(serviceregistry.Kubernetes) && servicePort.Protocol.IsTCP() {
						if instances, err := push.InstancesByPort(service, servicePort.Port, nil); err == nil {
							for _, instance := range instances {
								// Skip build outbound listener to the node itself,
								// as when app access itself by pod ip will not flow through this listener.
								// Simultaneously, it will be duplicate with inbound listener.
								if instance.Endpoint.Address == node.IPAddresses[0] {
									continue
								}
								listenerOpts.bind = instance.Endpoint.Address
								vout.insertService(configgen, node, listenerOpts, pluginParams,
									virtualServices, actualWildcard)
							}
						} else {
							// we can't do anything. Fallback to the usual way of constructing listeners
							// for headless services that use 0.0.0.0:Port listener
							vout.insertService(configgen, node, listenerOpts, pluginParams,
								virtualServices, actualWildcard)
						}
					} else {
						// Standard logic for headless and non headless services
						if features.EnableThriftFilter &&
							servicePort.Protocol.IsThrift() {
							listenerOpts.bind = service.Address
						}
						if bindToPort {
							vout.insertService(configgen, node, listenerOpts, pluginParams,
								virtualServices, actualWildcard)
						} else {
							vout.insertService(configgen, node, listenerOpts, pluginParams,
								virtualServices, actualWildcard)
						}
					}
				}
			}
		}
	}

	lb.outboundListeners = vout.getAllServiceListeners(configgen, node, push)
	return lb
}

// addressKey takes a *core.Address and coverts it to a unique string identifier
func addressKey(addr *core.Address) string {
	switch t := addr.Address.(type) {
	case *core.Address_SocketAddress:
		var port string
		switch pt := t.SocketAddress.PortSpecifier.(type) {
		case *core.SocketAddress_NamedPort:
			port = pt.NamedPort
		case *core.SocketAddress_PortValue:
			port = strconv.Itoa(int(pt.PortValue))
		}
		return t.SocketAddress.Address + "_" + port
	case *core.Address_Pipe:
		return t.Pipe.Path
	}
	return addr.String()
}

func (lb *ListenerBuilder) buildHTTPProxyListener(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	httpProxy := configgen.buildHTTPProxy(lb.node, lb.push)
	if httpProxy == nil {
		return lb
	}
	removeListenerFilterTimeout([]*xdsapi.Listener{httpProxy})
	lb.patchOneListener(httpProxy, networking.EnvoyFilter_SIDECAR_OUTBOUND)
	lb.httpProxyListener = httpProxy
	return lb
}

func (lb *ListenerBuilder) buildManagementListeners(_ *ConfigGeneratorImpl) *ListenerBuilder {
	// Do not generate any management port listeners if the user has specified a SidecarScope object
	// with ingress listeners. Specifying the ingress listener implies that the user wants
	// to only have those specific listeners and nothing else, in the inbound path.
	if lb.node.SidecarScope.HasCustomIngressListeners || lb.node.GetInterceptionMode() == model.InterceptionNone {
		return lb
	}
	// Let ServiceDiscovery decide which IP and Port are used for management if
	// there are multiple IPs
	mgmtListeners := make([]*xdsapi.Listener, 0)
	for _, ip := range lb.node.IPAddresses {
		managementPorts := lb.push.ManagementPorts(ip)
		management := buildSidecarInboundMgmtListeners(lb.node, lb.push, managementPorts, ip)
		mgmtListeners = append(mgmtListeners, management...)
	}
	addresses := make(map[string]*xdsapi.Listener)
	for _, listener := range lb.inboundListeners {
		if listener != nil {
			addresses[addressKey(listener.Address)] = listener
		}
	}
	for _, listener := range lb.outboundListeners {
		if listener != nil {
			addresses[addressKey(listener.Address)] = listener
		}
	}

	// If management listener port and service port are same, bad things happen
	// when running in kubernetes, as the probes stop responding. So, append
	// non overlapping listeners only.
	for i := range mgmtListeners {
		m := mgmtListeners[i]
		addressString := addressKey(m.Address)
		existingListener, ok := addresses[addressString]
		if ok {
			log.Debugf("Omitting listener for management address %s due to collision with service listener (%s)",
				m.Name, existingListener.Name)
			continue
		} else {
			// dedup management listeners as well
			addresses[addressString] = m
			lb.inboundListeners = append(lb.inboundListeners, m)
		}

	}
	return lb
}

func (lb *ListenerBuilder) buildVirtualOutboundListener(configgen *ConfigGeneratorImpl) *ListenerBuilder {

	lb.outboundListeners, lb.virtualOutboundListener = lb.voutBuilder.getAllOutboundListener(configgen, lb.node, lb.push, lb.outboundListeners)

	return lb
}

// TProxy uses only the virtual outbound listener on 15001 for both directions
// but we still ship the no-op virtual inbound listener, so that the code flow is same across REDIRECT and TPROXY.
func (lb *ListenerBuilder) buildVirtualInboundListener(configgen *ConfigGeneratorImpl) *ListenerBuilder {
	var isTransparentProxy *wrappers.BoolValue
	if lb.node.GetInterceptionMode() == model.InterceptionTproxy {
		isTransparentProxy = proto.BoolTrue
	}

	actualWildcard, _ := getActualWildcardAndLocalHost(lb.node)
	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	filterChains, needTLSForPassThroughFilterChain := buildInboundCatchAllNetworkFilterChains(configgen, lb.node, lb.push)
	if features.EnableProtocolSniffingForInbound {
		filterChains = append(filterChains, buildInboundCatchAllHTTPFilterChains(configgen, lb.node, lb.push)...)
	}
	lb.virtualInboundListener = &xdsapi.Listener{
		Name:             VirtualInboundListenerName,
		Address:          util.BuildAddress(actualWildcard, ProxyInboundListenPort),
		Transparent:      isTransparentProxy,
		UseOriginalDst:   proto.BoolTrue,
		TrafficDirection: core.TrafficDirection_INBOUND,
		FilterChains:     filterChains,
	}
	lb.aggregateVirtualInboundListener(needTLSForPassThroughFilterChain)

	return lb
}

func (lb *ListenerBuilder) patchOneListener(listener *xdsapi.Listener, ctx networking.EnvoyFilter_PatchContext) *xdsapi.Listener {
	if listener == nil {
		return nil
	}
	tempArray := []*xdsapi.Listener{listener}
	tempArray = envoyfilter.ApplyListenerPatches(ctx, lb.node, lb.push, tempArray, true)
	// temp array will either be empty [if virtual listener was removed] or will have a modified listener
	if len(tempArray) == 0 {
		return nil
	}
	return tempArray[0]
}

func (lb *ListenerBuilder) patchListeners() {
	if lb.node.Type == model.Router {
		envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_GATEWAY, lb.node, lb.push, lb.gatewayListeners, false)
		return
	}

	lb.virtualOutboundListener = lb.patchOneListener(lb.virtualOutboundListener, networking.EnvoyFilter_SIDECAR_OUTBOUND)
	lb.virtualInboundListener = lb.patchOneListener(lb.virtualInboundListener, networking.EnvoyFilter_SIDECAR_INBOUND)
	lb.inboundListeners = envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_SIDECAR_INBOUND, lb.node,
		lb.push, lb.inboundListeners, false)
	lb.outboundListeners = envoyfilter.ApplyListenerPatches(networking.EnvoyFilter_SIDECAR_OUTBOUND, lb.node,
		lb.push, lb.outboundListeners, false)
}

func (lb *ListenerBuilder) getListeners() []*xdsapi.Listener {
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

		listeners := make([]*xdsapi.Listener, 0, nListener)
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

		log.Debugf("Build %d listeners for node %s including %d outbound, %d http proxy, %d virtual outbound and %d virtual inbound listeners",
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

// Create pass through filter chains matching ipv4 address and ipv6 address independently.
// This function also returns a boolean indicating whether or not the TLS inspector is needed
// for the filter chain.
func buildInboundCatchAllNetworkFilterChains(configgen *ConfigGeneratorImpl,
	node *model.Proxy, push *model.PushContext) ([]*listener.FilterChain, bool) {
	// ipv4 and ipv6 feature detect
	ipVersions := make([]string, 0, 2)
	if node.SupportsIPv4() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv4)
	}
	if node.SupportsIPv6() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv6)
	}
	filterChains := make([]*listener.FilterChain, 0, 2)

	needTLS := false
	for _, clusterName := range ipVersions {
		tcpProxy := &tcp_proxy.TcpProxy{
			StatPrefix:       clusterName,
			ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: clusterName},
		}

		matchingIP := ""
		if clusterName == util.InboundPassthroughClusterIpv4 {
			matchingIP = "0.0.0.0/0"
		} else if clusterName == util.InboundPassthroughClusterIpv6 {
			matchingIP = "::0/0"
		}

		setAccessLog(push, tcpProxy)
		tcpProxyFilter := &listener.Filter{
			Name:       xdsutil.TCPProxy,
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(tcpProxy)},
		}

		in := &plugin.InputParams{
			Node:             node,
			Push:             push,
			ListenerProtocol: istionetworking.ListenerProtocolTCP,
		}
		var allChains []istionetworking.FilterChain
		for _, p := range configgen.Plugins {
			chains := p.OnInboundPassthroughFilterChains(in)
			allChains = append(allChains, chains...)
		}

		if len(allChains) == 0 {
			// Add one empty entry to the list if none of the plugins are interested in updating the filter chains.
			allChains = []istionetworking.FilterChain{{}}
		}
		// Override the filter chain match to make sure the pass through filter chain captures the pass through traffic.
		for i := range allChains {
			chain := &allChains[i]
			if chain.FilterChainMatch == nil {
				chain.FilterChainMatch = &listener.FilterChainMatch{}
			}
			// Port : EMPTY to match all ports
			chain.FilterChainMatch.DestinationPort = nil
			chain.FilterChainMatch.PrefixRanges = []*core.CidrRange{
				util.ConvertAddressToCidr(matchingIP),
			}
			chain.ListenerProtocol = istionetworking.ListenerProtocolTCP
		}

		mutable := &istionetworking.MutableObjects{
			FilterChains: allChains,
		}
		for _, p := range configgen.Plugins {
			if err := p.OnInboundPassthrough(in, mutable); err != nil {
				log.Errorf("Build inbound passthrough filter chains error: %v", err)
			}
		}

		// Construct the actual filter chains for each of the filter chain from the plugin.
		for _, chain := range allChains {
			filterChain := &listener.FilterChain{
				FilterChainMatch: chain.FilterChainMatch,
				Filters:          append(chain.TCP, tcpProxyFilter),
			}
			if chain.TLSContext != nil {
				filterChain.FilterChainMatch.TransportProtocol = "tls"
				// Update transport socket from the TLS context configured by the plugin.
				filterChain.TransportSocket = &core.TransportSocket{
					Name:       util.EnvoyTLSSocketName,
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(chain.TLSContext)},
				}
			}
			for _, filter := range chain.ListenerFilters {
				if filter.Name == xdsutil.TlsInspector {
					needTLS = true
					break
				}
			}
			filterChain.Name = VirtualInboundListenerName
			filterChains = append(filterChains, filterChain)
		}
	}

	return filterChains, needTLS
}

func buildInboundCatchAllHTTPFilterChains(configgen *ConfigGeneratorImpl,
	node *model.Proxy, push *model.PushContext) []*listener.FilterChain {
	// ipv4 and ipv6 feature detect
	ipVersions := make([]string, 0, 2)
	if node.SupportsIPv4() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv4)
	}
	if node.SupportsIPv6() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv6)
	}
	filterChains := make([]*listener.FilterChain, 0, 2)

	for _, clusterName := range ipVersions {
		matchingIP := ""
		if clusterName == util.InboundPassthroughClusterIpv4 {
			matchingIP = "0.0.0.0/0"
		} else if clusterName == util.InboundPassthroughClusterIpv6 {
			matchingIP = "::0/0"
		}

		port := &model.Port{
			Name:     "virtualInbound",
			Port:     15006,
			Protocol: protocol.HTTP,
		}

		in := &plugin.InputParams{
			ListenerProtocol:   istionetworking.ListenerProtocolHTTP,
			Node:               node,
			ServiceInstance:    dummyServiceInstance,
			Port:               port,
			Push:               push,
			Bind:               matchingIP,
			InboundClusterName: clusterName,
		}
		// Call plugins to install authn/authz policies.
		var allChains []istionetworking.FilterChain
		for _, p := range configgen.Plugins {
			chains := p.OnInboundPassthroughFilterChains(in)
			allChains = append(allChains, chains...)
		}
		if len(allChains) == 0 {
			// Add one empty entry to the list if none of the plugins are interested in updating the filter chains.
			allChains = []istionetworking.FilterChain{{}}
		}

		// Override the filter chain match to make sure the pass through filter chain captures the pass through traffic.
		for i := range allChains {
			chain := &allChains[i]
			if chain.FilterChainMatch == nil {
				chain.FilterChainMatch = &listener.FilterChainMatch{}
			}
			chain.FilterChainMatch.PrefixRanges = []*core.CidrRange{
				util.ConvertAddressToCidr(matchingIP),
			}
			chain.FilterChainMatch.ApplicationProtocols = plaintextHTTPALPNs
			chain.ListenerProtocol = istionetworking.ListenerProtocolHTTP
		}

		mutable := &istionetworking.MutableObjects{
			FilterChains: allChains,
		}
		for _, p := range configgen.Plugins {
			if err := p.OnInboundPassthrough(in, mutable); err != nil {
				log.Errorf("Build inbound passthrough filter chains error: %v", err)
			}
		}

		// Construct the actual filter chains for each of the filter chain from the plugin.
		for _, chain := range allChains {
			httpOpts := configgen.buildSidecarInboundHTTPListenerOptsForPortOrUDS(node, in)
			httpOpts.statPrefix = clusterName
			connectionManager := buildHTTPConnectionManager(in, httpOpts, chain.HTTP)

			filter := &listener.Filter{
				Name:       xdsutil.HTTPConnectionManager,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(connectionManager)},
			}

			filterChain := &listener.FilterChain{
				FilterChainMatch: chain.FilterChainMatch,
				Filters:          []*listener.Filter{filter},
			}
			if chain.TLSContext != nil {
				filterChain.FilterChainMatch.TransportProtocol = "tls"
				filterChain.FilterChainMatch.ApplicationProtocols =
					append(filterChain.FilterChainMatch.ApplicationProtocols, mtlsHTTPALPNs...)

				// Update transport socket from the TLS context configured by the plugin.
				filterChain.TransportSocket = &core.TransportSocket{
					Name:       util.EnvoyTLSSocketName,
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(chain.TLSContext)},
				}
			}
			filterChain.Name = virtualInboundCatchAllHTTPFilterChainName
			filterChains = append(filterChains, filterChain)
		}
	}

	return filterChains
}

func buildOutboundCatchAllNetworkFiltersOnly(push *model.PushContext, node *model.Proxy) []*listener.Filter {

	filterStack := make([]*listener.Filter, 0)
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

	tcpProxy := &tcp_proxy.TcpProxy{
		StatPrefix:       egressCluster,
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: egressCluster},
	}
	setAccessLog(push, tcpProxy)
	filterStack = append(filterStack, &listener.Filter{
		Name:       xdsutil.TCPProxy,
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

	return []*listener.FilterChain{
		{
			Name:    VirtualOutboundCatchAllTCPFilterChainName,
			Filters: filterStack,
		},
	}
}
