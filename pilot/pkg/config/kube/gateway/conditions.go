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
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
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
}

func createRouteStatus(parentResults []RouteParentResult, obj config.Config, currentParents []k8s.RouteParentStatus) []k8s.RouteParentStatus {
	parents := make([]k8s.RouteParentStatus, 0, len(parentResults))
	// Fill in all the gateways that are already present but not owned by us. This is non-trivial as there may be multiple
	// gateway controllers that are exposing their status on the same route. We need to attempt to manage ours properly (including
	// removing gateway references when they are removed), without mangling other Controller's status.
	for _, r := range currentParents {
		if r.ControllerName != k8sv1.GatewayController(features.ManagedGatewayController) {
			// We don't own this status, so keep it around
			parents = append(parents, r)
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
	reasonRanking := []ParentErrorReason{
		// No errors is preferred
		ParentNoError,
		// All route level errors
		ParentErrorNotAllowed,
		ParentErrorNoHostname,
		ParentErrorParentRefConflict,
		// Failures to match the Port or SectionName. These are last so that if we bind to 1 listener we
		// just report errors for that 1 listener instead of for all sections we didn't bind to
		ParentErrorNotAccepted,
	}
	// Next we want to collapse these. We need to report 1 type of error, or none.
	report := map[k8s.ParentReference]RouteParentResult{}
	for _, wantReason := range reasonRanking {
		if !seenReasons.Contains(wantReason) {
			continue
		}
		// We found our highest priority ranking, now we need to collapse this into a single message
		for k, refs := range seen {
			for _, ref := range refs {
				reason := ParentNoError
				if ref.DeniedReason != nil {
					reason = ref.DeniedReason.Reason
				}
				if wantReason != reason {
					// Skip this one, it is for a less relevant reason
					continue
				}
				exist, f := report[k]
				if f {
					if ref.DeniedReason != nil {
						if exist.DeniedReason != nil {
							// join the error
							exist.DeniedReason.Message += "; " + ref.DeniedReason.Message
						} else {
							exist.DeniedReason = ref.DeniedReason
						}
					}
				} else {
					exist = ref
				}
				report[k] = exist
			}
		}
		// Once we find the best reason, do not consider any others
		break
	}

	// Now we fill in all the parents we do own
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

		var currentConditions []metav1.Condition
		currentStatus := slices.FindFunc(currentParents, func(s k8sv1.RouteParentStatus) bool {
			return parentRefString(s.ParentRef) == parentRefString(gw.OriginalReference)
		})
		if currentStatus != nil {
			currentConditions = currentStatus.Conditions
		}
		parents = append(parents, k8s.RouteParentStatus{
			ParentRef:      gw.OriginalReference,
			ControllerName: k8sv1.GatewayController(features.ManagedGatewayController),
			Conditions:     setConditions(obj.Generation, currentConditions, conds),
		})
	}
	// Ensure output is deterministic.
	// TODO: will we fight over other controllers doing similar (but not identical) ordering?
	sort.SliceStable(parents, func(i, j int) bool {
		return parentRefString(parents[i].ParentRef) > parentRefString(parents[j].ParentRef)
	})
	return parents
}

type ParentErrorReason string

const (
	ParentErrorNotAccepted       = ParentErrorReason(k8sv1.RouteReasonNoMatchingParent)
	ParentErrorNotAllowed        = ParentErrorReason(k8s.RouteReasonNotAllowedByListeners)
	ParentErrorNoHostname        = ParentErrorReason(k8s.RouteReasonNoMatchingListenerHostname)
	ParentErrorParentRefConflict = ParentErrorReason("ParentRefConflict")
	ParentNoError                = ParentErrorReason("")
)

type ConfigErrorReason = string

const (
	// InvalidRefNotPermitted indicates a route was not permitted
	InvalidRefNotPermitted ConfigErrorReason = ConfigErrorReason(k8s.RouteReasonRefNotPermitted)
	// InvalidDestination indicates an issue with the destination
	InvalidDestination ConfigErrorReason = "InvalidDestination"
	InvalidAddress     ConfigErrorReason = ConfigErrorReason(k8sv1.GatewayReasonUnsupportedAddress)
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
	InvalidTLS ConfigErrorReason = ConfigErrorReason(k8sv1.ListenerReasonInvalidCertificateRef)
	// InvalidListenerRefNotPermitted indicates a listener reference was not permitted
	InvalidListenerRefNotPermitted ConfigErrorReason = ConfigErrorReason(k8sv1.ListenerReasonRefNotPermitted)
	// InvalidConfiguration indicates a generic error for all other invalid configurations
	InvalidConfiguration ConfigErrorReason = "InvalidConfiguration"
	InvalidResources     ConfigErrorReason = ConfigErrorReason(k8sv1.GatewayReasonNoResources)
	DeprecateFieldUsage                    = "DeprecatedField"
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
			conditions[string(k8sv1.ListenerConditionResolvedRefs)] = &condition{
				reason:  string(k8sv1.ListenerReasonInvalidRouteKinds),
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
	case k8sv1.HTTPProtocolType, k8sv1.HTTPSProtocolType:
		// Only terminate allowed, so its always HTTP
		supported = []k8s.RouteGroupKind{
			{Group: (*k8s.Group)(ptr.Of(gvk.HTTPRoute.Group)), Kind: k8s.Kind(gvk.HTTPRoute.Kind)},
			{Group: (*k8s.Group)(ptr.Of(gvk.GRPCRoute.Group)), Kind: k8s.Kind(gvk.GRPCRoute.Kind)},
		}
	case k8sv1.TCPProtocolType:
		supported = []k8s.RouteGroupKind{{Group: (*k8s.Group)(ptr.Of(gvk.TCPRoute.Group)), Kind: k8s.Kind(gvk.TCPRoute.Kind)}}
	case k8sv1.TLSProtocolType:
		if l.TLS != nil && l.TLS.Mode != nil && *l.TLS.Mode == k8sv1.TLSModePassthrough {
			supported = []k8s.RouteGroupKind{{Group: (*k8s.Group)(ptr.Of(gvk.TLSRoute.Group)), Kind: k8s.Kind(gvk.TLSRoute.Kind)}}
		} else {
			supported = []k8s.RouteGroupKind{{Group: (*k8s.Group)(ptr.Of(gvk.TCPRoute.Group)), Kind: k8s.Kind(gvk.TCPRoute.Kind)}}
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
	return ptr.OrDefault(rgk.Group, k8s.Group(gvk.KubernetesGateway.Group))
}
