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

package extension

import (
	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	hcm_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	securitymodel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/util/sets"

	// include for registering wasm logging scope
	_ "istio.io/istio/pkg/wasm"
)

const (
	wasmFilterType  = "envoy.extensions.filters.http.wasm.v3.Wasm"
	statsFilterName = "istio.stats"
)

var defaultConfigSource = &envoy_config_core_v3.ConfigSource{
	ConfigSourceSpecifier: &envoy_config_core_v3.ConfigSource_Ads{
		Ads: &envoy_config_core_v3.AggregatedConfigSource{},
	},
	ResourceApiVersion: envoy_config_core_v3.ApiVersion_V3,
	// we block proxy init until WasmPlugins are loaded because they might be
	// critical for security (e.g. authn/authz)
	InitialFetchTimeout: &durationpb.Duration{Seconds: 0},
}

// AddWasmPluginsToMutableObjects adds WasmPlugins to HTTP filterChains
// Note that the slices in the map must already be ordered by plugin
// priority! This will be the case for maps returned by PushContext.WasmPlugin()
func AddWasmPluginsToMutableObjects(
	mutable *networking.MutableObjects,
	extensionsMap map[extensions.PluginPhase][]*model.WasmPluginWrapper,
) {
	if mutable == nil {
		return
	}

	for fcIndex, fc := range mutable.FilterChains {
		// we currently only support HTTP
		if fc.ListenerProtocol != networking.ListenerProtocolHTTP {
			continue
		}
		mutable.FilterChains[fcIndex].HTTP = injectExtensions(fc.HTTP, extensionsMap)
	}
}

func injectExtensions(filterChain []*hcm_filter.HttpFilter, exts map[extensions.PluginPhase][]*model.WasmPluginWrapper) []*hcm_filter.HttpFilter {
	// copy map as we'll manipulate it in the loop
	extMap := make(map[extensions.PluginPhase][]*model.WasmPluginWrapper)
	for phase, list := range exts {
		extMap[phase] = []*model.WasmPluginWrapper{}
		extMap[phase] = append(extMap[phase], list...)
	}
	newHTTPFilters := make([]*hcm_filter.HttpFilter, 0)
	// The following algorithm tries to make as few assumptions as possible about the filter
	// chain - it might contain any number of filters that will have to retain their ordering.
	// The one assumption we make is about the ordering of the builtin filters. This is used to
	// position WasmPlugins relatively to the builtin filters according to their phase: when
	// we see the Stats filter, we know that all WasmPlugins with phases AUTHN, AUTHZ and STATS
	// must be injected before it. This method allows us to inject WasmPlugins in the right spots
	// while retaining any filters that were unknown at the time of writing this algorithm,
	// in linear time. The assumed ordering of builtin filters is:
	//
	// 1. Istio JWT, 2. Istio AuthN, 3. RBAC, 4. Stats, 5. Metadata Exchange
	//
	// TODO: how to deal with ext-authz? RBAC will be in the chain twice in that case
	for _, httpFilter := range filterChain {
		switch httpFilter.Name {
		case securitymodel.EnvoyJwtFilterName:
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		case securitymodel.AuthnFilterName:
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		case wellknown.HTTPRoleBasedAccessControl:
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHZ)
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		case statsFilterName:
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHZ)
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_STATS)
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		default:
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		}
	}
	// append all remaining extensions at the end. This is required because not all builtin filters
	// are always present (e.g. RBAC is only present when an AuthorizationPolicy was created), so
	// we might not have emptied all slices in the map.
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHZ)
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_STATS)
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_UNSPECIFIED_PHASE)
	return newHTTPFilters
}

func popAppend(list []*hcm_filter.HttpFilter,
	filterMap map[extensions.PluginPhase][]*model.WasmPluginWrapper,
	phase extensions.PluginPhase) []*hcm_filter.HttpFilter {
	for _, ext := range filterMap[phase] {
		list = append(list, toEnvoyHTTPFilter(ext))
	}
	filterMap[phase] = []*model.WasmPluginWrapper{}
	return list
}

func toEnvoyHTTPFilter(wasmPlugin *model.WasmPluginWrapper) *hcm_filter.HttpFilter {
	return &hcm_filter.HttpFilter{
		Name: wasmPlugin.ExtensionConfiguration.Name,
		ConfigType: &hcm_filter.HttpFilter_ConfigDiscovery{
			ConfigDiscovery: &envoy_config_core_v3.ExtensionConfigSource{
				ConfigSource: defaultConfigSource,
				TypeUrls:     []string{"type.googleapis.com/" + wasmFilterType},
			},
		},
	}
}

// InsertedExtensionConfigurations returns pre-generated extension configurations added via WasmPlugin.
func InsertedExtensionConfigurations(
	wasmPlugins map[extensions.PluginPhase][]*model.WasmPluginWrapper,
	names []string) []*envoy_config_core_v3.TypedExtensionConfig {
	result := make([]*envoy_config_core_v3.TypedExtensionConfig, 0)
	if len(wasmPlugins) == 0 {
		return result
	}
	hasName := sets.NewWith(names...)
	for _, list := range wasmPlugins {
		for _, p := range list {
			if !hasName.Contains(p.ExtensionConfiguration.Name) {
				continue
			}
			result = append(result, proto.Clone(p.ExtensionConfiguration).(*envoy_config_core_v3.TypedExtensionConfig))
		}
	}
	return result
}
