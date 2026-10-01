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
)

// BuildHTTPRoutes generates per-host routes for proxyless gRPC clients.
func (g *GrpcConfigGenerator) BuildHTTPRoutes(node *model.Proxy, push *model.PushContext, routeNames []string) model.Resources {
	resp := model.Resources{}
	remainingRoutes := make(map[int]int)
	for _, routeName := range routeNames {
		_, _, hostname, port := model.ParseSubsetKey(routeName)
		if hostname != "" && port != 0 {
			remainingRoutes[port]++
		}
	}
	// Nonzero ports select the same egress listener regardless of the route name.
	// Keep their virtual hosts only until the last request for each port.
	virtualHostsByPort := make(map[int][]*route.VirtualHost)
	for _, routeName := range routeNames {
		// TODO use route-style naming instead of cluster naming
		_, _, hostname, port := model.ParseSubsetKey(routeName)
		if hostname == "" || port == 0 {
			log.Warnf("failed to parse %v", routeName)
			continue
		}
		virtualHosts, found := virtualHostsByPort[port]
		if !found {
			virtualHosts, _, _ = core.BuildSidecarOutboundVirtualHosts(node, push, routeName, port, nil, &model.DisabledCache{})
		}
		remainingRoutes[port]--
		if remainingRoutes[port] == 0 {
			delete(virtualHostsByPort, port)
		} else if !found {
			virtualHostsByPort[port] = virtualHosts
		}
		// Limit each route configuration to its hostname to avoid churn from unrelated services.
		rc := &route.RouteConfiguration{
			Name:         routeName,
			VirtualHosts: filterVirtualHostsForHostname(virtualHosts, string(hostname), port),
		}
		resp = append(resp, &discovery.Resource{
			Name:     routeName,
			Resource: protoconv.MessageToAny(rc),
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

			domainHost, domainPort, err := net.SplitHostPort(domain)
			if err != nil {
				domainHost = domain
				domainPort = ""
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
