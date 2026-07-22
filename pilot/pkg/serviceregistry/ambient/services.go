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
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/label"
	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh/labelselector"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/workloadapi"
)

func (a Builder) ServicesCollection(
	clusterID cluster.ID,
	services krt.Collection[*v1.Service],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	meshConfig krt.Singleton[MeshConfig],
	serviceEntryVisibility krt.Singleton[model.ServiceEntryVisibilityMatcher],
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

	ServiceEntriesInfo := krt.NewManyCollection(serviceEntries, a.serviceEntryServiceBuilder(waypoints, namespaces, serviceEntryVisibility),
		append(
			opts.WithName("ServiceEntriesInfo"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: clusterID,
			}),
		)...)

	allTypedServiceInfos := krt.JoinCollection([]krt.Collection[TypedServiceInfo]{ServicesInfo, ServiceEntriesInfo},
		append(opts.WithName("AllTypedServiceInfo"), krt.WithMetadata(krt.Metadata{
			multicluster.ClusterKRTMetadataKey: clusterID,
		}))...)

	allTypedServiceInfosByHostname := krt.NewIndex(allTypedServiceInfos, "TypedServiceInfosByHostname", func(ts TypedServiceInfo) []string {
		return []string{ts.Service.Hostname}
	})

	WorkloadServices := krt.NewManyCollection(
		allTypedServiceInfosByHostname.AsCollection(),
		func(ctx krt.HandlerContext, ios krt.IndexObject[string, TypedServiceInfo]) []model.ServiceInfo {
			if len(ios.Objects) == 0 {
				return nil
			}
			return selectWorkloadServices(ios.Objects)
		}, append(
			opts.WithName("WorkloadServices"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: clusterID,
			}),
		)...,
	)
	return WorkloadServices
}

// selectWorkloadServices resolves the set of services sharing a hostname down to one winner per
// namespace and marks the single canonical service.
func selectWorkloadServices(typedServiceInfos []TypedServiceInfo) []model.ServiceInfo {
	bestByNamespace := make(map[string]model.ServiceInfo)

	// check if a is a better ServiceInfo than b
	// better meaning both:
	//     a is older than b
	//     b is not a Kubernetes Service
	isBetter := func(a, b model.ServiceInfo) bool {
		return a.CreationTime.Before(b.CreationTime) && b.Source.Kind != kind.Service
	}
	// Pick the winner for each namespace: a Kubernetes Service always wins, else the oldest.
	for _, tsi := range typedServiceInfos {
		tsiNamespace := tsi.GetNamespace()
		if tsi.Source.Kind == kind.Service {
			bestByNamespace[tsiNamespace] = tsi.ServiceInfo
			continue
		}
		currentBest, found := bestByNamespace[tsiNamespace]
		if !found || isBetter(tsi.ServiceInfo, currentBest) {
			bestByNamespace[tsiNamespace] = tsi.ServiceInfo
		}
	}

	// select and mark Canonical from bestByNamespace
	var canonical *model.ServiceInfo
	for ns := range bestByNamespace {
		si := bestByNamespace[ns]
		switch {
		case si.Source.Kind == kind.Service:
			canonical = &si
		case si.Service.GetVisibility() == workloadapi.Service_NAMESPACE:
			continue
		default:
			if canonical == nil || isBetter(si, *canonical) {
				canonical = &si
			}
		}
	}
	if canonical != nil {
		bestByNamespace[canonical.GetNamespace()] = setCanonical(canonical)
	}

	return maps.Values(bestByNamespace)
}

func GlobalNestedWorkloadServicesCollection(
	localCluster *multicluster.Cluster,
	localServiceInfos krt.Collection[model.ServiceInfo],
	localWaypoints krt.Collection[Waypoint],
	ctrl *multicluster.Controller,
	localServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	globalServices krt.Collection[krt.Collection[*v1.Service]],
	servicesByCluster krt.Index[cluster.ID, krt.Collection[*v1.Service]],
	globalWaypoints krt.Collection[krt.Collection[Waypoint]],
	waypointsByCluster krt.Index[cluster.ID, krt.Collection[Waypoint]],
	globalNamespaces krt.Collection[krt.Collection[*v1.Namespace]],
	namespacesByCluster krt.Index[cluster.ID, krt.Collection[*v1.Namespace]],
	meshConfig krt.Singleton[MeshConfig],
	globalNetworks NetworkCollections,
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
	return multicluster.NestedCollectionFromLocalAndRemote(
		ctrl,
		LocalServiceInfosWithCluster,
		func(ctx krt.HandlerContext, cluster *multicluster.Cluster) *krt.Collection[krt.ObjectWithCluster[model.ServiceInfo]] {
			opts := []krt.CollectionOption{
				krt.WithDebugging(opts.Debugger()),
				krt.WithStop(cluster.GetStop()),
			}
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
				}, false),
				append(
					opts,
					krt.WithName(fmt.Sprintf("ambient/ServiceServiceInfos[%s]", cluster.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: cluster.ID,
					}),
				)...)

			servicesInfoWithCluster := krt.MapCollection(
				servicesInfo,
				func(o model.ServiceInfo) krt.ObjectWithCluster[model.ServiceInfo] {
					return krt.ObjectWithCluster[model.ServiceInfo]{ClusterID: cluster.ID, Object: &o}
				},
				append(
					opts,
					krt.WithName(fmt.Sprintf("ServiceServiceInfosWithCluster[%s]", cluster.ID)),
				)...,
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
		ns := krt.FetchOne(ctx, namespaces, krt.FilterKey(s.Namespace))
		waypointStatus := model.WaypointBindingStatus{}
		waypoint, wperr := fetchWaypointForService(ctx, waypoints, namespaces, nil, s.ObjectMeta)
		if waypoint != nil {
			waypointStatus.ResourceName = waypoint.ResourceName()
			var nsLabels map[string]string
			if ns != nil {
				nsLabels = (*ns).Labels
			}
			waypointStatus.IngressLabelPresent, waypointStatus.IngressUseWaypoint = ingressUseWaypointFromLabels(s.Labels, nsLabels)
		}
		waypointStatus.Error = wperr

		var nsAnnotations map[string]string
		if ns != nil {
			nsAnnotations = (*ns).Annotations
		}

		svc := constructService(ctx, s, waypoint, domainSuffix, nsAnnotations, networkGetter)
		svc.IngressUseWaypoint = waypointStatus.IngressUseWaypoint
		svc.WeightedWaypoints = buildWeightedWaypoints(ctx, waypoints, namespaces, s.ObjectMeta, waypoint, &waypointStatus)

		svcInfo := &model.ServiceInfo{
			Service:       svc,
			PortNames:     portNames,
			LabelSelector: model.NewSelector(s.Spec.Selector),
			Source:        MakeSource(s),
			Waypoint:      waypointStatus,
			Scope:         serviceScope,
			CreationTime:  s.CreationTimestamp.Time,
		}
		if precompute {
			return precomputeServicePtr(svcInfo)
		}

		return svcInfo
	}
}

func typedServiceServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	meshConfig krt.Singleton[MeshConfig],
	domainSuffix string,
	localCluster bool,
	checkServiceScope bool,
	networkGetter func(ctx krt.HandlerContext) network.ID,
	precompute bool,
) krt.TransformationSingle[*v1.Service, TypedServiceInfo] {
	return func(ctx krt.HandlerContext, s *v1.Service) *TypedServiceInfo {
		svcInfo := serviceServiceBuilder(waypoints,
			namespaces,
			meshConfig,
			domainSuffix,
			localCluster,
			checkServiceScope,
			networkGetter,
			precompute)(ctx, s)
		if svcInfo == nil {
			return nil
		}
		return &TypedServiceInfo{ServiceInfo: *svcInfo}
	}
}

func matchServiceScope(ctx krt.HandlerContext, meshCfg *MeshConfig, namespaces krt.Collection[*v1.Namespace], s *v1.Service) model.ServiceScope {
	// Apply label selectors from the MeshConfig's servieScopeConfig to determine the scope of the service based on the namespace
	// or service label matches
	// Check if the service matches any label selectors defined in the meshConfig's serviceScopeConfig.
	for _, scopeConfig := range meshCfg.ServiceScopeConfigs {
		// Match namespace labels
		// Treat Nothing selectors as Everything selectors
		nss, err := labelselector.LabelSelectorAsSelector(scopeConfig.NamespaceSelector)
		if err != nil {
			log.Warnf("failed to convert namespace selector: %v", err)
			continue
		}
		if nss.String() == labels.Nothing().String() {
			nss = labels.Everything()
		}
		ss, err := labelselector.LabelSelectorAsSelector(scopeConfig.ServicesSelector)
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

// wdsVisibility maps the neutral resolved visibility to the WDS Service visibility field. NONE has
// no WDS representation (such services are dropped before this is called), so only the
// PUBLIC/NAMESPACE cases are relevant here.
func wdsVisibility(v model.ServiceVisibility) workloadapi.Service_Visibility {
	if v == model.ServiceVisibilityNamespace {
		return workloadapi.Service_NAMESPACE
	}
	return workloadapi.Service_PUBLIC
}

func (a Builder) serviceServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	meshConfig krt.Singleton[MeshConfig],
	precompute bool,
) krt.TransformationSingle[*v1.Service, TypedServiceInfo] {
	return typedServiceServiceBuilder(
		waypoints,
		namespaces,
		meshConfig,
		a.DomainSuffix,
		true,
		features.EnableAmbientMultiNetwork,
		a.Networks.FetchLocalNetworkID,
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

// TypedServiceInfo is a wrapper around ServiceInfo to avoid key conflicts during processing
type TypedServiceInfo struct {
	model.ServiceInfo
}

func (t TypedServiceInfo) ResourceName() string {
	return t.Source.Kind.String() + "/" + t.GetNamespace() + "/" + t.GetName() + "/" + t.Service.GetHostname()
}

func (t TypedServiceInfo) Equals(other TypedServiceInfo) bool {
	return t.ServiceInfo.Equals(other.ServiceInfo)
}

func (a Builder) serviceEntryServiceBuilder(
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	visibility krt.Singleton[model.ServiceEntryVisibilityMatcher],
) krt.TransformationMulti[*networkingclient.ServiceEntry, TypedServiceInfo] {
	return func(ctx krt.HandlerContext, s *networkingclient.ServiceEntry) []TypedServiceInfo {
		waypoint, waypointError := fetchWaypointForService(ctx, waypoints, namespaces, visibility, s.ObjectMeta)

		ns := krt.FetchOne(ctx, namespaces, krt.FilterKey(s.Namespace))
		var nsAnnotations map[string]string
		var nsLabels map[string]string
		if ns != nil {
			nsAnnotations = (*ns).Annotations
			nsLabels = (*ns).Labels
		}

		serviceInfos := serviceEntriesInfo(
			ctx, s, waypoints, namespaces, waypoint, waypointError,
			nsAnnotations, nsLabels, visibility, a.Networks.FetchLocalNetworkID,
		)
		return slices.Map(serviceInfos, func(si model.ServiceInfo) TypedServiceInfo {
			return TypedServiceInfo{ServiceInfo: si}
		})
	}
}

func serviceEntriesInfo(
	ctx krt.HandlerContext,
	s *networkingclient.ServiceEntry,
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	w *Waypoint,
	wperr *model.StatusMessage,
	nsAnnotations map[string]string,
	nsLabels map[string]string,
	visibility krt.Singleton[model.ServiceEntryVisibilityMatcher],
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
		waypoint.IngressLabelPresent, waypoint.IngressUseWaypoint = ingressUseWaypointFromLabels(s.Labels, nsLabels)
	}
	if wperr != nil {
		waypoint.Error = wperr
	}

	// Warn if we have a dynamic DNS service entry with no egress waypoint
	// This configuration will not be shared with the dataplane
	if s.Spec.Resolution == v1alpha3.ServiceEntry_DYNAMIC_DNS && (w == nil || wperr != nil) {
		log.Warnf("ServiceEntry %s/%s has dynamic DNS resolution but no valid waypoint", s.Namespace, s.Name)
	}

	weighted := buildWeightedWaypoints(ctx, waypoints, namespaces, s.ObjectMeta, w, &waypoint)

	vis := krt.FetchOne(ctx, visibility.AsCollection())
	// Resolve the ServiceEntry's visibility once from the precompiled serviceEntryVisibility; it is
	// uniform across the ServiceEntry's hosts. NONE means Istio configures no dataplane for it, so it
	// is not published to WDS.
	resolvedVisibility := vis.VisibilityFor(nsLabels)
	if resolvedVisibility == model.ServiceVisibilityNone {
		log.Debugf("ServiceEntry %s/%s resolves to visibility NONE; not configuring", s.Namespace, s.Name)
		// StatusBug: dropping the ServiceInfo means we can't write a condition that it was dropped.
		// https://github.com/istio/istio/issues/60908
		return nil
	}
	// Gate the visibility status on whether the feature is configured; an unset mesh writes no
	// condition. The status value itself comes from the WDS Service.Visibility set above.
	visibilityConfigured := vis.Configured()
	services := constructServiceEntries(ctx, s, w, nsAnnotations, networkGetter)
	result := make([]model.ServiceInfo, 0, len(services))
	for _, e := range services {
		e.Visibility = wdsVisibility(resolvedVisibility)
		e.IngressUseWaypoint = waypoint.IngressUseWaypoint
		e.WeightedWaypoints = weighted
		result = append(result, precomputeService(model.ServiceInfo{
			Service:              e,
			PortNames:            portNames,
			LabelSelector:        sel,
			Source:               MakeSource(s),
			Waypoint:             waypoint,
			DNSConnectStrategy:   model.GetDNSConnectStrategy(s.Annotations),
			CreationTime:         s.CreationTimestamp.Time,
			VisibilityConfigured: visibilityConfigured,
		}))
	}
	return result
}

func constructServiceEntries(
	ctx krt.HandlerContext,
	svc *networkingclient.ServiceEntry,
	w *Waypoint,
	nsAnnotations map[string]string,
	networkGetter func(ctx krt.HandlerContext) network.ID,
) []*workloadapi.Service {
	var autoassignedHostAddresses map[string][]netip.Addr
	addresses, err := slices.MapErr(svc.Spec.Addresses, func(e string) (*workloadapi.NetworkAddress, error) {
		return toNetworkAddressFromCidr(e, networkGetter(ctx))
	})
	if err != nil {
		return nil
	}
	// if this se has autoallocation we can se autoallocated IP, otherwise it will remain an empty slice
	if serviceentry.ShouldV2AutoAllocateIP(svc) {
		autoassignedHostAddresses = serviceentry.GetHostAddressesFromServiceEntry(svc)
	}
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	containsTLSPort := false
	for _, p := range svc.Spec.Ports {
		target := p.TargetPort
		if target == 0 {
			target = p.Number
		}
		if p.Protocol == string(protocol.TLS) {
			containsTLSPort = true
		}
		ports = append(ports, &workloadapi.Port{
			ServicePort: p.Number,
			TargetPort:  target,
			AppProtocol: toAppProtocolFromProtocol(protocol.Parse(p.Protocol)),
		})
	}

	var lb *workloadapi.LoadBalancing

	// When resolution is NONE, we want to passthrough traffic to the destination address.
	// In this case, we can skip attempting to get a traffic distribution because it is not applicable.
	if svc.Spec.Resolution == v1alpha3.ServiceEntry_NONE {
		lb = &workloadapi.LoadBalancing{
			Mode: workloadapi.LoadBalancing_PASSTHROUGH,
			// HealthPolicy is primarily aesthetic in this case, making the WDS easier to understand.
			// We pass through to the endpoint called, so it seems weird if WDS has a OnlyHealthy LoadBalancer setting.
			HealthPolicy: workloadapi.LoadBalancing_ALLOW_ALL,
		}
	} else {
		trafficDistribution := model.GetTrafficDistribution(nil, svc.Annotations, nsAnnotations)
		switch trafficDistribution {
		case model.TrafficDistributionPreferSameZone:
			lb = preferSameZoneLoadBalancer()
		case model.TrafficDistributionPreferSameNode:
			lb = preferSameNodeLoadBalancer()
		}
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	res := make([]*workloadapi.Service, 0, len(svc.Spec.Hosts))
	for _, h := range svc.Spec.Hosts {
		// if we have no user-provided hostsAddresses and we have hostsAddresses supported resolution
		// we can try to use autoassigned hostsAddresses
		hostsAddresses := addresses
		if len(hostsAddresses) == 0 && svc.Spec.Resolution != v1alpha3.ServiceEntry_NONE {
			// block auto IP assignment for wildcards unless the resolution type is DYNAMIC_DNS
			if host.Name(h).IsWildCarded() && svc.Spec.Resolution != v1alpha3.ServiceEntry_DYNAMIC_DNS {
				log.Debugf("autoassigned IPs only support for wildcarded hosts with dynamic DNS resolution, skipping %s", h)
			} else if hostsAddrs, ok := autoassignedHostAddresses[h]; ok {
				hostsAddresses = slices.Map(hostsAddrs, func(e netip.Addr) *workloadapi.NetworkAddress {
					return toNetworkAddressFromIP(e, networkGetter(ctx))
				})
			}
		}
		if host.Name(h).IsWildCarded() && containsTLSPort && svc.Spec.Resolution == v1alpha3.ServiceEntry_DYNAMIC_DNS &&
			!features.EnableWildcardHostServiceEntriesForTLS {
			log.Debugf("xds configuration will not be generated for the TLS port belonging to the service %s with wildcard "+
				"host %s since the feature is disabled", svc.Name, h)
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
	nsAnnotations map[string]string,
	networkGetter func(krt.HandlerContext) network.ID,
) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
			AppProtocol: toAppProtocolFromKube(p),
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
		// Check traffic distribution with namespace inheritance
		trafficDistribution := model.GetTrafficDistribution(svc.Spec.TrafficDistribution, svc.Annotations, nsAnnotations)
		switch trafficDistribution {
		case model.TrafficDistributionPreferSameZone:
			lb = preferSameZoneLoadBalancer()
		case model.TrafficDistributionPreferSameNode:
			lb = preferSameNodeLoadBalancer()
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
		// A kubernetes service is always considered to be canonical, overrides for this host must be namespace-local
		// to the client workload
		Canonical: true,
	}
}

// preferSameZoneLoadBalancer returns a fresh LoadBalancing config for PreferSameZone
// traffic distribution. It must return a new pointer on every call because callers
// (notably constructService) may mutate the returned object in place (for example,
// setting HealthPolicy when PublishNotReadyAddresses is true). Returning a shared
// package-level pointer here previously caused those mutations to leak across
// services, poisoning the preset for every other service in the cluster.
func preferSameZoneLoadBalancer() *workloadapi.LoadBalancing {
	return &workloadapi.LoadBalancing{
		// Prefer endpoints in close zones, but allow spilling over to further endpoints where required.
		RoutingPreference: []workloadapi.LoadBalancing_Scope{
			workloadapi.LoadBalancing_NETWORK,
			workloadapi.LoadBalancing_REGION,
			workloadapi.LoadBalancing_ZONE,
		},
		Mode: workloadapi.LoadBalancing_FAILOVER,
	}
}

// preferSameNodeLoadBalancer returns a fresh LoadBalancing config for PreferSameNode
// traffic distribution. See preferSameZoneLoadBalancer for why this must not be a
// shared package-level pointer.
func preferSameNodeLoadBalancer() *workloadapi.LoadBalancing {
	return &workloadapi.LoadBalancing{
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
	w.AsAddress = model.NewAddressInfo(serviceToAddress(w.Service))
	w.MarshaledAddress = w.AsAddress.Marshaled
	return w
}

// setCanonical sets the canonical field in a WDS service without mangling the ServiceInfo
func setCanonical(se *model.ServiceInfo) model.ServiceInfo {
	wdsSvc := protomarshal.ShallowClone(se.Service)
	wdsSvc.Canonical = true
	return precomputeService(model.ServiceInfo{
		Service:            wdsSvc,
		LabelSelector:      se.LabelSelector,
		PortNames:          se.PortNames,
		Source:             se.Source,
		Scope:              se.Scope,
		Waypoint:           se.Waypoint,
		MarshaledAddress:   se.MarshaledAddress,
		AsAddress:          se.AsAddress,
		DNSConnectStrategy: se.DNSConnectStrategy,
		CreationTime:       se.CreationTime,
	})
}

// ingressUseWaypointFromLabels returns whether the ingress-use-waypoint label is
// present and whether its value is "true". It checks the service labels first,
// then falls back to namespace labels.
func ingressUseWaypointFromLabels(serviceLabels, namespaceLabels map[string]string) (labelPresent bool, useWaypoint bool) {
	var val string
	val, labelPresent = serviceLabels[label.IoIstioIngressUseWaypoint.Name]
	if !labelPresent && namespaceLabels != nil {
		val, labelPresent = namespaceLabels[label.IoIstioIngressUseWaypoint.Name]
	}
	if !labelPresent {
		return false, false
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		log.Warnf("invalid value for label %s: %s, defaulting to false", label.IoIstioIngressUseWaypoint.Name, val)
	}
	return true, err == nil && parsed
}
