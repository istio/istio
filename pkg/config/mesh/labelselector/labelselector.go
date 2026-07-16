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

// Package labelselector converts the mesh API LabelSelector to a k8s labels.Selector. It is a
// separate package from pkg/config/mesh so that importing the converter pulls in
// k8s.io/apimachinery/pkg/labels only where it is needed: lean binaries like pilot-agent import
// pkg/config/mesh but must not gain the apimachinery dependency (enforced by tests/binary
// TestDependencies).
package labelselector

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// LabelSelectorAsSelector converts a mesh API LabelSelector to a labels.Selector.
// A nil selector matches nothing; an empty selector matches everything.
func LabelSelectorAsSelector(ps *meshconfig.LabelSelector) (labels.Selector, error) {
	if ps == nil {
		return labels.Nothing(), nil
	}
	if len(ps.MatchLabels)+len(ps.MatchExpressions) == 0 {
		return labels.Everything(), nil
	}
	requirements := make([]labels.Requirement, 0, len(ps.MatchLabels)+len(ps.MatchExpressions))
	for k, v := range ps.MatchLabels {
		r, err := labels.NewRequirement(k, selection.Equals, []string{v})
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	for _, expr := range ps.MatchExpressions {
		var op selection.Operator
		switch metav1.LabelSelectorOperator(expr.Operator) {
		case metav1.LabelSelectorOpIn:
			op = selection.In
		case metav1.LabelSelectorOpNotIn:
			op = selection.NotIn
		case metav1.LabelSelectorOpExists:
			op = selection.Exists
		case metav1.LabelSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		default:
			return nil, fmt.Errorf("%q is not a valid label selector operator", expr.Operator)
		}
		r, err := labels.NewRequirement(expr.Key, op, append([]string(nil), expr.Values...))
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	selector := labels.NewSelector()
	selector = selector.Add(requirements...)
	return selector, nil
}
