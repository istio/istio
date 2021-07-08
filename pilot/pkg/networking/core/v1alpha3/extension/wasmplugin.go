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
	udpa "github.com/cncf/udpa/go/udpa/type/v1"
	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	hcm_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"google.golang.org/protobuf/proto"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
	securitymodel "istio.io/istio/pilot/pkg/security/model"
)

const (
	wasmFilterType = "envoy.extensions.filters.http.wasm.v3.Wasm"
)

var defaultConfigSource = &envoy_config_core_v3.ConfigSource{
	ConfigSourceSpecifier: &envoy_config_core_v3.ConfigSource_Ads{
		Ads: &envoy_config_core_v3.AggregatedConfigSource{},
	},
	ResourceApiVersion: envoy_config_core_v3.ApiVersion_V3,
}

// AddWasmPluginsToMutableObjects adds WasmPlugins to HTTP filterChains
func AddWasmPluginsToMutableObjects(
	mutable *networking.MutableObjects,
	extensionsMap map[extensions.PluginPhase][]*model.WasmPluginWrapper,
	proxy *model.Proxy,
	push *model.PushContext,
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
	for _, httpFilter := range filterChain {
		switch httpFilter.Name {
		case securitymodel.EnvoyJwtFilterName:
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		case securitymodel.AuthnFilterName:
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		case authzmodel.RBACHTTPFilterName:
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
			newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHZ)
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		default:
			newHTTPFilters = append(newHTTPFilters, httpFilter)
		}
	}
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHN)
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_AUTHZ)
	// TODO: stats are currently injected using EnvoyFilter, we should migrate them to
	// WasmPlugin (with phase: stats and priority: -maxInt) so this becomes effective
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_STATS)
	newHTTPFilters = popAppend(newHTTPFilters, extMap, extensions.PluginPhase_UNSPECIFIED)
	return newHTTPFilters
}

func popAppend(list []*hcm_filter.HttpFilter,
	filterMap map[extensions.PluginPhase][]*model.WasmPluginWrapper,
	phase extensions.PluginPhase) []*hcm_filter.HttpFilter {
	for _, ext := range filterMap[phase] {
		if filter := toEnvoyHTTPFilter(ext); filter != nil {
			list = append(list, filter)
		}
	}
	filterMap[phase] = []*model.WasmPluginWrapper{}
	return list
}

func toEnvoyHTTPFilter(wasmPlugin *model.WasmPluginWrapper) *hcm_filter.HttpFilter {
	return &hcm_filter.HttpFilter{
		Name: wasmPlugin.Name,
		ConfigType: &hcm_filter.HttpFilter_ConfigDiscovery{
			ConfigDiscovery: &envoy_config_core_v3.ExtensionConfigSource{
				ConfigSource: defaultConfigSource,
				TypeUrls:     []string{wasmFilterType},
			},
		},
	}
}

// InsertedExtensionConfigurations returns extension configurations added via EnvoyFilter.
func InsertedExtensionConfigurations(
	wasmPlugins map[extensions.PluginPhase][]*model.WasmPluginWrapper,
	names []string) []*envoy_config_core_v3.TypedExtensionConfig {
	result := make([]*envoy_config_core_v3.TypedExtensionConfig, 0)
	if len(wasmPlugins) == 0 {
		return result
	}
	hasName := make(map[string]bool)
	for _, n := range names {
		hasName[n] = true
	}
	for _, list := range wasmPlugins {
		for _, p := range list {
			ws, _ := conversion.MessageToStruct(p.ExtensionConfiguration)
			ec := &envoy_config_core_v3.TypedExtensionConfig{
				Name: p.Name,
				TypedConfig: util.MessageToAny(&udpa.TypedStruct{
					TypeUrl: "type.googleapis.com/" + wasmFilterType,
					Value:   ws,
				}),
			}
			if _, ok := hasName[ec.GetName()]; ok {
				result = append(result, proto.Clone(ec).(*envoy_config_core_v3.TypedExtensionConfig))
			}
		}
	}
	return result
}
