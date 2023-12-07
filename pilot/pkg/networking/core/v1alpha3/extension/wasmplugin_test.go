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
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	networkwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	wasmextension "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
)

var (
	someAuthNFilter = &model.WasmPluginWrapper{
		Name:         "someAuthNFilter",
		Namespace:    "istio-system",
		ResourceName: "istio-system.someAuthNFilter",
		WasmPlugin: &extensions.WasmPlugin{
			Priority: &wrappers.Int32Value{Value: 1},
		},
	}
	someAuthZFilter = &model.WasmPluginWrapper{
		Name:         "someAuthZFilter",
		Namespace:    "istio-system",
		ResourceName: "istio-system.someAuthZFilter",
		WasmPlugin: &extensions.WasmPlugin{
			Priority: &wrappers.Int32Value{Value: 1000},
		},
	}
	someNetworkFilter = &model.WasmPluginWrapper{
		Name:         "someNetworkFilter",
		Namespace:    "istio-system",
		ResourceName: "istio-system.someNetworkFilter",
		WasmPlugin: &extensions.WasmPlugin{
			Priority: &wrappers.Int32Value{Value: 1000},
			Type:     extensions.PluginType_NETWORK,
		},
	}
)

func TestInsertedExtensionConfigurations(t *testing.T) {
	httpFilter := protoconv.MessageToAny(&httpwasm.Wasm{
		Config: &wasmextension.PluginConfig{
			Name:          "istio-system.someAuthNFilter",
			Configuration: &anypb.Any{},
			Vm: &wasmextension.PluginConfig_VmConfig{
				VmConfig: &wasmextension.VmConfig{
					Runtime: "envoy.wasm.runtime.v8",
					Code: &core.AsyncDataSource{
						Specifier: &core.AsyncDataSource_Remote{
							Remote: &core.RemoteDataSource{
								HttpUri: &core.HttpUri{
									Uri: "oci:",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "_",
									},
									Timeout: &durationpb.Duration{
										Seconds: 30,
									},
								},
							},
						},
					},
					EnvironmentVariables: &wasmextension.EnvironmentVariables{
						KeyValues: map[string]string{
							"ISTIO_META_WASM_PLUGIN_RESOURCE_VERSION": "",
						},
					},
				},
			},
		},
	})
	networkFilter := protoconv.MessageToAny(&networkwasm.Wasm{
		Config: &wasmextension.PluginConfig{
			Name:          "istio-system.someNetworkFilter",
			Configuration: &anypb.Any{},
			Vm: &wasmextension.PluginConfig_VmConfig{
				VmConfig: &wasmextension.VmConfig{
					Runtime: "envoy.wasm.runtime.v8",
					Code: &core.AsyncDataSource{
						Specifier: &core.AsyncDataSource_Remote{
							Remote: &core.RemoteDataSource{
								HttpUri: &core.HttpUri{
									Uri: "oci:",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "_",
									},
									Timeout: &durationpb.Duration{
										Seconds: 30,
									},
								},
							},
						},
					},
					EnvironmentVariables: &wasmextension.EnvironmentVariables{
						KeyValues: map[string]string{
							"ISTIO_META_WASM_PLUGIN_RESOURCE_VERSION": "",
						},
					},
				},
			},
		},
	})
	testCases := []struct {
		name        string
		wasmPlugins map[extensions.PluginPhase][]*model.WasmPluginWrapper
		names       []string
		expectedECs []*core.TypedExtensionConfig
	}{
		{
			name:        "empty",
			wasmPlugins: map[extensions.PluginPhase][]*model.WasmPluginWrapper{},
			names:       []string{someAuthNFilter.Name},
			expectedECs: []*core.TypedExtensionConfig{},
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
			expectedECs: []*core.TypedExtensionConfig{
				{
					Name:        "istio-system.someAuthNFilter",
					TypedConfig: httpFilter,
				},
			},
		},
		{
			name: "network",
			wasmPlugins: map[extensions.PluginPhase][]*model.WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					someNetworkFilter,
				},
			},
			names: []string{
				someNetworkFilter.Namespace + "." + someNetworkFilter.Name,
			},
			expectedECs: []*core.TypedExtensionConfig{
				{
					Name:        "istio-system.someNetworkFilter",
					TypedConfig: networkFilter,
				},
			},
		},
		{
			name: "combination of http and network",
			wasmPlugins: map[extensions.PluginPhase][]*model.WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					someAuthNFilter,
					someNetworkFilter,
				},
			},
			names: []string{
				someAuthNFilter.Namespace + "." + someAuthNFilter.Name,
				someNetworkFilter.Namespace + "." + someNetworkFilter.Name,
			},
			expectedECs: []*core.TypedExtensionConfig{
				{
					Name:        "istio-system.someAuthNFilter",
					TypedConfig: httpFilter,
				},
				{
					Name:        "istio-system.someNetworkFilter",
					TypedConfig: networkFilter,
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
