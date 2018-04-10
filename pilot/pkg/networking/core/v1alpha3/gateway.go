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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pkg/log"
)

func (configgen *ConfigGeneratorImpl) buildGatewayListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {
	config := env.IstioConfigStore

	var gateways []model.Config

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

	gateways = config.Gateways(workloadLabels)

	if len(gateways) == 0 {
		log.Debuga("no gateways for router", node.ID)
		return []*xdsapi.Listener{}, nil
	}

	// TODO: merging makes code simpler but we lose gateway names that are needed to determine
	// the virtual services pinned to each gateway
	//gateway := &networking.Gateway{}
	//for _, spec := range gateways {
	//	err := model.MergeGateways(gateway, spec.Spec.(*networking.Gateway))
	//	if err != nil {
	//		log.Errorf("Failed to merge gateway %s for router %s", spec.Name, node.ID)
	//		return nil, fmt.Errorf("merge gateways: %s", err)
	//	}
	//}

	// HACK for the above case
	if len(gateways) > 1 {
		log.Warn("Currently, Istio cannot bind multiple gateways to the same workload")
		return []*xdsapi.Listener{}, nil
	}

	name := gateways[0].Name
	gateway := gateways[0].Spec.(*networking.Gateway)

	listeners := make([]*xdsapi.Listener, 0, len(gateway.Servers))
	listenerPortMap := make(map[uint32]bool)
	for _, server := range gateway.Servers {

		// TODO: this does not handle the case where there are two servers on same port
		if listenerPortMap[server.Port.Number] {
			log.Warnf("Multiple servers on same port is not supported yet, port %d", server.Port.Number)
			continue
		}
		var opts buildListenerOpts
		var listenerType plugin.ListenerType
		var networkFilters []listener.Filter
		switch model.Protocol(server.Port.Protocol) {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC, model.ProtocolHTTPS:
			listenerType = plugin.ListenerTypeHTTP
			opts = buildListenerOpts{
				env:            env,
				proxy:          node,
				proxyInstances: nil, // only required to support deprecated mixerclient behavior
				ip:             WildcardAddress,
				port:           int(server.Port.Number),
				protocol:       model.ProtocolHTTP,
				sniHosts:       server.Hosts,
				tlsContext:     buildGatewayListenerTLSContext(server),
				bindToPort:     true,
				httpOpts: &httpListenerOpts{
					routeConfig:      buildGatewayInboundHTTPRouteConfig(env, name, server),
					rds:              "",
					useRemoteAddress: true,
					direction:        http_conn.EGRESS, // viewed as from gateway to internal
				},
			}

			// if https redirect is set, we need to enable requireTls field in all the virtual hosts
			if server.Tls != nil && server.Tls.HttpsRedirect {
				vhosts := opts.httpOpts.routeConfig.VirtualHosts
				for _, v := range vhosts {
					// TODO: should this be set to ALL ?
					v.RequireTls = route.VirtualHost_EXTERNAL_ONLY
				}
			}
		case model.ProtocolTCP, model.ProtocolMongo:
			listenerType = plugin.ListenerTypeTCP
			opts = buildListenerOpts{
				env:        env,
				proxy:      node,
				ip:         WildcardAddress,
				port:       int(server.Port.Number),
				protocol:   model.ProtocolTCP,
				sniHosts:   server.Hosts,
				tlsContext: buildGatewayListenerTLSContext(server),
				bindToPort: true,
			}
			networkFilters = buildGatewayNetworkFilters(env, server, []string{name})
		}
		newListener := buildListener(opts)

		var httpFilters []*http_conn.HttpFilter
		for _, p := range configgen.Plugins {
			params := &plugin.InputParams{
				ListenerType: listenerType,
				Env:          &env,
				Node:         &node,
			}
			mutable := &plugin.MutableObjects{
				Listener:    newListener,
				TCPFilters:  networkFilters,
				HTTPFilters: httpFilters,
			}
			if err := p.OnOutboundListener(params, mutable); err != nil {
				log.Warn(err.Error())
			}
		}

		// Filters are serialized one time into an opaque struct once we have the complete list.
		if err := marshalFilters(newListener, opts, networkFilters, httpFilters); err != nil {
			log.Warn(err.Error())
		}
		listeners = append(listeners, newListener)
	}
	return listeners, nil
}

func buildGatewayListenerTLSContext(server *networking.Server) *auth.DownstreamTlsContext {
	if server.Tls == nil {
		return nil
	}

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
			ValidationContext: &auth.CertificateValidationContext{
				TrustedCa: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: server.Tls.CaCertificates,
					},
				},
				VerifySubjectAltName: server.Tls.SubjectAltNames,
			},
			AlpnProtocols: ListenersALPNProtocols,
		},
		// For ingress, or if only one cert is defined we should not require SNI (would
		// break compat with not-so-old devices, IoT, etc)
		// TODO: Need config option to enable SNI
		//RequireSni: &types.BoolValue{
		//	Value: true, // is that OKAY?
		//},
	}
}

func buildGatewayInboundHTTPRouteConfig(env model.Environment, gatewayName string, server *networking.Server) *xdsapi.RouteConfiguration {
	// TODO WE DO NOT SUPPORT two gateways on same workload binding to same virtual service
	virtualServices := env.VirtualServices([]string{gatewayName})

	services, err := env.Services() // cannot panic here because gateways do not rely on services necessarily
	if err != nil {
		log.Errora("Failed to get services from registry")
		return nil
	}

	nameToServiceMap := make(map[string]*model.Service)
	for _, svc := range services {
		nameToServiceMap[svc.Hostname] = svc
	}

	nameF := istio_route.ConvertDestinationToCluster(nameToServiceMap, int(server.Port.Number))

	virtualHosts := make([]route.VirtualHost, 0)
	for _, v := range virtualServices {
		routes := istio_route.TranslateRoutes(v, nameF, int(server.Port.Number), nil, gatewayName)
		domains := v.Spec.(*networking.VirtualService).Hosts

		virtualHosts = append(virtualHosts, route.VirtualHost{
			Name:    fmt.Sprintf("%s:%d", v.Name, server.Port.Number),
			Domains: domains,
			Routes:  routes,
		})
	}

	return &xdsapi.RouteConfiguration{
		Name:         fmt.Sprintf("%d", server.Port.Number),
		VirtualHosts: virtualHosts,
	}
}

// buildGatewayInboundNetworkFilters retrieves all VirtualServices bound to the set of Gateways for this workload, filters
// them by this server's port and hostnames, and produces network filters for each destination from the filtered services
func buildGatewayNetworkFilters(env model.Environment, server *networking.Server, gatewayNames []string) []listener.Filter {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: model.ConvertCaseInsensitiveStringToProtocol(server.Port.Protocol),
	}

	dests := filterTCPDownstreams(env, server, gatewayNames)
	// de-dupe destinations by hostname; we'll take a random destination if multiple claim the same host
	byHost := make(map[string]*networking.Destination, len(dests))
	for _, dest := range dests {
		local := dest
		byHost[dest.Host] = local
	}

	filters := make([]listener.Filter, 0, len(byHost))
	for host, dest := range byHost {
		upstream, err := env.GetService(host)
		if err != nil {
			log.Debugf("failed to retrieve service for destination %q: %v", host, err)
			continue
		}
		filters = append(filters, buildOutboundNetworkFilters(destToClusterName(dest), []string{upstream.Address}, port)...)
	}
	return filters
}

// filterTCPDownstreams filters virtual services by gateway names, then determines if any match the (TCP) server
// TODO: move up to more general location so this can be re-used in sidecars
func filterTCPDownstreams(env model.Environment, server *networking.Server, gatewayNames []string) []*networking.Destination {
	hosts := make(map[string]bool, len(server.Hosts))
	for _, host := range server.Hosts {
		hosts[host] = true
	}

	gateways := make(map[string]bool, len(gatewayNames))
	for _, gateway := range gatewayNames {
		gateways[gateway] = true
	}

	virtualServices := env.VirtualServices(gatewayNames)
	downstreams := make([]*networking.Destination, 0, len(virtualServices))
	for _, spec := range virtualServices {
		vsvc := spec.Spec.(*networking.VirtualService)

		// TODO: real wildcard based matching; does code to do that not exist already?
		match := false
		for _, host := range vsvc.Hosts {
			match = match || hosts[host]
		}
		if !match {
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

// TODO: move up to more general location so this can be re-used in other service matching
func l4Match(predicates []*networking.L4MatchAttributes, server *networking.Server, gatewayNames map[string]bool) bool {
	// NB from proto definitions: each set of predicates is OR'd together; inside of a predicate all conditions are AND'd.
	// This means we can return as soon as we get any match of an entire predicate.
	for _, match := range predicates {
		// if there's no port predicate, portMatch is true; otherwise we evaluate the port predicate against the server's port
		portMatch := match.Port == nil
		if match.Port != nil {
			switch p := match.Port.Port.(type) {
			case *networking.PortSelector_Name:
				portMatch = server.Port.Name == p.Name
			case *networking.PortSelector_Number:
				portMatch = server.Port.Number == p.Number
			}
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

// TODO: move up to more general location so this can be re-used
func destToClusterName(d *networking.Destination) string {
	return model.BuildSubsetKey(model.TrafficDirectionOutbound, d.Subset, d.Host, &model.Port{Name: d.Port.GetName()})
}
