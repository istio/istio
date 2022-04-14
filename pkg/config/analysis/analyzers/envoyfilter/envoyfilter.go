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
	patchFilterNames := make([]string, 1)
	c.ForEach(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), func(r *resource.Instance) bool {
		names := s.analyzeEnvoyFilterPatch(r, c, patchFilterNames)
		patchFilterNames = names
		return true
	})
}

func (*EnvoyPatchAnalyzer) analyzeEnvoyFilterPatch(r *resource.Instance, c analysis.Context, patchFilterNames []string) []string {
	ef := r.Message.(*network.EnvoyFilter)
	for index, patch := range ef.ConfigPatches {
		// collect filter names to figure out if the order is correct
		tmpValue := patch.Patch.Value.GetFields()
		instanceName := tmpValue["name"]

		// check each operation type
		if patch.Patch.Operation == network.EnvoyFilter_Patch_ADD {
			// the ADD operation is an absolute operation but provide a warning
			// indicating that the operation will be ignored when applyTo is set to ROUTE_CONFIGURATION,
			// or HTTP_ROUTE
			if patch.ApplyTo == network.EnvoyFilter_ROUTE_CONFIGURATION || patch.ApplyTo == network.EnvoyFilter_HTTP_ROUTE {
				// provide an error message indicating a mismatch between the operation type and the filter type
				message := msg.NewEnvoyFilterUsesAddOperationIncorrectly(r)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
					message.Line = line
				}

				c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)
			}
		} else if patch.Patch.Operation == network.EnvoyFilter_Patch_REMOVE {
			// the REMOVE operation is ignored when applyTo is set to ROUTE_CONFIGURATION, or HTTP_ROUTE.
			if patch.ApplyTo == network.EnvoyFilter_ROUTE_CONFIGURATION || patch.ApplyTo == network.EnvoyFilter_HTTP_ROUTE {
				// provide an error message indicating a mismatch between the operation type and the filter type
				message := msg.NewEnvoyFilterUsesRemoveOperationIncorrectly(r)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
					message.Line = line
				}

				c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)
			} else if ef.Priority == 0 {
				// Provide a warning indicating that no priority was set and a relative operation was used
				message := msg.NewEnvoyFilterUsesRelativeOperation(r)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
					message.Line = line
				}

				c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)
			}
		} else if patch.Patch.Operation == network.EnvoyFilter_Patch_REPLACE {
			// the REPLACE operation is only valid for HTTP_FILTER and NETWORK_FILTER.
			if patch.ApplyTo != network.EnvoyFilter_NETWORK_FILTER && patch.ApplyTo != network.EnvoyFilter_HTTP_FILTER {
				// provide an error message indicating an invalid filter type
				message := msg.NewEnvoyFilterUsesReplaceOperationIncorrectly(r)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
					message.Line = line
				}

				c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)
			} else {
				count := 0
				for _, name := range patchFilterNames {
					if instanceName.String() == name {
						count++
						break
					}
				}
				if count > 0 && ef.Priority == 0 {
					// there is more than one envoy filter using the same name with the REPLACE operation
					// provide a warning indicating that no priority was set and a relative operation was used
					message := msg.NewEnvoyFilterUsesRelativeOperation(r)

					if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
						message.Line = line
					}
					c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)
				}
			}
		} else if ef.Priority == 0 {
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
			} else if patch.Patch.Operation == network.EnvoyFilter_Patch_MERGE {
				count := 0
				for _, name := range patchFilterNames {
					if instanceName.String() == name {
						count++
						break
					}
				}
				if count > 0 && ef.Priority == 0 {
					// there is more than one envoy filter using the same name with the REPLACE operation
					// provide a warning indicating that no priority was set and a relative operation was used

					message := msg.NewEnvoyFilterUsesRelativeOperation(r)

					if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
						message.Line = line
					}

					c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)
				}
			}
		}
		// append the patchValueStr to the slice for next iteration
		patchFilterNames = append(patchFilterNames, instanceName.String())
	}
	return patchFilterNames
}
