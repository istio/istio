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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

// Headers with special meaning in Envoy
const (
	HeaderMethod    = ":method"
	HeaderAuthority = ":authority"
	HeaderScheme    = ":scheme"
)

const (
	// UnresolvedCluster for destinations pointing to unknown clusters.
	UnresolvedCluster = "unresolved-cluster"

	// DefaultRoute is the default decorator
	DefaultRoute = "default-route"
)

// GuardedHost is a context-dependent virtual host entry with guarded routes.
type GuardedHost struct {
	// Port is the capture port (e.g. service port)
	Port int

	// Services are the services matching the virtual host.
	// The service host names need to be contextualized by the source.
	Services []*model.Service

	// Hosts is a list of alternative literal host names for the host.
	Hosts []string

	// Routes in the virtual host
	Routes []route.Route
}

// translateVirtualHosts creates the entire routing table for Istio v1alpha3 configs.
// Services are indexed by FQDN hostnames.
// Cluster domain is used to resolve short service names (e.g. "svc.cluster.local").
func translateVirtualHosts(
	serviceConfigs []model.Config,
	services map[string]*model.Service,
	proxyLabels model.LabelsCollection,
	gatewayName string) []GuardedHost {
	out := make([]GuardedHost, 0)

	// translate all virtual service configs
	for _, config := range serviceConfigs {
		out = append(out, translateVirtualHost(config, services, proxyLabels, gatewayName)...)
	}

	// compute services missing service configs
	missing := make(map[string]bool)
	for fqdn := range services {
		missing[fqdn] = true
	}
	for _, host := range out {
		for _, service := range host.Services {
			delete(missing, service.Hostname)
		}
	}

	// append default hosts for the service missing virtual services
	for fqdn := range missing {
		svc := services[fqdn]
		for _, port := range svc.Ports {
			if port.Protocol.IsHTTP() {
				cluster := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", svc.Hostname, port)
				out = append(out, GuardedHost{
					Port:     port.Port,
					Services: []*model.Service{svc},
					Routes:   []route.Route{*buildDefaultHTTPRoute(cluster)},
				})
			}
		}
	}

	return out
}

// matchServiceHosts splits the virtual service hosts into services and literal hosts
func matchServiceHosts(in model.Config, serviceIndex map[string]*model.Service) ([]string, []*model.Service) {
	rule := in.Spec.(*networking.VirtualService)
	hosts := make([]string, 0)
	services := make([]*model.Service, 0)
	for _, host := range rule.Hosts {
		if svc := serviceIndex[host]; svc != nil {
			services = append(services, svc)
		} else {
			hosts = append(hosts, host)
		}
	}
	return hosts, services
}

// translateVirtualHost creates virtual hosts corresponding to a virtual service.
func translateVirtualHost(in model.Config, serviceIndex map[string]*model.Service,
	proxyLabels model.LabelsCollection, gatewayName string) []GuardedHost {
	hosts, services := matchServiceHosts(in, serviceIndex)
	serviceByPort := make(map[int][]*model.Service)
	for _, svc := range services {
		for _, port := range svc.Ports {
			if port.Protocol.IsHTTP() {
				serviceByPort[port.Port] = append(serviceByPort[port.Port], svc)
			}
		}
	}

	// if no services matched, then we have no port information -- default to 80 for now
	if len(serviceByPort) == 0 {
		serviceByPort[80] = nil
	}

	out := make([]GuardedHost, len(serviceByPort))
	for port, services := range serviceByPort {
		clusterNameGenerator := convertDestinationToCluster(serviceIndex, port)
		routes := translateRoutes(in, clusterNameGenerator, port, proxyLabels, gatewayName)
		if len(routes) == 0 {
			continue
		}
		out = append(out, GuardedHost{
			Port:     port,
			Services: services,
			Hosts:    hosts,
			Routes:   routes,
		})
	}

	return out
}

// convertDestinationToCluster produces a cluster naming function using the config context.
func convertDestinationToCluster(
	serviceIndex map[string]*model.Service,
	defaultPort int) ClusterNameGenerator {
	return func(destination *networking.Destination) string {
		// detect if it is a service
		svc := serviceIndex[destination.Host]

		if svc == nil {
			return UnresolvedCluster
		}

		// default port uses port number
		svcPort, _ := svc.Ports.GetByPort(defaultPort)
		if destination.Port != nil {
			switch selector := destination.Port.Port.(type) {
			case *networking.PortSelector_Name:
				svcPort, _ = svc.Ports.Get(selector.Name)
			case *networking.PortSelector_Number:
				svcPort, _ = svc.Ports.GetByPort(int(selector.Number))
			}
		}

		if svcPort == nil {
			return UnresolvedCluster
		}

		// use subsets if it is a service
		return model.BuildSubsetKey(model.TrafficDirectionOutbound, destination.Subset, svc.Hostname, svcPort)
	}
}

// ClusterNameGenerator specifies cluster name for a destination
type ClusterNameGenerator func(*networking.Destination) string

// translateRoutes creates virtual host routes from the v1alpha3 config.
// The rule should be adapted to destination names (outbound clusters).
// Each rule is guarded by source labels.
func translateRoutes(in model.Config, nameF ClusterNameGenerator, port int,
	proxyLabels model.LabelsCollection, gatewayName string) []route.Route {
	rule, ok := in.Spec.(*networking.VirtualService)
	if !ok {
		return nil
	}

	operation := in.ConfigMeta.Name

	out := make([]route.Route, 0)
	for _, http := range rule.Http {
		if len(http.Match) == 0 {
			if r := translateRoute(http, nil, port, operation, nameF, proxyLabels, gatewayName); r != nil {
				// this cannot be nil
				out = append(out, *r)
			}
			break // we have a rule with catch all match prefix: /. Other rules are of no use
		} else {
			// TODO: https://github.com/istio/istio/issues/4239
			for _, match := range http.Match {
				if r := translateRoute(http, match, port, operation, nameF, proxyLabels, gatewayName); r != nil {
					out = append(out, *r)
				}
			}
		}
	}

	return out
}

// sourceMatchHttp checks if the sourceLabels or the gateways in a match condition match with the
// labels for the proxy or the gateway name for which we are generating a route
func sourceMatchHTTP(match *networking.HTTPMatchRequest, proxyLabels model.LabelsCollection, gatewayName string) bool {
	if match == nil {
		return true
	}

	// Trim by source labels or mesh gateway
	if len(match.Gateways) > 0 {
		for _, g := range match.Gateways {
			if g == gatewayName {
				return true
			}
		}
	} else if proxyLabels.IsSupersetOf(match.GetSourceLabels()) {
		return true
	}

	return false
}

// translateRoute translates HTTP routes
// TODO: fault filters -- issue https://github.com/istio/api/issues/388
func translateRoute(in *networking.HTTPRoute,
	match *networking.HTTPMatchRequest, port int,
	operation string,
	nameF ClusterNameGenerator,
	proxyLabels model.LabelsCollection,
	gatewayName string) *route.Route {

	// Match by source labels/gateway names inside the match condition
	if !sourceMatchHTTP(match, proxyLabels, gatewayName) {
		return nil
	}

	// Match by the destination port specified in the match condition
	if match != nil && match.Port != nil && match.Port.GetNumber() != uint32(port) {
		return nil
	}

	out := &route.Route{
		Match: translateRouteMatch(match),
		Decorator: &route.Decorator{
			Operation: operation,
		},
	}

	if redirect := in.Redirect; redirect != nil {
		out.Action = &route.Route_Redirect{
			Redirect: &route.RedirectAction{
				HostRedirect: redirect.Authority,
				PathRewriteSpecifier: &route.RedirectAction_PathRedirect{
					PathRedirect: redirect.Uri,
				},
			}}
	} else {
		action := &route.RouteAction{
			Cors:         translateCORSPolicy(in.CorsPolicy),
			RetryPolicy:  translateRetryPolicy(in.Retries),
			Timeout:      translateTime(in.Timeout),
			UseWebsocket: &types.BoolValue{Value: in.WebsocketUpgrade},
		}
		out.Action = &route.Route_Route{Route: action}

		if rewrite := in.Rewrite; rewrite != nil {
			action.PrefixRewrite = rewrite.Uri
			action.HostRewriteSpecifier = &route.RouteAction_HostRewrite{
				HostRewrite: rewrite.Authority,
			}
		}

		if len(in.AppendHeaders) > 0 {
			action.RequestHeadersToAdd = make([]*core.HeaderValueOption, 0)
			for key, value := range in.AppendHeaders {
				action.RequestHeadersToAdd = append(action.RequestHeadersToAdd, &core.HeaderValueOption{
					Header: &core.HeaderValue{
						Key:   key,
						Value: value,
					},
				})
			}
		}

		if in.Mirror != nil {
			action.RequestMirrorPolicy = &route.RouteAction_RequestMirrorPolicy{Cluster: nameF(in.Mirror)}
		}

		weighted := make([]*route.WeightedCluster_ClusterWeight, 0)
		for _, dst := range in.Route {
			weight := &types.UInt32Value{Value: uint32(dst.Weight)}
			if dst.Weight == 0 {
				weight.Value = uint32(100)
			}
			weighted = append(weighted, &route.WeightedCluster_ClusterWeight{
				Name:   nameF(dst.Destination),
				Weight: weight,
			})
		}

		// rewrite to a single cluster if there is only weighted cluster
		if len(weighted) == 1 {
			action.ClusterSpecifier = &route.RouteAction_Cluster{Cluster: weighted[0].Name}
		} else {
			action.ClusterSpecifier = &route.RouteAction_WeightedClusters{
				WeightedClusters: &route.WeightedCluster{
					Clusters: weighted,
				},
			}
		}
	}

	return out
}

// translateRouteMatch translates match condition
func translateRouteMatch(in *networking.HTTPMatchRequest) route.RouteMatch {
	out := route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}}
	if in == nil {
		return out
	}

	for name, stringMatch := range in.Headers {
		matcher := translateHeaderMatch(name, stringMatch)
		out.Headers = append(out.Headers, &matcher)
	}

	// guarantee ordering of headers
	sort.Slice(out.Headers, func(i, j int) bool {
		if out.Headers[i].Name == out.Headers[j].Name {
			return out.Headers[i].Value < out.Headers[j].Value
		}
		return out.Headers[i].Name < out.Headers[j].Name
	})

	if in.Uri != nil {
		switch m := in.Uri.MatchType.(type) {
		case *networking.StringMatch_Exact:
			out.PathSpecifier = &route.RouteMatch_Path{Path: m.Exact}
		case *networking.StringMatch_Prefix:
			out.PathSpecifier = &route.RouteMatch_Prefix{Prefix: m.Prefix}
		case *networking.StringMatch_Regex:
			out.PathSpecifier = &route.RouteMatch_Regex{Regex: m.Regex}
		}
	}

	if in.Method != nil {
		matcher := translateHeaderMatch(HeaderMethod, in.Method)
		out.Headers = append(out.Headers, &matcher)
	}

	if in.Authority != nil {
		matcher := translateHeaderMatch(HeaderAuthority, in.Authority)
		out.Headers = append(out.Headers, &matcher)
	}

	if in.Scheme != nil {
		matcher := translateHeaderMatch(HeaderScheme, in.Scheme)
		out.Headers = append(out.Headers, &matcher)
	}

	return out
}

// translateHeaderMatch translates to HeaderMatcher
func translateHeaderMatch(name string, in *networking.StringMatch) route.HeaderMatcher {
	out := route.HeaderMatcher{
		Name: name,
	}

	switch m := in.MatchType.(type) {
	case *networking.StringMatch_Exact:
		out.Value = m.Exact
	case *networking.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		out.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		out.Regex = &types.BoolValue{Value: true}
	case *networking.StringMatch_Regex:
		out.Value = m.Regex
		out.Regex = &types.BoolValue{Value: true}
	}

	return out
}

// translateRetryPolicy translates retry policy
func translateRetryPolicy(in *networking.HTTPRetry) *route.RouteAction_RetryPolicy {
	if in != nil && in.Attempts > 0 {
		return &route.RouteAction_RetryPolicy{
			NumRetries:    &types.UInt32Value{Value: uint32(in.GetAttempts())},
			RetryOn:       "5xx,connect-failure,refused-stream",
			PerTryTimeout: translateTime(in.PerTryTimeout),
		}
	}
	return nil
}

// translateCORSPolicy translates CORS policy
func translateCORSPolicy(in *networking.CorsPolicy) *route.CorsPolicy {
	if in == nil {
		return nil
	}

	out := route.CorsPolicy{
		AllowOrigin: in.AllowOrigin,
		Enabled:     &types.BoolValue{Value: true},
	}
	out.AllowCredentials = in.AllowCredentials
	out.AllowHeaders = strings.Join(in.AllowHeaders, ",")
	out.AllowMethods = strings.Join(in.AllowMethods, ",")
	out.ExposeHeaders = strings.Join(in.ExposeHeaders, ",")
	if in.MaxAge != nil {
		out.MaxAge = in.MaxAge.String()
	}
	return &out
}

// translateTime converts time protos.
func translateTime(in *types.Duration) *time.Duration {
	if in == nil {
		return nil
	}
	out, err := types.DurationFromProto(in)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", in, err)
	}
	return &out
}

// buildDefaultHTTPRoute builds a default route.
func buildDefaultHTTPRoute(clusterName string) *route.Route {
	return &route.Route{
		Match: translateRouteMatch(nil),
		Decorator: &route.Decorator{
			Operation: DefaultRoute,
		},
		Action: &route.Route_Route{
			Route: &route.RouteAction{
				ClusterSpecifier: &route.RouteAction_Cluster{Cluster: clusterName},
			},
		},
	}
}

// buildSidecarInboundHTTPRouteConfig builds the route config with a single wildcard virtual host on the inbound path
// TODO: enable websockets, trace decorators
func (configgen *ConfigGeneratorImpl) buildSidecarInboundHTTPRouteConfig(env model.Environment,
	node model.Proxy, instance *model.ServiceInstance) *xdsapi.RouteConfiguration {

	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, "",
		instance.Service.Hostname, instance.Endpoint.ServicePort)
	defaultRoute := buildDefaultHTTPRoute(clusterName)

	inboundVHost := route.VirtualHost{
		Name:    fmt.Sprintf("%s|http|%d", model.TrafficDirectionInbound, instance.Endpoint.ServicePort.Port),
		Domains: []string{"*"},
		Routes:  []route.Route{*defaultRoute},
	}

	r := &xdsapi.RouteConfiguration{
		Name:             clusterName,
		VirtualHosts:     []route.VirtualHost{inboundVHost},
		ValidateClusters: &types.BoolValue{Value: false},
	}

	// call plugins
	for _, p := range configgen.Plugins {
		p.OnInboundRoute(env, node, instance.Service, instance.Endpoint.ServicePort, r)
	}
	return r
}

// BuildSidecarOutboundHTTPRouteConfig generates outbound routes
func (configgen *ConfigGeneratorImpl) BuildSidecarOutboundHTTPRouteConfig(env model.Environment, node model.Proxy,
	proxyInstances []*model.ServiceInstance, services []*model.Service, routeName string) *xdsapi.RouteConfiguration {

	port := 0
	if routeName != RDSHttpProxy {
		var err error
		port, err = strconv.Atoi(routeName)
		if err != nil {
			return nil
		}
	}

	nameToServiceMap := make(map[string]*model.Service)
	for _, svc := range services {
		if port == 0 {
			nameToServiceMap[svc.Hostname] = svc
		} else {
			if svcPort, exists := svc.Ports.GetByPort(port); exists {
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
	virtualServices := env.VirtualServices([]string{model.IstioMeshGateway})
	guardedHosts := translateVirtualHosts(virtualServices, nameToServiceMap, proxyLabels, model.IstioMeshGateway)
	vHostPortMap := make(map[int][]route.VirtualHost)

	for _, guardedHost := range guardedHosts {
		// If none of the routes matched by source, skip this guarded host
		if len(guardedHost.Routes) == 0 {
			continue
		}

		virtualHosts := make([]route.VirtualHost, 0, len(guardedHost.Hosts)+len(guardedHost.Services))
		for _, host := range guardedHost.Hosts {
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    fmt.Sprintf("%s:%d", host, guardedHost.Port),
				Domains: []string{host},
				Routes:  guardedHost.Routes,
			})
		}

		for _, svc := range guardedHost.Services {
			domains := []string{svc.Hostname, fmt.Sprintf("%s:%d", svc.Hostname, guardedHost.Port)}
			if !svc.MeshExternal {
				domains = append(domains, generateAltVirtualHosts(svc.Hostname, guardedHost.Port)...)
			}
			if len(svc.Address) > 0 {
				// add a vhost match for the IP (if its non CIDR)
				cidr := util.ConvertAddressToCidr(svc.Address)
				if cidr.PrefixLen.Value == 32 {
					domains = append(domains, svc.Address)
					domains = append(domains, fmt.Sprintf("%s:%d", svc.Address, guardedHost.Port))
				}
			}
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    fmt.Sprintf("%s:%d", svc.Hostname, guardedHost.Port),
				Domains: domains,
				Routes:  guardedHost.Routes,
			})
		}

		vHostPortMap[guardedHost.Port] = append(vHostPortMap[guardedHost.Port], virtualHosts...)
	}

	var virtualHosts []route.VirtualHost
	if routeName == RDSHttpProxy {
		virtualHosts = mergeAllVirtualHosts(vHostPortMap)
	} else {
		virtualHosts = vHostPortMap[port]
	}

	out := &xdsapi.RouteConfiguration{
		Name:             fmt.Sprintf("%d", port),
		VirtualHosts:     virtualHosts,
		ValidateClusters: &types.BoolValue{Value: false},
	}

	// call plugins
	for _, p := range configgen.Plugins {
		p.OnOutboundRoute(env, node, out)
	}

	return out
}

// Given a service, and a port, this function generates all possible HTTP Host headers.
// For example, a service of the form foo.local.campus.net on port 80 could be accessed as
// http://foo:80 within the .local network, as http://foo.local:80 (by other clients in the campus.net domain),
// as http://foo.local.campus:80, etc.
func generateAltVirtualHosts(hostname string, port int) []string {
	var vhosts []string
	for i := len(hostname) - 1; i >= 0; i-- {
		if hostname[i] == '.' {
			variant := hostname[:i]
			variantWithPort := fmt.Sprintf("%s:%d", variant, port)
			vhosts = append(vhosts, variant)
			vhosts = append(vhosts, variantWithPort)
		}
	}
	return vhosts
}

// mergeAllVirtualHosts across all ports. On routes for ports other than port 80,
// virtual hosts without an explicit port suffix (IP:PORT) should be stripped
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
