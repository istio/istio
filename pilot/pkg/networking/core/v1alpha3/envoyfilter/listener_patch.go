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
	xdslistener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wellknown "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"

	"istio.io/pkg/log"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/runtime"
)

const (
	// VirtualOutboundListenerName is the name for traffic capture listener
	VirtualOutboundListenerName = "virtualOutbound"

	// VirtualInboundListenerName is the name for traffic capture listener
	VirtualInboundListenerName = "virtualInbound"
)

var (
	// DeprecatedFilterNames is to support both canonical filter names
	// and deprecated filter names for backward compatibility. Istiod
	// generates canonical filter names.
	DeprecatedFilterNames = map[string]string{
		wellknown.Buffer:                      "envoy.buffer",
		wellknown.CORS:                        "envoy.cors",
		"envoy.filters.http.csrf":             "envoy.csrf",
		wellknown.Dynamo:                      "envoy.http_dynamo_filter",
		wellknown.HTTPExternalAuthorization:   "envoy.ext_authz",
		wellknown.Fault:                       "envoy.fault",
		wellknown.GRPCHTTP1Bridge:             "envoy.grpc_http1_bridge",
		wellknown.GRPCJSONTranscoder:          "envoy.grpc_json_transcoder",
		wellknown.GRPCWeb:                     "envoy.grpc_web",
		wellknown.Gzip:                        "envoy.gzip",
		wellknown.HealthCheck:                 "envoy.health_check",
		wellknown.IPTagging:                   "envoy.ip_tagging",
		wellknown.Lua:                         "envoy.lua",
		wellknown.HTTPRateLimit:               "envoy.rate_limit",
		wellknown.Router:                      "envoy.router",
		wellknown.Squash:                      "envoy.squash",
		wellknown.HttpInspector:               "envoy.listener.http_inspector",
		wellknown.OriginalDestination:         "envoy.listener.original_dst",
		"envoy.filters.listener.original_src": "envoy.listener.original_src",
		wellknown.ProxyProtocol:               "envoy.listener.proxy_protocol",
		wellknown.TlsInspector:                "envoy.listener.tls_inspector",
		wellknown.ClientSSLAuth:               "envoy.client_ssl_auth",
		wellknown.ExternalAuthorization:       "envoy.ext_authz",
		wellknown.HTTPConnectionManager:       "envoy.http_connection_manager",
		wellknown.MongoProxy:                  "envoy.mongo_proxy",
		wellknown.RateLimit:                   "envoy.ratelimit",
		wellknown.RedisProxy:                  "envoy.redis_proxy",
		wellknown.TCPProxy:                    "envoy.tcp_proxy",
	}
)

// ApplyListenerPatches applies patches to LDS output
func ApplyListenerPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	proxy *model.Proxy,
	push *model.PushContext,
	listeners []*xdslistener.Listener,
	skipAdds bool) (out []*xdslistener.Listener) {
	defer runtime.HandleCrash(func() {
		log.Errorf("listeners patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the listeners generated before to reduce the influence.
	out = listeners

	envoyFilterWrapper := push.EnvoyFilters(proxy)
	if envoyFilterWrapper == nil {
		return
	}

	return doListenerListOperation(patchContext, envoyFilterWrapper, listeners, skipAdds)
}

func doListenerListOperation(
	patchContext networking.EnvoyFilter_PatchContext,
	envoyFilterWrapper *model.EnvoyFilterWrapper,
	listeners []*xdslistener.Listener,
	skipAdds bool) []*xdslistener.Listener {
	listenersRemoved := false

	// do all the changes for a single envoy filter crd object. [including adds]
	// then move on to the next one

	// only removes/merges plus next level object operations [add/remove/merge]
	for _, listener := range listeners {
		if listener.Name == "" {
			// removed by another op
			continue
		}
		doListenerOperation(patchContext, envoyFilterWrapper.Patches, listener, &listenersRemoved)
	}
	// adds at listener level if enabled
	if !skipAdds {
		for _, cp := range envoyFilterWrapper.Patches[networking.EnvoyFilter_LISTENER] {
			if cp.Operation == networking.EnvoyFilter_Patch_ADD {
				if !commonConditionMatch(patchContext, cp) {
					continue
				}

				// clone before append. Otherwise, subsequent operations on this listener will corrupt
				// the master value stored in CP..
				listeners = append(listeners, proto.Clone(cp.Value).(*xdslistener.Listener))
			}
		}
	}

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

func doListenerOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, listenersRemoved *bool) {
	for _, cp := range patches[networking.EnvoyFilter_LISTENER] {
		if !commonConditionMatch(patchContext, cp) ||
			!listenerMatch(listener, cp) {
			continue
		}

		if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			listener.Name = ""
			*listenersRemoved = true
			// terminate the function here as we have nothing more do to for this listener
			return
		} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
			proto.Merge(listener, cp.Value)
		}
	}

	doFilterChainListOperation(patchContext, patches, listener)
}

func doFilterChainListOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener) {
	filterChainsRemoved := false
	for i, fc := range listener.FilterChains {
		if fc.Filters == nil {
			continue
		}
		doFilterChainOperation(patchContext, patches, listener, listener.FilterChains[i], &filterChainsRemoved)
	}
	for _, cp := range patches[networking.EnvoyFilter_FILTER_CHAIN] {
		if cp.Operation == networking.EnvoyFilter_Patch_ADD {
			if !commonConditionMatch(patchContext, cp) ||
				!listenerMatch(listener, cp) {
				continue
			}
			listener.FilterChains = append(listener.FilterChains, proto.Clone(cp.Value).(*xdslistener.FilterChain))
		}
	}
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

func doFilterChainOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener,
	fc *xdslistener.FilterChain, filterChainRemoved *bool) {
	for _, cp := range patches[networking.EnvoyFilter_FILTER_CHAIN] {
		if !commonConditionMatch(patchContext, cp) ||
			!listenerMatch(listener, cp) ||
			!filterChainMatch(fc, cp) {
			continue
		}
		if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			fc.Filters = nil
			*filterChainRemoved = true
			// nothing more to do in other patches as we removed this filter chain
			return
		} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
			proto.Merge(fc, cp.Value)
		}
	}
	doNetworkFilterListOperation(patchContext, patches, listener, fc)
}

func doNetworkFilterListOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain) {
	networkFiltersRemoved := false
	for i, filter := range fc.Filters {
		if filter.Name == "" {
			continue
		}
		doNetworkFilterOperation(patchContext, patches, listener, fc, fc.Filters[i], &networkFiltersRemoved)
	}
	for _, cp := range patches[networking.EnvoyFilter_NETWORK_FILTER] {
		if !commonConditionMatch(patchContext, cp) ||
			!listenerMatch(listener, cp) ||
			!filterChainMatch(fc, cp) {
			continue
		}

		if cp.Operation == networking.EnvoyFilter_Patch_ADD {
			fc.Filters = append(fc.Filters, proto.Clone(cp.Value).(*xdslistener.Filter))
		} else if cp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a filter match is same as ADD in the end
			if !hasNetworkFilterMatch(cp) {
				fc.Filters = append(fc.Filters, proto.Clone(cp.Value).(*xdslistener.Filter))
				continue
			}
			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(fc.Filters); i++ {
				if networkFilterMatch(fc.Filters[i], cp) {
					insertPosition = i + 1
					break
				}
			}

			if insertPosition == -1 {
				continue
			}

			clonedVal := proto.Clone(cp.Value).(*xdslistener.Filter)
			fc.Filters = append(fc.Filters, clonedVal)
			if insertPosition < len(fc.Filters)-1 {
				copy(fc.Filters[insertPosition+1:], fc.Filters[insertPosition:])
				fc.Filters[insertPosition] = clonedVal
			}
		} else if cp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE || cp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			// insert before/first without a filter match is same as insert in the beginning
			if !hasNetworkFilterMatch(cp) {
				fc.Filters = append([]*xdslistener.Filter{proto.Clone(cp.Value).(*xdslistener.Filter)}, fc.Filters...)
				continue
			}
			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(fc.Filters); i++ {
				if networkFilterMatch(fc.Filters[i], cp) {
					insertPosition = i
					break
				}
			}

			// If matching filter is not found, then don't insert and continue.
			if insertPosition == -1 {
				continue
			}

			// In case of INSERT_FIRST, if a match is found, still insert it at the top of the filterchain.
			if cp.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
				insertPosition = 0
			}

			clonedVal := proto.Clone(cp.Value).(*xdslistener.Filter)
			fc.Filters = append(fc.Filters, clonedVal)
			copy(fc.Filters[insertPosition+1:], fc.Filters[insertPosition:])
			fc.Filters[insertPosition] = clonedVal
		}
	}
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

func doNetworkFilterOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain,
	filter *xdslistener.Filter, networkFilterRemoved *bool) {
	for _, cp := range patches[networking.EnvoyFilter_NETWORK_FILTER] {
		if !commonConditionMatch(patchContext, cp) ||
			!listenerMatch(listener, cp) ||
			!filterChainMatch(fc, cp) ||
			!networkFilterMatch(filter, cp) {
			continue
		}
		if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			filter.Name = ""
			*networkFilterRemoved = true
			// nothing more to do in other patches as we removed this filter
			return
		} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
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
			userFilter := cp.Value.(*xdslistener.Filter)
			var err error
			// we need to be able to overwrite filter names or simply empty out a filter's configs
			// as they could be supplied through per route filter configs
			filterName := filter.Name
			if userFilter.Name != "" {
				filterName = userFilter.Name
			}
			var retVal *any.Any
			if userFilter.GetTypedConfig() != nil {
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
			filter.Name = filterName
			if retVal != nil {
				filter.ConfigType = &xdslistener.Filter_TypedConfig{TypedConfig: retVal}
			}
		}
	}
	if filter.Name == wellknown.HTTPConnectionManager {
		doHTTPFilterListOperation(patchContext, patches, listener, fc, filter)
	}
}

func doHTTPFilterListOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain, filter *xdslistener.Filter) {
	hcm := &http_conn.HttpConnectionManager{}
	if filter.GetTypedConfig() != nil {
		if err := ptypes.UnmarshalAny(filter.GetTypedConfig(), hcm); err != nil {
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
		doHTTPFilterOperation(patchContext, patches, listener, fc, filter, httpFilter, &httpFiltersRemoved)
	}
	for _, cp := range patches[networking.EnvoyFilter_HTTP_FILTER] {
		if !commonConditionMatch(patchContext, cp) ||
			!listenerMatch(listener, cp) ||
			!filterChainMatch(fc, cp) ||
			!networkFilterMatch(filter, cp) {
			continue
		}

		if cp.Operation == networking.EnvoyFilter_Patch_ADD {
			hcm.HttpFilters = append(hcm.HttpFilters, proto.Clone(cp.Value).(*http_conn.HttpFilter))
		} else if cp.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			// Insert after without a filter match is same as ADD in the end
			if !hasHTTPFilterMatch(cp) {
				hcm.HttpFilters = append(hcm.HttpFilters, proto.Clone(cp.Value).(*http_conn.HttpFilter))
				continue
			}

			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(hcm.HttpFilters); i++ {
				if httpFilterMatch(hcm.HttpFilters[i], cp) {
					insertPosition = i + 1
					break
				}
			}

			if insertPosition == -1 {
				continue
			}

			clonedVal := proto.Clone(cp.Value).(*http_conn.HttpFilter)
			hcm.HttpFilters = append(hcm.HttpFilters, clonedVal)
			if insertPosition < len(hcm.HttpFilters)-1 {
				copy(hcm.HttpFilters[insertPosition+1:], hcm.HttpFilters[insertPosition:])
				hcm.HttpFilters[insertPosition] = clonedVal
			}
		} else if cp.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			// insert before without a filter match is same as insert in the beginning
			if !hasHTTPFilterMatch(cp) {
				hcm.HttpFilters = append([]*http_conn.HttpFilter{proto.Clone(cp.Value).(*http_conn.HttpFilter)}, hcm.HttpFilters...)
				continue
			}

			// find the matching filter first
			insertPosition := -1
			for i := 0; i < len(hcm.HttpFilters); i++ {
				if httpFilterMatch(hcm.HttpFilters[i], cp) {
					insertPosition = i
					break
				}
			}

			if insertPosition == -1 {
				continue
			}

			clonedVal := proto.Clone(cp.Value).(*http_conn.HttpFilter)
			hcm.HttpFilters = append(hcm.HttpFilters, clonedVal)
			copy(hcm.HttpFilters[insertPosition+1:], hcm.HttpFilters[insertPosition:])
			hcm.HttpFilters[insertPosition] = clonedVal
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
	if filter.GetTypedConfig() != nil {
		// convert to any type
		filter.ConfigType = &xdslistener.Filter_TypedConfig{TypedConfig: util.MessageToAny(hcm)}
	}
}

func doHTTPFilterOperation(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	listener *xdslistener.Listener, fc *xdslistener.FilterChain, filter *xdslistener.Filter,
	httpFilter *http_conn.HttpFilter, httpFilterRemoved *bool) {
	for _, cp := range patches[networking.EnvoyFilter_HTTP_FILTER] {
		if !commonConditionMatch(patchContext, cp) ||
			!listenerMatch(listener, cp) ||
			!filterChainMatch(fc, cp) ||
			!networkFilterMatch(filter, cp) ||
			!httpFilterMatch(httpFilter, cp) {
			continue
		}
		if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			httpFilter.Name = ""
			*httpFilterRemoved = true
			// nothing more to do in other patches as we removed this filter
			return
		} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
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
			userHTTPFilter := cp.Value.(*http_conn.HttpFilter)
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
			httpFilter.Name = httpFilterName
			if retVal != nil {
				httpFilter.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: retVal}
			}
		}
	}
}

func listenerMatch(listener *xdslistener.Listener, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetListener()
	if cMatch == nil {
		return true
	}

	if cMatch.Name != "" && cMatch.Name != listener.Name {
		return false
	}

	// skip listener port check for special virtual inbound and outbound listeners
	// to support portNumber listener filter field within those special listeners as well
	if cp.ApplyTo != networking.EnvoyFilter_LISTENER &&
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
func filterChainMatch(fc *xdslistener.FilterChain, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetListener()
	if cMatch == nil {
		return true
	}

	match := cMatch.FilterChain
	if match == nil {
		return true
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
	if cMatch.PortNumber > 0 &&
		fc.FilterChainMatch != nil && fc.FilterChainMatch.DestinationPort != nil &&
		fc.FilterChainMatch.DestinationPort.Value != cMatch.PortNumber {
		return false
	}

	return true
}

func hasNetworkFilterMatch(cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetListener()
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
func networkFilterMatch(filter *xdslistener.Filter, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasNetworkFilterMatch(cp) {
		return true
	}

	return cp.Match.GetListener().FilterChain.Filter.Name == filter.Name ||
		cp.Match.GetListener().FilterChain.Filter.Name == DeprecatedFilterNames[filter.Name]
}

func hasHTTPFilterMatch(cp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasNetworkFilterMatch(cp) {
		return false
	}

	match := cp.Match.GetListener().FilterChain.Filter.SubFilter
	return match != nil
}

// We assume that the parent listener and filter chain, and network filter have already been matched
func httpFilterMatch(filter *http_conn.HttpFilter, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	if !hasHTTPFilterMatch(cp) {
		return true
	}

	match := cp.Match.GetListener().FilterChain.Filter.SubFilter

	return match.Name == filter.Name || match.Name == DeprecatedFilterNames[filter.Name]
}

func patchContextMatch(patchContext networking.EnvoyFilter_PatchContext,
	cp *model.EnvoyFilterConfigPatchWrapper) bool {
	return cp.Match.Context == patchContext || cp.Match.Context == networking.EnvoyFilter_ANY
}

func commonConditionMatch(patchContext networking.EnvoyFilter_PatchContext,
	cp *model.EnvoyFilterConfigPatchWrapper) bool {
	return patchContextMatch(patchContext, cp)
}
