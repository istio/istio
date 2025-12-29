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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type EnablementSelector struct {
	PodSelector       metav1.LabelSelector `json:"podSelector"`
	NamespaceSelector metav1.LabelSelector `json:"namespaceSelector"`
}

type CompiledEnablementSelectors struct {
	SourceSelectors    []EnablementSelector
	podSelectors       []labels.Selector
	namespaceSelectors []labels.Selector
}

func NewCompiledEnablementSelectors(selectors []EnablementSelector) (*CompiledEnablementSelectors, error) {
	podSelectors := []labels.Selector{}
	namespaceSelectors := []labels.Selector{}

	for _, selector := range selectors {
		var podSelector labels.Selector
		var namespaceSelector labels.Selector
		var err error

		// TODO (mitch): there has got to be a better way to do equality here...
		s := selector.PodSelector.String()
		o := (&metav1.LabelSelector{}).String()
		if s == o {
			// if selector.PodSelector.String() == (&metav1.LabelSelector{}).String() {
			podSelector = labels.Everything()
		} else {
			podSelector, err = metav1.LabelSelectorAsSelector(&selector.PodSelector)
			if err != nil {
				return nil, fmt.Errorf("failed to instantiate ambient enablement pod selector: %v", err)
			}
		}
		if selector.NamespaceSelector.String() == ((&metav1.LabelSelector{}).String()) {
			namespaceSelector = labels.Everything()
		} else {
			namespaceSelector, err = metav1.LabelSelectorAsSelector(&selector.NamespaceSelector)
			if err != nil {
				return nil, fmt.Errorf("failed to instantiate ambient enablement namespace selector: %v", err)
			}
		}

		podSelectors = append(podSelectors, podSelector)
		namespaceSelectors = append(namespaceSelectors, namespaceSelector)
	}

	return &CompiledEnablementSelectors{
		SourceSelectors:    selectors,
		podSelectors:       podSelectors,
		namespaceSelectors: namespaceSelectors,
	}, nil
}

func (c *CompiledEnablementSelectors) Matches(podLabels, podAnnotations, namespaceLabels map[string]string) bool {
	if podHasSidecar(podAnnotations) {
		// Ztunnel and sidecar for a single pod is currently not supported; opt out.
		return false
	}
	podls := labels.Set(podLabels)
	namespacels := labels.Set(namespaceLabels)
	for i, podSelector := range c.podSelectors {
		if podSelector.Matches(podls) && c.namespaceSelectors[i].Matches(namespacels) {
			return true
		}
	}
	return false
}

func (c *CompiledEnablementSelectors) MatchesNamespace(nsLabels map[string]string) bool {
	namespacels := labels.Set(nsLabels)
	for _, namespaceSelector := range c.namespaceSelectors {
		if namespaceSelector.Empty() {
			continue
		}
		if namespaceSelector.Matches(namespacels) {
			return true
		}
	}
	return false
}
