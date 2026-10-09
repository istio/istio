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
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	istio_route "istio.io/istio/pilot/pkg/networking/core/route"
	"istio.io/istio/pilot/pkg/networking/plugin/authz"
	"istio.io/istio/pilot/pkg/networking/util"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/util/sets"
)

// gatewayWildcardTLSTarget is a wildcard DYNAMIC_DNS destination of an ISTIO_MUTUAL gateway server. As on a
// waypoint, the real host is only visible on an internal listener, after the gateway terminates the mTLS.
type gatewayWildcardTLSTarget struct {
	service *model.Service
	// port is the upstream port of the destination.
	port int
	// dfpCluster is the dynamic forward proxy cluster of the destination.
	dfpCluster string
	// subset is the route destination subset.
	subset string
}

// name is the name of the internal listener, and of the cluster that sends to it.
func (t gatewayWildcardTLSTarget) name() string {
	return model.BuildSubsetKey(model.TrafficDirectionOutboundWildcardTLS, t.subset, t.service.Hostname, t.port)
}

// gatewayWildcardDestination returns the wildcard DYNAMIC_DNS service and upstream port when wildcard TLS is
// enabled and the routes send to a single such service. Weighted routes are not supported, as one DNS cache
// cannot serve several destinations.
func gatewayWildcardDestination(
	node *model.Proxy, routes []*networking.RouteDestination, serverPort int,
) (*model.Service, int) {
	if !features.EnableWildcardHostServiceEntriesForTLS || len(routes) != 1 {
		return nil, 0
	}
	svc := node.SidecarScope.GetService(host.Name(routes[0].Destination.Host))
	if svc == nil || !svc.Hostname.IsWildCarded() || svc.Resolution != model.DynamicDNS {
		return nil, 0
	}
	// Must match the port of the destination cluster.
	return svc, istio_route.GetDestinationPort(routes[0].Destination, svc, serverPort)
}

// gatewayWildcardTLSTargetForServer returns the target of an ISTIO_MUTUAL TLS server whose routes send to a
// wildcard DYNAMIC_DNS service.
func gatewayWildcardTLSTargetForServer(
	node *model.Proxy, server *networking.Server, routes []*networking.RouteDestination,
) (gatewayWildcardTLSTarget, bool) {
	if server.GetTls().GetMode() != networking.ServerTLSSettings_ISTIO_MUTUAL || !protocol.Parse(server.GetPort().GetProtocol()).IsTLS() {
		return gatewayWildcardTLSTarget{}, false
	}
	svc, port := gatewayWildcardDestination(node, routes, int(server.GetPort().GetNumber()))
	if svc == nil {
		return gatewayWildcardTLSTarget{}, false
	}
	return gatewayWildcardTLSTarget{
		service:    svc,
		port:       port,
		dfpCluster: istio_route.GetDestinationCluster(routes[0].Destination, svc, int(server.GetPort().GetNumber())),
		subset:     routes[0].Destination.GetSubset(),
	}, true
}

// gatewayWildcardTLSTargets returns the distinct targets of all servers of the gateway. Listeners and clusters
// are both built from this list, so they always agree.
func gatewayWildcardTLSTargets(node *model.Proxy) []gatewayWildcardTLSTarget {
	mg := node.MergedGateway
	if !features.EnableWildcardHostServiceEntriesForTLS || mg == nil {
		return nil
	}
	var targets []gatewayWildcardTLSTarget
	seen := sets.New[string]()
	for _, port := range mg.ServerPorts {
		merged := mg.MergedServers[port]
		if merged == nil {
			continue
		}
		for _, server := range merged.Servers {
			if server.GetTls().GetMode() != networking.ServerTLSSettings_ISTIO_MUTUAL {
				continue
			}
			routes, _, ok := gatewayTCPRoutes(node, server, mg.GatewayNameForServer[server])
			if !ok {
				continue
			}
			target, ok := gatewayWildcardTLSTargetForServer(node, server, routes)
			if !ok || seen.InsertContains(target.dfpCluster) {
				continue
			}
			targets = append(targets, target)
		}
	}
	return targets
}

// buildGatewayWildcardTLSFilters builds the network filters of the ISTIO_MUTUAL gateway chain. The chain hands the
// peer identity and original addresses to the internal listener and has no authorization filters: on this chain,
// the SNI is the mesh mTLS SNI, so AuthorizationPolicy is applied on the internal listener instead.
func (lb *ListenerBuilder) buildGatewayWildcardTLSFilters(target gatewayWildcardTLSTarget, port *model.Port) []*listener.Filter {
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       target.dfpCluster,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: target.name()},
		// The internal listener applies the idle timeout, as on waypoints. A timeout here could close the connection
		// before the one configured for the destination.
		IdleTimeout: istio_route.Notimeout,
		// The peer identity is only known once the downstream TLS handshake completes. No application data is
		// read before then, so no early data needs to be buffered.
		UpstreamConnectMode: tcp.UpstreamConnectMode_ON_DOWNSTREAM_TLS_HANDSHAKE,
		MaxEarlyDataBytes:   wrappers.UInt32(0),
	}
	class := model.OutboundListenerClass(lb.node.Type)
	tcpFilter := setAccessLogAndBuildTCPFilter(lb.push, lb.node, tcpProxy, class, nil)
	return lb.buildCompleteNetworkFiltersWithAuthz(class, port.Port,
		[]*listener.Filter{xdsfilters.GatewayDownstreamPeerFilter, tcpFilter}, true, nil, nil, nil)
}

// buildGatewayWildcardTLSInternalListener builds the internal listener of a target. It restores the original
// addresses, reads the SNI of the application TLS, accepts only SNIs under the wildcard host, applies
// AuthorizationPolicy with the peer identity from filter state, and resolves the SNI with the dynamic forward proxy.
func (lb *ListenerBuilder) buildGatewayWildcardTLSInternalListener(target gatewayWildcardTLSTarget) *listener.Listener {
	destinationRule := CastDestinationRule(lb.node.SidecarScope.DestinationRule(
		model.TrafficDirectionOutbound, lb.node, target.service.Hostname).GetRule())
	tcpPool := destinationRule.GetTrafficPolicy().GetConnectionPool().GetTcp()
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:                      target.dfpCluster,
		ClusterSpecifier:                &tcp.TcpProxy_Cluster{Cluster: target.dfpCluster},
		IdleTimeout:                     tcpPool.GetIdleTimeout(),
		MaxDownstreamConnectionDuration: tcpPool.GetMaxConnectionDuration(),
	}
	if tcpProxy.IdleTimeout == nil {
		tcpProxy.IdleTimeout = parseDuration(lb.node.Metadata.IdleTimeout)
	}
	maybeSetHashPolicy(destinationRule, tcpProxy, target.subset)
	tcpFilter := setAccessLogAndBuildTCPFilter(lb.push, lb.node, tcpProxy, istionetworking.ListenerClassGateway, nil)

	var filters []*listener.Filter
	filters = append(filters, authz.NewBuilder(authz.Custom, lb.push, lb.node, true).BuildTCP()...)
	filters = append(filters, authz.NewBuilder(authz.Local, lb.push, lb.node, true).BuildTCP()...)
	filters = append(filters,
		buildSNIDFPFilter(target.port, target.service, util.SelectDNSLookupFamily(lb.node.IPAddresses)),
		tcpFilter)

	return &listener.Listener{
		Name:              target.name(),
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		ListenerFilters:   []*listener.ListenerFilter{xdsfilters.OriginalDestination, xdsfilters.TLSInspector},
		TrafficDirection:  core.TrafficDirection_OUTBOUND,
		// As on sidecars, only SNIs under the wildcard host are forwarded.
		FilterChains: []*listener.FilterChain{{
			Name:             target.name(),
			FilterChainMatch: &listener.FilterChainMatch{ServerNames: []string{string(target.service.Hostname)}},
			Filters:          filters,
		}},
	}
}

// buildGatewayWildcardTLSClusters builds the clusters that send the ISTIO_MUTUAL gateway chains to their internal
// listeners.
func buildGatewayWildcardTLSClusters(node *model.Proxy) []*cluster.Cluster {
	targets := gatewayWildcardTLSTargets(node)
	clusters := make([]*cluster.Cluster, 0, len(targets))
	for _, target := range targets {
		clusters = append(clusters, buildInternalUpstreamCluster(target.name(), target.name(), false))
	}
	return clusters
}
