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

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// BuildListeners handles a LDS request, returning listeners of ApiListener type.
// The request may include a list of resource names, using the full_hostname[:port] format to select only
// specific services.
func (g *GrpcConfigGenerator) BuildListeners(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	resp := model.Resources{}

	// TODO filter by port as well, this only filters by a host and includes every port on matching service
	filter := map[string]struct{}{}
	for _, name := range names {
		if strings.Contains(name, ":") {
			n, _, err := net.SplitHostPort(name)
			if err == nil {
				name = n
			}
		}
		filter[name] = struct{}{}
	}

	for _, el := range node.SidecarScope.EgressListeners {
		for _, sv := range el.Services() {
			shost := string(sv.Hostname)
			if len(filter) > 0 {
				// DiscReq has a filter - only return services that match
				if _, ok := filter[shost]; !ok {
					continue
				}
			}
			for _, p := range sv.Ports {
				hp := net.JoinHostPort(shost, strconv.Itoa(p.Port))
				ll := &listener.Listener{
					Name: hp,
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address: sv.Address,
								PortSpecifier: &core.SocketAddress_PortValue{
									PortValue: uint32(p.Port),
								},
							},
						},
					},
					ApiListener: &listener.ApiListener{
						ApiListener: util.MessageToAny(&hcm.HttpConnectionManager{
							RouteSpecifier: &hcm.HttpConnectionManager_Rds{
								// TODO: for TCP listeners don't generate RDS, but some indication of cluster name.
								Rds: &hcm.Rds{
									ConfigSource: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_Ads{
											Ads: &core.AggregatedConfigSource{},
										},
									},
									RouteConfigName: clusterKey(shost, p.Port),
								},
							},
						}),
					},
					// TODO server-side listeners + mTLS
				}

				resp = append(resp, &discovery.Resource{
					Name:     ll.Name,
					Resource: util.MessageToAny(ll),
				})
			}
		}
	}

	return resp
}
