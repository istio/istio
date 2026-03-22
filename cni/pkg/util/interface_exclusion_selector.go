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

package util

import (
	"fmt"

	"istio.io/istio/pkg/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// InterfaceExclusionRule defines a rule for excluding interfaces based on namespace matching
type InterfaceExclusionRule struct {
	InterfaceName     string               `json:"interfaceName"`
	NamespaceSelector metav1.LabelSelector `json:"namespaceSelector"`
}

// CompiledInterfaceExclusionRules contains pre-compiled selectors for efficient matching
type CompiledInterfaceExclusionRules struct {
	SourceRules        []InterfaceExclusionRule
	namespaceSelectors []labels.Selector
}

func NewCompiledInterfaceExclusionRules(rules []InterfaceExclusionRule) (*CompiledInterfaceExclusionRules, error) {
	namespaceSelectors := []labels.Selector{}

	for _, rule := range rules {
		var namespaceSelector labels.Selector
		var err error

		// Empty selector matches everything
		if rule.NamespaceSelector.String() == (&metav1.LabelSelector{}).String() {
			namespaceSelector = labels.Everything()
		} else {
			namespaceSelector, err = metav1.LabelSelectorAsSelector(&rule.NamespaceSelector)
			if err != nil {
				return nil, fmt.Errorf("failed to instantiate interface exclusion namespace selector: %v", err)
			}
		}

		namespaceSelectors = append(namespaceSelectors, namespaceSelector)
	}

	return &CompiledInterfaceExclusionRules{
		SourceRules:        rules,
		namespaceSelectors: namespaceSelectors,
	}, nil
}

// GetExcludedInterfaces returns a list of interface names that should be excluded for a given namespace
func (c *CompiledInterfaceExclusionRules) GetExcludedInterfaces(namespaceLabels map[string]string) []string {
	namespacels := labels.Set(namespaceLabels)
	excludedInterfaces := []string{}

	for i, namespaceSelector := range c.namespaceSelectors {
		if namespaceSelector.Matches(namespacels) {
			excludedInterfaces = append(excludedInterfaces, c.SourceRules[i].InterfaceName)
		}
	}

	return excludedInterfaces
}

// MatchesNamespace returns true if any rule matches the given namespace
// (useful for triggering re-evaluation when namespace labels change)
func (c *CompiledInterfaceExclusionRules) MatchesNamespace(namespaceLabels map[string]string) bool {
	namespacels := labels.Set(namespaceLabels)
	for _, namespaceSelector := range c.namespaceSelectors {
		if !namespaceSelector.Empty() && namespaceSelector.Matches(namespacels) {
			return true
		}
	}
	return false
}

// ExcludedInterfacesChanged compares current vs desired excluded interfaces.
// Returns true if they differ (order-independent comparison).
// Treats nil and empty slices as equivalent.
func ExcludedInterfacesChanged(current, desired []string) bool {
	if len(current) == 0 && len(desired) == 0 {
		return false
	}
	return !slices.EqualUnordered(current, desired)
}
