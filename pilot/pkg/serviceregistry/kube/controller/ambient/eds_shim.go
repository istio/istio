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
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

type serviceEDS struct {
	ServiceKey       string
	WaypointInstance []*workloadapi.Workload
}

func (w serviceEDS) ResourceName() string {
	return w.ServiceKey
}

type workloadEDS struct {
	WorkloadKey      string
	TunnelProtocol   workloadapi.TunnelProtocol
	WaypointInstance []*workloadapi.Workload
	ServiceHostnames []string
}

func (w workloadEDS) ResourceName() string {
	return w.WorkloadKey
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
	WorkloadsByServiceKey *krt.Index[model.WorkloadInfo, string],
	Services krt.Collection[model.ServiceInfo],
	ServicesByAddress *krt.Index[model.ServiceInfo, networkAddress],
	Waypoints krt.Collection[Waypoint],
) {
	// When sending information to a workload, we need two bits of information:
	// * Does it support tunnel? If so, we will need to use HBONE
	// * Does it have a waypoint? if so, we will need to send to the waypoint
	// Record both of these.
	// Note: currently, EDS uses the waypoint VIP for workload waypoints. This is probably something that will change.
	WorkloadEds := krt.NewCollection(
		Workloads,
		func(ctx krt.HandlerContext, wl model.WorkloadInfo) *workloadEDS {
			if wl.Waypoint == nil && wl.TunnelProtocol == workloadapi.TunnelProtocol_NONE {
				return nil
			}
			res := &workloadEDS{
				WorkloadKey:      wl.ResourceName(),
				TunnelProtocol:   wl.TunnelProtocol,
				ServiceHostnames: slices.Sort(maps.Keys(wl.Services)),
			}
			if wl.Waypoint != nil {
				wAddress := wl.Waypoint.GetAddress()
				serviceKey := networkAddress{
					network: wAddress.Network,
					ip:      mustByteIPToString(wAddress.Address),
				}
				svc := krt.FetchOne(ctx, Services, krt.FilterIndex(ServicesByAddress, serviceKey))
				if svc != nil {
					workloads := krt.Fetch(ctx, Workloads, krt.FilterIndex(WorkloadsByServiceKey, svc.ResourceName()))
					res.WaypointInstance = slices.Map(workloads, func(e model.WorkloadInfo) *workloadapi.Workload {
						return e.Workload
					})
				}
			}
			return res
		},
		krt.WithName("WorkloadEds"))
	ServiceEds := krt.NewCollection(
		Services,
		func(ctx krt.HandlerContext, svc model.ServiceInfo) *serviceEDS {
			if svc.Waypoint == nil {
				return nil
			}
			wAddress := svc.Waypoint.GetAddress()
			serviceKey := networkAddress{
				network: wAddress.Network,
				ip:      mustByteIPToString(wAddress.Address),
			}
			waypointSvc := krt.FetchOne(ctx, Services, krt.FilterIndex(ServicesByAddress, serviceKey))
			if waypointSvc == nil {
				return nil
			}
			workloads := krt.Fetch(ctx, Workloads, krt.FilterIndex(WorkloadsByServiceKey, waypointSvc.ResourceName()))
			return &serviceEDS{
				ServiceKey: svc.ResourceName(),
				WaypointInstance: slices.Map(workloads, func(e model.WorkloadInfo) *workloadapi.Workload {
					return e.Workload
				}),
			}
		},
		krt.WithName("ServiceEds"))
	WorkloadEds.RegisterBatch(
		PushXds(xdsUpdater, func(i workloadEDS, updates sets.Set[model.ConfigKey]) {
			for _, svc := range i.ServiceHostnames {
				ns, hostname, _ := strings.Cut(svc, "/")
				updates.Insert(model.ConfigKey{Kind: kind.ServiceEntry, Name: hostname, Namespace: ns})
			}
		}), false)
	ServiceEds.RegisterBatch(
		PushXds(xdsUpdater, func(svc serviceEDS, updates sets.Set[model.ConfigKey]) {
			ns, hostname, _ := strings.Cut(svc.ServiceKey, "/")
			updates.Insert(model.ConfigKey{Kind: kind.ServiceEntry, Name: hostname, Namespace: ns})
		}), false)
}
