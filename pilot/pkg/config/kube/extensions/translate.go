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

package extensions

import (
	"maps"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

// syntheticMarker is appended to the name of ExtensionFilter resources that are
// synthesized from WasmPlugin resources. The tilde character (~) is not a valid
// Kubernetes name character, preventing collisions with real ExtensionFilter objects.
const syntheticMarker = "~istio-translated-wasmplugin"

// translateWasmPlugin converts a WasmPlugin config.Config into a synthetic ExtensionFilter
// config.Config. Returns nil if the input is not a valid WasmPlugin.
func translateWasmPlugin(cfg config.Config) *config.Config {
	wp, ok := cfg.Spec.(*extensions.WasmPlugin)
	if !ok {
		return nil
	}

	ef := &extensions.ExtensionFilter{
		Selector:   wp.Selector,
		TargetRef:  wp.TargetRef,
		TargetRefs: wp.TargetRefs,
		Phase:      wp.Phase,
		Priority:   wp.Priority,
		Match:      convertTrafficSelectors(wp.Match),
		Wasm: &extensions.WasmConfig{
			Url:             wp.Url,
			Sha256:          wp.Sha256,
			ImagePullPolicy: wp.ImagePullPolicy,
			ImagePullSecret: wp.ImagePullSecret,
			PluginConfig:    wp.PluginConfig,
			PluginName:      wp.PluginName,
			FailStrategy:    wp.FailStrategy,
			VmConfig:        wp.VmConfig,
			Type:            wp.Type,
		},
	}

	annotations := make(map[string]string, len(cfg.Annotations)+1)
	maps.Copy(annotations, cfg.Annotations)
	annotations["istio.io/translated-from"] = "WasmPlugin"

	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  gvk.ExtensionFilter,
			Name:              cfg.Name + syntheticMarker,
			Namespace:         cfg.Namespace,
			ResourceVersion:   cfg.ResourceVersion,
			CreationTimestamp: cfg.CreationTimestamp,
			Annotations:       annotations,
		},
		Spec: ef,
	}
}

// convertTrafficSelectors maps []*extensions.WasmPlugin_TrafficSelector to
// []*extensions.TrafficSelector. Both types have identical Mode and Ports fields.
func convertTrafficSelectors(in []*extensions.WasmPlugin_TrafficSelector) []*extensions.TrafficSelector {
	if len(in) == 0 {
		return nil
	}
	out := make([]*extensions.TrafficSelector, 0, len(in))
	for _, ts := range in {
		if ts == nil {
			continue
		}
		out = append(out, &extensions.TrafficSelector{
			Mode:  ts.Mode,
			Ports: ts.Ports,
		})
	}
	return out
}
