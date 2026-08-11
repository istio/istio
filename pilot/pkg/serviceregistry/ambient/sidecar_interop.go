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

// nolint: gocritic
package ambient

import (
	"bytes"
	"net/netip"
	"strings"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/workloadapi"
)

// serviceEDS captures the inputs that affect gateway EDS for a waypoint-bound service.
type serviceEDS struct {
	ServiceKey       string
	WaypointInstance []model.WorkloadInfo
	UseWaypoint      bool
	// Included so weight changes trigger EDS for the bound service.
	WeightedWaypoints []*workloadapi.WeightedWaypoint
}

func (s serviceEDS) ResourceName() string {
	return s.ServiceKey
}

func (s serviceEDS) Equals(other serviceEDS) bool {
	if s.ServiceKey != other.ServiceKey {
		return false
	}
	if s.UseWaypoint != other.UseWaypoint {
		return false
	}
	if len(s.WaypointInstance) != len(other.WaypointInstance) {
		return false
	}
	// assumes builder sorted the slices
	for i := range s.WaypointInstance {
		if !s.WaypointInstance[i].Equals(other.WaypointInstance[i]) {
			return false
		}
	}
	if len(s.WeightedWaypoints) != len(other.WeightedWaypoints) {
		return false
	}
	// assumes builder preserved order; compare the fields cheaply rather than proto.Equal on the
	// EDS hot path.
	for i := range s.WeightedWaypoints {
		if !weightedWaypointEqual(s.WeightedWaypoints[i], other.WeightedWaypoints[i]) {
			return false
		}
	}
	return true
}

// weightedWaypointEqual compares the fields that affect gateway EDS: the weight and the destination
// waypoint (hostname or network address, plus HBONE port). The getters are nil-safe, so this covers
// either GatewayAddress oneof case.
func weightedWaypointEqual(a, b *workloadapi.WeightedWaypoint) bool {
	if a.GetWeight() != b.GetWeight() {
		return false
	}
	ad, bd := a.GetDestination(), b.GetDestination()
	if ad.GetHboneMtlsPort() != bd.GetHboneMtlsPort() {
		return false
	}
	if ad.GetHostname().GetNamespace() != bd.GetHostname().GetNamespace() ||
		ad.GetHostname().GetHostname() != bd.GetHostname().GetHostname() {
		return false
	}
	aa, ba := ad.GetAddress(), bd.GetAddress()
	return aa.GetNetwork() == ba.GetNetwork() && bytes.Equal(aa.GetAddress(), ba.GetAddress())
}

// RegisterEdsShim handles triggering xDS events when Envoy EDS needs to change.
// Most of ambient index works to build `workloadapi` types - Workload, Service, etc.
// Envoy uses a different API, with different relationships between types.
// To ensure Envoy are updated properly on changes, we compute this information.
// Currently, this is only used to trigger events.
// Ideally, the information we are using in Envoy and the event trigger are using the same data directly.
func RegisterEdsShim(
	xdsUpdater model.XDSUpdater,
	Workloads krt.Collection[model.WorkloadInfo],
	Namespaces krt.Collection[model.NamespaceInfo],
	WorkloadsByServiceKey krt.Index[string, model.WorkloadInfo],
	Services krt.Collection[model.ServiceInfo],
	ServicesByAddress krt.Index[networkAddress, model.ServiceInfo],
	opts krt.OptionsBuilder,
) {
	// Helps us avoid race conditions in tests.
	// Also, this flag shouldn't change once initialized, so this
	// has slightly better cache locality.
	multiNetworkEnabled := features.EnableAmbientMultiNetwork
	ServiceEds := krt.NewCollection(
		Services,
		func(ctx krt.HandlerContext, svc model.ServiceInfo) *serviceEDS {
			useWaypoint := ingressUseWaypoint(svc, krt.FetchOne(ctx, Namespaces, krt.FilterKey(svc.Service.Namespace)))
			if !useWaypoint && (!multiNetworkEnabled || svc.Scope != model.Global) {
				// Currently, we only need this for ingress/east west gateway -> waypoint usage
				// If we extend this to sidecars, etc we can drop this.
				return nil
			}
			if svc.Service.Waypoint == nil {
				return nil
			}
			// Track every waypoint whose endpoints can appear in the gateway CLA.
			var workloads []model.WorkloadInfo
			for _, wp := range serviceOwningWaypoints(svc) {
				var waypointServiceKey string
				switch addr := wp.Destination.(type) {
				case *workloadapi.GatewayAddress_Hostname:
					hn := addr.Hostname
					waypointServiceKey = namespacedHostname(hn.Namespace, hn.Hostname)
				case *workloadapi.GatewayAddress_Address:
					wAddress := addr.Address
					serviceKey := networkAddress{
						network: wAddress.Network,
						ip:      mustByteIPToString(wAddress.Address),
					}
					waypointSvc := krt.FetchOne(ctx, Services, krt.FilterIndex(ServicesByAddress, serviceKey))
					if waypointSvc == nil {
						continue
					}
					waypointServiceKey = waypointSvc.ResourceName()
				}
				workloads = append(workloads, WorkloadsByServiceKey.Fetch(ctx, waypointServiceKey)...)
			}
			// for comparison in Equals
			workloads = slices.SortBy(workloads, func(i model.WorkloadInfo) string {
				return i.Workload.Uid
			})
			return &serviceEDS{
				ServiceKey:        svc.ResourceName(),
				UseWaypoint:       useWaypoint,
				WaypointInstance:  workloads,
				WeightedWaypoints: svc.Service.WeightedWaypoints,
			}
		},
		opts.WithName("ServiceEds")...)
	ServiceEds.RegisterBatch(
		PushXds(xdsUpdater, func(svc serviceEDS) model.ConfigKey {
			ns, hostname, _ := strings.Cut(svc.ServiceKey, "/")
			return model.ConfigKey{Kind: kind.Endpoints, Name: hostname, Namespace: ns}
		}), false)
}

func (a *index) ServicesWithWaypoint(key string) []model.ServiceWaypointInfo {
	res := []model.ServiceWaypointInfo{}
	var svcs []model.ServiceInfo
	if key == "" {
		svcs = a.services.List()
	} else {
		svcs = ptr.ToList(a.services.GetKey(key))
	}
	for _, s := range svcs {
		wp := s.Service.GetWaypoint()
		useWaypoint := ingressUseWaypoint(s, a.namespaces.GetKey(s.Service.Namespace))
		wi := model.ServiceWaypointInfo{
			Service:            s.Service,
			IngressUseWaypoint: useWaypoint,
		}
		if wp == nil {
			continue
		}
		wi.WaypointHostname = a.resolveWaypointHostname(wp)
		if wi.WaypointHostname == "" {
			// Primary waypoint address (an IP) resolved to no known waypoint service.
			continue
		}
		// Resolve weighted waypoints for gateway EDS.
		for _, ww := range s.Service.GetWeightedWaypoints() {
			h := a.resolveWaypointHostname(ww.GetDestination())
			if h == "" {
				continue
			}
			wi.WeightedWaypoints = append(wi.WeightedWaypoints, model.WeightedWaypointHostname{
				Hostname:      h,
				HboneMtlsPort: ww.GetDestination().GetHboneMtlsPort(),
				Weight:        ww.GetWeight(),
			})
		}
		res = append(res, wi)
	}
	return res
}

// resolveWaypointHostname resolves hostname waypoints directly and IP waypoints via services.
func (a *index) resolveWaypointHostname(wp *workloadapi.GatewayAddress) string {
	switch addr := wp.GetDestination().(type) {
	case *workloadapi.GatewayAddress_Hostname:
		return addr.Hostname.Hostname
	case *workloadapi.GatewayAddress_Address:
		wpAddr, _ := netip.AddrFromSlice(addr.Address.Address)
		waypoints := a.services.ByAddress.Lookup(networkAddress{
			network: addr.Address.Network,
			ip:      wpAddr.String(),
		})
		if len(waypoints) == 0 {
			return ""
		}
		return waypoints[0].Service.Hostname
	}
	return ""
}

func ingressUseWaypoint(s model.ServiceInfo, ns *model.NamespaceInfo) bool {
	if s.Waypoint.IngressLabelPresent {
		return s.Waypoint.IngressUseWaypoint
	}
	if ns != nil {
		return ns.IngressUseWaypoint
	}
	return false
}
