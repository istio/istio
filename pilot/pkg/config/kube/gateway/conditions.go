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
	k8s "sigs.k8s.io/gateway-api/apis/v1"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// RouteParentResult holds the result of a route for a specific parent
type RouteParentResult struct {
	// OriginalReference contains the original reference
	OriginalReference k8s.ParentReference
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason *ParentError
	// RouteError, if present, indicates why the reference was not valid
	RouteError *ConfigError
	// WaypointError, if present, indicates why the reference was does not have a waypoint
	WaypointError *WaypointError
}

func createRouteStatus(
	parentResults []RouteParentResult,
	objectNamespace string,
	generation int64,
	currentParents []k8s.RouteParentStatus,
) []k8s.RouteParentStatus {
	parents := slices.Clone(currentParents)
	parentIndexes := map[string]int{}
	for idx, p := range parents {
		// Only consider our own
		if p.ControllerName != k8s.GatewayController(features.ManagedGatewayController) {
			continue
		}
		rs := parentRefString(p.ParentRef, objectNamespace)
		if _, f := parentIndexes[rs]; f {
			log.Warnf("invalid HTTPRoute detected: duplicate parent: %v", rs)
		} else {
			parentIndexes[rs] = idx
		}
	}

	// Collect all of our unique parent references. There may be multiple when we have a route without section name,
	// but reference a parent with multiple sections.
	// While we process these internally for-each sectionName, in the status we are just supposed to report one merged entry
	seen := map[k8s.ParentReference][]RouteParentResult{}
	seenReasons := sets.New[ParentErrorReason]()
	successCount := map[k8s.ParentReference]int{}
	for _, incoming := range parentResults {
		// We will append it if it is our first occurrence, or the existing one has an error. This means
		// if *any* section has no errors, we will declare Admitted
		if incoming.DeniedReason == nil {
			successCount[incoming.OriginalReference]++
		}
		seen[incoming.OriginalReference] = append(seen[incoming.OriginalReference], incoming)
		if incoming.DeniedReason != nil {
			seenReasons.Insert(incoming.DeniedReason.Reason)
		} else {
			seenReasons.Insert(ParentNoError)
		}
	}

	const (
		rankParentNoErrors = iota
		rankParentErrorNotAllowed
		rankParentErrorNoHostname
		rankParentErrorParentRefConflict
		rankParentErrorNotAccepted
	)

	rankParentError := func(result RouteParentResult) int {
		if result.DeniedReason == nil {
			return rankParentNoErrors
		}
		switch result.DeniedReason.Reason {
		case ParentErrorNotAllowed:
			return rankParentErrorNotAllowed
		case ParentErrorNoHostname:
			return rankParentErrorNoHostname
		case ParentErrorParentRefConflict:
			return rankParentErrorParentRefConflict
		case ParentErrorNotAccepted:
			return rankParentErrorNotAccepted
		}
		return rankParentNoErrors
	}

	// Next we want to collapse these. We need to report 1 type of error, or none.
	report := map[k8s.ParentReference]RouteParentResult{}
	for ref, results := range seen {
		if len(results) == 0 {
			continue
		}

		toReport := results[0]
		mostSevereRankSeen := rankParentError(toReport)

		for _, result := range results[1:] {
			resultRank := rankParentError(result)
			// lower number means more severe
			if resultRank < mostSevereRankSeen {
				mostSevereRankSeen = resultRank
				toReport = result
			} else if resultRank == mostSevereRankSeen {
				// join the error messages
				if toReport.DeniedReason == nil {
					toReport.DeniedReason = result.DeniedReason
				} else {
					toReport.DeniedReason.Message += "; " + result.DeniedReason.Message
				}
			}
		}

		report[ref] = toReport
	}

	// Now we fill in all the parents we do own
	var toAppend []k8s.RouteParentStatus
	for k, gw := range report {
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
		if gw.RouteError != nil {
			// Currently, the spec is not clear on where errors should be reported. The provided resources are:
			// * Accepted - used to describe errors binding to parents
			// * ResolvedRefs - used to describe errors about binding to objects
			// But no general errors
			// For now, we will treat all general route errors as "Ref" errors.
			conds[string(k8s.RouteConditionResolvedRefs)].error = gw.RouteError
		}
		if gw.DeniedReason != nil {
			conds[string(k8s.RouteConditionAccepted)].error = &ConfigError{
				Reason:  ConfigErrorReason(gw.DeniedReason.Reason),
				Message: gw.DeniedReason.Message,
			}
		}

		// when ambient is enabled, report the waypoints resolved condition
		if features.EnableAmbient {
			cond := &condition{
				reason:  string(RouteReasonResolvedWaypoints),
				message: "All waypoints resolved",
			}
			if gw.WaypointError != nil {
				cond.message = gw.WaypointError.Message
			}
			conds[string(RouteConditionResolvedWaypoints)] = cond
		}

		myRef := parentRefString(gw.OriginalReference, objectNamespace)
		var currentConditions []metav1.Condition
		currentStatus := slices.FindFunc(currentParents, func(s k8s.RouteParentStatus) bool {
			return parentRefString(s.ParentRef, objectNamespace) == myRef &&
				s.ControllerName == k8s.GatewayController(features.ManagedGatewayController)
		})
		if currentStatus != nil {
			currentConditions = currentStatus.Conditions
		}
		ns := k8s.RouteParentStatus{
			ParentRef:      gw.OriginalReference,
			ControllerName: k8s.GatewayController(features.ManagedGatewayController),
			Conditions:     setConditions(generation, currentConditions, conds),
		}
		// Parent ref already exists, insert in the same place
		if idx, f := parentIndexes[myRef]; f {
			parents[idx] = ns
			// Clear it out so we can detect which ones we need to delete later
			delete(parentIndexes, myRef)
		} else {
			// Else queue it up to append to the end. We don't append now since we will want to sort them.
			toAppend = append(toAppend, ns)
		}
	}
	// Ensure output is deterministic.
	// TODO: will we fight over other controllers doing similar (but not identical) ordering?
	sort.SliceStable(toAppend, func(i, j int) bool {
		return parentRefString(toAppend[i].ParentRef, objectNamespace) > parentRefString(toAppend[j].ParentRef, objectNamespace)
	})
	parents = append(parents, toAppend...)
	toDelete := sets.New(maps.Values(parentIndexes)...)
	parents = FilterInPlaceByIndex(parents, func(i int) bool {
		_, f := toDelete[i]
		return !f
	})

	if parents == nil {
		return []k8s.RouteParentStatus{}
	}
	return parents
}

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

type WaypointError struct {
	Reason  WaypointErrorReason
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

func reportListenerCondition(index int, l k8s.Listener, obj *k8sbeta.Gateway,
	gs *k8sbeta.GatewayStatus, conditions map[string]*condition,
) {
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
