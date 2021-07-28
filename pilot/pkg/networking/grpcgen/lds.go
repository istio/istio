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
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"net"
	"strconv"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// BuildListeners handles a LDS request, returning listeners of ApiListener type.
// The request may include a list of resource names, using the full_hostname[:port] format to select only
// specific services.
func (g *GrpcConfigGenerator) BuildListeners(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	resp := model.Resources{}

	filter := newListenerNameFilter(names)
	for _, el := range node.SidecarScope.EgressListeners {
		for _, sv := range el.Services() {
			sHost := string(sv.Hostname)
			if !filter.includeHostOrExactName(sHost) {
				continue
			}
			for _, p := range sv.Ports {
				sPort := strconv.Itoa(p.Port)
				if !filter.includeHostPort(sHost, sPort) {
					continue
				}
				hp := net.JoinHostPort(sHost, sPort)
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
									RouteConfigName: clusterKey(sHost, p.Port),
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

// map[host] -> map[port] -> exists
// if the map[port] is empty, an exact listener name was provided (non-hostport)
type listenerNameFilter map[string]map[string]struct{}

func (f listenerNameFilter) includeHostOrExactName(s string) bool {
	if len(f) == 0 {
		// filter is empty, include everything
		return true
	}
	_, ok := f[s]
	return ok
}

func (f listenerNameFilter) includeHostPort(host string, port string) bool {
	if len(f) == 0 {
		// filter is empty, include everything
		return true
	}
	portMap, ok := f[host]
	if !ok {
		return false
	}
	if len(portMap) == 0 {
		return true
	}
	_, ok = portMap[port]
	return ok
}

func newListenerNameFilter(names []string) listenerNameFilter {
	filter := make(listenerNameFilter, len(names))
	for _, name := range names {
		if host, port, err := net.SplitHostPort(name); err == nil {
			var first bool
			portMap, ok := filter[host]
			if !ok {
				portMap = map[string]struct{}{}
				filter[host] = portMap
				first = true
			}
			// the portMap is empty and we didn't just create it, we want to include all ports
			if len(portMap) == 0 && !first {
				continue
			}

			portMap[port] = struct{}{}
		} else {
			// if the listener name was "foo.com" and we already have "foo.com" -> {80 -> struct{}{}}, replace
			// with an empty map to indicate we will include all ports since only the hostname was provided
			// TODO, should we just default to some port in this case?
			filter[name] = map[string]struct{}{}
		}
	}
	return filter
}
