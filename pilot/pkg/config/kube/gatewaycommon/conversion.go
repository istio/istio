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
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
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

// SetResourceConditions applies resource-level condition updates.
func SetResourceConditions(
	generation int64,
	existingConditions []metav1.Condition,
	conditions map[string]*ListenerStatusCondition,
) []metav1.Condition {
	return setListenerConditions(generation, existingConditions, conditions)
}

// HumanReadableJoin formats a slice of strings for human-readable status messages.
func HumanReadableJoin(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	case 2:
		return ss[0] + " and " + ss[1]
	default:
		return strings.Join(ss[:len(ss)-1], ", ") + ", and " + ss[len(ss)-1]
	}
}

// SetProgrammedCondition updates the Programmed condition from address resolution results.
func SetProgrammedCondition(
	conditions map[string]*ListenerStatusCondition,
	internal []string,
	gatewayServices []string,
	warnings []string,
	allUsable bool,
) {
	if len(internal) > 0 {
		msg := fmt.Sprintf("Resource programmed, assigned to service(s) %s", HumanReadableJoin(internal))
		conditions[string(gatewayv1.GatewayConditionProgrammed)].Message = msg
	}

	if len(gatewayServices) == 0 {
		conditions[string(gatewayv1.GatewayConditionProgrammed)].Error = &ListenerStatusConfigError{
			Reason:  string(gatewayv1.GatewayReasonUnsupportedAddress),
			Message: "Failed to assign to any requested addresses",
		}
	} else if len(warnings) > 0 {
		var msg string
		var reason string
		if len(internal) != 0 {
			msg = fmt.Sprintf("Assigned to service(s) %s, but failed to assign to all requested addresses: %s",
				HumanReadableJoin(internal), strings.Join(warnings, "; "))
		} else {
			msg = fmt.Sprintf("Failed to assign to any requested addresses: %s", strings.Join(warnings, "; "))
		}
		if allUsable {
			reason = string(gatewayv1.GatewayReasonAddressNotAssigned)
		} else {
			reason = string(gatewayv1.GatewayReasonAddressNotUsable)
		}
		conditions[string(gatewayv1.GatewayConditionProgrammed)].Error = &ListenerStatusConfigError{
			// TODO: this only checks Service ready, we should also check Deployment ready?
			Reason:  reason,
			Message: msg,
		}
	}
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

// ReportListenerSetStatus computes ListenerSet-level Accepted/Programmed conditions and prunes stale listener entries.
func ReportListenerSetStatus(
	r *GatewayContext,
	parentGwObj *gatewayv1.Gateway,
	obj *gatewayv1.ListenerSet,
	gs *gatewayv1.ListenerSetStatus,
	gatewayServices []string,
	servers []*istio.Server,
	gatewayErr *ListenerStatusConfigError,
	listenersValid bool,
) {
	internal, _, _, _, warnings, allUsable := r.ResolveGatewayInstances(parentGwObj.Namespace, gatewayServices, servers)

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. Unlike a Gateway, a ListenerSet is only Accepted if at least
	// one of its listeners is valid; otherwise we report ListenersNotValid.
	// Programmed: is the data plane "ready" (note: eventually consistent)
	conditions := map[string]*ListenerStatusCondition{
		string(gatewayv1.GatewayConditionAccepted): {
			Reason:  string(gatewayv1.GatewayReasonAccepted),
			Message: "Resource accepted",
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			Reason:  string(gatewayv1.GatewayReasonProgrammed),
			Message: "Resource programmed",
		},
	}
	var listenersErr *ListenerStatusConfigError
	if !listenersValid {
		listenersErr = &ListenerStatusConfigError{
			Reason:  string(gatewayv1.ListenerSetReasonListenersNotValid),
			Message: "None of the ListenerSet's listeners are valid",
		}
	}

	if gatewayErr != nil {
		conditions[string(gatewayv1.GatewayConditionAccepted)].Error = &ListenerStatusConfigError{
			Reason:  gatewayErr.Reason,
			Message: "Parent not accepted: " + gatewayErr.Message,
		}
	} else if listenersErr != nil {
		conditions[string(gatewayv1.GatewayConditionAccepted)].Error = listenersErr
	}

	SetProgrammedCondition(conditions, internal, gatewayServices, warnings, allUsable)

	// A ListenerSet with no valid listeners cannot be programmed; this takes precedence over any
	// address-related programming status.
	if listenersErr != nil {
		conditions[string(gatewayv1.GatewayConditionProgrammed)].Error = listenersErr
	}

	// Prune stale listener status entries whose listener was removed from the spec,
	// mirroring reportGatewayStatus. ListenerSetCollection threads the previous status
	// forward to preserve per-listener data across reconciles, but only refreshes entries
	// for listeners still in the spec. Without this pruning, a removed listener's status
	// entry is orphaned forever (len(status.Listeners) > len(spec.Listeners)), which also
	// wedges observedGeneration once the recomputed status stops differing from the stored one.
	PruneListenerSetStatusListeners(obj, gs)

	gs.Conditions = SetResourceConditions(obj.Generation, gs.Conditions, conditions)
}

// ReportUnsupportedListenerSet reports status when the GatewayClass does not support ListenerSet.
func ReportUnsupportedListenerSet(class string, status *gatewayv1.ListenerSetStatus, obj *gatewayv1.ListenerSet) {
	msg := fmt.Sprintf("The %q GatewayClass does not support ListenerSet", class)
	conditions := listenerSetNotAllowedConditions(msg)
	status.Listeners = nil
	status.Conditions = SetResourceConditions(obj.Generation, status.Conditions, conditions)
}

// ReportNotAllowedListenerSet reports status when the parent Gateway does not allow the ListenerSet reference.
func ReportNotAllowedListenerSet(status *gatewayv1.ListenerSetStatus, obj *gatewayv1.ListenerSet) {
	conditions := listenerSetNotAllowedConditions(
		"The parent Gateway does not allow this reference; check the 'spec.allowedRoutes'",
	)
	status.Listeners = nil
	status.Conditions = SetResourceConditions(obj.Generation, status.Conditions, conditions)
}

func listenerSetNotAllowedConditions(message string) map[string]*ListenerStatusCondition {
	err := &ListenerStatusConfigError{
		Reason:  string(gatewayv1.ListenerSetReasonNotAllowed),
		Message: message,
	}
	return map[string]*ListenerStatusCondition{
		string(gatewayv1.GatewayConditionAccepted): {
			Reason: string(gatewayv1.GatewayReasonAccepted),
			Error:  err,
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			Reason: string(gatewayv1.GatewayReasonProgrammed),
			Error:  err,
		},
	}
}

// PruneListenerSetStatusListeners removes status entries for listeners no longer in the spec.
func PruneListenerSetStatusListeners(obj *gatewayv1.ListenerSet, gs *gatewayv1.ListenerSetStatus) {
	haveListeners := sets.New[gatewayv1.SectionName]()
	for _, l := range obj.Spec.Listeners {
		haveListeners.Insert(l.Name)
	}
	listeners := make([]gatewayv1.ListenerEntryStatus, 0, len(gs.Listeners))
	for _, l := range gs.Listeners {
		if haveListeners.Contains(l.Name) {
			haveListeners.Delete(l.Name)
			listeners = append(listeners, l)
		}
	}
	gs.Listeners = listeners
}

// ConvertStandardStatusToListenerSetStatus converts Gateway listener status to ListenerSet listener status.
func ConvertStandardStatusToListenerSetStatus(e gatewayv1.ListenerStatus) gatewayv1.ListenerEntryStatus {
	return gatewayv1.ListenerEntryStatus(e)
}

// ConvertListenerSetStatusToStandardStatus converts ListenerSet listener status to Gateway listener status.
func ConvertListenerSetStatusToStandardStatus(e gatewayv1.ListenerEntryStatus) gatewayv1.ListenerStatus {
	return gatewayv1.ListenerStatus(e)
}

// FinalListenerSetStatusCollection updates AttachedRoutes on each ListenerSet listener from route attachments.
func FinalListenerSetStatusCollection[R any](
	listenerSetStatuses krt.StatusCollection[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus],
	fetchRoutes func(ctx krt.HandlerContext, obj *gatewayv1.ListenerSet) []R,
	listenerName func(R) string,
	opts krt.OptionsBuilder,
) krt.StatusCollection[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus] {
	return krt.NewCollection(
		listenerSetStatuses,
		func(
			ctx krt.HandlerContext, i krt.ObjectWithStatus[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus],
		) *krt.ObjectWithStatus[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus] {
			routes := fetchRoutes(ctx, i.Obj)
			counts := map[string]int32{}
			for _, r := range routes {
				counts[listenerName(r)]++
			}
			status := i.Status.DeepCopy()
			for i, s := range status.Listeners {
				s.AttachedRoutes = counts[string(s.Name)]
				status.Listeners[i] = s
			}
			return &krt.ObjectWithStatus[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus]{
				Obj:    i.Obj,
				Status: *status,
			}
		}, opts.WithName("ListenerSetFinalStatus")...)
}
