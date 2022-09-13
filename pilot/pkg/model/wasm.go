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
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"

	"istio.io/istio/pilot/pkg/features"
)

// ConstructVMConfig constructs a VM config. If WASM is enabled, the wasm plugin at filename will be used.
// If not, the builtin (null vm) extension, name, will be used.
func ConstructVMConfig(filename, name string) *wasm.PluginConfig_VmConfig {
	var vmConfig *wasm.PluginConfig_VmConfig
	if features.EnableWasmTelemetry && filename != "" {
		vmConfig = &wasm.PluginConfig_VmConfig{
			VmConfig: &wasm.VmConfig{
				Runtime:          "envoy.wasm.runtime.v8",
				AllowPrecompiled: true,
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: filename,
						},
					},
				}},
			},
		}
	} else {
		vmConfig = &wasm.PluginConfig_VmConfig{
			VmConfig: &wasm.VmConfig{
				Runtime: "envoy.wasm.runtime.null",
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: name,
						},
					},
				}},
			},
		}
	}
	return vmConfig
}
