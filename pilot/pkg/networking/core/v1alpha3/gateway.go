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
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	multierror "github.com/hashicorp/go-multierror"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto"
)

func (configgen *ConfigGeneratorImpl) buildGatewayListeners(env *model.Environment, node *model.Proxy, push *model.PushContext) ([]*xdsapi.Listener, error) {
	// collect workload labels
	workloadInstances := node.ServiceInstances

	var workloadLabels model.LabelsCollection
	for _, w := range workloadInstances {
		workloadLabels = append(workloadLabels, w.Labels)
	}

	gatewaysForWorkload := env.Gateways(workloadLabels)
	if len(gatewaysForWorkload) == 0 {
		log.Debuga("buildGatewayListeners: no gateways for router ", node.ID)
		return []*xdsapi.Listener{}, nil
	}

	mergedGateway := model.MergeGateways(gatewaysForWorkload...)
	log.Debugf("buildGatewayListeners: gateways after merging: %v", mergedGateway)

	errs := &multierror.Error{}
	listeners := make([]*xdsapi.Listener, 0, len(mergedGateway.Servers))
	for portNumber, servers := range mergedGateway.Servers {
		// on a given port, we can either have plain text HTTP servers or
		// HTTPS/TLS servers with SNI. We cannot have a mix of http and https server on same port.
		opts := buildListenerOpts{
			env:        env,
			proxy:      node,
			bind:       WildcardAddress,
			port:       int(portNumber),
			bindToPort: true,
		}

		protocol := model.ParseProtocol(servers[0].Port.Protocol)
		listenerProtocol := plugin.ModelProtocolToListenerProtocol(protocol)
		if protocol.IsHTTP() {
			// We have a list of HTTP servers on this port. Build a single listener for the server port.
			// We only need to look at the first server in the list as the merge logic
			// ensures that all servers are of same type.
			routeName := mergedGateway.RouteNamesByServer[servers[0]]
			opts.filterChainOpts = []*filterChainOpts{configgen.createGatewayHTTPFilterChainOpts(node, servers[0], routeName)}
		} else {
			// build http connection manager with TLS context, for HTTPS servers using simple/mutual TLS
			// build listener with tcp proxy, with or without TLS context, for TCP servers
			//   or TLS servers using simple/mutual/passthrough TLS
			//   or HTTPS servers using passthrough TLS
			// This process typically yields multiple filter chain matches (with SNI) [if TLS is used]
			filterChainOpts := make([]*filterChainOpts, 0)

			for _, server := range servers {
				if model.IsTLSServer(server) && model.IsHTTPServer(server) {
					// This is a HTTPS server, where we are doing TLS termination. Build a http connection manager with TLS context
					routeName := mergedGateway.RouteNamesByServer[server]
					filterChainOpts = append(filterChainOpts, configgen.createGatewayHTTPFilterChainOpts(node, server, routeName))
				} else {
					// passthrough or tcp, yields multiple filter chains
					filterChainOpts = append(filterChainOpts, configgen.createGatewayTCPFilterChainOpts(node, env, push,
						server, map[string]bool{mergedGateway.GatewayNameForServer[server]: true})...)
				}
			}
			opts.filterChainOpts = filterChainOpts
		}

		l := buildListener(opts)
		mutable := &plugin.MutableObjects{
			Listener: l,
			// Note: buildListener creates filter chains but does not populate the filters in the chain; that's what
			// this is for.
			FilterChains: make([]plugin.FilterChain, len(l.FilterChains)),
		}

		// Begin shady logic
		// buildListener builds an empty array of filters in the listener struct
		// mutable object above has a FilterChains field that has same number of empty structs (matching number of
		// filter chains). All plugins iterate over this array, and fill up the HTTP or TCP part of the
		// plugin.FilterChain struct.
		// TODO: need a cleaner way of communicating this info
		for i := range mutable.FilterChains {
			if opts.filterChainOpts[i].httpOpts != nil {
				mutable.FilterChains[i].ListenerProtocol = plugin.ListenerProtocolHTTP
			} else {
				mutable.FilterChains[i].ListenerProtocol = plugin.ListenerProtocolTCP
			}
		}
		// end shady logic

		var si *model.ServiceInstance
		for _, w := range workloadInstances {
			if w.Endpoint.Port == int(portNumber) {
				si = w
				break
			}
		}

		pluginParams := &plugin.InputParams{
			ListenerProtocol: listenerProtocol,
			ListenerCategory: networking.EnvoyFilter_ListenerMatch_GATEWAY,
			Env:              env,
			Node:             node,
			ProxyInstances:   workloadInstances,
			Push:             push,
			ServiceInstance:  si,
			Port: &model.Port{
				Name:     servers[0].Port.Name,
				Port:     int(portNumber),
				Protocol: protocol,
			},
		}
		for _, p := range configgen.Plugins {
			if err := p.OnOutboundListener(pluginParams, mutable); err != nil {
				log.Warna("buildGatewayListeners: failed to build listener for gateway: ", err.Error())
			}
		}

		// Filters are serialized one time into an opaque struct once we have the complete list.
		if err := buildCompleteFilterChain(pluginParams, mutable, opts); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("gateway omitting listener %q due to: %v", mutable.Listener.Name, err.Error()))
			continue
		}

		if err := mutable.Listener.Validate(); err != nil {
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
	err := errs.ErrorOrNil()
	if err != nil {
		// we have some listeners to return, but we also have some errors; log them
		log.Info(err.Error())
	}

	if len(listeners) == 0 {
		log.Error("buildGatewayListeners: Have zero listeners")
		return []*xdsapi.Listener{}, nil
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

func (configgen *ConfigGeneratorImpl) buildGatewayHTTPRouteConfig(env *model.Environment, node *model.Proxy, push *model.PushContext,
	proxyInstances []*model.ServiceInstance, routeName string) (*xdsapi.RouteConfiguration, error) {

	services := push.Services(node)

	// collect workload labels
	var workloadLabels model.LabelsCollection
	for _, w := range proxyInstances {
		workloadLabels = append(workloadLabels, w.Labels)
	}

	gateways := env.Gateways(workloadLabels)
	if len(gateways) == 0 {
		log.Debuga("buildGatewayRoutes: no gateways for router ", node.ID)
		return nil, nil
	}

	merged := model.MergeGateways(gateways...)
	log.Debugf("buildGatewayRoutes: gateways after merging: %v", merged)

	// make sure that there is some server listening on this port
	if _, ok := merged.ServersByRouteName[routeName]; !ok {
		log.Errorf("buildGatewayRoutes: could not find server for routeName %s, have %v", routeName, merged.ServersByRouteName)
		return nil, fmt.Errorf("buildGatewayRoutes: could not find server for routeName %s, have %v", routeName, merged.ServersByRouteName)
	}

	servers := merged.ServersByRouteName[routeName]
	port := int(servers[0].Port.Number) // all these servers are for the same routeName, and therefore same port

	nameToServiceMap := make(map[model.Hostname]*model.Service, len(services))
	for _, svc := range services {
		nameToServiceMap[svc.Hostname] = svc
	}

	vHostDedupMap := make(map[model.Hostname]*route.VirtualHost)
	for _, server := range servers {
		gatewayName := merged.GatewayNameForServer[server]
		virtualServices := push.VirtualServices(node, map[string]bool{gatewayName: true})
		for _, virtualService := range virtualServices {
			virtualServiceHosts := model.StringsToHostnames(virtualService.Spec.(*networking.VirtualService).Hosts)
			serverHosts := model.HostnamesForNamespace(server.Hosts, virtualService.Namespace)

			// We have two cases here:
			// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and server hosts are ns/*.foo.com
			// 2. virtualService hosts are *.foo.com, and server hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
			intersectingHosts := serverHosts.Intersection(virtualServiceHosts)
			if len(intersectingHosts) == 0 {
				continue
			}

			routes, err := istio_route.BuildHTTPRoutesForVirtualService(node, push, virtualService, nameToServiceMap, port, nil, map[string]bool{gatewayName: true})
			if err != nil {
				log.Debugf("%s omitting routes for service %v due to error: %v", node.ID, virtualService, err)
				continue
			}

			for _, host := range intersectingHosts {
				if vHost, exists := vHostDedupMap[host]; exists {
					vHost.Routes = istio_route.CombineVHostRoutes(vHost.Routes, routes)
				} else {
					newVHost := &route.VirtualHost{
						Name:    fmt.Sprintf("%s:%d", host, port),
						Domains: []string{string(host), fmt.Sprintf("%s:%d", host, port)},
						Routes:  routes,
					}
					if server.Tls != nil && server.Tls.HttpsRedirect {
						newVHost.RequireTls = route.VirtualHost_ALL
					}
					vHostDedupMap[host] = newVHost
				}
			}
		}
	}

	var virtualHosts []route.VirtualHost
	if len(vHostDedupMap) == 0 {
		log.Warnf("constructed http route config for port %d with no vhosts; Setting up a default 404 vhost", port)
		virtualHosts = []route.VirtualHost{route.VirtualHost{
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
		}}
	} else {
		virtualHosts = make([]route.VirtualHost, 0, len(vHostDedupMap))
		for _, v := range vHostDedupMap {
			virtualHosts = append(virtualHosts, *v)
		}
	}

	util.SortVirtualHosts(virtualHosts)

	routeCfg := &xdsapi.RouteConfiguration{
		Name:             routeName,
		VirtualHosts:     virtualHosts,
		ValidateClusters: proto.BoolFalse,
	}
	// call plugins
	for _, p := range configgen.Plugins {
		in := &plugin.InputParams{
			ListenerProtocol: plugin.ListenerProtocolHTTP,
			Env:              env,
			Node:             node,
			Push:             push,
		}
		p.OnOutboundRouteConfiguration(in, routeCfg)
	}

	return routeCfg, nil
}

// builds a HTTP connection manager for servers of type HTTP or HTTPS (mode: simple/mutual)
func (configgen *ConfigGeneratorImpl) createGatewayHTTPFilterChainOpts(
	node *model.Proxy, server *networking.Server, routeName string) *filterChainOpts {

	serverProto := model.ParseProtocol(server.Port.Protocol)
	// Are we processing plaintext servers or HTTPS servers?
	// If plain text, we have to combine all servers into a single listener
	if serverProto.IsHTTP() {
		return &filterChainOpts{
			// This works because we validate that only HTTPS servers can have same port but still different port names
			// and that no two non-HTTPS servers can be on same port or share port names.
			// Validation is done per gateway and also during merging
			sniHosts:   nil,
			tlsContext: nil,
			httpOpts: &httpListenerOpts{
				rds:              routeName,
				useRemoteAddress: true,
				direction:        http_conn.EGRESS, // viewed as from gateway to internal
				connectionManager: &http_conn.HttpConnectionManager{
					// Forward client cert if connection is mTLS
					ForwardClientCertDetails: http_conn.SANITIZE_SET,
					SetCurrentClientCertDetails: &http_conn.HttpConnectionManager_SetCurrentClientCertDetails{
						Subject: proto.BoolTrue,
						Uri:     true,
						Dns:     true,
					},
					ServerName: EnvoyServerName,
				},
			},
		}
	}

	// Build a filter chain for the HTTPS server
	// We know that this is a HTTPS server because this function is called only for ports of type HTTP/HTTPS
	// where HTTPS server's TLS mode is not passthrough and not nil
	enableIngressSdsAgent := false
	// If proxy version is over 1.1, and proxy sends metadata USER_SDS, then create SDS config for
	// gateway listener.
	if enableSds, found := node.Metadata["USER_SDS"]; found && util.IsProxyVersionGE11(node) {
		enableIngressSdsAgent, _ = strconv.ParseBool(enableSds)
	}
	return &filterChainOpts{
		// This works because we validate that only HTTPS servers can have same port but still different port names
		// and that no two non-HTTPS servers can be on same port or share port names.
		// Validation is done per gateway and also during merging
		sniHosts:   getSNIHostsForServer(server),
		tlsContext: buildGatewayListenerTLSContext(server, enableIngressSdsAgent),
		httpOpts: &httpListenerOpts{
			rds:              routeName,
			useRemoteAddress: true,
			direction:        http_conn.EGRESS, // viewed as from gateway to internal
			connectionManager: &http_conn.HttpConnectionManager{
				// Forward client cert if connection is mTLS
				ForwardClientCertDetails: http_conn.SANITIZE_SET,
				SetCurrentClientCertDetails: &http_conn.HttpConnectionManager_SetCurrentClientCertDetails{
					Subject: proto.BoolTrue,
					Uri:     true,
					Dns:     true,
				},
				ServerName: EnvoyServerName,
			},
		},
	}
}

func buildGatewayListenerTLSContext(server *networking.Server, enableSds bool) *auth.DownstreamTlsContext {
	// Server.TLS cannot be nil or passthrough. But as a safety guard, return nil
	if server.Tls == nil || model.IsPassThroughServer(server) {
		return nil // We don't need to setup TLS context for passthrough mode
	}

	tls := &auth.DownstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{
			AlpnProtocols: ListenersALPNProtocols,
		},
	}

	if enableSds && server.Tls.CredentialName != "" {
		// If SDS is enabled at gateway, and credential name is specified at gateway config, create
		// SDS config for gateway to fetch key/cert at gateway agent.
		tls.CommonTlsContext.TlsCertificateSdsSecretConfigs = []*auth.SdsSecretConfig{
			model.ConstructSdsSecretConfigForGatewayListener(server.Tls.CredentialName, model.IngressGatewaySdsUdsPath),
		}
		// If tls mode is MUTUAL, create SDS config for gateway to fetch certificate validation context
		// at gateway agent. Otherwise, use the static certificate validation context config.
		if server.Tls.Mode == networking.Server_TLSOptions_MUTUAL {
			tls.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
				CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
					DefaultValidationContext: &auth.CertificateValidationContext{VerifySubjectAltName: server.Tls.SubjectAltNames},
					ValidationContextSdsSecretConfig: model.ConstructSdsSecretConfigForGatewayListener(
						server.Tls.CredentialName+model.IngressGatewaySdsCaSuffix, model.IngressGatewaySdsUdsPath),
				},
			}
		} else if len(server.Tls.SubjectAltNames) > 0 {
			tls.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{
				ValidationContext: &auth.CertificateValidationContext{
					VerifySubjectAltName: server.Tls.SubjectAltNames,
				},
			}
		}
	} else {
		// Fall back to the read-from-file approach when SDS is not enabled or Tls.CredentialName is not specified.
		tls.CommonTlsContext.TlsCertificates = []*auth.TlsCertificate{
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
		}
		var trustedCa *core.DataSource
		if len(server.Tls.CaCertificates) != 0 {
			trustedCa = &core.DataSource{
				Specifier: &core.DataSource_Filename{
					Filename: server.Tls.CaCertificates,
				},
			}
		}
		if trustedCa != nil || len(server.Tls.SubjectAltNames) > 0 {
			tls.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa:            trustedCa,
					VerifySubjectAltName: server.Tls.SubjectAltNames,
				},
			}
		}
	}

	tls.RequireClientCertificate = proto.BoolFalse
	if server.Tls.Mode == networking.Server_TLSOptions_MUTUAL {
		tls.RequireClientCertificate = proto.BoolTrue
	}

	// Set TLS parameters if they are non-default
	if len(server.Tls.CipherSuites) > 0 ||
		server.Tls.MinProtocolVersion != networking.Server_TLSOptions_TLS_AUTO ||
		server.Tls.MaxProtocolVersion != networking.Server_TLSOptions_TLS_AUTO {

		tls.CommonTlsContext.TlsParams = &auth.TlsParameters{
			TlsMinimumProtocolVersion: convertTLSProtocol(server.Tls.MinProtocolVersion),
			TlsMaximumProtocolVersion: convertTLSProtocol(server.Tls.MaxProtocolVersion),
			CipherSuites:              server.Tls.CipherSuites,
		}
	}

	return tls
}

func convertTLSProtocol(in networking.Server_TLSOptions_TLSProtocol) auth.TlsParameters_TlsProtocol {
	out := auth.TlsParameters_TlsProtocol(in) // There should be a one-to-one enum mapping
	if out < auth.TlsParameters_TLS_AUTO || out > auth.TlsParameters_TLSv1_3 {
		log.Warnf("was not able to map TLS protocol to Envoy TLS protocol")
		return auth.TlsParameters_TLS_AUTO
	}
	return out
}

func (configgen *ConfigGeneratorImpl) createGatewayTCPFilterChainOpts(
	node *model.Proxy, env *model.Environment, push *model.PushContext, server *networking.Server,
	gatewaysForWorkload map[string]bool) []*filterChainOpts {

	// We have a TCP/TLS server. This could be TLS termination (user specifies server.TLS with simple/mutual)
	// or opaque TCP (server.TLS is nil). or it could be a TLS passthrough with SNI based routing.

	// This is opaque TCP server. Find matching virtual services with TCP blocks and forward
	if server.Tls == nil {
		if filters := buildGatewayNetworkFiltersFromTCPRoutes(node, env,
			push, server, gatewaysForWorkload); len(filters) > 0 {
			return []*filterChainOpts{
				{
					sniHosts:       nil,
					tlsContext:     nil,
					networkFilters: filters,
				},
			}
		}
	} else if !model.IsPassThroughServer(server) {
		// TCP with TLS termination and forwarding. Setup TLS context to terminate, find matching services with TCP blocks
		// and forward to backend
		// Validation ensures that non-passthrough servers will have certs
		if filters := buildGatewayNetworkFiltersFromTCPRoutes(node, env,
			push, server, gatewaysForWorkload); len(filters) > 0 {
			enableIngressSdsAgent := false
			// If proxy version is over 1.1, and proxy sends metadata USER_SDS, then create SDS config for
			// gateway listener.
			if enableSds, found := node.Metadata["USER_SDS"]; found && util.IsProxyVersionGE11(node) {
				enableIngressSdsAgent, _ = strconv.ParseBool(enableSds)
			}
			return []*filterChainOpts{
				{
					sniHosts:       getSNIHostsForServer(server),
					tlsContext:     buildGatewayListenerTLSContext(server, enableIngressSdsAgent),
					networkFilters: filters,
				},
			}
		}
	} else {
		// Passthrough server.
		return buildGatewayNetworkFiltersFromTLSRoutes(node, env, push, server, gatewaysForWorkload)
	}

	return []*filterChainOpts{}
}

// buildGatewayNetworkFiltersFromTCPRoutes builds tcp proxy routes for all VirtualServices with TCP blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services.
func buildGatewayNetworkFiltersFromTCPRoutes(node *model.Proxy, env *model.Environment, push *model.PushContext, server *networking.Server,
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

	virtualServices := push.VirtualServices(node, gatewaysForWorkload)
	for _, v := range virtualServices {
		vsvc := v.Spec.(*networking.VirtualService)
		// We have two cases here:
		// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and gateway's hosts are ns/*.foo.com
		// 2. virtualService hosts are *.foo.com, and gateway's hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
		// Since this is TCP, neither matters. We are simply looking for matching virtual service for this gateway
		matchingHosts := pickMatchingGatewayHosts(gatewayServerHosts, v)
		if len(matchingHosts) == 0 {
			// the VirtualService's hosts don't include hosts advertised by server
			continue
		}

		// ensure we satisfy the rule's l4 match conditions, if any exist
		// For the moment, there can be only one match that succeeds
		// based on the match port/server port and the gateway name
		for _, tcp := range vsvc.Tcp {
			if l4MultiMatch(tcp.Match, server, gatewaysForWorkload) {
				return buildOutboundNetworkFilters(env, node, tcp.Route, push, port, v.ConfigMeta)
			}
		}
	}

	return nil
}

// buildGatewayNetworkFiltersFromTLSRoutes builds tcp proxy routes for all VirtualServices with TLS blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services
func buildGatewayNetworkFiltersFromTLSRoutes(node *model.Proxy, env *model.Environment, push *model.PushContext, server *networking.Server,
	gatewaysForWorkload map[string]bool) []*filterChainOpts {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: model.ParseProtocol(server.Port.Protocol),
	}

	gatewayServerHosts := make(map[model.Hostname]bool, len(server.Hosts))
	for _, host := range server.Hosts {
		gatewayServerHosts[model.Hostname(host)] = true
	}

	filterChains := make([]*filterChainOpts, 0)

	if server.Tls.Mode == networking.Server_TLSOptions_AUTO_PASSTHROUGH {
		// auto passthrough does not require virtual services. It sets up envoy.filters.network.sni_cluster filter
		filterChains = append(filterChains, &filterChainOpts{
			sniHosts:       getSNIHostsForServer(server),
			tlsContext:     nil, // NO TLS context because this is passthrough
			networkFilters: buildOutboundAutoPassthroughFilterStack(env, node, port),
		})
	} else {
		virtualServices := push.VirtualServices(node, gatewaysForWorkload)
		for _, v := range virtualServices {
			vsvc := v.Spec.(*networking.VirtualService)
			// We have two cases here:
			// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and gateway's hosts are ns/*.foo.com
			// 2. virtualService hosts are *.foo.com, and gateway's hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
			// The code below only handles 1.
			// TODO: handle case 2
			matchingHosts := pickMatchingGatewayHosts(gatewayServerHosts, v)
			if len(matchingHosts) == 0 {
				// the VirtualService's hosts don't include hosts advertised by server
				continue
			}

			// For every matching TLS block, generate a filter chain with sni match
			// TODO: Bug..if there is a single virtual service with *.foo.com, and multiple TLS block
			// matches, one for 1.foo.com, another for 2.foo.com, this code will produce duplicate filter
			// chain matches
			for _, tls := range vsvc.Tls {
				for _, match := range tls.Match {
					if l4SingleMatch(convertTLSMatchToL4Match(match), server, gatewaysForWorkload) {
						// the sni hosts in the match will become part of a filter chain match
						filterChains = append(filterChains, &filterChainOpts{
							sniHosts:       match.SniHosts,
							tlsContext:     nil, // NO TLS context because this is passthrough
							networkFilters: buildOutboundNetworkFilters(env, node, tls.Route, push, port, v.ConfigMeta),
						})
					}
				}
			}
		}
	}

	return filterChains
}

// Select the virtualService's hosts that match the ones specified in the gateway server's hosts
// based on the wildcard hostname match and the namespace match
func pickMatchingGatewayHosts(gatewayServerHosts map[model.Hostname]bool, virtualService model.Config) map[string]model.Hostname {
	matchingHosts := make(map[string]model.Hostname, 0)
	virtualServiceHosts := virtualService.Spec.(*networking.VirtualService).Hosts
	for _, vsvcHost := range virtualServiceHosts {
		for gatewayHost := range gatewayServerHosts {
			gwHostnameForMatching := gatewayHost
			if strings.Contains(string(gwHostnameForMatching), "/") {
				// match the namespace first
				// gateway merging code ensures that we only have ns/host
				// and no ./* or */host
				parts := strings.Split(string(gwHostnameForMatching), "/")
				if parts[0] != virtualService.Namespace {
					continue
				}
				//strip the namespace
				gwHostnameForMatching = model.Hostname(parts[1])
			}
			if gwHostnameForMatching.Matches(model.Hostname(vsvcHost)) {
				// assign the actual gateway host because calling code uses it as a key
				// to locate TLS redirect servers
				matchingHosts[vsvcHost] = gatewayHost
			}
		}
	}
	return matchingHosts
}

func convertTLSMatchToL4Match(tlsMatch *networking.TLSMatchAttributes) *networking.L4MatchAttributes {
	return &networking.L4MatchAttributes{
		DestinationSubnets: tlsMatch.DestinationSubnets,
		Port:               tlsMatch.Port,
		SourceSubnet:       tlsMatch.SourceSubnet,
		SourceLabels:       tlsMatch.SourceLabels,
		Gateways:           tlsMatch.Gateways,
	}
}

func l4MultiMatch(predicates []*networking.L4MatchAttributes, server *networking.Server, gatewaysForWorkload map[string]bool) bool {
	// NB from proto definitions: each set of predicates is OR'd together; inside of a predicate all conditions are AND'd.
	// This means we can return as soon as we get any match of an entire predicate.
	for _, match := range predicates {
		if l4SingleMatch(match, server, gatewaysForWorkload) {
			return true
		}
	}
	// If we had no predicates we match; otherwise we don't match since we'd have exited at the first match.
	return len(predicates) == 0
}

func l4SingleMatch(match *networking.L4MatchAttributes, server *networking.Server, gatewaysForWorkload map[string]bool) bool {
	// if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
	return isPortMatch(match.Port, server) && isGatewayMatch(gatewaysForWorkload, match.Gateways)
}

func isPortMatch(port uint32, server *networking.Server) bool {
	// if there's no port predicate, portMatch is true; otherwise we evaluate the port predicate against the server's port
	portMatch := port == 0
	if port != 0 {
		portMatch = server.Port.Number == port
	}
	return portMatch
}

func isGatewayMatch(gatewaysForWorkload map[string]bool, gateways []string) bool {
	// if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
	gatewayMatch := len(gateways) == 0
	if len(gateways) > 0 {
		for _, gateway := range gateways {
			gatewayMatch = gatewayMatch || gatewaysForWorkload[gateway]
		}
	}
	return gatewayMatch
}

func getSNIHostsForServer(server *networking.Server) []string {
	if server.Tls == nil {
		return nil
	}
	// sanitize the server hosts as it could contain hosts of form ns/host
	sniHosts := make([]string, 0)
	for _, h := range server.Hosts {
		if strings.Contains(h, "/") {
			parts := strings.Split(h, "/")
			sniHosts = append(sniHosts, parts[1])
		} else {
			sniHosts = append(sniHosts, h)
		}
	}

	return sniHosts
}
