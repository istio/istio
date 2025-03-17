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
	"fmt"
	"net/netip"
	"strings"

	v1 "k8s.io/api/core/v1"

	apiannotation "istio.io/api/annotation"
	"istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) ServicesCollection(
	services krt.Collection[*v1.Service],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	opts krt.OptionsBuilder,
) krt.Collection[model.ServiceInfo] {
	ServicesInfo := krt.NewCollection(services, a.serviceServiceBuilder(waypoints, namespaces),
		opts.WithName("ServicesInfo")...)
	ServiceEntriesInfo := krt.NewManyCollection(serviceEntries, a.serviceEntryServiceBuilder(waypoints, namespaces),
		opts.WithName("ServiceEntriesInfo")...)
	WorkloadServices := krt.JoinCollection([]krt.Collection[model.ServiceInfo]{ServicesInfo, ServiceEntriesInfo}, opts.WithName("WorkloadService")...)
	return WorkloadServices
}

func GlobalMergedServicesCollection(
	clusters krt.Collection[*Cluster],
	servicesByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[*v1.Service]]],
	serviceEntriesByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[*networkingclient.ServiceEntry]]],
	waypointsByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[Waypoint]]],
	namespacesByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[*v1.Namespace]]],
	networksByCluster krt.Index[cluster.ID, krt.Singleton[string]],
	domainSuffix string,
	localClusterID cluster.ID,
	opts krt.OptionsBuilder,
) krt.Collection[config.ObjectWithCluster[model.ServiceInfo]] {
	// This will contain the serviceinfos derived from Services AND ServiceEntries
	GlobalServiceInfos := krt.NewManyCollection(clusters, func(ctx krt.HandlerContext, cluster *Cluster) []krt.Collection[config.ObjectWithCluster[model.ServiceInfo]] {
		networks := networksByCluster.Lookup(cluster.ID)
		if len(networks) == 0 {
			log.Warnf("could not find network for cluster %s", cluster.ID)
			return nil
		}
		nw := networks[0]
		if nw == nil {
			log.Warnf("could not find network for cluster %s", cluster.ID)
			return nil
		}
		serviceCollections := servicesByCluster.Lookup(cluster.ID)
		serviceEntryCollections := serviceEntriesByCluster.Lookup(cluster.ID)
		if len(serviceCollections) == 0 || len(serviceEntryCollections) == 0 {
			return nil
		}
		waypointCollections := waypointsByCluster.Lookup(cluster.ID)
		namespaceCollections := namespacesByCluster.Lookup(cluster.ID)
		if len(waypointCollections) == 0 || len(namespaceCollections) == 0 {
			return nil
		}
		clusteredServices := serviceCollections[0]
		clusteredServiceEntries := serviceEntryCollections[0]
		clusteredWaypoints := waypointCollections[0]
		clusteredNamespaces := namespaceCollections[0]
		services := krt.MapCollection(clusteredServices, func(o config.ObjectWithCluster[*v1.Service]) *v1.Service {
			return ptr.Flatten(o.Object)
		})
		serviceEntries := krt.MapCollection(clusteredServiceEntries, func(o config.ObjectWithCluster[*networkingclient.ServiceEntry]) *networkingclient.ServiceEntry {
			return ptr.Flatten(o.Object)
		})
		waypoints := krt.MapCollection(clusteredWaypoints, func(o config.ObjectWithCluster[Waypoint]) Waypoint {
			// TODO: should figure out/confirm nils never get here
			if o.Object == nil {
				return Waypoint{}
			}
			return *o.Object
		})
		namespaces := krt.MapCollection(clusteredNamespaces, func(o config.ObjectWithCluster[*v1.Namespace]) *v1.Namespace {
			return ptr.Flatten(o.Object)
		})
		servicesInfo := krt.NewCollection(services, serviceServiceBuilder(waypoints, namespaces, domainSuffix, func(ctx krt.HandlerContext) network.ID {
			return network.ID(*nw.Get())
		}))
		servicesInfoWithCluster := krt.MapCollection(servicesInfo, func(o model.ServiceInfo) config.ObjectWithCluster[model.ServiceInfo] {
			return config.ObjectWithCluster[model.ServiceInfo]{ClusterID: cluster.ID, Object: &o}
		}, opts.WithName(fmt.Sprintf("ServiceServiceInfosWithCluster[%s]", cluster.ID))...)
		serviceEntriesInfo := krt.NewManyCollection(serviceEntries, serviceEntryServiceBuilder(waypoints, namespaces, func(ctx krt.HandlerContext) network.ID {
			return network.ID(*nw.Get())
		}))
		serviceEntriesWithCluster := krt.MapCollection(serviceEntriesInfo, func(o model.ServiceInfo) config.ObjectWithCluster[model.ServiceInfo] {
			return config.ObjectWithCluster[model.ServiceInfo]{ClusterID: cluster.ID, Object: &o}
		}, opts.WithName(fmt.Sprintf("ServiceEntryServiceInfosWithCluster[%s]", cluster.ID))...)

		return []krt.Collection[config.ObjectWithCluster[model.ServiceInfo]]{servicesInfoWithCluster, serviceEntriesWithCluster}
	}, opts.WithName("GlobalServiceInfos")...)
	return krt.NestedJoinWithMergeCollection(
		GlobalServiceInfos,
		mergeServiceInfosWithCluster(localClusterID),
		opts.WithName("GlobalMergedServiceInfos")...,
	)
}

func serviceServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	domainSuffix string,
	networkGetter func(ctx krt.HandlerContext) network.ID,
) krt.TransformationSingle[*v1.Service, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *v1.Service) *model.ServiceInfo {
		if s.Spec.Type == v1.ServiceTypeExternalName {
			// ExternalName services are not implemented by ambient (but will still work).
			// The DNS requests will forward to the upstream DNS server, then Ztunnel can handle the request based on the target
			// hostname.
			// In theory we could add support for native 'DNS alias' into Ztunnel's DNS proxy. This would give the same behavior
			// but let the DNS proxy handle it instead of forwarding upstream. However, at this time we do not do so.
			return nil
		}
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

		svc := constructService(ctx, s, waypoint, domainSuffix, networkGetter)
		return precomputeServicePtr(&model.ServiceInfo{
			Service:       svc,
			PortNames:     portNames,
			LabelSelector: model.NewSelector(s.Spec.Selector),
			Source:        MakeSource(s),
			Waypoint:      waypointStatus,
		})
	}
}

func (a *index) serviceServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
) krt.TransformationSingle[*v1.Service, model.ServiceInfo] {
	return serviceServiceBuilder(waypoints, namespaces, a.DomainSuffix, func(ctx krt.HandlerContext) network.ID {
		return a.Network(ctx)
	})
}

// MakeSource is a helper to turn an Object into a model.TypedObject.
func MakeSource(o controllers.Object) model.TypedObject {
	return model.TypedObject{
		NamespacedName: config.NamespacedName(o),
		Kind:           gvk.MustToKind(kubetypes.GvkFromObject(o)),
	}
}

func (a *index) serviceEntryServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
) krt.TransformationMulti[*networkingclient.ServiceEntry, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *networkingclient.ServiceEntry) []model.ServiceInfo {
		waypoint, waypointError := fetchWaypointForService(ctx, waypoints, namespaces, s.ObjectMeta)
		return serviceEntriesInfo(ctx, s, waypoint, waypointError, func(ctx krt.HandlerContext) network.ID {
			return a.Network(ctx)
		})
	}
}

func serviceEntryServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	networkGetter func(ctx krt.HandlerContext) network.ID,
) krt.TransformationMulti[*networkingclient.ServiceEntry, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *networkingclient.ServiceEntry) []model.ServiceInfo {
		waypoint, waypointError := fetchWaypointForService(ctx, waypoints, namespaces, s.ObjectMeta)
		return serviceEntriesInfo(ctx, s, waypoint, waypointError, networkGetter)
	}
}

func serviceEntriesInfo(
	ctx krt.HandlerContext,
	s *networkingclient.ServiceEntry,
	w *Waypoint,
	wperr *model.StatusMessage,
	networkGetter func(ctx krt.HandlerContext) network.ID,
) []model.ServiceInfo {
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
		if val, ok := s.Labels["istio.io/ingress-use-waypoint"]; ok {
			waypoint.IngressLabelPresent = true
			waypoint.IngressUseWaypoint = strings.EqualFold(val, "true")
		}
	}
	if wperr != nil {
		waypoint.Error = wperr
	}
	return slices.Map(constructServiceEntries(ctx, s, w, networkGetter), func(e *workloadapi.Service) model.ServiceInfo {
		return precomputeService(model.ServiceInfo{
			Service:       e,
			PortNames:     portNames,
			LabelSelector: sel,
			Source:        MakeSource(s),
			Waypoint:      waypoint,
		})
	})
}

func constructServiceEntries(
	ctx krt.HandlerContext,
	svc *networkingclient.ServiceEntry,
	w *Waypoint,
	networkGetter func(ctx krt.HandlerContext) network.ID,
) []*workloadapi.Service {
	var autoassignedHostAddresses map[string][]netip.Addr
	addresses, err := slices.MapErr(svc.Spec.Addresses, func(e string) (*workloadapi.NetworkAddress, error) {
		return toNetworkAddressFromCidr(ctx, e, networkGetter(ctx))
	})
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
				hostsAddresses = slices.Map(hostsAddrs, func(e netip.Addr) *workloadapi.NetworkAddress {
					return toNetworkAddressFromIP(ctx, e, networkGetter(ctx))
				})
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

func (a *index) constructServiceEntries(ctx krt.HandlerContext, svc *networkingclient.ServiceEntry, w *Waypoint) []*workloadapi.Service {
	return constructServiceEntries(ctx, svc, w, func(ctx krt.HandlerContext) network.ID {
		return a.Network(ctx)
	})
}

func constructService(ctx krt.HandlerContext, svc *v1.Service, w *Waypoint, domainSuffix string, networkGetter func(krt.HandlerContext) network.ID) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	addresses, err := slices.MapErr(getVIPs(svc), func(e string) (*workloadapi.NetworkAddress, error) {
		return toNetworkAddress(ctx, e, networkGetter)
	})
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
	// This is only checking one cluster - we'll merge later in the nested join to make sure
	// we get service VIPs from other clusters
	return &workloadapi.Service{
		Name:          svc.Name,
		Namespace:     svc.Namespace,
		Hostname:      string(kube.ServiceHostname(svc.Name, svc.Namespace, domainSuffix)),
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

func precomputeServicePtr(w *model.ServiceInfo) *model.ServiceInfo {
	return ptr.Of(precomputeService(*w))
}

func precomputeService(w model.ServiceInfo) model.ServiceInfo {
	addr := serviceToAddress(w.Service)
	w.MarshaledAddress = protoconv.MessageToAny(addr)
	w.AsAddress = model.AddressInfo{
		Address:   addr,
		Marshaled: w.MarshaledAddress,
	}
	return w
}
