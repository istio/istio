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

package ambient

import (
	"net/netip"
	"strings"

	"istio.io/api/label"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/kind"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	istiomulticluster "istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

const ClusterKRTMetadataKey = "cluster"

func (a *index) buildGlobalCollections(
	LocalAuthzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	LocalPeerAuths krt.Collection[*securityclient.PeerAuthentication],
	LocalNamespaces krt.Collection[*v1.Namespace],
	LocalGateways krt.Collection[*v1beta1.Gateway],
	LocalGatewayClasses krt.Collection[*v1beta1.GatewayClass],
	LocalPods krt.Collection[*v1.Pod],
	LocalWorkloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	LocalServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	options Options,
	opts krt.OptionsBuilder,
) error {
	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	clusters := buildRemoteClustersCollection(
		options,
		opts,
		istiomulticluster.DefaultBuildClientsFromConfig,
		filter,
	)
	a.remoteClusters = clusters

	LocalMeshConfig := options.MeshConfig
	// These first collections can't be merged since the Kubernetes APIs don't have enough room
	// for e.g. duplicate IPs, etc. So we keep around collections of collections and indexes per cluster.
	allPodInformers := krt.NewCollection(clusters, informerForCluster[*v1.Pod](
		filter,
		"Pods",
		opts,
		func(c Cluster) kclient.Filter {
			return kclient.Filter{
				ObjectFilter:    c.Client.ObjectFilter(),
				ObjectTransform: kubeclient.StripPodUnusedFields,
			}
		},
	))
	// Pod informers indexable by cluster ID
	podInformersByCluster := informerIndexByCluster(allPodInformers)

	allServicesInformers := krt.NewCollection(clusters, informerForCluster[*v1.Service](
		filter,
		"Services",
		opts,
		nil,
	))
	serviceInformersByCluster := informerIndexByCluster(allServicesInformers)

	allGateways := krt.NewCollection(clusters, informerForCluster[*v1beta1.Gateway](
		filter,
		"Gateways",
		opts,
		nil,
	))
	gatewayInformersByCluster := informerIndexByCluster(allGateways)
	globalGateways := krt.NewCollection(clusters, collectionFromCluster[*v1beta1.Gateway]("Gateways", gatewayInformersByCluster, opts))
	globalGatewaysByCluster := nestedCollectionIndexByCluster(globalGateways)

	allNamespaces := krt.NewCollection(clusters, informerForCluster[*v1.Namespace](
		filter,
		"Namespaces",
		opts,
		nil,
	))
	namespaceInformersByCluster := informerIndexByCluster(allNamespaces)

	allEndpointSlices := krt.NewCollection(clusters, informerForCluster[*discovery.EndpointSlice](
		filter,
		"EndpointSlices",
		opts,
		nil,
	))
	endointSliceInformersByCluster := informerIndexByCluster(allEndpointSlices)

	allNodes := krt.NewCollection(clusters, informerForCluster[*v1.Node](
		filter,
		"Nodes",
		opts,
		func(c Cluster) kclient.Filter {
			return kclient.Filter{
				ObjectFilter:    c.Client.ObjectFilter(),
				ObjectTransform: kubeclient.StripNodeUnusedFields,
			}
		},
	), opts.WithName("AllNodes")...)
	nodeInformersByCluster := informerIndexByCluster(allNodes)
	globalNodes := krt.NewCollection(clusters, collectionFromCluster[*v1.Node]("Nodes", nodeInformersByCluster, opts))
	// Set up collections for remote clusters
	GlobalNetworks := buildGlobalNetworkCollections(
		clusters,
		LocalNamespaces,
		globalGatewaysByCluster,
		options,
		opts,
	)
	a.networks = GlobalNetworks
	GlobalWaypoints := GlobalWaypointsCollection(
		clusters,
		LocalGatewayClasses,
		allGateways,
		gatewayInformersByCluster,
		allPodInformers,
		podInformersByCluster,
		GlobalNetworks,
		opts,
	)

	WaypointsPerCluster := nestedCollectionIndexByCluster(GlobalWaypoints)
	LocalWaypoints := a.WaypointsCollection(LocalGateways, LocalGatewayClasses, LocalPods, opts)
	// AllPolicies includes peer-authentication converted policies
	AuthorizationPolicies, AllPolicies := PolicyCollections(LocalAuthzPolicies, LocalPeerAuths, LocalMeshConfig, LocalWaypoints, opts, a.Flags)
	AllPolicies.RegisterBatch(PushXds(a.XDSUpdater,
		func(i model.WorkloadAuthorization) model.ConfigKey {
			if i.Authorization == nil {
				return model.ConfigKey{} // nop, filter this out
			}
			return model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace}
		}), false)

	// Now we get to collections where we can actually merge duplicate keys, so we can use nested collections
	GlobalWorkloadServices := GlobalMergedWorkloadServicesCollection(
		clusters,
		LocalServiceEntries,
		allServicesInformers,
		serviceInformersByCluster,
		GlobalWaypoints,
		WaypointsPerCluster,
		allNamespaces,
		namespaceInformersByCluster,
		GlobalNetworks,
		options.DomainSuffix,
		options.ClusterID,
		opts,
	)

	ServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](GlobalWorkloadServices, networkAddressFromService)
	ServiceInfosByOwningWaypointHostname := krt.NewIndex(GlobalWorkloadServices, func(s model.ServiceInfo) []NamespaceHostname {
		// Filter out waypoint services
		// TODO: we are looking at the *selector* -- we should be looking the labels themselves or something equivalent.
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := s.Service.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetHostname()
		if waypointAddress == nil {
			return nil
		}

		return []NamespaceHostname{{
			Namespace: waypointAddress.Namespace,
			Hostname:  waypointAddress.Hostname,
		}}
	})
	ServiceInfosByOwningWaypointIP := krt.NewIndex(GlobalWorkloadServices, func(s model.ServiceInfo) []networkAddress {
		// Filter out waypoint services
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := s.Service.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetAddress()
		if waypointAddress == nil {
			return nil
		}
		netip, _ := netip.AddrFromSlice(waypointAddress.Address)
		netaddr := networkAddress{
			network: waypointAddress.Network,
			ip:      netip.String(),
		}

		return []networkAddress{netaddr}
	})
	GlobalWorkloadServices.RegisterBatch(krt.BatchedEventFilter(
		func(a model.ServiceInfo) *workloadapi.Service {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Service
		},
		PushXdsAddress(a.XDSUpdater, model.ServiceInfo.ResourceName),
	), false)

	LocalNamespacesInfo := krt.NewCollection(LocalNamespaces, func(ctx krt.HandlerContext, ns *v1.Namespace) *model.NamespaceInfo {
		return &model.NamespaceInfo{
			Name:               ns.Name,
			IngressUseWaypoint: strings.EqualFold(ns.Labels["istio.io/ingress-use-waypoint"], "true"),
		}
	}, opts.WithName("LocalNamespacesInfo")...)

	GlobalNodeLocality := GlobalNodesCollection(globalNodes, opts.Stop(), opts.WithName("GlobalNodeLocality")...)
	GlobalNodeLocalityByCluster := nestedCollectionIndexByCluster(GlobalNodeLocality)
	// TODO: support PILOT_ENABLE_CROSS_CLUSTER_WORKLOAD_ENTRY
	GlobalWorkloads := MergedGlobalWorkloadsCollection(
		clusters,
		LocalWorkloadEntries,
		LocalServiceEntries,
		allPodInformers,
		podInformersByCluster,
		GlobalNodeLocality,
		GlobalNodeLocalityByCluster,
		options.MeshConfig,
		AuthorizationPolicies,
		LocalPeerAuths,
		GlobalWaypoints,
		WaypointsPerCluster,
		GlobalWorkloadServices,
		allEndpointSlices,
		endointSliceInformersByCluster,
		allNamespaces,
		namespaceInformersByCluster,
		GlobalNetworks,
		options.ClusterID,
		options.Flags,
		options.DomainSuffix,
		opts,
	)
	WorkloadAddressIndex := krt.NewIndex[networkAddress, model.WorkloadInfo](GlobalWorkloads, networkAddressFromWorkload)
	WorkloadServiceIndex := krt.NewIndex[string, model.WorkloadInfo](GlobalWorkloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Workload.Services)
	})
	WorkloadWaypointIndexHostname := krt.NewIndex(GlobalWorkloads, func(w model.WorkloadInfo) []NamespaceHostname {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := w.Workload.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetHostname()
		if waypointAddress == nil {
			return nil
		}

		return []NamespaceHostname{{
			Namespace: waypointAddress.Namespace,
			Hostname:  waypointAddress.Hostname,
		}}
	})
	WorkloadWaypointIndexIP := krt.NewIndex(GlobalWorkloads, func(w model.WorkloadInfo) []networkAddress {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := w.Workload.Waypoint
		if waypoint == nil {
			return nil
		}

		waypointAddress := waypoint.GetAddress()
		if waypointAddress == nil {
			return nil
		}
		netip, _ := netip.AddrFromSlice(waypointAddress.Address)
		netaddr := networkAddress{
			network: waypointAddress.Network,
			ip:      netip.String(),
		}

		return []networkAddress{netaddr}
	})
	GlobalWorkloads.RegisterBatch(krt.BatchedEventFilter(
		func(a model.WorkloadInfo) *workloadapi.Workload {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Workload
		},
		PushXdsAddress(a.XDSUpdater, model.WorkloadInfo.ResourceName),
	), false)

	if features.EnableIngressWaypointRouting {
		RegisterEdsShim(
			a.XDSUpdater,
			GlobalWorkloads,
			LocalNamespacesInfo,
			WorkloadServiceIndex,
			GlobalWorkloadServices,
			ServiceAddressIndex,
			opts,
		)
	}
	a.namespaces = LocalNamespacesInfo

	a.workloads = workloadsCollection{
		Collection:               GlobalWorkloads,
		ByAddress:                WorkloadAddressIndex,
		ByServiceKey:             WorkloadServiceIndex,
		ByOwningWaypointHostname: WorkloadWaypointIndexHostname,
		ByOwningWaypointIP:       WorkloadWaypointIndexIP,
	}

	a.services = servicesCollection{
		Collection:               GlobalWorkloadServices,
		ByAddress:                ServiceAddressIndex,
		ByOwningWaypointHostname: ServiceInfosByOwningWaypointHostname,
		ByOwningWaypointIP:       ServiceInfosByOwningWaypointIP,
	}
	a.authorizationPolicies = AllPolicies
	// TODO: Should this be global?
	a.waypoints = waypointsCollection{
		Collection: LocalWaypoints,
	}

	return nil
}

func informerForCluster[T controllers.ComparableObject](
	filter kclient.Filter,
	name string,
	opts krt.OptionsBuilder,
	filterTransform func(c Cluster) kclient.Filter,
) krt.TransformationSingle[Cluster, krt.Collection[T]] {
	return func(ctx krt.HandlerContext, c Cluster) *krt.Collection[T] {
		if filterTransform != nil {
			filter = filterTransform(c)
		}
		client := kclient.NewFiltered[T](c.Client, filter)
		inf := krt.WrapClient[T](client, opts.With(
			krt.WithName("cluster["+string(c.ID)+"]/informer/"+name),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...)

		return ptr.Of(inf)
	}
}

func collectionFromCluster[T controllers.ComparableObject](
	name string,
	informerIndex krt.Index[cluster.ID, krt.Collection[T]],
	opts krt.OptionsBuilder,
) krt.TransformationSingle[Cluster, krt.Collection[config.ObjectWithCluster[T]]] {
	return func(ctx krt.HandlerContext, c Cluster) *krt.Collection[config.ObjectWithCluster[T]] {
		clients := informerIndex.Lookup(c.ID)
		if len(clients) == 0 {
			return nil
		}
		objWithCluster := krt.NewCollection(clients[0], func(ctx krt.HandlerContext, obj T) *config.ObjectWithCluster[T] {
			return &config.ObjectWithCluster[T]{ClusterID: c.ID, Object: &obj}
		}, opts.With(
			krt.WithName(name+"WithCluster"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...)
		return &objWithCluster
	}
}

func informerIndexByCluster[T controllers.ComparableObject](
	informerCollection krt.Collection[krt.Collection[T]],
) krt.Index[cluster.ID, krt.Collection[T]] {
	return krt.NewIndex[cluster.ID, krt.Collection[T]](informerCollection, func(col krt.Collection[T]) []cluster.ID {
		val, ok := col.Metadata()[ClusterKRTMetadataKey]
		if !ok {
			log.Warnf("Cluster metadata not set on collection %v", col)
			return nil
		}
		id, ok := val.(cluster.ID)
		if !ok {
			log.Warnf("Invalid cluster metadata set on collection %v: %v", col, val)
			return nil
		}
		return []cluster.ID{id}
	})
}

func nestedCollectionIndexByCluster[T any](
	collection krt.Collection[krt.Collection[T]],
) krt.Index[cluster.ID, krt.Collection[T]] {
	return krt.NewIndex[cluster.ID, krt.Collection[T]](collection, func(col krt.Collection[T]) []cluster.ID {
		val, ok := col.Metadata()[ClusterKRTMetadataKey]
		if !ok {
			log.Warnf("Cluster metadata not set on collection %v", col)
			return nil
		}
		id, ok := val.(cluster.ID)
		if !ok {
			log.Warnf("Invalid cluster metadata set on collection %v: %v", col, val)
			return nil
		}
		return []cluster.ID{id}
	})
}

func mergeServiceInfosWithCluster(localClusterID cluster.ID) func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
	return func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
		if len(serviceInfos) == 0 {
			return nil
		}
		if len(serviceInfos) == 1 {
			return &serviceInfos[0]
		}

		// TODO: this is inaccurate; VIPs need to be scoped
		var vips sets.Set[*workloadapi.NetworkAddress]
		var sans sets.Set[string]
		ports := map[string]sets.Set[*workloadapi.Port]{}
		var base *config.ObjectWithCluster[model.ServiceInfo]
		for _, obj := range serviceInfos {
			if obj.Object == nil {
				continue
			}
			ports[obj.Object.Source.String()] = sets.New(obj.Object.Service.Ports...)
			vips.InsertAll(obj.Object.Service.GetAddresses()...)
			sans.InsertAll(obj.Object.Service.GetSubjectAltNames()...)
			if obj.ClusterID == localClusterID {
				if obj.Object.Source.Kind == kind.ServiceEntry {
					// If there's a service entry that's considered external to the mesh, we
					// prioritize that
					base = &obj
				} else if base == nil {
					base = &obj
				}
			}
		}

		if base.Object == nil {
			// No local objects found, so just use the first one
			base = &serviceInfos[0]
		}

		basePorts := sets.New(base.Object.Service.Ports...)
		for source, portSet := range ports {
			if !portSet.Equals(basePorts) {
				log.Warnf("ServiceInfo derived from %s has mismatched ports. Base ports %v != %v", source, basePorts, portSet)
			}
		}

		// TODO: Do we need to merge anything else?
		base.Object.Service.Addresses = vips.UnsortedList()
		base.Object.Service.SubjectAltNames = sans.UnsortedList()

		// Rememeber, we have to re-precompute the serviceinfo since we changed it
		return &config.ObjectWithCluster[model.ServiceInfo]{
			ClusterID: base.ClusterID,
			Object:    precomputeServicePtr(base.Object),
		}
	}
}

func mergeWorkloadInfosWithCluster(localClusterID cluster.ID) func(workloadInfos []config.ObjectWithCluster[model.WorkloadInfo]) *config.ObjectWithCluster[model.WorkloadInfo] {
	return func(workloadInfos []config.ObjectWithCluster[model.WorkloadInfo]) *config.ObjectWithCluster[model.WorkloadInfo] {
		if len(workloadInfos) == 0 {
			return nil
		}
		if len(workloadInfos) == 1 {
			return &workloadInfos[0]
		}

		log.Warnf("Duplicate workloadinfos found for %v. Trying to take local and then the first if we can't find it", workloadInfos)
		for _, obj := range workloadInfos {
			if obj.Object == nil {
				continue
			}
			if obj.ClusterID == localClusterID {
				return &obj
			}
		}

		log.Warnf("No local workloadinfo found for %v. Taking the first", workloadInfos)
		return &workloadInfos[0]
	}
}

func unwrapObjectWithCluster[T any](obj config.ObjectWithCluster[T]) T {
	if obj.Object == nil {
		return ptr.Empty[T]()
	}
	return *obj.Object
}

func wrapObjectWithCluster[T any](clusterID cluster.ID) func(obj T) config.ObjectWithCluster[T] {
	return func(obj T) config.ObjectWithCluster[T] {
		return config.ObjectWithCluster[T]{ClusterID: clusterID, Object: &obj}
	}
}
