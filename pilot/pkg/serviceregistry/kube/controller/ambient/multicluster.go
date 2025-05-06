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

	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/statusqueue"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) buildGlobalCollections(
	localCluster *Cluster,
	localAuthzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	localPeerAuths krt.Collection[*securityclient.PeerAuthentication],
	localGatewayClasses krt.Collection[*v1beta1.GatewayClass],
	localWorkloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	localServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	localServiceEntryInformers kclient.Informer[*networkingclient.ServiceEntry],
	localServiceInformers kclient.Informer[*v1.Service],
	localAuthzInformers kclient.Informer[*securityclient.AuthorizationPolicy],
	options Options,
	opts krt.OptionsBuilder,
	configOverrides ...func(*rest.Config),
) {
	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	clusters := a.buildRemoteClustersCollection(
		options,
		opts,
		configOverrides...,
	)

	a.remoteClusters = clusters

	LocalPods := localCluster.pods
	LocalServices := localCluster.services
	LocalNamespaces := localCluster.namespaces
	LocalNodes := localCluster.nodes
	LocalEndpointSlices := localCluster.endpointSlices
	LocalGateways := localCluster.gateways
	LocalWaypoints := a.WaypointsCollection(options.ClusterID, LocalGateways, localGatewayClasses, LocalPods, opts)

	LocalMeshConfig := options.MeshConfig
	// These first collections can't be merged since the Kubernetes APIs don't have enough room
	// for e.g. duplicate IPs, etc. So we keep around collections of collections and indexes per cluster.
	GlobalPods := nestedCollectionFromLocalAndRemote(LocalPods, clusters, func(_ krt.HandlerContext, c *Cluster) *krt.Collection[*v1.Pod] {
		return ptr.Of(c.pods)
	}, "Pods", opts)
	// Pod informers indexable by cluster ID
	podInformersByCluster := informerIndexByCluster(GlobalPods)

	GlobalEndpointSlices := nestedCollectionFromLocalAndRemote(LocalEndpointSlices, clusters, func(_ krt.HandlerContext, c *Cluster) *krt.Collection[*discovery.EndpointSlice] {
		return ptr.Of(c.endpointSlices)
	}, "EndpointSlices", opts)
	endpointSliceInformersByCluster := informerIndexByCluster(GlobalEndpointSlices)

	GlobalServices := nestedCollectionFromLocalAndRemote(LocalServices, clusters, func(_ krt.HandlerContext, c *Cluster) *krt.Collection[*v1.Service] {
		return ptr.Of(c.services)
	}, "Services", opts)
	serviceInformersByCluster := informerIndexByCluster(GlobalServices)

	GlobalGateways := nestedCollectionFromLocalAndRemote(LocalGateways, clusters, func(_ krt.HandlerContext, c *Cluster) *krt.Collection[*v1beta1.Gateway] {
		return ptr.Of(c.gateways)
	}, "Gateways", opts)
	gatewayInformersByCluster := informerIndexByCluster(GlobalGateways)

	LocalGatewaysWithCluster := krt.MapCollection(LocalGateways, func(obj *v1beta1.Gateway) config.ObjectWithCluster[*v1beta1.Gateway] {
		return config.ObjectWithCluster[*v1beta1.Gateway]{
			ClusterID: localCluster.ID,
			Object:    &obj,
		}
	}, opts.WithName("LocalGatewaysWithCluster")...)
	GlobalGatewaysWithCluster := nestedCollectionFromLocalAndRemote(
		LocalGatewaysWithCluster,
		clusters,
		func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[config.ObjectWithCluster[*v1beta1.Gateway]] {
			if !kube.WaitForCacheSync(fmt.Sprintf("ambient/informer/gateways[%s]", c.ID), a.stop, c.gateways.HasSynced) {
				log.Warnf("Failed to sync gateways informer for cluster %s", c.ID)
				ctx.DiscardResult()
				return nil
			}
			return ptr.Of(krt.MapCollection(c.gateways, func(obj *v1beta1.Gateway) config.ObjectWithCluster[*v1beta1.Gateway] {
				return config.ObjectWithCluster[*v1beta1.Gateway]{
					ClusterID: c.ID,
					Object:    &obj,
				}
			}, opts.WithName(fmt.Sprintf("GatewaysWithCluster[%s]", c.ID))...))
		}, "GatewaysWithCluster", opts)

	globalGatewaysByCluster := nestedCollectionIndexByCluster(GlobalGatewaysWithCluster)

	GlobalNamespaces := nestedCollectionFromLocalAndRemote(LocalNamespaces, clusters, func(_ krt.HandlerContext, c *Cluster) *krt.Collection[*v1.Namespace] {
		return ptr.Of(c.namespaces)
	}, "Namespaces", opts)
	namespaceInformersByCluster := informerIndexByCluster(GlobalNamespaces)

	LocalNodesWithCluster := krt.MapCollection(LocalNodes, func(obj *v1.Node) config.ObjectWithCluster[*v1.Node] {
		return config.ObjectWithCluster[*v1.Node]{
			ClusterID: localCluster.ID,
			Object:    &obj,
		}
	}, opts.WithName("LocalNodesWithCluster")...)
	GlobalNodesWithCluster := nestedCollectionFromLocalAndRemote(
		LocalNodesWithCluster,
		clusters,
		func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[config.ObjectWithCluster[*v1.Node]] {
			if !kube.WaitForCacheSync(fmt.Sprintf("ambient/informer/nodes[%s]", c.ID), a.stop, c.nodes.HasSynced) {
				log.Warnf("Failed to sync nodes informer for cluster %s", c.ID)
				ctx.DiscardResult()
				return nil
			}
			return ptr.Of(krt.MapCollection(c.nodes, func(obj *v1.Node) config.ObjectWithCluster[*v1.Node] {
				return config.ObjectWithCluster[*v1.Node]{
					ClusterID: c.ID,
					Object:    &obj,
				}
			}, opts.WithName(fmt.Sprintf("NodesWithCluster[%s]", c.ID))...))
		}, "NodesWithCluster", opts)
	// Set up collections for remote clusters
	GlobalNetworks := buildGlobalNetworkCollections(
		clusters,
		LocalNamespaces,
		LocalGateways,
		GlobalGatewaysWithCluster,
		globalGatewaysByCluster,
		options,
		opts,
	)
	a.networks = GlobalNetworks

	// We need this because there may be services and waypoints on remote clusters that aren't represented
	// in our local config cluster
	GlobalWaypoints := GlobalWaypointsCollection(
		localCluster,
		LocalWaypoints,
		clusters,
		localGatewayClasses,
		GlobalGateways,
		gatewayInformersByCluster,
		GlobalPods,
		podInformersByCluster,
		GlobalNetworks,
		opts,
	)

	WaypointsByCluster := nestedCollectionIndexByCluster(GlobalWaypoints)
	// AllPolicies includes peer-authentication converted policies
	AuthorizationPolicies, AllPolicies := PolicyCollections(localAuthzPolicies, localPeerAuths, LocalMeshConfig, LocalWaypoints, opts, a.Flags)
	AllPolicies.RegisterBatch(PushXds(a.XDSUpdater,
		func(i model.WorkloadAuthorization) model.ConfigKey {
			if i.Authorization == nil {
				return model.ConfigKey{} // nop, filter this out
			}
			return model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace}
		}), false)

	LocalWorkloadServices := a.ServicesCollection(localCluster.ID, localCluster.services, localServiceEntries, LocalWaypoints, LocalNamespaces, opts)
	// All of this is local only, but we need to do it here so we don't have to rebuild collections in ambientindex
	if features.EnableAmbientStatus {
		serviceEntriesWriter := kclient.NewWriteClient[*networkingclient.ServiceEntry](options.Client)
		servicesWriter := kclient.NewWriteClient[*v1.Service](options.Client)
		authorizationPoliciesWriter := kclient.NewWriteClient[*securityclient.AuthorizationPolicy](options.Client)

		WaypointPolicyStatus := WaypointPolicyStatusCollection(
			localAuthzPolicies,
			LocalWaypoints,
			localCluster.services,
			localServiceEntries,
			localGatewayClasses,
			LocalMeshConfig,
			localCluster.namespaces,
			opts,
		)
		statusQueue := statusqueue.NewQueue(options.StatusNotifier)
		statusqueue.Register(statusQueue, "istio-ambient-service", LocalWorkloadServices,
			func(info model.ServiceInfo) (kclient.Patcher, map[string]model.Condition) {
				// Since we have 1 collection for multiple types, we need to split these out
				if info.Source.Kind == kind.ServiceEntry {
					return kclient.ToPatcher(serviceEntriesWriter), getConditions(info.Source.NamespacedName, localServiceEntryInformers)
				}
				return kclient.ToPatcher(servicesWriter), getConditions(info.Source.NamespacedName, localServiceInformers)
			})
		statusqueue.Register(statusQueue, "istio-ambient-ztunnel-policy", AuthorizationPolicies,
			func(pol model.WorkloadAuthorization) (kclient.Patcher, map[string]model.Condition) {
				return kclient.ToPatcher(authorizationPoliciesWriter), getConditions(pol.Source.NamespacedName, localAuthzInformers)
			})
		statusqueue.Register(statusQueue, "istio-ambient-waypoint-policy", WaypointPolicyStatus,
			func(pol model.WaypointPolicyStatus) (kclient.Patcher, map[string]model.Condition) {
				return kclient.ToPatcher(authorizationPoliciesWriter), getConditions(pol.Source.NamespacedName, localAuthzInformers)
			})
		a.statusQueue = statusQueue
	}
	// Now we get to collections where we can actually merge duplicate keys, so we can use nested collections
	GlobalWorkloadServicesWithCluster := GlobalMergedWorkloadServicesCollection(
		localCluster,
		LocalWorkloadServices,
		LocalWaypoints,
		clusters,
		localServiceEntries,
		GlobalServices,
		serviceInformersByCluster,
		GlobalWaypoints,
		WaypointsByCluster,
		GlobalNamespaces,
		namespaceInformersByCluster,
		GlobalNetworks,
		options.DomainSuffix,
		opts,
	)

	GlobalMergedWorkloadServicesWithCluster := krt.NestedJoinWithMergeCollection(
		GlobalWorkloadServicesWithCluster,
		mergeServiceInfosWithCluster(localCluster.ID),
		opts.WithName("GlobalMergedServiceInfosWithCluster")...,
	)

	GlobalMergedWorkloadServices := krt.MapCollection(
		GlobalMergedWorkloadServicesWithCluster,
		unwrapObjectWithCluster,
		opts.WithName("GlobalMergedServiceInfos")...,
	)

	// TODO: confirm expected functionality before we register
	GlobalMergedWorkloadServices.RegisterBatch(krt.BatchedEventFilter(
		func(a model.ServiceInfo) *workloadapi.Service {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Service
		},
		PushXdsAddress(a.XDSUpdater, model.ServiceInfo.ResourceName),
	), true)

	GobalWorkloadServicesWithClusterByCluster := nestedCollectionIndexByCluster(GlobalWorkloadServicesWithCluster)
	LocalServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](LocalWorkloadServices, "serviceAddress", networkAddressFromService)
	ServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](GlobalMergedWorkloadServices, "serviceAddress", networkAddressFromService)
	ServiceInfosByOwningWaypointHostname := krt.NewIndex(GlobalMergedWorkloadServices, "namespaceHostname", func(s model.ServiceInfo) []NamespaceHostname {
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
	ServiceInfosByOwningWaypointIP := krt.NewIndex(GlobalMergedWorkloadServices, "owningWaypointIp", func(s model.ServiceInfo) []networkAddress {
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

	LocalNamespacesInfo := krt.NewCollection(LocalNamespaces, func(ctx krt.HandlerContext, ns *v1.Namespace) *model.NamespaceInfo {
		return &model.NamespaceInfo{
			Name:               ns.Name,
			IngressUseWaypoint: strings.EqualFold(ns.Labels["istio.io/ingress-use-waypoint"], "true"),
		}
	}, opts.WithName("LocalNamespacesInfo")...)

	LocalNodeLocality := NodesCollection(
		LocalNodes,
		opts.WithName("LocalNodeLocality")...,
	)
	GlobalNodeLocality := GlobalNodesCollection(GlobalNodesWithCluster, opts.Stop(), opts.WithName("GlobalNodeLocalityWithCluster")...)
	GlobalNodeLocalityByCluster := nestedCollectionIndexByCluster(GlobalNodeLocality)

	GlobalWorkloads := MergedGlobalWorkloadsCollection(
		localCluster,
		LocalWaypoints,
		LocalNodeLocality,
		clusters,
		localWorkloadEntries,
		localServiceEntries,
		GlobalPods,
		podInformersByCluster,
		GlobalNodeLocality,
		GlobalNodeLocalityByCluster,
		options.MeshConfig,
		AuthorizationPolicies,
		localPeerAuths,
		GlobalWaypoints,
		WaypointsByCluster,
		LocalWorkloadServices,
		GlobalWorkloadServicesWithCluster,
		GobalWorkloadServicesWithClusterByCluster,
		GlobalEndpointSlices,
		endpointSliceInformersByCluster,
		GlobalNamespaces,
		namespaceInformersByCluster,
		GlobalNetworks,
		options.ClusterID,
		options.Flags,
		options.DomainSuffix,
		opts,
	)

	WorkloadAddressIndex := krt.NewIndex[networkAddress, model.WorkloadInfo](GlobalWorkloads, "networkAddress", networkAddressFromWorkload)
	WorkloadServiceIndex := krt.NewIndex[string, model.WorkloadInfo](GlobalWorkloads, "service", func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Workload.Services)
	})
	WorkloadWaypointIndexHostname := krt.NewIndex(GlobalWorkloads, "namespaceHostname", func(w model.WorkloadInfo) []NamespaceHostname {
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
	WorkloadWaypointIndexIP := krt.NewIndex(GlobalWorkloads, "waypointIp", func(w model.WorkloadInfo) []networkAddress {
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
	// TODO: confirm expected functionality before we register
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
			GlobalMergedWorkloadServices,
			LocalServiceAddressIndex, // TODO: should we consider allowing ingress -> remote services?
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
		Collection:               GlobalMergedWorkloadServices,
		ByAddress:                ServiceAddressIndex,
		ByOwningWaypointHostname: ServiceInfosByOwningWaypointHostname,
		ByOwningWaypointIP:       ServiceInfosByOwningWaypointIP,
	}
	a.authorizationPolicies = AllPolicies
	// TODO: Should this be the set of global waypoints?
	// Probably yes, but coming back to it in a follow up
	a.waypoints = waypointsCollection{
		Collection: LocalWaypoints,
	}
}

func nestedCollectionFromLocalAndRemote[T any](
	localCollection krt.Collection[T],
	clustersCollection krt.Collection[*Cluster],
	clusterToCollection krt.TransformationSingle[*Cluster, krt.Collection[T]],
	name string,
	opts krt.OptionsBuilder,
) krt.Collection[krt.Collection[T]] {
	globalCollection := krt.NewStaticCollection(
		localCollection,
		[]krt.Collection[T]{localCollection},
		opts.WithName("Global"+name)...,
	)
	cache := NewCollectionCacheByClusterFromMetadata[T]()
	clustersCollection.Register(func(e krt.Event[*Cluster]) {
		if e.Event != controllers.EventDelete {
			// The krt transformation functions will take care of adds and updates...
			return
		}

		// Remove any existing collections in the cache for this cluster
		old := ptr.Flatten(e.Old)
		if !cache.Remove(old.ID) {
			log.Debugf("clusterID %s doesn't exist in cache %v. Removal is a no-op", old.ID, cache)
		}
	})
	remoteCollections := krt.NewCollection(clustersCollection, func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[T] {
		// Do this after the fetches just to ensure we stay subscribed
		if existing := cache.Get(c.ID); existing != nil {
			return ptr.Of(existing)
		}
		remoteCollection := clusterToCollection(ctx, c)
		if remoteCollection == nil {
			log.Warnf("no collection for %s returned for cluster %v", name, c.ID)
		} else {
			if !cache.Insert(*remoteCollection) {
				log.Warnf("Failed to insert collection %v into cache for cluster %s due to existing collection", remoteCollection, c.ID)
				ctx.DiscardResult()
				return nil
			}
		}

		return remoteCollection
	}, opts.WithName("Remote"+name)...)

	remoteCollections.RegisterBatch(func(o []krt.Event[krt.Collection[T]]) {
		for _, e := range o {
			l := e.Latest()
			switch e.Event {
			case controllers.EventAdd, controllers.EventUpdate:
				globalCollection.UpdateObject(l)
			case controllers.EventDelete:
				globalCollection.DeleteObject(krt.GetKey(l))
			}
		}
	}, true)
	return globalCollection
}

func informerIndexByCluster[T controllers.ComparableObject](
	informerCollection krt.Collection[krt.Collection[T]],
) krt.Index[cluster.ID, krt.Collection[T]] {
	return krt.NewIndex[cluster.ID, krt.Collection[T]](informerCollection, "cluster", func(col krt.Collection[T]) []cluster.ID {
		val, ok := col.Metadata()[ClusterKRTMetadataKey]
		if !ok {
			log.Warnf("Cluster metadata not set on informer %v", col)
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
	return krt.NewIndex[cluster.ID, krt.Collection[T]](collection, "cluster", func(col krt.Collection[T]) []cluster.ID {
		val, ok := col.Metadata()[multicluster.ClusterKRTMetadataKey]
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

func mergeServiceInfosWithCluster(
	localClusterID cluster.ID,
) func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
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
			// This flat merge is ok because the VIPs themselves are per-network and we require
			// VIP uniquness within a network
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

func mergeWorkloadInfosWithCluster(
	localClusterID cluster.ID,
) func(workloadInfos []config.ObjectWithCluster[model.WorkloadInfo]) *config.ObjectWithCluster[model.WorkloadInfo] {
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
