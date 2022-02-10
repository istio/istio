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
	"net/url"
	"strings"
	"time"

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_extensions_filters_http_wasm_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	envoy_extensions_wasm_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

const (
	defaultRuntime = "envoy.wasm.runtime.v8"
	WasmSecretEnv  = "ISTIO_META_WASM_IMAGE_PULL_SECRET"
)

type WasmPluginWrapper struct {
	extensions.WasmPlugin

	Name         string
	Namespace    string
	ResourceName string

	WasmExtensionConfig *envoy_extensions_filters_http_wasm_v3.Wasm
}

func convertToWasmPluginWrapper(plugin *config.Config) *WasmPluginWrapper {
	var ok bool
	var wasmPlugin *extensions.WasmPlugin
	if wasmPlugin, ok = plugin.Spec.(*extensions.WasmPlugin); !ok {
		return nil
	}

	cfg := &anypb.Any{}
	if wasmPlugin.PluginConfig != nil && len(wasmPlugin.PluginConfig.Fields) > 0 {
		cfgJSON, err := gogoprotomarshal.ToJSON(wasmPlugin.PluginConfig)
		if err != nil {
			log.Warnf("wasmplugin %v/%v discarded due to json marshaling error: %s", plugin.Namespace, plugin.Name, err)
			return nil
		}
		cfg = networking.MessageToAny(&wrapperspb.StringValue{
			Value: cfgJSON,
		})
	}
	var datasource *envoy_config_core_v3.AsyncDataSource
	u, err := url.Parse(wasmPlugin.Url)
	if err != nil {
		log.Warnf("wasmplugin %v/%v discarded due to failure to parse URL: %s", plugin.Namespace, plugin.Name, err)
		return nil
	}
	// when no scheme is given, default to oci://
	if u.Scheme == "" {
		u.Scheme = "oci"
	}
	if u.Scheme == "file" {
		datasource = &envoy_config_core_v3.AsyncDataSource{
			Specifier: &envoy_config_core_v3.AsyncDataSource_Local{
				Local: &envoy_config_core_v3.DataSource{
					Specifier: &envoy_config_core_v3.DataSource_Filename{
						Filename: strings.TrimPrefix(wasmPlugin.Url, "file://"),
					},
				},
			},
		}
	} else {
		datasource = &envoy_config_core_v3.AsyncDataSource{
			Specifier: &envoy_config_core_v3.AsyncDataSource_Remote{
				Remote: &envoy_config_core_v3.RemoteDataSource{
					HttpUri: &envoy_config_core_v3.HttpUri{
						Uri:     u.String(),
						Timeout: durationpb.New(30 * time.Second),
						HttpUpstreamType: &envoy_config_core_v3.HttpUri_Cluster{
							// this will be fetched by the agent anyway, so no need for a cluster
							Cluster: "_",
						},
					},
					Sha256: wasmPlugin.Sha256,
				},
			},
		}
	}
	// Normalize the image pull secret to the full resource name.
	wasmPlugin.ImagePullSecret = toSecretResourceName(wasmPlugin.ImagePullSecret, plugin.Namespace)
	wasmExtensionConfig := &envoy_extensions_filters_http_wasm_v3.Wasm{
		Config: &envoy_extensions_wasm_v3.PluginConfig{
			Name:          plugin.Namespace + "." + plugin.Name,
			RootId:        wasmPlugin.PluginName,
			Configuration: cfg,
			Vm: &envoy_extensions_wasm_v3.PluginConfig_VmConfig{
				VmConfig: &envoy_extensions_wasm_v3.VmConfig{
					Runtime: defaultRuntime,
					Code:    datasource,
					EnvironmentVariables: &envoy_extensions_wasm_v3.EnvironmentVariables{
						KeyValues: map[string]string{
							WasmSecretEnv: wasmPlugin.ImagePullSecret,
							// TODO: add Wasm image pull policy.
						},
					},
				},
			},
		},
	}
	return &WasmPluginWrapper{
		Name:                plugin.Name,
		Namespace:           plugin.Namespace,
		ResourceName:        plugin.Namespace + "." + plugin.Name,
		WasmPlugin:          *wasmPlugin,
		WasmExtensionConfig: wasmExtensionConfig,
	}
}

// toSecretResourceName converts a imagePullSecret to a resource name referenced at Wasm SDS.
func toSecretResourceName(name, namespace string) string {
	if name == "" {
		return ""
	}
	// If the secret already has type prefix, trim it for now.
	name = strings.TrimPrefix(name, credentials.KubernetesSecretTypeURI)
	// If the secret does not have namespace prefix, concat the secret name to be namespace/name.
	if !strings.HasPrefix(name, namespace+"/") {
		name = namespace + "/" + name
	}
	// Finally, add the kubernetes:// secret type.
	return credentials.KubernetesSecretTypeURI + name
}
