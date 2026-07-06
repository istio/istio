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

package gatewaycommon

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// ListenerStatusConfigError represents an invalid listener configuration reported in status.
type ListenerStatusConfigError struct {
	Reason  string
	Message string
}

// ListenerStatusCondition describes a listener status condition update.
type ListenerStatusCondition struct {
	Reason  string
	Message string
	Status  metav1.ConditionStatus
	Error   *ListenerStatusConfigError
	SetOnce string
}

func setListenerConditions(
	generation int64,
	existingConditions []metav1.Condition,
	conditions map[string]*ListenerStatusCondition,
) []metav1.Condition {
	for _, k := range slices.Sort(maps.Keys(conditions)) {
		cond := conditions[k]
		setter := kstatus.UpdateConditionIfChanged
		if cond.SetOnce != "" {
			setOnce := cond.SetOnce
			setter = func(conditions []metav1.Condition, condition metav1.Condition) []metav1.Condition {
				return kstatus.CreateCondition(conditions, condition, setOnce)
			}
		}
		if cond.Error != nil {
			existingConditions = setter(existingConditions, metav1.Condition{
				Type:               k,
				Status:             kstatus.InvertStatus(cond.Status),
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             cond.Error.Reason,
				Message:            cond.Error.Message,
			})
			continue
		}
		status := cond.Status
		if status == "" {
			status = kstatus.StatusTrue
		}
		existingConditions = setter(existingConditions, metav1.Condition{
			Type:               k,
			Status:             status,
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
			Reason:             cond.Reason,
			Message:            cond.Message,
		})
	}
	return existingConditions
}

// ReportListenerConflict marks a listener conflicted per Gateway API merge semantics.
func ReportListenerConflict(
	index int,
	l gatewayv1.Listener,
	obj controllers.Object,
	statusListeners []gatewayv1.ListenerStatus,
	reason gatewayv1.ListenerConditionReason,
) []gatewayv1.ListenerStatus {
	msg := "Listener has a conflict with another listener attached to the same Gateway"
	conditions := map[string]*ListenerStatusCondition{
		string(gatewayv1.ListenerConditionAccepted): {
			Error: &ListenerStatusConfigError{
				Reason:  string(reason),
				Message: msg,
			},
		},
		string(gatewayv1.ListenerConditionProgrammed): {
			Error: &ListenerStatusConfigError{
				Reason:  string(reason),
				Message: msg,
			},
		},
		string(gatewayv1.ListenerConditionConflicted): {
			Reason:  string(reason),
			Message: msg,
			Status:  kstatus.StatusTrue,
		},
	}
	return ReportListenerCondition(index, l, obj, statusListeners, conditions)
}

// ReportListenerCondition applies listener condition updates and supported route kinds.
func ReportListenerCondition(
	index int,
	l gatewayv1.Listener,
	obj controllers.Object,
	statusListeners []gatewayv1.ListenerStatus,
	conditions map[string]*ListenerStatusCondition,
) []gatewayv1.ListenerStatus {
	for index >= len(statusListeners) {
		statusListeners = append(statusListeners, gatewayv1.ListenerStatus{})
	}
	cond := statusListeners[index].Conditions
	supported, valid := GenerateSupportedKinds(l)
	if !valid {
		conditions[string(gatewayv1.ListenerConditionResolvedRefs)] = &ListenerStatusCondition{
			Reason:  string(gatewayv1.ListenerReasonInvalidRouteKinds),
			Status:  metav1.ConditionFalse,
			Message: "Invalid route kinds",
		}
	}
	statusListeners[index] = gatewayv1.ListenerStatus{
		Name:           l.Name,
		AttachedRoutes: 0, // reported later
		SupportedKinds: supported,
		Conditions:     setListenerConditions(obj.GetGeneration(), cond, conditions),
	}
	return statusListeners
}

// GenerateSupportedKinds returns route kinds supported by a listener and whether allowedRoutes kinds are valid.
func GenerateSupportedKinds(l gatewayv1.Listener) ([]gatewayv1.RouteGroupKind, bool) {
	supported := []gatewayv1.RouteGroupKind{}
	switch l.Protocol {
	case gatewayv1.HTTPProtocolType, gatewayv1.HTTPSProtocolType:
		supported = []gatewayv1.RouteGroupKind{
			ToRouteKind(gvk.HTTPRoute),
			ToRouteKind(gvk.GRPCRoute),
		}
	case gatewayv1.TCPProtocolType:
		supported = []gatewayv1.RouteGroupKind{ToRouteKind(gvk.TCPRoute)}
	case gatewayv1.TLSProtocolType:
		supported = []gatewayv1.RouteGroupKind{ToRouteKind(gvk.TLSRoute)}
		if l.TLS != nil && ptr.OrEmpty(l.TLS.Mode) == gatewayv1.TLSModeTerminate {
			supported = append(supported, ToRouteKind(gvk.TCPRoute))
		}
	}
	if l.AllowedRoutes != nil && len(l.AllowedRoutes.Kinds) > 0 {
		intersection := []gatewayv1.RouteGroupKind{}
		for _, s := range supported {
			for _, kind := range l.AllowedRoutes.Kinds {
				if RouteGroupKindEqual(s, kind) {
					intersection = append(intersection, s)
					break
				}
			}
		}
		return intersection, len(intersection) == len(l.AllowedRoutes.Kinds)
	}
	return supported, true
}

// ToRouteKind converts an Istio GVK to a Gateway API RouteGroupKind.
func ToRouteKind(g config.GroupVersionKind) gatewayv1.RouteGroupKind {
	return gatewayv1.RouteGroupKind{Group: (*gatewayv1.Group)(&g.Group), Kind: gatewayv1.Kind(g.Kind)}
}

// RouteGroupKindEqual reports whether two RouteGroupKinds refer to the same route type.
func RouteGroupKindEqual(rgk1, rgk2 gatewayv1.RouteGroupKind) bool {
	return rgk1.Kind == rgk2.Kind && routeGroupKindGroup(rgk1) == routeGroupKindGroup(rgk2)
}

func routeGroupKindGroup(rgk gatewayv1.RouteGroupKind) gatewayv1.Group {
	return ptr.OrDefault(rgk.Group, gatewayv1.Group(gvk.KubernetesGateway.Group))
}
