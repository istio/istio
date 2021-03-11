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
	"github.com/golang/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/pkg/log"
)

func ApplyRouteConfigurationPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	proxy *model.Proxy,
	push *model.PushContext,
	routeConfiguration *route.RouteConfiguration) (out *route.RouteConfiguration) {
	defer runtime.HandleCrash(runtime.LogPanic, func(interface{}) {
		log.Errorf("route patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the route generated before to reduce the influence.
	out = routeConfiguration

	efw := push.EnvoyFilters(proxy)
	if efw == nil {
		return out
	}

	// only merge is applicable for route configuration.
	for _, cp := range efw.Patches[networking.EnvoyFilter_ROUTE_CONFIGURATION] {
		if cp.Operation != networking.EnvoyFilter_Patch_MERGE {
			continue
		}

		if commonConditionMatch(patchContext, cp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, cp) {
			proto.Merge(routeConfiguration, cp.Value)
		}
	}

	doVirtualHostListOperation(patchContext, efw.Patches, routeConfiguration)

	return routeConfiguration
}

func doVirtualHostListOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration) {
	virtualHostsRemoved := false
	// first do removes/merges
	for _, vhost := range routeConfiguration.VirtualHosts {
		doVirtualHostOperation(patchContext, patches, routeConfiguration, vhost, &virtualHostsRemoved)
	}

	// now for the adds
	for _, cp := range patches[networking.EnvoyFilter_VIRTUAL_HOST] {
		if cp.Operation != networking.EnvoyFilter_Patch_ADD {
			continue
		}
		if commonConditionMatch(patchContext, cp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, cp) {
			routeConfiguration.VirtualHosts = append(routeConfiguration.VirtualHosts, proto.Clone(cp.Value).(*route.VirtualHost))
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
}

func doVirtualHostOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHost *route.VirtualHost, virtualHostRemoved *bool) {
	for _, cp := range patches[networking.EnvoyFilter_VIRTUAL_HOST] {
		if commonConditionMatch(patchContext, cp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, cp) &&
			virtualHostMatch(virtualHost, cp) {
			if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				virtualHost.Name = ""
				*virtualHostRemoved = true
				// nothing more to do.
				return
			} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
				proto.Merge(virtualHost, cp.Value)
			}
		}
	}
	doHTTPRouteListOperation(patchContext, patches, routeConfiguration, virtualHost)
}

func hasRouteMatch(cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetRouteConfiguration()
	if cMatch == nil {
		return false
	}

	vhMatch := cMatch.Vhost
	if vhMatch == nil {
		return false
	}

	return vhMatch.Route != nil
}

func doHTTPRouteListOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHost *route.VirtualHost) {
	routesRemoved := false
	// Apply the route level removes/merges if any.
	for index := range virtualHost.Routes {
		doHTTPRouteOperation(patchContext, patches, routeConfiguration, virtualHost, index, &routesRemoved)
	}

	// now for the adds
	for _, cp := range patches[networking.EnvoyFilter_HTTP_ROUTE] {
		if !commonConditionMatch(patchContext, cp) ||
			!routeConfigurationMatch(patchContext, routeConfiguration, cp) ||
			!virtualHostMatch(virtualHost, cp) {
			continue
		}

		if cp.Operation == networking.EnvoyFilter_Patch_ADD {
			virtualHost.Routes = append(virtualHost.Routes, proto.Clone(cp.Value).(*route.Route))
		} else if cp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a route match is same as ADD in the end
			if !hasRouteMatch(cp) {
				virtualHost.Routes = append(virtualHost.Routes, proto.Clone(cp.Value).(*route.Route))
				continue
			}
			// find the matching route first
			insertPosition := -1
			for i := 0; i < len(virtualHost.Routes); i++ {
				if routeMatch(virtualHost.Routes[i], cp) {
					insertPosition = i + 1
					break
				}
			}

			if insertPosition == -1 {
				continue
			}

			clonedVal := proto.Clone(cp.Value).(*route.Route)
			virtualHost.Routes = append(virtualHost.Routes, clonedVal)
			if insertPosition < len(virtualHost.Routes)-1 {
				copy(virtualHost.Routes[insertPosition+1:], virtualHost.Routes[insertPosition:])
				virtualHost.Routes[insertPosition] = clonedVal
			}
		} else if cp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE || cp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			// insert before/first without a route match is same as insert in the beginning
			if !hasRouteMatch(cp) {
				virtualHost.Routes = append([]*route.Route{proto.Clone(cp.Value).(*route.Route)}, virtualHost.Routes...)
				continue
			}
			// find the matching route first
			insertPosition := -1
			for i := 0; i < len(virtualHost.Routes); i++ {
				if routeMatch(virtualHost.Routes[i], cp) {
					insertPosition = i
					break
				}
			}

			// If matching route is not found, then don't insert and continue.
			if insertPosition == -1 {
				continue
			}

			// In case of INSERT_FIRST, if a match is found, still insert it at the top of the routes.
			if cp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
				insertPosition = 0
			}

			clonedVal := proto.Clone(cp.Value).(*route.Route)
			virtualHost.Routes = append(virtualHost.Routes, clonedVal)
			copy(virtualHost.Routes[insertPosition+1:], virtualHost.Routes[insertPosition:])
			virtualHost.Routes[insertPosition] = clonedVal
		}
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

func doHTTPRouteOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	routeConfiguration *route.RouteConfiguration, virtualHost *route.VirtualHost, routeIndex int, routesRemoved *bool) {
	for _, cp := range patches[networking.EnvoyFilter_HTTP_ROUTE] {
		if commonConditionMatch(patchContext, cp) &&
			routeConfigurationMatch(patchContext, routeConfiguration, cp) &&
			virtualHostMatch(virtualHost, cp) &&
			routeMatch(virtualHost.Routes[routeIndex], cp) {

			// different virtualHosts may share same routes pointer
			virtualHost.Routes = cloneVhostRoutes(virtualHost.Routes)
			if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				virtualHost.Routes[routeIndex] = nil
				*routesRemoved = true
				return
			} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
				proto.Merge(virtualHost.Routes[routeIndex], cp.Value)
			}
		}
	}
}

func routeConfigurationMatch(patchContext networking.EnvoyFilter_PatchContext, rc *route.RouteConfiguration,
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
		// we do not have a virtual host to match.
		return false
	}
	// check if virtual host names match
	return match.Name == "" || match.Name == vh.Name
}

func routeMatch(httpRoute *route.Route, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetRouteConfiguration()
	if cMatch == nil {
		return true
	}

	vMatch := cMatch.Vhost
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
