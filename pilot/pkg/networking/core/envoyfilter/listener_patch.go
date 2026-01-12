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
	"strings"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto/merge"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/wellknown"
)

// ApplyListenerPatches applies patches to LDS output
func ApplyListenerPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	efw *model.MergedEnvoyFilterWrapper,
	lis []*listener.Listener,
	skipAdds bool,
) (out []*listener.Listener) {
	defer runtime.HandleCrash(runtime.LogPanic, func(any) {
		IncrementEnvoyFilterErrorMetric(Listener)
		log.Errorf("listeners patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the listeners generated before to reduce the influence.
	out = lis

	if efw == nil {
		return out
	}

	return patchListeners(patchContext, efw, lis, skipAdds)
}

func patchListeners(
	patchContext networking.EnvoyFilter_PatchContext,
	efw *model.MergedEnvoyFilterWrapper,
	listeners []*listener.Listener,
	skipAdds bool,
) []*listener.Listener {
	// do all the changes for a single envoy filter crd object. [including adds]
	// then move on to the next one

	// only removes/merges plus next level object operations [add/remove/merge]
	for _, lis := range listeners {
		if lis.Name == "" {
			// removed by another op
			continue
		}
		patchListener(patchContext, efw.Patches, lis)
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

	return slices.FilterInPlace(listeners, func(l *listener.Listener) bool {
		return l.Name != ""
	})
}

func patchListener(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper, lis *listener.Listener,
) {
	for _, lp := range patches[networking.EnvoyFilter_LISTENER] {
		if !commonConditionMatch(patchContext, lp) ||
			!listenerMatch(lis, lp) {
			IncrementEnvoyFilterMetric(lp.Key(), Listener, false)
			continue
		}
		IncrementEnvoyFilterMetric(lp.Key(), Listener, true)
		if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			// empty name means this listener will be removed, we can return directly.
			lis.Name = ""
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
		switch lp.Operation {
		case networking.EnvoyFilter_Patch_ADD:
			lis.ListenerFilters = append(lis.ListenerFilters, proto.Clone(lp.Value).(*listener.ListenerFilter))
			applied = true
		case networking.EnvoyFilter_Patch_INSERT_FIRST:
			lis.ListenerFilters = append([]*listener.ListenerFilter{proto.Clone(lp.Value).(*listener.ListenerFilter)}, lis.ListenerFilters...)
			applied = true
		case networking.EnvoyFilter_Patch_INSERT_AFTER:
			// Insert after without a filter match is same as ADD in the end
			if !hasListenerFilterMatch(lp) {
				lis.ListenerFilters = append(lis.ListenerFilters, proto.Clone(lp.Value).(*listener.ListenerFilter))
				applied = true
				continue
			}
			lis.ListenerFilters, applied = insertAfterFunc(
				lis.ListenerFilters,
				func(e *listener.ListenerFilter) (bool, *listener.ListenerFilter) {
					if listenerFilterMatch(e, lp) {
						return true, proto.Clone(lp.Value).(*listener.ListenerFilter)
					}
					return false, nil
				},
			)
		case networking.EnvoyFilter_Patch_INSERT_BEFORE:
			// insert before without a filter match is same as insert in the beginning
			if !hasListenerFilterMatch(lp) {
				lis.ListenerFilters = append([]*listener.ListenerFilter{proto.Clone(lp.Value).(*listener.ListenerFilter)}, lis.ListenerFilters...)
				continue
			}
			lis.ListenerFilters, applied = insertBeforeFunc(
				lis.ListenerFilters,
				func(e *listener.ListenerFilter) (bool, *listener.ListenerFilter) {
					if listenerFilterMatch(e, lp) {
						return true, proto.Clone(lp.Value).(*listener.ListenerFilter)
					}
					return false, nil
				},
			)
		case networking.EnvoyFilter_Patch_REPLACE:
			if !hasListenerFilterMatch(lp) {
				continue
			}
			lis.ListenerFilters, applied = replaceFunc(
				lis.ListenerFilters,
				func(e *listener.ListenerFilter) (bool, *listener.ListenerFilter) {
					if listenerFilterMatch(e, lp) {
						return true, proto.Clone(lp.Value).(*listener.ListenerFilter)
					}
					return false, nil
				},
			)
		case networking.EnvoyFilter_Patch_REMOVE:
			if !hasListenerFilterMatch(lp) {
				continue
			}
			lis.ListenerFilters = slices.FilterInPlace(lis.ListenerFilters, func(filter *listener.ListenerFilter) bool {
				return !listenerFilterMatch(filter, lp)
			})
		case networking.EnvoyFilter_Patch_MERGE:
			if !hasListenerFilterMatch(lp) {
				continue
			}
			for _, lisFilter := range lis.ListenerFilters {
				merged := mergeListenerFilter(lp, lisFilter)
				if merged {
					applied = true
				}
			}
		default:
			log.Debugf("unknown listener filter operation %v for listener %s, skipping", lp.Operation, lis.Name)
		}
		IncrementEnvoyFilterMetric(lp.Key(), ListenerFilter, applied)
	}
}

func patchFilterChains(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener,
) {
	for i, fc := range lis.FilterChains {
		if fc.Filters == nil {
			continue
		}
		patchFilterChain(patchContext, patches, lis, lis.FilterChains[i])
	}

	if fc := lis.GetDefaultFilterChain(); fc.GetFilters() != nil {
		patchFilterChain(patchContext, patches, lis, fc)
		if fc.Filters == nil {
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

	lis.FilterChains = slices.FilterInPlace(lis.FilterChains, func(fc *listener.FilterChain) bool {
		return fc.Filters != nil
	})
}

func patchFilterChain(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	lis *listener.Listener,
	fc *listener.FilterChain,
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
			// nil means this filter chain will be removed, we can return directly.
			fc.Filters = nil
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
			fc.Filters, applied = insertAfterFunc(fc.Filters, func(e *listener.Filter) (bool, *listener.Filter) {
				if networkFilterMatch(e, lp) {
					return true, proto.Clone(lp.Value).(*listener.Filter)
				}
				return false, nil
			})
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a filter match is same as insert in the beginning
			if !hasNetworkFilterMatch(lp) {
				fc.Filters = append([]*listener.Filter{proto.Clone(lp.Value).(*listener.Filter)}, fc.Filters...)
				continue
			}
			fc.Filters, applied = insertBeforeFunc(fc.Filters, func(e *listener.Filter) (bool, *listener.Filter) {
				if networkFilterMatch(e, lp) {
					return true, proto.Clone(lp.Value).(*listener.Filter)
				}
				return false, nil
			})
		} else if lp.Operation == networking.EnvoyFilter_Patch_REPLACE {
			if !hasNetworkFilterMatch(lp) {
				continue
			}
			fc.Filters, applied = replaceFunc(fc.Filters, func(e *listener.Filter) (bool, *listener.Filter) {
				if networkFilterMatch(e, lp) {
					return true, proto.Clone(lp.Value).(*listener.Filter)
				}
				return false, nil
			})
		} else if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			if !hasNetworkFilterMatch(lp) {
				continue
			}
			fc.Filters = slices.FilterInPlace(fc.Filters, func(filter *listener.Filter) bool {
				return !networkFilterMatch(filter, lp)
			})
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
			httpconn.HttpFilters, applied = insertAfterFunc(
				httpconn.HttpFilters,
				func(e *hcm.HttpFilter) (bool, *hcm.HttpFilter) {
					if httpFilterMatch(e, lp) {
						return true, proto.Clone(lp.Value).(*hcm.HttpFilter)
					}
					return false, nil
				},
			)
		} else if lp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a filter match is same as insert in the beginning
			if !hasHTTPFilterMatch(lp) {
				httpconn.HttpFilters = append([]*hcm.HttpFilter{proto.Clone(lp.Value).(*hcm.HttpFilter)}, httpconn.HttpFilters...)
				continue
			}
			httpconn.HttpFilters, applied = insertBeforeFunc(
				httpconn.HttpFilters,
				func(e *hcm.HttpFilter) (bool, *hcm.HttpFilter) {
					if httpFilterMatch(e, lp) {
						return true, proto.Clone(lp.Value).(*hcm.HttpFilter)
					}
					return false, nil
				},
			)
		} else if lp.Operation == networking.EnvoyFilter_Patch_REPLACE {
			if !hasHTTPFilterMatch(lp) {
				continue
			}
			httpconn.HttpFilters, applied = replaceFunc(
				httpconn.HttpFilters,
				func(e *hcm.HttpFilter) (bool, *hcm.HttpFilter) {
					if httpFilterMatch(e, lp) {
						return true, proto.Clone(lp.Value).(*hcm.HttpFilter)
					}
					return false, nil
				},
			)
		} else if lp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			if !hasHTTPFilterMatch(lp) {
				continue
			}
			httpconn.HttpFilters = slices.FilterInPlace(httpconn.HttpFilters, func(h *hcm.HttpFilter) bool {
				return !httpFilterMatch(h, lp)
			})
		}
		IncrementEnvoyFilterMetric(lp.Key(), HttpFilter, applied)
	}
	for _, httpFilter := range httpconn.HttpFilters {
		mergeHTTPFilter(patchContext, patches, lis, fc, filter, httpFilter)
	}
	if filter.GetTypedConfig() != nil {
		// convert to any type
		filter.ConfigType = &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(httpconn)}
	}
}

// mergeHTTPFilter patches passed in filter if it is MERGE operation.
func mergeHTTPFilter(patchContext networking.EnvoyFilter_PatchContext,
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

func mergeListenerFilter(lp *model.EnvoyFilterConfigPatchWrapper, lisFilter *listener.ListenerFilter) bool {
	// proto merge doesn't work well when merging two filters with ANY typed configs
	// especially when the incoming cp.Value is a struct that could contain the json config
	// of an ANY typed filter. So convert our filter's typed config to Struct (retaining the any
	// typed output of json)
	if lisFilter.GetTypedConfig() == nil {
		// skip this op as we would possibly have to do a merge of Any with struct
		// which doesn't seem to work well.
		return false
	}
	userListenerFilter := lp.Value.(*listener.ListenerFilter)
	var (
		err    error
		retVal *anypb.Any
	)
	if userListenerFilter.GetTypedConfig() != nil {
		if retVal, err = util.MergeAnyWithAny(lisFilter.GetTypedConfig(), userListenerFilter.GetTypedConfig()); err != nil {
			retVal = lisFilter.GetTypedConfig()
		}
	}

	// we need to be able to overwrite filter names or simply empty out a filter's configs
	// as they could be supplied through per route filter configs
	lisFilterName := lisFilter.Name
	if userListenerFilter.Name != "" {
		lisFilterName = userListenerFilter.Name
	}
	userListenerFilter.Name = lisFilterName
	if retVal != nil {
		lisFilter.ConfigType = &listener.ListenerFilter_TypedConfig{TypedConfig: retVal}
	}

	return true
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

	if match.ApplicationProtocols != "" {
		if fc.FilterChainMatch == nil {
			return false
		}
		for _, p := range strings.Split(match.ApplicationProtocols, ",") {
			if !slices.Contains(fc.FilterChainMatch.ApplicationProtocols, p) {
				return false
			}
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

	return cp.Match.GetListener().FilterChain.Filter.Name == filter.Name
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
