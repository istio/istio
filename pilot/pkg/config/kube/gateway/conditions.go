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

package gateway

import (
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

func createRouteStatus(gateways []routeParentReference, obj config.Config, current []k8s.RouteParentStatus, routeErr *ConfigError) []k8s.RouteParentStatus {
	gws := make([]k8s.RouteParentStatus, 0, len(gateways))
	// Fill in all the gateways that are already present but not owned by us. This is non-trivial as there may be multiple
	// gateway controllers that are exposing their status on the same route. We need to attempt to manage ours properly (including
	// removing gateway references when they are removed), without mangling other Controller's status.
	for _, r := range current {
		if r.ControllerName != ControllerName {
			// We don't own this status, so keep it around
			gws = append(gws, r)
		}
	}
	// Collect all of our unique parent references. There may be multiple when we have a route without section name,
	// but reference a parent with multiple sections.
	seen := map[k8s.ParentReference]routeParentReference{}
	failedCount := map[k8s.ParentReference]int{}
	for _, gw := range gateways {
		// We will append it if it is our first occurrence, or the existing one has an error. This means
		// if *any* section has no errors, we will declare Admitted
		if gw.DeniedReason != nil {
			failedCount[gw.OriginalReference]++
		}
		if exist, f := seen[gw.OriginalReference]; !f || (exist.DeniedReason != nil && gw.DeniedReason == nil) {
			seen[gw.OriginalReference] = gw
		}
	}
	// Now we fill in all the ones we do own
	// TODO look into also reporting ResolvedRefs; we should be gracefully dropping invalid backends instead
	// of rejecting the whole thing.
	for k, gw := range seen {
		var condition metav1.Condition
		if routeErr != nil {
			condition = metav1.Condition{
				Type:               string(k8s.ConditionRouteAccepted),
				Status:             kstatus.StatusFalse,
				ObservedGeneration: obj.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             routeErr.Reason,
				Message:            routeErr.Message,
			}
		} else if gw.DeniedReason != nil {
			err := gw.DeniedReason.Error()
			if failedCount[k] > 1 {
				err = fmt.Sprintf("failed to bind to %d parents, last error: %v", failedCount[k], gw.DeniedReason.Error())
			}
			condition = metav1.Condition{
				Type:               string(k8s.ConditionRouteAccepted),
				Status:             kstatus.StatusFalse,
				ObservedGeneration: obj.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             InvalidParentRef,
				Message:            err,
			}
		} else {
			condition = metav1.Condition{
				Type:               string(k8s.ConditionRouteAccepted),
				Status:             kstatus.StatusTrue,
				ObservedGeneration: obj.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             "RouteAdmitted",
				Message:            "Route was valid",
			}
		}
		gws = append(gws, k8s.RouteParentStatus{
			ParentRef:      gw.OriginalReference,
			ControllerName: ControllerName,
			Conditions:     []metav1.Condition{condition},
		})
	}
	// Ensure output is deterministic.
	// TODO: will we fight over other controllers doing similar (but not identical) ordering?
	sort.SliceStable(gws, func(i, j int) bool {
		return parentRefString(gws[i].ParentRef) > parentRefString(gws[j].ParentRef)
	})
	return gws
}

type ConfigErrorReason = string

const (
	// InvalidDestination indicates an issue with the destination
	InvalidDestination ConfigErrorReason = "InvalidDestination"
	// InvalidParentRef indicates we could not refer to the parent we request
	InvalidParentRef ConfigErrorReason = "InvalidParentReference"
	// InvalidFilter indicates an issue with the filters
	InvalidFilter ConfigErrorReason = "InvalidFilter"
	// InvalidTLS indicates an issue with TLS settings
	InvalidTLS ConfigErrorReason = "InvalidTLS"
	// InvalidConfiguration indicates a generic error for all other invalid configurations
	InvalidConfiguration ConfigErrorReason = "InvalidConfiguration"
)

// ConfigError represents an invalid configuration that will be reported back to the user.
type ConfigError struct {
	Reason  ConfigErrorReason
	Message string
}

type condition struct {
	// reason defines the reason to report on success. Ignored if error is set
	reason string
	// message defines the message to report on success. Ignored if error is set
	message string
	// status defines the status to report on success. The inverse will be set if error is set
	// If not set, will default to StatusTrue
	status metav1.ConditionStatus
	// error defines an error state; the reason and message will be replaced with that of the error and
	// the status inverted
	error *ConfigError
	// setOnce, if enabled, will only set the condition if it is not yet present or set to this reason
	setOnce string
}

// setConditions sets the existingConditions with the new conditions
func setConditions(generation int64, existingConditions []metav1.Condition, conditions map[string]*condition) []metav1.Condition {
	// Sort keys for deterministic ordering
	condKeys := make([]string, 0, len(conditions))
	for k := range conditions {
		condKeys = append(condKeys, k)
	}
	sort.Strings(condKeys)
	for _, k := range condKeys {
		cond := conditions[k]
		setter := kstatus.UpdateConditionIfChanged
		if cond.setOnce != "" {
			setter = func(conditions []metav1.Condition, condition metav1.Condition) []metav1.Condition {
				return kstatus.CreateCondition(conditions, condition, cond.setOnce)
			}
		}
		// A condition can be "negative polarity" (ex: ListenerInvalid) or "positive polarity" (ex:
		// ListenerValid), so in order to determine the status we should set each `condition` defines its
		// default positive status. When there is an error, we will invert that. Example: If we have
		// condition ListenerInvalid, the status will be set to StatusFalse. If an error is reported, it
		// will be inverted to StatusTrue to indicate listeners are invalid. See
		// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
		// for more information
		if cond.error != nil {
			existingConditions = setter(existingConditions, metav1.Condition{
				Type:               k,
				Status:             kstatus.InvertStatus(cond.status),
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             cond.error.Reason,
				Message:            cond.error.Message,
			})
		} else {
			status := cond.status
			if status == "" {
				status = kstatus.StatusTrue
			}
			existingConditions = setter(existingConditions, metav1.Condition{
				Type:               k,
				Status:             status,
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             cond.reason,
				Message:            cond.message,
			})
		}
	}
	return existingConditions
}

func reportGatewayCondition(obj config.Config, conditions map[string]*condition) {
	obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
		gs := s.(*k8s.GatewayStatus)
		gs.Conditions = setConditions(obj.Generation, gs.Conditions, conditions)
		return gs
	})
}

func reportListenerAttachedRoutes(index int, obj config.Config, i int32) {
	obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
		gs := s.(*k8s.GatewayStatus)
		for index >= len(gs.Listeners) {
			gs.Listeners = append(gs.Listeners, k8s.ListenerStatus{})
		}
		status := gs.Listeners[index]
		status.AttachedRoutes = i
		gs.Listeners[index] = status
		return gs
	})
}

func reportListenerCondition(index int, l k8s.Listener, obj config.Config, conditions map[string]*condition) {
	obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
		gs := s.(*k8s.GatewayStatus)
		for index >= len(gs.Listeners) {
			gs.Listeners = append(gs.Listeners, k8s.ListenerStatus{})
		}
		cond := gs.Listeners[index].Conditions
		gs.Listeners[index] = k8s.ListenerStatus{
			Name:           l.Name,
			AttachedRoutes: 0, // this will be reported later
			SupportedKinds: generateSupportedKinds(l),
			Conditions:     setConditions(obj.Generation, cond, conditions),
		}
		return gs
	})
}

func generateSupportedKinds(l k8s.Listener) []k8s.RouteGroupKind {
	supported := []k8s.RouteGroupKind{}
	switch l.Protocol {
	case k8s.HTTPProtocolType, k8s.HTTPSProtocolType:
		// Only terminate allowed, so its always HTTP
		supported = []k8s.RouteGroupKind{{Group: (*k8s.Group)(StrPointer(gvk.HTTPRoute.Group)), Kind: k8s.Kind(gvk.HTTPRoute.Kind)}}
	case k8s.TCPProtocolType:
		supported = []k8s.RouteGroupKind{{Group: (*k8s.Group)(StrPointer(gvk.TCPRoute.Group)), Kind: k8s.Kind(gvk.TCPRoute.Kind)}}
	case k8s.TLSProtocolType:
		if l.TLS != nil && l.TLS.Mode != nil && *l.TLS.Mode == k8s.TLSModePassthrough {
			supported = []k8s.RouteGroupKind{{Group: (*k8s.Group)(StrPointer(gvk.TLSRoute.Group)), Kind: k8s.Kind(gvk.TLSRoute.Kind)}}
		} else {
			supported = []k8s.RouteGroupKind{{Group: (*k8s.Group)(StrPointer(gvk.TCPRoute.Group)), Kind: k8s.Kind(gvk.TCPRoute.Kind)}}
		}
		// UDP route note support
	}
	if l.AllowedRoutes != nil && len(l.AllowedRoutes.Kinds) > 0 {
		// We need to filter down to only ones we actually support
		intersection := []k8s.RouteGroupKind{}
		for _, s := range supported {
			for _, kind := range l.AllowedRoutes.Kinds {
				if s == kind {
					intersection = append(intersection, s)
					break
				}
			}
		}
		return intersection
	}
	return supported
}
