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
	"net/netip"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/ambient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/sets"
)

// ServiceWaypointResolver returns the k8s Gateway namespaced names of every waypoint fronting a
// service (both primary and any canary). Registers reactive krt dependencies on the underlying
// Waypoints collection so downstream collections re-run when a waypoint's address changes.
type ServiceWaypointResolver func(ctx krt.HandlerContext, svc model.ServiceInfo) []types.NamespacedName

// waypointIPKey is the composite key used to look up waypoints by their address. Kept local to
// agentgateway since only the resolver needs it.
type waypointIPKey struct {
	network string
	ip      string
}

// NewServiceWaypointResolver builds a ServiceWaypointResolver that maps the GatewayAddresses
// stamped onto a ServiceInfo by ambient's ServicesCollection (via Waypoint / WeightedWaypoints)
// back to the k8s Gateway identities that AGW listeners are keyed by.
//
// TODO(agw-multicluster): in multicluster deployments ambient.Index.Waypoints() currently returns
// only LocalWaypoints (see the TODO in pilot/pkg/serviceregistry/ambient/multicluster.go where
// a.waypoints is assigned). A service whose Waypoint/WeightedWaypoint destination points at a
// waypoint that lives in a remote cluster will not resolve here, and AGW will silently emit no
// binding. Track and fix once ambient exposes global waypoints (flattened) via Waypoints().
func NewServiceWaypointResolver(waypoints krt.Collection[ambient.Waypoint]) ServiceWaypointResolver {
	byHostname := krt.NewIndex(waypoints, "byHostname", func(w ambient.Waypoint) []ambient.NamespaceHostname {
		h := w.Address.GetHostname()
		if h == nil {
			return nil
		}
		return []ambient.NamespaceHostname{{Namespace: h.Namespace, Hostname: h.Hostname}}
	})
	// TODO(agw-scoping): byIP collides for two Gateways in different namespaces sharing an
	// externally assigned LoadBalancer IP. The workloadapi GatewayAddress IP form does not carry
	// the Gateway namespace, so we cannot disambiguate here. Rare in practice, but the resolver
	// may return both Gateways in that case; downstream will emit a WaypointServiceBinding for
	// each. Prefer hostname-based waypoint status if this matters.
	byIP := krt.NewIndex(waypoints, "byIP", func(w ambient.Waypoint) []waypointIPKey {
		na := w.Address.GetAddress()
		if na == nil {
			return nil
		}
		ip, ok := netip.AddrFromSlice(na.Address)
		if !ok {
			return nil
		}
		return []waypointIPKey{{network: na.Network, ip: ip.String()}}
	})
	return func(ctx krt.HandlerContext, svc model.ServiceInfo) []types.NamespacedName {
		addrs := ambient.ServiceOwningWaypoints(svc)
		if len(addrs) == 0 {
			return nil
		}
		seen := sets.New[types.NamespacedName]()
		var out []types.NamespacedName
		for _, addr := range addrs {
			if h := addr.GetHostname(); h != nil {
				for _, wp := range krt.Fetch(ctx, waypoints, krt.FilterIndex(byHostname,
					ambient.NamespaceHostname{Namespace: h.Namespace, Hostname: h.Hostname})) {
					nn := types.NamespacedName{Namespace: wp.GetNamespace(), Name: wp.GetName()}
					if !seen.InsertContains(nn) {
						out = append(out, nn)
					}
				}
				continue
			}
			if na := addr.GetAddress(); na != nil {
				ip, ok := netip.AddrFromSlice(na.Address)
				if !ok {
					continue
				}
				for _, wp := range krt.Fetch(ctx, waypoints, krt.FilterIndex(byIP,
					waypointIPKey{network: na.Network, ip: ip.String()})) {
					nn := types.NamespacedName{Namespace: wp.GetNamespace(), Name: wp.GetName()}
					if !seen.InsertContains(nn) {
						out = append(out, nn)
					}
				}
			}
		}
		return out
	}
}
