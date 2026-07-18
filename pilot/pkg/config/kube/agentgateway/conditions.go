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

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

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

// RouteParentResult holds the result of a route for a specific parent
type RouteParentResult struct {
	// OriginalReference contains the original reference
	OriginalReference k8s.ParentReference
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason *ParentError
	// RouteError, if present, indicates a route-level error (e.g. unresolved backend refs)
	RouteError *Condition
	// ControllerName is the name of the controller that generated the result
	ControllerName string
}

// createRouteStatus builds the RouteParentStatus slice from route parent results.
// It deduplicates parents by OriginalReference, ranks errors by severity,
// merges with existing status, and produces deterministic output.
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
		if p.ControllerName != constants.ManagedAgentgatewayController &&
			p.ControllerName != constants.ManagedAgentgatewayWaypointController {
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
				s.ControllerName == k8s.GatewayController(gw.ControllerName)
		})
		if cs != nil {
			currentConditions = cs.Conditions
		}
		ns := k8s.RouteParentStatus{
			ParentRef:      gw.OriginalReference,
			ControllerName: k8s.GatewayController(gw.ControllerName),
			Conditions:     gatewaycommon.SetListenerConditions(generation, currentConditions, toSharedConditions(conds)),
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
	case *k8s.TCPRoute:
		return t.Status.Parents
	case *k8s.TLSRoute:
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
