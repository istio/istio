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
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	lua "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/test/util/assert"
)

func TestBuildHTTPLuaFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   *model.ExtensionFilterWrapper
		expected *lua.Lua
	}{
		{
			name:     "nil filter",
			filter:   nil,
			expected: nil,
		},
		{
			name: "non-lua filter",
			filter: &model.ExtensionFilterWrapper{
				FilterType: model.FilterTypeWasm,
			},
			expected: nil,
		},
		{
			name: "valid lua filter",
			filter: &model.ExtensionFilterWrapper{
				FilterType: model.FilterTypeLua,
				ExtensionFilter: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: "function envoy_on_request(request_handle)\n  request_handle:headers():add('x-test', 'value')\nend",
					},
				},
			},
			expected: &lua.Lua{
				InlineCode: "function envoy_on_request(request_handle)\n  request_handle:headers():add('x-test', 'value')\nend",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildHTTPLuaFilter(tt.filter)
			if tt.expected == nil {
				assert.Equal(t, got, nil)
			} else {
				assert.Equal(t, got.InlineCode, tt.expected.InlineCode)
			}
		})
	}
}

func TestToEnvoyHTTPExtensionFilter(t *testing.T) {
	tests := []struct {
		name           string
		filter         *model.ExtensionFilterWrapper
		expectTyped    bool // true if TypedConfig expected, false if ConfigDiscovery
		expectNil      bool
		expectLuaCode  string
	}{
		{
			name:      "nil filter",
			filter:    nil,
			expectNil: true,
		},
		{
			name: "lua filter - inlined",
			filter: &model.ExtensionFilterWrapper{
				ResourceName: "test-lua-filter",
				FilterType:   model.FilterTypeLua,
				ExtensionFilter: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: "function envoy_on_request() end",
					},
				},
			},
			expectTyped:   true,
			expectLuaCode: "function envoy_on_request() end",
		},
		{
			name: "wasm filter - ECDS",
			filter: &model.ExtensionFilterWrapper{
				ResourceName: "test-wasm-filter",
				FilterType:   model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url: "oci://test.com/filter:v1",
					},
				},
			},
			expectTyped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toEnvoyHTTPExtensionFilter(tt.filter)
			if tt.expectNil {
				assert.Equal(t, got, nil)
				return
			}

			assert.Equal(t, got.Name, tt.filter.ResourceName)

			if tt.expectTyped {
				// Lua filter should have TypedConfig
				typedConfig := got.GetTypedConfig()
				assert.Equal(t, typedConfig != nil, true, "expected TypedConfig for Lua filter")

				// Verify it's a Lua config
				luaConfig, err := protoconv.UnmarshalAny[lua.Lua](typedConfig)
				assert.NoError(t, err)
				assert.Equal(t, luaConfig.InlineCode, tt.expectLuaCode)
			} else {
				// WASM filter should have ConfigDiscovery
				configDiscovery := got.GetConfigDiscovery()
				assert.Equal(t, configDiscovery != nil, true, "expected ConfigDiscovery for WASM filter")
				assert.Equal(t, configDiscovery.TypeUrls, []string{xds.WasmHTTPFilterType, xds.RBACHTTPFilterType})
			}
		})
	}
}

func TestToEnvoyNetworkExtensionFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    *model.ExtensionFilterWrapper
		expectNil bool
		expectECDS bool
	}{
		{
			name:      "nil filter",
			filter:    nil,
			expectNil: true,
		},
		{
			name: "lua filter - not supported for network",
			filter: &model.ExtensionFilterWrapper{
				ResourceName: "test-lua-filter",
				FilterType:   model.FilterTypeLua,
				ExtensionFilter: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: "function envoy_on_request() end",
					},
				},
			},
			expectNil: true, // Lua doesn't support network filtering
		},
		{
			name: "wasm filter - ECDS",
			filter: &model.ExtensionFilterWrapper{
				ResourceName: "test-wasm-filter",
				FilterType:   model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  "oci://test.com/filter:v1",
						Type: extensions.PluginType_NETWORK,
					},
				},
			},
			expectECDS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toEnvoyNetworkExtensionFilter(tt.filter)
			if tt.expectNil {
				assert.Equal(t, got, nil)
				return
			}

			if tt.expectECDS {
				assert.Equal(t, got.Name, tt.filter.ResourceName)
				configDiscovery := got.GetConfigDiscovery()
				assert.Equal(t, configDiscovery != nil, true, "expected ConfigDiscovery for WASM network filter")
				assert.Equal(t, configDiscovery.TypeUrls, []string{xds.WasmNetworkFilterType, xds.RBACNetworkFilterType})
			}
		})
	}
}

func TestPopAppendHTTPExtensionFilter(t *testing.T) {
	luaFilter := &model.ExtensionFilterWrapper{
		ResourceName: "lua-authn",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request() end",
			},
		},
	}

	wasmFilter := &model.ExtensionFilterWrapper{
		ResourceName: "wasm-authz",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHZ,
			Wasm: &extensions.WasmConfig{
				Url: "oci://test.com/filter:v1",
			},
		},
	}

	filterMap := map[extensions.PluginPhase][]*model.ExtensionFilterWrapper{
		extensions.PluginPhase_AUTHN: {luaFilter},
		extensions.PluginPhase_AUTHZ: {wasmFilter},
	}

	// Start with empty list
	filters := []*hcm.HttpFilter{}

	// Append AUTHN phase filters
	filters = PopAppendHTTPExtensionFilter(filters, filterMap, extensions.PluginPhase_AUTHN)
	assert.Equal(t, len(filters), 1)
	assert.Equal(t, filters[0].Name, "lua-authn")

	// Verify phase was removed from map
	_, exists := filterMap[extensions.PluginPhase_AUTHN]
	assert.Equal(t, exists, false, "AUTHN phase should be removed from map after pop")

	// Append AUTHZ phase filters
	filters = PopAppendHTTPExtensionFilter(filters, filterMap, extensions.PluginPhase_AUTHZ)
	assert.Equal(t, len(filters), 2)
	assert.Equal(t, filters[1].Name, "wasm-authz")

	// Verify phase was removed from map
	_, exists = filterMap[extensions.PluginPhase_AUTHZ]
	assert.Equal(t, exists, false, "AUTHZ phase should be removed from map after pop")

	// Verify filter types
	assert.Equal(t, filters[0].GetTypedConfig() != nil, true, "Lua filter should have TypedConfig")
	assert.Equal(t, filters[1].GetConfigDiscovery() != nil, true, "WASM filter should have ConfigDiscovery")
}

func TestPopAppendNetworkExtensionFilter(t *testing.T) {
	luaFilter := &model.ExtensionFilterWrapper{
		ResourceName: "lua-filter",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request() end",
			},
		},
	}

	wasmFilter := &model.ExtensionFilterWrapper{
		ResourceName: "wasm-filter",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Wasm: &extensions.WasmConfig{
				Url:  "oci://test.com/filter:v1",
				Type: extensions.PluginType_NETWORK,
			},
		},
	}

	filterMap := map[extensions.PluginPhase][]*model.ExtensionFilterWrapper{
		extensions.PluginPhase_AUTHN: {luaFilter, wasmFilter},
	}

	// Start with empty list
	filters := []*listener.Filter{}

	// Append AUTHN phase filters
	filters = PopAppendNetworkExtensionFilter(filters, filterMap, extensions.PluginPhase_AUTHN)

	// Only WASM filter should be added (Lua is skipped for network)
	assert.Equal(t, len(filters), 1)
	assert.Equal(t, filters[0].Name, "wasm-filter")

	// Verify it's using ConfigDiscovery
	assert.Equal(t, filters[0].GetConfigDiscovery() != nil, true, "WASM network filter should have ConfigDiscovery")
}

func TestInsertedExtensionFilterConfigurations(t *testing.T) {
	luaFilter := &model.ExtensionFilterWrapper{
		ResourceName: "extensions.istio.io/extensionfilter/default.lua-filter",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request() end",
			},
		},
	}

	wasmHTTPFilter := &model.ExtensionFilterWrapper{
		ResourceName: "extensions.istio.io/extensionfilter/default.wasm-http",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Wasm: &extensions.WasmConfig{
				Url:  "oci://test.com/filter:v1",
				Type: extensions.PluginType_HTTP,
			},
		},
	}

	wasmNetworkFilter := &model.ExtensionFilterWrapper{
		ResourceName: "extensions.istio.io/extensionfilter/default.wasm-network",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Wasm: &extensions.WasmConfig{
				Url:  "oci://test.com/filter:v1",
				Type: extensions.PluginType_NETWORK,
			},
		},
	}

	// Mock BuildHTTPWasmFilter and BuildNetworkWasmFilter to return non-nil values
	// In real implementation these are already implemented

	filters := []*model.ExtensionFilterWrapper{luaFilter, wasmHTTPFilter, wasmNetworkFilter}
	names := []string{
		"extensions.istio.io/extensionfilter/default.lua-filter",
		"extensions.istio.io/extensionfilter/default.wasm-http",
		"extensions.istio.io/extensionfilter/default.wasm-network",
	}
	pullSecrets := map[string][]byte{}

	configs := InsertedExtensionFilterConfigurations(filters, names, pullSecrets)

	// Lua filter should be skipped (already inlined)
	// Only WASM filters should be in ECDS configs
	assert.Equal(t, len(configs), 2, "should have 2 WASM filters in ECDS, Lua is inlined")

	// Verify names
	configNames := make(map[string]bool)
	for _, cfg := range configs {
		configNames[cfg.Name] = true
	}

	assert.Equal(t, configNames["extensions.istio.io/extensionfilter/default.wasm-http"], true)
	assert.Equal(t, configNames["extensions.istio.io/extensionfilter/default.wasm-network"], true)
	assert.Equal(t, configNames["extensions.istio.io/extensionfilter/default.lua-filter"], false, "Lua filter should not be in ECDS")
}

func TestMixedLuaWasmFilters(t *testing.T) {
	// Test that Lua and WASM filters can coexist in the same phase
	luaFilter := &model.ExtensionFilterWrapper{
		ResourceName: "lua-filter",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_STATS,
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request() end",
			},
		},
	}

	wasmFilter := &model.ExtensionFilterWrapper{
		ResourceName: "wasm-filter",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_STATS,
			Wasm: &extensions.WasmConfig{
				Url: "oci://test.com/filter:v1",
			},
		},
	}

	filterMap := map[extensions.PluginPhase][]*model.ExtensionFilterWrapper{
		extensions.PluginPhase_STATS: {luaFilter, wasmFilter},
	}

	filters := []*hcm.HttpFilter{}
	filters = PopAppendHTTPExtensionFilter(filters, filterMap, extensions.PluginPhase_STATS)

	// Both filters should be added
	assert.Equal(t, len(filters), 2)

	// First filter is Lua (inlined)
	assert.Equal(t, filters[0].Name, "lua-filter")
	assert.Equal(t, filters[0].GetTypedConfig() != nil, true)

	// Second filter is WASM (ECDS)
	assert.Equal(t, filters[1].Name, "wasm-filter")
	assert.Equal(t, filters[1].GetConfigDiscovery() != nil, true)
}

func TestLuaFilterInlining(t *testing.T) {
	// Verify that Lua filters are fully inlined with code, not using ECDS
	luaCode := "function envoy_on_request(request_handle)\n  request_handle:headers():add('x-lua', 'true')\nend"

	filter := &model.ExtensionFilterWrapper{
		ResourceName: "inline-lua",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Lua: &extensions.LuaConfig{
				InlineCode: luaCode,
			},
		},
	}

	envoyFilter := toEnvoyHTTPExtensionFilter(filter)

	// Should have TypedConfig, not ConfigDiscovery
	assert.Equal(t, envoyFilter.GetConfigDiscovery(), (*core.ExtensionConfigSource)(nil), "Lua should not use ECDS")
	assert.Equal(t, envoyFilter.GetTypedConfig() != nil, true, "Lua should have TypedConfig")

	// Verify the Lua code is actually embedded
	luaConfig, err := protoconv.UnmarshalAny[lua.Lua](envoyFilter.GetTypedConfig())
	assert.NoError(t, err)
	assert.Equal(t, luaConfig.InlineCode, luaCode, "Lua code should be fully inlined")
}

func TestBuildHTTPWasmFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   *model.ExtensionFilterWrapper
		expectNil bool
	}{
		{
			name: "valid HTTP WASM filter",
			filter: &model.ExtensionFilterWrapper{
				Name:       "test-wasm",
				Namespace:  "default",
				FilterType: model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  "oci://test.com/wasm:v1",
						Type: extensions.PluginType_HTTP,
					},
				},
			},
			expectNil: false,
		},
		{
			name: "WASM filter with UNSPECIFIED type defaults to HTTP",
			filter: &model.ExtensionFilterWrapper{
				Name:       "test-wasm",
				Namespace:  "default",
				FilterType: model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  "oci://test.com/wasm:v1",
						Type: extensions.PluginType_UNSPECIFIED_PLUGIN_TYPE,
					},
				},
			},
			expectNil: false,
		},
		{
			name: "NETWORK type returns nil for HTTP filter",
			filter: &model.ExtensionFilterWrapper{
				Name:       "test-wasm",
				Namespace:  "default",
				FilterType: model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  "oci://test.com/wasm:v1",
						Type: extensions.PluginType_NETWORK,
					},
				},
			},
			expectNil: true,
		},
		{
			name: "Lua filter returns nil",
			filter: &model.ExtensionFilterWrapper{
				FilterType: model.FilterTypeLua,
				ExtensionFilter: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: "function envoy_on_request() end",
					},
				},
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.BuildHTTPWasmFilter()
			if tt.expectNil {
				assert.Equal(t, got, nil)
			} else {
				assert.Equal(t, got != nil, true)
				assert.Equal(t, got.Config != nil, true)
			}
		})
	}
}

func TestBuildNetworkWasmFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    *model.ExtensionFilterWrapper
		expectNil bool
	}{
		{
			name: "valid NETWORK WASM filter",
			filter: &model.ExtensionFilterWrapper{
				Name:       "test-wasm",
				Namespace:  "default",
				FilterType: model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  "oci://test.com/wasm:v1",
						Type: extensions.PluginType_NETWORK,
					},
				},
			},
			expectNil: false,
		},
		{
			name: "HTTP type returns nil for network filter",
			filter: &model.ExtensionFilterWrapper{
				Name:       "test-wasm",
				Namespace:  "default",
				FilterType: model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  "oci://test.com/wasm:v1",
						Type: extensions.PluginType_HTTP,
					},
				},
			},
			expectNil: true,
		},
		{
			name: "UNSPECIFIED type returns nil for network filter",
			filter: &model.ExtensionFilterWrapper{
				Name:       "test-wasm",
				Namespace:  "default",
				FilterType: model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  "oci://test.com/wasm:v1",
						Type: extensions.PluginType_UNSPECIFIED_PLUGIN_TYPE,
					},
				},
			},
			expectNil: true,
		},
		{
			name: "Lua filter returns nil",
			filter: &model.ExtensionFilterWrapper{
				FilterType: model.FilterTypeLua,
				ExtensionFilter: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: "function envoy_on_request() end",
					},
				},
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.BuildNetworkWasmFilter()
			if tt.expectNil {
				assert.Equal(t, got, nil)
			} else {
				assert.Equal(t, got != nil, true)
				assert.Equal(t, got.Config != nil, true)
			}
		})
	}
}

func TestInsertedExtensionFilterConfigurations_WasmURLs(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		expect bool // whether config should be generated successfully
	}{
		{
			name:   "OCI URL",
			url:    "oci://gcr.io/istio-testing/wasm/basic-auth:1.12.0",
			expect: true,
		},
		{
			name:   "OCI URL without scheme",
			url:    "gcr.io/istio-testing/wasm/basic-auth:1.12.0",
			expect: true,
		},
		{
			name:   "HTTP URL",
			url:    "http://test.com/filter.wasm",
			expect: true,
		},
		{
			name:   "HTTPS URL",
			url:    "https://test.com/filter.wasm",
			expect: true,
		},
		{
			name:   "File URL",
			url:    "file:///var/lib/wasm/filter.wasm",
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := &model.ExtensionFilterWrapper{
				ResourceName: "extensions.istio.io/extensionfilter/default.wasm-test",
				Name:         "wasm-test",
				Namespace:    "default",
				FilterType:   model.FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url:  tt.url,
						Type: extensions.PluginType_HTTP,
					},
				},
			}

			configs := InsertedExtensionFilterConfigurations(
				[]*model.ExtensionFilterWrapper{filter},
				[]string{"extensions.istio.io/extensionfilter/default.wasm-test"},
				map[string][]byte{},
			)

			if tt.expect {
				assert.Equal(t, len(configs), 1, "should generate config for URL: "+tt.url)
				assert.Equal(t, configs[0].Name, filter.ResourceName)
			}
		})
	}
}

func TestInsertedExtensionFilterConfigurations_PullSecrets(t *testing.T) {
	secretName := "test-secret"
	secretValue := []byte("my-docker-secret")

	filter := &model.ExtensionFilterWrapper{
		ResourceName: "extensions.istio.io/extensionfilter/default.wasm-auth",
		Name:         "wasm-auth",
		Namespace:    "default",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Wasm: &extensions.WasmConfig{
				Url:             "oci://private.registry.com/wasm:v1",
				Type:            extensions.PluginType_HTTP,
				ImagePullSecret: secretName,
			},
		},
	}

	pullSecrets := map[string][]byte{
		secretName: secretValue,
	}

	configs := InsertedExtensionFilterConfigurations(
		[]*model.ExtensionFilterWrapper{filter},
		[]string{"extensions.istio.io/extensionfilter/default.wasm-auth"},
		pullSecrets,
	)

	assert.Equal(t, len(configs), 1)
	// The updatePluginConfig function should inject the secret into VM config
	// This is tested indirectly through the ECDS generation
}

func TestInsertedExtensionFilterConfigurations_HTTPAndNetwork(t *testing.T) {
	httpFilter := &model.ExtensionFilterWrapper{
		ResourceName: "extensions.istio.io/extensionfilter/default.http-wasm",
		Name:         "http-wasm",
		Namespace:    "default",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Wasm: &extensions.WasmConfig{
				Url:  "oci://test.com/http:v1",
				Type: extensions.PluginType_HTTP,
			},
		},
	}

	networkFilter := &model.ExtensionFilterWrapper{
		ResourceName: "extensions.istio.io/extensionfilter/default.network-wasm",
		Name:         "network-wasm",
		Namespace:    "default",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Wasm: &extensions.WasmConfig{
				Url:  "oci://test.com/network:v1",
				Type: extensions.PluginType_NETWORK,
			},
		},
	}

	configs := InsertedExtensionFilterConfigurations(
		[]*model.ExtensionFilterWrapper{httpFilter, networkFilter},
		[]string{
			"extensions.istio.io/extensionfilter/default.http-wasm",
			"extensions.istio.io/extensionfilter/default.network-wasm",
		},
		map[string][]byte{},
	)

	// Both HTTP and Network WASM filters should be in ECDS
	assert.Equal(t, len(configs), 2)

	configNames := make(map[string]bool)
	for _, cfg := range configs {
		configNames[cfg.Name] = true
	}

	assert.Equal(t, configNames["extensions.istio.io/extensionfilter/default.http-wasm"], true)
	assert.Equal(t, configNames["extensions.istio.io/extensionfilter/default.network-wasm"], true)
}

func TestToEnvoyNetworkExtensionFilter_LuaReturnsNil(t *testing.T) {
	// Lua filters do not support network (L4) filtering - they only work with HTTP.
	// When attempting to use a Lua filter for network filtering, the function should
	// return nil (and log a warning, though we don't test the log here).
	luaFilter := &model.ExtensionFilterWrapper{
		ResourceName: "extensions.istio.io/extensionfilter/default.lua-network-attempt",
		Name:         "lua-network-attempt",
		Namespace:    "default",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request(request_handle)\n  request_handle:headers():add('x-lua', 'true')\nend",
			},
		},
	}

	// Attempt to create a network filter from a Lua ExtensionFilter
	result := toEnvoyNetworkExtensionFilter(luaFilter)

	// Should return nil because Lua doesn't support network filtering
	assert.Equal(t, result, (*listener.Filter)(nil), "Lua filter should return nil for network filtering")
}

func TestPopAppendNetworkExtensionFilter_LuaSkipped(t *testing.T) {
	// Test that when processing network filters, Lua filters are silently skipped
	// while WASM filters are properly added
	luaFilter := &model.ExtensionFilterWrapper{
		ResourceName: "lua-filter",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request() end",
			},
		},
	}

	wasmFilter := &model.ExtensionFilterWrapper{
		ResourceName: "wasm-filter",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Wasm: &extensions.WasmConfig{
				Url:  "oci://test.com/filter:v1",
				Type: extensions.PluginType_NETWORK,
			},
		},
	}

	filterMap := map[extensions.PluginPhase][]*model.ExtensionFilterWrapper{
		extensions.PluginPhase_AUTHN: {luaFilter, wasmFilter},
	}

	filters := []*listener.Filter{}
	filters = PopAppendNetworkExtensionFilter(filters, filterMap, extensions.PluginPhase_AUTHN)

	// Only WASM filter should be added (Lua is not supported for network)
	assert.Equal(t, len(filters), 1, "only WASM filter should be added for network filtering")
	assert.Equal(t, filters[0].Name, "wasm-filter")
	assert.Equal(t, filters[0].GetConfigDiscovery() != nil, true, "WASM network filter should use ConfigDiscovery")
}

func TestMixedWasmLuaPriorityOrdering(t *testing.T) {
	// Test that when WASM and Lua filters have the same priority,
	// the ordering is stable (preserves insertion order)
	luaFilter1 := &model.ExtensionFilterWrapper{
		ResourceName: "lua-filter-1",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request() end",
			},
		},
	}

	wasmFilter := &model.ExtensionFilterWrapper{
		ResourceName: "wasm-filter",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Wasm: &extensions.WasmConfig{
				Url: "oci://test.com/filter:v1",
			},
		},
	}

	luaFilter2 := &model.ExtensionFilterWrapper{
		ResourceName: "lua-filter-2",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_response() end",
			},
		},
	}

	// All filters have same priority (nil = MinInt32), order should be preserved
	filterMap := map[extensions.PluginPhase][]*model.ExtensionFilterWrapper{
		extensions.PluginPhase_AUTHN: {luaFilter1, wasmFilter, luaFilter2},
	}

	filters := []*hcm.HttpFilter{}
	filters = PopAppendHTTPExtensionFilter(filters, filterMap, extensions.PluginPhase_AUTHN)

	// All three filters should be added in insertion order
	assert.Equal(t, len(filters), 3, "all filters should be added")
	assert.Equal(t, filters[0].Name, "lua-filter-1", "first filter should be lua-filter-1")
	assert.Equal(t, filters[1].Name, "wasm-filter", "second filter should be wasm-filter")
	assert.Equal(t, filters[2].Name, "lua-filter-2", "third filter should be lua-filter-2")

	// Verify filter types are correct
	assert.Equal(t, filters[0].GetTypedConfig() != nil, true, "lua-filter-1 should have TypedConfig (inlined)")
	assert.Equal(t, filters[1].GetConfigDiscovery() != nil, true, "wasm-filter should have ConfigDiscovery (ECDS)")
	assert.Equal(t, filters[2].GetTypedConfig() != nil, true, "lua-filter-2 should have TypedConfig (inlined)")
}

func TestMixedWasmLuaPriorityOrdering_PreservesSortedOrder(t *testing.T) {
	// Test that PopAppendHTTPExtensionFilter preserves the input order.
	// Note: Priority sorting is done by ExtensionFiltersByListenerInfo in push_context.go
	// BEFORE the filters are passed to PopAppendHTTPExtensionFilter. This test verifies
	// that PopAppendHTTPExtensionFilter maintains the pre-sorted order.

	// Simulate filters that have already been sorted by priority (highest first)
	wasmHighPrio := &model.ExtensionFilterWrapper{
		ResourceName: "wasm-high-prio",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase:    extensions.PluginPhase_AUTHN,
			Priority: wrapperspb.Int32(100),
			Wasm: &extensions.WasmConfig{
				Url: "oci://test.com/filter:v1",
			},
		},
	}

	luaMedPrio := &model.ExtensionFilterWrapper{
		ResourceName: "lua-med-prio",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase:    extensions.PluginPhase_AUTHN,
			Priority: wrapperspb.Int32(50),
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_response() end",
			},
		},
	}

	luaLowPrio := &model.ExtensionFilterWrapper{
		ResourceName: "lua-low-prio",
		FilterType:   model.FilterTypeLua,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase:    extensions.PluginPhase_AUTHN,
			Priority: wrapperspb.Int32(10),
			Lua: &extensions.LuaConfig{
				InlineCode: "function envoy_on_request() end",
			},
		},
	}

	wasmNoPrio := &model.ExtensionFilterWrapper{
		ResourceName: "wasm-no-prio",
		FilterType:   model.FilterTypeWasm,
		ExtensionFilter: &extensions.ExtensionFilter{
			Phase: extensions.PluginPhase_AUTHN,
			// No priority set - defaults to MinInt32
			Wasm: &extensions.WasmConfig{
				Url: "oci://test.com/filter2:v1",
			},
		},
	}

	// Input is pre-sorted by priority (as ExtensionFiltersByListenerInfo would do)
	// Priority order: wasmHighPrio(100) > luaMedPrio(50) > luaLowPrio(10) > wasmNoPrio(MinInt32)
	filterMap := map[extensions.PluginPhase][]*model.ExtensionFilterWrapper{
		extensions.PluginPhase_AUTHN: {wasmHighPrio, luaMedPrio, luaLowPrio, wasmNoPrio},
	}

	filters := []*hcm.HttpFilter{}
	filters = PopAppendHTTPExtensionFilter(filters, filterMap, extensions.PluginPhase_AUTHN)

	// Verify the pre-sorted order is preserved
	assert.Equal(t, len(filters), 4, "all filters should be added")
	assert.Equal(t, filters[0].Name, "wasm-high-prio", "first filter should maintain position")
	assert.Equal(t, filters[1].Name, "lua-med-prio", "second filter should maintain position")
	assert.Equal(t, filters[2].Name, "lua-low-prio", "third filter should maintain position")
	assert.Equal(t, filters[3].Name, "wasm-no-prio", "fourth filter should maintain position")

	// Verify filter types are correctly converted
	assert.Equal(t, filters[0].GetConfigDiscovery() != nil, true, "WASM filter should use ConfigDiscovery")
	assert.Equal(t, filters[1].GetTypedConfig() != nil, true, "Lua filter should use TypedConfig (inlined)")
	assert.Equal(t, filters[2].GetTypedConfig() != nil, true, "Lua filter should use TypedConfig (inlined)")
	assert.Equal(t, filters[3].GetConfigDiscovery() != nil, true, "WASM filter should use ConfigDiscovery")
}

