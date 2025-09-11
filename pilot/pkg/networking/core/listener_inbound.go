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
	"sort"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	envoytype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/extension"
	"istio.io/istio/pilot/pkg/networking/plugin/authz"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

// inboundChainConfig defines the configuration for a single inbound filter chain. This may be created
// as a result of a Service, and Sidecar CR, or for the built-in passthrough filter chains.
type inboundChainConfig struct {
	// clusterName defines the destination cluster for this chain
	clusterName string
	// port defines the port configuration for this chain. Note that there is a Port and TargetPort;
	// most usages should just use TargetPort. Port is mostly used for legacy compatibility and
	// telemetry.
	port model.ServiceInstancePort
	// bind determines where (IP) this filter chain should bind. Note: typically we just end up using
	// 'virtual' listener and do not literally bind to port; in these cases this just impacts naming
	// and telemetry.
	bind string

	// extraBind is string slice and each element is similar with bind address and support multiple addresses for 'virtual' listener
	extraBind []string

	// tlsSettings defines the *custom* TLS settings for the chain. mTLS settings are orthogonal; this
	// only configures TLS overrides.
	tlsSettings *networking.ServerTLSSettings

	// passthrough should be set to true for the 'passthrough' chains, which are the chains always
	// present to handle all unmatched traffic. These have a few naming and quirks that require
	// different configuration.
	passthrough bool

	// bindToPort determines if this chain should form a real listener that actually binds to a real port,
	// or if it should just be a filter chain part of the 'virtual inbound' listener.
	bindToPort bool

	// hbone determines if this is coming from an HBONE request originally
	hbone bool

	// telemetryMetadata defines additional information about the chain for telemetry purposes.
	telemetryMetadata telemetry.FilterChainMetadata

	// proxies that accept service-attached policy should include the service for per-service chains
	// so that those policies can be found
	policyService *model.Service

	configMetadata *config.Meta
}

// StatPrefix returns the stat prefix for the config
func (cc inboundChainConfig) StatPrefix() string {
	var statPrefix string
	if cc.passthrough {
		// A bit arbitrary, but for backwards compatibility just use the cluster name
		statPrefix = cc.clusterName
	} else {
		statPrefix = "inbound_" + cc.Name(istionetworking.ListenerProtocolHTTP)
	}

	statPrefix = util.DelimitedStatsPrefix(statPrefix)
	return statPrefix
}

// Name determines the name for this chain
func (cc inboundChainConfig) Name(protocol istionetworking.ListenerProtocol) string {
	if cc.passthrough {
		// A bit arbitrary, but for backwards compatibility fixed names are used
		// Passthrough chains have fix names based on protocol
		if protocol == istionetworking.ListenerProtocolHTTP {
			return model.VirtualInboundCatchAllHTTPFilterChainName
		}
		return model.VirtualInboundListenerName
	}
	// Everything else derived from bind/port
	return getListenerName(cc.bind, int(cc.port.TargetPort), istionetworking.TransportProtocolTCP)
}

// ToFilterChainMatch builds the FilterChainMatch for the config
func (cc inboundChainConfig) ToFilterChainMatch(opt FilterChainMatchOptions) *listener.FilterChainMatch {
	match := &listener.FilterChainMatch{}
	match.ApplicationProtocols = opt.ApplicationProtocols
	match.TransportProtocol = opt.TransportProtocol
	if cc.port.TargetPort > 0 {
		match.DestinationPort = &wrappers.UInt32Value{Value: cc.port.TargetPort}
	}
	return match
}

func (lb *ListenerBuilder) buildInboundHBONEListeners() []*listener.Listener {
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
	terminate := lb.buildConnectTerminateListener(routes)
	// Now we have top level listener... but we must have an internal listener for each standard filter chain
	// 1 listener per port; that listener will do protocol detection.
	l := &listener.Listener{
		Name:                             MainInternalName,
		ListenerSpecifier:                &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		TrafficDirection:                 core.TrafficDirection_INBOUND,
		ContinueOnListenerFiltersTimeout: true,
	}

	// Flush authz cache since we need filter state for the principal.
	oldBuilder := lb.authzBuilder
	lb.authzBuilder = authz.NewBuilder(authz.Local, lb.push, lb.node, true)
	inboundChainConfigs := lb.buildInboundChainConfigs()
	for _, cc := range inboundChainConfigs {
		cc.hbone = true
		lp := istionetworking.ModelProtocolToListenerProtocol(cc.port.Protocol)
		// Internal chain has no mTLS
		mtls := authn.MTLSSettings{Port: cc.port.TargetPort, Mode: model.MTLSDisable}
		opts := getFilterChainMatchOptions(mtls, lp)
		chains := lb.inboundChainForOpts(cc, mtls, opts)
		for _, c := range chains {
			lb.sanitizeFilterChainForHBONE(c)
		}
		l.FilterChains = append(l.FilterChains, chains...)
	}
	for _, passthrough := range buildInboundHBONEPassthroughChain(lb) {
		lb.sanitizeFilterChainForHBONE(passthrough)
		l.FilterChains = append(l.FilterChains, passthrough)
	}
	// If there are no filter chains, populate a dummy one that never matches. Envoy doesn't allow no chains, but removing the
	// entire listeners makes the errors logs more confusing (instead of "no filter chain found" we have no listener at all).
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
	lb.authzBuilder = oldBuilder
	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, l, istionetworking.ListenerClassSidecarInbound)
	l.ListenerFilters = append(l.ListenerFilters, xdsfilters.OriginalDestination)
	// TODO: Exclude inspectors from some inbound ports.
	l.ListenerFilters = append(l.ListenerFilters, populateListenerFilters(lb.node, l, true)...)
	return []*listener.Listener{terminate, l}
}

func (lb *ListenerBuilder) sanitizeFilterChainForHBONE(c *listener.FilterChain) {
	fcm := c.GetFilterChainMatch()
	if fcm == nil {
		fcm = &listener.FilterChainMatch{}
		c.FilterChainMatch = fcm
	}
	// Clear out settings that do not matter anymore
	fcm.TransportProtocol = ""
	// Filter to only allowed ranges. This ensures we do not get HBONE requests to garbage IPs
	fcm.PrefixRanges = slices.Map(lb.node.IPAddresses, util.ConvertAddressToCidr)
}

// buildInboundListeners creates inbound listeners.
// Typically, this a single listener with many filter chains for each applicable Service; traffic is redirect with iptables.
// However, explicit listeners can be used in NONE mode or with Sidecar.Ingress configuration.
func (lb *ListenerBuilder) buildInboundListeners() []*listener.Listener {
	// All listeners we build
	var listeners []*listener.Listener
	// virtualInboundFilterChains builds up all of the filter chains for the virtual inbound listener
	var virtualInboundFilterChains []*listener.FilterChain
	// For each chain config we will build required filter chain(s)
	for _, cc := range lb.buildInboundChainConfigs() {
		// First, construct our set of filter chain matchers. For a given port, we will have multiple matches
		// to handle mTLS vs plaintext and HTTP vs TCP (depending on protocol and PeerAuthentication).
		var opts []FilterChainMatchOptions
		mtls := lb.authnBuilder.ForPort(cc.port.TargetPort)
		// Chain has explicit user TLS config. This can only apply when the TLS mode is DISABLE to avoid conflicts.
		if cc.tlsSettings != nil && mtls.Mode == model.MTLSDisable {
			// Since we are terminating TLS, we need to treat the protocol as if its terminated.
			// Example: user specifies protocol=HTTPS and user TLS, we will use HTTP
			cc.port.Protocol = cc.port.Protocol.AfterTLSTermination()
			lp := istionetworking.ModelProtocolToListenerProtocol(cc.port.Protocol)
			opts = getTLSFilterChainMatchOptions(lp)
			mtls.TCP = BuildListenerTLSContext(cc.tlsSettings, lb.node, lb.push, istionetworking.TransportProtocolTCP, false)
			mtls.HTTP = mtls.TCP
		} else {
			lp := istionetworking.ModelProtocolToListenerProtocol(cc.port.Protocol)
			opts = getFilterChainMatchOptions(mtls, lp)
		}
		// Build the actual chain
		chains := lb.inboundChainForOpts(cc, mtls, opts)

		if cc.bindToPort {
			// If this config is for bindToPort, we want to actually create a real Listener.
			listeners = append(listeners, lb.inboundCustomListener(cc, chains))
		} else {
			// Otherwise, just append the filter chain to the virtual inbound chains.
			virtualInboundFilterChains = append(virtualInboundFilterChains, chains...)
		}
	}

	if lb.node.GetInterceptionMode() != model.InterceptionNone {
		// Prepend virtual inbound, as long as we are using redirection.
		listeners = append([]*listener.Listener{lb.inboundVirtualListener(virtualInboundFilterChains)}, listeners...)
	}

	return listeners
}

// inboundVirtualListener builds the virtual inbound listener.
func (lb *ListenerBuilder) inboundVirtualListener(chains []*listener.FilterChain) *listener.Listener {
	actualWildcards, _ := getWildcardsAndLocalHost(lb.node.GetIPMode())

	// Build the "virtual" inbound listener. This will capture all inbound redirected traffic and contains:
	// * Passthrough filter chains, matching all unmatched traffic. There are a few of these to handle all cases
	// * Service filter chains. These will either be for each Port exposed by a Service OR Sidecar.Ingress configuration.
	allChains := buildInboundPassthroughChains(lb)
	allChains = append(allChains, chains...)
	l := lb.buildInboundListener(model.VirtualInboundListenerName, actualWildcards, uint32(lb.push.Mesh.ProxyInboundListenPort), false, allChains)
	return l
}

// inboundCustomListener build a custom listener that actually binds to a port, rather than relying on redirection.
func (lb *ListenerBuilder) inboundCustomListener(cc inboundChainConfig, chains []*listener.FilterChain) *listener.Listener {
	addresses := []string{cc.bind}
	if len(cc.extraBind) > 0 {
		addresses = append(addresses, cc.extraBind...)
	}
	ll := lb.buildInboundListener(cc.Name(istionetworking.ListenerProtocolTCP), addresses, cc.port.TargetPort, true, chains)
	return ll
}

func (lb *ListenerBuilder) buildInboundListener(name string, addresses []string, tPort uint32,
	bindToPort bool, chains []*listener.FilterChain,
) *listener.Listener {
	if len(addresses) == 0 {
		return nil
	}
	address := util.BuildAddress(addresses[0], tPort)
	l := &listener.Listener{
		Name:                                 name,
		Address:                              address,
		TrafficDirection:                     core.TrafficDirection_INBOUND,
		ContinueOnListenerFiltersTimeout:     true,
		MaxConnectionsToAcceptPerSocketEvent: maxConnectionsToAcceptPerSocketEvent(),
	}
	if features.EnableDualStack && len(addresses) > 1 {
		// add extra addresses for the listener
		l.AdditionalAddresses = util.BuildAdditionalAddresses(addresses[1:], tPort)
	}
	if lb.node.Metadata.InboundListenerExactBalance {
		l.ConnectionBalanceConfig = &listener.Listener_ConnectionBalanceConfig{
			BalanceType: &listener.Listener_ConnectionBalanceConfig_ExactBalance_{
				ExactBalance: &listener.Listener_ConnectionBalanceConfig_ExactBalance{},
			},
		}
	}
	if !bindToPort && lb.node.GetInterceptionMode() == model.InterceptionTproxy {
		l.Transparent = proto.BoolTrue
	}
	if bindToPort {
		// This only applies to listeners that actually bind to a port given that only those listeners
		// interface with the OS.
		// See https://github.com/envoyproxy/envoy/blob/v1.35.3/source/common/network/tcp_listener_impl.cc#L57
		l.MaxConnectionsToAcceptPerSocketEvent = maxConnectionsToAcceptPerSocketEvent()
	}

	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, l, istionetworking.ListenerClassSidecarInbound)
	l.FilterChains = chains
	l.ListenerFilters = populateListenerFilters(lb.node, l, bindToPort)
	l.ListenerFiltersTimeout = lb.push.Mesh.GetProtocolDetectionTimeout()
	return l
}

// inboundChainForOpts builds a set of filter chains
func (lb *ListenerBuilder) inboundChainForOpts(cc inboundChainConfig, mtls authn.MTLSSettings, opts []FilterChainMatchOptions) []*listener.FilterChain {
	log.Debugf("jaellio: creating inbound chains")
	chains := make([]*listener.FilterChain, 0, len(opts))
	for _, opt := range opts {
		var filterChain *listener.FilterChain
		switch opt.Protocol {
		// Switch on the protocol. Note: we do not need to handle Auto protocol as it will already be split into a TCP and HTTP option.
		case istionetworking.ListenerProtocolHTTP:
			filterChain = &listener.FilterChain{
				FilterChainMatch: cc.ToFilterChainMatch(opt),
				Filters:          lb.buildInboundNetworkFiltersForHTTP(cc),
				TransportSocket:  buildDownstreamTLSTransportSocket(opt.ToTransportSocket(mtls)),
				Name:             cc.Name(opt.Protocol),
			}

		case istionetworking.ListenerProtocolTCP:
			filterChain = &listener.FilterChain{
				FilterChainMatch: cc.ToFilterChainMatch(opt),
				Filters:          lb.buildInboundNetworkFilters(cc),
				TransportSocket:  buildDownstreamTLSTransportSocket(opt.ToTransportSocket(mtls)),
				Name:             cc.Name(opt.Protocol),
			}

		}
		if cc.configMetadata != nil {
			filterChain.Metadata = util.BuildConfigInfoMetadata(*cc.configMetadata)
		}
		chains = append(chains, filterChain)
	}
	return chains
}

func getSidecarIngressPortList(node *model.Proxy) sets.Set[int] {
	sidecarScope := node.SidecarScope
	ingressPortListSet := sets.New[int]()
	for _, ingressListener := range sidecarScope.Sidecar.Ingress {
		ingressPortListSet.Insert(int(ingressListener.Port.Number))
	}
	return ingressPortListSet
}

func (lb *ListenerBuilder) getFilterChainsByServicePort() map[uint32]inboundChainConfig {
	chainsByPort := make(map[uint32]inboundChainConfig)
	ingressPortListSet := sets.New[int]()
	sidecarScope := lb.node.SidecarScope
	mergeServicePorts := features.EnableSidecarServiceInboundListenerMerge && sidecarScope.HasIngressListener()
	if mergeServicePorts {
		ingressPortListSet = getSidecarIngressPortList(lb.node)
	}
	actualWildcards, _ := getWildcardsAndLocalHost(lb.node.GetIPMode())
	for _, i := range lb.node.ServiceTargets {
		bindToPort := getBindToPort(networking.CaptureMode_DEFAULT, lb.node)
		// Skip ports we cannot bind to
		wildcard := wildCards[lb.node.GetIPMode()][0]
		if canbind, knownlistener := lb.node.CanBindToPort(bindToPort, lb.node, lb.push, actualWildcards[0],
			int(i.Port.TargetPort), i.Port.Protocol, wildcard); !canbind {
			if knownlistener {
				log.Warnf("buildInboundListeners: skipping sidecar port %d for node %s as it conflicts with static listener",
					i.Port.TargetPort, lb.node.ID)
			} else {
				log.Warnf("buildInboundListeners: skipping privileged service port %d for node %s as it is an unprivileged proxy",
					i.Port.TargetPort, lb.node.ID)
			}
			continue
		}
		port := i.Port
		if mergeServicePorts &&
			// ingress listener port means the target port, may not equal to service port
			ingressPortListSet.Contains(int(port.TargetPort)) {
			// here if port is declared in service and sidecar ingress both, we continue to take the one on sidecar + other service ports
			// e.g. 1,2, 3 in service and 3,4 in sidecar ingress,
			// this will still generate listeners for 1,2,3,4 where 3 is picked from sidecar ingress
			// port present in sidecarIngress listener so let sidecar take precedence
			continue
		}
		cc := inboundChainConfig{
			telemetryMetadata: telemetry.FilterChainMetadata{InstanceHostname: i.Service.Hostname},
			port:              port,
			clusterName:       model.BuildInboundSubsetKey(int(port.TargetPort)),
			bind:              actualWildcards[0],
			bindToPort:        bindToPort,
			hbone:             lb.node.IsWaypointProxy(),
		}

		// add extra binding addresses
		if len(actualWildcards) > 1 {
			cc.extraBind = actualWildcards[1:]
		}
		if i.Service.Attributes.ServiceRegistry == provider.Kubernetes {
			cc.telemetryMetadata.KubernetesServiceNamespace = i.Service.Attributes.Namespace
			cc.telemetryMetadata.KubernetesServiceName = i.Service.Attributes.Name
		}
		// First, make sure there is a distinct instance used per port.
		// The Service is *almost* not relevant, but some Telemetry is per-service.
		// If there is a conflict, we will use the oldest Service. This impacts the protocol used as well.
		if old, f := chainsByPort[port.TargetPort]; f {
			reportInboundConflict(lb, old, cc)
			continue
		}
		chainsByPort[port.TargetPort] = cc
	}
	return chainsByPort
}

// buildInboundChainConfigs builds all the application chain configs.
func (lb *ListenerBuilder) buildInboundChainConfigs() []inboundChainConfig {
	var chainsByPort map[uint32]inboundChainConfig
	// No user supplied sidecar scope or the user supplied one has no ingress listeners.
	if !lb.node.SidecarScope.HasIngressListener() {

		// We should not create inbound listeners in NONE mode based on the service instances
		// Doing so will prevent the workloads from starting as they would be listening on the same port
		// Users are required to provide the sidecar config to define the inbound listeners
		if lb.node.GetInterceptionMode() == model.InterceptionNone {
			return nil
		}
		chainsByPort = lb.getFilterChainsByServicePort()
	} else {
		// only allow to merge inbound listeners if sidecar has ingress listener pilot has env EnableSidecarServiceInboundListenerMerge set
		if features.EnableSidecarServiceInboundListenerMerge {
			chainsByPort = lb.getFilterChainsByServicePort()
		} else {
			chainsByPort = make(map[uint32]inboundChainConfig)
		}

		for _, i := range lb.node.SidecarScope.Sidecar.Ingress {
			port := model.ServiceInstancePort{
				ServicePort: &model.Port{
					Name:     i.Port.Name,
					Port:     int(i.Port.Number),
					Protocol: protocol.Parse(i.Port.Protocol),
				},
				TargetPort: i.Port.Number, // No targetPort support in the API
			}
			bindtoPort := getBindToPort(i.CaptureMode, lb.node)
			wildcard := wildCards[lb.node.GetIPMode()][0]
			// Skip ports we cannot bind to
			if canbind, knownlistener := lb.node.CanBindToPort(bindtoPort, lb.node, nil, i.Bind, port.Port, port.Protocol, wildcard); !canbind {
				if knownlistener {
					log.Warnf("buildInboundListeners: skipping sidecar port %d for node %s as it conflicts with static listener",
						port.TargetPort, lb.node.ID)
				} else {
					log.Warnf("buildInboundListeners: skipping privileged service port %d for node %s as it is an unprivileged proxy",
						port.TargetPort, lb.node.ID)
				}
				continue
			}
			cc := inboundChainConfig{
				// Sidecar config doesn't have a real hostname. In order to give some telemetry info, make a synthetic hostname.
				telemetryMetadata: telemetry.FilterChainMetadata{
					InstanceHostname: host.Name(lb.node.SidecarScope.Name + "." + lb.node.SidecarScope.Namespace),
				},
				port:        port,
				clusterName: model.BuildInboundSubsetKey(int(port.TargetPort)),
				bind:        i.Bind,
				bindToPort:  bindtoPort,
				hbone:       lb.node.IsWaypointProxy(),
				configMetadata: &config.Meta{
					Name:             lb.node.SidecarScope.Name,
					Namespace:        lb.node.SidecarScope.Namespace,
					GroupVersionKind: gvk.Sidecar,
				},
			}
			if cc.bind == "" {
				// If user didn't provide, pick one based on IP
				actualWildcards := getSidecarInboundBindIPs(lb.node)
				cc.bind = actualWildcards[0]
				if len(actualWildcards) > 1 {
					cc.extraBind = actualWildcards[1:]
				}
			}

			// If there is a conflict, we will use the oldest Service. This impacts the protocol used as well.
			if old, f := chainsByPort[port.TargetPort]; f {
				reportInboundConflict(lb, old, cc)
				continue
			}

			if i.Tls != nil && features.EnableTLSOnSidecarIngress {
				// User provided custom TLS settings
				cc.tlsSettings = i.Tls.DeepCopy()
				cc.tlsSettings.CipherSuites = security.FilterCipherSuites(cc.tlsSettings.CipherSuites)
				cc.port.Protocol = cc.port.Protocol.AfterTLSTermination()
			}

			chainsByPort[port.TargetPort] = cc
		}
	}
	chainConfigs := make([]inboundChainConfig, 0, len(chainsByPort))
	for _, cc := range chainsByPort {
		chainConfigs = append(chainConfigs, cc)
	}
	// Give a stable order to the chains
	sort.Slice(chainConfigs, func(i, j int) bool {
		return chainConfigs[i].port.TargetPort < chainConfigs[j].port.TargetPort
	})

	return chainConfigs
}

// getBindToPort determines whether we should bind to port based on the chain-specific config and the proxy
func getBindToPort(mode networking.CaptureMode, node *model.Proxy) bool {
	if mode == networking.CaptureMode_DEFAULT {
		// Chain doesn't specify explicit config, so use the proxy defaults
		return node.GetInterceptionMode() == model.InterceptionNone
	}
	// Explicitly configured in the config, ignore proxy defaults
	return mode == networking.CaptureMode_NONE
}

// populateListenerFilters determines the appropriate listener filters based on the listener
// HTTP and TLS inspectors are automatically derived based on FilterChainMatch requirements.
func populateListenerFilters(node *model.Proxy, vi *listener.Listener, bindToPort bool) []*listener.ListenerFilter {
	lf := make([]*listener.ListenerFilter, 0, 4)
	if !bindToPort {
		lf = append(lf, xdsfilters.OriginalDestination)
	}
	if !bindToPort && node.GetInterceptionMode() == model.InterceptionTproxy {
		lf = append(lf, xdsfilters.OriginalSrc)
	}

	// inspectors builds up a map of port -> required inspectors (TLS/HTTP)
	inspectors := map[int]enabledInspector{}
	for _, fc := range vi.FilterChains {
		port := fc.GetFilterChainMatch().GetDestinationPort().GetValue()
		needsTLS := fc.GetFilterChainMatch().GetTransportProtocol() == xdsfilters.TLSTransportProtocol
		needHTTP := false
		for _, ap := range fc.GetFilterChainMatch().GetApplicationProtocols() {
			// Check for HTTP protocol - these require HTTP inspector
			if ap == "http/1.1" || ap == "h2c" {
				needHTTP = true
				break
			}
		}
		// Port may already have config; we OR them together. If any filter chain on that port is enabled
		// we will enable the inspector.
		i := inspectors[int(port)]
		i.HTTPInspector = i.HTTPInspector || needHTTP
		i.TLSInspector = i.TLSInspector || needsTLS
		inspectors[int(port)] = i
	}

	// Enable TLS inspector on any ports we need it
	if needsTLS(inspectors) {
		lf = append(lf, buildTLSInspector(inspectors))
	}

	// Note: the HTTP inspector should be after TLS inspector.
	// If TLS inspector sets transport protocol to tls, the http inspector
	// won't inspect the packet.
	if needsHTTP(inspectors) {
		lf = append(lf, buildHTTPInspector(inspectors))
	}

	return lf
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

func needsTLS(inspectors map[int]enabledInspector) bool {
	for _, i := range inspectors {
		if i.TLSInspector {
			return true
		}
	}
	return false
}

func needsHTTP(inspectors map[int]enabledInspector) bool {
	for _, i := range inspectors {
		if i.HTTPInspector {
			return true
		}
	}
	return false
}

// buildTLSInspector creates a tls inspector filter. Based on the configured ports, this may be enabled
// for only some ports.
func buildTLSInspector(inspectors map[int]enabledInspector) *listener.ListenerFilter {
	// TODO share logic with HTTP inspector
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
			Name:           wellknown.TLSInspector,
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
		Name:       wellknown.TLSInspector,
		ConfigType: xdsfilters.TLSInspector.ConfigType,
		// Exclude all disabled ports
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
		Name:       wellknown.HTTPInspector,
		ConfigType: xdsfilters.HTTPInspector.ConfigType,
		// Exclude all disabled ports
		FilterDisabled: listenerPredicateExcludePorts(ports),
	}
	return filter
}

func reportInboundConflict(lb *ListenerBuilder, old inboundChainConfig, cc inboundChainConfig) {
	// If the protocols and service do not match, we have a real conflict. For example, one Service may
	// define TCP and the other HTTP. Report this up to the user.
	if old.port.Protocol != cc.port.Protocol && old.telemetryMetadata.InstanceHostname != cc.telemetryMetadata.InstanceHostname {
		lb.push.AddMetric(model.ProxyStatusConflictInboundListener, lb.node.ID, lb.node.ID,
			fmt.Sprintf("Conflicting inbound listener:%d. existing: %s, incoming: %s", cc.port.TargetPort,
				old.telemetryMetadata.InstanceHostname, cc.telemetryMetadata.InstanceHostname))
		return
	}
	// This can happen if two services select the same pod with same port and protocol - we should skip
	// building listener again, but no need to report to the user
	if old.telemetryMetadata.InstanceHostname != cc.telemetryMetadata.InstanceHostname {
		log.Debugf("skipping inbound listener:%d as we have already build it for existing host: %s, new host: %s",
			cc.port.TargetPort,
			old.telemetryMetadata.InstanceHostname, cc.telemetryMetadata.InstanceHostname)
	}
}

func buildInboundHBONEPassthroughChain(lb *ListenerBuilder) []*listener.FilterChain {
	mtls := authn.MTLSSettings{
		Port: 0,
		Mode: model.MTLSDisable,
	}
	cc := inboundChainConfig{
		port: model.ServiceInstancePort{
			ServicePort: &model.Port{
				Name: model.VirtualInboundListenerName,
				// Port as 0 doesn't completely make sense here, since we get weird tracing decorators like `:0/*`,
				// but this is backwards compatible and there aren't any perfect options.
				Port:     0,
				Protocol: protocol.Unsupported,
			},
			TargetPort: mtls.Port,
		},
		clusterName: util.InboundPassthroughCluster,
		passthrough: true,
		hbone:       lb.node.IsWaypointProxy(),
	}

	opts := getFilterChainMatchOptions(mtls, istionetworking.ListenerProtocolAuto)
	return lb.inboundChainForOpts(cc, mtls, opts)
}

// buildInboundPassthroughChains builds the passthrough chains. These match any unmatched traffic.
// This allows traffic to ports not exposed by any Service, for example.
func buildInboundPassthroughChains(lb *ListenerBuilder) []*listener.FilterChain {
	// Setup enough slots for common max size (permissive mode is 5 filter chains). This is not
	// exact, just best effort optimization
	filterChains := make([]*listener.FilterChain, 0, 1+5)
	filterChains = append(filterChains, buildInboundBlackhole(lb))

	mtlsOptions := lb.authnBuilder.ForPassthrough()
	for _, mtls := range mtlsOptions {
		cc := inboundChainConfig{
			port: model.ServiceInstancePort{
				ServicePort: &model.Port{
					Name: model.VirtualInboundListenerName,
					// Port as 0 doesn't completely make sense here, since we get weird tracing decorators like `:0/*`,
					// but this is backwards compatible and there aren't any perfect options.
					Port:     0,
					Protocol: protocol.Unsupported,
				},
				TargetPort: mtls.Port,
			},
			clusterName: util.InboundPassthroughCluster,
			passthrough: true,
			hbone:       lb.node.IsWaypointProxy(),
		}
		opts := getFilterChainMatchOptions(mtls, istionetworking.ListenerProtocolAuto)
		filterChains = append(filterChains, lb.inboundChainForOpts(cc, mtls, opts)...)
	}

	return filterChains
}

// buildInboundBlackhole builds a special filter chain for the virtual inbound matching traffic to the port the listener is actually on.
// This avoids a possible loop where traffic sent to this port would continually call itself indefinitely.
func buildInboundBlackhole(lb *ListenerBuilder) *listener.FilterChain {
	var filters []*listener.Filter
	if !lb.node.IsWaypointProxy() {
		filters = append(filters, buildMetadataExchangeNetworkFilters()...)
	}
	filters = append(filters, buildMetricsNetworkFilters(lb.push, lb.node, istionetworking.ListenerClassSidecarInbound, nil)...)
	filters = append(filters, &listener.Filter{
		Name: wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(&tcp.TcpProxy{
			StatPrefix:       util.BlackHoleCluster,
			ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
		})},
	})
	return &listener.FilterChain{
		Name: model.VirtualInboundBlackholeFilterChainName,
		FilterChainMatch: &listener.FilterChainMatch{
			DestinationPort: &wrappers.UInt32Value{Value: uint32(lb.push.Mesh.ProxyInboundListenPort)},
		},
		Filters: filters,
	}
}

// buildSidecarInboundHTTPOpts sets up HTTP options for a given chain.
func buildSidecarInboundHTTPOpts(lb *ListenerBuilder, cc inboundChainConfig) *httpListenerOpts {
	ph := util.GetProxyHeaders(lb.node, lb.push, istionetworking.ListenerClassSidecarInbound)
	httpOpts := &httpListenerOpts{
		routeConfig:      buildSidecarInboundHTTPRouteConfig(lb, cc),
		rds:              "", // no RDS for inbound traffic
		useRemoteAddress: false,
		connectionManager: &hcm.HttpConnectionManager{
			// Append and forward client cert to backend, if configured
			ForwardClientCertDetails: ph.ForwardedClientCert,
			SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
				Subject: &wrappers.BoolValue{Value: ph.SetCurrentCertDetails.GetSubject() == nil || ph.SetCurrentCertDetails.Subject.Value},
				Uri:     ph.SetCurrentCertDetails.GetUri() == nil || ph.SetCurrentCertDetails.Uri.Value,
				Dns:     ph.SetCurrentCertDetails.GetDns() == nil || ph.SetCurrentCertDetails.Dns.Value,
				Cert:    ph.SetCurrentCertDetails.GetCert() != nil && ph.SetCurrentCertDetails.Cert.Value,
				Chain:   ph.SetCurrentCertDetails.GetChain() != nil && ph.SetCurrentCertDetails.Chain.Value,
			},
			ServerName:                 ph.ServerName,
			ServerHeaderTransformation: ph.ServerHeaderTransformation,
			GenerateRequestId:          ph.GenerateRequestID,
			Proxy_100Continue:          features.Enable100ContinueHeaders,
		},
		suppressEnvoyDebugHeaders: ph.SuppressDebugHeaders,
		protocol:                  cc.port.Protocol,
		class:                     istionetworking.ListenerClassSidecarInbound,
		port:                      int(cc.port.TargetPort),
		statPrefix:                cc.StatPrefix(),
		hbone:                     cc.hbone,
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

	return httpOpts
}

// buildInboundNetworkFiltersForHTTP builds the network filters that should be inserted before an HCM.
// This should only be used with HTTP; see buildInboundNetworkFilters for TCP
func (lb *ListenerBuilder) buildInboundNetworkFiltersForHTTP(cc inboundChainConfig) []*listener.Filter {
	// Add network level WASM filters if any configured.
	httpOpts := buildSidecarInboundHTTPOpts(lb, cc)
	wasm := lb.push.WasmPluginsByListenerInfo(lb.node, model.WasmPluginListenerInfo{
		Port:  httpOpts.port,
		Class: httpOpts.class,
	}, model.WasmPluginTypeNetwork)

	var filters []*listener.Filter
	// Metadata exchange goes first, so RBAC failures, etc can access the state. See https://github.com/istio/istio/issues/41066
	if !cc.hbone {
		filters = append(filters, buildMetadataExchangeNetworkFilters()...)
	}

	// Authn
	filters = extension.PopAppendNetwork(filters, wasm, extensions.PluginPhase_AUTHN)

	// Authz. Since this is HTTP, we only add WASM network filters -- not TCP RBAC, stats, etc.
	filters = extension.PopAppendNetwork(filters, wasm, extensions.PluginPhase_AUTHZ)
	filters = extension.PopAppendNetwork(filters, wasm, extensions.PluginPhase_STATS)
	filters = extension.PopAppendNetwork(filters, wasm, extensions.PluginPhase_UNSPECIFIED_PHASE)

	h := lb.buildHTTPConnectionManager(httpOpts)
	filters = append(filters, &listener.Filter{
		Name:       wellknown.HTTPConnectionManager,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(h)},
	})
	return filters
}

// buildInboundNetworkFilters generates a TCP proxy network filter on the inbound path
func (lb *ListenerBuilder) buildInboundNetworkFilters(fcc inboundChainConfig) []*listener.Filter {
	statPrefix := fcc.clusterName
	// If stat name is configured, build the stat prefix from configured pattern.
	if len(lb.push.Mesh.InboundClusterStatName) != 0 {
		statPrefix = telemetry.BuildInboundStatPrefix(lb.push.Mesh.InboundClusterStatName, fcc.telemetryMetadata, "", uint32(fcc.port.Port), fcc.port.Name)
	}
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: fcc.clusterName},
		IdleTimeout:      parseDuration(lb.node.Metadata.IdleTimeout),
	}
	tcpFilter := setAccessLogAndBuildTCPFilter(lb.push, lb.node, tcpProxy, istionetworking.ListenerClassSidecarInbound, fcc.policyService)
	networkFilterstack := buildNetworkFiltersStack(fcc.port.Protocol, tcpFilter, statPrefix, fcc.clusterName)
	return lb.buildCompleteNetworkFilters(istionetworking.ListenerClassSidecarInbound, fcc.port.Port, networkFilterstack, true, fcc.policyService)
}
