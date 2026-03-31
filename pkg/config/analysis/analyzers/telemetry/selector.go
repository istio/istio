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

	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// SelectorAnalyzer validates, per namespace, that:
// * telemetry resources that define a workload selector match at least one pod
// * there aren't multiple telemetry resources that select overlapping pods
type SelectorAnalyzer struct{}

var _ analysis.Analyzer = &SelectorAnalyzer{}

// Metadata implements Analyzer
func (a *SelectorAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "telemetry.SelectorAnalyzer",
		Description: "Validates that telemetries that define a selector " +
			"match at least one pod, and that there aren't multiple telemetry resources that select overlapping pods",
		Inputs: []config.GroupVersionKind{
			gvk.Telemetry,
			gvk.Pod,
		},
	}
}

// Analyze implements Analyzer
func (a *SelectorAnalyzer) Analyze(c analysis.Context) {
	podsToTelemetries := make(map[resource.FullName][]*resource.Instance)

	// This is using an unindexed approach for matching selectors.
	// Using an index for selectors is problematic because selector != label
	// We can match a label to a selector, but we can't generate a selector from a label.
	c.ForEach(gvk.Telemetry, func(rs *resource.Instance) bool {
		s := rs.Message.(*v1alpha1.Telemetry)

		// For this analysis, ignore Telemetries with no selectors specified at all.
		if s.Selector == nil || len(s.GetSelector().MatchLabels) == 0 {
			return true
		}

		sNs := rs.Metadata.FullName.Namespace
		sel := labels.SelectorFromSet(s.GetSelector().MatchLabels)

		foundPod := false
		c.ForEach(gvk.Pod, func(rp *resource.Instance) bool {
			pNs := rp.Metadata.FullName.Namespace
			podLabels := labels.Set(rp.Metadata.Labels)

			// Only attempt to match in the same namespace
			if pNs != sNs {
				return true
			}

			if sel.Matches(podLabels) {
				foundPod = true
				podsToTelemetries[rp.Metadata.FullName] = append(podsToTelemetries[rp.Metadata.FullName], rs)
			}

			return true
		})

		if !foundPod {
			m := msg.NewReferencedResourceNotFound(rs, "selector", sel.String())

			label := util.ExtractLabelFromSelectorString(sel.String())
			if line, ok := util.ErrorLine(rs, fmt.Sprintf(util.TelemetrySelector, label)); ok {
				m.Line = line
			}

			c.Report(gvk.Telemetry, m)
		}

		return true
	})

	for p, sList := range podsToTelemetries {
		if len(sList) == 1 {
			continue
		}

		sNames := getNames(sList)
		for _, rs := range sList {
			m := msg.NewConflictingTelemetryWorkloadSelectors(rs, sNames, p.Namespace.String(), p.Name.String())

			if line, ok := util.ErrorLine(rs, fmt.Sprintf(util.MetadataName)); ok {
				m.Line = line
			}

			c.Report(gvk.Telemetry, m)
		}
	}
}
