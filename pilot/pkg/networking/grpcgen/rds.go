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

	"github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

// BuildHTTPRoutes supports per-VIP routes, as used by GRPC.
// This mode is indicated by using names containing full host:port instead of just port.
// Returns true of the request is of this type.
func (g *GrpcConfigGenerator) BuildHTTPRoutes(node *model.Proxy, push *model.PushContext, routeNames []string) model.Resources {
	resp := model.Resources{}

	// Currently this mode is only used by GRPC, to extract Cluster for the default
	// route.
	for _, n := range routeNames {
		_, _, hostname, port := model.ParseSubsetKey(n)
		if hostname == "" || port == 0 {
			log.Warn("Failed to parse ", n)
			continue
		}
		hn := string(hostname)
		el := node.SidecarScope.GetEgressListenerForRDS(port, "")
		// TODO: use VirtualServices instead !
		// Currently gRPC doesn't support matching the path.
		svc := el.Services()
		for _, s := range svc {
			if s.Hostname.Matches(host.Name(hn)) {
				// Only generate the required route for grpc. Will need to generate more
				// as GRPC adds more features.
				rc := &envoy_config_route_v3.RouteConfiguration{
					Name: n,
					VirtualHosts: []*envoy_config_route_v3.VirtualHost{
						{
							Name:    hn,
							Domains: []string{hn, net.JoinHostPort(hn, strconv.Itoa(port))},

							Routes: []*envoy_config_route_v3.Route{
								{
									Match: &envoy_config_route_v3.RouteMatch{
										PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{Prefix: ""},
									},
									Action: &envoy_config_route_v3.Route_Route{
										Route: &envoy_config_route_v3.RouteAction{
											ClusterSpecifier: &envoy_config_route_v3.RouteAction_Cluster{
												Cluster: n,
											},
										},
									},
								},
							},
						},
					},
				}
				resp = append(resp, &envoy_service_discovery_v3.Resource{
					Name:     n,
					Resource: util.MessageToAny(rc),
				})
			}
		}
	}
	return resp
}
