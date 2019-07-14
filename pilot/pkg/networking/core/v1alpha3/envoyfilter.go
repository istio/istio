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
	"fmt"
	"net"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

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
func insertUserFilters(in *plugin.InputParams, listener *xdsapi.Listener,
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
		if !listenerMatch(in, listenerIPAddress, f.ListenerMatch) {
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
					insertHTTPFilter(listener.Name, &listener.FilterChains[cnum], httpConnectionManagers[cnum], f, util.IsXDSMarshalingToAnyEnabled(in.Node))
				} else {
					// http listener, tcp filter
					insertNetworkFilter(listener.Name, &listener.FilterChains[cnum], f)
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
				insertNetworkFilter(listener.Name, &listener.FilterChains[cnum], f)
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

func listenerMatch(in *plugin.InputParams, listenerIP net.IP,
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

func insertHTTPFilter(listenerName string, filterChain *listener.FilterChain, hcm *http_conn.HttpConnectionManager,
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

func insertNetworkFilter(listenerName string, filterChain *listener.FilterChain,
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

func buildXDSObjectFromValue(applyTo networking.EnvoyFilter_ApplyTo, value *types.Value) (proto.Message, error) {
	var obj proto.Message
	switch applyTo {
	case networking.EnvoyFilter_CLUSTER:
		obj = &xdsapi.Cluster{}
	case networking.EnvoyFilter_LISTENER:
		obj = &xdsapi.Listener{}
	case networking.EnvoyFilter_ROUTE_CONFIGURATION:
		obj = &xdsapi.RouteConfiguration{}
	case networking.EnvoyFilter_FILTER_CHAIN:
		obj = &listener.FilterChain{}
	case networking.EnvoyFilter_HTTP_FILTER:
		obj = &http_conn.HttpFilter{}
	case networking.EnvoyFilter_NETWORK_FILTER:
		obj = &listener.Filter{}
	case networking.EnvoyFilter_VIRTUAL_HOST:
		obj = &route.VirtualHost{}
	default:
		return nil, fmt.Errorf("unknown object type")
	}

	val := value.GetStringValue()
	if val != "" {
		jsonum := &jsonpb.Unmarshaler{}
		r := strings.NewReader(val)
		err := jsonum.Unmarshal(r, obj)
		if err != nil {
			return nil, err
		}
	}

	return obj, nil
}

func applyClusterConfigPatches(env *model.Environment, proxy *model.Proxy,
	push *model.PushContext, clusters []*xdsapi.Cluster) []*xdsapi.Cluster {
	// TODO: multiple envoy filters per workload
	filterCRD := getUserFiltersForWorkload(env, proxy.WorkloadLabels)
	if filterCRD == nil {
		return clusters
	}

	// remove cluster if there is operation is remove, and matches
	clustersRemoved := false

	// First process remove operations, then the merge and finally the add.
	// If add is done before remove, then remove could end up deleting a cluster that
	// was added by the user.
	for _, cp := range filterCRD.ConfigPatches {
		if cp.Patch == nil || cp.ApplyTo != networking.EnvoyFilter_CLUSTER {
			continue
		}

		if cp.Patch.Operation != networking.EnvoyFilter_Patch_REMOVE {
			continue
		}

		for i := range clusters {
			if matchApproximateContext(proxy, cp.Match) && clusterMatch(clusters[i], cp.Match) {
				clusters[i] = nil
				clustersRemoved = true
			}
		}
	}

	// TODO: Create a EnvoyFilterContext struct, just like SidecarContext
	// pre-process the values and convert them into appropriate structs when initializing the push context
	for _, cp := range filterCRD.ConfigPatches {
		if cp.Patch == nil || cp.ApplyTo != networking.EnvoyFilter_CLUSTER {
			continue
		}

		if cp.Patch.Operation != networking.EnvoyFilter_Patch_MERGE {
			continue
		}

		userChanges, err := buildXDSObjectFromValue(cp.ApplyTo, cp.Patch.Value)
		if err != nil {
			//log.Warnf("Failed to unmarshal provided value into cluster")
			continue
		}

		for i := range clusters {
			if clusters[i] == nil {
				// deleted by the remove operation
				continue
			}
			if matchExactContext(proxy, clusters[i], cp.Match) && clusterMatch(clusters[i], cp.Match) {
					proto.Merge(clusters[i], userChanges)
			}
		}
	}

	// Add cluster if there is operation is add, and patch context matches
	for _, cp := range filterCRD.ConfigPatches {
		if cp.Patch == nil || cp.ApplyTo != networking.EnvoyFilter_CLUSTER {
			continue
		}

		if cp.Patch.Operation != networking.EnvoyFilter_Patch_ADD {
			continue
		}

		if !matchApproximateContext(proxy, cp.Match) {
			continue
		}

		newCluster, err := buildXDSObjectFromValue(cp.ApplyTo, cp.Patch.Value)
		if err != nil {
			// log.Warnf("Failed to unmarshal provided value into cluster")
			continue
		}
		clusters = append(clusters, newCluster.(*xdsapi.Cluster))
	}

	if clustersRemoved {
		trimmedClusters := make([]*xdsapi.Cluster, len(clusters))
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

func applyListenerConfigPatches(listeners []*xdsapi.Listener, env *model.Environment, labels model.LabelsCollection) []*xdsapi.Listener {
	filterCRD := getUserFiltersForWorkload(env, labels)
	if filterCRD == nil {
		return listeners
	}

	for _, cp := range filterCRD.ConfigPatches {
		if cp.GetPatch() == nil {
			continue
		}

		if cp.GetMatch() == nil && cp.GetApplyTo() == networking.EnvoyFilter_LISTENER {
			if cp.GetPatch().GetOperation() == networking.EnvoyFilter_Patch_ADD {
				newListener, err := buildListenerFromEnvoyConfig(cp.GetPatch().GetValue())
				if err != nil {
					log.Warnf("Failed to unmarshal provided value into listener")
					continue
				}
				listeners = append(listeners, newListener)
			}
		}
	}

	return listeners
}

func buildListenerFromEnvoyConfig(value *types.Value) (*xdsapi.Listener, error) {
	listener := xdsapi.Listener{}
	val := value.GetStringValue()
	if val != "" {
		jsonum := &jsonpb.Unmarshaler{}
		r := strings.NewReader(val)
		err := jsonum.Unmarshal(r, &listener)
		if err != nil {
			return nil, err
		}
	}

	return &listener, nil
}

// Matches if context is any, or if proxy is gateway and context is gateway,
// or if proxy is sidecar and context is sidecar_inbound or outbound
func matchApproximateContext(proxy *model.Proxy,
	matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch) bool {
	if matchCondition == nil {
		return true
	}

	if matchCondition.Context == networking.EnvoyFilter_ANY {
		return true
	}

	if proxy.Type == model.Router {
		if matchCondition.Context == networking.EnvoyFilter_GATEWAY {
			return true
		}
		return false
	}

	// we now have a sidecar proxy.
	if matchCondition.Context == networking.EnvoyFilter_GATEWAY {
		return false
	}

	return true
}

func matchExactContext(proxy *model.Proxy, cluster *xdsapi.Cluster,
	matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch) bool {
	if matchCondition == nil {
		return true
	}

	if matchCondition.Context == networking.EnvoyFilter_ANY {
		return true
	}

	if proxy.Type == model.Router {
		if matchCondition.Context == networking.EnvoyFilter_GATEWAY {
			return true
		}
		return false
	}

	// we now have a sidecar proxy.
	if matchCondition.Context == networking.EnvoyFilter_GATEWAY {
		return false
	}

	if matchCondition.Context == networking.EnvoyFilter_SIDECAR_INBOUND {
		if 	strings.HasPrefix(cluster.Name, string(model.TrafficDirectionInbound)) {
			return true
		}
		return false
	}

	// we have sidecar_outbound context. Check if cluster is outbound
	if 	strings.HasPrefix(cluster.Name, string(model.TrafficDirectionOutbound)) {
		return true
	}
	return false
}

func clusterMatch(cluster *xdsapi.Cluster, matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch) bool {
	if matchCondition == nil {
		return true
	}

	cMatch := matchCondition.GetCluster()
	if cMatch == nil {
		return true
	}

	if cMatch.Name != "" {
		if cMatch.Name != cluster.Name {
			return false
		}
		// cluster name matched. Nothing else matters.
		return true
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
	if port != 0 && int(cMatch.PortNumber) != port {
		return false
	}
	return true
}
