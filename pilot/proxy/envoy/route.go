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

package envoy

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/golang/protobuf/ptypes/duration"

	routing "istio.io/api/routing/v1alpha1"
	routing_v1alpha2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/proxy"
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
		CertChainFile:            path.Join(certsDir, proxy.CertChainFilename),
		PrivateKeyFile:           path.Join(certsDir, proxy.KeyFilename),
		CaCertFile:               path.Join(certsDir, proxy.RootCertFilename),
		RequireClientCertificate: true,
	}
}

// buildClusterSSLContext returns an SSLContextWithSAN struct with VerifySubjectAltName.
// The list of service accounts may be empty but not nil.
func buildClusterSSLContext(certsDir string, serviceAccounts []string) *SSLContextWithSAN {
	return &SSLContextWithSAN{
		CertChainFile:        path.Join(certsDir, proxy.CertChainFilename),
		PrivateKeyFile:       path.Join(certsDir, proxy.KeyFilename),
		CaCertFile:           path.Join(certsDir, proxy.RootCertFilename),
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

func buildOutboundCluster(hostname string, port *model.Port, labels model.Labels) *Cluster {
	svc := model.Service{Hostname: hostname}
	key := svc.Key(port, labels)
	name := truncateClusterName(OutboundClusterPrefix + key)

	cluster := &Cluster{
		Name:        name,
		ServiceName: key,
		Type:        SDSName,
		LbType:      DefaultLbType,
		outbound:    true,
		hostname:    hostname,
		port:        port,
		tags:        labels,
	}

	if port.Protocol == model.ProtocolGRPC || port.Protocol == model.ProtocolHTTP2 {
		cluster.Features = ClusterFeatureHTTP2
	}
	return cluster
}

// buildHTTPRoute translates a route rule to an Envoy route
func buildHTTPRoute(config model.Config, service *model.Service, port *model.Port) []*HTTPRoute {
	switch config.Spec.(type) {
	case *routing.RouteRule:
		return []*HTTPRoute{buildHTTPRouteV1Alpha1(config, service, port)}
	case *routing_v1alpha2.RouteRule:
		return buildHTTPRouteV1Alpha2(config, service, port)
	default:
		panic("unsupported rule")
	}
}

func buildHTTPRouteV1Alpha1(config model.Config, service *model.Service, port *model.Port) *HTTPRoute {
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
			cluster := buildOutboundCluster(actualDestination, port, dst.Labels)
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
		cluster := buildOutboundCluster(destination, port, nil)
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
			if fault := buildHTTPFaultFilterV1Alpha1(c.Name, rule.HttpFault, route.Headers); fault != nil {
				route.faults = append(route.faults, fault)
			}
		}
	}

	if rule.Mirror != nil {
		route.ShadowCluster = &ShadowCluster{
			Cluster: model.ResolveHostname(config.ConfigMeta, rule.Mirror),
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

func buildHTTPRouteV1Alpha2(config model.Config, service *model.Service, port *model.Port) []*HTTPRoute {
	rule := config.Spec.(*routing_v1alpha2.RouteRule)
	routes := make([]*HTTPRoute, 0)

	defaultDestination := service.Hostname
	for _, http := range rule.Http {
		matchRoutes := buildHTTPRouteMatches(http.Match)
		for _, route := range matchRoutes {
			// TODO: logic in this block is not dependent on the route match, so we may be able to avoid
			// rerunning it N times

			route.WeightedClusters = &WeightedCluster{}
			for _, dst := range http.Route {
				destination := defaultDestination
				if dst.Destination != nil {
					destination = model.ResolveFQDN(config.ConfigMeta, dst.Destination.Name)
				}
				cluster := buildOutboundCluster(destination, port, dst.Destination.Labels) // TODO: support Destination.Port
				route.clusters = append(route.clusters, cluster)
				route.WeightedClusters.Clusters = append(route.WeightedClusters.Clusters,
					&WeightedClusterEntry{
						Name:   cluster.Name,
						Weight: int(dst.Weight),
					})
			}

			// TODO: rewrite to a single cluster if it's one weighted cluster
			//if len(http.Route) == 1 {
			//	route.Cluster = route.WeightedClusters.Clusters[0].Name
			//	route.WeightedClusters = nil
			//}

			// setup timeouts for the route
			if http.Timeout != nil &&
				protoDurationToMS(http.Timeout) > 0 {
				route.TimeoutMS = protoDurationToMS(http.Timeout)
			}

			// setup retries
			if http.Retries != nil &&
				http.Retries.Attempts > 0 {
				route.RetryPolicy = &RetryPolicy{
					NumRetries: int(http.Retries.GetAttempts()),
					Policy:     "5xx,connect-failure,refused-stream",
				}
				if protoDurationToMS(http.Retries.PerTryTimeout) > 0 {
					route.RetryPolicy.PerTryTimeoutMS = protoDurationToMS(http.Retries.PerTryTimeout)
				}
			}

			if http.Redirect != nil {
				route.HostRedirect = http.Redirect.Authority
				route.PathRedirect = http.Redirect.Uri
				route.Cluster = ""
			}

			if http.Rewrite != nil {
				route.HostRewrite = http.Rewrite.Authority
				route.PrefixRewrite = http.Rewrite.Uri
			}

			// Add the fault filters, one per cluster defined in weighted cluster or cluster
			if http.Fault != nil {
				route.faults = make([]*HTTPFilter, 0, len(route.clusters))
				for _, c := range route.clusters {
					if fault := buildHTTPFaultFilterV1Alpha2(c.Name, http.Fault, route.Headers); fault != nil {
						route.faults = append(route.faults, fault)
					}
				}
			}

			if http.Mirror != nil {
				route.ShadowCluster = &ShadowCluster{
					Cluster: model.ResolveFQDN(config.ConfigMeta, http.Mirror.Name),
				}
			}

			for name, val := range http.AppendHeaders {
				route.HeadersToAdd = append(route.HeadersToAdd, AppendedHeader{
					Key:   name,
					Value: val,
				})
			}

			if http.CorsPolicy != nil {
				route.CORSPolicy = &CORSPolicy{
					AllowOrigin: http.CorsPolicy.AllowOrigin,
					Enabled:     true,
				}
				if http.CorsPolicy.AllowCredentials != nil {
					route.CORSPolicy.AllowCredentials = http.CorsPolicy.AllowCredentials.Value
				}
				if len(http.CorsPolicy.AllowHeaders) > 0 {
					route.CORSPolicy.AllowHeaders = strings.Join(http.CorsPolicy.AllowHeaders, ",")
				}
				if len(http.CorsPolicy.AllowMethods) > 0 {
					route.CORSPolicy.AllowMethods = strings.Join(http.CorsPolicy.AllowMethods, ",")
				}
				if len(http.CorsPolicy.ExposeHeaders) > 0 {
					route.CORSPolicy.ExposeHeaders = strings.Join(http.CorsPolicy.ExposeHeaders, ",")
				}
				if http.CorsPolicy.MaxAge != nil {
					route.CORSPolicy.MaxAge = http.CorsPolicy.MaxAge.String()
				}
			}

			if http.WebsocketUpgrade {
				route.WebsocketUpgrade = true
			}

			route.Decorator = buildDecorator(config)
		}

		routes = append(routes, matchRoutes...)
	}
	if len(rule.Http) == 0 { // TODO: how do we setup the default cluster? could be TCP
		route := &HTTPRoute{}
		// default route for the destination
		cluster := buildOutboundCluster(defaultDestination, port, nil)
		route.Cluster = cluster.Name
		route.clusters = append(route.clusters, cluster)
		routes = append(routes, route)
	}

	return routes
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
		Name:             OutboundClusterPrefix + name,
		Type:             ClusterTypeOriginalDST,
		ConnectTimeoutMs: protoDurationToMS(timeout),
		LbType:           LbTypeOriginalDST,
		outbound:         true,
	}
}
