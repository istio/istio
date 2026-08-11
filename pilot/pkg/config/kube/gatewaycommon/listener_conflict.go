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
	"sort"

	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// mergedListener is a listener plus its owning Gateway or ListenerSet.
type mergedListener struct {
	listener gatewayv1.Listener
	owner    listenerOwner
}

// listenerOwner identifies the Gateway or ListenerSet that owns a merged listener.
type listenerOwner struct {
	kind      string
	namespace string
	name      string
}

// EligibleListenerSetsForConflict filters attached ListenerSets to those allowed on the Gateway.
// Ineligible sets are excluded so they cannot mark allowed listeners as conflicted.
func EligibleListenerSetsForConflict(
	attached []*gatewayv1.ListenerSet,
	namespaceAllowed func(namespace string) bool,
) []*gatewayv1.ListenerSet {
	return slices.Filter(attached, func(set *gatewayv1.ListenerSet) bool {
		return set != nil && (namespaceAllowed == nil || namespaceAllowed(set.Namespace))
	})
}

// ComputeGatewayListenerConflicts merges Gateway and ListenerSet listeners in precedence order
// and returns conflict reasons for listeners that lose to an earlier listener.
//
// Conflict checks compare only against winning (accepted) listeners. A Gateway listener that
// loses to another Gateway listener is recorded as conflicted but is not added to accepted,
// so it does not block a later ListenerSet listener unless that ListenerSet listener also
// conflicts with a surviving winner. This matches Gateway API merge semantics: only listeners
// that win precedence are programmed; conflicted listeners free their slot.
func ComputeGatewayListenerConflicts(
	parent *gatewayv1.Gateway,
	eligible []*gatewayv1.ListenerSet,
) map[types.NamespacedName]map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason {
	if parent == nil {
		return nil
	}
	ordered := orderedMergedListeners(parent, eligible)
	conflicts := map[types.NamespacedName]map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason{}
	accepted := make([]gatewayv1.Listener, 0, len(ordered))
	for _, entry := range ordered {
		if prior, ok := firstConflictingPrior(entry.listener, accepted); ok {
			recordListenerConflict(conflicts, entry.owner, entry.listener.Name, listenerConflictReason(entry.listener, prior))
			continue
		}
		accepted = append(accepted, entry.listener)
	}
	return conflicts
}

// firstConflictingPrior returns the earliest accepted listener that conflicts with listener, if any.
func firstConflictingPrior(listener gatewayv1.Listener, accepted []gatewayv1.Listener) (gatewayv1.Listener, bool) {
	idx := slices.IndexFunc(accepted, func(prior gatewayv1.Listener) bool {
		return !listenersDistinct(listener, prior)
	})
	if idx == -1 {
		return gatewayv1.Listener{}, false
	}
	return accepted[idx], true
}

// recordListenerConflict records a conflict reason for a Gateway or ListenerSet listener.
func recordListenerConflict(
	conflicts map[types.NamespacedName]map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason,
	owner listenerOwner,
	name gatewayv1.SectionName,
	reason gatewayv1.ListenerConditionReason,
) {
	nsn := types.NamespacedName{Namespace: owner.namespace, Name: owner.name}
	if conflicts[nsn] == nil {
		conflicts[nsn] = map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason{}
	}
	conflicts[nsn][name] = reason
}

// orderedMergedListeners returns Gateway listeners followed by ListenerSet listeners in merge precedence order.
func orderedMergedListeners(parent *gatewayv1.Gateway, attached []*gatewayv1.ListenerSet) []mergedListener {
	capacity := len(parent.Spec.Listeners)
	for _, set := range attached {
		if set != nil {
			capacity += len(set.Spec.Listeners)
		}
	}
	out := make([]mergedListener, 0, capacity)
	for _, l := range parent.Spec.Listeners {
		out = append(out, mergedListener{
			listener: l,
			owner: listenerOwner{
				kind:      "Gateway",
				namespace: parent.Namespace,
				name:      parent.Name,
			},
		})
	}

	sets := append([]*gatewayv1.ListenerSet(nil), attached...)
	sort.SliceStable(sets, func(i, j int) bool {
		left := sets[i].CreationTimestamp.Time
		right := sets[j].CreationTimestamp.Time
		if !left.Equal(right) {
			return left.Before(right)
		}
		leftKey := sets[i].Namespace + "/" + sets[i].Name
		rightKey := sets[j].Namespace + "/" + sets[j].Name
		return leftKey < rightKey
	})

	for _, set := range sets {
		owner := listenerOwner{
			kind:      "ListenerSet",
			namespace: set.Namespace,
			name:      set.Name,
		}
		for _, l := range set.Spec.Listeners {
			listener := ConvertListenerSetToListener(l)
			if port, err := ListenerEntryPortNumber(l); err == nil {
				listener.Port = port
			}
			out = append(out, mergedListener{
				listener: listener,
				owner:    owner,
			})
		}
	}
	return out
}

// listenerHostname returns the hostname used for distinctness checks, defaulting unset to "*".
func listenerHostname(l gatewayv1.Listener) string {
	// Unset hostname matches all hostnames, consistent with buildHostnameMatch.
	if l.Hostname == nil || string(*l.Hostname) == "" {
		return "*"
	}
	return string(*l.Hostname)
}

// isTCPUDP reports whether the protocol is TCP or UDP.
func isTCPUDP(p gatewayv1.ProtocolType) bool {
	return p == gatewayv1.TCPProtocolType || p == gatewayv1.UDPProtocolType
}

// isHTTPLike reports whether the protocol is HTTP, HTTPS, or TLS.
func isHTTPLike(p gatewayv1.ProtocolType) bool {
	switch p {
	case gatewayv1.HTTPProtocolType, gatewayv1.HTTPSProtocolType, gatewayv1.TLSProtocolType:
		return true
	default:
		return false
	}
}

// listenersDistinct reports whether two listeners can coexist on the same Gateway.
func listenersDistinct(a, b gatewayv1.Listener) bool {
	if a.Port != b.Port {
		return true
	}
	if isTCPUDP(a.Protocol) && isHTTPLike(b.Protocol) || isHTTPLike(a.Protocol) && isTCPUDP(b.Protocol) {
		return false
	}
	if isTCPUDP(a.Protocol) && isTCPUDP(b.Protocol) {
		return false
	}
	if isHTTPLike(a.Protocol) && isHTTPLike(b.Protocol) {
		// HTTP-like distinctness is per (port, protocol, hostname); different hostnames
		// are distinct even when protocols differ (e.g. HTTPS:443:a.com + TLS:443:b.com).
		return listenerHostname(a) != listenerHostname(b)
	}
	return a.Protocol != b.Protocol
}

// listenerConflictReason returns the Gateway API condition reason for a conflicting listener pair.
func listenerConflictReason(a, b gatewayv1.Listener) gatewayv1.ListenerConditionReason {
	if isTCPUDP(a.Protocol) && isTCPUDP(b.Protocol) {
		// L4 listeners have no hostname; same-port collisions are protocol/port conflicts.
		return gatewayv1.ListenerReasonProtocolConflict
	}
	if a.Protocol != b.Protocol {
		return gatewayv1.ListenerReasonProtocolConflict
	}
	return gatewayv1.ListenerReasonHostnameConflict
}

// ListenerSetParentKey returns the parent Gateway namespace and name for a ListenerSet.
func ListenerSetParentKey(ls *gatewayv1.ListenerSet) (namespace, name string) {
	p := ls.Spec.ParentRef
	pns := ptr.OrDefault(p.Namespace, gatewayv1.Namespace(ls.Namespace))
	return string(pns), string(p.Name)
}
