// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package telemetry

import (
	"fmt"

	"istio.io/api/mesh/v1alpha1"
	telemetryapi "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/sets"
)

type LightstepAnalyzer struct{}

var _ analysis.Analyzer = &LightstepAnalyzer{}

// Metadata implements Analyzer
func (a *LightstepAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "telemetry.LightstepAnalyzer",
		Description: "Validates that lightstep provider is still used",
		Inputs: []config.GroupVersionKind{
			gvk.Telemetry,
			gvk.MeshConfig,
		},
	}
}

// Analyze implements Analyzer
func (a *LightstepAnalyzer) Analyze(c analysis.Context) {
	meshConfig := fetchMeshConfig(c)
	providerNames := sets.New[string]()
	for _, prov := range meshConfig.ExtensionProviders {
		switch prov.Provider.(type) {
		case *v1alpha1.MeshConfig_ExtensionProvider_Lightstep:
			providerNames.Insert(prov.Name)
		}
	}
	if len(providerNames) == 0 {
		return
	}

	c.ForEach(gvk.Telemetry, func(r *resource.Instance) bool {
		telemetry := r.Message.(*telemetryapi.Telemetry)
		for _, tracing := range telemetry.Tracing {
			for _, p := range tracing.Providers {
				if providerNames.Contains(p.Name) {
					c.Report(gvk.Telemetry,
						msg.NewDeprecated(r, fmt.Sprintf("The Lightstep provider %s is deprecated, please migrate to OpenTelemetry provider.", p.Name)))
				}
			}
		}

		return true
	})
}
