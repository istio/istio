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

package conditions

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/meta/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// ConditionAnalyzer checks for negative status conditions on services
type ConditionAnalyzer struct{}

var _ analysis.Analyzer = &ConditionAnalyzer{}

// Metadata implements Analyzer
func (c *ConditionAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "conditions.ConditionAnalyzer",
		Description: "Checks for negative status conditions on resources",
		Inputs: []config.GroupVersionKind{
			gvk.Service,
			gvk.ServiceEntry,
			gvk.AuthorizationPolicy,
		},
	}
}

// Analyze implements Analyzer
func (c *ConditionAnalyzer) Analyze(ctx analysis.Context) {
	// Check conditions for all supported types
	for _, gvk := range []config.GroupVersionKind{gvk.Service, gvk.ServiceEntry, gvk.AuthorizationPolicy} {
		ctx.ForEach(gvk, func(r *resource.Instance) bool {
			conditions := extractConditions(r)
			for _, condition := range conditions {
				if condition.Status == metav1.ConditionFalse {
					ctx.Report(gvk, msg.NewNegativeConditionStatus(
						r,
						condition.Type,
						condition.Reason,
						condition.Message,
					))
				}
			}
			return true
		})
	}
}

// extractConditions returns the name, namespace and conditions from a resource
func extractConditions(r *resource.Instance) (conditions []metav1.Condition) {
	if r.Status == nil {
		return nil
	}
	switch status := r.Status.(type) {
	case *v1alpha1.IstioStatus:
		return toMetaV1Conditions(status.Conditions)
	case *networking.ServiceEntryStatus:
		return toMetaV1Conditions(status.Conditions)
	case *corev1.ServiceStatus:
		return (status.Conditions)
	}
	return nil
}

// toMetaV1Conditions converts Istio conditions to Kubernetes meta/v1 conditions
// TODO: surely this exists somewhere in the istio codebase
func toMetaV1Conditions(istioConditions []*v1alpha1.IstioCondition) []metav1.Condition {
	var conditions []metav1.Condition
	for _, ic := range istioConditions {
		conditions = append(conditions, metav1.Condition{
			Type:               ic.Type,
			Status:             metav1.ConditionStatus(ic.Status),
			LastTransitionTime: metav1.Time{Time: ic.LastTransitionTime.AsTime()},
			Reason:             ic.Reason,
			Message:            ic.Message,
			ObservedGeneration: ic.ObservedGeneration,
		})
	}
	return conditions
}
