// Copyright 2018 Istio Authors
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
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

var (
	// TODO: extract this into istio.io/pkg/proto/{bool.go or types.go or values.go}
	boolFalse = &types.BoolValue{
		Value: false,
	}
)

func (configgen *ConfigGeneratorImpl) buildGatewayListeners(env *model.Environment, node *model.Proxy, push *model.PushStatus) ([]*xdsapi.Listener, error) {
	// collect workload labels
	workloadInstances, err := env.GetProxyServiceInstances(node)
	if err != nil {
		log.Errora("Failed to get gateway instances for router ", node.ID, err)
		return nil, err
	}

	var workloadLabels model.LabelsCollection
	for _, w := range workloadInstances {
		workloadLabels = append(workloadLabels, w.Labels)
	}

	gatewaysForWorkload := env.Gateways(workloadLabels)
	if len(gatewaysForWorkload) == 0 {
		log.Debuga("buildGatewayListeners: no gateways for router", node.ID)
		return []*xdsapi.Listener{}, nil
	}

	mergedGateway := model.MergeGateways(gatewaysForWorkload...)
	log.Debugf("buildGatewayListeners: gateways after merging: %v", mergedGateway)

	errs := &multierror.Error{}
	listeners := make([]*xdsapi.Listener, 0, len(mergedGateway.Servers))
	for portNumber, servers := range mergedGateway.Servers {
		protocol := model.ParseProtocol(servers[0].Port.Protocol)
		if protocol == model.ProtocolHTTPS {
			// Gateway terminates TLS connection if TLS mode is not Passthrough So, its effectively a H2 listener.
			// This is complicated. We have multiple servers. One of these servers could have passthrough HTTPS while
			// others could be a simple/mutual TLS.
			// The code as it is, is not capable of handling this mixed listener type and set up the proper SNI chains
			// such that the passthrough ones go through a TCP proxy while others get terminated and go through http connection
			// manager. Ideally, the merge gateway function should take care of this and intelligently create multiple
			// groups of servers based on their TLS types as well. For now, we simply assume that if HTTPS,
			// and the first server in the group is not a passthrough, then this is a HTTP connection manager.
			if servers[0].Tls != nil && servers[0].Tls.Mode != networking.Server_TLSOptions_PASSTHROUGH {
				protocol = model.ProtocolHTTP2
			}
		}

		opts := buildListenerOpts{
			env:        env,
			proxy:      node,
			ip:         WildcardAddress,
			port:       int(portNumber),
			bindToPort: true,
			protocol:   protocol,
		}
		listenerType := plugin.ModelProtocolToListenerProtocol(protocol)
		switch listenerType {
		case plugin.ListenerProtocolHTTP:
			// virtualService.HTTP applies here for both plain text HTTP and HTTPS termination
			opts.filterChainOpts = configgen.createGatewayHTTPFilterChainOpts(node, env, push, servers, mergedGateway.Names)
		case plugin.ListenerProtocolTCP:
			// virtualService.TLS/virtualService.TCP applies here
			opts.filterChainOpts = configgen.createGatewayTCPFilterChainOpts(node, env, push, servers, mergedGateway.Names)
		default:
			log.Warnf("buildGatewayListeners: unknown listener type %v", listenerType)
			continue
		}

		l := buildListener(opts)
		mutable := &plugin.MutableObjects{
			Listener: l,
			// Note: buildListener creates filter chains but does not populate the filters in the chain; that's what
			// this is for.
			FilterChains: make([]plugin.FilterChain, len(l.FilterChains)),
		}

		var si *model.ServiceInstance
		for _, w := range workloadInstances {
			if w.Endpoint.Port == int(portNumber) {
				si = w
				break
			}
		}

		for _, p := range configgen.Plugins {
			params := &plugin.InputParams{
				ListenerProtocol: listenerType,
				Env:              env,
				Node:             node,
				ProxyInstances:   workloadInstances,
				ServiceInstance:  si,
				Port: &model.Port{
					Name:     servers[0].Port.Name,
					Port:     int(portNumber),
					Protocol: protocol,
				},
			}
			if err = p.OnOutboundListener(params, mutable); err != nil {
				log.Warna("buildGatewayListeners: failed to build listener for gateway: ", err.Error())
			}
		}

		// Filters are serialized one time into an opaque struct once we have the complete list.
		if err = marshalFilters(mutable.Listener, opts, mutable.FilterChains); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("gateway omitting listener %q due to: %v", mutable.Listener.Name, err.Error()))
			continue
		}

		if err = mutable.Listener.Validate(); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("gateway listener %s validation failed: %v", mutable.Listener.Name, err.Error()))
			continue
		}

		if log.DebugEnabled() {
			log.Debugf("buildGatewayListeners: constructed listener with %d filter chains:\n%v",
				len(mutable.Listener.FilterChains), mutable.Listener)
		}
		listeners = append(listeners, mutable.Listener)
	}
	// We'll try to return any listeners we successfully marshaled; if we have none, we'll emit the error we built up
	err = errs.ErrorOrNil()
	if len(listeners) == 0 {
		log.Errorf("buildGatewayListeners: Have zero listeners: %v", err.Error())
		return []*xdsapi.Listener{}, nil
	}

	if err != nil {
		// we have some listeners to return, but we also have some errors; log them
		log.Info(err.Error())
	}

	validatedListeners := make([]*xdsapi.Listener, 0, len(mergedGateway.Servers))
	for _, l := range listeners {
		if err := l.Validate(); err != nil {
			log.Warnf("buildGatewayListeners: error validating listener %s: %v.. Skipping.", l.Name, err)
			continue
		}
		validatedListeners = append(validatedListeners, l)
	}

	return validatedListeners, nil
}

func (configgen *ConfigGeneratorImpl) buildGatewayHTTPRouteConfig(env *model.Environment, node *model.Proxy, push *model.PushStatus,
	proxyInstances []*model.ServiceInstance, services []*model.Service, routeName string) (*xdsapi.RouteConfiguration, error) {

	// collect workload labels
	var workloadLabels model.LabelsCollection
	for _, w := range proxyInstances {
		workloadLabels = append(workloadLabels, w.Labels)
	}

	gateways := env.Gateways(workloadLabels)
	if len(gateways) == 0 {
		log.Debuga("buildGatewayRoutes: no gateways for router", node.ID)
		return nil, nil
	}

	merged := model.MergeGateways(gateways...)
	log.Debugf("buildGatewayRoutes: gateways after merging: %v", merged)

	// make sure that there is some server listening on this port
	if _, ok := merged.RDSRouteConfigNames[routeName]; !ok {
		log.Errorf("buildGatewayRoutes: could not find server for routeName %s, have %v", routeName, merged.RDSRouteConfigNames)
		return nil, fmt.Errorf("buildGatewayRoutes: could not find server for routeName %s, have %v", routeName, merged.RDSRouteConfigNames)
	}

	servers := merged.RDSRouteConfigNames[routeName]

	nameToServiceMap := make(map[model.Hostname]*model.Service, len(services))
	for _, svc := range services {
		nameToServiceMap[svc.Hostname] = svc
	}

	gatewayHosts := make(map[model.Hostname]bool)
	tlsRedirect := make(map[model.Hostname]bool)

	for _, server := range servers {
		for _, host := range server.Hosts {
			gatewayHosts[model.Hostname(host)] = true
			if server.Tls != nil && server.Tls.HttpsRedirect {
				tlsRedirect[model.Hostname(host)] = true
			}
		}
	}

	port := int(servers[0].Port.Number)
	// NOTE: WE DO NOT SUPPORT two gateways on same workload binding to same virtual service
	virtualServices := env.VirtualServices(merged.Names)
	virtualHosts := make([]route.VirtualHost, 0, len(virtualServices))
	vhostDomains := map[string]bool{}

	for _, v := range virtualServices {
		vs := v.Spec.(*networking.VirtualService)
		matchingHosts := pickMatchingGatewayHosts(gatewayHosts, vs.Hosts)
		if len(matchingHosts) == 0 {
			log.Infof("%s omitting virtual service %q because its hosts  don't match gateways %v server %d", node.ID, v.Name, gateways, port)
			continue
		}
		routes, err := istio_route.BuildHTTPRoutesForVirtualService(node, v, nameToServiceMap, port, nil, merged.Names, env.IstioConfigStore)
		if err != nil {
			log.Warnf("%s omitting routes for service %v due to error: %v", node.ID, v, err)
			continue
		}

		for vsvcHost, gatewayHost := range matchingHosts {
			_, f := vhostDomains[vsvcHost]
			if f {
				// RDS would reject this, resulting in all vhosts rejection.
				push.Add(model.DuplicatedDomains, vsvcHost, node,
					fmt.Sprintf("%s duplicate domain %s for %s", node.ID, vsvcHost, v.Name))
				continue
			}
			vhostDomains[vsvcHost] = true
			host := route.VirtualHost{
				Name:    fmt.Sprintf("%s:%d", v.Name, port),
				Domains: []string{vsvcHost},
				Routes:  routes,
			}

			if tlsRedirect[gatewayHost] {
				host.RequireTls = route.VirtualHost_ALL
			}
			virtualHosts = append(virtualHosts, host)
		}
	}

	if len(virtualHosts) == 0 {
		log.Warnf("constructed http route config for port %d with no vhosts; Setting up a default 404 vhost", port)
		virtualHosts = append(virtualHosts, route.VirtualHost{
			Name:    fmt.Sprintf("blackhole:%d", port),
			Domains: []string{"*"},
			Routes: []route.Route{
				{
					Match: route.RouteMatch{
						PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &route.Route_DirectResponse{
						DirectResponse: &route.DirectResponseAction{
							Status: 404,
						},
					},
				},
			},
		})
	}
	util.SortVirtualHosts(virtualHosts)

	routeCfg := &xdsapi.RouteConfiguration{
		Name:             routeName,
		VirtualHosts:     virtualHosts,
		ValidateClusters: boolFalse,
	}
	// call plugins
	for _, p := range configgen.Plugins {
		in := &plugin.InputParams{
			ListenerProtocol: plugin.ListenerProtocolHTTP,
			Env:              env,
			Node:             node,
		}
		p.OnOutboundRouteConfiguration(in, routeCfg)
	}

	return routeCfg, nil
}

// to process HTTP and HTTPS servers along with virtualService.HTTP rules
func (configgen *ConfigGeneratorImpl) createGatewayHTTPFilterChainOpts(
	node *model.Proxy, env *model.Environment, push *model.PushStatus, servers []*networking.Server, gatewaysForWorkload map[string]bool) []*filterChainOpts {

	httpListeners := make([]*filterChainOpts, 0, len(servers))
	// Are we processing plaintext servers or HTTPS servers?
	// If plain text, we have to combine all servers into a single listener
	if model.ParseProtocol(servers[0].Port.Protocol).IsHTTP() {
		rdsName := model.GatewayRDSRouteName(servers[0])
		o := &filterChainOpts{
			// This works because we validate that only HTTPS servers can have same port but still different port names
			// and that no two non-HTTPS servers can be on same port or share port names.
			// Validation is done per gateway and also during merging
			sniHosts:   nil,
			tlsContext: nil,
			httpOpts: &httpListenerOpts{
				rds:              rdsName,
				useRemoteAddress: true,
				direction:        http_conn.EGRESS, // viewed as from gateway to internal
			},
		}
		httpListeners = append(httpListeners, o)
	} else {
		// Build a filter chain for each HTTPS server
		// We know that this is a HTTPS server because this function is called only for ports of type HTTP/HTTPS
		// where HTTPS server's TLS mode is not passthrough and not nil
		for _, server := range servers {
			o := &filterChainOpts{
				// This works because we validate that only HTTPS servers can have same port but still different port names
				// and that no two non-HTTPS servers can be on same port or share port names.
				// Validation is done per gateway and also during merging
				sniHosts:   getSNIHostsForServer(server),
				tlsContext: buildGatewayListenerTLSContext(server),
				httpOpts: &httpListenerOpts{
					rds:              model.GatewayRDSRouteName(server),
					useRemoteAddress: true,
					direction:        http_conn.EGRESS, // viewed as from gateway to internal
				},
			}
			httpListeners = append(httpListeners, o)
		}
	}

	return httpListeners
}

func buildGatewayListenerTLSContext(server *networking.Server) *auth.DownstreamTlsContext {
	// Server.TLS cannot be nil or passthrough. But as a safety guard, return nil
	if server.Tls == nil || server.Tls.Mode == networking.Server_TLSOptions_PASSTHROUGH {
		return nil // We don't need to setup TLS context for passthrough mode
	}

	var certValidationContext *auth.CertificateValidationContext
	var trustedCa *core.DataSource
	if len(server.Tls.CaCertificates) != 0 {
		trustedCa = &core.DataSource{
			Specifier: &core.DataSource_Filename{
				Filename: server.Tls.CaCertificates,
			},
		}
	}
	if trustedCa != nil || len(server.Tls.SubjectAltNames) > 0 {
		certValidationContext = &auth.CertificateValidationContext{
			TrustedCa:            trustedCa,
			VerifySubjectAltName: server.Tls.SubjectAltNames,
		}
	}

	requireClientCert := server.Tls.Mode == networking.Server_TLSOptions_MUTUAL

	return &auth.DownstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{
			TlsCertificates: []*auth.TlsCertificate{
				{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: server.Tls.ServerCertificate,
						},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: server.Tls.PrivateKey,
						},
					},
				},
			},
			ValidationContextType: &auth.CommonTlsContext_ValidationContext{
				ValidationContext: certValidationContext,
			},
			AlpnProtocols: ListenersALPNProtocols,
		},
		RequireClientCertificate: &types.BoolValue{
			Value: requireClientCert,
		},
	}
}

func (configgen *ConfigGeneratorImpl) createGatewayTCPFilterChainOpts(
	node *model.Proxy, env *model.Environment, push *model.PushStatus, servers []*networking.Server,
	gatewaysForWorkload map[string]bool) []*filterChainOpts {

	opts := make([]*filterChainOpts, 0, len(servers))
	for _, server := range servers {
		// We have a TCP/TLS server. This could be TLS termination (user specifies server.TLS with simple/mutual)
		// or opaque TCP (server.TLS is nil). or it could be a TLS passthrough with SNI based routing.
		// Handle the TLS termination or opaque TCP first.

		// This is opaque TCP server. Find matching virtual services with TCP blocks and forward
		if server.Tls == nil {
			if filters := buildGatewayNetworkFiltersFromTCPRoutes(node, env, server, gatewaysForWorkload); len(filters) > 0 {
				opts = append(opts, &filterChainOpts{
					sniHosts:       nil,
					tlsContext:     nil,
					networkFilters: filters,
				})
			}
		} else if server.Tls.Mode != networking.Server_TLSOptions_PASSTHROUGH {
			// TCP with TLS termination and forwarding. Setup TLS context to terminate, find matching services with TCP blocks
			// and forward to backend
			// Validation ensures that non-passthrough servers will have certs
			if filters := buildGatewayNetworkFiltersFromTCPRoutes(node, env, server, gatewaysForWorkload); len(filters) > 0 {
				opts = append(opts, &filterChainOpts{
					sniHosts:       getSNIHostsForServer(server),
					tlsContext:     buildGatewayListenerTLSContext(server),
					networkFilters: filters,
				})
			}
		} else {
			// Passthrough server.
			// Get "sets" of SNI matches based on virtual services with TLS blocks and the server's hosts
			// setup a filter chain for each such set. We talk about sets here because the server's host could
			// specify a wildcard like *.com, while the virtual services could be splitting this up into
			// two pieces like foo.com goes to service1, and bar.com goes to service2. In this case, we need
			// two filter chains, one for each set defined by the virtual services.

			// Get the list of SNI matches for this server. Each match is made of one or more hosts.
			sniMatchBlocks := getSNIMatchesFromVirtualServices(env, server, gatewaysForWorkload)

			// Build a filter chain for each SNI host set, where the target cluster is decided by the virtual
			// service that refers to this list of SNI values.
			for _, sniMatchBlock := range sniMatchBlocks {
				if filters := buildGatewayNetworkFiltersFromTLSRoutes(node, env, server, gatewaysForWorkload, sniMatchBlock); len(filters) > 0 {
					opts = append(opts, &filterChainOpts{
						sniHosts:       sniMatchBlock,
						tlsContext:     nil, // NO TLS context because this is passthrough
						networkFilters: filters,
					})
				}
			}
		}
	}
	return opts
}

// buildGatewayNetworkFiltersFromTLSRoutes builds tcp proxy routes for all VirtualServices with TCP blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services
func buildGatewayNetworkFiltersFromTCPRoutes(node *model.Proxy, env *model.Environment, server *networking.Server,
	gatewaysForWorkload map[string]bool) []listener.Filter {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: model.ParseProtocol(server.Port.Protocol),
	}

	gatewayServerHosts := make(map[model.Hostname]bool, len(server.Hosts))
	for _, host := range server.Hosts {
		gatewayServerHosts[model.Hostname(host)] = true
	}

	virtualServices := env.VirtualServices(gatewaysForWorkload)
	upstreams := make([]*networking.Destination, 0, len(virtualServices))
	for _, spec := range virtualServices {
		vsvc := spec.Spec.(*networking.VirtualService)
		matchingHosts := pickMatchingGatewayHosts(gatewayServerHosts, vsvc.Hosts)
		if len(matchingHosts) == 0 {
			// the VirtualService's hosts don't include hosts advertised by server
			continue
		}

		// ensure we satisfy the rule's l4 match conditions, if any exist
		for _, tcp := range vsvc.Tcp {
			if l4Match(tcp.Match, server, gatewaysForWorkload) {
				upstreams = append(upstreams, gatherDestinations(tcp.Route)...)
			}
		}

		// TODO: detect conflicting upstreams
	}

	// de-dupe destinations by hostname; we'll take a random destination if multiple claim the same host
	byHost := make(map[model.Hostname]*networking.Destination, len(upstreams))
	for _, dest := range upstreams {
		byHost[model.Hostname(dest.Host)] = dest
	}

	filters := make([]listener.Filter, 0, len(byHost))
	for host, dest := range byHost {
		upstream, err := env.GetService(host)
		if err != nil || upstream == nil {
			log.Debugf("failed to retrieve service for destination %q: %v", host, err)
			continue
		}
		filters = append(filters, buildOutboundNetworkFilters(node,
			istio_route.GetDestinationCluster(dest, upstream, int(server.Port.Number)), "", port)...)
	}
	return filters
}

// buildGatewayNetworkFiltersFromTLSRoutes builds tcp proxy routes for all VirtualServices with TLS blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services
func buildGatewayNetworkFiltersFromTLSRoutes(node *model.Proxy, env *model.Environment, server *networking.Server,
	gatewaysForWorkload map[string]bool, sniHostsSet []string) []listener.Filter {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: model.ParseProtocol(server.Port.Protocol),
	}

	gatewayServerHosts := make(map[model.Hostname]bool, len(server.Hosts))
	for _, host := range server.Hosts {
		gatewayServerHosts[model.Hostname(host)] = true
	}

	virtualServices := env.VirtualServices(gatewaysForWorkload)
	upstreams := make([]*networking.Destination, 0, len(virtualServices))
	for _, spec := range virtualServices {
		vsvc := spec.Spec.(*networking.VirtualService)
		matchingHosts := pickMatchingGatewayHosts(gatewayServerHosts, vsvc.Hosts)
		if len(matchingHosts) == 0 {
			// the VirtualService's hosts don't include hosts advertised by server
			continue
		}

		// ensure we satisfy the rule's tls match conditions, if any exist
		for _, tls := range vsvc.Tls {
			if tlsMatch(tls.Match, server, gatewaysForWorkload, sniHostsSet) {
				upstreams = append(upstreams, gatherDestinations(tls.Route)...)
			}
		}

		// TODO: detect conflicting upstreams
	}

	// de-dupe destinations by hostname; we'll take a random destination if multiple claim the same host
	byHost := make(map[model.Hostname]*networking.Destination, len(upstreams))
	for _, dest := range upstreams {
		byHost[model.Hostname(dest.Host)] = dest
	}

	filters := make([]listener.Filter, 0, len(byHost))
	for host, dest := range byHost {
		upstream, err := env.GetService(host)
		if err != nil || upstream == nil {
			log.Debugf("failed to retrieve service for destination %q: %v", host, err)
			continue
		}
		filters = append(filters, buildOutboundNetworkFilters(node,
			istio_route.GetDestinationCluster(dest, upstream, int(server.Port.Number)), "", port)...)
	}
	return filters
}

func pickMatchingGatewayHosts(gatewayServerHosts map[model.Hostname]bool, virtualServiceHosts []string) map[string]model.Hostname {
	matchingHosts := make(map[string]model.Hostname, 0)
	for _, vsvcHost := range virtualServiceHosts {
		for gatewayHost := range gatewayServerHosts {
			if gatewayHost.Matches(model.Hostname(vsvcHost)) {
				matchingHosts[vsvcHost] = gatewayHost
			}
		}
	}
	return matchingHosts
}

func l4Match(predicates []*networking.L4MatchAttributes, server *networking.Server, gatewaysForWorkload map[string]bool) bool {
	// NB from proto definitions: each set of predicates is OR'd together; inside of a predicate all conditions are AND'd.
	// This means we can return as soon as we get any match of an entire predicate.
	for _, match := range predicates {
		// TODO: implement more matches, like CIDR ranges, etc.

		// if there's no port predicate, portMatch is true; otherwise we evaluate the port predicate against the server's port
		portMatch := match.Port == 0
		if match.Port != 0 {
			portMatch = server.Port.Number == match.Port
		}

		// similarly, if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
		gatewayMatch := len(match.Gateways) == 0
		if len(match.Gateways) > 0 {
			for _, gateway := range match.Gateways {
				gatewayMatch = gatewayMatch || gatewaysForWorkload[gateway]
			}
		}

		if portMatch && gatewayMatch {
			return true
		}
	}
	// If we had no predicates we match; otherwise we don't match since we'd have exited at the first match.
	return len(predicates) == 0
}

func tlsMatch(predicates []*networking.TLSMatchAttributes, server *networking.Server, gatewaysForWorkload map[string]bool, sniHostsSet []string) bool {
	gatewayServerHosts := make(map[model.Hostname]bool, len(server.Hosts))
	for _, host := range server.Hosts {
		gatewayServerHosts[model.Hostname(host)] = true
	}

	// NB from proto definitions: each set of predicates is OR'd together; inside of a predicate all conditions are AND'd.
	// This means we can return as soon as we get any match of an entire predicate.
	for _, match := range predicates {
		// TODO: implement more matches, like CIDR ranges, etc.

		// the match's sni hosts includes hosts advertised by server
		sniHostsMatch := sniHostsKey(match.SniHosts) == sniHostsKey(sniHostsSet)

		// if there's no port predicate, portMatch is true; otherwise we evaluate the port predicate against the server's port
		portMatch := match.Port == 0
		if match.Port != 0 {
			portMatch = server.Port.Number == match.Port
		}

		// similarly, if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
		gatewayMatch := len(match.Gateways) == 0
		if len(match.Gateways) > 0 {
			for _, gateway := range match.Gateways {
				gatewayMatch = gatewayMatch || gatewaysForWorkload[gateway]
			}
		}

		if sniHostsMatch && portMatch && gatewayMatch {
			return true
		}
	}
	// If we had no predicates we match; otherwise we don't match since we'd have exited at the first match.
	return len(predicates) == 0
}

func gatherDestinations(weights []*networking.DestinationWeight) []*networking.Destination {
	dests := make([]*networking.Destination, 0, len(weights))
	for _, w := range weights {
		dests = append(dests, w.Destination)
	}
	return dests
}

func getSNIHostsForServer(server *networking.Server) []string {
	if server.Tls == nil {
		return nil
	}
	return server.Hosts
}

func sniHostsKey(sniHosts []string) string {
	sort.Strings(sniHosts)
	return strings.Join(sniHosts, ",")
}

// Collects the set of SNI host sets that a given server is filtered on.
// To illustrate, suppose we have the following configs:
//
// 1) Gateway with a TLS server for hosts a, b, c, d.
// 2) Virtual service for hosts a, b and a TLS block such that TLS connections for ServerName a go to x, and ServerName b go to y.
// 3) Virtual service for hosts c with no TLS block.
//
// This function would return two sets of SNI hosts, a and b, corresponding to the virtual service defined in 2.
// No SNI host sets are generated for c or d, because both are missing either a virtual service or a virtual service
// with a TLS block.
func getSNIMatchesFromVirtualServices(env *model.Environment, server *networking.Server, gatewaysForWorkload map[string]bool) [][]string {
	have := make(map[string]bool)

	gatewayServerHosts := make(map[model.Hostname]bool, len(server.Hosts))
	for _, host := range server.Hosts {
		gatewayServerHosts[model.Hostname(host)] = true
	}

	virtualServices := env.VirtualServices(gatewaysForWorkload)

	out := make([][]string, 0)
	for _, spec := range virtualServices {
		vsvc := spec.Spec.(*networking.VirtualService)

		matchingHosts := pickMatchingGatewayHosts(gatewayServerHosts, vsvc.Hosts)
		if len(matchingHosts) == 0 {
			// the VirtualService's hosts don't include hosts advertised by server
			continue
		}

		for _, tls := range vsvc.Tls {
			for _, match := range tls.Match {
				if matchTLS(match, nil, gatewaysForWorkload, int(server.Port.Number)) {
					key := sniHostsKey(match.SniHosts)
					if !have[key] {
						out = append(out, match.SniHosts)
					}
					have[key] = true
				}
			}
		}
	}

	return out
}
