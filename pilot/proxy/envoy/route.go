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
	"sort"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/ptypes/duration"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
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
		CertChainFile:  certsDir + "/cert-chain.pem",
		PrivateKeyFile: certsDir + "/key.pem",
		CaCertFile:     certsDir + "/root-cert.pem",
	}
}

// buildClusterSSLContext returns an SSLContextWithSAN struct with VerifySubjectAltName.
// The list of service accounts may be empty but not nil.
func buildClusterSSLContext(certsDir string, serviceAccounts []string) *SSLContextWithSAN {
	return &SSLContextWithSAN{
		CertChainFile:        certsDir + "/cert-chain.pem",
		PrivateKeyFile:       certsDir + "/key.pem",
		CaCertFile:           certsDir + "/root-cert.pem",
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

func buildInboundCluster(port int, protocol model.Protocol, timeout *duration.Duration) *Cluster {
	cluster := &Cluster{
		Name:             fmt.Sprintf("%s%d", InboundClusterPrefix, port),
		Type:             "static",
		ConnectTimeoutMs: int(convertDuration(timeout) / time.Millisecond),
		LbType:           DefaultLbType,
		Hosts:            []Host{{URL: fmt.Sprintf("tcp://%s:%d", "127.0.0.1", port)}},
	}
	if protocol == model.ProtocolGRPC || protocol == model.ProtocolHTTP2 {
		cluster.Features = "http2"
	}
	return cluster
}

func buildOutboundCluster(hostname string, port *model.Port, tags model.Tags) *Cluster {
	svc := model.Service{Hostname: hostname}
	key := svc.Key(port, tags)
	cluster := &Cluster{
		Name:        OutboundClusterPrefix + key,
		ServiceName: key,
		Type:        "sds",
		LbType:      DefaultLbType,
		hostname:    hostname,
		port:        port,
		tags:        tags,
	}
	if port.Protocol == model.ProtocolGRPC || port.Protocol == model.ProtocolHTTP2 {
		cluster.Features = "http2"
	}
	return cluster
}

// buildHTTPRoute translates a route rule to an Envoy route
func buildHTTPRoute(rule *proxyconfig.RouteRule, port *model.Port) (*HTTPRoute, bool) {
	route := &HTTPRoute{
		Path:   "",
		Prefix: "/",
	}

	catchAll := true

	// setup timeouts for the route
	if rule.HttpReqTimeout != nil &&
		rule.HttpReqTimeout.GetSimpleTimeout() != nil &&
		rule.HttpReqTimeout.GetSimpleTimeout().TimeoutSeconds > 0 {
		// convert from float sec to ms
		route.TimeoutMS = int(rule.HttpReqTimeout.GetSimpleTimeout().TimeoutSeconds * 1000)
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
	}

	if rule.Match != nil {
		route.Headers = buildHeaders(rule.Match.HttpHeaders)

		if uri, ok := rule.Match.HttpHeaders[HeaderURI]; ok {
			switch m := uri.MatchType.(type) {
			case *proxyconfig.StringMatch_Exact:
				route.Path = m.Exact
				route.Prefix = ""
			case *proxyconfig.StringMatch_Prefix:
				route.Path = ""
				route.Prefix = m.Prefix
			case *proxyconfig.StringMatch_Regex:
				glog.Warningf("Unsupported route match condition: regex")
			}
		}

		if len(route.Headers) > 0 || route.Path != "" || route.Prefix != "/" {
			catchAll = false
		}
	}

	if len(rule.Route) > 0 {
		clusters := make([]*WeightedClusterEntry, 0)
		for _, dst := range rule.Route {
			destination := dst.Destination

			// fallback to rule destination
			if destination == "" {
				destination = rule.Destination
			}

			cluster := buildOutboundCluster(destination, port, dst.Tags)
			clusters = append(clusters, &WeightedClusterEntry{
				Name:   cluster.Name,
				Weight: int(dst.Weight),
			})
			route.clusters = append(route.clusters, cluster)
		}
		route.WeightedClusters = &WeightedCluster{Clusters: clusters}

		// rewrite to a single cluster if it's one weighted cluster
		if len(rule.Route) == 1 {
			route.Cluster = route.WeightedClusters.Clusters[0].Name
			route.WeightedClusters = nil
		}
	} else {
		route.WeightedClusters = nil
		// default route for the destination
		cluster := buildOutboundCluster(rule.Destination, port, nil)
		route.Cluster = cluster.Name
		route.clusters = make([]*Cluster, 0)
		route.clusters = append(route.clusters, cluster)
	}

	// Add the fault filters, one per cluster defined in weighted cluster or cluster
	if rule.HttpFault != nil {
		route.faults = make([]*HTTPFilter, 0)
		for _, c := range route.clusters {
			route.faults = append(route.faults, buildHTTPFaultFilter(c.Name, rule.HttpFault, route.Headers))
		}
	}

	return route, catchAll
}

func buildDiscoveryCluster(address, name string, timeout *duration.Duration) *Cluster {
	return &Cluster{
		Name:             name,
		Type:             "strict_dns",
		ConnectTimeoutMs: int(convertDuration(timeout) / time.Millisecond),
		LbType:           DefaultLbType,
		Hosts: []Host{
			{
				URL: "tcp://" + address,
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

		// default port 80 does not need to be specified
		if port.Port == 80 {
			domains = append(domains, host)
		}
	}

	return &VirtualHost{
		Name:    svc.Key(port, nil),
		Domains: domains,
		Routes:  routes,
	}
}

// sharedInstanceHost computes the shared subdomain suffix for co-located instances
func sharedInstanceHost(instances []*model.ServiceInstance) []string {
	hostnames := make([][]string, 0)
	for _, instance := range instances {
		hostnames = append(hostnames, strings.Split(instance.Service.Hostname, "."))
	}
	return sharedHost(hostnames...)
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
