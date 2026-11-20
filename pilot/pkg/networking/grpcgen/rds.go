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

package grpcgen

import (
	"net"
	"strconv"
	"strings"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/util/sets"
)

// routeRequest is a parsed route name, "outbound|port|subset|hostname".
type routeRequest struct {
	name     string
	hostname host.Name
	port     int
}

// BuildHTTPRoutes supports per-VIP routes, as used by GRPC.
// This mode is indicated by using names containing full host:port instead of just port.
// Clients subscribe to one route configuration per hostname, so virtual hosts are built once per
// port for the requested hostnames only, then split by name.
func (g *GrpcConfigGenerator) BuildHTTPRoutes(node *model.Proxy, push *model.PushContext, routeNames []string) model.Resources {
	requests := make([]routeRequest, 0, len(routeNames))
	hostsByPort := make(map[int]sets.Set[host.Name])
	for _, routeName := range routeNames {
		// TODO use route-style naming instead of cluster naming
		_, _, hostname, port := model.ParseSubsetKey(routeName)
		if hostname == "" || port == 0 {
			log.Warnf("failed to parse %v", routeName)
			continue
		}
		requests = append(requests, routeRequest{name: routeName, hostname: hostname, port: port})
		sets.InsertOrNew(hostsByPort, port, host.Name(strings.ToLower(string(hostname))))
	}

	virtualHostsByPort := make(map[int][]*route.VirtualHost, len(hostsByPort))
	for _, r := range requests {
		if _, built := virtualHostsByPort[r.port]; !built {
			virtualHostsByPort[r.port], _, _ = core.BuildSidecarOutboundVirtualHosts(node, push, r.name, r.port, nil, &model.DisabledCache{}, hostsByPort[r.port])
		}
	}

	resp := make(model.Resources, 0, len(requests))
	var fullVirtualHostsByPort map[int][]*route.VirtualHost
	for _, r := range requests {
		virtualHosts := filterVirtualHostsForHostname(virtualHostsByPort[r.port], string(r.hostname), r.port)
		if len(virtualHosts) == 0 {
			// Fall back to every service on the port for names that match only an alternate
			// domain, such as an address or short name.
			if fullVirtualHostsByPort == nil {
				fullVirtualHostsByPort = make(map[int][]*route.VirtualHost)
			}
			full, built := fullVirtualHostsByPort[r.port]
			if !built {
				full, _, _ = core.BuildSidecarOutboundVirtualHosts(node, push, r.name, r.port, nil, &model.DisabledCache{}, nil)
				fullVirtualHostsByPort[r.port] = full
			}
			virtualHosts = filterVirtualHostsForHostname(full, string(r.hostname), r.port)
		}
		resp = append(resp, &discovery.Resource{
			Name: r.name,
			Resource: protoconv.MessageToAny(&route.RouteConfiguration{
				Name:         r.name,
				VirtualHosts: virtualHosts,
			}),
		})
	}
	return resp
}

// filterVirtualHostsForHostname returns only the virtual hosts whose domains contain the given
// hostname or hostname:port. Wildcard domains are matched using host.Name semantics. Uses the same
// formatting as domain generation (util.IPv6Compliant, util.DomainName) to handle IPv6 addresses
// correctly.
func filterVirtualHostsForHostname(
	virtualHosts []*route.VirtualHost,
	hostname string,
	port int,
) []*route.VirtualHost {
	var (
		h            = strings.ToLower(hostname)
		wantHost     = util.IPv6Compliant(h)
		wantHostPort = util.DomainName(h, port)
		wantHostName = host.Name(h)
		wantPort     = strconv.Itoa(port)
		filtered     []*route.VirtualHost
	)

	for _, vh := range virtualHosts {
		for _, d := range vh.Domains {
			domain := strings.ToLower(d)
			if domain == wantHost || domain == wantHostPort {
				filtered = append(filtered, vh)
				break
			}

			domainHost, domainPort := domain, ""
			if strings.Contains(domain, ":") {
				if h, p, err := net.SplitHostPort(domain); err == nil {
					domainHost, domainPort = h, p
				}
			}

			domainHostName := host.Name(domainHost)
			if domainHostName.IsWildCarded() {
				if domainPort != "" && domainPort != wantPort {
					continue
				}
				if domainHostName.Matches(wantHostName) {
					filtered = append(filtered, vh)
					break
				}
			}

			if domainHost == wantHost && (domainPort == "" || domainPort == wantPort) {
				filtered = append(filtered, vh)
				break
			}
		}
	}

	return filtered
}
