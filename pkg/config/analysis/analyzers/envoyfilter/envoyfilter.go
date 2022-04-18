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

func relativeOperationMsg(r *resource.Instance, c analysis.Context, index int, priority int32) {
	if priority == 0 {
		message := msg.NewEnvoyFilterUsesRelativeOperation(r)

		if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, index)); ok {
			message.Line = line
		}
		c.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), message)

	}
}

func (*EnvoyPatchAnalyzer) analyzeEnvoyFilterPatch(r *resource.Instance, c analysis.Context) {
	ef := r.Message.(*network.EnvoyFilter)
	for index, patch := range ef.ConfigPatches {
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
			} else {
				// A relative operation (REMOVE) was used so check if priority is set and if not set provide a warning
				relativeOperationMsg(r, c, index, ef.Priority)
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
				// A relative operation (REPLACE) was used so check if priority is set and if not set provide a warning
				relativeOperationMsg(r, c, index, ef.Priority)
			}
		} else if patch.Patch.Operation == network.EnvoyFilter_Patch_INSERT_BEFORE || patch.Patch.Operation == network.EnvoyFilter_Patch_INSERT_AFTER {
			// Also a relative operation (INSERT_BEFORE or INSERT_AFTER) was used so check if priority is set and if not set provide a warning
			relativeOperationMsg(r, c, index, ef.Priority)
		} else if patch.Patch.Operation == network.EnvoyFilter_Patch_MERGE {
			// A relative operation (MERGE) was used so check if priority is set and if not set provide a warning
			relativeOperationMsg(r, c, index, ef.Priority)
		}
		// append the patchValueStr to the slice for next iteration
	}
}
