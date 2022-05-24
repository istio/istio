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
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	defaultRuntime = "envoy.wasm.runtime.v8"
	fileScheme     = "file"
	ociScheme      = "oci"

	// name of environment variable at Wasm VM, which will carry the Wasm image pull secret.
	WasmSecretEnv = "ISTIO_META_WASM_IMAGE_PULL_SECRET"
	// name of environment variable at Wasm VM, which will carry the Wasm image pull policy.
	WasmPolicyEnv = "ISTIO_META_WASM_IMAGE_PULL_POLICY"
	// name of environment variable at Wasm VM, which will carry the resource version of WasmPlugin.
	WasmResourceVersionEnv = "ISTIO_META_WASM_PLUGIN_RESOURCE_VERSION"
)

type WasmPluginWrapper struct {
	*extensions.WasmPlugin

	Name         string
	Namespace    string
	ResourceName string

	WasmExtensionConfig *envoyWasmFilterV3.Wasm
}

func convertToWasmPluginWrapper(originPlugin config.Config) *WasmPluginWrapper {
	var ok bool
	// Make a deep copy since we are going to mutate the resource later for secret env variable.
	// We do not want to mutate the underlying resource at informer cache.
	plugin := originPlugin.DeepCopy()
	var wasmPlugin *extensions.WasmPlugin
	if wasmPlugin, ok = plugin.Spec.(*extensions.WasmPlugin); !ok {
		return nil
	}

	cfg := &anypb.Any{}
	if wasmPlugin.PluginConfig != nil && len(wasmPlugin.PluginConfig.Fields) > 0 {
		cfgJSON, err := protomarshal.ToJSON(wasmPlugin.PluginConfig)
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
	// Normalize the image pull secret to the full resource name.
	wasmPlugin.ImagePullSecret = toSecretResourceName(wasmPlugin.ImagePullSecret, plugin.Namespace)
	datasource := buildDataSource(u, wasmPlugin)
	wasmExtensionConfig := &envoyWasmFilterV3.Wasm{
		Config: &envoyExtensionsWasmV3.PluginConfig{
			Name:          plugin.Namespace + "." + plugin.Name,
			RootId:        wasmPlugin.PluginName,
			Configuration: cfg,
			Vm:            buildVMConfig(datasource, plugin.ResourceVersion, wasmPlugin),
		},
	}
	if err != nil {
		log.Warnf("WasmPlugin %s/%s failed to marshal to TypedExtensionConfig: %s", plugin.Namespace, plugin.Name, err)
		return nil
	}
	return &WasmPluginWrapper{
		Name:                plugin.Name,
		Namespace:           plugin.Namespace,
		ResourceName:        plugin.Namespace + "." + plugin.Name,
		WasmPlugin:          wasmPlugin,
		WasmExtensionConfig: wasmExtensionConfig,
	}
}

// toSecretResourceName converts a imagePullSecret to a resource name referenced at Wasm SDS.
// NOTE: the secret referenced by WasmPlugin has to be in the same namespace as the WasmPlugin,
// so this function makes sure that the secret resource name, which will be used to retrieve secret at
// xds generation time, has the same namespace as the WasmPlugin.
func toSecretResourceName(name, pluginNamespace string) string {
	if name == "" {
		return ""
	}
	// Convert user provided secret name to secret resource name.
	rn := credentials.ToResourceName(name)
	// Parse the secret resource name.
	sr, err := credentials.ParseResourceName(rn, pluginNamespace, "", "")
	if err != nil {
		log.Debugf("Failed to parse wasm secret resource name %v", err)
		return ""
	}
	// Forcely rewrite secret namespace to plugin namespace, since we require secret resource
	// referenced by WasmPlugin co-located with WasmPlugin in the same namespace.
	sr.Namespace = pluginNamespace
	return sr.KubernetesResourceName()
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

func buildVMConfig(
	datasource *envoyCoreV3.AsyncDataSource,
	resourceVersion string,
	wasmPlugin *extensions.WasmPlugin,
) *envoyExtensionsWasmV3.PluginConfig_VmConfig {
	cfg := &envoyExtensionsWasmV3.PluginConfig_VmConfig{
		VmConfig: &envoyExtensionsWasmV3.VmConfig{
			Runtime: defaultRuntime,
			Code:    datasource,
			EnvironmentVariables: &envoyExtensionsWasmV3.EnvironmentVariables{
				KeyValues: map[string]string{},
			},
		},
	}

	if wasmPlugin.ImagePullSecret != "" {
		cfg.VmConfig.EnvironmentVariables.KeyValues[WasmSecretEnv] = wasmPlugin.ImagePullSecret
	}

	if wasmPlugin.ImagePullPolicy != extensions.PullPolicy_UNSPECIFIED_POLICY {
		cfg.VmConfig.EnvironmentVariables.KeyValues[WasmPolicyEnv] = wasmPlugin.ImagePullPolicy.String()
	}

	cfg.VmConfig.EnvironmentVariables.KeyValues[WasmResourceVersionEnv] = resourceVersion

	vm := wasmPlugin.VmConfig
	if vm != nil && len(vm.Env) != 0 {
		hostEnvKeys := make([]string, 0, len(vm.Env))
		for _, e := range vm.Env {
			switch e.ValueFrom {
			case extensions.EnvValueSource_INLINE:
				cfg.VmConfig.EnvironmentVariables.KeyValues[e.Name] = e.Value
			case extensions.EnvValueSource_HOST:
				hostEnvKeys = append(hostEnvKeys, e.Name)
			}
		}
		cfg.VmConfig.EnvironmentVariables.HostEnvKeys = hostEnvKeys
	}

	return cfg
}
