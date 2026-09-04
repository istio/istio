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

// ServiceWaypointResolver returns the k8s Gateway ns/names of every waypoint fronting a service
// (primary + any canary). Reactive: re-runs when a waypoint's address changes.
type ServiceWaypointResolver func(ctx krt.HandlerContext, svc model.ServiceInfo) []types.NamespacedName

// waypointIPKey keys the byIP index. Local because only the resolver needs it.
type waypointIPKey struct {
	network string
	ip      string
}

// String satisfies fmt.Stringer so krt.NewIndex can key this struct (see krt/index.go toString).
func (k waypointIPKey) String() string {
	return k.network + "/" + k.ip
}

// NewServiceWaypointResolver maps a ServiceInfo back to the k8s Gateways that front it.
//
// Ambient stamps waypoints on ServiceInfo as workloadapi.GatewayAddresses (hostname or IP), not
// as ns/names. Two indexes over the Waypoints collection reverse the lookup.
//
// Example — service ns1/foo has primary waypoint ns1/wp-a and canary waypoint ns1/wp-b:
//
//	ambient.ServiceOwningWaypoints(foo) -> [<addr of wp-a>, <addr of wp-b>]
//	resolver(ctx, foo)                  -> [{ns1, wp-a}, {ns1, wp-b}]
//
// TODO(agw-multicluster): Waypoints() currently returns LocalWaypoints only (see the TODO in
// pilot/pkg/serviceregistry/ambient/multicluster.go). Services whose waypoint destination lives
// in a remote cluster resolve to nothing and AGW silently emits no binding.
func NewServiceWaypointResolver(waypoints krt.Collection[ambient.Waypoint]) ServiceWaypointResolver {
	byHostname := krt.NewIndex(waypoints, "byHostname", func(w ambient.Waypoint) []ambient.NamespaceHostname {
		h := w.Address.GetHostname()
		if h == nil {
			return nil
		}
		return []ambient.NamespaceHostname{{Namespace: h.Namespace, Hostname: h.Hostname}}
	})
	// TODO(agw-scoping): byIP is only ns-safe if every Waypoint has a Hostname status address (the
	// stock case). Two IP-only Gateways in different namespaces sharing an IP would both match.
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
		// Primary alone, or the weighted set (primary + canary); never both.
		addrs := ambient.ServiceOwningWaypoints(svc)
		if len(addrs) == 0 {
			return nil
		}
		// Dedupe: two addresses can still resolve to the same Gateway on IP collision or when
		// primary and canary reach the same Waypoint via different address types.
		seen := sets.New[types.NamespacedName]()
		var out []types.NamespacedName
		for _, addr := range addrs {
			// Destination is a proto oneof: hostname XOR IP.
			if h := addr.GetHostname(); h != nil {
				// Fetch, not FetchOne — a hostname index bucket may hold multiple Waypoints.
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
					continue // malformed proto bytes; ambient shouldn't emit these
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
