// Copyright 2018 Istio Authors
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
	"net"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/pkg/log"
)

// We process EnvoyFilter CRDs after calling all plugins and building the listener with the required filter chains
// Having the entire filter chain is essential because users can specify one or more filters to be inserted
// before/after  a filter or remove one or more filters.
// We use the plugin.InputParams as a convenience object to pass around parameters like proxy, proxyInstances, ports,
// etc., instead of having a long argument list
// If one or more filters are added to the HTTP connection manager, we will update the last filter in the listener
// filter chain (which is the http connection manager) with the updated object.
func deprecatedInsertUserFilters(in *plugin.InputParams, listener *xdsapi.Listener,
	httpConnectionManagers []*http_conn.HttpConnectionManager) error { //nolint: unparam
	filterCRD := getUserFiltersForWorkload(in.Env, in.Node.WorkloadLabels)
	if filterCRD == nil {
		return nil
	}

	listenerIPAddress := getListenerIPAddress(&listener.Address)
	if listenerIPAddress == nil {
		log.Warnf("Failed to parse IP Address from plugin listener")
	}

	for _, f := range filterCRD.Filters {
		if !deprecatedListenerMatch(in, listenerIPAddress, f.ListenerMatch) {
			continue
		}
		// 4 cases of filter insertion
		// http listener, http filter
		// tcp listener, tcp filter
		// http listener, tcp filter
		// tcp listener, http filter -- invalid

		for cnum, lFilterChain := range listener.FilterChains {
			if util.IsHTTPFilterChain(lFilterChain) {
				// The listener match logic does not take into account the listener protocol
				// because filter chains in a listener can have multiple protocols.
				// for each filter chain, if the filter chain has a http connection manager,
				// treat it as a http listener
				// ListenerProtocol defaults to ALL. But if user specified listener protocol TCP, then
				// skip this filter chain as its a HTTP filter chain
				if f.ListenerMatch != nil &&
					!(f.ListenerMatch.ListenerProtocol == networking.EnvoyFilter_DeprecatedListenerMatch_ALL ||
						f.ListenerMatch.ListenerProtocol == networking.EnvoyFilter_DeprecatedListenerMatch_HTTP) {
					continue
				}

				// Now that the match condition is true, insert the filter if compatible
				// http listener, http filter case
				if f.FilterType == networking.EnvoyFilter_Filter_HTTP {
					// Insert into http connection manager
					deprecatedInsertHTTPFilter(listener.Name, &listener.FilterChains[cnum], httpConnectionManagers[cnum], f, util.IsXDSMarshalingToAnyEnabled(in.Node))
				} else {
					// http listener, tcp filter
					deprecatedInsertNetworkFilter(listener.Name, &listener.FilterChains[cnum], f)
				}
			} else {
				// The listener match logic does not take into account the listener protocol
				// because filter chains in a listener can have multiple protocols.
				// for each filter chain, if the filter chain does not have a http connection manager,
				// treat it as a tcp listener
				// ListenerProtocol defaults to ALL. But if user specified listener protocol HTTP, then
				// skip this filter chain as its a TCP filter chain
				if f.ListenerMatch != nil &&
					!(f.ListenerMatch.ListenerProtocol == networking.EnvoyFilter_DeprecatedListenerMatch_ALL ||
						f.ListenerMatch.ListenerProtocol == networking.EnvoyFilter_DeprecatedListenerMatch_TCP) {
					continue
				}

				// treat both as insert network filter X into network filter chain.
				// We cannot insert a HTTP in filter in network filter chain.
				// Even HTTP connection manager is a network filter
				if f.FilterType == networking.EnvoyFilter_Filter_HTTP {
					log.Warnf("Ignoring filter %s. Cannot insert HTTP filter in network filter chain",
						f.FilterName)
					continue
				}
				deprecatedInsertNetworkFilter(listener.Name, &listener.FilterChains[cnum], f)
			}
		}
	}
	return nil
}

// NOTE: There can be only one filter for a workload. If multiple filters are defined, the behavior
// is undefined.
func getUserFiltersForWorkload(env *model.Environment, labels model.LabelsCollection) *networking.EnvoyFilter {
	f := env.EnvoyFilter(labels)
	if f != nil {
		return f.Spec.(*networking.EnvoyFilter)
	}
	return nil
}

func getListenerIPAddress(address *core.Address) net.IP {
	if address != nil && address.Address != nil {
		switch t := address.Address.(type) {
		case *core.Address_SocketAddress:
			if t.SocketAddress != nil {
				ip := "0.0.0.0"
				if t.SocketAddress.Address != "::" {
					ip = t.SocketAddress.Address
				}
				return net.ParseIP(ip)
			}
		}
	}
	return nil
}

func deprecatedListenerMatch(in *plugin.InputParams, listenerIP net.IP,
	matchCondition *networking.EnvoyFilter_DeprecatedListenerMatch) bool {
	if matchCondition == nil {
		return true
	}

	// match by port first
	if matchCondition.PortNumber > 0 && in.Port.Port != int(matchCondition.PortNumber) {
		return false
	}

	// match by port name prefix
	if matchCondition.PortNamePrefix != "" {
		if !strings.HasPrefix(strings.ToLower(in.Port.Name), strings.ToLower(matchCondition.PortNamePrefix)) {
			return false
		}
	}

	// case ANY implies do not care about proxy type or direction
	if matchCondition.ListenerType != networking.EnvoyFilter_DeprecatedListenerMatch_ANY {
		// check if the current listener category matches with the user specified type
		if matchCondition.ListenerType != in.DeprecatedListenerCategory {
			return false
		}

		// Check if the node's role matches properly with the listener category
		switch matchCondition.ListenerType {
		case networking.EnvoyFilter_DeprecatedListenerMatch_GATEWAY:
			if in.Node.Type != model.Router {
				return false // We don't care about direction for gateways
			}
		case networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_INBOUND,
			networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND:
			if in.Node.Type != model.SidecarProxy {
				return false
			}
		}
	}

	// Listener protocol will be matched as we try to insert the filters

	if len(matchCondition.Address) > 0 {
		matched := false
		// if any of the addresses here match, return true
		for _, address := range matchCondition.Address {
			if strings.Contains(address, "/") {
				var ipNet *net.IPNet
				var err error
				if _, ipNet, err = net.ParseCIDR(address); err != nil {
					log.Warnf("Failed to parse address %s in EnvoyFilter: %v", address, err)
					continue
				}
				if ipNet.Contains(listenerIP) {
					matched = true
					break
				}
			} else if net.ParseIP(address).Equal(listenerIP) {
				matched = true
				break
			}
		}
		return matched
	}

	return true
}

func deprecatedInsertHTTPFilter(listenerName string, filterChain *listener.FilterChain, hcm *http_conn.HttpConnectionManager,
	envoyFilter *networking.EnvoyFilter_Filter, isXDSMarshalingToAnyEnabled bool) {
	filter := &http_conn.HttpFilter{
		Name:       envoyFilter.FilterName,
		ConfigType: &http_conn.HttpFilter_Config{Config: envoyFilter.FilterConfig},
	}

	position := networking.EnvoyFilter_InsertPosition_FIRST
	if envoyFilter.InsertPosition != nil {
		position = envoyFilter.InsertPosition.Index
	}

	oldLen := len(hcm.HttpFilters)
	switch position {
	case networking.EnvoyFilter_InsertPosition_FIRST, networking.EnvoyFilter_InsertPosition_BEFORE:
		hcm.HttpFilters = append([]*http_conn.HttpFilter{filter}, hcm.HttpFilters...)
		if position == networking.EnvoyFilter_InsertPosition_BEFORE {
			// bubble the filter to the right position scanning from beginning
			for i := 1; i < len(hcm.HttpFilters); i++ {
				if hcm.HttpFilters[i].Name != envoyFilter.InsertPosition.RelativeTo {
					hcm.HttpFilters[i-1], hcm.HttpFilters[i] = hcm.HttpFilters[i], hcm.HttpFilters[i-1]
				} else {
					break
				}
			}
		}
	case networking.EnvoyFilter_InsertPosition_LAST, networking.EnvoyFilter_InsertPosition_AFTER:
		hcm.HttpFilters = append(hcm.HttpFilters, filter)
		if position == networking.EnvoyFilter_InsertPosition_AFTER {
			// bubble the filter to the right position scanning from end
			for i := len(hcm.HttpFilters) - 2; i >= 0; i-- {
				if hcm.HttpFilters[i].Name != envoyFilter.InsertPosition.RelativeTo {
					hcm.HttpFilters[i+1], hcm.HttpFilters[i] = hcm.HttpFilters[i], hcm.HttpFilters[i+1]
				} else {
					break
				}
			}
		}
	}

	// Rebuild the HTTP connection manager in the network filter chain
	// Its the last filter in the filter chain
	filterStruct := listener.Filter{
		Name: xdsutil.HTTPConnectionManager,
	}
	if isXDSMarshalingToAnyEnabled {
		filterStruct.ConfigType = &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(hcm)}
	} else {
		filterStruct.ConfigType = &listener.Filter_Config{Config: util.MessageToStruct(hcm)}
	}
	filterChain.Filters[len(filterChain.Filters)-1] = filterStruct
	log.Infof("EnvoyFilters: Rebuilt HTTP Connection Manager %s (from %d filters to %d filters)",
		listenerName, oldLen, len(hcm.HttpFilters))
}

func deprecatedInsertNetworkFilter(listenerName string, filterChain *listener.FilterChain,
	envoyFilter *networking.EnvoyFilter_Filter) {
	filter := &listener.Filter{
		Name:       envoyFilter.FilterName,
		ConfigType: &listener.Filter_Config{Config: envoyFilter.FilterConfig},
	}

	position := networking.EnvoyFilter_InsertPosition_FIRST
	if envoyFilter.InsertPosition != nil {
		position = envoyFilter.InsertPosition.Index
	}

	oldLen := len(filterChain.Filters)
	switch position {
	case networking.EnvoyFilter_InsertPosition_FIRST, networking.EnvoyFilter_InsertPosition_BEFORE:
		filterChain.Filters = append([]listener.Filter{*filter}, filterChain.Filters...)
		if position == networking.EnvoyFilter_InsertPosition_BEFORE {
			// bubble the filter to the right position scanning from beginning
			for i := 1; i < len(filterChain.Filters); i++ {
				if filterChain.Filters[i].Name != envoyFilter.InsertPosition.RelativeTo {
					filterChain.Filters[i-1], filterChain.Filters[i] = filterChain.Filters[i], filterChain.Filters[i-1]
					break
				}
			}
		}
	case networking.EnvoyFilter_InsertPosition_LAST, networking.EnvoyFilter_InsertPosition_AFTER:
		filterChain.Filters = append(filterChain.Filters, *filter)
		if position == networking.EnvoyFilter_InsertPosition_AFTER {
			// bubble the filter to the right position scanning from end
			for i := len(filterChain.Filters) - 2; i >= 0; i-- {
				if filterChain.Filters[i].Name != envoyFilter.InsertPosition.RelativeTo {
					filterChain.Filters[i+1], filterChain.Filters[i] = filterChain.Filters[i], filterChain.Filters[i+1]
					break
				}
			}
		}
	}
	log.Infof("EnvoyFilters: Rebuilt network filter stack for listener %s (from %d filters to %d filters)",
		listenerName, oldLen, len(filterChain.Filters))
}

func applyClusterPatches(_ *model.Environment, proxy *model.Proxy,
	push *model.PushContext, clusters []*xdsapi.Cluster) []*xdsapi.Cluster {

	envoyFilterWrappers := push.EnvoyFilters(proxy)
	clustersRemoved := false
	for _, efw := range envoyFilterWrappers {
		// First process remove operations, then the merge and finally the add.
		// If add is done before remove, then remove could end up deleting a cluster that
		// was added by the user.
		for _, cp := range efw.ConfigPatches {
			if cp.ApplyTo != networking.EnvoyFilter_CLUSTER {
				continue
			}

			if cp.Operation != networking.EnvoyFilter_Patch_REMOVE {
				continue
			}

			for i := range clusters {
				if clusters[i] == nil {
					// deleted by the remove operation
					continue
				}

				if clusterMatch(proxy, clusters[i], cp.Match, cp.Operation) {
					clusters[i] = nil
					clustersRemoved = true
				}
			}
		}

		for _, cp := range efw.ConfigPatches {
			if cp.ApplyTo != networking.EnvoyFilter_CLUSTER {
				continue
			}

			if cp.Operation != networking.EnvoyFilter_Patch_MERGE {
				continue
			}

			for i := range clusters {
				if clusters[i] == nil {
					// deleted by the remove operation
					continue
				}
				if clusterMatch(proxy, clusters[i], cp.Match, cp.Operation) {
					proto.Merge(clusters[i], cp.Value)
				}
			}
		}

		// Add cluster if the operation is add, and patch context matches
		for _, cp := range efw.ConfigPatches {
			if cp.ApplyTo != networking.EnvoyFilter_CLUSTER {
				continue
			}

			if cp.Operation != networking.EnvoyFilter_Patch_ADD {
				continue
			}

			if clusterMatch(proxy, nil, cp.Match, cp.Operation) {
				clusters = append(clusters, cp.Value.(*xdsapi.Cluster))
			}
		}
	}

	if clustersRemoved {
		trimmedClusters := make([]*xdsapi.Cluster, 0, len(clusters))
		for i := range clusters {
			if clusters[i] == nil {
				continue
			}
			trimmedClusters = append(trimmedClusters, clusters[i])
		}
		clusters = trimmedClusters
	}
	return clusters
}

func applyListenerPatches(proxy *model.Proxy, push *model.PushContext, builder *ListenerBuilder) *ListenerBuilder {
	envoyFilterWrappers := push.EnvoyFilters(proxy)
	mergeRemoveListener := func(patchContext networking.EnvoyFilter_PatchContext,
		listener *xdsapi.Listener, cp *model.EnvoyFilterConfigPatchWrapper, objectsRemoved *bool) {
		if listenerMatch(patchContext, listener, cp) {
			if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				listener.Name = ""
				*objectsRemoved = true
			} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
				proto.Merge(listener, cp.Value)
			}
		}
	}
	// utility function to remove deleted listeners and rebuild the array if needed.
	rebuildListenerArray := func(listeners []*xdsapi.Listener) []*xdsapi.Listener {
		tempArray := make([]*xdsapi.Listener, 0, len(listeners))
		for _, l := range listeners {
			if l.Name != "" {
				tempArray = append(tempArray, l)
			}
		}
		return tempArray
	}
	doListenerOperation := func(patchContext networking.EnvoyFilter_PatchContext, listener *xdsapi.Listener, objectsRemoved *bool) {
		for _, efw := range envoyFilterWrappers {
			for _, cp := range efw.ConfigPatches {
				switch cp.ApplyTo {
				case networking.EnvoyFilter_LISTENER:
					if cp.Operation != networking.EnvoyFilter_Patch_REMOVE &&
						cp.Operation != networking.EnvoyFilter_Patch_MERGE {
						// we will add stuff in the end
						continue
					}
					mergeRemoveListener(patchContext, listener, cp, objectsRemoved)
				case networking.EnvoyFilter_FILTER_CHAIN:
					// handle adds, removes, merges
				case networking.EnvoyFilter_NETWORK_FILTER:
					// handle  adds, inserts, removes, merges
				case networking.EnvoyFilter_HTTP_FILTER:
					// match on  http conn man and handle adds, inserts, removes, merges
				}
			}
		}
	}
	doListenerListOperation := func(patchContext networking.EnvoyFilter_PatchContext, listeners []*xdsapi.Listener) []*xdsapi.Listener {
		objectsRemoved := false
		for _, listener := range listeners {
			doListenerOperation(patchContext, listener, &objectsRemoved)
		}
		// now the adds at listener level
		for _, efw := range envoyFilterWrappers {
			for _, cp := range efw.ConfigPatches {
				// todo inserts
				if cp.ApplyTo != networking.EnvoyFilter_LISTENER || cp.Operation != networking.EnvoyFilter_Patch_ADD {
					continue
				}
				if listenerMatch(patchContext, nil, cp) {
					listeners = append(listeners, cp.Value.(*xdsapi.Listener))
				}
			}
		}
		if objectsRemoved {
			return rebuildListenerArray(listeners)
		}
		return listeners
	}

	if proxy.Type == model.Router {
		builder.gatewayListeners = doListenerListOperation(networking.EnvoyFilter_GATEWAY, builder.gatewayListeners)
		return builder
	}

	// this is all for sidecar
	if builder.virtualInboundListener != nil {
		removed := false
		doListenerOperation(networking.EnvoyFilter_SIDECAR_INBOUND, builder.virtualInboundListener, &removed)
		if removed {
			builder.virtualInboundListener = nil
		}
	}
	if builder.virtualListener != nil {
		removed := false
		doListenerOperation(networking.EnvoyFilter_SIDECAR_OUTBOUND, builder.virtualListener, &removed)
		if removed {
			builder.virtualListener = nil
		}
	}

	builder.inboundListeners = doListenerListOperation(networking.EnvoyFilter_SIDECAR_INBOUND, builder.inboundListeners)
	builder.managementListeners = doListenerListOperation(networking.EnvoyFilter_SIDECAR_INBOUND, builder.managementListeners)
	builder.outboundListeners = doListenerListOperation(networking.EnvoyFilter_SIDECAR_OUTBOUND, builder.outboundListeners)

	return builder
}

func applyRouteConfigurationPatches(pluginParams *plugin.InputParams,
	routeConfiguration *xdsapi.RouteConfiguration) *xdsapi.RouteConfiguration {

	virtualHostsRemoved := false
	envoyFilterWrappers := pluginParams.Push.EnvoyFilters(pluginParams.Node)
	for _, efw := range envoyFilterWrappers {
		// remove & add are not applicable for route configuration but applicable for virtual hosts
		// remove virtual host if there is operation is remove, and matches

		// First process remove operations, then the merge and finally the add.
		// If add is done before remove, then remove could end up deleting a vhost that
		// was added by the user.
		for _, cp := range efw.ConfigPatches {
			if cp.ApplyTo != networking.EnvoyFilter_VIRTUAL_HOST {
				continue
			}

			if cp.Operation != networking.EnvoyFilter_Patch_REMOVE {
				continue
			}

			// iterate through all virtual hosts in a route and remove ones that match
			for i := range routeConfiguration.VirtualHosts {
				if routeConfiguration.VirtualHosts[i].Name == "" {
					// removed by another envoy filter
					continue
				}
				if routeConfigurationMatch(routeConfiguration, &routeConfiguration.VirtualHosts[i], pluginParams, cp.Match) {
					// set name to empty. We remove virtual hosts with empty names later in this function
					routeConfiguration.VirtualHosts[i].Name = ""
					virtualHostsRemoved = true
				}
			}
		}

		for _, cp := range efw.ConfigPatches {
			if cp.ApplyTo != networking.EnvoyFilter_ROUTE_CONFIGURATION &&
				cp.ApplyTo != networking.EnvoyFilter_VIRTUAL_HOST {
				continue
			}

			if cp.Operation != networking.EnvoyFilter_Patch_MERGE {
				continue
			}

			// if applying the merge at routeConfiguration level, then do standard proto merge
			if cp.ApplyTo == networking.EnvoyFilter_ROUTE_CONFIGURATION {
				if routeConfigurationMatch(routeConfiguration, nil, pluginParams, cp.Match) {
					proto.Merge(routeConfiguration, cp.Value)
				}
			} else {
				// This is for a specific virtual host. We have to iterate through all the vhosts in a route,
				// and match the specific virtual host to merge
				for i := range routeConfiguration.VirtualHosts {
					if routeConfiguration.VirtualHosts[i].Name == "" {
						// removed by another envoy filter
						continue
					}

					if routeConfigurationMatch(routeConfiguration, &routeConfiguration.VirtualHosts[i], pluginParams, cp.Match) {
						proto.Merge(&routeConfiguration.VirtualHosts[i], cp.Value)
					}
				}
			}
		}

		// Add virtual host if the operation is add, and patch context matches
		for _, cp := range efw.ConfigPatches {
			if cp.ApplyTo != networking.EnvoyFilter_VIRTUAL_HOST {
				continue
			}

			if cp.Operation != networking.EnvoyFilter_Patch_ADD {
				continue
			}

			if routeConfigurationMatch(routeConfiguration, nil, pluginParams, cp.Match) {
				routeConfiguration.VirtualHosts = append(routeConfiguration.VirtualHosts, *cp.Value.(*route.VirtualHost))
			}
		}
	}
	if virtualHostsRemoved {
		trimmedVirtualHosts := make([]route.VirtualHost, 0, len(routeConfiguration.VirtualHosts))
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

func clusterMatch(proxy *model.Proxy, cluster *xdsapi.Cluster,
	matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch, operation networking.EnvoyFilter_Patch_Operation) bool {
	if matchCondition == nil {
		return true
	}

	getClusterContext := func(proxy *model.Proxy, cluster *xdsapi.Cluster) networking.EnvoyFilter_PatchContext {
		if proxy.Type == model.Router {
			return networking.EnvoyFilter_GATEWAY
		}
		if strings.HasPrefix(cluster.Name, string(model.TrafficDirectionInbound)) {
			return networking.EnvoyFilter_SIDECAR_INBOUND
		}
		return networking.EnvoyFilter_SIDECAR_OUTBOUND
	}

	patchContextToProxyType := func(context networking.EnvoyFilter_PatchContext) model.NodeType {
		if context == networking.EnvoyFilter_GATEWAY {
			return model.Router
		}
		return model.SidecarProxy
	}

	// For cluster adds, cluster param will be nil. In this case, we simply have to match
	// between gateways and sidecar contexts. No inbound/outbound
	if operation == networking.EnvoyFilter_Patch_ADD {
		if matchCondition.Context == networking.EnvoyFilter_ANY {
			return true
		}

		return patchContextToProxyType(matchCondition.Context) == proxy.Type
	}

	// cluster is not nil. This is for merge and remove ops
	if matchCondition.Context != networking.EnvoyFilter_ANY {
		if matchCondition.Context != getClusterContext(proxy, cluster) {
			return false
		}
	}

	cMatch := matchCondition.GetCluster()
	if cMatch == nil {
		return true
	}

	if cMatch.Name != "" {
		return cMatch.Name == cluster.Name
	}

	_, subset, host, port := model.ParseSubsetKey(cluster.Name)

	if cMatch.Subset != "" && cMatch.Subset != subset {
		return false
	}

	if cMatch.Service != "" && model.Hostname(cMatch.Service) != host {
		return false
	}

	// FIXME: Ports on a cluster can be 0. the API only takes uint32 for ports
	// We should either make that field in API as a wrapper type or switch to int
	if cMatch.PortNumber != 0 && int(cMatch.PortNumber) != port {
		return false
	}
	return true
}

func listenerMatch(patchContext networking.EnvoyFilter_PatchContext, listener *xdsapi.Listener, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	if cp.Match == nil {
		return true
	}

	// For listener adds, listener param will be nil. In this case, we simply have to match
	// between gateways and sidecar contexts. No inbound/outbound
	if cp.Operation == networking.EnvoyFilter_Patch_ADD {
		return cp.Match.Context == patchContext || cp.Match.Context == networking.EnvoyFilter_ANY
	}

	// listener is not nil. This is for merge and remove ops
	if cp.Match.Context != networking.EnvoyFilter_ANY {
		if cp.Match.Context != patchContext {
			return false
		}
	}

	cMatch := cp.Match.GetListener()
	if cMatch == nil {
		return true
	}

	// FIXME: Ports on a listener can be 0. the API only takes uint32 for ports
	// We should either make that field in API as a wrapper type or switch to int
	if cMatch.PortNumber != 0 {
		sockAddr := listener.Address.GetSocketAddress()
		if sockAddr == nil || sockAddr.GetPortValue() != cMatch.PortNumber {
			return false
		}
	}

	if cMatch.Name != "" && cMatch.Name != listener.Name {
		return false
	}

	if cp.ApplyTo == networking.EnvoyFilter_LISTENER {
		// nothing more to match here.
		return true
	}

	matchFound := true
	if cMatch.FilterChain != nil {
		matchFound = false
		for _, fc := range listener.FilterChains {
			if filterChainMatch(cMatch.FilterChain, fc.FilterChainMatch) {
				matchFound = true
				if cMatch.FilterChain.Filter != nil {
					matchFound = false
					for _, networkFilter := range fc.Filters {
						// validation ensures that filter name is not empty
						if networkFilter.Name == cMatch.FilterChain.Filter.Name {
							matchFound = true
							// subfilter match is not necessary at it applies only to HTTP_FILTER
							// while here,  we are matching  only for listener/network_filter or filter_chain
						}
						if matchFound {
							break
						}
					}
				}
			}
			if matchFound {
				break
			}
		}
	}
	return matchFound
}

func routeConfigurationMatch(rc *xdsapi.RouteConfiguration, vh *route.VirtualHost, pluginParams *plugin.InputParams,
	matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch) bool {
	if matchCondition == nil {
		return true
	}

	if matchCondition.Context != networking.EnvoyFilter_ANY {
		if matchCondition.Context != pluginParams.ListenerCategory {
			return false
		}
	}

	cMatch := matchCondition.GetRouteConfiguration()
	if cMatch == nil {
		return true
	}

	// we match on the port number and virtual host for sidecars
	// we match on port number, server port name, gateway name, plus virtual host for gateways
	if pluginParams.Node.Type == model.SidecarProxy {
		// FIXME: Ports on a route can be 0. the API only takes uint32 for ports
		// We should either make that field in API as a wrapper type or switch to int
		if cMatch.PortNumber != 0 && int(cMatch.PortNumber) != pluginParams.Port.Port {
			return false
		}

		if cMatch.Name != "" && cMatch.Name != rc.Name {
			return false
		}

		// ports have matched for the rds in sidecar. Check for virtual host match if any
		return virtualHostMatch(cMatch.Vhost, vh)
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

	// all gateway fields have matched for the rds. Check for virtual host match if any
	return virtualHostMatch(cMatch.Vhost, vh)
}

func virtualHostMatch(match *networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch,
	vh *route.VirtualHost) bool {
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

func filterChainMatch(match *networking.EnvoyFilter_ListenerMatch_FilterChainMatch, fc *listener.FilterChainMatch) bool {
	if match == nil {
		return true
	}
	if match.Sni != "" {
		if fc == nil || len(fc.ServerNames) == 0 {
			return false
		}
		sniMatched := false
		for _, sni := range fc.ServerNames {
			if sni == match.Sni {
				sniMatched = true
				break
			}
		}
		if !sniMatched {
			return false
		}
	}

	if match.TransportProtocol != "" {
		if fc == nil || fc.TransportProtocol != match.TransportProtocol {
			return false
		}
	}
	return true
}
