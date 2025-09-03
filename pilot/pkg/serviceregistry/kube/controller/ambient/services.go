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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
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
	clusterID cluster.ID,
	services krt.Collection[*v1.Service],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	meshConfig krt.Singleton[MeshConfig],
	opts krt.OptionsBuilder,
	precompute bool,
) krt.Collection[model.ServiceInfo] {
	ServicesInfo := krt.NewCollection(services, a.serviceServiceBuilder(waypoints, namespaces, meshConfig, precompute),
		append(
			opts.WithName("ServicesInfo"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: clusterID,
			}),
		)...)
	ServiceEntriesInfo := krt.NewManyCollection(serviceEntries, a.serviceEntryServiceBuilder(waypoints, namespaces),
		append(
			opts.WithName("ServiceEntriesInfo"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: clusterID,
			}),
		)...)
	WorkloadServices := krt.JoinCollection(
		[]krt.Collection[model.ServiceInfo]{ServicesInfo, ServiceEntriesInfo},
		append(opts.WithName("WorkloadService"), krt.WithMetadata(
			krt.Metadata{
				multicluster.ClusterKRTMetadataKey: clusterID,
			},
		))...)
	return WorkloadServices
}

func GlobalMergedWorkloadServicesCollection(
	localCluster *multicluster.Cluster,
	localServiceInfos krt.Collection[model.ServiceInfo],
	localWaypoints krt.Collection[Waypoint],
	clusters krt.Collection[*multicluster.Cluster],
	localServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	globalServices krt.Collection[krt.Collection[*v1.Service]],
	servicesByCluster krt.Index[cluster.ID, krt.Collection[*v1.Service]],
	globalWaypoints krt.Collection[krt.Collection[Waypoint]],
	waypointsByCluster krt.Index[cluster.ID, krt.Collection[Waypoint]],
	globalNamespaces krt.Collection[krt.Collection[*v1.Namespace]],
	namespacesByCluster krt.Index[cluster.ID, krt.Collection[*v1.Namespace]],
	meshConfig krt.Singleton[MeshConfig],
	globalNetworks networkCollections,
	domainSuffix string,
	opts krt.OptionsBuilder,
) krt.Collection[krt.Collection[krt.ObjectWithCluster[model.ServiceInfo]]] {
	// This will contain the serviceinfos derived from Services AND ServiceEntries
	LocalServiceInfosWithCluster := krt.MapCollection(
		localServiceInfos,
		wrapObjectWithCluster[model.ServiceInfo](localCluster.ID),
		opts.WithName("LocalServiceInfosWithCluster")...)

	checkServiceScope := features.EnableAmbientMultiNetwork

	// This will contain the serviceinfos derived from ServiceEntries only
	return nestedCollectionFromLocalAndRemote(
		LocalServiceInfosWithCluster,
		clusters,
		func(ctx krt.HandlerContext, cluster *multicluster.Cluster) *krt.Collection[krt.ObjectWithCluster[model.ServiceInfo]] {
			services := cluster.Services()
			waypointsPtr := krt.FetchOne(ctx, globalWaypoints, krt.FilterIndex(waypointsByCluster, cluster.ID))
			if waypointsPtr == nil {
				log.Warnf("Cluster %s does not have waypoints assigned, skipping", cluster.ID)
				return nil
			}
			waypoints := *waypointsPtr
			namespaces := cluster.Namespaces()
			// N.B Never precompute the service info for remote clusters; the merge function will do that
			servicesInfo := krt.NewCollection(services, serviceServiceBuilder(
				waypoints,
				namespaces,
				meshConfig,
				domainSuffix,
				false,
				checkServiceScope,
				func(ctx krt.HandlerContext) network.ID {
					nw := krt.FetchOne(ctx, globalNetworks.RemoteSystemNamespaceNetworks, krt.FilterIndex(globalNetworks.SystemNamespaceNetworkByCluster, cluster.ID))
					if nw == nil {
						log.Warnf("Cluster %s does not have network assigned yet, skipping", cluster.ID)
						ctx.DiscardResult()
						return ""
					}
					return nw.Network
				}, false), opts.With(
				append(
					opts.WithName(fmt.Sprintf("ServiceServiceInfos[%s]", cluster.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: cluster.ID,
					}),
				)...,
			)...)

			servicesInfoWithCluster := krt.MapCollection(
				servicesInfo,
				func(o model.ServiceInfo) krt.ObjectWithCluster[model.ServiceInfo] {
					return krt.ObjectWithCluster[model.ServiceInfo]{ClusterID: cluster.ID, Object: &o}
				},
				opts.WithName(fmt.Sprintf("ServiceServiceInfosWithCluster[%s]", cluster.ID))...,
			)
			return ptr.Of(servicesInfoWithCluster)
		},
		"ServiceInfosWithCluster",
		opts,
	)
}

func serviceServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	meshConfig krt.Singleton[MeshConfig],
	domainSuffix string,
	localCluster bool,
	checkServiceScope bool,
	networkGetter func(ctx krt.HandlerContext) network.ID,
	precompute bool,
) krt.TransformationSingle[*v1.Service, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *v1.Service) *model.ServiceInfo {
		serviceScope := model.Local
		if checkServiceScope {
			meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
			if meshCfg != nil {
				serviceScope = matchServiceScope(ctx, meshCfg, namespaces, s)
			}
			if !localCluster && serviceScope != model.Global {
				// If this is a remote service, we only want to return it if it is globally scoped.
				// This is because we do not want to expose local services from other clusters.
				log.Debugf("Skipping non-global service %s/%s in remote cluster", s.Namespace, s.Name)
				return nil
			}
		}
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

		svcInfo := &model.ServiceInfo{
			Service:       svc,
			PortNames:     portNames,
			LabelSelector: model.NewSelector(s.Spec.Selector),
			Source:        MakeSource(s),
			Waypoint:      waypointStatus,
			Scope:         serviceScope,
		}
		if precompute {
			return precomputeServicePtr(svcInfo)
		}

		return svcInfo
	}
}

// LabelSelectorAsSelector converts a mesh api LabelSelector to a labels.Selector.
func LabelSelectorAsSelector(ps *meshapi.LabelSelector) (labels.Selector, error) {
	if ps == nil {
		return labels.Nothing(), nil
	}
	if len(ps.MatchLabels)+len(ps.MatchExpressions) == 0 {
		return labels.Everything(), nil
	}
	requirements := make([]labels.Requirement, 0, len(ps.MatchLabels)+len(ps.MatchExpressions))
	for k, v := range ps.MatchLabels {
		r, err := labels.NewRequirement(k, selection.Equals, []string{v})
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	for _, expr := range ps.MatchExpressions {
		var op selection.Operator
		switch metav1.LabelSelectorOperator(expr.Operator) {
		case metav1.LabelSelectorOpIn:
			op = selection.In
		case metav1.LabelSelectorOpNotIn:
			op = selection.NotIn
		case metav1.LabelSelectorOpExists:
			op = selection.Exists
		case metav1.LabelSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		default:
			return nil, fmt.Errorf("%q is not a valid label selector operator", expr.Operator)
		}
		r, err := labels.NewRequirement(expr.Key, op, append([]string(nil), expr.Values...))
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	selector := labels.NewSelector()
	selector = selector.Add(requirements...)
	return selector, nil
}

func matchServiceScope(ctx krt.HandlerContext, meshCfg *MeshConfig, namespaces krt.Collection[*v1.Namespace], s *v1.Service) model.ServiceScope {
	// Apply label selectors from the MeshConfig's servieScopeConfig to determine the scope of the service based on the namespace
	// or service label matches
	// Check if the service matches any label selectors defined in the meshConfig's serviceScopeConfig.
	for _, scopeConfig := range meshCfg.ServiceScopeConfigs {
		// Match namespace labels
		// Treat Nothing selectors as Everything selectors
		nss, err := LabelSelectorAsSelector(scopeConfig.NamespaceSelector)
		if err != nil {
			log.Warnf("failed to convert namespace selector: %v", err)
			continue
		}
		if nss.String() == labels.Nothing().String() {
			nss = labels.Everything()
		}
		ss, err := LabelSelectorAsSelector(scopeConfig.ServicesSelector)
		if err != nil {
			log.Warnf("failed to convert service selector: %v", err)
			continue
		}
		if ss.String() == labels.Nothing().String() {
			ss = labels.Everything()
		}

		// Get labels from the service and service's namespace
		namespace := krt.FetchOne(ctx, namespaces, krt.FilterKey(s.Namespace))
		if namespace == nil || *namespace == nil {
			log.Warnf("namespace %s not found for service %s/%s in namespaces %v", s.Namespace, s.Name, s.UID, namespaces.List())
			continue
		}
		namespaceLabels := labels.Set((*namespace).Labels)
		serviceLabels := labels.Set(s.Labels)

		if nss.Matches(namespaceLabels) && ss.Matches(serviceLabels) {
			// If the service matches a global scope config, return global scope
			if scopeConfig.GetScope() == meshapi.MeshConfig_ServiceScopeConfigs_GLOBAL {
				log.Debugf("service %s/%s is globally scoped", s.Namespace, s.Name)
				return model.Global
			}
		}
	}

	// Default to local scope if no match is found
	log.Debugf("service %s/%s is locally scoped", s.Namespace, s.Name)
	return model.Local
}

func (a *index) serviceServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	meshConfig krt.Singleton[MeshConfig],
	precompute bool,
) krt.TransformationSingle[*v1.Service, model.ServiceInfo] {
	return serviceServiceBuilder(
		waypoints,
		namespaces,
		meshConfig,
		a.DomainSuffix,
		true,
		features.EnableAmbientMultiNetwork,
		func(ctx krt.HandlerContext) network.ID {
			return a.Network(ctx)
		},
		precompute,
	)
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
		return toNetworkAddressFromCidr(e, networkGetter(ctx))
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

	trafficDistribution := model.GetTrafficDistribution(nil, svc.Annotations)
	switch trafficDistribution {
	case model.TrafficDistributionPreferSameZone:
		lb = preferSameZoneLoadBalancer
	case model.TrafficDistributionPreferSameNode:
		lb = preferSameNodeLoadBalancer
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
					return toNetworkAddressFromIP(e, networkGetter(ctx))
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

// Construct a workload API service
func constructService(
	ctx krt.HandlerContext,
	svc *v1.Service,
	w *Waypoint,
	domainSuffix string,
	networkGetter func(krt.HandlerContext) network.ID,
) *workloadapi.Service {
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

	// First, use internal traffic policy if set.
	if itp := svc.Spec.InternalTrafficPolicy; itp != nil && *itp == v1.ServiceInternalTrafficPolicyLocal {
		lb = &workloadapi.LoadBalancing{
			// Only allow endpoints on the same node.
			RoutingPreference: []workloadapi.LoadBalancing_Scope{
				workloadapi.LoadBalancing_NODE,
			},
			Mode: workloadapi.LoadBalancing_STRICT,
		}
	} else {
		// Check traffic distribution.
		trafficDistribution := model.GetTrafficDistribution(svc.Spec.TrafficDistribution, svc.Annotations)
		switch trafficDistribution {
		case model.TrafficDistributionPreferSameZone:
			lb = preferSameZoneLoadBalancer
		case model.TrafficDistributionPreferSameNode:
			lb = preferSameNodeLoadBalancer
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

var preferSameZoneLoadBalancer = &workloadapi.LoadBalancing{
	// Prefer endpoints in close zones, but allow spilling over to further endpoints where required.
	RoutingPreference: []workloadapi.LoadBalancing_Scope{
		workloadapi.LoadBalancing_NETWORK,
		workloadapi.LoadBalancing_REGION,
		workloadapi.LoadBalancing_ZONE,
	},
	Mode: workloadapi.LoadBalancing_FAILOVER,
}

var preferSameNodeLoadBalancer = &workloadapi.LoadBalancing{
	// Prefer endpoints in close zones, but allow spilling over to further endpoints where required.
	RoutingPreference: []workloadapi.LoadBalancing_Scope{
		workloadapi.LoadBalancing_NETWORK,
		workloadapi.LoadBalancing_REGION,
		workloadapi.LoadBalancing_ZONE,
		workloadapi.LoadBalancing_SUBZONE,
		workloadapi.LoadBalancing_NODE,
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
