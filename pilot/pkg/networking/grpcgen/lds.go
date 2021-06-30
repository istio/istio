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

	"github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// BuildListeners handles a LDS request, returning listeners of ApiListener type.
// The request may include a list of resource names, using the full_hostname[:port] format to select only
// specific services.
func (g *GrpcConfigGenerator) BuildListeners(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	resp := model.Resources{}

	filter := map[string]bool{}
	for _, name := range names {
		if strings.Contains(name, ":") {
			n, _, err := net.SplitHostPort(name)
			if err == nil {
				name = n
			}
		}
		filter[name] = true
	}

	for _, el := range node.SidecarScope.EgressListeners {
		for _, sv := range el.Services() {
			shost := string(sv.Hostname)
			if len(filter) > 0 {
				// DiscReq has a filter - only return services that match
				if !filter[shost] {
					continue
				}
			}
			for _, p := range sv.Ports {
				hp := net.JoinHostPort(shost, strconv.Itoa(p.Port))
				ll := &envoy_config_listener_v3.Listener{
					Name: hp,
				}

				ll.Address = &envoy_config_core_v3.Address{
					Address: &envoy_config_core_v3.Address_SocketAddress{
						SocketAddress: &envoy_config_core_v3.SocketAddress{
							Address: sv.Address,
							PortSpecifier: &envoy_config_core_v3.SocketAddress_PortValue{
								PortValue: uint32(p.Port),
							},
						},
					},
				}
				hcm := &envoy_extensions_filters_network_http_connection_manager_v3.HttpConnectionManager{
					RouteSpecifier: &envoy_extensions_filters_network_http_connection_manager_v3.HttpConnectionManager_Rds{
						Rds: &envoy_extensions_filters_network_http_connection_manager_v3.Rds{
							ConfigSource: &envoy_config_core_v3.ConfigSource{
								ConfigSourceSpecifier: &envoy_config_core_v3.ConfigSource_Ads{
									Ads: &envoy_config_core_v3.AggregatedConfigSource{},
								},
							},
							RouteConfigName: clusterKey(shost, p.Port),
						},
					},
				}
				hcmAny := util.MessageToAny(hcm)
				// TODO: for TCP listeners don't generate RDS, but some indication of cluster name.
				ll.ApiListener = &envoy_config_listener_v3.ApiListener{
					ApiListener: hcmAny,
				}
				resp = append(resp, &envoy_service_discovery_v3.Resource{
					Name:     hp,
					Resource: util.MessageToAny(ll),
				})
			}
		}
	}

	return resp
}
