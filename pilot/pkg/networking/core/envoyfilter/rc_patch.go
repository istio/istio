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
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto/merge"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

func ApplyRouteConfigurationPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	proxy *model.Proxy,
	efw *model.MergedEnvoyFilterWrapper,
	routeConfiguration *route.RouteConfiguration,
) (out *route.RouteConfiguration) {
	defer runtime.HandleCrash(runtime.LogPanic, func(any) {
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
			merge.Merge(routeConfiguration, rp.Value)
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
	routeConfiguration *route.RouteConfiguration, portMap model.GatewayPortMap,
) {
	removedVirtualHosts := sets.New[string]()
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
	if removedVirtualHosts.Len() > 0 {
		routeConfiguration.VirtualHosts = slices.FilterInPlace(routeConfiguration.VirtualHosts, func(virtualHost *route.VirtualHost) bool {
			return !removedVirtualHosts.Contains(virtualHost.Name)
		})
	}
}

// patchVirtualHost patches passed in virtual host if it is MERGE operation.
// The return value indicates whether the virtual host has been removed for REMOVE operations.
func patchVirtualHost(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHosts []*route.VirtualHost,
	idx int, portMap model.GatewayPortMap,
) bool {
	for _, rp := range patches[networking.EnvoyFilter_VIRTUAL_HOST] {
		applied := false
		if commonConditionMatch(patchContext, rp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, rp, portMap) &&
			virtualHostMatch(virtualHosts[idx], rp) {
			applied = true
			if rp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				return true
			} else if rp.Operation == networking.EnvoyFilter_Patch_MERGE {
				merge.Merge(virtualHosts[idx], rp.Value)
			} else if rp.Operation == networking.EnvoyFilter_Patch_REPLACE {
				virtualHosts[idx] = proto.Clone(rp.Value).(*route.VirtualHost)
			}
		}
		IncrementEnvoyFilterMetric(rp.Key(), VirtualHost, applied)
	}
	patchHTTPRoutes(patchContext, patches, routeConfiguration, virtualHosts[idx], portMap)
	return false
}

func hasRouteMatch(patchContext networking.EnvoyFilter_PatchContext, rp *model.EnvoyFilterConfigPatchWrapper) bool {
	if patchContext == networking.EnvoyFilter_WAYPOINT {
		wMatch := rp.Match.GetWaypoint()
		if wMatch == nil {
			return false
		}

		return wMatch.GetRoute() != nil
	}

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
	routeConfiguration *route.RouteConfiguration, virtualHost *route.VirtualHost, portMap model.GatewayPortMap,
) {
	clonedVhostRoutes := false
	routesRemoved := false
	// Apply the route level removes/merges if any.
	for index := range virtualHost.Routes {
		patchHTTPRoute(patchContext, patches, routeConfiguration, virtualHost, index, &routesRemoved, portMap, &clonedVhostRoutes)
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
			if !hasRouteMatch(patchContext, rp) {
				virtualHost.Routes = append(virtualHost.Routes, proto.Clone(rp.Value).(*route.Route))
				continue
			}
			virtualHost.Routes, applied = insertAfterFunc(
				virtualHost.Routes,
				func(e *route.Route) (bool, *route.Route) {
					if routeMatch(patchContext, e, rp) {
						return true, proto.Clone(rp.Value).(*route.Route)
					}
					return false, nil
				},
			)
		} else if rp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a route match is same as insert in the beginning
			if !hasRouteMatch(patchContext, rp) {
				virtualHost.Routes = append([]*route.Route{proto.Clone(rp.Value).(*route.Route)}, virtualHost.Routes...)
				continue
			}
			virtualHost.Routes, applied = insertBeforeFunc(
				virtualHost.Routes,
				func(e *route.Route) (bool, *route.Route) {
					if routeMatch(patchContext, e, rp) {
						return true, proto.Clone(rp.Value).(*route.Route)
					}
					return false, nil
				},
			)
		} else if rp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			// insert first without a route match is same as insert in the beginning
			if !hasRouteMatch(patchContext, rp) {
				virtualHost.Routes = append([]*route.Route{proto.Clone(rp.Value).(*route.Route)}, virtualHost.Routes...)
				continue
			}

			// find the matching route first
			insertPosition := -1
			for i := 0; i < len(virtualHost.Routes); i++ {
				if routeMatch(patchContext, virtualHost.Routes[i], rp) {
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
			virtualHost.Routes = append([]*route.Route{proto.Clone(rp.Value).(*route.Route)}, virtualHost.Routes...)
		}
		IncrementEnvoyFilterMetric(rp.Key(), Route, applied)
	}
	if routesRemoved {
		virtualHost.Routes = slices.FilterInPlace(virtualHost.Routes, func(r *route.Route) bool {
			return r != nil
		})
	}
}

func patchHTTPRoute(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHost *route.VirtualHost, routeIndex int, routesRemoved *bool, portMap model.GatewayPortMap,
	clonedVhostRoutes *bool,
) {
	for _, rp := range patches[networking.EnvoyFilter_HTTP_ROUTE] {
		applied := false
		if commonConditionMatch(patchContext, rp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, rp, portMap) &&
			virtualHostMatch(virtualHost, rp) &&
			routeMatch(patchContext, virtualHost.Routes[routeIndex], rp) {
			if !*clonedVhostRoutes {
				// different virtualHosts may share same routes pointer
				virtualHost.Routes = slices.Clone(virtualHost.Routes)
				*clonedVhostRoutes = true
			}
			if rp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				virtualHost.Routes[routeIndex] = nil
				*routesRemoved = true
				return
			} else if rp.Operation == networking.EnvoyFilter_Patch_MERGE {
				cloneVhostRouteByRouteIndex(virtualHost, routeIndex)
				merge.Merge(virtualHost.Routes[routeIndex], rp.Value)
			}
			applied = true
		}
		IncrementEnvoyFilterMetric(rp.Key(), Route, applied)
	}
}

func routeConfigurationMatch(patchContext networking.EnvoyFilter_PatchContext, rc *route.RouteConfiguration,
	rp *model.EnvoyFilterConfigPatchWrapper, portMap model.GatewayPortMap,
) bool {
	if patchContext == networking.EnvoyFilter_WAYPOINT {
		return true
	}

	rMatch := rp.Match.GetRouteConfiguration()
	if rMatch == nil {
		return true
	}

	// we match on the port number and virtual host for sidecars
	// we match on port number, server port name, gateway name, plus virtual host for gateways
	if patchContext != networking.EnvoyFilter_GATEWAY {
		if rMatch.Name != "" && rMatch.Name != rc.Name {
			return false
		}
		// FIXME: Ports on a route can be 0. the API only takes uint32 for ports
		// We should either make that field in API as a wrapper type or switch to int
		if rMatch.PortNumber != 0 && int(rMatch.PortNumber) != getListenerPortFromName(rc.Name) {
			return false
		}
		return true
	}

	// This is a gateway. Get all the fields in the gateway's RDS route name
	if rMatch.Name != "" && rMatch.Name != rc.Name {
		return false
	}
	routePortNumber, portName, gateway := model.ParseGatewayRDSRouteName(rc.Name)
	if rMatch.PortName != "" && rMatch.PortName != portName {
		return false
	}
	if rMatch.Gateway != "" && rMatch.Gateway != gateway {
		return false
	}
	if rMatch.PortNumber != 0 && !anyPortMatches(portMap, routePortNumber, int(rMatch.PortNumber)) {
		return false
	}
	return true
}

func getListenerPortFromName(name string) int {
	listenerPort := 0
	if strings.HasPrefix(name, string(model.TrafficDirectionInbound)) {
		_, _, _, listenerPort = model.ParseSubsetKey(name)
	} else {
		listenerPort, _ = strconv.Atoi(name)
	}
	return listenerPort
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

	// check if virtual host name and a domain name matches
	return (match.Name == "" || match.Name == vh.Name) &&
		(match.DomainName == "" || slices.Contains(vh.Domains, match.DomainName))
}

type RouteWithNameMatch interface {
	GetName() string
}

func routeNameMatch(httpRoute *route.Route, nameMatch RouteWithNameMatch) bool {
	if httpRoute == nil {
		// we have a specific match for particular route but
		// we do not have a route to match.
		return false
	}

	// check if httpRoute names match
	name := nameMatch.GetName()
	if name != "" && name != httpRoute.Name {
		return false
	}

	return true
}

func routeMatch(patchContext networking.EnvoyFilter_PatchContext, httpRoute *route.Route, rp *model.EnvoyFilterConfigPatchWrapper) bool {
	if patchContext == networking.EnvoyFilter_WAYPOINT {
		wMatch := rp.Match.GetWaypoint()
		if wMatch == nil {
			return true
		}

		rMatch := wMatch.GetRoute()
		if rMatch == nil {
			// match any route in the waypoint configuration
			return true
		}

		if !routeNameMatch(httpRoute, rMatch) {
			return false
		}

		return true
	}
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

	if !routeNameMatch(httpRoute, match) {
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

func cloneVhostRouteByRouteIndex(virtualHost *route.VirtualHost, routeIndex int) {
	virtualHost.Routes[routeIndex] = protomarshal.Clone(virtualHost.Routes[routeIndex])
}
