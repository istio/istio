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

package core

import (
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/extension"
	"istio.io/istio/pkg/log"
)

// BuildExtensionConfiguration returns the list of extension configuration for the given proxy and list of names.
// This is the ECDS output.
func (configgen *ConfigGeneratorImpl) BuildExtensionConfiguration(
	proxy *model.Proxy, push *model.PushContext, extensionConfigNames []string, pullSecrets map[string][]byte,
) []*core.TypedExtensionConfig {
	envoyFilterPatches := push.EnvoyFilters(proxy)
	extensions := envoyfilter.InsertedExtensionConfigurations(envoyFilterPatches, extensionConfigNames)
	wasmPlugins := push.WasmPluginsByName(proxy, parseExtensionName(extensionConfigNames))
	extensions = append(extensions, extension.InsertedExtensionConfigurations(proxy, wasmPlugins, extensionConfigNames, pullSecrets)...)
	return extensions
}

func parseExtensionName(names []string) []types.NamespacedName {
	res := make([]types.NamespacedName, 0, len(names))
	for _, n := range names {
		if !strings.HasPrefix(n, model.WasmPluginResourceNamePrefix) {
			log.Debugf("ignoring unknown ECDS: %v", n)
			continue
		}
		ns, name, ok := strings.Cut(n[len(model.WasmPluginResourceNamePrefix):], ".")
		if !ok {
			log.Debugf("ignoring unknown ECDS: %v", n)
			continue
		}
		res = append(res, types.NamespacedName{Namespace: ns, Name: name})
	}
	return res
}
