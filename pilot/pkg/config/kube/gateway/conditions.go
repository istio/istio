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
	// While we process these internally for-each sectionName, in the status we are just supposed to report one merged entry
	seen := map[k8s.ParentReference]routeParentReference{}
	failedCount := map[k8s.ParentReference]int{}
	successCount := map[k8s.ParentReference]int{}
	for _, incoming := range gateways {
		// We will append it if it is our first occurrence, or the existing one has an error. This means
		// if *any* section has no errors, we will declare Admitted
		if incoming.DeniedReason != nil {
			failedCount[incoming.OriginalReference]++
		} else {
			successCount[incoming.OriginalReference]++
		}
		exist, f := seen[incoming.OriginalReference]
		if !f {
			exist = incoming
		} else {
			if incoming.DeniedReason == nil {
				// The incoming gateway has no error; wipe out any errors
				// When we have multiple, this means we attached without sectionName - we only need one success to be valid.
				exist.DeniedReason = nil
			} else if exist.DeniedReason != nil {
				// TODO this only handles message, not reason
				exist.DeniedReason.Message += "; " + incoming.DeniedReason.Message
			}
		}
		seen[exist.OriginalReference] = exist
	}
	// Enhance error message when we have multiple
	for k, gw := range seen {
		if gw.DeniedReason == nil {
			continue
		}
		if failedCount[k] > 1 {
			gw.DeniedReason.Message = fmt.Sprintf("failed to bind to %d parents, errors: %v", failedCount[k], gw.DeniedReason.Message)
		}
	}

	// Now we fill in all the parents we do own
	for k, gw := range seen {
		msg := "Route was valid"
		if successCount[k] > 1 {
			msg = fmt.Sprintf("Route was valid, bound to %d parents", successCount[k])
		}
		conds := map[string]*condition{
			string(k8s.RouteConditionAccepted): {
				reason:  string(k8s.RouteReasonAccepted),
				message: msg,
			},
			string(k8s.RouteConditionResolvedRefs): {
				reason:  string(k8s.RouteReasonResolvedRefs),
				message: "All references resolved",
			},
		}
		if routeErr != nil {
			// Currently, the spec is not clear on where errors should be reported. The provided resources are:
			// * Accepted - used to describe errors binding to parents
			// * ResolvedRefs - used to describe errors about binding to objects
			// But no general errors
			// For now, we will treat all general route errors as "Ref" errors.
			conds[string(k8s.RouteConditionResolvedRefs)].error = routeErr
		}
		if gw.DeniedReason != nil {
			conds[string(k8s.RouteConditionAccepted)].error = &ConfigError{
				Reason:  ConfigErrorReason(gw.DeniedReason.Reason),
				Message: gw.DeniedReason.Message,
			}
		}
		gws = append(gws, k8s.RouteParentStatus{
			ParentRef:      gw.OriginalReference,
			ControllerName: ControllerName,
			Conditions:     setConditions(obj.Generation, nil, conds),
		})
	}
	// Ensure output is deterministic.
	// TODO: will we fight over other controllers doing similar (but not identical) ordering?
	sort.SliceStable(gws, func(i, j int) bool {
		return parentRefString(gws[i].ParentRef) > parentRefString(gws[j].ParentRef)
	})
	return gws
}

type ParentErrorReason string

const (
	ParentErrorNotAllowed = ParentErrorReason(k8s.RouteReasonNotAllowedByListeners)
	ParentErrorNoHostname = ParentErrorReason(k8s.RouteReasonNoMatchingListenerHostname)
)

type ConfigErrorReason = string

const (
	// InvalidRefNotPermitted indicates a route was not permitted
	InvalidRefNotPermitted ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonRefNotPermitted)
	// InvalidDestination indicates an issue with the destination
	InvalidDestination ConfigErrorReason = "InvalidDestination"
	// InvalidDestinationPermit indicates a destination was not permitted
	InvalidDestinationPermit ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonRefNotPermitted)
	// InvalidDestinationKind indicates an issue with the destination kind
	InvalidDestinationKind ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonInvalidKind)
	// InvalidDestinationNotFound indicates a destination does not exist
	InvalidDestinationNotFound ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonBackendNotFound)
	// InvalidParentRef indicates we could not refer to the parent we request
	InvalidParentRef ConfigErrorReason = "InvalidParentReference"
	// InvalidFilter indicates an issue with the filters
	InvalidFilter ConfigErrorReason = "InvalidFilter"
	// InvalidTLS indicates an issue with TLS settings
	InvalidTLS ConfigErrorReason = ConfigErrorReason(k8s.ListenerReasonInvalidCertificateRef)
	// InvalidListenerRefNotPermitted indicates a listener reference was not permitted
	InvalidListenerRefNotPermitted ConfigErrorReason = ConfigErrorReason(k8s.ListenerReasonRefNotPermitted)
	// InvalidConfiguration indicates a generic error for all other invalid configurations
	InvalidConfiguration ConfigErrorReason = "InvalidConfiguration"
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
		supported, valid := generateSupportedKinds(l)
		if !valid {
			conditions[string(k8s.ListenerConditionResolvedRefs)] = &condition{
				reason:  string(k8s.ListenerReasonInvalidRouteKinds),
				status:  metav1.ConditionFalse,
				message: "Invalid route kinds",
			}
		}
		gs.Listeners[index] = k8s.ListenerStatus{
			Name:           l.Name,
			AttachedRoutes: 0, // this will be reported later
			SupportedKinds: supported,
			Conditions:     setConditions(obj.Generation, cond, conditions),
		}
		return gs
	})
}

func generateSupportedKinds(l k8s.Listener) ([]k8s.RouteGroupKind, bool) {
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

// This and the following function really belongs in some gateway-api lib
func routeGroupKindEqual(rgk1, rgk2 k8s.RouteGroupKind) bool {
	return rgk1.Kind == rgk2.Kind && getGroup(rgk1) == getGroup(rgk2)
}

func getGroup(rgk k8s.RouteGroupKind) k8s.Group {
	return k8s.Group(defaultIfNil((*string)(rgk.Group), gvk.KubernetesGateway.Group))
}
