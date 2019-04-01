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
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/features/pilot"
	"istio.io/istio/pkg/proto"
)

// BuildHTTPRoutes produces a list of routes for the proxy
func (configgen *ConfigGeneratorImpl) BuildHTTPRoutes(env *model.Environment, node *model.Proxy, push *model.PushContext,
	routeName string) (*xdsapi.RouteConfiguration, error) {
	// TODO: Move all this out
	proxyInstances := node.ServiceInstances
	switch node.Type {
	case model.SidecarProxy:
		return configgen.buildSidecarOutboundHTTPRouteConfig(env, node, push, proxyInstances, routeName), nil
	case model.Router, model.Ingress:
		return configgen.buildGatewayHTTPRouteConfig(env, node, push, proxyInstances, routeName)
	}
	return nil, nil
}

// buildSidecarInboundHTTPRouteConfig builds the route config with a single wildcard virtual host on the inbound path
// TODO: trace decorators, inbound timeouts
func (configgen *ConfigGeneratorImpl) buildSidecarInboundHTTPRouteConfig(env *model.Environment,
	node *model.Proxy, push *model.PushContext, instance *model.ServiceInstance) *xdsapi.RouteConfiguration {

	// In case of unix domain sockets, the service port will be 0. So use the port name to distinguish the
	// inbound listeners that a user specifies in Sidecar. Otherwise, all inbound clusters will be the same.
	// We use the port name as the subset in the inbound cluster for differentiation. Its fine to use port
	// names here because the inbound clusters are not referred to anywhere in the API, unlike the outbound
	// clusters and these are static endpoint clusters used only for sidecar (proxy -> app)
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, instance.Endpoint.ServicePort.Name,
		instance.Service.Hostname, instance.Endpoint.ServicePort.Port)
	traceOperation := fmt.Sprintf("%s:%d/*", instance.Service.Hostname, instance.Endpoint.ServicePort.Port)
	defaultRoute := istio_route.BuildDefaultHTTPInboundRoute(clusterName, traceOperation)

	inboundVHost := route.VirtualHost{
		Name:    fmt.Sprintf("%s|http|%d", model.TrafficDirectionInbound, instance.Endpoint.ServicePort.Port),
		Domains: []string{"*"},
		Routes:  []route.Route{*defaultRoute},
	}

	r := &xdsapi.RouteConfiguration{
		Name:             clusterName,
		VirtualHosts:     []route.VirtualHost{inboundVHost},
		ValidateClusters: proto.BoolFalse,
	}

	for _, p := range configgen.Plugins {
		in := &plugin.InputParams{
			ListenerProtocol: plugin.ListenerProtocolHTTP,
			Env:              env,
			Node:             node,
			ServiceInstance:  instance,
			Service:          instance.Service,
			Push:             push,
		}
		p.OnInboundRouteConfiguration(in, r)
	}

	return r
}

// buildSidecarOutboundHTTPRouteConfig builds an outbound HTTP Route for sidecar.
// Based on port, will determine all virtual hosts that listen on the port.
func (configgen *ConfigGeneratorImpl) buildSidecarOutboundHTTPRouteConfig(env *model.Environment, node *model.Proxy, push *model.PushContext,
	proxyInstances []*model.ServiceInstance, routeName string) *xdsapi.RouteConfiguration {

	listenerPort := 0
	var err error
	listenerPort, err = strconv.Atoi(routeName)
	if err != nil {
		// we have a port whose name is http_proxy or unix:///foo/bar
		// check for both.
		if routeName != RDSHttpProxy && !strings.HasPrefix(routeName, model.UnixAddressPrefix) {
			return nil
		}
	}

	var virtualServices []model.Config
	var services []*model.Service

	// Get the list of services that correspond to this egressListener from the sidecarScope
	sidecarScope := node.SidecarScope
	// sidecarScope should never be nil
	if sidecarScope != nil && sidecarScope.Config != nil {
		// this is a user supplied sidecar scope. Get the services from the egress listener
		egressListener := sidecarScope.GetEgressListenerForRDS(listenerPort, routeName)
		// We should never be getting a nil egress listener because the code that setup this RDS
		// call obviously saw an egress listener
		if egressListener == nil {
			return nil
		}

		services = egressListener.Services()
		// To maintain correctness, we should only use the virtualservices for
		// this listener and not all virtual services accessible to this proxy.
		virtualServices = egressListener.VirtualServices()

		// When generating RDS for ports created via the SidecarScope, we treat
		// these ports as HTTP proxy style ports. All services attached to this listener
		// must feature in this RDS route irrespective of the service port.
		if egressListener.IstioListener != nil && egressListener.IstioListener.Port != nil {
			listenerPort = 0
		}
	} else {
		meshGateway := map[string]bool{model.IstioMeshGateway: true}
		services = push.Services(node)
		virtualServices = push.VirtualServices(node, meshGateway)
	}

	nameToServiceMap := make(map[model.Hostname]*model.Service)
	for _, svc := range services {
		if listenerPort == 0 {
			// Take all ports when listen port is 0 (http_proxy or uds)
			// Expect virtualServices to resolve to right port
			nameToServiceMap[svc.Hostname] = svc
		} else {
			if svcPort, exists := svc.Ports.GetByPort(listenerPort); exists {

				nameToServiceMap[svc.Hostname] = &model.Service{
					Hostname:     svc.Hostname,
					Address:      svc.Address,
					MeshExternal: svc.MeshExternal,
					Ports:        []*model.Port{svcPort},
				}
			}
		}
	}

	// Collect all proxy labels for source match
	var proxyLabels model.LabelsCollection
	for _, w := range proxyInstances {
		proxyLabels = append(proxyLabels, w.Labels)
	}

	// Get list of virtual services bound to the mesh gateway
	virtualHostWrappers := istio_route.BuildSidecarVirtualHostsFromConfigAndRegistry(node, push, nameToServiceMap, proxyLabels, virtualServices, listenerPort)
	vHostPortMap := make(map[int][]route.VirtualHost)

	for _, virtualHostWrapper := range virtualHostWrappers {
		// If none of the routes matched by source, skip this virtual host
		if len(virtualHostWrapper.Routes) == 0 {
			continue
		}

		virtualHosts := make([]route.VirtualHost, 0, len(virtualHostWrapper.VirtualServiceHosts)+len(virtualHostWrapper.Services))
		for _, host := range virtualHostWrapper.VirtualServiceHosts {
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    fmt.Sprintf("%s:%d", host, virtualHostWrapper.Port),
				Domains: []string{host, fmt.Sprintf("%s:%d", host, virtualHostWrapper.Port)},
				Routes:  virtualHostWrapper.Routes,
			})
		}

		for _, svc := range virtualHostWrapper.Services {
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    fmt.Sprintf("%s:%d", svc.Hostname, virtualHostWrapper.Port),
				Domains: generateVirtualHostDomains(svc, virtualHostWrapper.Port, node),
				Routes:  virtualHostWrapper.Routes,
			})
		}

		vHostPortMap[virtualHostWrapper.Port] = append(vHostPortMap[virtualHostWrapper.Port], virtualHosts...)
	}

	var virtualHosts []route.VirtualHost
	if listenerPort == 0 {
		virtualHosts = mergeAllVirtualHosts(vHostPortMap)
	} else {
		virtualHosts = vHostPortMap[listenerPort]
	}

	util.SortVirtualHosts(virtualHosts)

	if pilot.EnableFallthroughRoute() {
		// This needs to be the last virtual host, as routes are evaluated in order.
		if env.Mesh.OutboundTrafficPolicy.Mode == meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY {
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    "allow_any",
				Domains: []string{"*"},
				Routes: []route.Route{
					{
						Match: route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								ClusterSpecifier: &route.RouteAction_Cluster{Cluster: util.PassthroughCluster},
							},
						},
					},
				},
			})
		} else {
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    "block_all",
				Domains: []string{"*"},
				Routes: []route.Route{
					{
						Match: route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &route.Route_DirectResponse{
							DirectResponse: &route.DirectResponseAction{
								Status: 502,
							},
						},
					},
				},
			})
		}
	}

	out := &xdsapi.RouteConfiguration{
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
		p.OnOutboundRouteConfiguration(in, out)
	}

	return out
}

// generateVirtualHostDomains generates the set of domain matches for a service being accessed from
// a proxy node
func generateVirtualHostDomains(service *model.Service, port int, node *model.Proxy) []string {
	domains := []string{string(service.Hostname), fmt.Sprintf("%s:%d", service.Hostname, port)}
	domains = append(domains, generateAltVirtualHosts(string(service.Hostname), port, node.DNSDomain)...)

	if len(service.Address) > 0 && service.Address != model.UnspecifiedIP {
		svcAddr := service.GetServiceAddressForProxy(node)
		// add a vhost match for the IP (if its non CIDR)
		cidr := util.ConvertAddressToCidr(svcAddr)
		if cidr.PrefixLen.Value == 32 {
			domains = append(domains, svcAddr)
			domains = append(domains, fmt.Sprintf("%s:%d", svcAddr, port))
		}
	}
	return domains
}

// Given a service, and a port, this function generates all possible HTTP Host headers.
// For example, a service of the form foo.local.campus.net on port 80, with local domain "local.campus.net"
// could be accessed as http://foo:80 within the .local network, as http://foo.local:80 (by other clients
// in the campus.net domain), as http://foo.local.campus:80, etc.
// NOTE: When a sidecar in remote.campus.net domain is talking to foo.local.campus.net,
// we should only generate foo.local, foo.local.campus, etc (and never just "foo").
//
// - Given foo.local.campus.net on proxy domain local.campus.net, this function generates
// foo:80, foo.local:80, foo.local.campus:80, with and without ports. It will not generate
// foo.local.campus.net (full hostname) since its already added elsewhere.
//
// - Given foo.local.campus.net on proxy domain remote.campus.net, this function generates
// foo.local:80, foo.local.campus:80
//
// - Given foo.local.campus.net on proxy domain "" or proxy domain example.com, this
// function returns nil
func generateAltVirtualHosts(hostname string, port int, proxyDomain string) []string {
	var vhosts []string
	uniqHostname, sharedDNSDomain := getUniqueAndSharedDNSDomain(hostname, proxyDomain)

	// If there is no shared DNS name (e.g., foobar.com service on local.net proxy domain)
	// do not generate any alternate virtual host representations
	if len(sharedDNSDomain) == 0 {
		return nil
	}

	// adds the uniq piece foo, foo:80
	vhosts = append(vhosts, uniqHostname)
	vhosts = append(vhosts, fmt.Sprintf("%s:%d", uniqHostname, port))

	// adds all the other variants (foo.local, foo.local:80)
	for i := len(sharedDNSDomain) - 1; i > 0; i-- {
		if sharedDNSDomain[i] == '.' {
			variant := fmt.Sprintf("%s.%s", uniqHostname, sharedDNSDomain[:i])
			variantWithPort := fmt.Sprintf("%s:%d", variant, port)
			vhosts = append(vhosts, variant)
			vhosts = append(vhosts, variantWithPort)
		}
	}
	return vhosts
}

// mergeAllVirtualHosts across all ports. On routes for ports other than port 80,
// virtual hosts without an explicit port suffix (IP:PORT) should not be added
func mergeAllVirtualHosts(vHostPortMap map[int][]route.VirtualHost) []route.VirtualHost {
	var virtualHosts []route.VirtualHost
	for p, vhosts := range vHostPortMap {
		if p == 80 {
			virtualHosts = append(virtualHosts, vhosts...)
		} else {
			for _, vhost := range vhosts {
				var newDomains []string
				for _, domain := range vhost.Domains {
					if strings.Contains(domain, ":") {
						newDomains = append(newDomains, domain)
					}
				}
				if len(newDomains) > 0 {
					vhost.Domains = newDomains
					virtualHosts = append(virtualHosts, vhost)
				}
			}
		}
	}
	return virtualHosts
}

// reverseArray returns its argument string array reversed
func reverseArray(r []string) []string {
	for i, j := 0, len(r)-1; i < len(r)/2; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return r
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getUniqueAndSharedDNSDomain computes the unique and shared DNS suffix from a FQDN service name and
// the proxy's local domain with namespace. This is especially useful in Kubernetes environments, where
// a two services can have same name in different namespaces (e.g., foo.ns1.svc.cluster.local,
// foo.ns2.svc.cluster.local). In this case, if the proxy is in ns2.svc.cluster.local, then while
// generating alt virtual hosts for service foo.ns1 for the sidecars in ns2 namespace, we should generate
// foo.ns1, foo.ns1.svc, foo.ns1.svc.cluster.local and should not generate a virtual host called "foo" for
// foo.ns1 service.
// So given foo.ns1.svc.cluster.local and ns2.svc.cluster.local, this function will return
// foo.ns1, and svc.cluster.local.
// When given foo.ns2.svc.cluster.local and ns2.svc.cluster.local, this function will return
// foo, ns2.svc.cluster.local.
func getUniqueAndSharedDNSDomain(fqdnHostname, proxyDomain string) (string, string) {
	// split them by the dot and reverse the arrays, so that we can
	// start collecting the shared bits of DNS suffix.
	// E.g., foo.ns1.svc.cluster.local -> local,cluster,svc,ns1,foo
	//       ns2.svc.cluster.local -> local,cluster,svc,ns2
	partsFQDN := reverseArray(strings.Split(fqdnHostname, "."))
	partsProxyDomain := reverseArray(strings.Split(proxyDomain, "."))
	var sharedSuffixesInReverse []string // pieces shared between proxy and svc. e.g., local,cluster,svc

	for i := 0; i < min(len(partsFQDN), len(partsProxyDomain)); i++ {
		if partsFQDN[i] == partsProxyDomain[i] {
			sharedSuffixesInReverse = append(sharedSuffixesInReverse, partsFQDN[i])
		} else {
			break
		}
	}

	if len(sharedSuffixesInReverse) == 0 {
		return fqdnHostname, ""
	}

	// get the non shared pieces (ns1, foo) and reverse Array
	uniqHostame := strings.Join(reverseArray(partsFQDN[len(sharedSuffixesInReverse):]), ".")
	sharedSuffixes := strings.Join(reverseArray(sharedSuffixesInReverse), ".")
	return uniqHostame, sharedSuffixes
}
