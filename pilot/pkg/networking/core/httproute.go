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
	"net"
	"sort"
	"strconv"
	"strings"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	statefulsession "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/stateful_session/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/envoyfilter"
	istio_route "istio.io/istio/pilot/pkg/networking/core/route"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

const (
	wildcardDomainPrefix     = "*."
	inboundVirtualHostPrefix = string(model.TrafficDirectionInbound) + "|http|"
)

// BuildHTTPRoutes produces a list of routes for the proxy
func (configgen *ConfigGeneratorImpl) BuildHTTPRoutes(
	node *model.Proxy,
	req *model.PushRequest,
	routeNames []string,
) ([]*discovery.Resource, model.XdsLogDetails) {
	var routeConfigurations model.Resources

	efw := req.Push.EnvoyFilters(node)
	hit, miss := 0, 0
	switch node.Type {
	case model.SidecarProxy, model.Waypoint:
		vHostCache := make(map[int][]*route.VirtualHost)
		// dependent envoyfilters' key, calculate in front once to prevent calc for each route.
		envoyfilterKeys := efw.KeysApplyingTo(
			networking.EnvoyFilter_ROUTE_CONFIGURATION,
			networking.EnvoyFilter_VIRTUAL_HOST,
			networking.EnvoyFilter_HTTP_ROUTE,
		)
		for _, routeName := range routeNames {
			rc, cached := configgen.buildSidecarOutboundHTTPRouteConfig(node, req, routeName, vHostCache, efw, envoyfilterKeys)
			if cached && !features.EnableUnsafeAssertions {
				hit++
			} else {
				miss++
			}
			if rc == nil {
				emptyRoute := &route.RouteConfiguration{
					Name:             routeName,
					VirtualHosts:     []*route.VirtualHost{},
					ValidateClusters: proto.BoolFalse,
				}
				rc = &discovery.Resource{
					Name:     routeName,
					Resource: protoconv.MessageToAny(emptyRoute),
				}
			}
			routeConfigurations = append(routeConfigurations, rc)
		}
	case model.Router:
		for _, routeName := range routeNames {
			rc := configgen.buildGatewayHTTPRouteConfig(node, req.Push, routeName)
			if rc != nil {
				rc = envoyfilter.ApplyRouteConfigurationPatches(networking.EnvoyFilter_GATEWAY, node, efw, rc)
				resource := &discovery.Resource{
					Name:     routeName,
					Resource: protoconv.MessageToAny(rc),
				}
				routeConfigurations = append(routeConfigurations, resource)
			}
		}
	}
	if !features.EnableRDSCaching {
		return routeConfigurations, model.DefaultXdsLogDetails
	}
	return routeConfigurations, model.XdsLogDetails{AdditionalInfo: fmt.Sprintf("cached:%v/%v", hit, hit+miss)}
}

// buildSidecarInboundHTTPRouteConfig builds the route config with a single wildcard virtual host on the inbound path
// TODO: trace decorators, inbound timeouts
func buildSidecarInboundHTTPRouteConfig(lb *ListenerBuilder, cc inboundChainConfig) *route.RouteConfiguration {
	traceOperation := telemetry.TraceOperation(string(cc.telemetryMetadata.InstanceHostname), cc.port.Port)
	defaultRoute := istio_route.BuildDefaultHTTPInboundRoute(lb.node, cc.clusterName, traceOperation, cc.port.Protocol)

	inboundVHost := &route.VirtualHost{
		Name:    inboundVirtualHostPrefix + strconv.Itoa(cc.port.Port), // Format: "inbound|http|%d"
		Domains: []string{"*"},
		Routes:  []*route.Route{defaultRoute},
	}

	r := &route.RouteConfiguration{
		Name:             cc.clusterName,
		VirtualHosts:     []*route.VirtualHost{inboundVHost},
		ValidateClusters: proto.BoolFalse,
	}
	efw := lb.push.EnvoyFilters(lb.node)
	r = envoyfilter.ApplyRouteConfigurationPatches(networking.EnvoyFilter_SIDECAR_INBOUND, lb.node, efw, r)
	return r
}

// buildSidecarOutboundHTTPRouteConfig builds an outbound HTTP Route for sidecar.
// Based on port, will determine all virtual hosts that listen on the port.
func (configgen *ConfigGeneratorImpl) buildSidecarOutboundHTTPRouteConfig(
	node *model.Proxy,
	req *model.PushRequest,
	routeName string,
	vHostCache map[int][]*route.VirtualHost,
	efw *model.MergedEnvoyFilterWrapper,
	efKeys []string,
) (*discovery.Resource, bool) {
	listenerPort, useSniffing, err := extractListenerPort(routeName)
	if err != nil && routeName != model.RDSHttpProxy && !strings.HasPrefix(routeName, model.UnixAddressPrefix) {
		// TODO: This is potentially one place where envoyFilter ADD operation can be helpful if the
		// user wants to ship a custom RDS. But at this point, the match semantics are murky. We have no
		// object to match upon. This needs more thought. For now, we will continue to return nil for
		// unknown routes
		return nil, false
	}

	var virtualHosts []*route.VirtualHost
	var routeCache *istio_route.Cache
	var resource *discovery.Resource

	cacheHit := false
	if useSniffing && listenerPort != 0 {
		// Check if we have already computed the list of all virtual hosts for this port
		// If so, then  we simply have to return only the relevant virtual hosts for
		// this listener's host:port
		if vhosts, exists := vHostCache[listenerPort]; exists {
			virtualHosts = getVirtualHostsForSniffedServicePort(vhosts, routeName)
			cacheHit = true
		}
	}
	if !cacheHit {
		virtualHosts, resource, routeCache = BuildSidecarOutboundVirtualHosts(node, req.Push, routeName, listenerPort, efKeys, configgen.Cache)
		if resource != nil {
			return resource, true
		}
		if listenerPort > 0 {
			// only cache for tcp ports and not for uds
			vHostCache[listenerPort] = virtualHosts
		}

		// FIXME: This will ignore virtual services with hostnames that do not match any service in the registry
		// per api spec, these hostnames + routes should appear in the virtual hosts (think bookinfo.com and
		// productpage.ns1.svc.cluster.local). See the TODO in BuildSidecarOutboundVirtualHosts for the right solution
		if useSniffing {
			virtualHosts = getVirtualHostsForSniffedServicePort(virtualHosts, routeName)
		}
	}

	util.SortVirtualHosts(virtualHosts)

	if !useSniffing {
		ph := util.GetProxyHeaders(node, req.Push, istionetworking.ListenerClassSidecarOutbound)
		appendXForwardedHost := ph.XForwardedHost
		includeRequestAttemptCount := ph.IncludeRequestAttemptCount
		virtualHosts = append(virtualHosts, buildCatchAllVirtualHost(node, includeRequestAttemptCount, appendXForwardedHost))
	}

	out := &route.RouteConfiguration{
		Name:                           routeName,
		VirtualHosts:                   virtualHosts,
		ValidateClusters:               proto.BoolFalse,
		MaxDirectResponseBodySizeBytes: istio_route.DefaultMaxDirectResponseBodySizeBytes,
		IgnorePortInHostMatching:       true,
	}

	// apply envoy filter patches
	out = envoyfilter.ApplyRouteConfigurationPatches(networking.EnvoyFilter_SIDECAR_OUTBOUND, node, efw, out)

	resource = &discovery.Resource{
		Name:     out.Name,
		Resource: protoconv.MessageToAny(out),
	}

	if features.EnableRDSCaching && routeCache != nil {
		configgen.Cache.Add(routeCache, req, resource)
	}

	return resource, false
}

func extractListenerPort(routeName string) (int, bool, error) {
	hasPrefix := strings.HasPrefix(routeName, model.UnixAddressPrefix)
	index := strings.IndexRune(routeName, ':')
	if !hasPrefix {
		routeName = routeName[index+1:]
	}

	listenerPort, err := strconv.Atoi(routeName)
	useSniffing := !hasPrefix && index != -1
	return listenerPort, useSniffing, err
}

// TODO: merge with IstioEgressListenerWrapper.selectVirtualServices
// selectVirtualServices selects the virtual services by matching given services' host names.
func selectVirtualServices(virtualServices []config.Config, servicesByName map[host.Name]*model.Service) []config.Config {
	out := make([]config.Config, 0)
	// As a performance optimization, find out wildcard service hosts first, so that
	// if non wildcard vs hosts can't be looked up directly in the service map, only need to
	// loop through wildcard service hosts instead of all.
	wcSvcHosts := []host.Name{}
	for svcHost := range servicesByName {
		if svcHost.IsWildCarded() {
			wcSvcHosts = append(wcSvcHosts, svcHost)
		}
	}

	for i := range virtualServices {
		rule := virtualServices[i].Spec.(*networking.VirtualService)
		var match bool

		// Selection algorithm:
		// virtualservices have a list of hosts in the API spec
		// if any host in the list matches one service hostname, select the virtual service
		// and break out of the loop.
		for _, h := range rule.Hosts {
			lch := host.Name(strings.ToLower(h))
			// TODO: This is a bug. VirtualServices can have many hosts
			// while the user might be importing only a single host
			// We need to generate a new VirtualService with just the matched host
			if servicesByName[lch] != nil {
				match = true
				break
			}

			if lch.IsWildCarded() {
				// Process wildcard vs host as it need to follow the slow path of
				// looping through all services in the map.
				for svcHost := range servicesByName {
					if lch.Matches(svcHost) {
						match = true
						break
					}
				}
			} else {
				// If non wildcard vs host isn't be found in service map, only loop through
				// wildcard service hosts to avoid repeated matching.
				for _, svcHost := range wcSvcHosts {
					if lch.Matches(svcHost) {
						match = true
						break
					}
				}
			}

			if match {
				break
			}
		}

		if match {
			out = append(out, virtualServices[i])
		}
	}
	return out
}

func BuildSidecarOutboundVirtualHosts(node *model.Proxy, push *model.PushContext,
	routeName string,
	listenerPort int,
	efKeys []string,
	xdsCache model.XdsCache,
) ([]*route.VirtualHost, *discovery.Resource, *istio_route.Cache) {
	// Get the services from the egress listener.  When sniffing is enabled, we send
	// route name as foo.bar.com:8080 which is going to match against the wildcard
	// egress listener only. A route with sniffing would not have been generated if there
	// was a sidecar with explicit port (and hence protocol declaration). A route with
	// sniffing is generated only in the case of the catch all egress listener.
	egressListener := node.SidecarScope.GetEgressListenerForRDS(listenerPort, routeName)
	// We should never be getting a nil egress listener because the code that setup this RDS
	// call obviously saw an egress listener
	if egressListener == nil {
		return nil, nil, nil
	}

	services := egressListener.Services()
	// To maintain correctness, we should only use the virtualservices for
	// this listener and not all virtual services accessible to this proxy.
	virtualServices := egressListener.VirtualServices()

	// When generating RDS for ports created via the SidecarScope, we treat ports as HTTP proxy style ports
	// if ports protocol is HTTP_PROXY.
	if egressListener.IstioListener != nil && egressListener.IstioListener.Port != nil &&
		protocol.Parse(egressListener.IstioListener.Port.Protocol) == protocol.HTTP_PROXY {
		listenerPort = 0
	}

	includeRequestAttemptCount := util.GetProxyHeaders(node, push, istionetworking.ListenerClassSidecarOutbound).IncludeRequestAttemptCount

	servicesByName := make(map[host.Name]*model.Service)
	for _, svc := range services {
		if listenerPort == 0 {
			// Take all ports when listen port is 0 (http_proxy or uds)
			// Expect virtualServices to resolve to right port
			servicesByName[svc.Hostname] = svc
		} else if svcPort, exists := svc.Ports.GetByPort(listenerPort); exists {
			h := host.Name(strings.ToLower(string(svc.Hostname)))
			servicesByName[h] = &model.Service{
				Hostname:       h,
				DefaultAddress: svc.GetAddressForProxy(node),
				ClusterVIPs:    *svc.ClusterVIPs.DeepCopy(),
				MeshExternal:   svc.MeshExternal,
				Resolution:     svc.Resolution,
				Ports:          []*model.Port{svcPort},
				Attributes: model.ServiceAttributes{
					Namespace:       svc.Attributes.Namespace,
					ServiceRegistry: svc.Attributes.ServiceRegistry,
					Labels:          svc.Attributes.Labels,
					Aliases:         svc.Attributes.Aliases,
					K8sAttributes:   svc.Attributes.K8sAttributes,
				},
			}
		}
	}

	var routeCache *istio_route.Cache
	if listenerPort > 0 && features.EnableRDSCaching {
		// sort services, ensure that routeCache calculation result is stable
		services = make([]*model.Service, 0, len(servicesByName))
		for _, svc := range servicesByName {
			services = append(services, svc)
		}
		sort.SliceStable(services, func(i, j int) bool {
			return services[i].Hostname <= services[j].Hostname
		})
		routeCache = &istio_route.Cache{
			RouteName:               routeName,
			ProxyVersion:            node.Metadata.IstioVersion,
			ClusterID:               string(node.Metadata.ClusterID),
			DNSDomain:               node.DNSDomain,
			DNSCapture:              bool(node.Metadata.DNSCapture),
			DNSAutoAllocate:         bool(node.Metadata.DNSAutoAllocate),
			AllowAny:                util.IsAllowAnyOutbound(node),
			ListenerPort:            listenerPort,
			Services:                services,
			VirtualServices:         virtualServices,
			DelegateVirtualServices: push.DelegateVirtualServices(virtualServices),
			EnvoyFilterKeys:         efKeys,
		}
	}

	// This is hack to keep consistent with previous behavior.
	if listenerPort != 80 {
		// only select virtualServices that matches a service
		virtualServices = selectVirtualServices(virtualServices, servicesByName)
	}

	mostSpecificWildcardVsIndex := egressListener.MostSpecificWildcardVirtualServiceIndex()
	// Get list of virtual services bound to the mesh gateway
	virtualHostWrappers := istio_route.BuildSidecarVirtualHostWrapper(routeCache, node, push,
		servicesByName, virtualServices, listenerPort, mostSpecificWildcardVsIndex,
	)

	if features.EnableRDSCaching {
		resource := xdsCache.Get(routeCache)
		if resource != nil && !features.EnableUnsafeAssertions {
			return nil, resource, routeCache
		}
	}

	vHostPortMap := make(map[int][]*route.VirtualHost)
	vhosts := sets.String{}
	vhdomains := sets.String{}
	knownFQDN := sets.String{}

	buildVirtualHost := func(hostname string, vhwrapper istio_route.VirtualHostWrapper, svc *model.Service) *route.VirtualHost {
		name := util.DomainName(hostname, vhwrapper.Port)
		if vhosts.InsertContains(name) {
			// This means this virtual host has caused duplicate virtual host name.
			var msg string
			if svc == nil {
				msg = fmt.Sprintf("duplicate domain from virtual service: %s", name)
			} else {
				msg = fmt.Sprintf("duplicate domain from service: %s", name)
			}
			push.AddMetric(model.DuplicatedDomains, name, node.ID, msg)
			return nil
		}
		var domains []string
		var altHosts []string
		if svc == nil {
			if SidecarIgnorePort(node) {
				domains = []string{util.IPv6Compliant(hostname)}
			} else {
				domains = []string{util.IPv6Compliant(hostname), name}
			}
		} else {
			domains, altHosts = generateVirtualHostDomains(svc, listenerPort, vhwrapper.Port, node)
		}
		dl := len(domains)
		domains = dedupeDomains(domains, vhdomains, altHosts, knownFQDN)
		if dl != len(domains) {
			var msg string
			if svc == nil {
				msg = fmt.Sprintf("duplicate domain from virtual service: %s", name)
			} else {
				msg = fmt.Sprintf("duplicate domain from service: %s", name)
			}
			// This means this virtual host has caused duplicate virtual host domain.
			push.AddMetric(model.DuplicatedDomains, name, node.ID, msg)
		}
		if len(domains) > 0 {
			pervirtualHostFilters := map[string]*anypb.Any{}
			if statefulConfig := util.MaybeBuildStatefulSessionFilterConfig(svc); statefulConfig != nil {
				perRouteStatefulSession := &statefulsession.StatefulSessionPerRoute{
					Override: &statefulsession.StatefulSessionPerRoute_StatefulSession{
						StatefulSession: statefulConfig,
					},
				}
				pervirtualHostFilters[util.StatefulSessionFilter] = protoconv.MessageToAny(perRouteStatefulSession)
			}
			return &route.VirtualHost{
				Name:                       name,
				Domains:                    domains,
				Routes:                     vhwrapper.Routes,
				IncludeRequestAttemptCount: includeRequestAttemptCount,
				TypedPerFilterConfig:       pervirtualHostFilters,
			}
		}

		return nil
	}

	for _, virtualHostWrapper := range virtualHostWrappers {
		for _, svc := range virtualHostWrapper.Services {
			name := util.DomainName(string(svc.Hostname), virtualHostWrapper.Port)
			knownFQDN.InsertAll(name, string(svc.Hostname))
		}
	}

	for _, virtualHostWrapper := range virtualHostWrappers {
		// If none of the routes matched by source, skip this virtual host
		if len(virtualHostWrapper.Routes) == 0 {
			continue
		}
		virtualHosts := make([]*route.VirtualHost, 0, len(virtualHostWrapper.VirtualServiceHosts)+len(virtualHostWrapper.Services))

		for _, hostname := range virtualHostWrapper.VirtualServiceHosts {
			if vhost := buildVirtualHost(strings.ToLower(hostname), virtualHostWrapper, nil); vhost != nil {
				virtualHosts = append(virtualHosts, vhost)
			}
		}

		for _, svc := range virtualHostWrapper.Services {
			if vhost := buildVirtualHost(string(svc.Hostname), virtualHostWrapper, svc); vhost != nil {
				virtualHosts = append(virtualHosts, vhost)
			}
		}
		vHostPortMap[virtualHostWrapper.Port] = append(vHostPortMap[virtualHostWrapper.Port], virtualHosts...)
	}

	var out []*route.VirtualHost
	if listenerPort == 0 {
		out = mergeAllVirtualHosts(vHostPortMap)
	} else {
		out = vHostPortMap[listenerPort]
	}

	return out, nil, routeCache
}

// dedupeDomains removes the duplicate domains from the passed in domains.
func dedupeDomains(domains []string, vhdomains sets.String, expandedHosts []string, knownFQDNs sets.String) []string {
	temp := domains[:0]
	for _, d := range domains {
		if vhdomains.Contains(strings.ToLower(d)) {
			continue
		}
		// Check if the domain is an "expanded" host, and its also a known FQDN
		// This prevents a case where a domain like "foo.com.cluster.local" gets expanded to "foo.com", overwriting
		// the real "foo.com"
		// This works by providing a list of domains that were added as expanding the DNS domain as part of expandedHosts,
		// and a list of known unexpanded FQDNs to compare against
		if slices.Contains(expandedHosts, d) && knownFQDNs.Contains(d) { // O(n) search, but n is at most 10
			continue
		}
		temp = append(temp, d)
		vhdomains.Insert(strings.ToLower(d))
	}
	return temp
}

// Returns the set of virtual hosts that correspond to the listener that has HTTP protocol detection
// setup. This listener should only get the virtual hosts that correspond to this service+port and not
// all virtual hosts that are usually supplied for 0.0.0.0:PORT.
func getVirtualHostsForSniffedServicePort(vhosts []*route.VirtualHost, routeName string) []*route.VirtualHost {
	nameWithoutPort, _, _ := net.SplitHostPort(routeName)
	var virtualHosts []*route.VirtualHost
	for _, vh := range vhosts {
		for _, domain := range vh.Domains {
			if domain == routeName || domain == nameWithoutPort {
				virtualHosts = append(virtualHosts, vh)
				break
			}
		}
	}

	if len(virtualHosts) == 0 {
		return virtualHosts
	}
	if len(virtualHosts) == 1 {
		virtualHosts[0].Domains = []string{"*"}
		return virtualHosts
	}
	if features.EnableUnsafeAssertions {
		panic(fmt.Sprintf("unexpectedly matched multiple virtual hosts for %v: %v", routeName, virtualHosts))
	}
	return virtualHosts
}

func SidecarIgnorePort(node *model.Proxy) bool {
	return !node.IsProxylessGrpc()
}

// generateVirtualHostDomains generates the set of domain matches for a service being accessed from
// a proxy node
func generateVirtualHostDomains(service *model.Service, listenerPort int, port int, node *model.Proxy) ([]string, []string) {
	if SidecarIgnorePort(node) && listenerPort != 0 {
		// Indicate we do not need port, as we will set IgnorePortInHostMatching
		port = portNoAppendPortSuffix
	}
	domains := []string{}
	allAltHosts := []string{}
	all := []string{string(service.Hostname)}
	for _, a := range service.Attributes.Aliases {
		all = append(all, a.Hostname.String())
	}
	for _, s := range all {
		altHosts := GenerateAltVirtualHosts(s, port, node.DNSDomain)
		domains = appendDomainPort(domains, s, port)
		domains = append(domains, altHosts...)
		allAltHosts = append(allAltHosts, altHosts...)
	}

	if service.Resolution == model.Passthrough &&
		service.Attributes.ServiceRegistry == provider.Kubernetes {
		for _, domain := range domains {
			domains = append(domains, wildcardDomainPrefix+domain)
		}
	}

	for _, svcAddr := range service.GetAllAddressesForProxy(node) {
		if len(svcAddr) > 0 && svcAddr != constants.UnspecifiedIP {
			domains = appendDomainPort(domains, svcAddr, port)
		}
	}

	return domains, allAltHosts
}

// appendDomainPort appends `domain` and `domain:port` to `domains`. The `domain:port` variant is skipped
// if port is unset.
func appendDomainPort(domains []string, domain string, port int) []string {
	if port == portNoAppendPortSuffix {
		return append(domains, util.IPv6Compliant(domain))
	}
	return append(domains, util.IPv6Compliant(domain), util.DomainName(domain, port))
}

// GenerateAltVirtualHosts given a service and a port, generates all possible HTTP Host headers.
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
func GenerateAltVirtualHosts(hostname string, port int, proxyDomain string) []string {
	var vhosts []string // Initialize the slice for alternate hosts

	if features.EnableAbsoluteFqdnVhostDomain {
		// Add the absolute FQDN variant (with trailing dot) if the hostname is not an IP address.
		// This is considered another form of alternate host representation.
		// See https://github.com/istio/istio/issues/56007 for context.
		// "foo.local.campus.net" -> "foo.local.campus.net."
		// "foo.bar.svc.cluster.local" -> "foo.bar.svc.cluster.local."
		isIP := net.ParseIP(hostname) != nil
		if !isIP {
			vhosts = append(vhosts, hostname+".")
		}
		if port != portNoAppendPortSuffix {
			vhosts = append(vhosts, util.DomainName(hostname+".", port))
		}
	}

	// If the dns/proxy domain contains `.svc`, only services following the <ns>.svc.<suffix>
	// naming convention and that share a suffix with the domain should be expanded.
	if strings.Contains(proxyDomain, ".svc.") {

		if strings.HasSuffix(hostname, removeSvcNamespace(proxyDomain)) {
			kubeSVCAltHosts := generateAltVirtualHostsForKubernetesService(hostname, port, proxyDomain)
			return append(vhosts, kubeSVCAltHosts...)
		}

		// Hostname is not a kube service.  It is not safe to expand the
		// hostname as non-fully-qualified names could conflict with expansion of other kube service
		// hostnames
		return vhosts
	}

	uniqueHostnameParts, sharedDNSDomainParts := getUniqueAndSharedDNSDomain(hostname, proxyDomain)

	// If there is no shared DNS name (e.g., foobar.com service on local.net proxy domain)
	// do not generate any alternate virtual host representations
	if len(sharedDNSDomainParts) == 0 {
		return vhosts
	}

	uniqueHostname := strings.Join(uniqueHostnameParts, ".")

	// Add the uniqueHost.
	vhosts = appendDomainPort(vhosts, uniqueHostname, port)
	if len(uniqueHostnameParts) == 2 {
		// This is the case of uniqHostname having namespace already.
		dnsHostName := uniqueHostname + "." + sharedDNSDomainParts[0]
		vhosts = appendDomainPort(vhosts, dnsHostName, port)
	}
	return vhosts
}

// portNoAppendPortSuffix is a signal to not append port to vhost
const portNoAppendPortSuffix = 0

func generateAltVirtualHostsForKubernetesService(hostname string, port int, proxyDomain string) []string {
	id := strings.Index(proxyDomain, ".svc.")
	ih := strings.Index(hostname, ".svc.")
	if ih > 0 { // Proxy and service hostname are in kube
		ns := strings.Index(hostname, ".")
		if ns+1 >= len(hostname) || ns+1 > ih {
			// Invalid domain
			return nil
		}
		if hostname[ns+1:ih] == proxyDomain[:id] {
			// Same namespace
			if port == portNoAppendPortSuffix {
				return []string{
					hostname[:ns],
					hostname[:ih] + ".svc",
					hostname[:ih],
				}
			}
			return []string{
				hostname[:ns],
				util.DomainName(hostname[:ns], port),
				hostname[:ih] + ".svc",
				util.DomainName(hostname[:ih]+".svc", port),
				hostname[:ih],
				util.DomainName(hostname[:ih], port),
			}
		}
		// Different namespace
		if port == portNoAppendPortSuffix {
			return []string{
				hostname[:ih],
				hostname[:ih] + ".svc",
			}
		}
		return []string{
			hostname[:ih],
			util.DomainName(hostname[:ih], port),
			hostname[:ih] + ".svc",
			util.DomainName(hostname[:ih]+".svc", port),
		}
	}
	// Proxy is in k8s, but service isn't. No alt hosts
	return nil
}

// mergeAllVirtualHosts across all ports. On routes for ports other than port 80,
// virtual hosts without an explicit port suffix (IP:PORT) should not be added
func mergeAllVirtualHosts(vHostPortMap map[int][]*route.VirtualHost) []*route.VirtualHost {
	var virtualHosts []*route.VirtualHost
	for p, vhosts := range vHostPortMap {
		if p == 80 {
			virtualHosts = append(virtualHosts, vhosts...)
		} else {
			for _, vhost := range vhosts {
				vhost.Domains = slices.FilterInPlace(vhost.Domains, func(domain string) bool {
					return strings.Contains(domain, ":")
				})
				if len(vhost.Domains) > 0 {
					virtualHosts = append(virtualHosts, vhost)
				}
			}
		}
	}
	return virtualHosts
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
func getUniqueAndSharedDNSDomain(fqdnHostname, proxyDomain string) (partsUnique []string, partsShared []string) {
	// split them by the dot and reverse the arrays, so that we can
	// start collecting the shared bits of DNS suffix.
	// E.g., foo.ns1.svc.cluster.local -> local,cluster,svc,ns1,foo
	//       ns2.svc.cluster.local -> local,cluster,svc,ns2
	partsFQDN := strings.Split(fqdnHostname, ".")
	partsProxyDomain := strings.Split(proxyDomain, ".")
	partsFQDNInReverse := slices.Reverse(partsFQDN)
	partsProxyDomainInReverse := slices.Reverse(partsProxyDomain)
	var sharedSuffixesInReverse []string // pieces shared between proxy and svc. e.g., local,cluster,svc

	for i := 0; i < min(len(partsFQDNInReverse), len(partsProxyDomainInReverse)); i++ {
		if partsFQDNInReverse[i] == partsProxyDomainInReverse[i] {
			sharedSuffixesInReverse = append(sharedSuffixesInReverse, partsFQDNInReverse[i])
		} else {
			break
		}
	}

	if len(sharedSuffixesInReverse) == 0 {
		partsUnique = partsFQDN
	} else {
		// get the non shared pieces (ns1, foo) and reverse Array
		partsUnique = slices.Reverse(partsFQDNInReverse[len(sharedSuffixesInReverse):])
		partsShared = slices.Reverse(sharedSuffixesInReverse)
	}
	return partsUnique, partsShared
}

func buildCatchAllVirtualHost(node *model.Proxy, includeRequestAttemptCount bool, appendXForwardedHost bool) *route.VirtualHost {
	if util.IsAllowAnyOutbound(node) {
		egressCluster := util.PassthroughCluster
		notimeout := durationpb.New(0)

		// no need to check for nil value as the previous if check has checked
		if node.SidecarScope.OutboundTrafficPolicy.EgressProxy != nil {
			// user has provided an explicit destination for all the unknown traffic.
			// build a cluster out of this destination
			egressCluster = istio_route.GetDestinationCluster(node.SidecarScope.OutboundTrafficPolicy.EgressProxy,
				nil, 0)
		}

		routeAction := &route.RouteAction{
			ClusterSpecifier: &route.RouteAction_Cluster{Cluster: egressCluster},
			// Disable timeout instead of assuming some defaults.
			Timeout: notimeout,
			// Use deprecated value for now as the replacement MaxStreamDuration has some regressions.
			// nolint: staticcheck
			MaxGrpcTimeout:       notimeout,
			AppendXForwardedHost: appendXForwardedHost,
		}

		return &route.VirtualHost{
			Name:    util.Passthrough,
			Domains: []string{"*"},
			Routes: []*route.Route{
				{
					Name: util.Passthrough,
					Match: &route.RouteMatch{
						PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &route.Route_Route{
						Route: routeAction,
					},
				},
			},
			IncludeRequestAttemptCount: includeRequestAttemptCount,
		}
	}

	return &route.VirtualHost{
		Name:    util.BlackHole,
		Domains: []string{"*"},
		Routes: []*route.Route{
			{
				Name: util.BlackHole,
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
				},
				Action: &route.Route_DirectResponse{
					DirectResponse: &route.DirectResponseAction{
						Status: 502,
					},
				},
			},
		},
		IncludeRequestAttemptCount: includeRequestAttemptCount,
	}
}

// Simply removes everything before .svc, if present
func removeSvcNamespace(domain string) string {
	if idx := strings.Index(domain, ".svc."); idx > 0 {
		return domain[idx:]
	}

	return domain
}
