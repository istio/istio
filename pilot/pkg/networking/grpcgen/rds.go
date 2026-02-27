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

// BuildHTTPRoutes supports per-VIP routes, as used by GRPC.
// This mode is indicated by using names containing full host:port instead of just port.
// Returns true of the request is of this type.
func (g *GrpcConfigGenerator) BuildHTTPRoutes(node *model.Proxy, push *model.PushContext, routeNames []string) model.Resources {
	resp := model.Resources{}
	for _, routeName := range routeNames {
		if rc := buildHTTPRoute(node, push, routeName); rc != nil {
			resp = append(resp, &discovery.Resource{
				Name:     routeName,
				Resource: protoconv.MessageToAny(rc),
			})
		}
	}
	return resp
}

func buildHTTPRoute(node *model.Proxy, push *model.PushContext, routeName string) *route.RouteConfiguration {
	// TODO use route-style naming instead of cluster naming
	_, _, hostname, port := model.ParseSubsetKey(routeName)
	if hostname == "" || port == 0 {
		log.Warnf("failed to parse %v", routeName)
		return nil
	}

	virtualHosts, _, _ := core.BuildSidecarOutboundVirtualHosts(node, push, routeName, port, nil, &model.DisabledCache{})

	// gRPC-xDS clients self-filter by subscribing to individual route configs by name (e.g.
	// "outbound|443||svc.ns.svc.cluster.local"). Filter the returned virtual hosts to only include
	// the one matching the requested. Without this, the RouteConfiguration contains every service on
	// the port from around the mesh, causing unnecessary churn pushes when unrelated services change.
	virtualHosts = filterVirtualHostsForHostname(virtualHosts, string(hostname), port)

	return &route.RouteConfiguration{
		Name:         routeName,
		VirtualHosts: virtualHosts,
	}
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
