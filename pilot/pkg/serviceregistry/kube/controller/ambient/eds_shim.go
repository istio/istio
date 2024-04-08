// nolint: gocritic
package ambient

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/workloadapi"
)

type serviceEDS struct{}
type workloadEDS struct {
	WorkloadName     string
	TunnelProtocol   workloadapi.TunnelProtocol
	WaypointKey      *workloadapi.GatewayAddress
	WaypointInstance []*workloadapi.Workload
}

func (w workloadEDS) ResourceName() string {
	return w.WorkloadName
}

func RegisterEdsShim(
	Workloads krt.Collection[model.WorkloadInfo],
	Services krt.Collection[model.ServiceInfo],
	Waypoints krt.Collection[Waypoint],
) {
	WorkloadEds := krt.NewCollection()
		Workloads,
		func(ctx krt.HandlerContext, wl model.WorkloadInfo) *workloadEDS {
			if wl.Waypoint == nil {
				return nil
			}
			waypoints := waypoints
		}
		krt.WithName("WorkloadEds"))
}
