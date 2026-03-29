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
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1"
	k8salpha "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// setConditions sets the existingConditions with the new conditions
func setConditions(generation int64, existingConditions []metav1.Condition, conditions map[string]*Condition) []metav1.Condition {
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

// generateSupportedKinds generates the supported kinds for a listener based on its protocol and allowedRoutes.
// The boolean return indicates whether all allowed routes were supported.
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

func reportListenerCondition(index int, l k8s.Listener, obj controllers.Object,
	statusListeners []k8s.ListenerStatus, conditions map[string]*Condition,
) []k8s.ListenerStatus {
	for index >= len(statusListeners) {
		statusListeners = append(statusListeners, k8s.ListenerStatus{})
	}
	cond := statusListeners[index].Conditions
	supported, valid := generateSupportedKinds(l)
	if !valid {
		conditions[string(k8s.ListenerConditionResolvedRefs)] = &Condition{
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

func FilterInPlaceByIndex[E any](s []E, keep func(int) bool) []E {
	i := 0
	for j := range s {
		if keep(j) {
			s[i] = s[j]
			i++
		}
	}

	clear(s[i:]) // zero/nil out the obsolete elements, for GC
	return s[:i]
}

// RouteParentResult holds the result of a route for a specific parent
type RouteParentResult struct {
	// OriginalReference contains the original reference
	OriginalReference k8s.ParentReference
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason *ParentError
	// RouteError, if present, indicates a route-level error (e.g. unresolved backend refs)
	RouteError *Condition
}

// createRouteStatus builds the RouteParentStatus slice from route parent results.
// It deduplicates parents by OriginalReference, ranks errors by severity,
// merges with existing status, and produces deterministic output.
func createRouteStatus(
	parentResults []RouteParentResult,
	objectNamespace string,
	generation int64,
	controllerName string,
	currentParents []k8s.RouteParentStatus,
) []k8s.RouteParentStatus {
	parents := slices.Clone(currentParents)
	parentIndexes := map[string]int{}
	for idx, p := range parents {
		// Only consider our own
		if p.ControllerName != k8s.GatewayController(controllerName) {
			continue
		}
		rs := parentRefStringWithNS(p.ParentRef, objectNamespace)
		if _, f := parentIndexes[rs]; f {
			log.Warnf("invalid route detected: duplicate parent: %v", rs)
		} else {
			parentIndexes[rs] = idx
		}
	}

	// Collect unique parent references. There may be multiple when a route without section name
	// references a parent with multiple sections.
	seen := map[k8s.ParentReference][]RouteParentResult{}
	successCount := map[k8s.ParentReference]int{}
	for _, incoming := range parentResults {
		if incoming.DeniedReason == nil {
			successCount[incoming.OriginalReference]++
		}
		seen[incoming.OriginalReference] = append(seen[incoming.OriginalReference], incoming)
	}

	const (
		rankNoErrors = iota
		rankNotAllowed
		rankNoHostname
		rankParentRefConflict
		rankNotAccepted
	)

	rankError := func(result RouteParentResult) int {
		if result.DeniedReason == nil {
			return rankNoErrors
		}
		switch result.DeniedReason.Reason {
		case ParentErrorNotAllowed:
			return rankNotAllowed
		case ParentErrorNoHostname:
			return rankNoHostname
		case ParentErrorParentRefConflict:
			return rankParentRefConflict
		case ParentErrorNotAccepted:
			return rankNotAccepted
		}
		return rankNoErrors
	}

	// Collapse to one result per parent ref, keeping the most severe error.
	report := map[k8s.ParentReference]RouteParentResult{}
	for ref, results := range seen {
		if len(results) == 0 {
			continue
		}
		toReport := results[0]
		mostSevere := rankError(toReport)
		for _, result := range results[1:] {
			rank := rankError(result)
			if rank < mostSevere {
				mostSevere = rank
				toReport = result
			} else if rank == mostSevere && toReport.DeniedReason != nil && result.DeniedReason != nil {
				toReport.DeniedReason.Message += "; " + result.DeniedReason.Message
			}
		}
		report[ref] = toReport
	}

	// Build status for our parents
	var toAppend []k8s.RouteParentStatus
	for k, gw := range report {
		msg := "Route was valid"
		if successCount[k] > 1 {
			msg = fmt.Sprintf("Route was valid, bound to %d parents", successCount[k])
		}
		conds := map[string]*Condition{
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
			conds[string(k8s.RouteConditionResolvedRefs)] = gw.RouteError
		}
		if gw.DeniedReason != nil {
			conds[string(k8s.RouteConditionAccepted)].error = &ConfigError{
				Reason:  ConfigErrorReason(gw.DeniedReason.Reason),
				Message: gw.DeniedReason.Message,
			}
		}

		myRef := parentRefStringWithNS(gw.OriginalReference, objectNamespace)
		var currentConditions []metav1.Condition
		cs := slices.FindFunc(currentParents, func(s k8s.RouteParentStatus) bool {
			return parentRefStringWithNS(s.ParentRef, objectNamespace) == myRef &&
				s.ControllerName == k8s.GatewayController(controllerName)
		})
		if cs != nil {
			currentConditions = cs.Conditions
		}
		ns := k8s.RouteParentStatus{
			ParentRef:      gw.OriginalReference,
			ControllerName: k8s.GatewayController(controllerName),
			Conditions:     setConditions(generation, currentConditions, conds),
		}
		if idx, f := parentIndexes[myRef]; f {
			parents[idx] = ns
			delete(parentIndexes, myRef)
		} else {
			toAppend = append(toAppend, ns)
		}
	}

	// Sort for deterministic output
	sort.SliceStable(toAppend, func(i, j int) bool {
		return parentRefStringWithNS(toAppend[i].ParentRef, objectNamespace) > parentRefStringWithNS(toAppend[j].ParentRef, objectNamespace)
	})
	parents = append(parents, toAppend...)

	// Remove stale entries that we previously owned but no longer report
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

// parentRefStringWithNS generates a unique key for a ParentReference, defaulting namespace.
func parentRefStringWithNS(ref k8s.ParentReference, objectNamespace string) string {
	ns := objectNamespace
	if ref.Namespace != nil {
		ns = string(*ref.Namespace)
	}
	g := gvk.KubernetesGateway.Group
	if ref.Group != nil {
		g = string(*ref.Group)
	}
	k := gvk.KubernetesGateway.Kind
	if ref.Kind != nil {
		k = string(*ref.Kind)
	}
	var sn string
	if ref.SectionName != nil {
		sn = string(*ref.SectionName)
	}
	var port int
	if ref.Port != nil {
		port = int(*ref.Port)
	}
	return fmt.Sprintf("%s/%s/%s/%s/%d.%s", g, k, ref.Name, sn, port, ns)
}

// GetCommonRouteStateParents extracts the current status parents from a route object.
func GetCommonRouteStateParents(spec any) []k8s.RouteParentStatus {
	switch t := spec.(type) {
	case *k8salpha.TCPRoute:
		return t.Status.Parents
	case *k8salpha.TLSRoute:
		return t.Status.Parents
	case *k8s.HTTPRoute:
		return t.Status.Parents
	case *k8s.GRPCRoute:
		return t.Status.Parents
	default:
		log.Fatalf("unknown type %T", t)
		return nil
	}
}
