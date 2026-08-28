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

package ambient

import (
	"net/netip"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/sets"
)

// ServiceWaypointResolver returns the k8s Gateway namespaced names of every waypoint fronting a
// service (both primary and any canary). Registers reactive krt dependencies on the underlying
// Waypoints collection so downstream collections re-run when a waypoint's address changes.
type ServiceWaypointResolver func(ctx krt.HandlerContext, svc model.ServiceInfo) []types.NamespacedName

// NewServiceWaypointResolver builds a ServiceWaypointResolver that maps the GatewayAddresses
// stamped onto a ServiceInfo by ServicesCollection (via Waypoint / WeightedWaypoints) back to the
// k8s Gateway identities that AGW listeners are keyed by. Callers whose only need is
// "which Gateways front this service" should prefer this over walking the raw labels.
func NewServiceWaypointResolver(waypoints krt.Collection[Waypoint], opts krt.OptionsBuilder) ServiceWaypointResolver {
	byHostname := krt.NewIndex(waypoints, "byHostname", func(w Waypoint) []NamespaceHostname {
		h := w.Address.GetHostname()
		if h == nil {
			return nil
		}
		return []NamespaceHostname{{Namespace: h.Namespace, Hostname: h.Hostname}}
	})
	byIP := krt.NewIndex(waypoints, "byIP", func(w Waypoint) []networkAddress {
		na := w.Address.GetAddress()
		if na == nil {
			return nil
		}
		ip, ok := netip.AddrFromSlice(na.Address)
		if !ok {
			return nil
		}
		return []networkAddress{{network: na.Network, ip: ip.String()}}
	})
	return func(ctx krt.HandlerContext, svc model.ServiceInfo) []types.NamespacedName {
		addrs := serviceOwningWaypoints(svc)
		if len(addrs) == 0 {
			return nil
		}
		seen := sets.New[types.NamespacedName]()
		var out []types.NamespacedName
		for _, addr := range addrs {
			if h := addr.GetHostname(); h != nil {
				for _, wp := range krt.Fetch(ctx, waypoints, krt.FilterIndex(byHostname,
					NamespaceHostname{Namespace: h.Namespace, Hostname: h.Hostname})) {
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
					networkAddress{network: na.Network, ip: ip.String()})) {
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
