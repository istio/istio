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
	"fmt"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto/merge"
)

// ApplyListenerPatches applies patches to LDS output
func ApplyListenerPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	efw *model.EnvoyFilterWrapper,
	lis []*listener.Listener,
	skipAdds bool,
) (out []*listener.Listener) {
	defer runtime.HandleCrash(runtime.LogPanic, func(any) {
		IncrementEnvoyFilterErrorMetric(Listener)
		log.Errorf("listeners patch %s/%s caused panic, so the patches did not take effect", efw.Namespace, efw.Name)
	})
	// In case the patches cause panic, use the listeners generated before to reduce the influence.
	out = lis

	if efw == nil {
		return
	}

	return patchListeners(patchContext, efw, lis, skipAdds)
}

func patchListeners(
	patchContext networking.EnvoyFilter_PatchContext,
	efw *model.EnvoyFilterWrapper,
	listeners []*listener.Listener,
	skipAdds bool,
) []*listener.Listener {
	listenersRemoved := false

	// do all the changes for a single envoy filter crd object. [including adds]
	// then move on to the next one

	// only removes/merges plus next level object operations [add/remove/merge]
	for _, lis := range listeners {
		if lis.Name == "" {
			// removed by another op
			continue
		}
		patchListener(patchContext, efw.Patches, lis, &listenersRemoved)
	}
	// adds at listener level if enabled
	if !skipAdds {
		for _, lp := range efw.Patches[networking.EnvoyFilter_LISTENER] {
			if lp.Operation == networking.EnvoyFilter_Patch_ADD {
				// If listener ADD patch does not specify a patch context, only add for sidecar outbound and gateway.
				if lp.Match.Context == networking.EnvoyFilter_ANY && patchContext != networking.EnvoyFilter_SIDECAR_OUTBOUND &&
					patchContext != networking.EnvoyFilter_GATEWAY {
					continue
				}
				if !commonConditionMatch(patchContext, lp) {
					IncrementEnvoyFilterMetric(lp.Key(), Listener, false)
					continue
				}
				// clone before append. Otherwise, subsequent operations on this listener will corrupt
				// the master value stored in CP.
				listeners = append(listeners, proto.Clone(lp.Value).(*listener.Listener))
				IncrementEnvoyFilterMetric(lp.Key(), Listener, true)
			}
		}
	}
	if listenersRemoved {
		tempArray := make([]*listener.Listener, 0, len(listeners))
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
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener, listenersRemoved *bool,
) {
	for _, lp := range patches[networking.EnvoyFilter_LISTENER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(lis, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), Listener, false)
			continue
		}
		IncrementEnvoyFilterMetric(lp.Key(), Listener, true)
		if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			lis.Name = ""
			*listenersRemoved = true
			// terminate the function here as we have nothing more do to for this listener
			return
		} else if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			merge.Merge(lis, lp.Value)
		}
	}
	patchListenerFilters(patchContext, patches[networking.EnvoyFilter_LISTENER_FILTER], lis)
	patchFilterChains(patchContext, patches, lis)
}

// patchListenerFilters patches passed in listener filters with listener filter patches.
func patchListenerFilters(patchContext networking.EnvoyFilter_PatchContext,
	patches []*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener,
) {
	for _, lp := range patches {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(lis, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), ListenerFilter, false)
			continue
		}
		applied := false
		if lp.Operation == networking.EnvoyFilter_Patch_ADD {
			lis.ListenerFilters = append(lis.ListenerFilters, proto.Clone(lp.Value).(*listener.ListenerFilter))
			applied = true
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			lis.ListenerFilters = append([]*listener.ListenerFilter{proto.Clone(lp.Value).(*listener.ListenerFilter)}, lis.ListenerFilters...)
			applied = true
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a filter match is same as ADD in the end
			if !hasListenerFilterMatch(lp) {
				lis.ListenerFilters = append(lis.ListenerFilters, proto.Clone(lp.Value).(*listener.ListenerFilter))
				applied = true
				continue
			}

			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(lis.ListenerFilters); i++ {
				if listenerFilterMatch(lis.ListenerFilters[i], lp) {
					insertPosition = i + 1
					break
				}
			}

			if insertPosition == -1 {
				continue
			}
			applied = true
			clonedVal := proto.Clone(lp.Value).(*listener.ListenerFilter)
			lis.ListenerFilters = append(lis.ListenerFilters, clonedVal)
			if insertPosition < len(lis.ListenerFilters)-1 {
				copy(lis.ListenerFilters[insertPosition+1:], lis.ListenerFilters[insertPosition:])
				lis.ListenerFilters[insertPosition] = clonedVal
			}
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a filter match is same as insert in the beginning
			if !hasListenerFilterMatch(lp) {
				lis.ListenerFilters = append([]*listener.ListenerFilter{proto.Clone(lp.Value).(*listener.ListenerFilter)}, lis.ListenerFilters...)
				continue
			}
			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(lis.ListenerFilters); i++ {
				if listenerFilterMatch(lis.ListenerFilters[i], lp) {
					insertPosition = i
					break
				}
			}

			// If matching filter is not found, then don't insert and continue.
			if insertPosition == -1 {
				continue
			}
			applied = true
			clonedVal := proto.Clone(lp.Value).(*listener.ListenerFilter)
			lis.ListenerFilters = append(lis.ListenerFilters, clonedVal)
			copy(lis.ListenerFilters[insertPosition+1:], lis.ListenerFilters[insertPosition:])
			lis.ListenerFilters[insertPosition] = clonedVal
		} else if lp.Operation == networking.EnvoyFilter_Patch_REPLACE {
			if !hasListenerFilterMatch(lp) {
				continue
			}
			// find the matching filter first
			replacePosition := -1
			for i := 0; i < len(lis.ListenerFilters); i++ {
				if listenerFilterMatch(lis.ListenerFilters[i], lp) {
					replacePosition = i
					break
				}
			}
			if replacePosition == -1 {
				continue
			}
			applied = true
			lis.ListenerFilters[replacePosition] = proto.Clone(lp.Value).(*listener.ListenerFilter)
		} else if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			if !hasListenerFilterMatch(lp) {
				continue
			}
			tempListenerFilters := []*listener.ListenerFilter{}
			for _, filter := range lis.ListenerFilters {
				if !listenerFilterMatch(filter, lp) {
					tempListenerFilters = append(tempListenerFilters, filter)
				}
			}
			lis.ListenerFilters = tempListenerFilters
		}
		IncrementEnvoyFilterMetric(lp.Key(), ListenerFilter, applied)
	}
}

func patchFilterChains(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener,
) {
	filterChainsRemoved := false
	for i, fc := range lis.FilterChains {
		if fc.Filters == nil {
			continue
		}
		patchFilterChain(patchContext, patches, lis, lis.FilterChains[i], &filterChainsRemoved)
	}
	if fc := lis.GetDefaultFilterChain(); fc.GetFilters() != nil {
		removed := false
		patchFilterChain(patchContext, patches, lis, fc, &removed)
		if removed {
			lis.DefaultFilterChain = nil
		}
	}
	for _, lp := range patches[networking.EnvoyFilter_FILTER_CHAIN] {
		if lp.Operation == networking.EnvoyFilter_Patch_ADD {
			if !commonConditionMatch(patchContext, lp) ||
				!listenerMatch(lis, lp) {
				IncrementEnvoyFilterMetric(lp.Key(), FilterChain, false)
				continue
			}
			IncrementEnvoyFilterMetric(lp.Key(), FilterChain, true)
			lis.FilterChains = append(lis.FilterChains, proto.Clone(lp.Value).(*listener.FilterChain))
		}
	}
	if filterChainsRemoved {
		tempArray := make([]*listener.FilterChain, 0, len(lis.FilterChains))
		for _, fc := range lis.FilterChains {
			if fc.Filters != nil {
				tempArray = append(tempArray, fc)
			}
		}
		lis.FilterChains = tempArray
	}
}

func patchFilterChain(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener,
	fc *listener.FilterChain, filterChainRemoved *bool,
) {
	for _, lp := range patches[networking.EnvoyFilter_FILTER_CHAIN] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(lis, lp) ||
			!filterChainMatch(lis, fc, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), FilterChain, false)
			continue
		}
		IncrementEnvoyFilterMetric(lp.Key(), FilterChain, true)
		if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			fc.Filters = nil
			*filterChainRemoved = true
			// nothing more to do in other patches as we removed this filter chain
			return
		} else if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			merged, err := mergeTransportSocketListener(fc, lp)
			if err != nil {
				log.Debugf("merge of transport socket failed for listener: %v", err)
				continue
			}
			if !merged {
				merge.Merge(fc, lp.Value)
			}
		}
	}
	patchNetworkFilters(patchContext, patches, lis, fc)
}

// Test if the patch contains a config for TransportSocket
// Returns a boolean indicating if the merge was handled by this function; if false, it should still be called
// outside of this function.
func mergeTransportSocketListener(fc *listener.FilterChain, lp *model.EnvoyFilterConfigPatchWrapper) (merged bool, err error) {
	lpValueCast, ok := (lp.Value).(*listener.FilterChain)
	if !ok {
		return false, fmt.Errorf("cast of cp.Value failed: %v", ok)
	}

	// Test if the patch contains a config for TransportSocket
	applyPatch := false
	if lpValueCast.GetTransportSocket() != nil {
		if fc.GetTransportSocket() == nil {
			// There is no existing filter chain, we will add it outside this function; report back that we did not merge.
			return false, nil
		}
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
			merge.Merge(dstListener, retVal)
		}
	}
	// If we already applied the patch, we skip merge.Merge() in the outer function
	return applyPatch, nil
}

func patchNetworkFilters(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener, fc *listener.FilterChain,
) {
	for _, lp := range patches[networking.EnvoyFilter_NETWORK_FILTER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(lis, lp) ||
			!filterChainMatch(lis, fc, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), NetworkFilter, false)
			continue
		}
		applied := false
		if lp.Operation == networking.EnvoyFilter_Patch_ADD {
			fc.Filters = append(fc.Filters, proto.Clone(lp.Value).(*listener.Filter))
			applied = true
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			fc.Filters = append([]*listener.Filter{proto.Clone(lp.Value).(*listener.Filter)}, fc.Filters...)
			applied = true
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a filter match is same as ADD in the end
			if !hasNetworkFilterMatch(lp) {
				fc.Filters = append(fc.Filters, proto.Clone(lp.Value).(*listener.Filter))
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
			clonedVal := proto.Clone(lp.Value).(*listener.Filter)
			fc.Filters = append(fc.Filters, clonedVal)
			if insertPosition < len(fc.Filters)-1 {
				copy(fc.Filters[insertPosition+1:], fc.Filters[insertPosition:])
				fc.Filters[insertPosition] = clonedVal
			}
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a filter match is same as insert in the beginning
			if !hasNetworkFilterMatch(lp) {
				fc.Filters = append([]*listener.Filter{proto.Clone(lp.Value).(*listener.Filter)}, fc.Filters...)
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
			clonedVal := proto.Clone(lp.Value).(*listener.Filter)
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
			fc.Filters[replacePosition] = proto.Clone(lp.Value).(*listener.Filter)
		} else if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			if !hasNetworkFilterMatch(lp) {
				continue
			}

			var tempFilters []*listener.Filter
			for _, filter := range fc.Filters {
				if !networkFilterMatch(filter, lp) {
					tempFilters = append(tempFilters, filter)
				}
			}
			fc.Filters = tempFilters
		}
		IncrementEnvoyFilterMetric(lp.Key(), NetworkFilter, applied)
	}

	for i := range fc.Filters {
		patchNetworkFilter(patchContext, patches, lis, fc, fc.Filters[i])
	}
}

// patchNetworkFilter patches passed in filter if it is MERGE operation.
// The return value indicates whether the filter has been removed for REMOVE operations.
func patchNetworkFilter(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener, fc *listener.FilterChain,
	filter *listener.Filter,
) {
	for _, lp := range patches[networking.EnvoyFilter_NETWORK_FILTER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(lis, lp) ||
			!filterChainMatch(lis, fc, lp) ||
			!networkFilterMatch(filter, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), NetworkFilter, false)
			continue
		}
		if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			// proto merge doesn't work well when merging two filters with ANY typed configs
			// especially when the incoming cp.Value is a struct that could contain the json config
			// of an ANY typed filter. So convert our filter's typed config to Struct (retaining the any
			// typed output of json)
			if filter.GetTypedConfig() == nil {
				// TODO(rshriram): fixme
				// skip this op as we would possibly have to do a merge of Any with struct
				// which doesn't seem to work well.
				continue
			}
			userFilter := lp.Value.(*listener.Filter)
			var err error
			// we need to be able to overwrite filter names or simply empty out a filter's configs
			// as they could be supplied through per route filter configs
			filterName := filter.Name
			if userFilter.Name != "" {
				filterName = userFilter.Name
			}
			var retVal *anypb.Any
			if userFilter.GetTypedConfig() != nil {
				IncrementEnvoyFilterMetric(lp.Key(), NetworkFilter, true)
				// user has any typed struct
				// The type may not match up exactly. For example, if we use v2 internally but they use v3.
				// Assuming they are not using deprecated/new fields, we can safely swap out the TypeUrl
				// If we did not do this, merge.Merge below will panic (which is recovered), so even though this
				// is not 100% reliable its better than doing nothing
				if userFilter.GetTypedConfig().TypeUrl != filter.GetTypedConfig().TypeUrl {
					userFilter.ConfigType.(*listener.Filter_TypedConfig).TypedConfig.TypeUrl = filter.GetTypedConfig().TypeUrl
				}
				if retVal, err = util.MergeAnyWithAny(filter.GetTypedConfig(), userFilter.GetTypedConfig()); err != nil {
					retVal = filter.GetTypedConfig()
				}
			}
			filter.Name = filterName
			if retVal != nil {
				filter.ConfigType = &listener.Filter_TypedConfig{TypedConfig: retVal}
			}
		}
	}
	if filter.Name == wellknown.HTTPConnectionManager {
		patchHTTPFilters(patchContext, patches, lis, fc, filter)
	}
}

func patchHTTPFilters(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener, fc *listener.FilterChain, filter *listener.Filter,
) {
	httpconn := &hcm.HttpConnectionManager{}
	if filter.GetTypedConfig() != nil {
		if err := filter.GetTypedConfig().UnmarshalTo(httpconn); err != nil {
			return
			// todo: figure out a non noisy logging option here
			//  as this loop will be called very frequently
		}
	}
	for _, lp := range patches[networking.EnvoyFilter_HTTP_FILTER] {
		applied := false
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(lis, lp) ||
			!filterChainMatch(lis, fc, lp) ||
			!networkFilterMatch(filter, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), HttpFilter, false)
			continue
		}
		if lp.Operation == networking.EnvoyFilter_Patch_ADD {
			applied = true
			httpconn.HttpFilters = append(httpconn.HttpFilters, proto.Clone(lp.Value).(*hcm.HttpFilter))
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			httpconn.HttpFilters = append([]*hcm.HttpFilter{proto.Clone(lp.Value).(*hcm.HttpFilter)}, httpconn.HttpFilters...)
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a filter match is same as ADD in the end
			if !hasHTTPFilterMatch(lp) {
				httpconn.HttpFilters = append(httpconn.HttpFilters, proto.Clone(lp.Value).(*hcm.HttpFilter))
				continue
			}

			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(httpconn.HttpFilters); i++ {
				if httpFilterMatch(httpconn.HttpFilters[i], lp) {
					insertPosition = i + 1
					break
				}
			}

			if insertPosition == -1 {
				continue
			}
			applied = true
			clonedVal := proto.Clone(lp.Value).(*hcm.HttpFilter)
			httpconn.HttpFilters = append(httpconn.HttpFilters, clonedVal)
			if insertPosition < len(httpconn.HttpFilters)-1 {
				copy(httpconn.HttpFilters[insertPosition+1:], httpconn.HttpFilters[insertPosition:])
				httpconn.HttpFilters[insertPosition] = clonedVal
			}
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a filter match is same as insert in the beginning
			if !hasHTTPFilterMatch(lp) {
				httpconn.HttpFilters = append([]*hcm.HttpFilter{proto.Clone(lp.Value).(*hcm.HttpFilter)}, httpconn.HttpFilters...)
				continue
			}

			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(httpconn.HttpFilters); i++ {
				if httpFilterMatch(httpconn.HttpFilters[i], lp) {
					insertPosition = i
					break
				}
			}

			if insertPosition == -1 {
				continue
			}
			applied = true
			clonedVal := proto.Clone(lp.Value).(*hcm.HttpFilter)
			httpconn.HttpFilters = append(httpconn.HttpFilters, clonedVal)
			copy(httpconn.HttpFilters[insertPosition+1:], httpconn.HttpFilters[insertPosition:])
			httpconn.HttpFilters[insertPosition] = clonedVal
		} else if lp.Operation == networking.EnvoyFilter_Patch_REPLACE {
			if !hasHTTPFilterMatch(lp) {
				continue
			}

			// find the matching filter first
			replacePosition := -1
			for i := 0; i < len(httpconn.HttpFilters); i++ {
				if httpFilterMatch(httpconn.HttpFilters[i], lp) {
					replacePosition = i
					break
				}
			}
			if replacePosition == -1 {
				log.Debugf("EnvoyFilter patch %v is not applied because no matching HTTP filter found.", lp)
				continue
			}
			applied = true
			clonedVal := proto.Clone(lp.Value).(*hcm.HttpFilter)
			httpconn.HttpFilters[replacePosition] = clonedVal
		} else if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			if !hasHTTPFilterMatch(lp) {
				continue
			}
			var httpFilters []*hcm.HttpFilter
			for _, h := range httpconn.HttpFilters {
				if !httpFilterMatch(h, lp) {
					httpFilters = append(httpFilters, h)
				}
			}
			httpconn.HttpFilters = httpFilters
		}
		IncrementEnvoyFilterMetric(lp.Key(), HttpFilter, applied)
	}
	for _, httpFilter := range httpconn.HttpFilters {
		patchHTTPFilter(patchContext, patches, lis, fc, filter, httpFilter)
	}
	if filter.GetTypedConfig() != nil {
		// convert to any type
		filter.ConfigType = &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(httpconn)}
	}
}

// patchHTTPFilter patches passed in filter if it is MERGE operation.
// The return value indicates whether the filter has been removed for REMOVE operations.
func patchHTTPFilter(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *listener.Listener, fc *listener.FilterChain, filter *listener.Filter,
	httpFilter *hcm.HttpFilter,
) {
	for _, lp := range patches[networking.EnvoyFilter_HTTP_FILTER] {
		applied := false
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(listener, lp) ||
			!filterChainMatch(listener, fc, lp) ||
			!networkFilterMatch(filter, lp) ||
			!httpFilterMatch(httpFilter, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), HttpFilter, applied)
			continue
		}
		if lp.Operation == networking.EnvoyFilter_Patch_MERGE {
			// proto merge doesn't work well when merging two filters with ANY typed configs
			// especially when the incoming cp.Value is a struct that could contain the json config
			// of an ANY typed filter. So convert our filter's typed config to Struct (retaining the any
			// typed output of json)
			if httpFilter.GetTypedConfig() == nil {
				// TODO(rshriram): fixme
				// skip this op as we would possibly have to do a merge of Any with struct
				// which doesn't seem to work well.
				continue
			}
			userHTTPFilter := lp.Value.(*hcm.HttpFilter)
			var err error
			// we need to be able to overwrite filter names or simply empty out a filter's configs
			// as they could be supplied through per route filter configs
			httpFilterName := httpFilter.Name
			if userHTTPFilter.Name != "" {
				httpFilterName = userHTTPFilter.Name
			}
			var retVal *anypb.Any
			if userHTTPFilter.GetTypedConfig() != nil {
				// user has any typed struct
				// The type may not match up exactly. For example, if we use v2 internally but they use v3.
				// Assuming they are not using deprecated/new fields, we can safely swap out the TypeUrl
				// If we did not do this, merge.Merge below will panic (which is recovered), so even though this
				// is not 100% reliable its better than doing nothing
				if userHTTPFilter.GetTypedConfig().TypeUrl != httpFilter.GetTypedConfig().TypeUrl {
					userHTTPFilter.ConfigType.(*hcm.HttpFilter_TypedConfig).TypedConfig.TypeUrl = httpFilter.GetTypedConfig().TypeUrl
				}
				if retVal, err = util.MergeAnyWithAny(httpFilter.GetTypedConfig(), userHTTPFilter.GetTypedConfig()); err != nil {
					retVal = httpFilter.GetTypedConfig()
				}
			}
			applied = true
			httpFilter.Name = httpFilterName
			if retVal != nil {
				httpFilter.ConfigType = &hcm.HttpFilter_TypedConfig{TypedConfig: retVal}
			}
		}
		IncrementEnvoyFilterMetric(lp.Key(), HttpFilter, applied)
	}
}

func listenerMatch(listener *listener.Listener, lp *model.EnvoyFilterConfigPatchWrapper) bool {
	lMatch := lp.Match.GetListener()
	if lMatch == nil {
		return true
	}

	if lMatch.Name != "" && lMatch.Name != listener.Name {
		return false
	}

	// skip listener port check for special virtual inbound and outbound listeners
	// to support portNumber listener filter field within those special listeners as well
	if lp.ApplyTo != networking.EnvoyFilter_LISTENER &&
		(listener.Name == model.VirtualInboundListenerName || listener.Name == model.VirtualOutboundListenerName) {
		return true
	}

	// FIXME: Ports on a listener can be 0. the API only takes uint32 for ports
	// We should either make that field in API as a wrapper type or switch to int
	if lMatch.PortNumber != 0 {
		sockAddr := listener.Address.GetSocketAddress()
		if sockAddr == nil || sockAddr.GetPortValue() != lMatch.PortNumber {
			return false
		}
	}
	return true
}

// We assume that the parent listener has already been matched
func filterChainMatch(listener *listener.Listener, fc *listener.FilterChain, lp *model.EnvoyFilterConfigPatchWrapper) bool {
	lMatch := lp.Match.GetListener()
	if lMatch == nil {
		return true
	}

	isVirtual := listener.Name == model.VirtualInboundListenerName || listener.Name == model.VirtualOutboundListenerName
	// We only do this for virtual listeners, which will move the listener port into a FCM. For non-virtual listeners,
	// we will handle this in the proper listener match.
	if isVirtual && lMatch.GetPortNumber() > 0 && fc.GetFilterChainMatch().GetDestinationPort().GetValue() != lMatch.GetPortNumber() {
		return false
	}

	match := lMatch.FilterChain
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
	return true
}

func hasListenerFilterMatch(lp *model.EnvoyFilterConfigPatchWrapper) bool {
	lMatch := lp.Match.GetListener()
	if lMatch == nil {
		return false
	}

	return lMatch.ListenerFilter != ""
}

// We assume that the parent listener has already been matched
func listenerFilterMatch(filter *listener.ListenerFilter, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasListenerFilterMatch(cp) {
		return true
	}

	return cp.Match.GetListener().ListenerFilter == filter.Name
}

func hasNetworkFilterMatch(lp *model.EnvoyFilterConfigPatchWrapper) bool {
	lMatch := lp.Match.GetListener()
	if lMatch == nil {
		return false
	}

	fcMatch := lMatch.FilterChain
	if fcMatch == nil {
		return false
	}

	return fcMatch.Filter != nil
}

// We assume that the parent listener and filter chain have already been matched
func networkFilterMatch(filter *listener.Filter, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasNetworkFilterMatch(cp) {
		return true
	}

	return nameMatches(cp.Match.GetListener().FilterChain.Filter.Name, filter.Name)
}

func hasHTTPFilterMatch(lp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasNetworkFilterMatch(lp) {
		return false
	}

	match := lp.Match.GetListener().FilterChain.Filter.SubFilter
	return match != nil
}

// We assume that the parent listener and filter chain, and network filter have already been matched
func httpFilterMatch(filter *hcm.HttpFilter, lp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasHTTPFilterMatch(lp) {
		return true
	}

	match := lp.Match.GetListener().FilterChain.Filter.SubFilter

	return match.Name == filter.Name
}

func patchContextMatch(patchContext networking.EnvoyFilter_PatchContext,
	lp *model.EnvoyFilterConfigPatchWrapper,
) bool {
	return lp.Match.Context == patchContext || lp.Match.Context == networking.EnvoyFilter_ANY
}

func commonConditionMatch(patchContext networking.EnvoyFilter_PatchContext,
	lp *model.EnvoyFilterConfigPatchWrapper,
) bool {
	return patchContextMatch(patchContext, lp)
}

// nameMatches compares two filter names, matching even if a deprecated filter name is used.
func nameMatches(matchName, filterName string) bool {
	return matchName == filterName || matchName == xds.DeprecatedFilterNames[filterName]
}
