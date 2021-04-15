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
	"errors"
	"fmt"

	xdslistener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/any"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/xds"
	"istio.io/pkg/log"
)

const (
	// VirtualOutboundListenerName is the name for traffic capture listener
	VirtualOutboundListenerName = "virtualOutbound"

	// VirtualInboundListenerName is the name for traffic capture listener
	VirtualInboundListenerName = "virtualInbound"
)

// patchDependencies struct holds patch and its dependencies.
type patchDependencies struct {
	patch *model.EnvoyFilterConfigPatchWrapper
	deps  []*model.EnvoyFilterConfigPatchWrapper
}

// patchSet is set of unique patches used for dependency calculation.
type patchSet map[*model.EnvoyFilterConfigPatchWrapper]struct{}

// ApplyListenerPatches applies patches to LDS output
func ApplyListenerPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	proxy *model.Proxy,
	push *model.PushContext,
	efw *model.EnvoyFilterWrapper,
	listeners []*xdslistener.Listener,
	skipAdds bool) (out []*xdslistener.Listener) {
	defer runtime.HandleCrash(runtime.LogPanic, func(interface{}) {
		IncrementEnvoyFilterErrorMetric(efw.Key(), Listener)
		log.Errorf("listeners patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the listeners generated before to reduce the influence.
	out = listeners

	if efw == nil {
		return
	}

	return patchListeners(patchContext, efw, listeners, skipAdds)
}

func patchListeners(
	patchContext networking.EnvoyFilter_PatchContext,
	efw *model.EnvoyFilterWrapper,
	listeners []*xdslistener.Listener,
	skipAdds bool) []*xdslistener.Listener {
	listenersRemoved := false
	filterKey := efw.Key()

	// do all the changes for a single envoy filter crd object. [including adds]
	// then move on to the next one

	// only removes/merges plus next level object operations [add/remove/merge]
	for _, listener := range listeners {
		if listener.Name == "" {
			// removed by another op
			continue
		}
		patchListener(patchContext, filterKey, efw.Patches, listener, &listenersRemoved)
	}
	// adds at listener level if enabled
	applied := false
	if !skipAdds {
		for _, lp := range efw.Patches[networking.EnvoyFilter_LISTENER] {
			if lp.Operation == networking.EnvoyFilter_Patch_ADD {
				if !commonConditionMatch(patchContext, lp) {
					continue
				}

				// clone before append. Otherwise, subsequent operations on this listener will corrupt
				// the master value stored in CP..
				listeners = append(listeners, proto.Clone(lp.Value).(*xdslistener.Listener))
				applied = true
			}
		}
	}
	IncrementEnvoyFilterMetric(filterKey, Listener, applied)
	if listenersRemoved {
		tempArray := make([]*xdslistener.Listener, 0, len(listeners))
		for _, l := range listeners {
			if l.Name != "" {
				tempArray = append(tempArray, l)
			}
		}
		return tempArray
	}
	return listeners
}

func patchListener(patchContext networking.EnvoyFilter_PatchContext,
	filterKey string,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, listenersRemoved *bool) {
	applied := false
	for _, lp := range patches[networking.EnvoyFilter_LISTENER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(listener, lp) {
			continue
		}
		applied = true
		if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			listener.Name = ""
			*listenersRemoved = true
			// terminate the function here as we have nothing more do to for this listener
			return
		} else if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			proto.Merge(listener, lp.Value)
		}
	}
	IncrementEnvoyFilterMetric(filterKey, Listener, applied)
	patchFilterChains(patchContext, filterKey, patches, listener)
}

func patchFilterChains(patchContext networking.EnvoyFilter_PatchContext,
	filterKey string,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener) {
	filterChainsRemoved := false
	for i, fc := range listener.FilterChains {
		if fc.Filters == nil {
			continue
		}
		patchFilterChain(patchContext, filterKey, patches, listener, listener.FilterChains[i], &filterChainsRemoved)
	}
	if fc := listener.GetDefaultFilterChain(); fc.GetFilters() != nil {
		removed := false
		patchFilterChain(patchContext, filterKey, patches, listener, fc, &removed)
		if removed {
			listener.DefaultFilterChain = nil
		}
	}
	applied := false
	for _, lp := range patches[networking.EnvoyFilter_FILTER_CHAIN] {
		if lp.Operation == networking.EnvoyFilter_Patch_ADD {
			if !commonConditionMatch(patchContext, lp) ||
				!listenerMatch(listener, lp) {
				continue
			}
			applied = true
			listener.FilterChains = append(listener.FilterChains, proto.Clone(lp.Value).(*xdslistener.FilterChain))
		}
	}
	IncrementEnvoyFilterMetric(filterKey, FilterChain, applied)
	if filterChainsRemoved {
		tempArray := make([]*xdslistener.FilterChain, 0, len(listener.FilterChains))
		for _, fc := range listener.FilterChains {
			if fc.Filters != nil {
				tempArray = append(tempArray, fc)
			}
		}
		listener.FilterChains = tempArray
	}
}

func patchFilterChain(patchContext networking.EnvoyFilter_PatchContext,
	filterKey string,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener,
	fc *xdslistener.FilterChain, filterChainRemoved *bool) {
	applied := false
	for _, lp := range patches[networking.EnvoyFilter_FILTER_CHAIN] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(listener, lp) ||
			!filterChainMatch(listener, fc, lp) {
			continue
		}
		applied = true
		if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			fc.Filters = nil
			*filterChainRemoved = true
			// nothing more to do in other patches as we removed this filter chain
			return
		} else if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			ret, err := mergeTransportSocketListener(fc, lp)
			if err != nil {
				log.Debugf("merge of transport socket failed for listener: %v", err)
				continue
			}
			if !ret {
				proto.Merge(fc, lp.Value)
			}
		}
	}
	IncrementEnvoyFilterMetric(filterKey, FilterChain, applied)
	patchNetworkFilters(patchContext, filterKey, patches, listener, fc)
}

// Test if the patch contains a config for TransportSocket
func mergeTransportSocketListener(fc *xdslistener.FilterChain, lp *model.EnvoyFilterConfigPatchWrapper) (bool, error) {
	lpValueCast, oklpCast := (lp.Value).(*xdslistener.FilterChain)
	if !oklpCast {
		return false, fmt.Errorf("cast of lp.Value failed: %v", oklpCast)
	}

	// Test if the patch contains a config for TransportSocket
	applyPatch := false
	if lpValueCast.GetTransportSocket() != nil {
		// Test if the listener contains a config for TransportSocket
		applyPatch = fc.GetTransportSocket() != nil && lpValueCast.GetTransportSocket().Name == fc.GetTransportSocket().Name
	} else {
		return false, nil
	}

	if applyPatch {
		// Merge the patch and the listener at a lower level
		dstListener := fc.GetTransportSocket().GetTypedConfig()
		srcPatch := lpValueCast.GetTransportSocket().GetTypedConfig()

		if dstListener != nil && srcPatch != nil {

			retVal, errMerge := util.MergeAnyWithAny(dstListener, srcPatch)
			if errMerge != nil {
				return false, fmt.Errorf("function mergeAnyWithAny failed for doFilterChainOperation: %v", errMerge)
			}

			// Merge the above result with the whole listener
			proto.Merge(dstListener, retVal)
		}
	}
	// Default: we won't call proto.Merge() if the patch is transportSocket and the listener isn't
	return true, nil
}

func patchNetworkFilters(patchContext networking.EnvoyFilter_PatchContext,
	filterKey string,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain) {
	networkFiltersRemoved := false
	for i, filter := range fc.Filters {
		if filter.Name == "" {
			continue
		}
		patchNetworkFilter(patchContext, filterKey, patches, listener, fc, fc.Filters[i], &networkFiltersRemoved)
	}
	applied := false
	for _, lp := range patches[networking.EnvoyFilter_NETWORK_FILTER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(listener, lp) ||
			!filterChainMatch(listener, fc, lp) {
			continue
		}
		if lp.Operation == networking.EnvoyFilter_Patch_ADD {
			fc.Filters = append(fc.Filters, proto.Clone(lp.Value).(*xdslistener.Filter))
			applied = true
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			fc.Filters = append([]*xdslistener.Filter{proto.Clone(lp.Value).(*xdslistener.Filter)}, fc.Filters...)
			applied = true
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a filter match is same as ADD in the end
			if !hasNetworkFilterMatch(lp) {
				fc.Filters = append(fc.Filters, proto.Clone(lp.Value).(*xdslistener.Filter))
				continue
			}
			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(fc.Filters); i++ {
				if networkFilterMatch(fc.Filters[i], lp) {
					insertPosition = i + 1
					break
				}
			}

			if insertPosition == -1 {
				continue
			}
			applied = true
			clonedVal := proto.Clone(lp.Value).(*xdslistener.Filter)
			fc.Filters = append(fc.Filters, clonedVal)
			if insertPosition < len(fc.Filters)-1 {
				copy(fc.Filters[insertPosition+1:], fc.Filters[insertPosition:])
				fc.Filters[insertPosition] = clonedVal
			}
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a filter match is same as insert in the beginning
			if !hasNetworkFilterMatch(lp) {
				fc.Filters = append([]*xdslistener.Filter{proto.Clone(lp.Value).(*xdslistener.Filter)}, fc.Filters...)
				continue
			}
			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(fc.Filters); i++ {
				if networkFilterMatch(fc.Filters[i], lp) {
					insertPosition = i
					break
				}
			}

			// If matching filter is not found, then don't insert and continue.
			if insertPosition == -1 {
				continue
			}
			applied = true
			clonedVal := proto.Clone(lp.Value).(*xdslistener.Filter)
			fc.Filters = append(fc.Filters, clonedVal)
			copy(fc.Filters[insertPosition+1:], fc.Filters[insertPosition:])
			fc.Filters[insertPosition] = clonedVal
		} else if lp.Operation == networking.EnvoyFilter_Patch_REPLACE {
			if !hasNetworkFilterMatch(lp) {
				continue
			}
			// find the matching filter first
			replacePosition := -1
			for i := 0; i < len(fc.Filters); i++ {
				if networkFilterMatch(fc.Filters[i], lp) {
					replacePosition = i
					break
				}
			}
			if replacePosition == -1 {
				continue
			}
			applied = true
			fc.Filters[replacePosition] = proto.Clone(lp.Value).(*xdslistener.Filter)
		}
	}
	IncrementEnvoyFilterMetric(filterKey, NetworkFilter, applied)
	if networkFiltersRemoved {
		tempArray := make([]*xdslistener.Filter, 0, len(fc.Filters))
		for _, filter := range fc.Filters {
			if filter.Name != "" {
				tempArray = append(tempArray, filter)
			}
		}
		fc.Filters = tempArray
	}
}

func patchNetworkFilter(patchContext networking.EnvoyFilter_PatchContext,
	filterKey string,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain,
	filter *xdslistener.Filter, networkFilterRemoved *bool) {
	applied := false
	for _, lp := range patches[networking.EnvoyFilter_NETWORK_FILTER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(listener, lp) ||
			!filterChainMatch(listener, fc, lp) ||
			!networkFilterMatch(filter, lp) {
			continue
		}
		if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			filter.Name = ""
			*networkFilterRemoved = true
			// nothing more to do in other patches as we removed this filter
			return
		} else if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			// proto merge doesn't work well when merging two filters with ANY typed configs
			// especially when the incoming lp.Value is a struct that could contain the json config
			// of an ANY typed filter. So convert our filter's typed config to Struct (retaining the any
			// typed output of json)
			if filter.GetTypedConfig() == nil {
				// TODO(rshriram): fixme
				// skip this op as we would possibly have to do a merge of Any with struct
				// which doesn't seem to work well.
				continue
			}
			userFilter := lp.Value.(*xdslistener.Filter)
			var err error
			// we need to be able to overwrite filter names or simply empty out a filter's configs
			// as they could be supplied through per route filter configs
			filterName := filter.Name
			if userFilter.Name != "" {
				filterName = userFilter.Name
			}
			var retVal *any.Any
			if userFilter.GetTypedConfig() != nil {
				applied = true
				// user has any typed struct
				// The type may not match up exactly. For example, if we use v2 internally but they use v3.
				// Assuming they are not using deprecated/new fields, we can safely swap out the TypeUrl
				// If we did not do this, proto.Merge below will panic (which is recovered), so even though this
				// is not 100% reliable its better than doing nothing
				if userFilter.GetTypedConfig().TypeUrl != filter.GetTypedConfig().TypeUrl {
					userFilter.ConfigType.(*xdslistener.Filter_TypedConfig).TypedConfig.TypeUrl = filter.GetTypedConfig().TypeUrl
				}
				if retVal, err = util.MergeAnyWithAny(filter.GetTypedConfig(), userFilter.GetTypedConfig()); err != nil {
					retVal = filter.GetTypedConfig()
				}
			}
			filter.Name = toCanonicalName(filterName)
			if retVal != nil {
				filter.ConfigType = &xdslistener.Filter_TypedConfig{TypedConfig: retVal}
			}
		}
	}
	IncrementEnvoyFilterMetric(filterKey, NetworkFilter, applied)
	if filter.Name == wellknown.HTTPConnectionManager {
		patchHTTPFilters(patchContext, filterKey, patches, listener, fc, filter)
	}
}

func patchHTTPFilters(patchContext networking.EnvoyFilter_PatchContext, filterKey string,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain, filter *xdslistener.Filter) {
	hcm := &http_conn.HttpConnectionManager{}
	if filter.GetTypedConfig() != nil {
		if err := filter.GetTypedConfig().UnmarshalTo(hcm); err != nil {
			return
			// todo: figure out a non noisy logging option here
			//  as this loop will be called very frequently
		}
	}
	httpFiltersRemoved := false
	for _, httpFilter := range hcm.HttpFilters {
		if httpFilter.Name == "" {
			continue
		}
		patchHTTPFilter(patchContext, filterKey, patches, listener, fc, filter, httpFilter, &httpFiltersRemoved)
	}

	// First collect all matched patches.
	mpatches := make([]*model.EnvoyFilterConfigPatchWrapper, 0)
	for _, lp := range patches[networking.EnvoyFilter_HTTP_FILTER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(listener, lp) ||
			!filterChainMatch(listener, fc, lp) ||
			!networkFilterMatch(filter, lp) {
			continue
		}
		mpatches = append(mpatches, lp)
	}

	// Order patches as per dependencies. This will also detect cycles.
	optaches, err := orderPatches(mpatches, hcm)
	// TODO(ramaraochavali): When cycle is detected instead of ignoring the entire patch,
	// consider applying remaining filters.
	if err != nil {
		IncrementEnvoyFilterMetric(filterKey, HttpFilter, false)
		log.Warnf("envoy filter has patches that have cyclic dependencies. So filter is skipped: %s", toString(optaches))
		return
	}

	// Finally apply the ordered patches.
	applied := false
	unique := sets.Set{}
	for _, pnode := range optaches {
		// First apply all dependencies.
		for _, dep := range pnode.deps {
			if !unique.Contains(dep.Value.(*http_conn.HttpFilter).Name) {
				applied = applyHttpFilterPatch(dep, hcm) && applied
				unique.Insert(dep.Value.(*http_conn.HttpFilter).Name)
			}
		}
		// Then apply the main patch.
		if !unique.Contains(pnode.patch.Value.(*http_conn.HttpFilter).Name) {
			applied = applyHttpFilterPatch(pnode.patch, hcm) && applied
			unique.Insert(pnode.patch.Value.(*http_conn.HttpFilter).Name)
		}
	}
	if httpFiltersRemoved {
		tempArray := make([]*http_conn.HttpFilter, 0, len(hcm.HttpFilters))
		for _, filter := range hcm.HttpFilters {
			if filter.Name != "" {
				tempArray = append(tempArray, filter)
			}
		}
		hcm.HttpFilters = tempArray
	}
	IncrementEnvoyFilterMetric(filterKey, HttpFilter, applied)
	if filter.GetTypedConfig() != nil {
		// convert to any type
		filter.ConfigType = &xdslistener.Filter_TypedConfig{TypedConfig: util.MessageToAny(hcm)}
	}
}

// nolint
func applyHttpFilterPatch(lp *model.EnvoyFilterConfigPatchWrapper, hcm *http_conn.HttpConnectionManager) bool {
	applied := false
	switch lp.Operation {
	case networking.EnvoyFilter_Patch_ADD:
		hcm.HttpFilters = append(hcm.HttpFilters, proto.Clone(lp.Value).(*http_conn.HttpFilter))
		applied = true
	case networking.EnvoyFilter_Patch_INSERT_FIRST:
		hcm.HttpFilters = append([]*http_conn.HttpFilter{proto.Clone(lp.Value).(*http_conn.HttpFilter)}, hcm.HttpFilters...)
		applied = true
	case networking.EnvoyFilter_Patch_INSERT_BEFORE:
		// insert before without a filter match is same as insert in the beginning
		if !hasHTTPFilterMatch(lp) {
			hcm.HttpFilters = append([]*http_conn.HttpFilter{proto.Clone(lp.Value).(*http_conn.HttpFilter)}, hcm.HttpFilters...)
			return applied
		}

		// find the matching filter first
		insertPosition := -1
		for i := 0; i < len(hcm.HttpFilters); i++ {
			if httpFilterMatch(hcm.HttpFilters[i], lp) {
				insertPosition = i
				break
			}
		}

		if insertPosition != -1 {
			clonedVal := proto.Clone(lp.Value).(*http_conn.HttpFilter)
			hcm.HttpFilters = append(hcm.HttpFilters, clonedVal)
			copy(hcm.HttpFilters[insertPosition+1:], hcm.HttpFilters[insertPosition:])
			hcm.HttpFilters[insertPosition] = clonedVal
			applied = true
		}
	case networking.EnvoyFilter_Patch_INSERT_AFTER:
		// Insert after without a filter match is same as ADD in the end
		if !hasHTTPFilterMatch(lp) {
			hcm.HttpFilters = append(hcm.HttpFilters, proto.Clone(lp.Value).(*http_conn.HttpFilter))
			return applied
		}

		// find the matching filter first
		insertPosition := -1
		for i := 0; i < len(hcm.HttpFilters); i++ {
			if httpFilterMatch(hcm.HttpFilters[i], lp) {
				insertPosition = i + 1
				break
			}
		}

		if insertPosition != -1 {
			clonedVal := proto.Clone(lp.Value).(*http_conn.HttpFilter)
			hcm.HttpFilters = append(hcm.HttpFilters, clonedVal)
			if insertPosition < len(hcm.HttpFilters)-1 {
				copy(hcm.HttpFilters[insertPosition+1:], hcm.HttpFilters[insertPosition:])
				hcm.HttpFilters[insertPosition] = clonedVal
			}
			applied = true
		}

	case networking.EnvoyFilter_Patch_REPLACE:
		if !hasHTTPFilterMatch(lp) {
			return applied
		}

		// find the matching filter first
		replacePosition := -1
		for i := 0; i < len(hcm.HttpFilters); i++ {
			if httpFilterMatch(hcm.HttpFilters[i], lp) {
				replacePosition = i
				break
			}
		}
		if replacePosition == -1 {
			log.Debugf("EnvoyFilter patch %v is not applied because no matching HTTP filter found.", lp)
			return applied
		}
		clonedVal := proto.Clone(lp.Value).(*http_conn.HttpFilter)
		hcm.HttpFilters[replacePosition] = clonedVal
		applied = true
	}
	return applied
}

// toString returns the string representation of the patch dependencies.
func toString(pd []*patchDependencies) string {
	var format string
	for _, patch := range pd {
		for _, dep := range patch.deps {
			filter := patch.patch.Value.(*http_conn.HttpFilter)
			format += fmt.Sprintf("%s -> %s\n", filter.Name, dep)
		}
	}
	return format
}

// orderPatches orders paches as per dependecies and also detects if they have cycles.
// Inspired by https://github.com/dnaeon/go-dependency-graph-algorithm/blob/08beaada45a80cb1cc40aaf51adfe4fd69d684cf/dependency-graph.go#L56.
func orderPatches(original []*model.EnvoyFilterConfigPatchWrapper, hcm *http_conn.HttpConnectionManager) ([]*patchDependencies, error) {
	// Maps from patch to all other patches that depend on it and it maintains the order.
	orderedPatches := make(map[*model.EnvoyFilterConfigPatchWrapper]*patchDependencies)

	// A map containing the patch and their unique dependencies.
	dependencySet := make(map[*model.EnvoyFilterConfigPatchWrapper]patchSet)

	// First collect all dependencies for every patch.
	for _, lp := range original {
		lmatch := lp.Match.GetListener().FilterChain.Filter.SubFilter
		// If operation is not insert before or insert after, we will not have any dependencies.
		if !(lp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE || lp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER) {
			orderedPatches[lp] = &patchDependencies{lp, []*model.EnvoyFilterConfigPatchWrapper{}}
			dependencySet[lp] = make(map[*model.EnvoyFilterConfigPatchWrapper]struct{})
			continue
		}
		// If there is no http filter match - we should add it to top or bottom depending on operation.
		if !hasHTTPFilterMatch(lp) {
			orderedPatches[lp] = &patchDependencies{lp, []*model.EnvoyFilterConfigPatchWrapper{}}
			dependencySet[lp] = make(map[*model.EnvoyFilterConfigPatchWrapper]struct{})
			continue
		} else {
			// If there is a http filter match, check if the filter already exists in hcm filters.
			// If it is there, we should not compute dependencies.
			exists := false
			for _, filter := range hcm.HttpFilters {
				if lmatch != nil && lmatch.Name == filter.Name {
					exists = true
					break
				}
			}
			if exists {
				orderedPatches[lp] = &patchDependencies{lp, []*model.EnvoyFilterConfigPatchWrapper{}}
				dependencySet[lp] = make(patchSet)
				continue
			}
		}
		// Now collect all dependencies of this patch.
		deps := make([]*model.EnvoyFilterConfigPatchWrapper, 0)
		dset := make(patchSet)
		for _, rp := range original {
			if lp == rp {
				continue
			}
			rfilter := rp.Value.(*http_conn.HttpFilter)
			// A patch is dependendent if it has sub filter match that matches with filter name.
			if lmatch != nil && nameMatches(lmatch.Name, rfilter.Name) {
				deps = append(deps, rp)
				dset[rp] = struct{}{}
			}
		}
		dependencySet[lp] = dset
		orderedPatches[lp] = &patchDependencies{lp, deps}
	}

	var opatches []*patchDependencies

	// Iteratively find and remove patches which have no dependencies. If at some point
	// there are still patches in the dependencies and we cannot find patches without
	// dependencies, that means we have a circular dependency.
	for len(dependencySet) != 0 {
		// Get all patches from the dependencies which have no dependencies.
		ready := make([]*model.EnvoyFilterConfigPatchWrapper, 0)
		readySet := make(patchSet)
		for _, patch := range original {
			if len(dependencySet[patch]) == 0 {
				ready = append(ready, patch)
				readySet[patch] = struct{}{}
			}
		}

		// If there aren't any ready patches, then we have a circular dependency.
		if len(ready) == 0 {
			var g []*patchDependencies
			for lp := range dependencySet {
				g = append(g, orderedPatches[lp])
			}
			return g, errors.New("circular dependency found")
		}

		for _, patch := range ready {
			delete(dependencySet, patch)
			opatches = append(opatches, orderedPatches[patch])
		}

		for patch, deps := range dependencySet {
			diff := deps.difference(readySet)
			dependencySet[patch] = diff
		}
	}
	return opatches, nil
}

func (ps patchSet) difference(other patchSet) patchSet {
	difference := make(patchSet)
	for elem := range ps {
		if _, exists := other[elem]; !exists {
			difference[elem] = struct{}{}
		}
	}
	return difference
}

func patchHTTPFilter(patchContext networking.EnvoyFilter_PatchContext,
	filterKey string,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain, filter *xdslistener.Filter,
	httpFilter *http_conn.HttpFilter, httpFilterRemoved *bool) {
	applied := false
	for _, lp := range patches[networking.EnvoyFilter_HTTP_FILTER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(listener, lp) ||
			!filterChainMatch(listener, fc, lp) ||
			!networkFilterMatch(filter, lp) ||
			!httpFilterMatch(httpFilter, lp) {
			continue
		}
		if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			httpFilter.Name = ""
			*httpFilterRemoved = true
			// nothing more to do in other patches as we removed this filter
			return
		} else if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			// proto merge doesn't work well when merging two filters with ANY typed configs
			// especially when the incoming lp.Value is a struct that could contain the json config
			// of an ANY typed filter. So convert our filter's typed config to Struct (retaining the any
			// typed output of json)
			if httpFilter.GetTypedConfig() == nil {
				// TODO(rshriram): fixme
				// skip this op as we would possibly have to do a merge of Any with struct
				// which doesn't seem to work well.
				continue
			}
			userHTTPFilter := lp.Value.(*http_conn.HttpFilter)
			var err error
			// we need to be able to overwrite filter names or simply empty out a filter's configs
			// as they could be supplied through per route filter configs
			httpFilterName := httpFilter.Name
			if userHTTPFilter.Name != "" {
				httpFilterName = userHTTPFilter.Name
			}
			var retVal *any.Any
			if userHTTPFilter.GetTypedConfig() != nil {
				// user has any typed struct
				// The type may not match up exactly. For example, if we use v2 internally but they use v3.
				// Assuming they are not using deprecated/new fields, we can safely swap out the TypeUrl
				// If we did not do this, proto.Merge below will panic (which is recovered), so even though this
				// is not 100% reliable its better than doing nothing
				if userHTTPFilter.GetTypedConfig().TypeUrl != httpFilter.GetTypedConfig().TypeUrl {
					userHTTPFilter.ConfigType.(*http_conn.HttpFilter_TypedConfig).TypedConfig.TypeUrl = httpFilter.GetTypedConfig().TypeUrl
				}
				if retVal, err = util.MergeAnyWithAny(httpFilter.GetTypedConfig(), userHTTPFilter.GetTypedConfig()); err != nil {
					retVal = httpFilter.GetTypedConfig()
				}
			}
			applied = true
			httpFilter.Name = toCanonicalName(httpFilterName)
			if retVal != nil {
				httpFilter.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: retVal}
			}
		}
	}
	IncrementEnvoyFilterMetric(filterKey, HttpFilter, applied)
}

func listenerMatch(listener *xdslistener.Listener, lp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := lp.Match.GetListener()
	if cMatch == nil {
		return true
	}

	if cMatch.Name != "" && cMatch.Name != listener.Name {
		return false
	}

	// skip listener port check for special virtual inbound and outbound listeners
	// to support portNumber listener filter field within those special listeners as well
	if lp.ApplyTo != networking.EnvoyFilter_LISTENER &&
		(listener.Name == VirtualInboundListenerName || listener.Name == VirtualOutboundListenerName) {
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

	return true
}

// We assume that the parent listener has already been matched
func filterChainMatch(listener *xdslistener.Listener, fc *xdslistener.FilterChain, lp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := lp.Match.GetListener()
	if cMatch == nil {
		return true
	}

	match := cMatch.FilterChain
	if match == nil {
		return true
	}
	if match.Name != "" {
		if match.Name != fc.Name {
			return false
		}
	}
	if match.Sni != "" {
		if fc.FilterChainMatch == nil || len(fc.FilterChainMatch.ServerNames) == 0 {
			return false
		}
		sniMatched := false
		for _, sni := range fc.FilterChainMatch.ServerNames {
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
		if fc.FilterChainMatch == nil || fc.FilterChainMatch.TransportProtocol != match.TransportProtocol {
			return false
		}
	}

	// check match for destination port within the FilterChainMatch
	if match.DestinationPort > 0 {
		if fc.FilterChainMatch == nil || fc.FilterChainMatch.DestinationPort == nil {
			return false
		} else if fc.FilterChainMatch.DestinationPort.Value != match.DestinationPort {
			return false
		}
	}
	isVirtual := listener.Name == VirtualInboundListenerName || listener.Name == VirtualOutboundListenerName
	// We only do this for virtual listeners, which will move the listener port into a FCM. For non-virtual listeners,
	// we will handle this in the proper listener match.
	if isVirtual && cMatch.GetPortNumber() > 0 && fc.GetFilterChainMatch().GetDestinationPort().GetValue() != cMatch.GetPortNumber() {
		return false
	}

	return true
}

func hasNetworkFilterMatch(lp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := lp.Match.GetListener()
	if cMatch == nil {
		return false
	}

	fcMatch := cMatch.FilterChain
	if fcMatch == nil {
		return false
	}

	return fcMatch.Filter != nil
}

// We assume that the parent listener and filter chain have already been matched
func networkFilterMatch(filter *xdslistener.Filter, lp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasNetworkFilterMatch(lp) {
		return true
	}

	return nameMatches(lp.Match.GetListener().FilterChain.Filter.Name, filter.Name)
}

func hasHTTPFilterMatch(lp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasNetworkFilterMatch(lp) {
		return false
	}

	return lp.Match.GetListener().FilterChain.Filter.SubFilter != nil
}

// We assume that the parent listener and filter chain, and network filter have already been matched
func httpFilterMatch(filter *http_conn.HttpFilter, lp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasHTTPFilterMatch(lp) {
		return true
	}
	match := lp.Match.GetListener().FilterChain.Filter.SubFilter

	return nameMatches(match.Name, filter.Name)
}

func patchContextMatch(patchContext networking.EnvoyFilter_PatchContext,
	lp *model.EnvoyFilterConfigPatchWrapper) bool {
	return lp.Match.Context == patchContext || lp.Match.Context == networking.EnvoyFilter_ANY
}

func commonConditionMatch(patchContext networking.EnvoyFilter_PatchContext,
	lp *model.EnvoyFilterConfigPatchWrapper) bool {
	return patchContextMatch(patchContext, lp)
}

// toCanonicalName converts a deprecated filter name to the replacement, if present. Otherwise, the
// same name is returned.
func toCanonicalName(name string) string {
	if nn, f := xds.ReverseDeprecatedFilterNames[name]; f {
		return nn
	}
	return name
}

// nameMatches compares two filter names, matching even if a deprecated filter name is used.
func nameMatches(matchName, filterName string) bool {
	return matchName == filterName || matchName == xds.DeprecatedFilterNames[filterName]
}
