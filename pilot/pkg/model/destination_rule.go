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
	"istio.io/istio/pkg/config/constants"
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
//
// When a user authored DestinationRule and a DestinationRule synthesized from a Gateway API backend policy
// (BackendTLSPolicy/XBackendTrafficPolicy) target the same host, the user fields win and the backend policy
// only fills in fields the user rule leaves unset, regardless of creation order.
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
			if features.EnableEnhancedDestinationRuleMerge {
				if exportToSet.Equals(mdr.exportTo) {
					appendSeparately = false
				} else if len(mdr.exportTo) > 0 && exportToSet.SupersetOf(mdr.exportTo) {
					// If the new exportTo is superset of existing, merge and also append as a standalone one
					appendSeparately = true
				} else {
					// can not merge with existing one, append as a standalone one
					appendSeparately = true
					continue
				}
			}

			existingRule := mdr.rule.Spec.(*networking.DestinationRule)
			bothWithoutSelector := rule.GetWorkloadSelector() == nil && existingRule.GetWorkloadSelector() == nil
			bothWithSelector := existingRule.GetWorkloadSelector() != nil && rule.GetWorkloadSelector() != nil
			selectorsMatch := labels.Instance(existingRule.GetWorkloadSelector().GetMatchLabels()).Equals(rule.GetWorkloadSelector().GetMatchLabels())
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

			existingIsBackendPolicy := isBackendPolicyDestinationRule(&copied)
			incomingIsBackendPolicy := isBackendPolicyDestinationRule(&destRuleConfig)
			switch {
			case existingIsBackendPolicy == incomingIsBackendPolicy:
				// Both rules have the same origin (both user authored or both synthesized).
				// Keep the first-wins behavior: only adopt the incoming top level policy when
				// the merged rule does not have one yet.
				if mergedRule.TrafficPolicy == nil && rule.TrafficPolicy != nil {
					mergedRule.TrafficPolicy = rule.TrafficPolicy
				}
			case incomingIsBackendPolicy:
				// The merged rule is user authored and the incoming one is synthesized from a
				// backend policy. User fields win; the backend policy fills the gaps.
				mergedRule.TrafficPolicy = mergeBackendPolicyTrafficPolicy(mergedRule.TrafficPolicy, rule.TrafficPolicy)
			default:
				// The merged rule is synthesized from a backend policy and the incoming one is
				// user authored. Same precedence: user fields win.
				mergedRule.TrafficPolicy = mergeBackendPolicyTrafficPolicy(rule.TrafficPolicy, mergedRule.TrafficPolicy)
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

// isBackendPolicyDestinationRule reports whether a DestinationRule was synthesized by Istio from a
// Gateway API backend policy rather than authored directly by a user. Synthesized rules carry the
// internal parents annotation, which users are not permitted to set.
func isBackendPolicyDestinationRule(cfg *config.Config) bool {
	_, ok := cfg.Annotations[constants.InternalParentNames]
	return ok
}

// mergeBackendPolicyTrafficPolicy merges a backend-policy-synthesized traffic policy underneath a user
// authored one. Fields set on the user policy take precedence; the backend policy only supplies fields
// the user policy leaves unset.
func mergeBackendPolicyTrafficPolicy(user, backend *networking.TrafficPolicy) *networking.TrafficPolicy {
	if user == nil {
		return backend
	}
	if backend == nil {
		return user
	}
	// Start from the backend policy and let the user fields override it. hasPortLevel is false so only the
	// fields the user actually sets win; the rest fall back to the backend policy. ShallowCopyTrafficPolicy
	// drops port level settings, so they are merged back in separately below.
	merged := MergeTrafficPolicy(ShallowCopyTrafficPolicy(backend), user, false)
	merged.PortLevelSettings = mergeBackendPolicyPortLevelSettings(user.PortLevelSettings, backend.PortLevelSettings)
	return merged
}

// MergeTrafficPolicy overrides mergedPolicy with the fields set on subsetPolicy and returns mergedPolicy.
// When hasPortLevel is true, port level settings replace the destination level settings wholesale, so a
// field omitted in the port level policy falls back to its default rather than being inherited.
// Lives here rather than pilot/pkg/networking/util to avoid an import cycle (that package imports model);
// it is also used by util.MergeSubsetTrafficPolicy and mergeBackendPolicyTrafficPolicy.
func MergeTrafficPolicy(mergedPolicy, subsetPolicy *networking.TrafficPolicy, hasPortLevel bool) *networking.TrafficPolicy {
	if subsetPolicy.ConnectionPool != nil || hasPortLevel {
		mergedPolicy.ConnectionPool = subsetPolicy.ConnectionPool
	}
	if subsetPolicy.OutlierDetection != nil || hasPortLevel {
		mergedPolicy.OutlierDetection = subsetPolicy.OutlierDetection
	}
	if subsetPolicy.LoadBalancer != nil || hasPortLevel {
		mergedPolicy.LoadBalancer = subsetPolicy.LoadBalancer
	}
	if subsetPolicy.Tls != nil || hasPortLevel {
		mergedPolicy.Tls = subsetPolicy.Tls
	}
	if subsetPolicy.Tunnel != nil {
		mergedPolicy.Tunnel = subsetPolicy.Tunnel
	}
	if subsetPolicy.ProxyProtocol != nil {
		mergedPolicy.ProxyProtocol = subsetPolicy.ProxyProtocol
	}
	if subsetPolicy.RetryBudget != nil {
		mergedPolicy.RetryBudget = subsetPolicy.RetryBudget
	}
	return mergedPolicy
}

// ShallowCopyTrafficPolicy shallow copies a traffic policy. Port level settings are ignored.
// Lives here rather than pilot/pkg/networking/util to avoid an import cycle (that package imports model).
func ShallowCopyTrafficPolicy(original *networking.TrafficPolicy) *networking.TrafficPolicy {
	if original == nil {
		return nil
	}
	ret := &networking.TrafficPolicy{}
	ret.ConnectionPool = original.ConnectionPool
	ret.LoadBalancer = original.LoadBalancer
	ret.OutlierDetection = original.OutlierDetection
	ret.Tls = original.Tls
	ret.Tunnel = original.Tunnel
	ret.ProxyProtocol = original.ProxyProtocol
	ret.RetryBudget = original.RetryBudget
	return ret
}

// mergeBackendPolicyPortLevelSettings overlays user port settings on top of the backend ones. For a port
// configured by both, the user fields win and the backend fills the gaps; ports only the backend sets are
// appended.
func mergeBackendPolicyPortLevelSettings(user, backend []*networking.TrafficPolicy_PortTrafficPolicy) []*networking.TrafficPolicy_PortTrafficPolicy {
	if len(backend) == 0 {
		return user
	}
	byPort := make(map[uint32]*networking.TrafficPolicy_PortTrafficPolicy, len(user))
	for _, p := range user {
		byPort[p.GetPort().GetNumber()] = p
	}
	merged := user
	for _, bp := range backend {
		up, ok := byPort[bp.GetPort().GetNumber()]
		if !ok {
			merged = append(merged, bp)
			continue
		}
		if up.LoadBalancer == nil {
			up.LoadBalancer = bp.LoadBalancer
		}
		if up.ConnectionPool == nil {
			up.ConnectionPool = bp.ConnectionPool
		}
		if up.OutlierDetection == nil {
			up.OutlierDetection = bp.OutlierDetection
		}
		if up.Tls == nil {
			up.Tls = bp.Tls
		}
	}
	return merged
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
