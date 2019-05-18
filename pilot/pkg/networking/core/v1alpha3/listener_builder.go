package v1alpha3

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	google_protobuf "github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/proto"
	"istio.io/pkg/log"
)

// A stateful listener builder
type ListenerBuilder struct {
	node                   *model.Proxy
	inboundListeners       []*xdsapi.Listener
	outboundListeners      []*xdsapi.Listener
	managementListeners    []*xdsapi.Listener
	virtualListener        *xdsapi.Listener
	virtualInboundListener *xdsapi.Listener
}

func NewListenerBuilder(node *model.Proxy) *ListenerBuilder {
	builder := &ListenerBuilder{
		node: node,
	}
	return builder
}

func (builder *ListenerBuilder) buildSidecarInboundListeners(
	configgen *ConfigGeneratorImpl,
	env *model.Environment, node *model.Proxy, push *model.PushContext,
	proxyInstances []*model.ServiceInstance) *ListenerBuilder {
	builder.inboundListeners = configgen.buildSidecarInboundListeners(env, node, push, proxyInstances)
	return builder
}

func (builder *ListenerBuilder) buildSidecarOutboundListeners(configgen *ConfigGeneratorImpl,
	env *model.Environment, node *model.Proxy, push *model.PushContext,
	proxyInstances []*model.ServiceInstance) *ListenerBuilder {
	builder.outboundListeners = configgen.buildSidecarOutboundListeners(env, node, push, proxyInstances)
	return builder
}

func (builder *ListenerBuilder) buildManagementListeners(_ *ConfigGeneratorImpl,
	env *model.Environment, node *model.Proxy, _ *model.PushContext,
	_ []*model.ServiceInstance) *ListenerBuilder {

	noneMode := node.GetInterceptionMode() == model.InterceptionNone

	// Do not generate any management port listeners if the user has specified a SidecarScope object
	// with ingress listeners. Specifying the ingress listener implies that the user wants
	// to only have those specific listeners and nothing else, in the inbound path.
	sidecarScope := node.SidecarScope
	if sidecarScope != nil && sidecarScope.HasCustomIngressListeners ||
		noneMode {
		return builder
	}
	// Let ServiceDiscovery decide which IP and Port are used for management if
	// there are multiple IPs
	mgmtListeners := make([]*xdsapi.Listener, 0)
	for _, ip := range node.IPAddresses {
		managementPorts := env.ManagementPorts(ip)
		management := buildSidecarInboundMgmtListeners(node, env, managementPorts, ip)
		mgmtListeners = append(mgmtListeners, management...)
	}
	addresses := make(map[string]*xdsapi.Listener)
	for _, listener := range builder.inboundListeners {
		if listener != nil {
			addresses[listener.Address.String()] = listener
		}
	}
	for _, listener := range builder.outboundListeners {
		if listener != nil {
			addresses[listener.Address.String()] = listener
		}
	}

	// If management listener port and service port are same, bad things happen
	// when running in kubernetes, as the probes stop responding. So, append
	// non overlapping listeners only.
	for i := range mgmtListeners {
		m := mgmtListeners[i]
		addressString := m.Address.String()
		existingListener, ok := addresses[addressString]
		if ok {
			log.Warnf("Omitting listener for management address %s (%s) due to collision with service listener (%s)",
				m.Name, m.Address.String(), existingListener.Name)
			continue
		} else {
			// dedup management listeners as well
			addresses[addressString] = m
			builder.managementListeners = append(builder.managementListeners, m)
		}

	}
	return builder
}

func (builder *ListenerBuilder) buildVirtualListener(
	env *model.Environment, node *model.Proxy) *ListenerBuilder {

	var isTransparentProxy *google_protobuf.BoolValue
	if node.GetInterceptionMode() == model.InterceptionTproxy {
		isTransparentProxy = proto.BoolTrue
	}

	tcpProxyFilter := newTCPProxyListenerFilter(env, node, false)
	actualWildcard, _ := getActualWildcardAndLocalHost(node)
	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	builder.virtualListener = &xdsapi.Listener{
		Name:           VirtualListenerName,
		Address:        util.BuildAddress(actualWildcard, uint32(env.Mesh.ProxyListenPort)),
		Transparent:    isTransparentProxy,
		UseOriginalDst: proto.BoolTrue,
		FilterChains: []listener.FilterChain{
			{
				Filters: []listener.Filter{*tcpProxyFilter},
			},
		},
	}
	return builder
}

func (builder *ListenerBuilder) buildVirtualInboundListener(env *model.Environment, node *model.Proxy) *ListenerBuilder {
	shouldSplitInOutBound := node.IsInboundCaptureAllPorts()
	if !shouldSplitInOutBound {
		log.Debugf("Inbound and outbound listeners are united in for node %s", node.ID)
		return builder
	}
	var isTransparentProxy *google_protobuf.BoolValue
	if node.GetInterceptionMode() == model.InterceptionTproxy {
		isTransparentProxy = proto.BoolTrue
	}

	tcpProxyFilter := newTCPProxyListenerFilter(env, node, true)
	actualWildcard, _ := getActualWildcardAndLocalHost(node)
	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	builder.virtualInboundListener = &xdsapi.Listener{
		Name:           VirtualInboundListenerName,
		Address:        util.BuildAddress(actualWildcard, ProxyInboundListenPort),
		Transparent:    isTransparentProxy,
		UseOriginalDst: proto.BoolTrue,
		FilterChains: []listener.FilterChain{
			{
				Filters: []listener.Filter{*tcpProxyFilter},
			},
		},
	}
	return builder
}

func (builder *ListenerBuilder) getListeners() []*xdsapi.Listener {
	nInbound, nOutbound, nManagement := len(builder.inboundListeners), len(builder.outboundListeners), len(builder.managementListeners)
	nVirtual, nVirtualInbound := 0, 0
	if builder.virtualListener != nil {
		nVirtual = 1
	}
	if builder.virtualInboundListener != nil {
		nVirtualInbound = 1
	}
	nListener := nInbound + nOutbound + nManagement + nVirtual + nVirtualInbound

	listeners := make([]*xdsapi.Listener, 0, nListener)
	listeners = append(listeners, builder.inboundListeners...)
	listeners = append(listeners, builder.outboundListeners...)
	listeners = append(listeners, builder.managementListeners...)
	if builder.virtualListener != nil {
		listeners = append(listeners, builder.virtualListener)
	}
	if builder.virtualInboundListener != nil {
		listeners = append(listeners, builder.virtualInboundListener)
	}

	log.Debugf("Build %d listeners for node %s including %d inbound, %d outbound, %d management, %d virtual and %d virtual inbound listeners",
		nListener,
		builder.node.ID,
		nInbound, nOutbound, nManagement,
		nVirtual, nVirtualInbound)
	return listeners
}

func newTCPProxyListenerFilter(env *model.Environment, node *model.Proxy, isInboundListener bool) *listener.Filter {
	tcpProxy := &tcp_proxy.TcpProxy{
		StatPrefix:       util.BlackHoleCluster,
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
	}

	if env.Mesh.OutboundTrafficPolicy.Mode == meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY ||
		isInboundListener {
		// We need a passthrough filter to fill in the filter stack for orig_dst listener
		tcpProxy = &tcp_proxy.TcpProxy{
			StatPrefix:       util.PassthroughCluster,
			ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: util.PassthroughCluster},
		}
		setAccessLog(env, node, tcpProxy)
	}

	filter := listener.Filter{
		Name: xdsutil.TCPProxy,
	}

	if util.IsXDSMarshalingToAnyEnabled(node) {
		filter.ConfigType = &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(tcpProxy)}
	} else {
		filter.ConfigType = &listener.Filter_Config{Config: util.MessageToStruct(tcpProxy)}
	}
	return &filter
}
