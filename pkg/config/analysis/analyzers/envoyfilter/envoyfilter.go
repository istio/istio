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
	"fmt"

	network "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// EnvoyPatchAnalyzer checks envoyFilters to see if the patch section is okay
type EnvoyPatchAnalyzer struct{}

// (compile-time check that we implement the interface)
var _ analysis.Analyzer = &EnvoyPatchAnalyzer{}

// Metadata implements analysis.Analyzer
func (*EnvoyPatchAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "envoyfilter.EnvoyPatchAnalyzer",
		Description: "Checks an envoyFilters ",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Envoyfilters.Name(),
		},
	}
}

// Analyze implements analysis.Analyzer
func (s *EnvoyPatchAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), func(r *resource.Instance) bool {
		s.analyzeEnvoyFilterPatch(r, c)
		return true
	})
}

func (*EnvoyPatchAnalyzer) analyzeEnvoyFilterPatch(r *resource.Instance, c analysis.Context) {
	ef := r.Message.(*network.EnvoyFilter)

	// if the envoyFilter does not have a priority and it uses the INSERT_BEFORE or INSERT_AFTER
	// then it can have issues if its using an operation that is relative to another filter.
	// the default priority for an envoyFilter is 0
	if ef.Priority == 0 {
		for index, patch := range ef.ConfigPatches {
			if patch.Patch.Operation == network.EnvoyFilter_Patch_INSERT_BEFORE || patch.Patch.Operation == network.EnvoyFilter_Patch_INSERT_AFTER {
				// indicate that this envoy filter does not have a priority and has a relative patch
				// operation set which can cause issues. Indicate a warning to the use to use a
				// priority to ensure its placed after something or use the INSERT_FIRST option to
				//  ensure its always done first
				message := msg.NewEnvoyFilterUsesRelativeOperation(r)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
					message.Line = line
				}

				c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)
			}
		}
	}
}
