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

	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/util/sets"
)

// This function merges one or more destination rules for a given host string
// into a single destination rule. Note that it does not perform inheritance style merging.
// IOW, given three dest rules (*.foo.com, *.foo.com, *.com) without selectors, calling this function for
// each config will result in a final dest rule set (*.foo.com, and *.com).
//
// The following is the merge logic:
// 1. Unique subsets (based on subset name) are concatenated to the original rule's list of subsets
// 2. If the original rule did not have any top level traffic policy, traffic policies from the new rule will be
// used.
// 3. If the original rule did not have any exportTo, exportTo settings from the new rule will be used.
func (ps *PushContext) mergeDestinationRule(p *consolidatedDestRules, destRuleConfig config.Config, exportToSet sets.Set[visibility.Instance]) {
	rule := destRuleConfig.Spec.(*networking.DestinationRule)
	resolvedHost := host.Name(rule.Host)

	var destRules map[host.Name][]*ConsolidatedDestRule

	if resolvedHost.IsWildCarded() {
		destRules = p.wildcardDestRules
	} else {
		destRules = p.specificDestRules
	}

	if mdrList, exists := destRules[resolvedHost]; exists {
		// `appendSeparately` determines if the incoming destination rule would become a new unique entry in the processedDestRules list.
		appendSeparately := true
		for _, mdr := range mdrList {
			existingRule := mdr.rule.Spec.(*networking.DestinationRule)
			bothWithoutSelector := rule.GetWorkloadSelector() == nil && existingRule.GetWorkloadSelector() == nil
			bothWithSelector := existingRule.GetWorkloadSelector() != nil && rule.GetWorkloadSelector() != nil
			selectorsMatch := labels.Instance(existingRule.GetWorkloadSelector().GetMatchLabels()).Equals(rule.GetWorkloadSelector().GetMatchLabels())

			if features.EnableEnhancedDestinationRuleMerge {
				if exportToSet.Equals(mdr.exportTo) {
					if bothWithoutSelector || (bothWithSelector && selectorsMatch) {
						appendSeparately = false // Merge the destination rules since the selectors match and exportTo is the same
					} else {
						appendSeparately = true // Attempt to merge and also append as a standalone one since the selectors do not match but exportTo is the same
					}
				} else if len(mdr.exportTo) > 0 && exportToSet.SupersetOf(mdr.exportTo) {
					// If the new exportTo is superset of existing, merge and also append as a standalone one
					appendSeparately = true
				} else {
					// can not merge with existing one, append as a standalone one
					appendSeparately = true
					continue
				}
			}

			if bothWithSelector && !selectorsMatch {
				// If the new destination rule and the existing one has workload selectors associated with them, skip merging
				// if the selectors do not match
				appendSeparately = true
				continue
			}
			// If both the destination rules are without a workload selector or with matching workload selectors, simply merge them.
			// If the incoming rule has a workload selector, it has to be merged with the existing rules with workload selector, and
			// at the same time added as a unique entry in the processedDestRules.
			if bothWithoutSelector || (bothWithSelector && selectorsMatch) {
				appendSeparately = false
			}

			// Deep copy destination rule, to prevent mutate it later when merge with a new one.
			// This can happen when there are more than one destination rule of same host in one namespace.
			copied := mdr.rule.DeepCopy()
			mdr.rule = &copied
			mdr.from = append(mdr.from, destRuleConfig.NamespacedName())
			mergedRule := copied.Spec.(*networking.DestinationRule)

			existingSubset := sets.String{}
			for _, subset := range mergedRule.Subsets {
				existingSubset.Insert(subset.Name)
			}
			// we have another destination rule for same host.
			// concatenate both of them -- essentially add subsets from one to other.
			// Note: we only add the subsets and do not overwrite anything else like exportTo or top level
			// traffic policies if they already exist
			for _, subset := range rule.Subsets {
				if !existingSubset.Contains(subset.Name) {
					// if not duplicated, append
					mergedRule.Subsets = append(mergedRule.Subsets, subset)
				} else {
					// duplicate subset
					ps.AddMetric(DuplicatedSubsets, string(resolvedHost), "",
						fmt.Sprintf("Duplicate subset %s found while merging destination rules for %s",
							subset.Name, string(resolvedHost)))
				}
			}

			// If there is no top level policy and the incoming rule has top level
			// traffic policy, use the one from the incoming rule.
			if mergedRule.TrafficPolicy == nil && rule.TrafficPolicy != nil {
				mergedRule.TrafficPolicy = rule.TrafficPolicy
			}
		}
		if appendSeparately {
			destRules[resolvedHost] = append(destRules[resolvedHost], ConvertConsolidatedDestRule(&destRuleConfig, exportToSet))
		}
		return
	}
	// DestinationRule does not exist for the resolved host so add it
	destRules[resolvedHost] = append(destRules[resolvedHost], ConvertConsolidatedDestRule(&destRuleConfig, exportToSet))
}

func ConvertConsolidatedDestRule(cfg *config.Config, exportToSet sets.Set[visibility.Instance]) *ConsolidatedDestRule {
	return &ConsolidatedDestRule{
		exportTo: exportToSet,
		rule:     cfg,
		from:     []types.NamespacedName{cfg.NamespacedName()},
	}
}

// Equals compare l equals r consolidatedDestRule or not.
func (l *ConsolidatedDestRule) Equals(r *ConsolidatedDestRule) bool {
	if l == r {
		return true
	}
	if l == nil || r == nil {
		return false
	}

	// compare from
	if len(l.from) != len(r.from) {
		return false
	}
	for i, v := range l.from {
		if v != r.from[i] {
			return false
		}
	}
	return true
}

func (l *ConsolidatedDestRule) GetRule() *config.Config {
	if l == nil {
		return nil
	}
	return l.rule
}

func (l *ConsolidatedDestRule) GetFrom() []types.NamespacedName {
	if l == nil {
		return nil
	}
	return l.from
}
