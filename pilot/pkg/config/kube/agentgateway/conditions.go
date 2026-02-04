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

package agentgateway

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

type ParentErrorReason string

const (
	ParentErrorNotAccepted       = ParentErrorReason(k8s.RouteReasonNoMatchingParent)
	ParentErrorNotAllowed        = ParentErrorReason(k8s.RouteReasonNotAllowedByListeners)
	ParentErrorNoHostname        = ParentErrorReason(k8s.RouteReasonNoMatchingListenerHostname)
	ParentErrorParentRefConflict = ParentErrorReason("ParentRefConflict")
	ParentNoError                = ParentErrorReason("")
)

type ConfigErrorReason = string

const (
	// InvalidDestination indicates an issue with the destination
	InvalidDestination ConfigErrorReason = "InvalidDestination"
	InvalidAddress     ConfigErrorReason = ConfigErrorReason(k8s.GatewayReasonUnsupportedAddress)
	// InvalidDestinationPermit indicates a destination was not permitted
	InvalidDestinationPermit ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonRefNotPermitted)
	// InvalidDestinationKind indicates an issue with the destination kind
	InvalidDestinationKind ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonInvalidKind)
	// InvalidDestinationNotFound indicates a destination does not exist
	InvalidDestinationNotFound ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonBackendNotFound)
	// InvalidFilter indicates an issue with the filters
	InvalidFilter ConfigErrorReason = "InvalidFilter"
	// InvalidTLS indicates an issue with TLS settings
	InvalidTLS ConfigErrorReason = ConfigErrorReason(k8s.ListenerReasonInvalidCertificateRef)
	// InvalidListenerRefNotPermitted indicates a listener reference was not permitted
	InvalidListenerRefNotPermitted ConfigErrorReason = ConfigErrorReason(k8s.ListenerReasonRefNotPermitted)
	// InvalidConfiguration indicates a generic error for all other invalid configurations
	InvalidConfiguration ConfigErrorReason = "InvalidConfiguration"
	DeprecateFieldUsage  ConfigErrorReason = "DeprecatedField"
)

const (
	// This condition indicates whether a route's parent reference has
	// a waypoint configured by resolving the "istio.io/use-waypoint" label
	// on either the referenced parent or the parent's namespace.
	RouteConditionResolvedWaypoints k8s.RouteConditionType   = "ResolvedWaypoints"
	RouteReasonResolvedWaypoints    k8s.RouteConditionReason = "ResolvedWaypoints"
)

type WaypointErrorReason string

const (
	WaypointErrorReasonMissingLabel     = WaypointErrorReason("MissingUseWaypointLabel")
	WaypointErrorMsgMissingLabel        = "istio.io/use-waypoint label missing from parent and parent namespace; in ambient mode, route will not be respected"
	WaypointErrorReasonNoMatchingParent = WaypointErrorReason("NoMatchingParent")
	WaypointErrorMsgNoMatchingParent    = "parent not found"
)

// ParentError represents that a parent could not be referenced
type ParentError struct {
	Reason  ParentErrorReason
	Message string
}

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
	for _, k := range slices.Sort(maps.Keys(conditions)) {
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

func reportListenerCondition(index int, l k8s.Listener, obj controllers.Object,
	statusListeners []k8s.ListenerStatus, conditions map[string]*condition,
) []k8s.ListenerStatus {
	for index >= len(statusListeners) {
		statusListeners = append(statusListeners, k8s.ListenerStatus{})
	}
	cond := statusListeners[index].Conditions
	supported, valid := generateSupportedKinds(l)
	if !valid {
		conditions[string(k8s.ListenerConditionResolvedRefs)] = &condition{
			reason:  string(k8s.ListenerReasonInvalidRouteKinds),
			status:  metav1.ConditionFalse,
			message: "Invalid route kinds",
		}
	}
	statusListeners[index] = k8s.ListenerStatus{
		Name:           l.Name,
		AttachedRoutes: 0, // this will be reported later
		SupportedKinds: supported,
		Conditions:     setConditions(obj.GetGeneration(), cond, conditions),
	}
	return statusListeners
}

func generateSupportedKinds(l k8s.Listener) ([]k8s.RouteGroupKind, bool) {
	supported := []k8s.RouteGroupKind{}
	switch l.Protocol {
	case k8s.HTTPProtocolType, k8s.HTTPSProtocolType:
		// Only terminate allowed, so its always HTTP
		supported = []k8s.RouteGroupKind{
			toRouteKind(gvk.HTTPRoute),
			toRouteKind(gvk.GRPCRoute),
		}
	case k8s.TCPProtocolType:
		supported = []k8s.RouteGroupKind{toRouteKind(gvk.TCPRoute)}
	case k8s.TLSProtocolType:
		if l.TLS != nil && l.TLS.Mode != nil && *l.TLS.Mode == k8s.TLSModePassthrough {
			supported = []k8s.RouteGroupKind{toRouteKind(gvk.TLSRoute)}
		} else {
			supported = []k8s.RouteGroupKind{toRouteKind(gvk.TCPRoute)}
		}
		// UDP route not support
	}
	if l.AllowedRoutes != nil && len(l.AllowedRoutes.Kinds) > 0 {
		// We need to filter down to only ones we actually support
		intersection := []k8s.RouteGroupKind{}
		for _, s := range supported {
			for _, kind := range l.AllowedRoutes.Kinds {
				if routeGroupKindEqual(s, kind) {
					intersection = append(intersection, s)
					break
				}
			}
		}
		return intersection, len(intersection) == len(l.AllowedRoutes.Kinds)
	}
	return supported, true
}

func FilterInPlaceByIndex[E any](s []E, keep func(int) bool) []E {
	i := 0
	for j := 0; j < len(s); j++ {
		if keep(j) {
			s[i] = s[j]
			i++
		}
	}

	clear(s[i:]) // zero/nil out the obsolete elements, for GC
	return s[:i]
}
