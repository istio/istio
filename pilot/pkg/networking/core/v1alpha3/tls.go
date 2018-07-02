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

package v1alpha3

import (
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pkg/log"
)

func sourceMatchTLS(match *v1alpha3.TLSMatchAttributes, proxyLabels model.LabelsCollection, gateways map[string]bool, port int) bool {
	if match == nil {
		return true
	}

	gatewayMatch := len(match.Gateways) == 0
	for _, gateway := range match.Gateways {
		gatewayMatch = gatewayMatch || gateways[gateway]
	}

	labelMatch := proxyLabels.IsSupersetOf(model.Labels(match.SourceLabels))

	portMatch := match.Port == 0 || match.Port == uint32(port)

	return gatewayMatch && labelMatch && portMatch
}

func sourceMatchTCP(match *v1alpha3.L4MatchAttributes, proxyLabels model.LabelsCollection, gateways map[string]bool, port int) bool {
	if match == nil {
		return true
	}

	gatewayMatch := len(match.Gateways) == 0
	for _, gateway := range match.Gateways {
		gatewayMatch = gatewayMatch || gateways[gateway]
	}

	labelMatch := proxyLabels.IsSupersetOf(model.Labels(match.SourceLabels))

	portMatch := match.Port == 0 || match.Port == uint32(port)

	return gatewayMatch && labelMatch && portMatch
}

// TODO: very inefficient
// TODO: result is not guaranteed for overlapping hosts (e.g. "dog.ceo" matches both "*.ceo" and "*")
func getVirtualServiceForHost(host model.Hostname, configs []model.Config) *v1alpha3.VirtualService {
	for _, config := range configs {
		virtualService := config.Spec.(*v1alpha3.VirtualService)
		for _, vsHost := range virtualService.Hosts {
			if model.Hostname(vsHost).Matches(host) {
				return virtualService
			}
		}
	}
	return nil
}

// TODO: check port protocol?
func buildOutboundTCPFilterChainOpts(proxy model.Proxy, env model.Environment, configs []model.Config, addresses []string,
	host model.Hostname, listenPort *model.Port, proxyLabels model.LabelsCollection, gateways map[string]bool) []*filterChainOpts {

	out := make([]*filterChainOpts, 0)

	virtualService := getVirtualServiceForHost(host, configs)
	if virtualService != nil {
		// TODO: Make SNI compatible with RBAC. Deprecated tcp route configs are incompatible with SNI.
		// RBAC requires deprecated tcp route configs, so RBAC is incompatible with SNI.
		for _, tls := range virtualService.Tls {
			// since we don't support weighted destinations yet there can only be exactly 1 destination
			dest := tls.Route[0].Destination
			destSvc, err := env.GetService(model.Hostname(dest.Host))
			if err != nil {
				log.Debugf("failed to retrieve service for destination %q: %v", host, err)
				continue
			}
			clusterName := istio_route.GetDestinationCluster(dest, destSvc, listenPort.Port)

			if len(tls.Match) == 0 { // implicit match
				out = append(out, &filterChainOpts{
					sniHosts:       []string{"*"},
					networkFilters: buildOutboundNetworkFilters(clusterName, nil, listenPort),
				})
				break // this matches everything, so short-circuit (anything beyond this can never be reached)
			}
			for _, match := range tls.Match {
				if sourceMatchTLS(match, proxyLabels, gateways, listenPort.Port) {
					sniHosts := match.SniHosts
					if len(sniHosts) == 0 {
						sniHosts = []string{"*"}
					}

					out = append(out, &filterChainOpts{
						sniHosts:       sniHosts,
						networkFilters: buildOutboundNetworkFilters(clusterName, nil, listenPort),
					})
				}
			}
		}

		// very basic TCP (no L4 matching)
		// only add if we don't have any network filters
		// break as soon as we add one network filter
		if len(out) == 0 {
		TcpLoop:
			for _, tcp := range virtualService.Tcp {
				// since we don't support weighted destinations yet there can only be exactly 1 destination
				dest := tcp.Route[0].Destination
				destSvc, err := env.GetService(model.Hostname(dest.Host))
				if err != nil {
					log.Debugf("failed to retrieve service for destination %q: %v", host, err)
					continue
				}
				clusterName := istio_route.GetDestinationCluster(dest, destSvc, listenPort.Port)
				addresses = []string{destSvc.GetServiceAddressForProxy(&proxy)}

				if len(tcp.Match) == 0 { // implicit match
					out = []*filterChainOpts{{
						networkFilters: buildOutboundNetworkFilters(clusterName, addresses, listenPort),
					}}
					break TcpLoop
				}
				for _, match := range tcp.Match {
					if sourceMatchTCP(match, proxyLabels, gateways, listenPort.Port) {
						out = []*filterChainOpts{{
							networkFilters: buildOutboundNetworkFilters(clusterName, addresses, listenPort),
						}}
						break TcpLoop
					}
				}
			}
		}
	}

	if len(out) == 0 { // FIXME:
		clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", host, int(listenPort.Port))
		out = []*filterChainOpts{{
			networkFilters: buildOutboundNetworkFilters(clusterName, addresses, listenPort),
		}}
	}

	return out
}
