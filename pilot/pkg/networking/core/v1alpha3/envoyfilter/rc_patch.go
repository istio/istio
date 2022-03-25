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

package envoyfilter

import (
	"strconv"
	"strings"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/pkg/log"
)

func ApplyRouteConfigurationPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	proxy *model.Proxy,
	efw *model.EnvoyFilterWrapper,
	routeConfiguration *route.RouteConfiguration) (out *route.RouteConfiguration) {
	defer runtime.HandleCrash(runtime.LogPanic, func(interface{}) {
		IncrementEnvoyFilterErrorMetric(Route)
		log.Errorf("route patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the route generated before to reduce the influence.
	out = routeConfiguration
	if efw == nil {
		return out
	}

	var portMap model.GatewayPortMap
	if proxy.MergedGateway != nil {
		portMap = proxy.MergedGateway.PortMap
	}

	// only merge is applicable for route configuration.
	for _, rp := range efw.Patches[networking.EnvoyFilter_ROUTE_CONFIGURATION] {
		if rp.Operation != networking.EnvoyFilter_Patch_MERGE {
			continue
		}
		if commonConditionMatch(patchContext, rp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, rp, portMap) {
			proto.Merge(routeConfiguration, rp.Value)
			IncrementEnvoyFilterMetric(rp.Key(), Route, true)
		} else {
			IncrementEnvoyFilterMetric(rp.Key(), Route, false)
		}
	}
	patchVirtualHosts(patchContext, efw.Patches, routeConfiguration, portMap)

	return routeConfiguration
}

func patchVirtualHosts(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, portMap model.GatewayPortMap) {
	removedVirtualHosts := sets.NewSet()
	// first do removes/merges/replaces
	for i := range routeConfiguration.VirtualHosts {
		if patchVirtualHost(patchContext, patches, routeConfiguration, routeConfiguration.VirtualHosts, i, portMap) {
			removedVirtualHosts.Insert(routeConfiguration.VirtualHosts[i].Name)
		}
	}

	// now for the adds
	for _, rp := range patches[networking.EnvoyFilter_VIRTUAL_HOST] {
		if rp.Operation != networking.EnvoyFilter_Patch_ADD {
			continue
		}
		if commonConditionMatch(patchContext, rp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, rp, portMap) {
			routeConfiguration.VirtualHosts = append(routeConfiguration.VirtualHosts, proto.Clone(rp.Value).(*route.VirtualHost))
			IncrementEnvoyFilterMetric(rp.Key(), VirtualHost, true)
		} else {
			IncrementEnvoyFilterMetric(rp.Key(), VirtualHost, false)
		}
	}
	if len(removedVirtualHosts) > 0 {
		trimmedVirtualHosts := make([]*route.VirtualHost, 0, len(routeConfiguration.VirtualHosts))
		for _, virtualHost := range routeConfiguration.VirtualHosts {
			if removedVirtualHosts.Contains(virtualHost.Name) {
				continue
			}
			trimmedVirtualHosts = append(trimmedVirtualHosts, virtualHost)
		}
		routeConfiguration.VirtualHosts = trimmedVirtualHosts
	}
}

// patchVirtualHost patches passed in virtual host if it is MERGE operation.
// The return value indicates whether the virtual host has been removed for REMOVE operations.
func patchVirtualHost(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHosts []*route.VirtualHost,
	idx int, portMap model.GatewayPortMap) bool {
	for _, rp := range patches[networking.EnvoyFilter_VIRTUAL_HOST] {
		applied := false
		if commonConditionMatch(patchContext, rp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, rp, portMap) &&
			virtualHostMatch(virtualHosts[idx], rp) {
			applied = true
			if rp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				return true
			} else if rp.Operation == networking.EnvoyFilter_Patch_MERGE {
				proto.Merge(virtualHosts[idx], rp.Value)
			} else if rp.Operation == networking.EnvoyFilter_Patch_REPLACE {
				virtualHosts[idx] = proto.Clone(rp.Value).(*route.VirtualHost)
			}
		}
		IncrementEnvoyFilterMetric(rp.Key(), VirtualHost, applied)
	}
	patchHTTPRoutes(patchContext, patches, routeConfiguration, virtualHosts[idx], portMap)
	return false
}

func hasRouteMatch(rp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := rp.Match.GetRouteConfiguration()
	if cMatch == nil {
		return false
	}

	vhMatch := cMatch.Vhost
	if vhMatch == nil {
		return false
	}

	return vhMatch.Route != nil
}

func patchHTTPRoutes(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHost *route.VirtualHost, portMap model.GatewayPortMap) {
	routesRemoved := false
	// Apply the route level removes/merges if any.
	for index := range virtualHost.Routes {
		patchHTTPRoute(patchContext, patches, routeConfiguration, virtualHost, index, &routesRemoved, portMap)
	}

	// now for the adds
	for _, rp := range patches[networking.EnvoyFilter_HTTP_ROUTE] {
		applied := false
		if !commonConditionMatch(patchContext, rp) ||
			!routeConfigurationMatch(patchContext, routeConfiguration, rp, portMap) ||
			!virtualHostMatch(virtualHost, rp) {
			IncrementEnvoyFilterMetric(rp.Key(), Route, applied)
			continue
		}
		if rp.Operation == networking.EnvoyFilter_Patch_ADD {
			virtualHost.Routes = append(virtualHost.Routes, proto.Clone(rp.Value).(*route.Route))
			applied = true
		} else if rp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a route match is same as ADD in the end
			if !hasRouteMatch(rp) {
				virtualHost.Routes = append(virtualHost.Routes, proto.Clone(rp.Value).(*route.Route))
				continue
			}
			// find the matching route first
			insertPosition := -1
			for i := 0; i < len(virtualHost.Routes); i++ {
				if routeMatch(virtualHost.Routes[i], rp) {
					insertPosition = i + 1
					break
				}
			}

			if insertPosition == -1 {
				continue
			}
			applied = true
			clonedVal := proto.Clone(rp.Value).(*route.Route)
			virtualHost.Routes = append(virtualHost.Routes, clonedVal)
			if insertPosition < len(virtualHost.Routes)-1 {
				copy(virtualHost.Routes[insertPosition+1:], virtualHost.Routes[insertPosition:])
				virtualHost.Routes[insertPosition] = clonedVal
			}
		} else if rp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE || rp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			// insert before/first without a route match is same as insert in the beginning
			if !hasRouteMatch(rp) {
				virtualHost.Routes = append([]*route.Route{proto.Clone(rp.Value).(*route.Route)}, virtualHost.Routes...)
				continue
			}
			// find the matching route first
			insertPosition := -1
			for i := 0; i < len(virtualHost.Routes); i++ {
				if routeMatch(virtualHost.Routes[i], rp) {
					insertPosition = i
					break
				}
			}

			// If matching route is not found, then don't insert and continue.
			if insertPosition == -1 {
				continue
			}

			applied = true

			// In case of INSERT_FIRST, if a match is found, still insert it at the top of the routes.
			if rp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
				insertPosition = 0
			}

			clonedVal := proto.Clone(rp.Value).(*route.Route)
			virtualHost.Routes = append(virtualHost.Routes, clonedVal)
			copy(virtualHost.Routes[insertPosition+1:], virtualHost.Routes[insertPosition:])
			virtualHost.Routes[insertPosition] = clonedVal
		}
		IncrementEnvoyFilterMetric(rp.Key(), Route, applied)
	}
	if routesRemoved {
		trimmedRoutes := make([]*route.Route, 0, len(virtualHost.Routes))
		for i := range virtualHost.Routes {
			if virtualHost.Routes[i] == nil {
				continue
			}
			trimmedRoutes = append(trimmedRoutes, virtualHost.Routes[i])
		}
		virtualHost.Routes = trimmedRoutes
	}
}

func patchHTTPRoute(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHost *route.VirtualHost, routeIndex int, routesRemoved *bool, portMap model.GatewayPortMap) {
	for _, rp := range patches[networking.EnvoyFilter_HTTP_ROUTE] {
		applied := false
		if commonConditionMatch(patchContext, rp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, rp, portMap) &&
			virtualHostMatch(virtualHost, rp) &&
			routeMatch(virtualHost.Routes[routeIndex], rp) {

			// different virtualHosts may share same routes pointer
			virtualHost.Routes = cloneVhostRoutes(virtualHost.Routes)
			if rp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				virtualHost.Routes[routeIndex] = nil
				*routesRemoved = true
				return
			} else if rp.Operation == networking.EnvoyFilter_Patch_MERGE {
				proto.Merge(virtualHost.Routes[routeIndex], rp.Value)
			}
			applied = true
		}
		IncrementEnvoyFilterMetric(rp.Key(), Route, applied)
	}
}

func routeConfigurationMatch(patchContext networking.EnvoyFilter_PatchContext, rc *route.RouteConfiguration,
	rp *model.EnvoyFilterConfigPatchWrapper, portMap model.GatewayPortMap) bool {
	rMatch := rp.Match.GetRouteConfiguration()
	if rMatch == nil {
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
		if rMatch.PortNumber != 0 && int(rMatch.PortNumber) != listenerPort {
			return false
		}

		if rMatch.Name != "" && rMatch.Name != rc.Name {
			return false
		}

		return true
	}

	// This is a gateway. Get all the fields in the gateway's RDS route name
	routePortNumber, portName, gateway := model.ParseGatewayRDSRouteName(rc.Name)
	if rMatch.PortNumber != 0 && !anyPortMatches(portMap, routePortNumber, int(rMatch.PortNumber)) {
		return false
	}
	if rMatch.PortName != "" && rMatch.PortName != portName {
		return false
	}
	if rMatch.Gateway != "" && rMatch.Gateway != gateway {
		return false
	}

	if rMatch.Name != "" && rMatch.Name != rc.Name {
		return false
	}

	return true
}

func anyPortMatches(m model.GatewayPortMap, number int, matchNumber int) bool {
	if servicePorts, f := m[number]; f {
		// We do have service ports mapping to this, see if we match those
		for s := range servicePorts {
			if s == matchNumber {
				return true
			}
		}
		return false
	}
	// Otherwise, check the port directly
	return number == matchNumber
}

func virtualHostMatch(vh *route.VirtualHost, rp *model.EnvoyFilterConfigPatchWrapper) bool {
	rMatch := rp.Match.GetRouteConfiguration()
	if rMatch == nil {
		return true
	}

	match := rMatch.Vhost
	if match == nil {
		// match any virtual host in the named route configuration
		return true
	}
	if vh == nil {
		// route configuration has a specific match for a virtual host but
		// we do not have a virtual host to match.
		return false
	}
	// check if virtual host names match
	return match.Name == "" || match.Name == vh.Name
}

func routeMatch(httpRoute *route.Route, rp *model.EnvoyFilterConfigPatchWrapper) bool {
	rMatch := rp.Match.GetRouteConfiguration()
	if rMatch == nil {
		return true
	}

	vMatch := rMatch.Vhost
	if vMatch == nil {
		// match any virtual host in the named httpRoute configuration
		return true
	}

	match := vMatch.Route
	if match == nil {
		// match any httpRoute in the virtual host
		return true
	}

	if httpRoute == nil {
		// we have a specific match for particular httpRoute but
		// we do not have a httpRoute to match.
		return false
	}

	// check if httpRoute names match
	if match.Name != "" && match.Name != httpRoute.Name {
		return false
	}

	if match.Action != networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ANY {
		switch httpRoute.Action.(type) {
		case *route.Route_Route:
			return match.Action == networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ROUTE
		case *route.Route_Redirect:
			return match.Action == networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_REDIRECT
		case *route.Route_DirectResponse:
			return match.Action == networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_DIRECT_RESPONSE
		}
	}
	return true
}

func cloneVhostRoutes(routes []*route.Route) []*route.Route {
	out := make([]*route.Route, len(routes))
	for i := 0; i < len(routes); i++ {
		clone := proto.Clone(routes[i]).(*route.Route)
		out[i] = clone
	}
	return out
}
