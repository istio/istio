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

package v1alpha3

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/extension"
)

// BuildExtensionConfiguration returns the list of extension configuration for the given proxy and list of names.
// This is the ECDS output.
func (configgen *ConfigGeneratorImpl) BuildExtensionConfiguration(
	proxy *model.Proxy, push *model.PushContext, extensionConfigNames []string, pullSecrets map[string][]byte,
) []*core.TypedExtensionConfig {
	envoyFilterPatches := push.EnvoyFilters(proxy)
	extensions := envoyfilter.InsertedExtensionConfigurations(envoyFilterPatches, extensionConfigNames)
	wasmPlugins := push.WasmPlugins(proxy)
	extensions = append(extensions, extension.InsertedExtensionConfigurations(wasmPlugins, extensionConfigNames, pullSecrets)...)
	return extensions
}
