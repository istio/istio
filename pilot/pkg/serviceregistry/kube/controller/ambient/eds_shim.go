// nolint: gocritic
package ambient

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	"strings"
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

func RegisterEdsShim(
	xdsUpdater model.XDSUpdater,
	Workloads krt.Collection[model.WorkloadInfo],
	WorkloadsByServiceKey *krt.Index[model.WorkloadInfo, string],
	Services krt.Collection[model.ServiceInfo],
	ServicesByAddress *krt.Index[model.ServiceInfo, networkAddress],
	Waypoints krt.Collection[Waypoint],
) {
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
				if svc == nil {
					log.Fatalf("howardjohn: UNEXPECTED")
				} else {
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
				log.Fatalf("howardjohn: UNEXPECTED")
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
