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

	envoyCoreV3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyWasmFilterV3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	envoyExtensionsWasmV3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

const (
	defaultRuntime = "envoy.wasm.runtime.v8"
	fileScheme     = "file"
	ociScheme      = "oci"
)

type WasmPluginWrapper struct {
	extensions.WasmPlugin

	Name      string
	Namespace string

	ExtensionConfiguration *envoyCoreV3.TypedExtensionConfig
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

	u, err := url.Parse(wasmPlugin.Url)
	if err != nil {
		log.Warnf("wasmplugin %v/%v discarded due to failure to parse URL: %s", plugin.Namespace, plugin.Name, err)
		return nil
	}
	// when no scheme is given, default to oci://
	if u.Scheme == "" {
		u.Scheme = ociScheme
	}

	datasource := buildDataSource(u, wasmPlugin)
	typedConfig, err := anypb.New(&envoyWasmFilterV3.Wasm{
		Config: &envoyExtensionsWasmV3.PluginConfig{
			Name:          plugin.Namespace + "." + plugin.Name,
			RootId:        wasmPlugin.PluginName,
			Configuration: cfg,
			Vm:            buildVMConfig(datasource, wasmPlugin.VmConfig),
		},
	})
	if err != nil {
		log.Warnf("WasmPlugin %s/%s failed to marshal to TypedExtensionConfig: %s", plugin.Namespace, plugin.Name, err)
		return nil
	}
	ec := &envoyCoreV3.TypedExtensionConfig{
		Name:        plugin.Namespace + "." + plugin.Name,
		TypedConfig: typedConfig,
	}
	return &WasmPluginWrapper{
		Name:                   plugin.Name,
		Namespace:              plugin.Namespace,
		WasmPlugin:             *wasmPlugin,
		ExtensionConfiguration: ec,
	}
}

func buildDataSource(u *url.URL, wasmPlugin *extensions.WasmPlugin) *envoyCoreV3.AsyncDataSource {
	if u.Scheme == fileScheme {
		return &envoyCoreV3.AsyncDataSource{
			Specifier: &envoyCoreV3.AsyncDataSource_Local{
				Local: &envoyCoreV3.DataSource{
					Specifier: &envoyCoreV3.DataSource_Filename{
						Filename: strings.TrimPrefix(wasmPlugin.Url, "file://"),
					},
				},
			},
		}
	}

	return &envoyCoreV3.AsyncDataSource{
		Specifier: &envoyCoreV3.AsyncDataSource_Remote{
			Remote: &envoyCoreV3.RemoteDataSource{
				HttpUri: &envoyCoreV3.HttpUri{
					Uri:     u.String(),
					Timeout: durationpb.New(30 * time.Second),
					HttpUpstreamType: &envoyCoreV3.HttpUri_Cluster{
						// this will be fetched by the agent anyway, so no need for a cluster
						Cluster: "_",
					},
				},
				Sha256: wasmPlugin.Sha256,
			},
		},
	}
}

func buildVMConfig(datasource *envoyCoreV3.AsyncDataSource, vm *extensions.VmConfig) *envoyExtensionsWasmV3.PluginConfig_VmConfig {
	cfg := &envoyExtensionsWasmV3.PluginConfig_VmConfig{
		VmConfig: &envoyExtensionsWasmV3.VmConfig{
			Runtime: defaultRuntime,
			Code:    datasource,
		},
	}

	if vm != nil && len(vm.Env) != 0 {
		hostEnvKeys := make([]string, 0, len(vm.Env))
		inlineEnvs := make(map[string]string, 0)
		for _, e := range vm.Env {
			switch e.ValueFrom {
			case extensions.EnvValueSource_INLINE:
				inlineEnvs[e.Name] = e.Value
			case extensions.EnvValueSource_HOST:
				hostEnvKeys = append(hostEnvKeys, e.Name)
			}
		}
		cfg.VmConfig.EnvironmentVariables = &envoyExtensionsWasmV3.EnvironmentVariables{
			HostEnvKeys: hostEnvKeys,
			KeyValues:   inlineEnvs,
		}
	}

	return cfg
}
