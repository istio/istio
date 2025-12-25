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

package core

import (
	"sort"
	"strings"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/tunnelingconfig"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// Match by source labels, the listener port where traffic comes in, the gateway on which the rule is being
// bound, etc. All these can be checked statically, since we are generating the configuration for a proxy
// with predefined labels, on a specific port.
func matchTLS(match *v1alpha3.TLSMatchAttributes, proxyLabels labels.Instance, gateways sets.String, port int, proxyNamespace string) bool {
	if match == nil {
		return true
	}

	gatewayMatch := len(match.Gateways) == 0
	for _, gateway := range match.Gateways {
		gatewayMatch = gatewayMatch || gateways.Contains(gateway)
	}

	labelMatch := labels.Instance(match.SourceLabels).SubsetOf(proxyLabels)

	portMatch := match.Port == 0 || match.Port == uint32(port)

	nsMatch := match.SourceNamespace == "" || match.SourceNamespace == proxyNamespace

	return gatewayMatch && labelMatch && portMatch && nsMatch
}

// Match by source labels, the listener port where traffic comes in, the gateway on which the rule is being
// bound, etc. All these can be checked statically, since we are generating the configuration for a proxy
// with predefined labels, on a specific port.
func matchTCP(match *v1alpha3.L4MatchAttributes, proxyLabels labels.Instance, gateways sets.String, port int, proxyNamespace string) bool {
	if match == nil {
		return true
	}

	gatewayMatch := len(match.Gateways) == 0
	for _, gateway := range match.Gateways {
		gatewayMatch = gatewayMatch || gateways.Contains(gateway)
	}

	labelMatch := labels.Instance(match.SourceLabels).SubsetOf(proxyLabels)

	portMatch := match.Port == 0 || match.Port == uint32(port)

	nsMatch := match.SourceNamespace == "" || match.SourceNamespace == proxyNamespace

	return gatewayMatch && labelMatch && portMatch && nsMatch
}

// Select the config pertaining to the service being processed.
func getConfigsForHost(filterNamespace string, hostname host.Name, configs []config.Config) []config.Config {
	svcConfigs := make([]config.Config, 0)
	for _, cfg := range configs {
		virtualService := cfg.Spec.(*v1alpha3.VirtualService)
		for _, vsHost := range virtualService.Hosts {
			if filterNamespace != "" && filterNamespace != cfg.Namespace {
				continue
			}
			if host.Name(vsHost).Matches(hostname) {
				svcConfigs = append(svcConfigs, cfg)
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

func buildSidecarOutboundTLSFilterChainOpts(node *model.Proxy, push *model.PushContext, destinationCIDRs []string,
	service *model.Service, bind string, listenPort *model.Port,
	gateways sets.String, configs []config.Config,
) []*filterChainOpts {
	if !listenPort.Protocol.IsTLS() {
		return nil
	}
	actualWildcard, _ := getActualWildcardAndLocalHost(node)
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
	matchHasBeenHandled := sets.New[string]() // Runtime predicate set -> have we generated config for this set?

	// Is there a virtual service with a TLS block that matches us?
	hasTLSMatch := false

	lb := &ListenerBuilder{node: node, push: push}
	out := make([]*filterChainOpts, 0)
	for _, cfg := range configs {
		virtualService := cfg.Spec.(*v1alpha3.VirtualService)
		for _, tls := range virtualService.Tls {
			for _, match := range tls.Match {
				if matchTLS(match, node.Labels, gateways, listenPort.Port, node.Metadata.Namespace) {
					// Use the service's CIDRs.
					// But if a virtual service overrides it with its own destination subnet match
					// give preference to the user provided one
					// destinationCIDRs will be empty for services with VIPs
					// Only set CIDR match if the listener is bound to an IP.
					// If its bound to a unix domain socket, then ignore the CIDR matches
					// Unix domain socket bound ports have Port value set to 0
					if len(match.DestinationSubnets) > 0 && listenPort.Port > 0 {
						destinationCIDRs = match.DestinationSubnets
					}
					matchHash := hashRuntimeTLSMatchPredicates(match)
					if !matchHasBeenHandled.Contains(matchHash) {
						out = append(out, &filterChainOpts{
							metadata:         util.BuildConfigInfoMetadata(cfg.Meta),
							sniHosts:         match.SniHosts,
							destinationCIDRs: destinationCIDRs,
							networkFilters:   lb.buildOutboundNetworkFilters(tls.Route, listenPort, cfg.Meta, false),
						})
						hasTLSMatch = true
					}
					matchHasBeenHandled.Insert(matchHash)
				}
			}
		}
	}

	// HTTPS or TLS ports without associated virtual service
	if !hasTLSMatch {
		var sniHosts []string

		// In case of a sidecar config with user defined port, if the user specified port is not the same as the
		// service's port, then pick the service port if and only if the service has only one port. If service
		// has multiple ports, then route to a cluster with the listener port (i.e. sidecar defined port) - the
		// traffic will most likely blackhole.
		port := listenPort.Port
		if len(service.Ports) == 1 {
			port = service.Ports[0].Port
		}

		clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port)
		statPrefix := clusterName
		// If stat name is configured, use it to build the stat prefix.
		if len(push.Mesh.OutboundClusterStatName) != 0 {
			statPrefix = telemetry.BuildStatPrefix(push.Mesh.OutboundClusterStatName, string(service.Hostname),
				"", &model.Port{Port: port}, 0, &service.Attributes)
		}
		// Use the hostname as the SNI value if and only:
		// 1) if the destination is a CIDR;
		// 2) or if we have an empty destination VIP (i.e. which we should never get in case some platform adapter improper handlings);
		// 3) or if the destination is a wildcard destination VIP with the listener bound to the wildcard as well.
		// In the above cited cases, the listener will be bound to 0.0.0.0. So SNI match is the only way to distinguish different
		// target services. If we have a VIP, then we know the destination. Or if we do not have an VIP, but have
		// `PILOT_ENABLE_HEADLESS_SERVICE_POD_LISTENERS` enabled (by default) and applicable to all that's needed, pilot will generate
		// an outbound listener for each pod in a headless service. There is thus no need to do a SNI match. It saves us from having to
		// generate expensive permutations of the host name just like RDS does..
		// NOTE that we cannot have two services with the same VIP as our listener build logic will treat it as a collision and
		// ignore one of the services.
		svcListenAddress := service.GetAddressForProxy(node)
		if strings.Contains(svcListenAddress, "/") {
			// Address is a CIDR, already captured by destinationCIDR parameter.
			svcListenAddress = ""
		}

		if service.Attributes.ServiceRegistry == provider.External && node.IsIPv6() && svcListenAddress == constants.UnspecifiedIP {
			svcListenAddress = constants.UnspecifiedIPv6
		}

		if len(destinationCIDRs) > 0 || len(svcListenAddress) == 0 || (svcListenAddress == actualWildcard && bind == actualWildcard) {
			sniHosts = []string{string(service.Hostname)}
			for _, a := range service.Attributes.Aliases {
				alt := GenerateAltVirtualHosts(a.Hostname.String(), 0, node.DNSDomain, node)
				sniHosts = append(sniHosts, a.Hostname.String())
				sniHosts = append(sniHosts, alt...)
			}
		}
		destinationRule := CastDestinationRule(node.SidecarScope.DestinationRule(
			model.TrafficDirectionOutbound, node, service.Hostname).GetRule())
		out = append(out, &filterChainOpts{
			sniHosts:         sniHosts,
			destinationCIDRs: destinationCIDRs,
			networkFilters: lb.buildOutboundNetworkFiltersWithSingleDestination(statPrefix, clusterName, "",
				listenPort, destinationRule, tunnelingconfig.Apply, false),
		})
	}

	return out
}

func buildSidecarOutboundTCPFilterChainOpts(node *model.Proxy, push *model.PushContext, destinationCIDRs []string,
	service *model.Service, listenPort *model.Port,
	gateways sets.String, configs []config.Config,
) []*filterChainOpts {
	if listenPort.Protocol.IsTLS() {
		return nil
	}

	out := make([]*filterChainOpts, 0)

	lb := &ListenerBuilder{node: node, push: push}
	// very basic TCP
	// break as soon as we add one network filter with no destination addresses to match
	// This is the terminating condition in the filter chain match list
	defaultRouteAdded := false
TcpLoop:
	for _, cfg := range configs {
		virtualService := cfg.Spec.(*v1alpha3.VirtualService)
		for _, tcp := range virtualService.Tcp {
			if len(tcp.Match) == 0 {
				// implicit match
				out = append(out, &filterChainOpts{
					metadata:         util.BuildConfigInfoMetadata(cfg.Meta),
					destinationCIDRs: destinationCIDRs,
					networkFilters:   lb.buildOutboundNetworkFilters(tcp.Route, listenPort, cfg.Meta, false),
				})
				defaultRouteAdded = true
				break TcpLoop
			}

			// Use the service's virtual address first.
			// But if a virtual service overrides it with its own destination subnet match
			// give preference to the user provided one
			virtualServiceDestinationSubnets := make([]string, 0)

			for _, match := range tcp.Match {
				if matchTCP(match, node.Labels, gateways, listenPort.Port, node.Metadata.Namespace) {
					// Scan all the match blocks
					// if we find any match block without a runtime destination subnet match
					// i.e. match any destination address, then we treat it as the terminal match/catch all match
					// and break out of the loop. We also treat it as a terminal match if the listener is bound
					// to a unix domain socket.
					// But if we find only runtime destination subnet matches in all match blocks, collect them
					// (this is similar to virtual hosts in http) and create filter chain match accordingly.
					if len(match.DestinationSubnets) == 0 || listenPort.Port == 0 {
						out = append(out, &filterChainOpts{
							metadata:         util.BuildConfigInfoMetadata(cfg.Meta),
							destinationCIDRs: destinationCIDRs,
							networkFilters:   lb.buildOutboundNetworkFilters(tcp.Route, listenPort, cfg.Meta, false),
						})
						defaultRouteAdded = true
						break TcpLoop
					}
					virtualServiceDestinationSubnets = append(virtualServiceDestinationSubnets, match.DestinationSubnets...)
				}
			}

			if len(virtualServiceDestinationSubnets) > 0 {
				out = append(out, &filterChainOpts{
					destinationCIDRs: virtualServiceDestinationSubnets,
					networkFilters:   lb.buildOutboundNetworkFilters(tcp.Route, listenPort, cfg.Meta, false),
				})

				// If at this point there is a filter chain generated with the same CIDR match as the
				// one that may be generated for the service as the default route, do not generate it.
				// Otherwise, Envoy will complain about having filter chains with identical matches
				// and will reject the config.
				sort.Strings(virtualServiceDestinationSubnets)
				sort.Strings(destinationCIDRs)
				if slices.Equal(virtualServiceDestinationSubnets, destinationCIDRs) {
					log.Warnf("Existing filter chain with same matching CIDR: %v.", destinationCIDRs)
					defaultRouteAdded = true
				}
			}
		}
	}

	if !defaultRouteAdded {
		// In case of a sidecar config with user defined port, if the user specified port is not the same as the
		// service's port, then pick the service port if and only if the service has only one port. If service
		// has multiple ports, then route to a cluster with the listener port (i.e. sidecar defined port) - the
		// traffic will most likely blackhole.
		port := listenPort.Port
		if len(service.Ports) == 1 {
			port = service.Ports[0].Port
		}

		clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port)
		statPrefix := clusterName
		destinationRule := CastDestinationRule(node.SidecarScope.DestinationRule(
			model.TrafficDirectionOutbound, node, service.Hostname).GetRule())
		// If stat name is configured, use it to build the stat prefix.
		if len(push.Mesh.OutboundClusterStatName) != 0 {
			statPrefix = telemetry.BuildStatPrefix(push.Mesh.OutboundClusterStatName, string(service.Hostname), "",
				&model.Port{Port: port}, 0, &service.Attributes)
		}
		out = append(out, &filterChainOpts{
			destinationCIDRs: destinationCIDRs,
			networkFilters: lb.buildOutboundNetworkFiltersWithSingleDestination(statPrefix, clusterName, "",
				listenPort, destinationRule, tunnelingconfig.Apply, false),
		})
	}

	return out
}
