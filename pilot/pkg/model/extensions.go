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
package model

import (
	"strings"
	"time"

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_extensions_filters_http_wasm_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	envoy_extensions_wasm_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/util/gogo"
)

const (
	defaultRuntime = "envoy.wasm.runtime.v8"
)

type WasmPluginWrapper struct {
	extensions.WasmPlugin

	Name      string
	Namespace string

	ExtensionConfiguration *envoy_extensions_filters_http_wasm_v3.Wasm
}

func convertToWasmPluginWrapper(plugin *config.Config) *WasmPluginWrapper {
	if wasmPlugin, ok := plugin.Spec.(*extensions.WasmPlugin); ok {
		cfg := &any.Any{}
		if wasmPlugin.PluginConfig != nil && len(wasmPlugin.PluginConfig.Fields) > 0 {
			cfgJSON, _ := protojson.Marshal(proto.MessageV2(wasmPlugin.PluginConfig))
			cfg = gogo.MessageToAny(&types.StringValue{
				Value: string(cfgJSON),
			})
		}
		var datasource *envoy_config_core_v3.AsyncDataSource
		if strings.HasPrefix(wasmPlugin.Url, "http") {
			// SHA256 is required for .wasm deployments
			if wasmPlugin.XSha256 == nil {
				return nil
			}
			datasource = &envoy_config_core_v3.AsyncDataSource{
				Specifier: &envoy_config_core_v3.AsyncDataSource_Remote{
					Remote: &envoy_config_core_v3.RemoteDataSource{
						HttpUri: &envoy_config_core_v3.HttpUri{
							Uri:     wasmPlugin.Url,
							Timeout: durationpb.New(30 * time.Second),
							HttpUpstreamType: &envoy_config_core_v3.HttpUri_Cluster{
								// this will be fetched by the agent anyway, so no need for a cluster
								Cluster: "_",
							},
						},
						Sha256: wasmPlugin.XSha256.(*extensions.WasmPlugin_Sha256).Sha256,
					},
				},
			}
		} else {
			datasource = &envoy_config_core_v3.AsyncDataSource{
				Specifier: &envoy_config_core_v3.AsyncDataSource_Local{
					Local: &envoy_config_core_v3.DataSource{
						Specifier: &envoy_config_core_v3.DataSource_Filename{
							Filename: wasmPlugin.Url,
						},
					},
				},
			}
		}
		return &WasmPluginWrapper{
			Name:       plugin.Name,
			Namespace:  plugin.Namespace,
			WasmPlugin: *wasmPlugin,
			ExtensionConfiguration: &envoy_extensions_filters_http_wasm_v3.Wasm{
				Config: &envoy_extensions_wasm_v3.PluginConfig{
					Name:          plugin.Name,
					RootId:        wasmPlugin.PluginName,
					Configuration: cfg,
					Vm: &envoy_extensions_wasm_v3.PluginConfig_VmConfig{
						VmConfig: &envoy_extensions_wasm_v3.VmConfig{
							Runtime: defaultRuntime,
							Code:    datasource,
						},
					},
				},
			},
		}
	}
	return nil
}
