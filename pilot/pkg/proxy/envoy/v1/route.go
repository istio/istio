// Copyright 2017 Istio Authors
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

// Functions related to data-path routes in Envoy config: virtual hosts, clusters,
// routes.

package v1

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/golang/protobuf/ptypes/duration"

	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// InboundClusterPrefix is the prefix for service clusters co-hosted on the proxy instance
	InboundClusterPrefix = "in."

	// OutboundClusterPrefix is the prefix for service clusters external to the proxy instance
	OutboundClusterPrefix = "out."
)

// buildListenerSSLContext returns an SSLContext struct.
func buildListenerSSLContext(certsDir string) *SSLContext {
	return &SSLContext{
		CertChainFile:            path.Join(certsDir, model.CertChainFilename),
		PrivateKeyFile:           path.Join(certsDir, model.KeyFilename),
		CaCertFile:               path.Join(certsDir, model.RootCertFilename),
		RequireClientCertificate: true,
	}
}

// buildClusterSSLContext returns an SSLContextWithSAN struct with VerifySubjectAltName.
// The list of service accounts may be empty but not nil.
func buildClusterSSLContext(certsDir string, serviceAccounts []string) *SSLContextWithSAN {
	return &SSLContextWithSAN{
		CertChainFile:        path.Join(certsDir, model.CertChainFilename),
		PrivateKeyFile:       path.Join(certsDir, model.KeyFilename),
		CaCertFile:           path.Join(certsDir, model.RootCertFilename),
		VerifySubjectAltName: serviceAccounts,
	}
}

func buildDefaultRoute(cluster *Cluster) *HTTPRoute {
	return &HTTPRoute{
		Prefix:   "/",
		Cluster:  cluster.Name,
		clusters: []*Cluster{cluster},
		Decorator: &Decorator{
			Operation: "default-route",
		},
	}
}

func buildInboundRoute(config model.Config, rule *routing.RouteRule, cluster *Cluster) *HTTPRoute {
	route := buildHTTPRouteMatch(rule.Match)
	route.Cluster = cluster.Name
	route.clusters = []*Cluster{cluster}
	route.WebsocketUpgrade = rule.WebsocketUpgrade
	if rule.Rewrite != nil && rule.Rewrite.GetUri() != "" {
		// overwrite the computed prefix with the rewritten prefix,
		// for this is what we expect from remote envoys
		route.Prefix = rule.Rewrite.GetUri()
		route.Path = ""
	}

	if !rule.WebsocketUpgrade {
		route.Decorator = buildDecorator(config)
	}

	return route
}

func buildInboundRoutesV2(instances []*model.ServiceInstance, config model.Config, rule *routingv2.RouteRule, cluster *Cluster) []*HTTPRoute {
	routes := make([]*HTTPRoute, 0)
	for _, http := range rule.Http {
		if len(http.Match) == 0 {
			routes = append(routes, buildInboundRouteV2(config, cluster, http, nil))
		}
		for _, match := range http.Match {
			for _, instance := range instances {
				if model.Labels(match.SourceLabels).SubsetOf(instance.Labels) {
					routes = append(routes, buildInboundRouteV2(config, cluster, http, match))
					break
				}
			}
		}
	}
	return routes
}

func buildInboundRouteV2(config model.Config, cluster *Cluster, http *routingv2.HTTPRoute, match *routingv2.HTTPMatchRequest) *HTTPRoute {
	route := buildHTTPRouteMatchV2(match)

	route.Cluster = cluster.Name
	route.clusters = []*Cluster{cluster}
	route.WebsocketUpgrade = http.WebsocketUpgrade
	if http.Rewrite != nil && http.Rewrite.Uri != "" {
		// overwrite the computed prefix with the rewritten prefix,
		// for this is what we expect from remote envoys
		route.Prefix = http.Rewrite.Uri
		route.Path = ""
	}

	if !http.WebsocketUpgrade {
		route.Decorator = buildDecorator(config)
	}

	return route
}

func buildInboundCluster(port int, protocol model.Protocol, timeout *duration.Duration) *Cluster {
	cluster := &Cluster{
		Name:             fmt.Sprintf("%s%d", InboundClusterPrefix, port),
		Type:             ClusterTypeStatic,
		ConnectTimeoutMs: protoDurationToMS(timeout),
		LbType:           DefaultLbType,
		Hosts:            []Host{{URL: fmt.Sprintf("tcp://%s:%d", "127.0.0.1", port)}},
	}
	if protocol == model.ProtocolGRPC || protocol == model.ProtocolHTTP2 {
		cluster.Features = ClusterFeatureHTTP2
	}
	return cluster
}

func buildOutboundCluster(hostname string, port *model.Port, labels model.Labels, isExternal bool) *Cluster {
	svc := model.Service{Hostname: hostname}
	key := svc.Key(port, labels)
	name := truncateClusterName(OutboundClusterPrefix + key)
	clusterType := ClusterTypeSDS

	if isExternal {
		clusterType = ClusterTypeStrictDNS
	}

	hosts := []Host{}
	if isExternal {
		hosts = []Host{{URL: fmt.Sprintf("tcp://%s:%d", hostname, port.Port)}}
	}

	cluster := &Cluster{
		Name:        name,
		ServiceName: key,
		Type:        clusterType,
		LbType:      DefaultLbType,
		Hosts:       hosts,
		outbound:    !isExternal, // outbound means outbound-in-mesh. The name to be refactored later.
		hostname:    hostname,
		port:        port,
		labels:      labels,
	}

	if port.Protocol == model.ProtocolGRPC || port.Protocol == model.ProtocolHTTP2 {
		cluster.Features = ClusterFeatureHTTP2
	}
	return cluster
}

// buildHTTPRoutes translates a route rule to an Envoy route
func buildHTTPRoutes(store model.IstioConfigStore, config model.Config, service *model.Service,
	port *model.Port, instances []*model.ServiceInstance, domain string, buildCluster buildClusterFunc) []*HTTPRoute {

	switch config.Spec.(type) {
	case *routing.RouteRule:
		return []*HTTPRoute{buildHTTPRouteV1(config, service, port)}
	case *routingv2.RouteRule:
		return buildHTTPRoutesV2(store, config, service, port, instances, domain, buildCluster)
	default:
		panic("unsupported rule")
	}
}

func buildHTTPRouteV1(config model.Config, service *model.Service, port *model.Port) *HTTPRoute {
	rule := config.Spec.(*routing.RouteRule)
	route := buildHTTPRouteMatch(rule.Match)

	// setup timeouts for the route
	if rule.HttpReqTimeout != nil &&
		rule.HttpReqTimeout.GetSimpleTimeout() != nil &&
		protoDurationToMS(rule.HttpReqTimeout.GetSimpleTimeout().Timeout) > 0 {
		route.TimeoutMS = protoDurationToMS(rule.HttpReqTimeout.GetSimpleTimeout().Timeout)
	}

	// setup retries
	if rule.HttpReqRetries != nil &&
		rule.HttpReqRetries.GetSimpleRetry() != nil &&
		rule.HttpReqRetries.GetSimpleRetry().Attempts > 0 {
		route.RetryPolicy = &RetryPolicy{
			NumRetries: int(rule.HttpReqRetries.GetSimpleRetry().Attempts),
			// These are the safest retry policies as per envoy docs
			Policy: "5xx,connect-failure,refused-stream",
		}
		if protoDurationToMS(rule.HttpReqRetries.GetSimpleRetry().PerTryTimeout) > 0 {
			route.RetryPolicy.PerTryTimeoutMS = protoDurationToMS(rule.HttpReqRetries.GetSimpleRetry().PerTryTimeout)
		}
	}

	destination := service.Hostname

	if len(rule.Route) > 0 {
		route.WeightedClusters = &WeightedCluster{}
		for _, dst := range rule.Route {
			actualDestination := destination
			if dst.Destination != nil {
				actualDestination = model.ResolveHostname(config.ConfigMeta, dst.Destination)
			}
			cluster := buildOutboundCluster(actualDestination, port, dst.Labels, service.External())
			route.clusters = append(route.clusters, cluster)
			route.WeightedClusters.Clusters = append(route.WeightedClusters.Clusters, &WeightedClusterEntry{
				Name:   cluster.Name,
				Weight: int(dst.Weight),
			})
		}

		// rewrite to a single cluster if it's one weighted cluster
		if len(rule.Route) == 1 {
			route.Cluster = route.WeightedClusters.Clusters[0].Name
			route.WeightedClusters = nil
		}
	} else {
		// default route for the destination
		cluster := buildOutboundCluster(destination, port, nil, service.External())
		route.Cluster = cluster.Name
		route.clusters = append(route.clusters, cluster)
	}

	if rule.Redirect != nil {
		route.HostRedirect = rule.Redirect.Authority
		route.PathRedirect = rule.Redirect.Uri
		route.Cluster = ""
	}

	if rule.Rewrite != nil {
		route.HostRewrite = rule.Rewrite.Authority
		route.PrefixRewrite = rule.Rewrite.Uri
	}

	// Add the fault filters, one per cluster defined in weighted cluster or cluster
	if rule.HttpFault != nil {
		route.faults = make([]*HTTPFilter, 0, len(route.clusters))
		for _, c := range route.clusters {
			if fault := buildHTTPFaultFilter(c.Name, rule.HttpFault, route.Headers); fault != nil {
				route.faults = append(route.faults, fault)
			}
		}
	}

	if rule.Mirror != nil {
		fqdnDest := model.ResolveHostname(config.ConfigMeta, rule.Mirror)
		route.ShadowCluster = &ShadowCluster{
			//TODO support shadowing between internal and external kubernetes services
			// currently only shadowing between internal kubernetes services is supported
			Cluster: buildOutboundCluster(fqdnDest, port, rule.Mirror.Labels, service.External()).Name,
		}
	}

	for name, val := range rule.AppendHeaders {
		route.HeadersToAdd = append(route.HeadersToAdd, AppendedHeader{
			Key:   name,
			Value: val,
		})
	}

	if rule.CorsPolicy != nil {
		route.CORSPolicy = &CORSPolicy{
			AllowOrigin: rule.CorsPolicy.AllowOrigin,
			Enabled:     true,
		}
		if rule.CorsPolicy.AllowCredentials != nil {
			route.CORSPolicy.AllowCredentials = rule.CorsPolicy.AllowCredentials.Value
		}
		if len(rule.CorsPolicy.AllowHeaders) > 0 {
			route.CORSPolicy.AllowHeaders = strings.Join(rule.CorsPolicy.AllowHeaders, ",")
		}
		if len(rule.CorsPolicy.AllowMethods) > 0 {
			route.CORSPolicy.AllowMethods = strings.Join(rule.CorsPolicy.AllowMethods, ",")
		}
		if len(rule.CorsPolicy.ExposeHeaders) > 0 {
			route.CORSPolicy.ExposeHeaders = strings.Join(rule.CorsPolicy.ExposeHeaders, ",")
		}
		if rule.CorsPolicy.MaxAge != nil {
			route.CORSPolicy.MaxAge = rule.CorsPolicy.MaxAge.String()
		}
	}

	if rule.WebsocketUpgrade {
		route.WebsocketUpgrade = true
	}

	route.Decorator = buildDecorator(config)

	return route
}

func buildHTTPRoutesV2(store model.IstioConfigStore, config model.Config, service *model.Service, port *model.Port,
	instances []*model.ServiceInstance, domain string, buildCluster buildClusterFunc) []*HTTPRoute {

	rule := config.Spec.(*routingv2.RouteRule)
	routes := make([]*HTTPRoute, 0)

	for _, http := range rule.Http {
		if len(http.Match) == 0 {
			routes = append(routes, buildHTTPRouteV2(store, config, service, port, http, nil, domain, buildCluster))
		}
		for _, match := range http.Match {
			for _, instance := range instances {
				if model.Labels(match.SourceLabels).SubsetOf(instance.Labels) {
					routes = append(routes, buildHTTPRouteV2(store, config, service, port, http, match, domain, buildCluster))
					break
				}
			}
		}
	}
	if len(rule.Http) == 0 {
		route := &HTTPRoute{
			Prefix: "/",
		}
		// default route for the destination
		cluster := buildCluster(service.Hostname, port, nil, service.External())
		route.Cluster = cluster.Name
		route.clusters = append(route.clusters, cluster)
		routes = append(routes, route)
	}

	return routes
}

func buildHTTPRouteV2(store model.IstioConfigStore, config model.Config, service *model.Service, port *model.Port,
	http *routingv2.HTTPRoute, match *routingv2.HTTPMatchRequest, domain string, buildCluster buildClusterFunc) *HTTPRoute {

	route := buildHTTPRouteMatchV2(match)

	if len(http.Route) == 0 { // build default cluster
		cluster := buildCluster(service.Hostname, port, nil, service.External())
		route.Cluster = cluster.Name
		route.clusters = append(route.clusters, cluster)
	} else {
		route.WeightedClusters = &WeightedCluster{Clusters: make([]*WeightedClusterEntry, 0, len(http.Route))}
		for _, dst := range http.Route {
			fqdn := model.ResolveFQDN(dst.Destination.Name, domain)
			labels := fetchSubsetLabels(store, fqdn, dst.Destination.Subset, domain)
			cluster := buildCluster(fqdn, port, labels, service.External()) // TODO: support Destination.Port
			route.clusters = append(route.clusters, cluster)
			route.WeightedClusters.Clusters = append(route.WeightedClusters.Clusters,
				&WeightedClusterEntry{
					Name:   cluster.Name,
					Weight: int(dst.Weight),
				})
		}

		// rewrite to a single cluster if there is only weighted cluster
		if len(route.WeightedClusters.Clusters) == 1 {
			route.Cluster = route.WeightedClusters.Clusters[0].Name
			route.WeightedClusters = nil
		}
	}

	if http.Timeout != nil &&
		protoDurationToMS(http.Timeout) > 0 {
		route.TimeoutMS = protoDurationToMS(http.Timeout)
	}

	route.RetryPolicy = buildRetryPolicy(http.Retries)

	applyRedirect(route, http.Redirect)
	applyRewrite(route, http.Rewrite)

	// Add the fault filters, one per cluster defined in weighted cluster or cluster
	if http.Fault != nil {
		route.faults = make([]*HTTPFilter, 0, len(route.clusters))
		for _, cluster := range route.clusters {
			if fault := buildHTTPFaultFilterV2(cluster.Name, http.Fault, route.Headers); fault != nil {
				route.faults = append(route.faults, fault)
			}
		}
	}

	route.ShadowCluster = buildShadowCluster(store, domain, port, http.Mirror, buildCluster) // FIXME: add any new cluster
	route.HeadersToAdd = buildHeadersToAdd(http.AppendHeaders)
	route.CORSPolicy = buildCORSPolicy(http.CorsPolicy)
	route.WebsocketUpgrade = http.WebsocketUpgrade
	route.Decorator = buildDecorator(config)

	return route
}

// TODO: This logic is temporary until we fully switch from v1alpha1 to v1alpha2.
// In v1alpha2, cluster names will be built using the subset name instead of labels.
// This will allow us to remove this function, which is very inefficient.
func fetchSubsetLabels(store model.IstioConfigStore, name, subsetName, domain string) (labels model.Labels) {
	if subsetName == "" {
		return
	}

	destinationRuleConfig := store.DestinationRule(name, domain)
	if destinationRuleConfig != nil {
		destinationRule := destinationRuleConfig.Spec.(*routingv2.DestinationRule)

		var found bool
		for _, subset := range destinationRule.Subsets {
			if subset.Name == subsetName {
				labels = model.Labels(subset.Labels)
				found = true
				break
			}
		}

		if !found {
			log.Warnf("Reference to non-existent subset %q", subsetName)
		}
	}

	return
}

func applyRedirect(route *HTTPRoute, redirect *routingv2.HTTPRedirect) {
	if redirect != nil {
		route.HostRedirect = redirect.Authority
		route.PathRedirect = redirect.Uri
		route.Cluster = ""
	}
}

func buildRetryPolicy(retries *routingv2.HTTPRetry) (policy *RetryPolicy) {
	if retries != nil && retries.Attempts > 0 {
		policy = &RetryPolicy{
			NumRetries: int(retries.GetAttempts()),
			Policy:     "5xx,connect-failure,refused-stream",
		}
		if protoDurationToMS(retries.PerTryTimeout) > 0 {
			policy.PerTryTimeoutMS = protoDurationToMS(retries.PerTryTimeout)
		}
	}
	return
}

func applyRewrite(route *HTTPRoute, rewrite *routingv2.HTTPRewrite) {
	if rewrite != nil {
		route.HostRewrite = rewrite.Authority
		route.PrefixRewrite = rewrite.Uri
	}
}

func buildShadowCluster(store model.IstioConfigStore, domain string, port *model.Port,
	mirror *routingv2.Destination, buildCluster buildClusterFunc) *ShadowCluster {

	if mirror != nil {
		fqdn := model.ResolveFQDN(mirror.Name, domain)
		labels := fetchSubsetLabels(store, fqdn, mirror.Subset, domain)
		// TODO support shadow cluster for external kubernetes service mirror
		return &ShadowCluster{Cluster: buildCluster(fqdn, port, labels, false).Name}
	}
	return nil
}

func buildHeadersToAdd(headers map[string]string) []AppendedHeader {
	out := make([]AppendedHeader, 0, len(headers))
	for name, val := range headers {
		out = append(out, AppendedHeader{
			Key:   name,
			Value: val,
		})
	}
	return out
}

func buildCORSPolicy(policy *routingv2.CorsPolicy) *CORSPolicy {
	if policy == nil {
		return nil
	}

	out := &CORSPolicy{
		AllowOrigin: policy.AllowOrigin,
		Enabled:     true,
	}
	if policy.AllowCredentials != nil {
		out.AllowCredentials = policy.AllowCredentials.Value
	}
	if len(policy.AllowHeaders) > 0 {
		out.AllowHeaders = strings.Join(policy.AllowHeaders, ",")
	}
	if len(policy.AllowMethods) > 0 {
		out.AllowMethods = strings.Join(policy.AllowMethods, ",")
	}
	if len(policy.ExposeHeaders) > 0 {
		out.ExposeHeaders = strings.Join(policy.ExposeHeaders, ",")
	}
	if policy.MaxAge != nil {
		out.MaxAge = policy.MaxAge.String()
	}
	return out
}

func buildCluster(address, name string, timeout *duration.Duration) *Cluster {
	return &Cluster{
		Name:             name,
		Type:             ClusterTypeStrictDNS,
		ConnectTimeoutMs: protoDurationToMS(timeout),
		LbType:           DefaultLbType,
		Hosts: []Host{
			{
				URL: "tcp://" + address,
			},
		},
	}
}

func buildDecorator(config model.Config) *Decorator {
	if config.ConfigMeta.Name != "" {
		return &Decorator{
			Operation: config.ConfigMeta.Name,
		}
	}
	return nil
}

func buildZipkinTracing() *Tracing {
	return &Tracing{
		HTTPTracer: HTTPTracer{
			HTTPTraceDriver: HTTPTraceDriver{
				HTTPTraceDriverType: ZipkinTraceDriverType,
				HTTPTraceDriverConfig: HTTPTraceDriverConfig{
					CollectorCluster:  ZipkinCollectorCluster,
					CollectorEndpoint: ZipkinCollectorEndpoint,
				},
			},
		},
	}
}

// buildVirtualHost constructs an entry for VirtualHost for a destination service.
// The unique name for a virtual host is a combination of the destination service and the port, e.g.
// "svc.ns.svc.cluster.local:http".
// Suffix provides the proxy context information - it is the shared sub-domain between co-located
// service instances (e.g. "namespace", "svc", "cluster", "local")
func buildVirtualHost(svc *model.Service, port *model.Port, suffix []string, routes []*HTTPRoute) *VirtualHost {
	hosts := make([]string, 0)
	domains := make([]string, 0)
	parts := strings.Split(svc.Hostname, ".")
	shared := sharedHost(suffix, parts)

	// if shared is "svc.cluster.local", then we can add "name.namespace", "name.namespace.svc", etc
	host := strings.Join(parts[0:len(parts)-len(shared)], ".")
	if len(host) > 0 {
		hosts = append(hosts, host)
	}

	for _, part := range shared {
		if len(host) > 0 {
			host = host + "."
		}
		host = host + part
		hosts = append(hosts, host)
	}

	// add service cluster IP domain name
	if len(svc.Address) > 0 {
		hosts = append(hosts, svc.Address)
	}

	// add ports
	for _, host := range hosts {
		domains = append(domains, fmt.Sprintf("%s:%d", host, port.Port))

		// since the port on the TCP listener address matches the service port,
		// the colon suffix is optional and is inferred.
		// (see https://tools.ietf.org/html/rfc7230#section-5.5)
		domains = append(domains, host)
	}

	return &VirtualHost{
		Name:    svc.Key(port, nil),
		Domains: domains,
		Routes:  routes,
	}
}

// sharedHost computes the shared host name suffix for instances.
// Each host name is split into its domains.
func sharedHost(parts ...[]string) []string {
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return parts[0]
	default:
		// longest common suffix
		out := make([]string, 0)
		for i := 1; i <= len(parts[0]); i++ {
			part := ""
			all := true
			for j, host := range parts {
				hostpart := host[len(host)-i]
				if j == 0 {
					part = hostpart
				} else if part != hostpart {
					all = false
					break
				}
			}
			if all {
				out = append(out, part)
			} else {
				break
			}
		}

		// reverse
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out
	}
}

func buildTCPRoute(cluster *Cluster, addresses []string) *TCPRoute {
	// destination port is unnecessary with use_original_dst since
	// the listener address already contains the port
	route := &TCPRoute{
		Cluster:    cluster.Name,
		clusterRef: cluster,
	}
	sort.Sort(sort.StringSlice(addresses))
	for _, addr := range addresses {
		tcpRouteAddr := addr
		if !strings.Contains(addr, "/") {
			tcpRouteAddr = addr + "/32"
		}
		route.DestinationIPList = append(route.DestinationIPList, tcpRouteAddr)
	}
	return route
}

func buildOriginalDSTCluster(name string, timeout *duration.Duration) *Cluster {
	return &Cluster{
		Name:             truncateClusterName(OutboundClusterPrefix + name),
		Type:             ClusterTypeOriginalDST,
		ConnectTimeoutMs: protoDurationToMS(timeout),
		LbType:           LbTypeOriginalDST,
		outbound:         true,
	}
}
