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
	"fmt"
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
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/workloadapi"
)

const ClusterKRTMetadataKey = "cluster"

func (a *index) buildGlobalCollections(options Options, opts krt.OptionsBuilder) error {
	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	clusters := buildRemoteClustersCollection(
		options,
		opts,
		multicluster.DefaultBuildClientsFromConfig,
		filter,
	)
	a.remoteClusters = clusters

	LocalMeshConfig := options.MeshConfig
	localAuthzPolicies := kclient.NewDelayedInformer[*securityclient.AuthorizationPolicy](options.Client,
		gvr.AuthorizationPolicy, kubetypes.StandardInformer, filter)
	LocalAuthzPolicies := krt.WrapClient[*securityclient.AuthorizationPolicy](localAuthzPolicies, opts.WithName("informer/AuthorizationPolicies")...)
	localPeerAuths := kclient.NewDelayedInformer[*securityclient.PeerAuthentication](options.Client,
		gvr.PeerAuthentication, kubetypes.StandardInformer, filter)
	LocalPeerAuths := krt.WrapClient[*securityclient.PeerAuthentication](localPeerAuths, opts.WithName("informer/LocalPeerAuthentications")...)

	// These first collections can't be merged since the Kubernetes APIs don't have enough room
	// for e.g. duplicate IPs, etc. So we keep around collections of collections and indexes per cluster.
	// TODO: come back and see if we use all of these indexes
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
	), opts.WithName("AllPodInformers")...)
	podInformersByCluster := informerIndexByCluster(allPodInformers)
	globalPods := krt.NewCollection(clusters, collectionFromCluster[*v1.Pod]("Pods", podInformersByCluster, opts))
	globalPodsByCluster := nestedCollectionIndexByCluster(globalPods)

	allServicesInformers := krt.NewCollection(clusters, informerForCluster[*v1.Service](
		filter,
		"Services",
		opts,
		nil,
	), opts.WithName("AllServicesInformers")...)
	serviceInformersByCluster := informerIndexByCluster(allServicesInformers)
	globalServices := krt.NewCollection(clusters, collectionFromCluster[*v1.Service]("Services", serviceInformersByCluster, opts))
	globalServicesByCluster := nestedCollectionIndexByCluster(globalServices)

	allServiceEntries := krt.NewCollection(clusters, informerForCluster[*networkingclient.ServiceEntry](
		filter,
		"ServiceEntries",
		opts,
		nil,
	), opts.WithName("AllServiceEntriesInformers")...)
	serviceEntryInformersByCluster := informerIndexByCluster(allServiceEntries)
	globalServiceEntries := krt.NewCollection(clusters, collectionFromCluster[*networkingclient.ServiceEntry]("ServiceEntries", serviceEntryInformersByCluster, opts))
	globalServiceEntriesByCluster := nestedCollectionIndexByCluster(globalServiceEntries)

	allWorkloadEntries := krt.NewCollection(clusters, informerForCluster[*networkingclient.WorkloadEntry](
		filter,
		"WorkloadEntries",
		opts,
		nil,
	), opts.WithName("AllWorkloadEntriesInformers")...)
	workloadEntryInformersByCluster := informerIndexByCluster(allWorkloadEntries)
	globalWorkloadEntries := krt.NewCollection(clusters, collectionFromCluster[*networkingclient.WorkloadEntry]("WorkloadEntries", workloadEntryInformersByCluster, opts))
	globalWorkloadEntriesByCluster := nestedCollectionIndexByCluster(globalWorkloadEntries)

	allGateways := krt.NewCollection(clusters, informerForCluster[*v1beta1.Gateway](
		filter,
		"Gateways",
		opts,
		nil,
	), opts.WithName("AllGatewaysInformers")...)
	gatewayInformersByCluster := informerIndexByCluster(allGateways)
	globalGateways := krt.NewCollection(clusters, collectionFromCluster[*v1beta1.Gateway]("Gateways", gatewayInformersByCluster, opts))
	globalGatewaysByCluster := nestedCollectionIndexByCluster(globalGateways)

	allGatewayClasses := krt.NewCollection(clusters, informerForCluster[*v1beta1.GatewayClass](
		filter,
		"GatewayClasses",
		opts,
		nil,
	), opts.WithName("AllGatewayClassesInformers")...)
	gatewayClassInformersByCluster := informerIndexByCluster(allGatewayClasses)
	globalGatewayClasses := krt.NewCollection(clusters, collectionFromCluster[*v1beta1.GatewayClass]("GatewayClasses", gatewayClassInformersByCluster, opts))
	globalGatewayClassesByCluster := nestedCollectionIndexByCluster(globalGatewayClasses)

	allNamespaces := krt.NewCollection(clusters, informerForCluster[*v1.Namespace](
		filter,
		"Namespaces",
		opts,
		nil,
	), opts.WithName("AllNamespacesInformers")...)
	namespaceInformersByCluster := informerIndexByCluster(allNamespaces)
	globalNamespaces := krt.NewCollection(clusters, collectionFromCluster[*v1.Namespace]("Namespaces", namespaceInformersByCluster, opts))
	globalNamespacesByCluster := nestedCollectionIndexByCluster(globalNamespaces)

	allEndpointSlices := krt.NewCollection(clusters, informerForCluster[*discovery.EndpointSlice](
		filter,
		"EndpointSlices",
		opts,
		nil,
	), opts.WithName("AllEndpointSlices")...)
	endointSliceInformersByCluster := informerIndexByCluster(allEndpointSlices)
	globalEndpointSlices := krt.NewCollection(clusters, collectionFromCluster[*discovery.EndpointSlice]("EndpointSlice", endointSliceInformersByCluster, opts))
	globalEndpointSlicesByCluster := nestedCollectionIndexByCluster(globalEndpointSlices)

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
		globalGatewaysByCluster,
		options,
		opts,
	)
	a.networks = GlobalNetworks
	GlobalWaypoints := GlobalWaypointsCollection(
		clusters,
		globalGatewaysByCluster,
		globalGatewayClassesByCluster,
		globalPodsByCluster,
		GlobalNetworks.SystemNamespaceNetworkByCluster,
		opts,
	)
	// We need to get the waypoints for the local cluster, so we need to wait for the
	// collection to be synced
	if !GlobalWaypoints.WaitUntilSynced(opts.Stop()) {
		return fmt.Errorf("could not sync global waypoints collection")
	}
	WaypointsPerCluster := nestedCollectionIndexByCluster(GlobalWaypoints)
	waypointCollections := WaypointsPerCluster.Lookup(options.ClusterID)
	if len(waypointCollections) == 0 {
		return fmt.Errorf("could not find waypoint collection for local cluster %s", options.ClusterID)
	}
	LocalWaypointsWithCluster := waypointCollections[0]
	LocalWaypoints := krt.MapCollection(LocalWaypointsWithCluster, func(obj config.ObjectWithCluster[Waypoint]) Waypoint {
		if obj.Object == nil {
			return Waypoint{}
		}
		return *obj.Object
	})
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
	GlobalWorkloadServices := GlobalMergedServicesCollection(
		clusters,
		globalServicesByCluster,
		globalServiceEntriesByCluster,
		WaypointsPerCluster,
		globalNamespacesByCluster,
		GlobalNetworks.SystemNamespaceNetworkByCluster,
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

	// TODO: I doubt we really care about whether remote namespaces set ingress-use-waypoint, but should double check
	LocalNamespaces := globalNamespacesByCluster.Lookup(options.ClusterID)
	if len(LocalNamespaces) == 0 {
		return fmt.Errorf("could not find local namespaces collection for local cluster %s", options.ClusterID)
	}
	LocalNamespacesWithCluster := LocalNamespaces[0]
	LocalNamespacesInfo := krt.NewCollection(LocalNamespacesWithCluster, func(ctx krt.HandlerContext, i config.ObjectWithCluster[*v1.Namespace]) *model.NamespaceInfo {
		if i.Object == nil {
			return nil
		}
		ns := ptr.Flatten(i.Object)
		return &model.NamespaceInfo{
			Name:               ns.Name,
			IngressUseWaypoint: strings.EqualFold(ns.Labels["istio.io/ingress-use-waypoint"], "true"),
		}
	}, opts.WithName("LocalNamespacesInfo")...)

	GlobalNodeLocality := GlobalNodesCollection(globalNodes, opts.WithName("GlobalNodeLocality")...)
	GlobalNodeLocalityByCluster := nestedCollectionIndexByCluster(GlobalNodeLocality)
	GlobalWorkloads := MergedGlobalWorkloadsCollection(
		clusters,
		globalPodsByCluster,
		GlobalNodeLocalityByCluster,
		options.MeshConfig,
		AuthorizationPolicies,
		LocalPeerAuths,
		WaypointsPerCluster,
		GlobalWorkloadServices,
		globalWorkloadEntriesByCluster,
		globalServiceEntriesByCluster,
		globalEndpointSlicesByCluster,
		globalNamespacesByCluster,
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
		return ptr.Of(krt.WrapClient[T](client, opts.With(
			krt.WithName("cluster/"+string(c.ID)+"informer/"+name),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...))
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
	collection krt.Collection[krt.Collection[config.ObjectWithCluster[T]]],
) krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[T]]] {
	return krt.NewIndex[cluster.ID, krt.Collection[config.ObjectWithCluster[T]]](collection, func(col krt.Collection[config.ObjectWithCluster[T]]) []cluster.ID {
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

		var vips []*workloadapi.NetworkAddress
		var sans []string
		var base config.ObjectWithCluster[model.ServiceInfo]
		for _, obj := range serviceInfos {
			if obj.Object == nil {
				continue
			}

			vips = append(vips, obj.Object.Service.GetAddresses()...)
			sans = append(sans, obj.Object.Service.GetSubjectAltNames()...)
			if obj.ClusterID == localClusterID {
				// This is the base object we'll append to since it's local
				base = obj
			}
		}

		// TODO: Do we need to merge anything else?
		base.Object.Service.Addresses = vips
		base.Object.Service.SubjectAltNames = sans

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
