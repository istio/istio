// Copyright 2019 Istio Authors
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

package envoyfilter

import (
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func ApplyRouteConfigurationPatches(patchContext networking.EnvoyFilter_PatchContext,
	proxy *model.Proxy, push *model.PushContext,
	routeConfiguration *xdsapi.RouteConfiguration) *xdsapi.RouteConfiguration {

	virtualHostsRemoved := false
	envoyFilterWrappers := push.EnvoyFilters(proxy)
	for _, efw := range envoyFilterWrappers {
		// only merge is applicable for route configuration. Validation checks for the same.
		for _, cp := range efw.Patches[networking.EnvoyFilter_ROUTE_CONFIGURATION] {
			if commonConditionMatch(proxy, patchContext, cp) &&
				routeConfigurationMatch(patchContext, routeConfiguration, cp) {
				proto.Merge(routeConfiguration, cp.Value)
			}
		}

		// First process remove operations, then the merge and finally the add.
		// If add is done before remove, then remove could end up deleting a vhost that
		// was added by the user.
		for _, cp := range efw.Patches[networking.EnvoyFilter_VIRTUAL_HOST] {
			if cp.Operation != networking.EnvoyFilter_Patch_REMOVE &&
				cp.Operation != networking.EnvoyFilter_Patch_MERGE {
				continue
			}

			if !commonConditionMatch(proxy, patchContext, cp) ||
				!routeConfigurationMatch(patchContext, routeConfiguration, cp) {
				continue
			}

			// iterate through all virtual hosts in a route and remove/merge ones that match
			for i := range routeConfiguration.VirtualHosts {
				if routeConfiguration.VirtualHosts[i].Name == "" {
					// removed by another envoy filter
					continue
				}
				if virtualHostMatch(routeConfiguration.VirtualHosts[i], cp) {
					if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
						// set name to empty. We remove virtual hosts with empty names later in this function
						routeConfiguration.VirtualHosts[i].Name = ""
						virtualHostsRemoved = true
					} else {
						proto.Merge(routeConfiguration.VirtualHosts[i], cp.Value)
					}
				}
			}
		}

		// Add virtual host if the operation is add, and patch context matches
		for _, cp := range efw.Patches[networking.EnvoyFilter_VIRTUAL_HOST] {
			if cp.Operation != networking.EnvoyFilter_Patch_ADD {
				continue
			}

			if commonConditionMatch(proxy, patchContext, cp) &&
				routeConfigurationMatch(patchContext, routeConfiguration, cp) {
				routeConfiguration.VirtualHosts = append(routeConfiguration.VirtualHosts, proto.Clone(cp.Value).(*route.VirtualHost))
			}
		}
	}
	if virtualHostsRemoved {
		trimmedVirtualHosts := make([]*route.VirtualHost, 0, len(routeConfiguration.VirtualHosts))
		for _, virtualHost := range routeConfiguration.VirtualHosts {
			if virtualHost.Name == "" {
				continue
			}
			trimmedVirtualHosts = append(trimmedVirtualHosts, virtualHost)
		}
		routeConfiguration.VirtualHosts = trimmedVirtualHosts
	}
	return routeConfiguration
}

func routeConfigurationMatch(patchContext networking.EnvoyFilter_PatchContext, rc *xdsapi.RouteConfiguration,
	cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetRouteConfiguration()
	if cMatch == nil {
		return true
	}

	// we match on the port number and virtual host for sidecars
	// we match on port number, server port name, gateway name, plus virtual host for gateways
	if patchContext != networking.EnvoyFilter_GATEWAY {
		listenerPort := 0
		if strings.HasPrefix(rc.Name, string(model.TrafficDirectionInbound)) {
			_, _, _, listenerPort = model.ParseSubsetKey(rc.Name)
		} else {
			listenerPort, _ = strconv.Atoi(rc.Name)
		}

		// FIXME: Ports on a route can be 0. the API only takes uint32 for ports
		// We should either make that field in API as a wrapper type or switch to int
		if cMatch.PortNumber != 0 && int(cMatch.PortNumber) != listenerPort {
			return false
		}

		if cMatch.Name != "" && cMatch.Name != rc.Name {
			return false
		}

		return true
	}

	// This is a gateway. Get all the fields in the gateway's RDS route name
	portNumber, portName, gateway := model.ParseGatewayRDSRouteName(rc.Name)
	if cMatch.PortNumber != 0 && int(cMatch.PortNumber) != portNumber {
		return false
	}
	if cMatch.PortName != "" && cMatch.PortName != portName {
		return false
	}
	if cMatch.Gateway != "" && cMatch.Gateway != gateway {
		return false
	}

	if cMatch.Name != "" && cMatch.Name != rc.Name {
		return false
	}

	return true
}

func virtualHostMatch(vh *route.VirtualHost, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetRouteConfiguration()
	if cMatch == nil {
		return true
	}

	match := cMatch.Vhost
	if match == nil {
		// match any virtual host in the named route configuration
		return true
	}
	if vh == nil {
		// route configuration has a specific match for a virtual host but
		// we dont have a virtual host to match.
		return false
	}
	// check if virtual host names match
	return match.Name == vh.Name
}
