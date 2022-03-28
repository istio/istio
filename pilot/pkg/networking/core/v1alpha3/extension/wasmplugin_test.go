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

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extensionsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	securitymodel "istio.io/istio/pilot/pkg/security/model"
)

var (
	istioJWT = &http_conn.HttpFilter{
		Name: securitymodel.EnvoyJwtFilterName,
	}
	istioAuthN = &http_conn.HttpFilter{
		Name: securitymodel.AuthnFilterName,
	}
	istioAuthZ = &http_conn.HttpFilter{
		Name: wellknown.HTTPRoleBasedAccessControl,
	}
	istioStats = &http_conn.HttpFilter{
		Name: "istio.stats",
	}
	unknown = &http_conn.HttpFilter{
		Name: "unknown.filter",
	}
	someAuthNFilter = &model.WasmPluginWrapper{
		Name:         "someAuthNFilter",
		Namespace:    "istio-system",
		ResourceName: "istio-system.someAuthNFilter",
		WasmPlugin: &extensions.WasmPlugin{
			Priority: &wrappers.Int64Value{Value: 1},
		},
	}
	someImportantAuthNFilter = &model.WasmPluginWrapper{
		Name:         "someImportantAuthNFilter",
		Namespace:    "istio-system",
		ResourceName: "istio-system.someImportantAuthNFilter",
		WasmPlugin: &extensions.WasmPlugin{
			Priority: &wrappers.Int64Value{Value: 1000},
		},
	}
	someAuthZFilter = &model.WasmPluginWrapper{
		Name:         "someAuthZFilter",
		Namespace:    "istio-system",
		ResourceName: "istio-system.someAuthZFilter",
		WasmPlugin: &extensions.WasmPlugin{
			Priority: &wrappers.Int64Value{Value: 1000},
		},
	}
)

func TestAddWasmPluginsToMutableObjects(t *testing.T) {
	testCases := []struct {
		name           string
		filterChains   []networking.FilterChain
		extensions     map[extensions.PluginPhase][]*model.WasmPluginWrapper
		expectedResult []networking.FilterChain
	}{
		{
			name:           "empty",
			filterChains:   []networking.FilterChain{},
			extensions:     map[extensions.PluginPhase][]*model.WasmPluginWrapper{},
			expectedResult: []networking.FilterChain{},
		},
		{
			name: "ignore tcp",
			filterChains: []networking.FilterChain{
				{
					ListenerProtocol: networking.ListenerProtocolTCP,
				},
			},
			extensions: map[extensions.PluginPhase][]*model.WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					someImportantAuthNFilter,
					someAuthNFilter,
				},
			},
			expectedResult: []networking.FilterChain{
				{
					ListenerProtocol: networking.ListenerProtocolTCP,
				},
			},
		},
		{
			name: "authN",
			filterChains: []networking.FilterChain{
				{
					ListenerProtocol: networking.ListenerProtocolHTTP,
					HTTP: []*http_conn.HttpFilter{
						istioJWT,
						istioAuthN,
						istioStats,
					},
				},
			},
			extensions: map[extensions.PluginPhase][]*model.WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					someImportantAuthNFilter,
					someAuthNFilter,
				},
			},
			expectedResult: []networking.FilterChain{
				{
					ListenerProtocol: networking.ListenerProtocolHTTP,
					HTTP: []*http_conn.HttpFilter{
						toEnvoyHTTPFilter(someImportantAuthNFilter),
						toEnvoyHTTPFilter(someAuthNFilter),
						istioJWT,
						istioAuthN,
						istioStats,
					},
				},
			},
		},
		{
			name: "authZ",
			filterChains: []networking.FilterChain{
				{
					ListenerProtocol: networking.ListenerProtocolHTTP,
					HTTP: []*http_conn.HttpFilter{
						istioAuthN,
						unknown,
						istioAuthZ,
						istioStats,
					},
				},
			},
			extensions: map[extensions.PluginPhase][]*model.WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					someImportantAuthNFilter,
					someAuthNFilter,
				},
				extensions.PluginPhase_AUTHZ: {
					someAuthZFilter,
				},
			},
			expectedResult: []networking.FilterChain{
				{
					ListenerProtocol: networking.ListenerProtocolHTTP,
					HTTP: []*http_conn.HttpFilter{
						toEnvoyHTTPFilter(someImportantAuthNFilter),
						toEnvoyHTTPFilter(someAuthNFilter),
						istioAuthN,
						unknown,
						toEnvoyHTTPFilter(someAuthZFilter),
						istioAuthZ,
						istioStats,
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mut := &networking.MutableObjects{
				FilterChains: tc.filterChains,
			}
			AddWasmPluginsToMutableObjects(mut, tc.extensions)
			if diff := cmp.Diff(tc.expectedResult, mut.FilterChains, protocmp.Transform()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestInsertedExtensionConfigurations(t *testing.T) {
	wasm, _ := anypb.New(&extensionsv3.Wasm{})
	testCases := []struct {
		name        string
		wasmPlugins map[extensions.PluginPhase][]*model.WasmPluginWrapper
		names       []string
		expectedECs []*envoy_config_core_v3.TypedExtensionConfig
	}{
		{
			name:        "empty",
			wasmPlugins: map[extensions.PluginPhase][]*model.WasmPluginWrapper{},
			names:       []string{someAuthNFilter.Name},
			expectedECs: []*envoy_config_core_v3.TypedExtensionConfig{},
		},
		{
			name: "authn",
			wasmPlugins: map[extensions.PluginPhase][]*model.WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					someAuthNFilter,
					someAuthZFilter,
				},
			},
			names: []string{someAuthNFilter.Namespace + "." + someAuthNFilter.Name},
			expectedECs: []*envoy_config_core_v3.TypedExtensionConfig{
				{
					Name:        "istio-system.someAuthNFilter",
					TypedConfig: wasm,
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ecs := InsertedExtensionConfigurations(tc.wasmPlugins, tc.names, nil)
			if diff := cmp.Diff(tc.expectedECs, ecs, protocmp.Transform()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
