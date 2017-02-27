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
	"strings"

	"github.com/golang/glog"

	"istio.io/manager/model"
	proxyconfig "istio.io/manager/model/proxy/alphav1/config"
)

const (
	// InboundClusterPrefix is the prefix for service clusters co-hosted on the proxy instance
	InboundClusterPrefix = "inbound:"

	// OutboundClusterPrefix is the prefix for service clusters external to the proxy instance
	OutboundClusterPrefix = "outbound:"
)

func buildDefaultRoute(cluster *Cluster) *HTTPRoute {
	return &HTTPRoute{
		Prefix:   "/",
		Cluster:  cluster.Name,
		clusters: []*Cluster{cluster},
	}
}

func buildInboundCluster(port int, protocol model.Protocol) *Cluster {
	cluster := &Cluster{
		Name:             fmt.Sprintf("%s%d", InboundClusterPrefix, port),
		Type:             "static",
		ConnectTimeoutMs: DefaultTimeoutMs,
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
		Name:             OutboundClusterPrefix + key,
		ServiceName:      key,
		Type:             "sds",
		LbType:           DefaultLbType,
		ConnectTimeoutMs: DefaultTimeoutMs,
		hostname:         hostname,
		port:             port,
		tags:             tags,
	}
	if port.Protocol == model.ProtocolGRPC || port.Protocol == model.ProtocolHTTP2 {
		cluster.Features = "http2"
	}
	return cluster
}

// buildHTTPRoute translates a route rule to an Envoy route
func buildHTTPRoute(rule *proxyconfig.RouteRule, port *model.Port) *HTTPRoute {
	route := &HTTPRoute{
		Path:   "",
		Prefix: "/",
	}

	if rule.Match != nil {
		route.Headers = buildHeaders(rule.Match.Http)

		if uri, ok := rule.Match.Http[HeaderURI]; ok {
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
	}

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

	return route
}

func buildSDSCluster(mesh *MeshConfig) *Cluster {
	return &Cluster{
		Name:             "sds",
		Type:             "strict_dns",
		ConnectTimeoutMs: DefaultTimeoutMs,
		LbType:           DefaultLbType,
		Hosts: []Host{
			{
				URL: "tcp://" + mesh.DiscoveryAddress,
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
