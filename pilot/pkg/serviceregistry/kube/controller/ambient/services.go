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

	v1 "k8s.io/api/core/v1"

	apiannotation "istio.io/api/annotation"
	"istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) ServicesCollection(
	services krt.Collection[*v1.Service],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	withDebug krt.CollectionOption,
) krt.Collection[model.ServiceInfo] {
	ServicesInfo := krt.NewCollection(services, a.serviceServiceBuilder(waypoints, namespaces),
		krt.WithName("ServicesInfo"), withDebug)
	ServiceEntriesInfo := krt.NewManyCollection(serviceEntries, a.serviceEntryServiceBuilder(waypoints, namespaces),
		krt.WithName("ServiceEntriesInfo"), withDebug)
	WorkloadServices := krt.JoinCollection([]krt.Collection[model.ServiceInfo]{ServicesInfo, ServiceEntriesInfo}, krt.WithName("WorkloadServices"))
	return WorkloadServices
}

func (a *index) serviceServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
) krt.TransformationSingle[*v1.Service, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *v1.Service) *model.ServiceInfo {
		portNames := map[int32]model.ServicePortName{}
		for _, p := range s.Spec.Ports {
			portNames[p.Port] = model.ServicePortName{
				PortName:       p.Name,
				TargetPortName: p.TargetPort.StrVal,
			}
		}
		waypointStatus := model.WaypointBindingStatus{}
		waypoint, wperr := fetchWaypointForService(ctx, waypoints, namespaces, s.ObjectMeta)
		if waypoint != nil {
			waypointStatus.ResourceName = waypoint.ResourceName()

			// TODO: add this label to the istio api labels so we have constants to use
			if val, ok := s.Labels["istio.io/ingress-use-waypoint"]; ok {
				waypointStatus.IngressLabelPresent = true
				waypointStatus.IngressUseWaypoint = strings.EqualFold(val, "true")
			}
		}
		waypointStatus.Error = wperr

		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		return &model.ServiceInfo{
			Service:       a.constructService(s, waypoint),
			PortNames:     portNames,
			LabelSelector: model.NewSelector(s.Spec.Selector),
			Source:        MakeSource(s),
			Waypoint:      waypointStatus,
		}
	}
}

// MakeSource is a helper to turn an Object into a model.TypedObject.
func MakeSource(o controllers.Object) model.TypedObject {
	return model.TypedObject{
		NamespacedName: config.NamespacedName(o),
		Kind:           kind.MustFromGVK(kubetypes.GvkFromObject(o)),
	}
}

func (a *index) serviceEntryServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
) krt.TransformationMulti[*networkingclient.ServiceEntry, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *networkingclient.ServiceEntry) []model.ServiceInfo {
		waypoint, waypointError := fetchWaypointForService(ctx, waypoints, namespaces, s.ObjectMeta)
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		return a.serviceEntriesInfo(s, waypoint, waypointError)
	}
}

func (a *index) serviceEntriesInfo(s *networkingclient.ServiceEntry, w *Waypoint, wperr *model.StatusMessage) []model.ServiceInfo {
	sel := model.NewSelector(s.Spec.GetWorkloadSelector().GetLabels())
	portNames := map[int32]model.ServicePortName{}
	for _, p := range s.Spec.Ports {
		portNames[int32(p.Number)] = model.ServicePortName{
			PortName: p.Name,
		}
	}
	waypoint := model.WaypointBindingStatus{}
	if w != nil {
		waypoint.ResourceName = w.ResourceName()
		waypoint.IngressUseWaypoint = s.Labels["istio.io/ingress-use-waypoint"] == "true"
	}
	if wperr != nil {
		waypoint.Error = wperr
	}
	return slices.Map(a.constructServiceEntries(s, w), func(e *workloadapi.Service) model.ServiceInfo {
		return model.ServiceInfo{
			Service:       e,
			PortNames:     portNames,
			LabelSelector: sel,
			Source:        MakeSource(s),
			Waypoint:      waypoint,
		}
	})
}

func (a *index) constructServiceEntries(svc *networkingclient.ServiceEntry, w *Waypoint) []*workloadapi.Service {
	var autoassignedHostAddresses map[string][]netip.Addr
	addresses, err := slices.MapErr(svc.Spec.Addresses, a.toNetworkAddressFromCidr)
	if err != nil {
		// TODO: perhaps we should support CIDR in the future?
		return nil
	}
	// if this se has autoallocation we can se autoallocated IP, otherwise it will remain an empty slice
	if serviceentry.ShouldV2AutoAllocateIP(svc) {
		autoassignedHostAddresses = serviceentry.GetHostAddressesFromServiceEntry(svc)
	}
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		target := p.TargetPort
		if target == 0 {
			target = p.Number
		}
		ports = append(ports, &workloadapi.Port{
			ServicePort: p.Number,
			TargetPort:  target,
		})
	}

	var lb *workloadapi.LoadBalancing
	preferClose := strings.EqualFold(svc.Annotations[apiannotation.NetworkingTrafficDistribution.Name], v1.ServiceTrafficDistributionPreferClose)
	if preferClose {
		lb = preferCloseLoadBalancer
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	res := make([]*workloadapi.Service, 0, len(svc.Spec.Hosts))
	for _, h := range svc.Spec.Hosts {
		// if we have no user-provided hostsAddresses and h is not wildcarded and we have hostsAddresses supported resolution
		// we can try to use autoassigned hostsAddresses
		hostsAddresses := addresses
		if len(hostsAddresses) == 0 && !host.Name(h).IsWildCarded() && svc.Spec.Resolution != v1alpha3.ServiceEntry_NONE {
			if hostsAddrs, ok := autoassignedHostAddresses[h]; ok {
				hostsAddresses = slices.Map(hostsAddrs, a.toNetworkAddressFromIP)
			}
		}
		res = append(res, &workloadapi.Service{
			Name:            svc.Name,
			Namespace:       svc.Namespace,
			Hostname:        h,
			Addresses:       hostsAddresses,
			Ports:           ports,
			Waypoint:        w.GetAddress(),
			SubjectAltNames: svc.Spec.SubjectAltNames,
			LoadBalancing:   lb,
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

	var lb *workloadapi.LoadBalancing

	// The TrafficDistribution field is quite new, so we allow a legacy annotation option as well
	preferClose := strings.EqualFold(svc.Annotations[apiannotation.NetworkingTrafficDistribution.Name], v1.ServiceTrafficDistributionPreferClose)
	if svc.Spec.TrafficDistribution != nil {
		preferClose = *svc.Spec.TrafficDistribution == v1.ServiceTrafficDistributionPreferClose
	}
	if preferClose {
		lb = preferCloseLoadBalancer
	}
	if itp := svc.Spec.InternalTrafficPolicy; itp != nil && *itp == v1.ServiceInternalTrafficPolicyLocal {
		lb = &workloadapi.LoadBalancing{
			// Only allow endpoints on the same node.
			RoutingPreference: []workloadapi.LoadBalancing_Scope{
				workloadapi.LoadBalancing_NODE,
			},
			Mode: workloadapi.LoadBalancing_STRICT,
		}
	}
	if svc.Spec.PublishNotReadyAddresses {
		if lb == nil {
			lb = &workloadapi.LoadBalancing{}
		}
		lb.HealthPolicy = workloadapi.LoadBalancing_ALLOW_ALL
	}

	ipFamily := workloadapi.IPFamilies_AUTOMATIC
	if len(svc.Spec.IPFamilies) == 2 {
		ipFamily = workloadapi.IPFamilies_DUAL
	} else if len(svc.Spec.IPFamilies) == 1 {
		family := svc.Spec.IPFamilies[0]
		if family == v1.IPv4Protocol {
			ipFamily = workloadapi.IPFamilies_IPV4_ONLY
		} else {
			ipFamily = workloadapi.IPFamilies_IPV6_ONLY
		}
	}
	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	return &workloadapi.Service{
		Name:          svc.Name,
		Namespace:     svc.Namespace,
		Hostname:      string(kube.ServiceHostname(svc.Name, svc.Namespace, a.DomainSuffix)),
		Addresses:     addresses,
		Ports:         ports,
		Waypoint:      w.GetAddress(),
		LoadBalancing: lb,
		IpFamilies:    ipFamily,
	}
}

var preferCloseLoadBalancer = &workloadapi.LoadBalancing{
	// Prefer endpoints in close zones, but allow spilling over to further endpoints where required.
	RoutingPreference: []workloadapi.LoadBalancing_Scope{
		workloadapi.LoadBalancing_NETWORK,
		workloadapi.LoadBalancing_REGION,
		workloadapi.LoadBalancing_ZONE,
		workloadapi.LoadBalancing_SUBZONE,
	},
	Mode: workloadapi.LoadBalancing_FAILOVER,
}

func getVIPs(svc *v1.Service) []string {
	res := []string{}
	cips := svc.Spec.ClusterIPs
	if len(cips) == 0 {
		cips = []string{svc.Spec.ClusterIP}
	}
	for _, cip := range cips {
		if cip != "" && cip != v1.ClusterIPNone {
			res = append(res, cip)
		}
	}
	return res
}
