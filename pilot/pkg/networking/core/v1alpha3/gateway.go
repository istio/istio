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
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/gateway"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/pkg/log"
)

func (configgen *ConfigGeneratorImpl) buildGatewayListeners(builder *ListenerBuilder) *ListenerBuilder {
	if builder.node.MergedGateway == nil {
		log.Debug("buildGatewayListeners: no gateways for router ", builder.node.ID)
		return builder
	}

	mergedGateway := builder.node.MergedGateway
	log.Debugf("buildGatewayListeners: gateways after merging: %v", mergedGateway)

	actualWildcard, _ := getActualWildcardAndLocalHost(builder.node)
	errs := &multierror.Error{
		ErrorFormat: util.MultiErrorFormat(),
	}
	listeners := make([]*listener.Listener, 0)
	proxyConfig := builder.node.Metadata.ProxyConfigOrDefault(builder.push.Mesh.DefaultConfig)
	for port, ms := range mergedGateway.MergedServers {
		servers := ms.Servers
		var si *model.ServiceInstance
		services := make(map[host.Name]struct{}, len(builder.node.ServiceInstances))
		for _, w := range builder.node.ServiceInstances {
			if w.ServicePort.Port == int(port.Number) {
				if si == nil {
					si = w
				}
				services[w.Service.Hostname] = struct{}{}
			}
		}
		if len(services) != 1 {
			log.Warnf("buildGatewayListeners: found %d services on port %d: %v",
				len(services), port.Number, services)
		}
		// if we found a ServiceInstance with matching ServicePort, listen on TargetPort
		if si != nil && si.Endpoint != nil {
			port.Number = si.Endpoint.EndpointPort
		}
		if builder.node.Metadata.UnprivilegedPod != "" && port.Number < 1024 {
			log.Warnf("buildGatewayListeners: skipping privileged gateway port %d for node %s as it is an unprivileged pod",
				port.Number, builder.node.ID)
			continue
		}
		bind := actualWildcard
		if len(port.Bind) > 0 {
			bind = port.Bind
		}

		// on a given port, we can either have plain text HTTP servers or
		// HTTPS/TLS servers with SNI. We cannot have a mix of http and https server on same port.
		opts := buildListenerOpts{
			push:       builder.push,
			proxy:      builder.node,
			bind:       bind,
			port:       &model.Port{Port: int(port.Number)},
			bindToPort: true,
			class:      ListenerClassGateway,
		}

		p := protocol.Parse(port.Protocol)
		listenerProtocol := istionetworking.ModelProtocolToListenerProtocol(p, core.TrafficDirection_OUTBOUND)
		filterChains := make([]istionetworking.FilterChain, 0)
		if p.IsHTTP() {
			// We have a list of HTTP servers on this port. Build a single listener for the server port.
			// We only need to look at the first server in the list as the merge logic
			// ensures that all servers are of same type.
			port := &networking.Port{Number: port.Number, Protocol: port.Protocol}
			opts.filterChainOpts = []*filterChainOpts{configgen.createGatewayHTTPFilterChainOpts(builder.node, port, nil, ms.RouteName, proxyConfig)}
			filterChains = append(filterChains, istionetworking.FilterChain{ListenerProtocol: istionetworking.ListenerProtocolHTTP})
		} else {
			// build http connection manager with TLS context, for HTTPS servers using simple/mutual TLS
			// build listener with tcp proxy, with or without TLS context, for TCP servers
			//   or TLS servers using simple/mutual/passthrough TLS
			//   or HTTPS servers using passthrough TLS
			// This process typically yields multiple filter chain matches (with SNI) [if TLS is used]
			filterChainOpts := make([]*filterChainOpts, 0)

			for _, server := range servers {
				if gateway.IsTLSServer(server) && gateway.IsHTTPServer(server) {
					routeName := mergedGateway.TLSServerInfo[server].RouteName
					// This is a HTTPS server, where we are doing TLS termination. Build a http connection manager with TLS context
					filterChainOpts = append(filterChainOpts, configgen.createGatewayHTTPFilterChainOpts(builder.node, server.Port, server,
						routeName, proxyConfig))
					filterChains = append(filterChains, istionetworking.FilterChain{
						ListenerProtocol:   istionetworking.ListenerProtocolHTTP,
						IstioMutualGateway: server.Tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL,
					})
				} else {
					// passthrough or tcp, yields multiple filter chains
					tcpChainOpts := configgen.createGatewayTCPFilterChainOpts(builder.node, builder.push,
						server, mergedGateway.GatewayNameForServer[server])
					filterChainOpts = append(filterChainOpts, tcpChainOpts...)
					for i := 0; i < len(tcpChainOpts); i++ {
						filterChains = append(filterChains, istionetworking.FilterChain{ListenerProtocol: istionetworking.ListenerProtocolTCP})
					}
				}
			}
			opts.filterChainOpts = filterChainOpts
		}

		l := buildListener(opts, core.TrafficDirection_OUTBOUND)

		mutable := &istionetworking.MutableObjects{
			Listener: l,
			// Note: buildListener creates filter chains but does not populate the filters in the chain; that's what
			// this is for.
			FilterChains: filterChains,
		}

		pluginParams := &plugin.InputParams{
			ListenerProtocol: listenerProtocol,
			Node:             builder.node,
			Push:             builder.push,
			ServiceInstance:  si,
		}
		for _, p := range configgen.Plugins {
			if err := p.OnOutboundListener(pluginParams, mutable); err != nil {
				log.Warn("buildGatewayListeners: failed to build listener for gateway: ", err.Error())
			}
		}

		// Filters are serialized one time into an opaque struct once we have the complete list.
		if err := buildCompleteFilterChain(mutable, opts); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("gateway omitting listener %q due to: %v", mutable.Listener.Name, err.Error()))
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
		log.Warnf("gateway has zero listeners for node %v", builder.node.ID)
		return builder
	}

	builder.gatewayListeners = listeners
	return builder
}

func buildNameToServiceMapForHTTPRoutes(node *model.Proxy, push *model.PushContext,
	virtualService config.Config) map[host.Name]*model.Service {
	vs := virtualService.Spec.(*networking.VirtualService)
	nameToServiceMap := map[host.Name]*model.Service{}

	addService := func(hostname host.Name) {
		if _, exist := nameToServiceMap[hostname]; exist {
			return
		}

		var service *model.Service
		// First, we obtain the service which has the same namespace as virtualService
		s, exist := push.ServiceIndex.HostnameAndNamespace[hostname][virtualService.Namespace]
		if exist {
			// We should check whether the selected service is visible to the proxy node.
			if push.IsServiceVisible(s, node.ConfigNamespace) {
				service = s
			}
		}
		// If we find no service for the namespace of virtualService or the selected service is not visible to the proxy node,
		// we should fallback to pick one service which is visible to the ConfigNamespace of node.
		if service == nil {
			service = push.ServiceForHostname(node, hostname)
		}
		nameToServiceMap[hostname] = service
	}

	for _, httpRoute := range vs.Http {
		if httpRoute.GetMirror() != nil {
			addService(host.Name(httpRoute.GetMirror().GetHost()))
		}

		for _, route := range httpRoute.GetRoute() {
			if route.GetDestination() != nil {
				addService(host.Name(route.GetDestination().GetHost()))
			}
		}
	}

	return nameToServiceMap
}

func (configgen *ConfigGeneratorImpl) buildGatewayHTTPRouteConfig(node *model.Proxy, push *model.PushContext,
	routeName string) *route.RouteConfiguration {
	if node.MergedGateway == nil {
		log.Debug("buildGatewayRoutes: no gateways for router ", node.ID)
		return nil
	}

	merged := node.MergedGateway
	log.Debugf("buildGatewayRoutes: gateways after merging: %v", merged)

	// make sure that there is some server listening on this port
	if _, ok := merged.ServersByRouteName[routeName]; !ok {
		log.Warnf("Gateway missing for route %s. This is normal if gateway was recently deleted.", routeName)

		// This can happen when a gateway has recently been deleted. Envoy will still request route
		// information due to the draining of listeners, so we should not return an error.
		return nil
	}

	servers := merged.ServersByRouteName[routeName]
	port := int(servers[0].Port.Number) // all these servers are for the same routeName, and therefore same port

	gatewayRoutes := make(map[string]map[string][]*route.Route)
	gatewayVirtualServices := make(map[string][]config.Config)
	vHostDedupMap := make(map[host.Name]*route.VirtualHost)
	for _, server := range servers {
		gatewayName := merged.GatewayNameForServer[server]

		var virtualServices []config.Config
		var exists bool

		if virtualServices, exists = gatewayVirtualServices[gatewayName]; !exists {
			virtualServices = push.VirtualServicesForGateway(node, gatewayName)
			gatewayVirtualServices[gatewayName] = virtualServices
		}
		if len(virtualServices) == 0 && server.Tls != nil && server.Tls.HttpsRedirect {
			// this is a plaintext server with TLS redirect enabled. There is no point in processing the
			// virtual services for this server because all traffic is anyway going to get redirected
			// to https. short circuit the route computation by generating a virtual host with no routes
			// but with tls redirect enabled
			for _, hostname := range server.Hosts {
				if vHost, exists := vHostDedupMap[host.Name(hostname)]; exists {
					if server.Tls != nil && server.Tls.HttpsRedirect {
						vHost.RequireTls = route.VirtualHost_ALL
					}
				} else {
					newVHost := &route.VirtualHost{
						Name:                       domainName(hostname, port),
						Domains:                    buildGatewayVirtualHostDomains(hostname, port),
						IncludeRequestAttemptCount: true,
					}
					if server.Tls != nil && server.Tls.HttpsRedirect {
						newVHost.RequireTls = route.VirtualHost_ALL
					}
					vHostDedupMap[host.Name(hostname)] = newVHost
				}
			}
			continue
		}
		for _, virtualService := range virtualServices {
			virtualServiceHosts := host.NewNames(virtualService.Spec.(*networking.VirtualService).Hosts)
			serverHosts := host.NamesForNamespace(server.Hosts, virtualService.Namespace)

			// We have two cases here:
			// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and server hosts are ns/*.foo.com
			// 2. virtualService hosts are *.foo.com, and server hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
			intersectingHosts := serverHosts.Intersection(virtualServiceHosts)
			if len(intersectingHosts) == 0 {
				continue
			}

			// Make sure we can obtain services which are visible to this virtualService as much as possible.
			nameToServiceMap := buildNameToServiceMapForHTTPRoutes(node, push, virtualService)

			var routes []*route.Route
			var exists bool
			var err error
			if _, exists = gatewayRoutes[gatewayName]; !exists {
				gatewayRoutes[gatewayName] = make(map[string][]*route.Route)
			}

			vskey := virtualService.Name + "/" + virtualService.Namespace

			if routes, exists = gatewayRoutes[gatewayName][vskey]; !exists {
				routes, err = istio_route.BuildHTTPRoutesForVirtualService(node, push, virtualService, nameToServiceMap, port, map[string]bool{gatewayName: true})
				if err != nil {
					log.Debugf("%s omitting routes for virtual service %v/%v due to error: %v", node.ID, virtualService.Namespace, virtualService.Name, err)
					continue
				}
				gatewayRoutes[gatewayName][vskey] = routes
			}

			for _, hostname := range intersectingHosts {
				if vHost, exists := vHostDedupMap[hostname]; exists {
					vHost.Routes = append(vHost.Routes, routes...)
					if server.Tls != nil && server.Tls.HttpsRedirect {
						vHost.RequireTls = route.VirtualHost_ALL
					}
				} else {
					newVHost := &route.VirtualHost{
						Name:                       domainName(string(hostname), port),
						Domains:                    buildGatewayVirtualHostDomains(string(hostname), port),
						Routes:                     routes,
						IncludeRequestAttemptCount: true,
					}
					if server.Tls != nil && server.Tls.HttpsRedirect {
						newVHost.RequireTls = route.VirtualHost_ALL
					}
					vHostDedupMap[hostname] = newVHost
				}
			}
		}
	}

	var virtualHosts []*route.VirtualHost
	if len(vHostDedupMap) == 0 {
		log.Warnf("constructed http route config for route %s on port %d with no vhosts; Setting up a default 404 vhost", routeName, port)
		virtualHosts = []*route.VirtualHost{{
			Name:    domainName("blackhole", port),
			Domains: []string{"*"},
			// Empty route list will cause Envoy to 404 NR any requests
			Routes: []*route.Route{},
		}}
	} else {
		virtualHosts = make([]*route.VirtualHost, 0, len(vHostDedupMap))
		for _, v := range vHostDedupMap {
			v.Routes = istio_route.CombineVHostRoutes(v.Routes)
			virtualHosts = append(virtualHosts, v)
		}
	}

	util.SortVirtualHosts(virtualHosts)

	routeCfg := &route.RouteConfiguration{
		// Retain the routeName as its used by EnvoyFilter patching logic
		Name:             routeName,
		VirtualHosts:     virtualHosts,
		ValidateClusters: proto.BoolFalse,
	}

	return routeCfg
}

// builds a HTTP connection manager for servers of type HTTP or HTTPS (mode: simple/mutual)
func (configgen *ConfigGeneratorImpl) createGatewayHTTPFilterChainOpts(node *model.Proxy, port *networking.Port, server *networking.Server,
	routeName string, proxyConfig *meshconfig.ProxyConfig) *filterChainOpts {
	serverProto := protocol.Parse(port.Protocol)

	httpProtoOpts := &core.Http1ProtocolOptions{}

	if features.HTTP10 || node.Metadata.HTTP10 == "1" {
		httpProtoOpts.AcceptHttp_10 = true
	}

	xffNumTrustedHops := uint32(0)
	forwardClientCertDetails := util.MeshConfigToEnvoyForwardClientCertDetails(meshconfig.Topology_SANITIZE_SET)

	if proxyConfig != nil && proxyConfig.GatewayTopology != nil {
		xffNumTrustedHops = proxyConfig.GatewayTopology.NumTrustedProxies
		if proxyConfig.GatewayTopology.ForwardClientCertDetails != meshconfig.Topology_UNDEFINED {
			forwardClientCertDetails = util.MeshConfigToEnvoyForwardClientCertDetails(proxyConfig.GatewayTopology.ForwardClientCertDetails)
		}
	}

	var stripPortMode *hcm.HttpConnectionManager_StripAnyHostPort = nil
	if features.StripHostPort {
		stripPortMode = &hcm.HttpConnectionManager_StripAnyHostPort{StripAnyHostPort: true}
	}

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
				connectionManager: &hcm.HttpConnectionManager{
					XffNumTrustedHops: xffNumTrustedHops,
					// Forward client cert if connection is mTLS
					ForwardClientCertDetails: forwardClientCertDetails,
					SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
						Subject: proto.BoolTrue,
						Cert:    true,
						Uri:     true,
						Dns:     true,
					},
					ServerName:          EnvoyServerName,
					HttpProtocolOptions: httpProtoOpts,
					StripPortMode:       stripPortMode,
				},
				addGRPCWebFilter: serverProto == protocol.GRPCWeb,
			},
		}
	}

	// Build a filter chain for the HTTPS server
	// We know that this is a HTTPS server because this function is called only for ports of type HTTP/HTTPS
	// where HTTPS server's TLS mode is not passthrough and not nil
	return &filterChainOpts{
		// This works because we validate that only HTTPS servers can have same port but still different port names
		// and that no two non-HTTPS servers can be on same port or share port names.
		// Validation is done per gateway and also during merging
		sniHosts:   node.MergedGateway.TLSServerInfo[server].SNIHosts,
		tlsContext: buildGatewayListenerTLSContext(server, node),
		httpOpts: &httpListenerOpts{
			rds:              routeName,
			useRemoteAddress: true,
			connectionManager: &hcm.HttpConnectionManager{
				XffNumTrustedHops: xffNumTrustedHops,
				// Forward client cert if connection is mTLS
				ForwardClientCertDetails: forwardClientCertDetails,
				SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
					Subject: proto.BoolTrue,
					Cert:    true,
					Uri:     true,
					Dns:     true,
				},
				ServerName:          EnvoyServerName,
				HttpProtocolOptions: httpProtoOpts,
				StripPortMode:       stripPortMode,
			},
			addGRPCWebFilter: serverProto == protocol.GRPCWeb,
			statPrefix:       server.Name,
		},
	}
}

// sdsPath: is the path to the mesh-wide workload sds uds path, and it is assumed that if this path is unset, that sds is
// disabled mesh-wide
// metadata: map of miscellaneous configuration values sent from the Envoy instance back to Pilot, could include the field
//
// Below is a table of potential scenarios for the gateway configuration:
//
// TLS mode      | Mesh-wide SDS | Ingress SDS | Resulting Configuration
// SIMPLE/MUTUAL |    ENABLED    |   ENABLED   | support SDS at ingress gateway to terminate SSL communication outside the mesh
// ISTIO_MUTUAL  |    ENABLED    |   DISABLED  | support SDS at gateway to terminate workload mTLS, with internal workloads
// 											   | for egress or with another trusted cluster for ingress)
// ISTIO_MUTUAL  |    DISABLED   |   DISABLED  | use file-mounted secret paths to terminate workload mTLS from gateway
//
// Note that ISTIO_MUTUAL TLS mode and ingressSds should not be used simultaneously on the same ingress gateway.
func buildGatewayListenerTLSContext(
	server *networking.Server, proxy *model.Proxy) *tls.DownstreamTlsContext {
	// Server.TLS cannot be nil or passthrough. But as a safety guard, return nil
	if server.Tls == nil || gateway.IsPassThroughServer(server) {
		return nil // We don't need to setup TLS context for passthrough mode
	}

	ctx := &tls.DownstreamTlsContext{
		CommonTlsContext: &tls.CommonTlsContext{
			AlpnProtocols: util.ALPNHttp,
		},
	}

	ctx.RequireClientCertificate = proto.BoolFalse
	if server.Tls.Mode == networking.ServerTLSSettings_MUTUAL ||
		server.Tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL {
		ctx.RequireClientCertificate = proto.BoolTrue
	}

	switch {
	// If SDS is enabled at gateway, and credential name is specified at gateway config, create
	// SDS config for gateway to fetch key/cert at gateway agent.
	case server.Tls.CredentialName != "":
		authn_model.ApplyCredentialSDSToServerCommonTLSContext(ctx.CommonTlsContext, server.Tls)
	case server.Tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL:
		authn_model.ApplyToCommonTLSContext(ctx.CommonTlsContext, proxy, server.Tls.SubjectAltNames, []string{}, ctx.RequireClientCertificate.Value)
	default:
		certProxy := &model.Proxy{}
		certProxy.IstioVersion = proxy.IstioVersion
		// If certificate files are specified in gateway configuration, use file based SDS.
		certProxy.Metadata = &model.NodeMetadata{
			TLSServerCertChain: server.Tls.ServerCertificate,
			TLSServerKey:       server.Tls.PrivateKey,
			TLSServerRootCert:  server.Tls.CaCertificates,
		}

		authn_model.ApplyToCommonTLSContext(ctx.CommonTlsContext, certProxy, server.Tls.SubjectAltNames, []string{}, ctx.RequireClientCertificate.Value)
	}

	// Set TLS parameters if they are non-default
	if len(server.Tls.CipherSuites) > 0 ||
		server.Tls.MinProtocolVersion != networking.ServerTLSSettings_TLS_AUTO ||
		server.Tls.MaxProtocolVersion != networking.ServerTLSSettings_TLS_AUTO {
		ctx.CommonTlsContext.TlsParams = &tls.TlsParameters{
			TlsMinimumProtocolVersion: convertTLSProtocol(server.Tls.MinProtocolVersion),
			TlsMaximumProtocolVersion: convertTLSProtocol(server.Tls.MaxProtocolVersion),
			CipherSuites:              server.Tls.CipherSuites,
		}
	}

	return ctx
}

func convertTLSProtocol(in networking.ServerTLSSettings_TLSProtocol) tls.TlsParameters_TlsProtocol {
	out := tls.TlsParameters_TlsProtocol(in) // There should be a one-to-one enum mapping
	if out < tls.TlsParameters_TLS_AUTO || out > tls.TlsParameters_TLSv1_3 {
		log.Warnf("was not able to map TLS protocol to Envoy TLS protocol")
		return tls.TlsParameters_TLS_AUTO
	}
	return out
}

func (configgen *ConfigGeneratorImpl) createGatewayTCPFilterChainOpts(
	node *model.Proxy, push *model.PushContext, server *networking.Server,
	gatewayName string) []*filterChainOpts {
	// We have a TCP/TLS server. This could be TLS termination (user specifies server.TLS with simple/mutual)
	// or opaque TCP (server.TLS is nil). or it could be a TLS passthrough with SNI based routing.

	// This is opaque TCP server. Find matching virtual services with TCP blocks and forward
	if server.Tls == nil {
		if filters := buildGatewayNetworkFiltersFromTCPRoutes(node,
			push, server, gatewayName); len(filters) > 0 {
			return []*filterChainOpts{
				{
					sniHosts:       nil,
					tlsContext:     nil,
					networkFilters: filters,
				},
			}
		}
	} else if !gateway.IsPassThroughServer(server) {
		// TCP with TLS termination and forwarding. Setup TLS context to terminate, find matching services with TCP blocks
		// and forward to backend
		// Validation ensures that non-passthrough servers will have certs
		if filters := buildGatewayNetworkFiltersFromTCPRoutes(node,
			push, server, gatewayName); len(filters) > 0 {
			return []*filterChainOpts{
				{
					sniHosts:       node.MergedGateway.TLSServerInfo[server].SNIHosts,
					tlsContext:     buildGatewayListenerTLSContext(server, node),
					networkFilters: filters,
				},
			}
		}
	} else {
		// Passthrough server.
		return buildGatewayNetworkFiltersFromTLSRoutes(node, push, server, gatewayName)
	}

	return []*filterChainOpts{}
}

// buildGatewayNetworkFiltersFromTCPRoutes builds tcp proxy routes for all VirtualServices with TCP blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services.
func buildGatewayNetworkFiltersFromTCPRoutes(node *model.Proxy, push *model.PushContext, server *networking.Server, gateway string) []*listener.Filter {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: protocol.Parse(server.Port.Protocol),
	}

	gatewayServerHosts := make(map[host.Name]bool, len(server.Hosts))
	for _, hostname := range server.Hosts {
		gatewayServerHosts[host.Name(hostname)] = true
	}

	virtualServices := push.VirtualServicesForGateway(node, gateway)
	if len(virtualServices) == 0 {
		log.Warnf("no virtual service bound to gateway: %v", gateway)
	}
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
			if l4MultiMatch(tcp.Match, server, gateway) {
				return buildOutboundNetworkFilters(node, tcp.Route, push, port, v.Meta)
			}
		}
	}

	return nil
}

// buildGatewayNetworkFiltersFromTLSRoutes builds tcp proxy routes for all VirtualServices with TLS blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services
func buildGatewayNetworkFiltersFromTLSRoutes(node *model.Proxy, push *model.PushContext, server *networking.Server,
	gatewayName string) []*filterChainOpts {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: protocol.Parse(server.Port.Protocol),
	}

	gatewayServerHosts := make(map[host.Name]bool, len(server.Hosts))
	for _, hostname := range server.Hosts {
		gatewayServerHosts[host.Name(hostname)] = true
	}

	filterChains := make([]*filterChainOpts, 0)

	if server.Tls.Mode == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
		// auto passthrough does not require virtual services. It sets up envoy.filters.network.sni_cluster filter
		filterChains = append(filterChains, &filterChainOpts{
			sniHosts:       node.MergedGateway.TLSServerInfo[server].SNIHosts,
			tlsContext:     nil, // NO TLS context because this is passthrough
			networkFilters: buildOutboundAutoPassthroughFilterStack(push, node, port),
		})
	} else {
		tlsSniHosts := map[string]struct{}{} // sni host -> exists

		virtualServices := push.VirtualServicesForGateway(node, gatewayName)
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
				for i, match := range tls.Match {
					if l4SingleMatch(convertTLSMatchToL4Match(match), server, gatewayName) {
						// Envoy will reject config that has multiple filter chain matches with the same matching rules
						// To avoid this, we need to make sure we don't have duplicated SNI hosts, which will become
						// SNI filter chain matches
						if duplicateSniHosts := model.CheckDuplicates(match.SniHosts, tlsSniHosts); len(duplicateSniHosts) != 0 {
							log.Debugf(
								"skipping VirtualService %s rule #%v on server port %d of gateway %s, duplicate SNI host names: %v",
								v.Meta.Name, i, port.Port, gatewayName, duplicateSniHosts)
							model.RecordRejectedConfig(gatewayName)
							continue
						}

						// the sni hosts in the match will become part of a filter chain match
						filterChains = append(filterChains, &filterChainOpts{
							sniHosts:       match.SniHosts,
							tlsContext:     nil, // NO TLS context because this is passthrough
							networkFilters: buildOutboundNetworkFilters(node, tls.Route, push, port, v.Meta),
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
func pickMatchingGatewayHosts(gatewayServerHosts map[host.Name]bool, virtualService config.Config) map[string]host.Name {
	matchingHosts := make(map[string]host.Name)
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
				// strip the namespace
				gwHostnameForMatching = host.Name(parts[1])
			}
			if gwHostnameForMatching.Matches(host.Name(vsvcHost)) {
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
		SourceLabels:       tlsMatch.SourceLabels,
		Gateways:           tlsMatch.Gateways,
		SourceNamespace:    tlsMatch.SourceNamespace,
	}
}

func l4MultiMatch(predicates []*networking.L4MatchAttributes, server *networking.Server, gateway string) bool {
	// NB from proto definitions: each set of predicates is OR'd together; inside of a predicate all conditions are AND'd.
	// This means we can return as soon as we get any match of an entire predicate.
	for _, match := range predicates {
		if l4SingleMatch(match, server, gateway) {
			return true
		}
	}
	// If we had no predicates we match; otherwise we don't match since we'd have exited at the first match.
	return len(predicates) == 0
}

func l4SingleMatch(match *networking.L4MatchAttributes, server *networking.Server, gateway string) bool {
	// if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
	return isPortMatch(match.Port, server) && isGatewayMatch(gateway, match.Gateways)
}

func isPortMatch(port uint32, server *networking.Server) bool {
	// if there's no port predicate, portMatch is true; otherwise we evaluate the port predicate against the server's port
	portMatch := port == 0
	if port != 0 {
		portMatch = server.Port.Number == port
	}
	return portMatch
}

func isGatewayMatch(gateway string, gatewayNames []string) bool {
	// if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
	if len(gatewayNames) == 0 {
		return true
	}
	if len(gatewayNames) > 0 {
		for _, gatewayName := range gatewayNames {
			if gatewayName == gateway {
				return true
			}
		}
	}
	return false
}

func buildGatewayVirtualHostDomains(hostname string, port int) []string {
	domains := []string{hostname}
	if features.StripHostPort || hostname == "*" {
		return domains
	}

	// Per https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#config-route-v3-virtualhost
	// we can only have one wildcard. Ideally, we want to match any port, as the host
	// header may have a different port (behind a LB, nodeport, etc). However, if we
	// have a wildcard domain we cannot do that since we would need two wildcards.
	// Therefore, we we will preserve the original port if there is a wildcard host.
	// TODO(https://github.com/envoyproxy/envoy/issues/12647) support wildcard host with wildcard port.
	if len(hostname) > 0 && hostname[0] == '*' {
		domains = append(domains, hostname+":"+strconv.Itoa(port))
	} else {
		domains = append(domains, hostname+":*")
	}
	return domains
}
