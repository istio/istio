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
	// TODO: extract these two into istio.io/pkg/proto/{bool.go or types.go or values.go}
	boolTrue = &types.BoolValue{
		Value: true,
	}

	boolFalse = &types.BoolValue{
		Value: false,
	}
)

// TODO: create gateway struct to hold state that we shuttle around in all of the _GatewayFoo(...) methods (the names, the environment, etc)
// then refactor all the methods to hang off that type, remove the redundant "Gateway" in all the method names

func (configgen *ConfigGeneratorImpl) buildGatewayListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {
	// collect workload labels
	workloadInstances, err := env.GetProxyServiceInstances(&node)
	if err != nil {
		log.Errora("Failed to get gateway instances for router ", node.ID, err)
		return nil, err
	}

	var workloadLabels model.LabelsCollection
	for _, w := range workloadInstances {
		workloadLabels = append(workloadLabels, w.Labels)
	}

	gateways := env.Gateways(workloadLabels)
	if len(gateways) == 0 {
		log.Debuga("buildGatewayListeners: no gateways for router", node.ID)
		return []*xdsapi.Listener{}, nil
	}

	merged := model.MergeGateways(gateways...)
	log.Debugf("buildGatewayListeners: gateways after merging: %v", merged)

	errs := &multierror.Error{}
	listeners := make([]*xdsapi.Listener, 0, len(merged.Servers))
	for portNumber, servers := range merged.Servers {
		// TODO: this works because all Servers on the same port use the same protocol due to model.MergeGateways's implementation.
		// When Envoy supports filter chain matching, we'll have to group the ports by number and protocol, so this logic will
		// no longer work.
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
		listenerType := plugin.ModelProtocolToListenerType(protocol)
		switch listenerType {
		case plugin.ListenerTypeHTTP:
			opts.filterChainOpts = createGatewayHTTPFilterChainOpts(env, servers, merged.Names)
		case plugin.ListenerTypeTCP:
			opts.filterChainOpts = createGatewayTCPFilterChainOpts(env, servers, merged.Names)
		default:
			log.Warnf("buildGatewayListeners: unknown listener type %v", listenerType)
			continue
		}

		// one filter chain => 0 or 1 certs => SNI not required
		if len(opts.filterChainOpts) == 1 && opts.filterChainOpts[0].tlsContext != nil {
			opts.filterChainOpts[0].tlsContext.RequireSni = boolFalse
		}

		l := buildListener(opts)
		mutable := &plugin.MutableObjects{
			Listener: l,
			// Note: buildListener creates filter chains but does not populate the filters in the chain; that's what
			// this is for.
			FilterChains: make([]plugin.FilterChain, len(l.FilterChains)),
		}
		for _, p := range configgen.Plugins {
			params := &plugin.InputParams{
				ListenerType:   listenerType,
				Env:            &env,
				Node:           &node,
				ProxyInstances: workloadInstances,
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
	return listeners, nil
}

func createGatewayHTTPFilterChainOpts(
	env model.Environment, servers []*networking.Server, gatewayNames map[string]bool) []*filterChainOpts {

	services, err := env.Services() // cannot panic here because gateways do not rely on services necessarily
	if err != nil {
		log.Errora("Failed to get services from registry")
		return []*filterChainOpts{}
	}

	nameToServiceMap := make(map[model.Hostname]*model.Service, len(services))
	for _, svc := range services {
		nameToServiceMap[svc.Hostname] = svc
	}

	httpListeners := make([]*filterChainOpts, 0, len(servers))
	for i, server := range servers {
		routeCfg := buildGatewayInboundHTTPRouteConfig(env, nameToServiceMap, gatewayNames, server)
		if routeCfg == nil {
			log.Debugf("omitting HTTP listeners for port %d filter chain %d due to no routes", server.Port, i)
			continue
		}
		// if https redirect is set, we need to enable requireTls field in all the virtual hosts
		if server.Tls != nil && server.Tls.HttpsRedirect {
			for i := range routeCfg.VirtualHosts {
				// TODO: should this be set to ALL ?
				routeCfg.VirtualHosts[i].RequireTls = route.VirtualHost_EXTERNAL_ONLY
			}
		}
		o := &filterChainOpts{
			sniHosts:   getSNIHosts(server),
			tlsContext: buildGatewayListenerTLSContext(server),
			httpOpts: &httpListenerOpts{
				routeConfig:      routeCfg,
				rds:              "",
				useRemoteAddress: true,
				direction:        http_conn.EGRESS, // viewed as from gateway to internal
			},
		}
		httpListeners = append(httpListeners, o)
	}
	return httpListeners
}

func buildGatewayListenerTLSContext(server *networking.Server) *auth.DownstreamTlsContext {
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
			ValidationContext: certValidationContext,
			AlpnProtocols:     ListenersALPNProtocols,
		},
		RequireClientCertificate: &types.BoolValue{
			Value: requireClientCert,
		},
		RequireSni: boolTrue,
	}
}

func buildGatewayInboundHTTPRouteConfig(
	env model.Environment,
	svcs map[model.Hostname]*model.Service,
	gateways map[string]bool,
	server *networking.Server) *xdsapi.RouteConfiguration {

	hosts := make(map[model.Hostname]bool)
	for _, host := range server.Hosts {
		hosts[model.Hostname(host)] = true
	}

	port := int(server.Port.Number)
	// TODO: WE DO NOT SUPPORT two gateways on same workload binding to same virtual service
	virtualServices := env.VirtualServices(gateways)
	virtualHosts := make([]route.VirtualHost, 0, len(virtualServices))
	for _, v := range virtualServices {
		vs := v.Spec.(*networking.VirtualService)
		if !hostMatch(vs.Hosts, hosts) {
			log.Debugf("omitting virtual service %q because its hosts don't match gateways %v server %d", v.Name, gateways, port)
			continue
		}
		// TODO: we need to filter the hosts this produces by those exposed on the gateway. This impl exposes the full
		// set of hosts of the virtual service, rather than just the subset that is listed on this server.
		routes, err := istio_route.TranslateRoutes(v, svcs, port, nil, gateways)
		if err != nil {
			log.Debugf("omitting routes for service %v due to error: %v", v, err)
			continue
		}
		domains := vs.Hosts
		host := route.VirtualHost{
			Name:    fmt.Sprintf("%s:%d", v.Name, port),
			Domains: domains,
			Routes:  routes,
		}
		virtualHosts = append(virtualHosts, host)
	}

	if len(virtualHosts) == 0 {
		log.Debugf("constructed http route config for port %d with no vhosts; Setting up a default 404 vhost", port)
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
	return &xdsapi.RouteConfiguration{
		Name:             fmt.Sprintf("%d", port),
		VirtualHosts:     virtualHosts,
		ValidateClusters: boolFalse,
	}
}

func createGatewayTCPFilterChainOpts(
	env model.Environment, servers []*networking.Server, gatewayNames map[string]bool) []*filterChainOpts {

	opts := make([]*filterChainOpts, 0, len(servers))
	for _, server := range servers {
		opts = append(opts, &filterChainOpts{
			sniHosts:       getSNIHosts(server),
			tlsContext:     buildGatewayListenerTLSContext(server),
			networkFilters: buildGatewayNetworkFilters(env, server, gatewayNames),
		})
	}
	return opts
}

// buildGatewayNetworkFilters retrieves all VirtualServices bound to the set of Gateways for this workload, filters
// them by this server's port and hostnames, and produces network filters for each destination from the filtered services
func buildGatewayNetworkFilters(env model.Environment, server *networking.Server, gatewayNames map[string]bool) []listener.Filter {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: model.ParseProtocol(server.Port.Protocol),
	}

	dests := getVirtualServiceTCPDestinations(env, server, gatewayNames)
	// de-dupe destinations by hostname; we'll take a random destination if multiple claim the same host
	byHost := make(map[model.Hostname]*networking.Destination, len(dests))
	for _, dest := range dests {
		byHost[model.Hostname(dest.Host)] = dest
	}

	filters := make([]listener.Filter, 0, len(byHost))
	for host, dest := range byHost {
		upstream, err := env.GetService(host)
		if err != nil || upstream == nil {
			log.Debugf("failed to retrieve service for destination %q: %v", host, err)
			continue
		}
		filters = append(filters, buildOutboundNetworkFilters(
			istio_route.GetDestinationCluster(dest, upstream, int(server.Port.Number)),
			[]string{upstream.Address}, port)...)
	}
	return filters
}

// getVirtualServiceTCPDestinations filters virtual services by gateway names, then determines if any match the (TCP) server
// TODO: move up to more general location so this can be re-used in sidecars
func getVirtualServiceTCPDestinations(env model.Environment, server *networking.Server, gateways map[string]bool) []*networking.Destination {
	hosts := make(map[model.Hostname]bool, len(server.Hosts))
	for _, host := range server.Hosts {
		hosts[model.Hostname(host)] = true
	}

	virtualServices := env.VirtualServices(gateways)
	downstreams := make([]*networking.Destination, 0, len(virtualServices))
	for _, spec := range virtualServices {
		vsvc := spec.Spec.(*networking.VirtualService)
		if !hostMatch(vsvc.Hosts, hosts) {
			// the VirtualService's hosts don't include hosts advertised by server
			continue
		}

		// hosts match, now we ensure we satisfy the rule's l4 match conditions, if any exist
		for _, tcp := range vsvc.Tcp {
			if l4Match(tcp.Match, server, gateways) {
				downstreams = append(downstreams, gatherDestinations(tcp.Route)...)
			}
		}
	}
	return downstreams
}

func hostMatch(hosts []string, serviceHosts map[model.Hostname]bool) bool {
	for _, host := range hosts {
		for h := range serviceHosts {
			if h.Matches(model.Hostname(host)) {
				return true
			}
		}
	}
	return false
}

// TODO: move up to more general location so this can be re-used in other service matching
func l4Match(predicates []*networking.L4MatchAttributes, server *networking.Server, gatewayNames map[string]bool) bool {
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
				gatewayMatch = gatewayMatch || gatewayNames[gateway]
			}
		}

		if portMatch && gatewayMatch {
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

func getSNIHosts(server *networking.Server) []string {
	if server.Tls == nil {
		return nil
	}
	return server.Hosts
}
