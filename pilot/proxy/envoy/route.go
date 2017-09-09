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
	"crypto/sha1"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/golang/protobuf/ptypes/duration"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
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
	}
}

func buildInboundWebsocketRoute(rule *proxyconfig.RouteRule, cluster *Cluster) *HTTPRoute {
	route := buildHTTPRouteMatch(rule.Match)
	route.Cluster = cluster.Name
	route.clusters = []*Cluster{cluster}
	route.WebsocketUpgrade = true
	if rule.Rewrite != nil && rule.Rewrite.GetUri() != "" {
		// overwrite the computed prefix with the rewritten prefix,
		// for this is what we expect from remote envoys
		route.Prefix = rule.Rewrite.GetUri()
		route.Path = ""
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

	// cluster name must be below 60 characters
	cluster := &Cluster{
		Name:        OutboundClusterPrefix + fmt.Sprintf("%x", sha1.Sum([]byte(key))),
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
func buildHTTPRoute(config model.Config, service *model.Service, port *model.Port) *HTTPRoute {
	rule := config.Spec.(*proxyconfig.RouteRule)
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
			// TODO: add destination service override from the route weights

			cluster := buildOutboundCluster(destination, port, dst.Labels)
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
			if fault := buildHTTPFaultFilter(c.Name, rule.HttpFault, route.Headers); fault != nil {
				route.faults = append(route.faults, fault)
			}
		}
	}

	if rule.WebsocketUpgrade {
		route.WebsocketUpgrade = true
	}

	return route
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
		route.DestinationIPList = append(route.DestinationIPList, addr+"/32")
	}
	return route
}

func buildOriginalDSTCluster(name string, timeout *duration.Duration) *Cluster {
	return &Cluster{
		Name:             OutboundClusterPrefix + name,
		Type:             ClusterTypeOriginalDST,
		ConnectTimeoutMs: protoDurationToMS(timeout),
		LbType:           LbTypeOriginalDST,
	}
}
