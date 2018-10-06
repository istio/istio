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
func getConfigForHost(host model.Hostname, configs []model.Config) *model.Config {
	for _, config := range configs {
		virtualService := config.Spec.(*v1alpha3.VirtualService)
		for _, vsHost := range virtualService.Hosts {
			if model.Hostname(vsHost).Matches(host) {
				return &config
			}
		}
	}
	return nil
}

// hashRuntimeTLSMatchPredicates hashes runtime predicates of a TLS match
func hashRuntimeTLSMatchPredicates(match *v1alpha3.TLSMatchAttributes) string {
	return strings.Join(match.SniHosts, ",") + "|" + strings.Join(match.DestinationSubnets, ",")
}

func buildSidecarOutboundTLSFilterChainOpts(env *model.Environment, node *model.Proxy, push *model.PushContext, destinationIPAddress string,
	service *model.Service, listenPort *model.Port, proxyLabels model.LabelsCollection,
	gateways map[string]bool, config *model.Config) []*filterChainOpts {

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
	if config != nil {
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
					destinationCIDRs := []string{destinationIPAddress}
					if len(match.DestinationSubnets) > 0 {
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
		clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, int(listenPort.Port))
		out = append(out, &filterChainOpts{
			destinationCIDRs: []string{destinationIPAddress},
			networkFilters:   buildOutboundNetworkFiltersWithSingleDestination(env, node, clusterName, listenPort),
		})
	}

	return out
}

func buildSidecarOutboundTCPFilterChainOpts(env *model.Environment, node *model.Proxy, push *model.PushContext, destinationIPAddress string,
	service *model.Service, listenPort *model.Port, proxyLabels model.LabelsCollection,
	gateways map[string]bool, config *model.Config) []*filterChainOpts {

	if listenPort.Protocol.IsTLS() {
		return nil
	}

	out := make([]*filterChainOpts, 0)

	// very basic TCP
	// break as soon as we add one network filter with no destination addresses to match
	// This is the terminating condition in the filter chain match list
	defaultRouteAdded := false
	if config != nil {
		virtualService := config.Spec.(*v1alpha3.VirtualService)
	TcpLoop:
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
					// and break out of the loop.
					// But if we find only runtime destination subnet matches in all match blocks, collect them
					// (this is similar to virtual hosts in http) and create filter chain match accordingly.
					if len(match.DestinationSubnets) == 0 {
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
		clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, int(listenPort.Port))
		out = append(out, &filterChainOpts{
			destinationCIDRs: []string{destinationIPAddress},
			networkFilters:   buildOutboundNetworkFiltersWithSingleDestination(env, node, clusterName, listenPort),
		})
	}

	return out
}

func buildSidecarOutboundTCPTLSFilterChainOpts(env *model.Environment, node *model.Proxy, push *model.PushContext,
	configs []model.Config, destinationIPAddress string, service *model.Service, listenPort *model.Port,
	proxyLabels model.LabelsCollection, gateways map[string]bool) []*filterChainOpts {

	out := make([]*filterChainOpts, 0)
	config := getConfigForHost(service.Hostname, configs)
	out = append(out, buildSidecarOutboundTLSFilterChainOpts(env, node, push, destinationIPAddress, service, listenPort,
		proxyLabels, gateways, config)...)
	out = append(out, buildSidecarOutboundTCPFilterChainOpts(env, node, push, destinationIPAddress, service, listenPort,
		proxyLabels, gateways, config)...)
	return out
}
