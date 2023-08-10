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
	"istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// DefaultSelectorAnalyzer validates, per namespace, that there aren't multiple
// telemetry resources that have no selector. This is distinct from
// SelectorAnalyzer because it does not require pods, so it can run even if that
// collection is unavailable.
type DefaultSelectorAnalyzer struct{}

var _ analysis.Analyzer = &DefaultSelectorAnalyzer{}

// Metadata implements Analyzer
func (a *DefaultSelectorAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "telemetry.DefaultSelectorAnalyzer",
		Description: "Validates that there aren't multiple telemetry resources that have no selector",
		Inputs: []config.GroupVersionKind{
			gvk.Telemetry,
		},
	}
}

// Analyze implements Analyzer
func (a *DefaultSelectorAnalyzer) Analyze(c analysis.Context) {
	nsToTelemetries := make(map[resource.Namespace][]*resource.Instance)

	c.ForEach(gvk.Telemetry, func(r *resource.Instance) bool {
		s := r.Message.(*v1alpha1.Telemetry)

		ns := r.Metadata.FullName.Namespace

		if s.Selector == nil {
			nsToTelemetries[ns] = append(nsToTelemetries[ns], r)
		}
		return true
	})

	// Check for more than one selector-less telemetry instance, per namespace
	for ns, sList := range nsToTelemetries {
		if len(sList) > 1 {
			sNames := getNames(sList)
			for _, r := range sList {
				c.Report(gvk.Telemetry, msg.NewMultipleTelemetriesWithoutWorkloadSelectors(r, sNames, string(ns)))
			}
		}
	}
}
