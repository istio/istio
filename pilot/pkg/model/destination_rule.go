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

package model

import (
	"fmt"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pkg/config/visibility"
)

// This function merges one or more destination rules for a given host string
// into a single destination rule. Note that it does not perform inheritance style merging.
// IOW, given three dest rules (*.foo.com, *.foo.com, *.com), calling this function for
// each config will result in a final dest rule set (*.foo.com, and *.com).
//
// The following is the merge logic:
// 1. Unique subsets (based on subset name) are concatenated to the original rule's list of subsets
// 2. If the original rule did not have any top level traffic policy, traffic policies from the new rule will be
// used.
// 3. If the original rule did not have any exportTo, exportTo settings from the new rule will be used.
func (ps *PushContext) mergeDestinationRule(p *processedDestRules, destRuleConfig Config, exportToMap map[visibility.Instance]bool) {
	rule := destRuleConfig.Spec.(*networking.DestinationRule)
	resolvedHost := ResolveShortnameToFQDN(rule.Host, destRuleConfig.ConfigMeta)

	if mdr, exists := p.destRule[resolvedHost]; exists {
		// Deep copy destination rule, to prevent mutate it later when merge with a new one.
		// This can happen when there are more than one destination rule of same host in one namespace.
		copied := mdr.DeepCopy()
		p.destRule[resolvedHost] = &copied
		mergedRule := copied.Spec.(*networking.DestinationRule)
		existingSubset := map[string]struct{}{}
		for _, subset := range mergedRule.Subsets {
			existingSubset[subset.Name] = struct{}{}
		}
		// we have an another destination rule for same host.
		// concatenate both of them -- essentially add subsets from one to other.
		// Note: we only add the subsets and do not overwrite anything else like exportTo or top level
		// traffic policies if they already exist
		for _, subset := range rule.Subsets {
			if _, ok := existingSubset[subset.Name]; !ok {
				// if not duplicated, append
				mergedRule.Subsets = append(mergedRule.Subsets, subset)
			} else {
				// duplicate subset
				ps.AddMetric(DuplicatedSubsets, string(resolvedHost), nil,
					fmt.Sprintf("Duplicate subset %s found while merging destination rules for %s",
						subset.Name, string(resolvedHost)))
			}
		}

		// If there is no top level policy and the incoming rule has top level
		// traffic policy, use the one from the incoming rule.
		if mergedRule.TrafficPolicy == nil && rule.TrafficPolicy != nil {
			mergedRule.TrafficPolicy = rule.TrafficPolicy
		}

		// If there is no exportTo in the existing rule and
		// the incoming rule has an explicit exportTo, use the
		// one from the incoming rule.
		if len(p.exportTo[resolvedHost]) == 0 && len(exportToMap) > 0 {
			p.exportTo[resolvedHost] = exportToMap
		}
		return
	}

	// DestinationRule does not exist for the resolved host so add it
	p.hosts = append(p.hosts, resolvedHost)
	p.destRule[resolvedHost] = &destRuleConfig
	p.exportTo[resolvedHost] = exportToMap
}
