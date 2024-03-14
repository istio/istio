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
	v1 "k8s.io/api/core/v1"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) ServicesCollection(
	Services krt.Collection[*v1.Service],
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	Waypoints krt.Collection[Waypoint],
	Namespaces krt.Collection[*v1.Namespace],
) krt.Collection[model.ServiceInfo] {
	ServicesInfo := krt.NewCollection(Services, func(ctx krt.HandlerContext, s *v1.Service) *model.ServiceInfo {
		portNames := map[int32]model.ServicePortName{}
		for _, p := range s.Spec.Ports {
			portNames[p.Port] = model.ServicePortName{
				PortName:       p.Name,
				TargetPortName: p.TargetPort.StrVal,
			}
		}
		waypoint := fetchWaypoint(ctx, Waypoints, Namespaces, s.ObjectMeta)
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		return &model.ServiceInfo{
			Service:       a.constructService(s, waypoint),
			PortNames:     portNames,
			LabelSelector: model.NewSelector(s.Spec.Selector),
			Source:        kind.Service,
		}
	}, krt.WithName("ServicesInfo"))
	ServiceEntriesInfo := krt.NewManyCollection(ServiceEntries, func(ctx krt.HandlerContext, s *networkingclient.ServiceEntry) []model.ServiceInfo {
		waypoint := fetchWaypoint(ctx, Waypoints, Namespaces, s.ObjectMeta)
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		return a.serviceEntriesInfo(s, waypoint)
	}, krt.WithName("ServiceEntriesInfo"))
	WorkloadServices := krt.JoinCollection([]krt.Collection[model.ServiceInfo]{ServicesInfo, ServiceEntriesInfo}, krt.WithName("WorkloadServices"))
	// workloadapi services NOT workloads x services somehow
	return WorkloadServices
}

func (a *index) serviceEntriesInfo(s *networkingclient.ServiceEntry, w *Waypoint) []model.ServiceInfo {
	sel := model.NewSelector(s.Spec.GetWorkloadSelector().GetLabels())
	portNames := map[int32]model.ServicePortName{}
	for _, p := range s.Spec.Ports {
		portNames[int32(p.Number)] = model.ServicePortName{
			PortName: p.Name,
		}
	}
	return slices.Map(a.constructServiceEntries(s, w), func(e *workloadapi.Service) model.ServiceInfo {
		return model.ServiceInfo{
			Service:       e,
			PortNames:     portNames,
			LabelSelector: sel,
			Source:        kind.ServiceEntry,
		}
	})
}

func (a *index) constructServiceEntries(svc *networkingclient.ServiceEntry, w *Waypoint) []*workloadapi.Service {
	addresses, err := slices.MapErr(svc.Spec.Addresses, a.toNetworkAddressFromCidr)
	if err != nil {
		// TODO: perhaps we should support CIDR in the future?
		return nil
	}
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: p.Number,
			TargetPort:  p.TargetPort,
		})
	}

	// handle svc waypoint scenario
	var waypointAddress *workloadapi.GatewayAddress
	if w != nil {
		waypointAddress = a.getWaypointAddress(w)
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	res := make([]*workloadapi.Service, 0, len(svc.Spec.Hosts))
	for _, h := range svc.Spec.Hosts {
		res = append(res, &workloadapi.Service{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Hostname:  h,
			Addresses: addresses,
			Ports:     ports,
			Waypoint:  waypointAddress,
		})
	}
	return res
}

func (a *index) constructService(svc *v1.Service, w *Waypoint) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	addresses, err := slices.MapErr(getVIPs(svc), a.toNetworkAddress)
	if err != nil {
		log.Warnf("fail to parse service %v: %v", config.NamespacedName(svc), err)
		return nil
	}
	// handle svc waypoint scenario
	var waypointAddress *workloadapi.GatewayAddress
	if w != nil {
		waypointAddress = a.getWaypointAddress(w)
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	return &workloadapi.Service{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Hostname:  string(kube.ServiceHostname(svc.Name, svc.Namespace, a.DomainSuffix)),
		Addresses: addresses,
		Ports:     ports,
		Waypoint:  waypointAddress,
	}
}

func getVIPs(svc *v1.Service) []string {
	res := []string{}
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		res = append(res, svc.Spec.ClusterIP)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		// IPs are strictly optional for loadbalancers - they may just have a hostname.
		if ing.IP != "" {
			res = append(res, ing.IP)
		}
	}
	return res
}
