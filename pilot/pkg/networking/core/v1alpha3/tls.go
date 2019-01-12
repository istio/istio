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
	"strings"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// Match by source labels, the listener port where traffic comes in, the gateway on which the rule is being
// bound, etc. All these can be checked statically, since we are generating the configuration for a proxy
// with predefined labels, on a specific port.
func matchTLS(match *v1alpha3.TLSMatchAttributes, proxyLabels model.LabelsCollection, gateways map[string]bool, port int) bool {
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

// Match by source labels, the listener port where traffic comes in, the gateway on which the rule is being
// bound, etc. All these can be checked statically, since we are generating the configuration for a proxy
// with predefined labels, on a specific port.
func matchTCP(match *v1alpha3.L4MatchAttributes, proxyLabels model.LabelsCollection, gateways map[string]bool, port int) bool {
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

// Select the config pertaining to the service being processed.
func getConfigsForHost(host model.Hostname, configs []model.Config) []*model.Config {
	svcConfigs := make([]*model.Config, 0)
	for index := range configs {
		virtualService := configs[index].Spec.(*v1alpha3.VirtualService)
		for _, vsHost := range virtualService.Hosts {
			if model.Hostname(vsHost).Matches(host) {
				svcConfigs = append(svcConfigs, &configs[index])
				break
			}
		}
	}
	return svcConfigs
}

// hashRuntimeTLSMatchPredicates hashes runtime predicates of a TLS match
func hashRuntimeTLSMatchPredicates(match *v1alpha3.TLSMatchAttributes) string {
	return strings.Join(match.SniHosts, ",") + "|" + strings.Join(match.DestinationSubnets, ",")
}

func buildSidecarOutboundTLSFilterChainOpts(env *model.Environment, node *model.Proxy, push *model.PushContext, destinationIPAddress string,
	service *model.Service, listenPort *model.Port, proxyLabels model.LabelsCollection,
	gateways map[string]bool, configs []*model.Config) []*filterChainOpts {

	if !listenPort.Protocol.IsTLS() {
		return nil
	}

	// TLS matches are composed of runtime and static predicates.
	// Static predicates can be evaluated during the generation of the config. Examples: gateway, source labels, etc.
	// Runtime predicates cannot be evaluated during config generation. Instead the proxy must be configured to
	// evaluate them. Examples: SNI hosts, source/destination subnets, etc.
	//
	// A list of matches may contain duplicate runtime matches, but different static matches. For example:
	//
	// {sni_hosts: A, sourceLabels: X} => destination M
	// {sni_hosts: A, sourceLabels: *} => destination N
	//
	// For a proxy with labels X, we can evaluate the static predicates to get:
	// {sni_hosts: A} => destination M
	// {sni_hosts: A} => destination N
	//
	// The matches have the same runtime predicates. Since the second match can never be reached, we only
	// want to generate config for the first match.
	//
	// To achieve this in this function we keep track of which runtime matches we have already generated config for
	// and only add config if the we have not already generated config for that set of runtime predicates.
	matchHasBeenHandled := make(map[string]bool) // Runtime predicate set -> have we generated config for this set?

	// Is there a virtual service with a TLS block that matches us?
	hasTLSMatch := false

	out := make([]*filterChainOpts, 0)
	for _, config := range configs {
		virtualService := config.Spec.(*v1alpha3.VirtualService)
		// Ports marked as TLS will have SNI routing if and only if they have an accompanying
		// virtual service for the same host, and the said virtual service has a TLS route block.
		// Otherwise we treat ports marked as TLS as opaque TCP services.
		for _, tls := range virtualService.Tls {
			for _, match := range tls.Match {
				if matchTLS(match, proxyLabels, gateways, listenPort.Port) {
					// Use the service's virtual address first.
					// But if a virtual service overrides it with its own destination subnet match
					// give preference to the user provided one
					// destinationIPAddress will be empty for unix domain sockets
					destinationCIDRs := []string{destinationIPAddress}
					// Only set CIDR match if the listener is bound to an IP.
					// If its bound to a unix domain socket, then ignore the CIDR matches
					// Unix domain socket bound ports have Port value set to 0
					if len(match.DestinationSubnets) > 0 && listenPort.Port > 0 {
						destinationCIDRs = match.DestinationSubnets
					}
					matchHash := hashRuntimeTLSMatchPredicates(match)
					if !matchHasBeenHandled[matchHash] {
						out = append(out, &filterChainOpts{
							sniHosts:         match.SniHosts,
							destinationCIDRs: destinationCIDRs,
							networkFilters:   buildOutboundNetworkFilters(env, node, tls.Route, push, listenPort, config.ConfigMeta),
						})
						hasTLSMatch = true
					}
					matchHasBeenHandled[matchHash] = true
				}
			}
		}
	}

	// HTTPS or TLS ports without associated virtual service will be treated as opaque TCP traffic.
	if !hasTLSMatch {
		port := listenPort.Port
		var clusterName string
		// The service could be nil if we are being called in the context of a sidecar config with
		// user specified port in the egress listener. Since we dont know the destination service
		// and this piece of code is establishing the final fallback path, we set the
		// tcp proxy cluster to a blackhole cluster
		if service != nil {
			// If the service has only one port, use that instead of the
			// listenPort. Same logic as GetDestinationCluster in route.
			if len(service.Ports) == 1 {
				port = service.Ports[0].Port
			}
			clusterName = model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port)
		} else {
			clusterName = util.BlackHoleCluster
		}

		out = append(out, &filterChainOpts{
			destinationCIDRs: []string{destinationIPAddress},
			networkFilters:   buildOutboundNetworkFiltersWithSingleDestination(env, node, clusterName, listenPort),
		})
	}

	return out
}

func buildSidecarOutboundTCPFilterChainOpts(env *model.Environment, node *model.Proxy, push *model.PushContext, destinationIPAddress string,
	service *model.Service, listenPort *model.Port, proxyLabels model.LabelsCollection,
	gateways map[string]bool, configs []*model.Config) []*filterChainOpts {

	if listenPort.Protocol.IsTLS() {
		return nil
	}

	out := make([]*filterChainOpts, 0)

	// very basic TCP
	// break as soon as we add one network filter with no destination addresses to match
	// This is the terminating condition in the filter chain match list
	defaultRouteAdded := false
TcpLoop:
	for _, config := range configs {
		virtualService := config.Spec.(*v1alpha3.VirtualService)
		for _, tcp := range virtualService.Tcp {
			destinationCIDRs := []string{destinationIPAddress}
			if len(tcp.Match) == 0 {
				// implicit match
				out = append(out, &filterChainOpts{
					destinationCIDRs: destinationCIDRs,
					networkFilters:   buildOutboundNetworkFilters(env, node, tcp.Route, push, listenPort, config.ConfigMeta),
				})
				defaultRouteAdded = true
				break TcpLoop
			}

			// Use the service's virtual address first.
			// But if a virtual service overrides it with its own destination subnet match
			// give preference to the user provided one
			virtualServiceDestinationSubnets := make([]string, 0)

			for _, match := range tcp.Match {
				if matchTCP(match, proxyLabels, gateways, listenPort.Port) {
					// Scan all the match blocks
					// if we find any match block without a runtime destination subnet match
					// i.e. match any destination address, then we treat it as the terminal match/catch all match
					// and break out of the loop. We also treat it as a terminal match if the listener is bound
					// to a unix domain socket.
					// But if we find only runtime destination subnet matches in all match blocks, collect them
					// (this is similar to virtual hosts in http) and create filter chain match accordingly.
					if len(match.DestinationSubnets) == 0 || listenPort.Port == 0 {
						out = append(out, &filterChainOpts{
							destinationCIDRs: destinationCIDRs,
							networkFilters:   buildOutboundNetworkFilters(env, node, tcp.Route, push, listenPort, config.ConfigMeta),
						})
						defaultRouteAdded = true
						break TcpLoop
					} else {
						virtualServiceDestinationSubnets = append(virtualServiceDestinationSubnets, match.DestinationSubnets...)
					}
				}
			}

			if len(virtualServiceDestinationSubnets) > 0 {
				out = append(out, &filterChainOpts{
					destinationCIDRs: virtualServiceDestinationSubnets,
					networkFilters:   buildOutboundNetworkFilters(env, node, tcp.Route, push, listenPort, config.ConfigMeta),
				})
			}
		}
	}

	if !defaultRouteAdded {
		port := listenPort.Port

		var clusterName string
		// The service could be nil if we are being called in the context of a sidecar config with
		// user specified port in the egress listener. Since we dont know the destination service
		// and this piece of code is establishing the final fallback path, we set the
		// tcp proxy cluster to a blackhole cluster
		if service != nil {
			// If the service has only one port, use that instead of the
			// listenPort. Same logic as GetDestinationCluster in route.
			if len(service.Ports) == 1 {
				port = service.Ports[0].Port
			}
			clusterName = model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port)
		} else {
			clusterName = util.BlackHoleCluster
		}

		out = append(out, &filterChainOpts{
			destinationCIDRs: []string{destinationIPAddress},
			networkFilters:   buildOutboundNetworkFiltersWithSingleDestination(env, node, clusterName, listenPort),
		})
	}

	return out
}

// This function can be called for namespaces with the auto generated sidecar, i.e. once per service and per port.
// OR, it could be called in the context of an egress listener with specific TCP port on a sidecar config.
// In the latter case, there is no service associated with this listen port. So we have to account for this
// missing service throughout this file
func buildSidecarOutboundTCPTLSFilterChainOpts(env *model.Environment, node *model.Proxy, push *model.PushContext,
	configs []model.Config, destinationIPAddress string, service *model.Service, listenPort *model.Port,
	proxyLabels model.LabelsCollection, gateways map[string]bool) []*filterChainOpts {

	out := make([]*filterChainOpts, 0)
	var svcConfigs []*model.Config
	if service != nil {
		svcConfigs = getConfigsForHost(service.Hostname, configs)
	}

	out = append(out, buildSidecarOutboundTLSFilterChainOpts(env, node, push, destinationIPAddress, service, listenPort,
		proxyLabels, gateways, svcConfigs)...)
	out = append(out, buildSidecarOutboundTCPFilterChainOpts(env, node, push, destinationIPAddress, service, listenPort,
		proxyLabels, gateways, svcConfigs)...)
	return out
}
