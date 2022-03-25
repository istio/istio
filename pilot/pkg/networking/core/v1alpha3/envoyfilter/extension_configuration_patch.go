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

package envoyfilter

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

// InsertedExtensionConfigurations returns extension configurations added via EnvoyFilter.
func InsertedExtensionConfigurations(efw *model.EnvoyFilterWrapper, names []string) []*core.TypedExtensionConfig {
	result := make([]*core.TypedExtensionConfig, 0)
	if efw == nil {
		return result
	}
	hasName := sets.NewWith(names...)
	for _, p := range efw.Patches[networking.EnvoyFilter_EXTENSION_CONFIG] {
		if p.Operation != networking.EnvoyFilter_Patch_ADD {
			continue
		}
		ec, ok := p.Value.(*core.TypedExtensionConfig)
		if !ok {
			log.Errorf("extension config patch %+v does not match TypeExtensionConfig type", p.Value)
			continue
		}
		if hasName.Contains(ec.GetName()) {
			result = append(result, proto.Clone(p.Value).(*core.TypedExtensionConfig))
		}
	}
	return result
}
