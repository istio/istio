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
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wasmextensions "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

var defaultConfigSource = &core.ConfigSource{
	ConfigSourceSpecifier: &core.ConfigSource_Ads{
		Ads: &core.AggregatedConfigSource{},
	},
	ResourceApiVersion: core.ApiVersion_V3,
	// we block proxy init until extension filters are loaded because they might be
	// critical for security (e.g. authn/authz)
	InitialFetchTimeout: &durationpb.Duration{Seconds: 0},
}

func updatePluginConfig(pluginConfig *wasmextensions.PluginConfig, pullSecrets map[string][]byte) {
	// Find the pull secret resource name from wasm vm env variables.
	// The Wasm extension config should already have a `ISTIO_META_WASM_IMAGE_PULL_SECRET` env variable
	// at in the VM env variables, with value being the secret resource name. We try to find the actual
	// secret, and replace the env variable value with it. When ECDS config update reaches the proxy,
	// agent will extract out the secret from env variable, use it for image pulling, and strip the
	// env variable from VM config before forwarding it to envoy.
	envs := pluginConfig.GetVmConfig().GetEnvironmentVariables().GetKeyValues()
	secretName := envs[model.WasmSecretEnv]
	if secretName != "" {
		if sec, found := pullSecrets[secretName]; found {
			envs[model.WasmSecretEnv] = string(sec)
		} else {
			envs[model.WasmSecretEnv] = ""
		}
	}
}

// PopAppendHTTPExtensionFilter takes a list of HTTP filters and a set of ExtensionFilters, keyed by phase.
// It will remove all filters of the provided phase from the ExtensionFilter set and append them to the list of filters.
func PopAppendHTTPExtensionFilter(list []*hcm.HttpFilter,
	filterMap map[extensions.PluginPhase][]*model.ExtensionFilterWrapper,
	phase extensions.PluginPhase,
) []*hcm.HttpFilter {
	for _, ext := range filterMap[phase] {
		if filter := toEnvoyHTTPExtensionFilter(ext); filter != nil {
			list = append(list, filter)
		}
	}
	delete(filterMap, phase)
	return list
}

// PopAppendNetworkExtensionFilter takes a list of network filters and a set of ExtensionFilters, keyed by phase.
// It will remove all filters of the provided phase from the ExtensionFilter set and append them to the list of filters.
func PopAppendNetworkExtensionFilter(list []*listener.Filter,
	filterMap map[extensions.PluginPhase][]*model.ExtensionFilterWrapper,
	phase extensions.PluginPhase,
) []*listener.Filter {
	for _, ext := range filterMap[phase] {
		if filter := toEnvoyNetworkExtensionFilter(ext); filter != nil {
			list = append(list, filter)
		}
	}
	delete(filterMap, phase)
	return list
}

// toEnvoyHTTPExtensionFilter converts an ExtensionFilterWrapper to an Envoy HTTP filter.
// For Lua filters, it inlines the code directly using TypedConfig.
// For WASM filters, it uses ConfigDiscovery (ECDS).
func toEnvoyHTTPExtensionFilter(filter *model.ExtensionFilterWrapper) *hcm.HttpFilter {
	if filter == nil {
		return nil
	}

	switch filter.FilterType {
	case model.FilterTypeLua:
		// Lua filters are inlined directly
		luaConfig := BuildHTTPLuaFilter(filter)
		if luaConfig == nil {
			return nil
		}
		return &hcm.HttpFilter{
			Name: filter.ResourceName,
			ConfigType: &hcm.HttpFilter_TypedConfig{
				TypedConfig: protoconv.MessageToAny(luaConfig),
			},
		}
	case model.FilterTypeWasm:
		// WASM filters use ECDS
		return &hcm.HttpFilter{
			Name: filter.ResourceName,
			ConfigType: &hcm.HttpFilter_ConfigDiscovery{
				ConfigDiscovery: &core.ExtensionConfigSource{
					ConfigSource: defaultConfigSource,
					TypeUrls: []string{
						xds.WasmHTTPFilterType,
						xds.RBACHTTPFilterType,
					},
				},
			},
		}
	default:
		log.Warnf("unknown filter type for ExtensionFilter %s", filter.ResourceName)
		return nil
	}
}

// toEnvoyNetworkExtensionFilter converts an ExtensionFilterWrapper to an Envoy network filter.
// Only WASM filters are supported for network (L4) filtering.
// Lua filters do not support network filtering and will return nil with a warning.
func toEnvoyNetworkExtensionFilter(filter *model.ExtensionFilterWrapper) *listener.Filter {
	if filter == nil {
		return nil
	}

	switch filter.FilterType {
	case model.FilterTypeLua:
		// Lua filters do not support network (L4) filtering
		log.Warnf("Lua filters do not support network filtering, skipping ExtensionFilter %s", filter.ResourceName)
		return nil
	case model.FilterTypeWasm:
		// WASM filters use ECDS
		return &listener.Filter{
			Name: filter.ResourceName,
			ConfigType: &listener.Filter_ConfigDiscovery{
				ConfigDiscovery: &core.ExtensionConfigSource{
					ConfigSource: defaultConfigSource,
					TypeUrls: []string{
						xds.WasmNetworkFilterType,
						xds.RBACNetworkFilterType,
					},
				},
			},
		}
	default:
		log.Warnf("unknown filter type for ExtensionFilter %s", filter.ResourceName)
		return nil
	}
}

// InsertedExtensionFilterConfigurations builds ECDS configurations for ExtensionFilters.
// Only WASM filters are included; Lua filters are already inlined in listeners.
func InsertedExtensionFilterConfigurations(
	extensionFilters []*model.ExtensionFilterWrapper,
	names []string, pullSecrets map[string][]byte,
) []*core.TypedExtensionConfig {
	result := make([]*core.TypedExtensionConfig, 0)
	if len(extensionFilters) == 0 {
		return result
	}
	hasName := sets.New(names...)
	for _, filter := range extensionFilters {
		if !hasName.Contains(filter.ResourceName) {
			continue
		}
		// Skip Lua filters - they are inlined directly, not via ECDS
		if filter.FilterType == model.FilterTypeLua {
			continue
		}
		// Only WASM filters use ECDS
		switch filter.Wasm.Type {
		case extensions.PluginType_NETWORK:
			wasmExtensionConfig := filter.BuildNetworkWasmFilter()
			if wasmExtensionConfig == nil {
				continue
			}
			updatePluginConfig(wasmExtensionConfig.GetConfig(), pullSecrets)
			typedConfig := protoconv.MessageToAny(wasmExtensionConfig)
			ec := &core.TypedExtensionConfig{
				Name:        filter.ResourceName,
				TypedConfig: typedConfig,
			}
			result = append(result, ec)
		default:
			wasmExtensionConfig := filter.BuildHTTPWasmFilter()
			if wasmExtensionConfig == nil {
				continue
			}
			updatePluginConfig(wasmExtensionConfig.GetConfig(), pullSecrets)
			typedConfig := protoconv.MessageToAny(wasmExtensionConfig)
			ec := &core.TypedExtensionConfig{
				Name:        filter.ResourceName,
				TypedConfig: typedConfig,
			}
			result = append(result, ec)
		}
	}
	return result
}
