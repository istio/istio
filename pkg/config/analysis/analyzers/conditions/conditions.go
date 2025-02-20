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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/meta/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	groupVersionKind "istio.io/istio/pkg/config/schema/gvk"
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
			groupVersionKind.Service,
			groupVersionKind.ServiceEntry,
			groupVersionKind.AuthorizationPolicy,
			groupVersionKind.KubernetesGateway,
			groupVersionKind.HTTPRoute,
			groupVersionKind.GRPCRoute,
		},
	}
}

// Analyze implements Analyzer
func (c *ConditionAnalyzer) Analyze(ctx analysis.Context) {
	// Check conditions for all supported types
	for _, gvk := range []config.GroupVersionKind{
		groupVersionKind.Service,
		groupVersionKind.ServiceEntry,
		groupVersionKind.AuthorizationPolicy,
		groupVersionKind.KubernetesGateway,
		groupVersionKind.HTTPRoute, groupVersionKind.GRPCRoute,
	} {
		ctx.ForEach(gvk, func(r *resource.Instance) bool {
			conditions := extractConditions(r)
			for _, condition := range conditions {
				if shouldReportCondition(gvk, condition.Type, condition.Status) {
					ctx.Report(gvk, msg.NewNegativeConditionStatus(
						r,
						condition.Type,
						condition.Reason,
						condition.Message,
					))
				}
				// Special case for AuthorizationPolicy to report PartiallyInvalid
				// This is a WaypointAccepted condition which is partially negative but will be reported as status=true
				if gvk == groupVersionKind.AuthorizationPolicy && condition.Type == "WaypointAccepted" && condition.Reason == "PartiallyInvalid" {
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

// negativeConditionsToReport maps GVK to condition type to what we consider the "negative" status
// if we cannot find the condition type, we will not report the condition
var negativeConditionsToReport = map[config.GroupVersionKind]map[string]metav1.ConditionStatus{
	groupVersionKind.Service: {
		"istio.io/WaypointBound": metav1.ConditionFalse,
	},
	groupVersionKind.ServiceEntry: {
		"istio.io/WaypointBound": metav1.ConditionFalse,
	},
	groupVersionKind.AuthorizationPolicy: {
		"ZtunnelAccepted":  metav1.ConditionFalse,
		"WaypointAccepted": metav1.ConditionFalse,
	},
	groupVersionKind.KubernetesGateway: {
		"Accepted":     metav1.ConditionFalse,
		"Programmed":   metav1.ConditionFalse,
		"ResolvedRefs": metav1.ConditionFalse,
	},
	groupVersionKind.HTTPRoute: {
		"Accepted":     metav1.ConditionFalse,
		"Programmed":   metav1.ConditionFalse,
		"ResolvedRefs": metav1.ConditionFalse,
	},
	groupVersionKind.GRPCRoute: {
		"Accepted":     metav1.ConditionFalse,
		"Programmed":   metav1.ConditionFalse,
		"ResolvedRefs": metav1.ConditionFalse,
	},
}

// shouldReportCondition returns true if the condition is considered negative
func shouldReportCondition(gvk config.GroupVersionKind, conditionType string, status metav1.ConditionStatus) bool {
	if negativeConditionsToReport[gvk] == nil {
		return false
	}
	if negativeConditionsToReport[gvk][conditionType] == status {
		return true
	}
	return false
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
		return status.Conditions
	case *gatewayv1.GatewayStatus:
		// TODO: handle listener conditions?
		return status.Conditions
	case *gatewayv1.HTTPRouteStatus:
		return extractParentConditions(status.Parents)
	case *gatewayv1.GRPCRouteStatus:
		return extractParentConditions(status.Parents)
	default:
		return nil
	}
}

// extractParentConditions extracts conditions from RouteParentStatus entries
func extractParentConditions(parents []gatewayv1.RouteParentStatus) []metav1.Condition {
	var conditions []metav1.Condition
	for _, parent := range parents {
		conditions = append(conditions, parent.Conditions...)
	}
	return conditions
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
