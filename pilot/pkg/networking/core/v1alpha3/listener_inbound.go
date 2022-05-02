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
	"sort"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	envoytype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes/duration"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/pkg/log"
)

// inboundChainConfig defines the configuration for a single inbound filter chain. This may be created
// as a result of a Service, and Sidecar CR, or for the built-in passthrough filter chains.
type inboundChainConfig struct {
	// clusterName defines the destination cluster for this chain
	clusterName string
	// port defines the port configuration for this chain. Note that there is a Port and TargetPort;
	// most usages should just use TargetPort. Port is mostly used for legacy compatibility and
	// telemetry.
	port ServiceInstancePort
	// bind determines where (IP) this filter chain should bind. Note: typically we just end up using
	// 'virtual' listener and do not literally bind to port; in these cases this just impacts naming
	// and telemetry.
	bind string

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

	// telemetryMetadata defines additional information about the chain for telemetry purposes.
	telemetryMetadata telemetry.FilterChainMetadata
}

// StatPrefix returns the stat prefix for the config
func (cc inboundChainConfig) StatPrefix() string {
	if cc.passthrough {
		// A bit arbitrary, but for backwards compatibility just use the cluster name
		return cc.clusterName
	}
	return "inbound_" + cc.Name(istionetworking.ListenerProtocolHTTP)
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

var (
	IPv4PassthroughCIDR = []*core.CidrRange{util.ConvertAddressToCidr("0.0.0.0/0")}
	IPv6PassthroughCIDR = []*core.CidrRange{util.ConvertAddressToCidr("::/0")}
)

// ToFilterChainMatch builds the FilterChainMatch for the config
func (cc inboundChainConfig) ToFilterChainMatch(opt FilterChainMatchOptions) *listener.FilterChainMatch {
	match := &listener.FilterChainMatch{}
	match.ApplicationProtocols = opt.ApplicationProtocols
	match.TransportProtocol = opt.TransportProtocol
	if cc.passthrough {
		// Pasthrough listeners do an IP match - but matching all IPs. This is really an IP *version* match,
		// but Envoy doesn't explicitly have version check.
		if cc.clusterName == util.InboundPassthroughClusterIpv4 {
			match.PrefixRanges = IPv4PassthroughCIDR
		} else {
			match.PrefixRanges = IPv6PassthroughCIDR
		}
	}
	if cc.port.TargetPort > 0 {
		match.DestinationPort = &wrappers.UInt32Value{Value: cc.port.TargetPort}
	}
	return match
}

// ServiceInstancePort defines a port that has both a port and targetPort (which distinguishes it from model.Port)
// Note: ServiceInstancePort only makes sense in the context of a specific ServiceInstance, because TargetPort depends on a specific instance.
type ServiceInstancePort struct {
	// Name ascribes a human readable name for the port object. When a
	// service has multiple ports, the name field is mandatory
	Name string `json:"name,omitempty"`

	// Port number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service.
	Port uint32 `json:"port"`

	TargetPort uint32 `json:"targetPort"`

	// Protocol to be used for the port.
	Protocol protocol.Instance `json:"protocol,omitempty"`
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
		// Chain has explicit user TLS config. This can only apply when the mTLS settings are DISABLE to avoid conflicts
		if cc.tlsSettings != nil && mtls.Mode == model.MTLSDisable {
			// Since we are terminating TLS, we need to treat the protocol as if its terminated.
			// Example: user specifies protocol=HTTPS and user TLS, we will use HTTP
			cc.port.Protocol = cc.port.Protocol.AfterTLSTermination()
			lp := istionetworking.ModelProtocolToListenerProtocol(cc.port.Protocol, core.TrafficDirection_INBOUND)
			opts = getTLSFilterChainMatchOptions(lp)
			mtls.TCP = BuildListenerTLSContext(cc.tlsSettings, lb.node, istionetworking.TransportProtocolTCP)
			mtls.HTTP = mtls.TCP
		} else {
			lp := istionetworking.ModelProtocolToListenerProtocol(cc.port.Protocol, core.TrafficDirection_INBOUND)
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
	actualWildcard, _ := getActualWildcardAndLocalHost(lb.node)

	// Build the "virtual" inbound listener. This will capture all inbound redirected traffic and contains:
	// * Passthrough filter chains, matching all unmatched traffic. There are a few of these to handle all cases
	// * Service filter chains. These will either be for each Port exposed by a Service OR Sidecar.Ingress configuration.
	allChains := buildInboundPassthroughChains(lb)
	allChains = append(allChains, chains...)
	return lb.buildInboundListener(model.VirtualInboundListenerName, util.BuildAddress(actualWildcard, ProxyInboundListenPort), false, allChains)
}

// inboundCustomListener build a custom listener that actually binds to a port, rather than relying on redirection.
func (lb *ListenerBuilder) inboundCustomListener(cc inboundChainConfig, chains []*listener.FilterChain) *listener.Listener {
	return lb.buildInboundListener(cc.Name(istionetworking.ListenerProtocolTCP), util.BuildAddress(cc.bind, cc.port.TargetPort), true, chains)
}

func (lb *ListenerBuilder) buildInboundListener(name string, address *core.Address, bindToPort bool, chains []*listener.FilterChain) *listener.Listener {
	l := &listener.Listener{
		Name:             name,
		Address:          address,
		TrafficDirection: core.TrafficDirection_INBOUND,
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

	accessLogBuilder.setListenerAccessLog(lb.push, lb.node, l, istionetworking.ListenerClassSidecarInbound)
	l.FilterChains = chains
	l.ListenerFilters = populateListenerFilters(lb.node, l, bindToPort)
	l.ListenerFiltersTimeout = getProtocolDetectionTimeout(lb.push.Mesh)
	l.ContinueOnListenerFiltersTimeout = true
	return l
}

// inboundChainForOpts builds a set of filter chains
func (lb *ListenerBuilder) inboundChainForOpts(cc inboundChainConfig, mtls plugin.MTLSSettings, opts []FilterChainMatchOptions) []*listener.FilterChain {
	chains := make([]*listener.FilterChain, 0, len(opts))
	for _, opt := range opts {
		switch opt.Protocol {
		// Switch on the protocol. Note: we do not need to handle Auto protocol as it will already be split into a TCP and HTTP option.
		case istionetworking.ListenerProtocolHTTP:
			chains = append(chains, &listener.FilterChain{
				FilterChainMatch: cc.ToFilterChainMatch(opt),
				Filters:          lb.buildInboundNetworkFiltersForHTTP(cc),
				TransportSocket:  buildDownstreamTLSTransportSocket(opt.ToTransportSocket(mtls)),
				Name:             cc.Name(opt.Protocol),
			})
		case istionetworking.ListenerProtocolTCP:
			chains = append(chains, &listener.FilterChain{
				FilterChainMatch: cc.ToFilterChainMatch(opt),
				Filters:          lb.buildInboundNetworkFilters(cc),
				TransportSocket:  buildDownstreamTLSTransportSocket(opt.ToTransportSocket(mtls)),
				Name:             cc.Name(opt.Protocol),
			})
		}
	}
	return chains
}

// buildInboundChainConfigs builds all the application chain configs.
func (lb *ListenerBuilder) buildInboundChainConfigs() []inboundChainConfig {
	chainsByPort := make(map[uint32]inboundChainConfig)
	// No user supplied sidecar scope or the user supplied one has no ingress listeners.
	if !lb.node.SidecarScope.HasIngressListener() {
		// We will look at all Services that apply to this proxy and build chains for each distinct port.
		// Note: this does mean that we may have multiple Services applying to the same port, which introduces a conflict
		for _, i := range lb.node.ServiceInstances {
			port := ServiceInstancePort{
				Name:       i.ServicePort.Name,
				Port:       uint32(i.ServicePort.Port),
				TargetPort: i.Endpoint.EndpointPort,
				Protocol:   i.ServicePort.Protocol,
			}

			cc := inboundChainConfig{
				telemetryMetadata: telemetry.FilterChainMetadata{InstanceHostname: i.Service.Hostname},
				port:              port,
				clusterName:       model.BuildInboundSubsetKey(int(port.TargetPort)),
				bind:              "0.0.0.0", // TODO ipv6
				bindToPort:        getBindToPort(networking.CaptureMode_DEFAULT, lb.node),
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
	} else {
		for _, i := range lb.node.SidecarScope.Sidecar.Ingress {
			port := ServiceInstancePort{
				Name:       i.Port.Name,
				Port:       i.Port.Number,
				TargetPort: i.Port.Number, // No targetPort support in the API
				Protocol:   protocol.Parse(i.Port.Protocol),
			}
			bindtoPort := getBindToPort(i.CaptureMode, lb.node)
			// Skip ports we cannot bind to
			if !lb.node.CanBindToPort(bindtoPort, port.TargetPort) {
				log.Warnf("buildInboundListeners: skipping privileged sidecar port %d for node %s as it is an unprivileged proxy",
					i.Port.Number, lb.node.ID)
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
			}
			if cc.bind == "" {
				// If user didn't provide, pick one based on IP
				cc.bind = getSidecarInboundBindIP(lb.node)
			}
			// If there is a conflict, we will use the oldest Service. This impacts the protocol used as well.
			if old, f := chainsByPort[port.TargetPort]; f {
				reportInboundConflict(lb, old, cc)
				continue
			}

			if i.Tls != nil && features.EnableTLSOnSidecarIngress {
				// User provided custom TLS settings
				cc.tlsSettings = i.Tls.DeepCopy()
				cc.tlsSettings.CipherSuites = filteredSidecarCipherSuites(cc.tlsSettings.CipherSuites)
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

func getProtocolDetectionTimeout(mesh *meshconfig.MeshConfig) *duration.Duration {
	timeout := mesh.GetProtocolDetectionTimeout()
	if features.InboundProtocolDetectionTimeoutSet {
		timeout = durationpb.New(features.InboundProtocolDetectionTimeout)
	}
	return timeout
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
	if features.EnableProtocolSniffingForInbound && needsHTTP(inspectors) {
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
		Name:       wellknown.TlsInspector,
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
		Name:       wellknown.HttpInspector,
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

// buildInboundPassthroughChains builds the passthrough chains. These match any unmatched traffic.
// This allows traffic to ports not exposed by any Service, for example.
func buildInboundPassthroughChains(lb *ListenerBuilder) []*listener.FilterChain {
	// ipv4 and ipv6 feature detect
	ipVersions := make([]string, 0, 2)
	if lb.node.SupportsIPv4() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv4)
	}
	if lb.node.SupportsIPv6() {
		ipVersions = append(ipVersions, util.InboundPassthroughClusterIpv6)
	}

	// Setup enough slots for common max size (permissive mode is 5 filter chains). This is not
	// exact, just best effort optimization
	filterChains := make([]*listener.FilterChain, 0, 1+5*len(ipVersions))
	filterChains = append(filterChains, buildInboundBlackhole(lb))

	for _, clusterName := range ipVersions {
		mtlsOptions := lb.authnBuilder.ForPassthrough()
		for _, mtls := range mtlsOptions {
			cc := inboundChainConfig{
				port: ServiceInstancePort{
					Name: model.VirtualInboundListenerName,
					// Port as 0 doesn't completely make sense here, since we get weird tracing decorators like `:0/*`,
					// but this is backwards compatible and there aren't any perfect options.
					Port:       0,
					Protocol:   protocol.Unsupported,
					TargetPort: mtls.Port,
				},
				clusterName: clusterName,
				passthrough: true,
			}
			opts := getFilterChainMatchOptions(mtls, istionetworking.ListenerProtocolAuto)
			filterChains = append(filterChains, lb.inboundChainForOpts(cc, mtls, opts)...)
		}
	}

	return filterChains
}

// buildInboundBlackhole builds a special filter chain for the virtual inbound matching traffic to the port the listener is actually on.
// This avoids a possible loop where traffic sent to this port would continually call itself indefinitely.
func buildInboundBlackhole(lb *ListenerBuilder) *listener.FilterChain {
	var filters []*listener.Filter
	filters = append(filters, buildMetadataExchangeNetworkFilters(istionetworking.ListenerClassSidecarInbound)...)
	filters = append(filters, buildMetricsNetworkFilters(lb.push, lb.node, istionetworking.ListenerClassSidecarInbound)...)
	filters = append(filters, &listener.Filter{
		Name: wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
			StatPrefix:       util.BlackHoleCluster,
			ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
		})},
	})
	return &listener.FilterChain{
		Name: model.VirtualInboundBlackholeFilterChainName,
		FilterChainMatch: &listener.FilterChainMatch{
			DestinationPort: &wrappers.UInt32Value{Value: ProxyInboundListenPort},
		},
		Filters: filters,
	}
}

// buildSidecarInboundHTTPOpts sets up HTTP options for a given chain.
func buildSidecarInboundHTTPOpts(lb *ListenerBuilder, cc inboundChainConfig) *httpListenerOpts {
	httpOpts := &httpListenerOpts{
		routeConfig:      buildSidecarInboundHTTPRouteConfig(lb, cc),
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
	var filters []*listener.Filter
	filters = append(filters, buildMetadataExchangeNetworkFilters(istionetworking.ListenerClassSidecarInbound)...)

	httpOpts := buildSidecarInboundHTTPOpts(lb, cc)
	hcm := lb.buildHTTPConnectionManager(httpOpts)
	filters = append(filters, &listener.Filter{
		Name:       wellknown.HTTPConnectionManager,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(hcm)},
	})
	return filters
}

// buildInboundNetworkFilters generates a TCP proxy network filter on the inbound path
func (lb *ListenerBuilder) buildInboundNetworkFilters(fcc inboundChainConfig) []*listener.Filter {
	statPrefix := fcc.clusterName
	// If stat name is configured, build the stat prefix from configured pattern.
	if len(lb.push.Mesh.InboundClusterStatName) != 0 {
		statPrefix = telemetry.BuildInboundStatPrefix(lb.push.Mesh.InboundClusterStatName, fcc.telemetryMetadata, "", fcc.port.Port, fcc.port.Name)
	}
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: fcc.clusterName},
	}
	idleTimeout, err := time.ParseDuration(lb.node.Metadata.IdleTimeout)
	if err == nil {
		tcpProxy.IdleTimeout = durationpb.New(idleTimeout)
	}
	tcpFilter := setAccessLogAndBuildTCPFilter(lb.push, lb.node, tcpProxy, istionetworking.ListenerClassSidecarInbound)

	var filters []*listener.Filter
	filters = append(filters, buildMetadataExchangeNetworkFilters(istionetworking.ListenerClassSidecarInbound)...)
	filters = append(filters, lb.authzCustomBuilder.BuildTCP()...)
	filters = append(filters, lb.authzBuilder.BuildTCP()...)
	filters = append(filters, buildMetricsNetworkFilters(lb.push, lb.node, istionetworking.ListenerClassSidecarInbound)...)
	filters = append(filters, buildNetworkFiltersStack(fcc.port.Protocol, tcpFilter, statPrefix, fcc.clusterName)...)

	return filters
}
