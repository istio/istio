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

package sidecar

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// SelectorAnalyzer validates, per namespace, that:
// * sidecar resources that define a workload selector match at least one pod
// * there aren't multiple sidecar resources that select overlapping pods
type SelectorAnalyzer struct{}

var _ analysis.Analyzer = &SelectorAnalyzer{}

// Metadata implements Analyzer
func (a *SelectorAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "sidecar.SelectorAnalyzer",
		Description: "Validates that sidecars that define a workload selector " +
			"match at least one pod, and that there aren't multiple sidecar resources that select overlapping pods",
		Inputs: []config.GroupVersionKind{
			gvk.Sidecar,
			gvk.Pod,
			gvk.Namespace,
		},
	}
}

// Analyze implements Analyzer
func (a *SelectorAnalyzer) Analyze(c analysis.Context) {
	podsToSidecars := make(map[resource.FullName][]*resource.Instance)
	pods := make(map[resource.FullName]*resource.Instance)
	namespacesToSidecars := make(map[resource.Namespace][]*resource.Instance)
	namespaces := make(map[string]*resource.Instance)

	c.ForEach(gvk.Sidecar, func(rs *resource.Instance) bool {
		s := rs.Message.(*v1alpha3.Sidecar)

		// record namespace-scoped sidecars
		if s.WorkloadSelector == nil || len(s.WorkloadSelector.Labels) == 0 {
			namespacesToSidecars[rs.Metadata.FullName.Namespace] = append(namespacesToSidecars[rs.Metadata.FullName.Namespace], rs)
			return true
		}

		sNs := rs.Metadata.FullName.Namespace
		sel := labels.SelectorFromSet(s.WorkloadSelector.Labels)

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
				podsToSidecars[rp.Metadata.FullName] = append(podsToSidecars[rp.Metadata.FullName], rs)
				pods[rp.Metadata.FullName] = rp
			}

			return true
		})

		if !foundPod {
			m := msg.NewReferencedResourceNotFound(rs, "selector", sel.String())

			label := util.ExtractLabelFromSelectorString(sel.String())
			if line, ok := util.ErrorLine(rs, fmt.Sprintf(util.WorkloadSelector, label)); ok {
				m.Line = line
			}

			c.Report(gvk.Sidecar, m)
		}

		return true
	})

	c.ForEach(gvk.Namespace, func(r *resource.Instance) bool {
		namespaces[r.Metadata.FullName.Name.String()] = r
		return true
	})

	reportedResources := make(map[string]bool)
	for p, sList := range podsToSidecars {
		podResource := pods[p]

		if len(sList) == 1 && !util.PodInAmbientMode(podResource) {
			continue
		}

		sNames := getNames(sList)

		for _, rs := range sList {
			// We don't want to report errors for pods in ambient mode, since there is no sidecar,
			// but we do want to warn that the policy is ineffective.
			if util.PodInAmbientMode(podResource) {
				if !reportedResources[rs.Metadata.FullName.String()] {
					c.Report(gvk.Sidecar, msg.NewIneffectivePolicy(rs,
						"selected workload is in ambient mode, the policy has no impact"))
					reportedResources[rs.Metadata.FullName.String()] = true
				}
				continue
			}

			m := msg.NewConflictingSidecarWorkloadSelectors(rs, sNames, p.Namespace.String(), p.Name.String())

			if line, ok := util.ErrorLine(rs, fmt.Sprintf(util.MetadataName)); ok {
				m.Line = line
			}

			c.Report(gvk.Sidecar, m)
		}
	}

	for ns, sList := range namespacesToSidecars {
		nsResource := namespaces[ns.String()]
		// We don't want to report errors for namespaces in ambient mode, since there is no sidecar,
		// but we do want to warn that the policy is ineffective.
		// TODO: do we need to check mixed mode?
		if util.NamespaceInAmbientMode(nsResource) {
			for _, rs := range sList {
				if !reportedResources[rs.Metadata.FullName.String()] {
					c.Report(gvk.Sidecar, msg.NewIneffectivePolicy(rs,
						"namespace is in ambient mode, the policy has no impact"))
					reportedResources[rs.Metadata.FullName.String()] = true
				}
			}
			continue
		}
		if len(sList) > 1 {
			sNames := getNames(sList)
			for _, r := range sList {
				c.Report(gvk.Sidecar, msg.NewMultipleSidecarsWithoutWorkloadSelectors(r, sNames, string(ns)))
			}
		}
	}
}
