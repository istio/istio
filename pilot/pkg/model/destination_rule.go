// Copyright 2018 Istio Authors
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
)

// This function merges one or more destination rules for a given host string
// into a single destination rule. Note that it does not perform inheritance style merging.
// IOW, given three dest rules (*.foo.com, *.foo.com, *.com), calling this function for
// each config will result in a final dest rule set (*.foo.com, and *.com).
func (ps *PushContext) combineSingleDestinationRule(
	combinedDestRuleHosts []Hostname,
	combinedDestRuleMap map[Hostname]*combinedDestinationRule,
	destRuleConfig Config) ([]Hostname, map[Hostname]*combinedDestinationRule) {
	rule := destRuleConfig.Spec.(*networking.DestinationRule)
	resolvedHost := ResolveShortnameToFQDN(rule.Host, destRuleConfig.ConfigMeta)

	if mdr, exists := combinedDestRuleMap[resolvedHost]; exists {
		combinedRule := mdr.config.Spec.(*networking.DestinationRule)
		// we have an another destination rule for same host.
		// concatenate both of them -- essentially add subsets from one to other.
		for _, subset := range rule.Subsets {
			if _, subsetExists := mdr.subsets[subset.Name]; !subsetExists {
				mdr.subsets[subset.Name] = struct{}{}
				combinedRule.Subsets = append(combinedRule.Subsets, subset)
			} else {
				ps.Add(DuplicatedSubsets, string(resolvedHost), nil,
					fmt.Sprintf("Duplicate subset %s found while merging destination rules for %s",
						subset.Name, string(resolvedHost)))
			}

			// If there is no top level policy and the incoming rule has top level
			// traffic policy, use the one from the incoming rule.
			if combinedRule.TrafficPolicy == nil && rule.TrafficPolicy != nil {
				combinedRule.TrafficPolicy = rule.TrafficPolicy
			}
		}
		return combinedDestRuleHosts, combinedDestRuleMap
	}

	combinedDestRuleMap[resolvedHost] = &combinedDestinationRule{
		subsets: make(map[string]struct{}),
		config:  &destRuleConfig,
	}
	for _, subset := range rule.Subsets {
		combinedDestRuleMap[resolvedHost].subsets[subset.Name] = struct{}{}
	}
	combinedDestRuleHosts = append(combinedDestRuleHosts, resolvedHost)

	return combinedDestRuleHosts, combinedDestRuleMap
}
