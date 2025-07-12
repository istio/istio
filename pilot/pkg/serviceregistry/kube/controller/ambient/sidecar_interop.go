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

// serviceEDS represents a service and a list of all instances of waypoints.
// For example, we may have ServiceKey=httpbin.org and WaypointInstance=[list of *waypoint workloads* attached].
// This is to map to the eventual EDS structure.
type serviceEDS struct {
	ServiceKey       string
	WaypointInstance []*workloadapi.Workload
	UseWaypoint      bool
}

func (w serviceEDS) ResourceName() string {
	return w.ServiceKey
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
	ServiceEds := krt.NewCollection(
		Services,
		func(ctx krt.HandlerContext, svc model.ServiceInfo) *serviceEDS {
			useWaypoint := ingressUseWaypoint(svc, krt.FetchOne(ctx, Namespaces, krt.FilterKey(svc.Service.Namespace)))
			if !useWaypoint && (!features.EnableAmbientMultiNetwork || svc.Scope != model.Global) {
				// Currently, we only need this for ingress/east west gateway -> waypoint usage
				// If we extend this to sidecars, etc we can drop this.
				return nil
			}
			wp := svc.Service.Waypoint
			if wp == nil {
				return nil
			}
			var waypointServiceKey string
			switch addr := wp.Destination.(type) {
			// Easy case: waypoint is already a hostname. Just return it directly
			case *workloadapi.GatewayAddress_Hostname:
				hn := addr.Hostname
				waypointServiceKey = hn.Namespace + "/" + hn.Hostname
			// Hard case: waypoint is an IP address. Need to look it up.
			case *workloadapi.GatewayAddress_Address:
				wAddress := addr.Address
				serviceKey := networkAddress{
					network: wAddress.Network,
					ip:      mustByteIPToString(wAddress.Address),
				}
				waypointSvc := krt.FetchOne(ctx, Services, krt.FilterIndex(ServicesByAddress, serviceKey))
				if waypointSvc == nil {
					return nil
				}
				waypointServiceKey = waypointSvc.ResourceName()
			}
			workloads := krt.Fetch(ctx, Workloads, krt.FilterIndex(WorkloadsByServiceKey, waypointServiceKey))
			return &serviceEDS{
				ServiceKey:  svc.ResourceName(),
				UseWaypoint: useWaypoint,
				WaypointInstance: slices.Map(workloads, func(e model.WorkloadInfo) *workloadapi.Workload {
					return e.Workload
				}),
			}
		},
		opts.WithName("ServiceEds")...)
	ServiceEds.RegisterBatch(
		PushXds(xdsUpdater, func(svc serviceEDS) model.ConfigKey {
			ns, hostname, _ := strings.Cut(svc.ServiceKey, "/")
			return model.ConfigKey{Kind: kind.ServiceEntry, Name: hostname, Namespace: ns}
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
		switch addr := wp.GetDestination().(type) {
		// Easy case: waypoint is already a hostname. Just return it directly
		case *workloadapi.GatewayAddress_Hostname:
			wi.WaypointHostname = addr.Hostname.Hostname
		// Hard case: waypoint is an IP address. Need to look it up.
		case *workloadapi.GatewayAddress_Address:
			wpAddr, _ := netip.AddrFromSlice(addr.Address.Address)
			wpNetAddr := networkAddress{
				network: addr.Address.Network,
				ip:      wpAddr.String(),
			}
			waypoints := a.services.ByAddress.Lookup(wpNetAddr)
			if len(waypoints) == 0 {
				// No waypoint found.
				continue
			}
			wi.WaypointHostname = waypoints[0].Service.Hostname
		}
		res = append(res, wi)
	}
	return res
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
