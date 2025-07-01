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

	"google.golang.org/protobuf/types/known/wrapperspb"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/statusqueue"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
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
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) buildGlobalCollections(
	localCluster *multicluster.Cluster,
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
	clusters := a.buildRemoteClustersCollection(
		options,
		opts,
		configOverrides...,
	)

	a.remoteClusters = clusters

	LocalPods := localCluster.Pods()
	LocalServices := localCluster.Services()
	LocalNamespaces := localCluster.Namespaces()
	LocalNodes := localCluster.Nodes()
	LocalGateways := localCluster.Gateways()
	LocalWaypoints := a.WaypointsCollection(options.ClusterID, LocalGateways, localGatewayClasses, LocalPods, opts)

	LocalMeshConfig := options.MeshConfig
	// These first collections can't be merged since the Kubernetes APIs don't have enough room
	// for e.g. duplicate IPs, etc. So we keep around collections of collections and indexes per cluster.
	GlobalServices := nestedCollectionFromLocalAndRemote(
		LocalServices,
		clusters,
		func(_ krt.HandlerContext, c *multicluster.Cluster) *krt.Collection[*v1.Service] {
			return ptr.Of(c.Services())
		},
		"Services",
		opts,
	)
	serviceInformersByCluster := informerIndexByCluster(GlobalServices)

	LocalGatewaysWithCluster := krt.MapCollection(LocalGateways, func(obj *v1beta1.Gateway) config.ObjectWithCluster[*v1beta1.Gateway] {
		return config.ObjectWithCluster[*v1beta1.Gateway]{
			ClusterID: localCluster.ID,
			Object:    &obj,
		}
	}, opts.WithName("LocalGatewaysWithCluster")...)
	GlobalGatewaysWithCluster := nestedCollectionFromLocalAndRemote(
		LocalGatewaysWithCluster,
		clusters,
		func(ctx krt.HandlerContext, c *multicluster.Cluster) *krt.Collection[config.ObjectWithCluster[*v1beta1.Gateway]] {
			if !kube.WaitForCacheSync(fmt.Sprintf("ambient/informer/gateways[%s]", c.ID), a.stop, c.Gateways().HasSynced) {
				log.Warnf("Failed to sync gateways informer for cluster %s", c.ID)
				return nil
			}
			return ptr.Of(krt.MapCollection(c.Gateways(), func(obj *v1beta1.Gateway) config.ObjectWithCluster[*v1beta1.Gateway] {
				return config.ObjectWithCluster[*v1beta1.Gateway]{
					ClusterID: c.ID,
					Object:    &obj,
				}
			}, opts.WithName(fmt.Sprintf("GatewaysWithCluster[%s]", c.ID))...))
		}, "GatewaysWithCluster", opts)

	globalGatewaysByCluster := nestedCollectionIndexByCluster(GlobalGatewaysWithCluster)

	GlobalNamespaces := nestedCollectionFromLocalAndRemote(
		LocalNamespaces,
		clusters,
		func(_ krt.HandlerContext, c *multicluster.Cluster) *krt.Collection[*v1.Namespace] {
			return ptr.Of(c.Namespaces())
		},
		"Namespaces",
		opts,
	)
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
		func(ctx krt.HandlerContext, c *multicluster.Cluster) *krt.Collection[config.ObjectWithCluster[*v1.Node]] {
			if !kube.WaitForCacheSync(fmt.Sprintf("ambient/informer/nodes[%s]", c.ID), a.stop, c.Nodes().HasSynced) {
				log.Warnf("Failed to sync nodes informer for cluster %s", c.ID)
				return nil
			}
			return ptr.Of(krt.MapCollection(c.Nodes(), func(obj *v1.Node) config.ObjectWithCluster[*v1.Node] {
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
		GlobalNetworks,
		opts,
	)

	WaypointsByCluster := nestedCollectionIndexByCluster(GlobalWaypoints)
	// AllPolicies includes peer-authentication converted policies
	AuthorizationPolicies, AllPolicies := a.buildAndRegisterPolicyCollections(
		localAuthzPolicies,
		localPeerAuths,
		LocalWaypoints,
		opts,
	)

	LocalWorkloadServices := a.ServicesCollection(
		localCluster.ID,
		localCluster.Services(),
		localServiceEntries,
		LocalWaypoints,
		LocalNamespaces,
		LocalMeshConfig,
		opts,
	)
	// All of this is local only, but we need to do it here so we don't have to rebuild collections in ambientindex
	if features.EnableAmbientStatus {
		serviceEntriesWriter := kclient.NewWriteClient[*networkingclient.ServiceEntry](options.Client)
		servicesWriter := kclient.NewWriteClient[*v1.Service](options.Client)
		authorizationPoliciesWriter := kclient.NewWriteClient[*securityclient.AuthorizationPolicy](options.Client)

		WaypointPolicyStatus := WaypointPolicyStatusCollection(
			localAuthzPolicies,
			LocalWaypoints,
			localCluster.Services(),
			localServiceEntries,
			localGatewayClasses,
			LocalMeshConfig,
			localCluster.Namespaces(),
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
		LocalMeshConfig,
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

	workloadNetworkServiceIndex := krt.NewIndex[string, model.WorkloadInfo](GlobalWorkloads, "network;service", func(o model.WorkloadInfo) []string {
		res := make([]string, 0, len(o.Workload.Services))
		for svc := range o.Workload.Services {
			res = append(res, strings.Join([]string{o.Workload.Network, svc}, ";"))
		}
		return res
	})

	coalescedWorkloads := krt.NewManyCollection(
		workloadNetworkServiceIndex.AsCollection(),
		func(ctx krt.HandlerContext, i krt.IndexObject[string, model.WorkloadInfo]) []model.WorkloadInfo {
			parts := strings.Split(i.Key, ";")
			if len(parts) != 2 {
				log.Errorf("Invalid key %s for SplitHorizonWorkloads, expected <network>;<service>", i.Key)
				return nil
			}
			networkID := network.ID(parts[0])
			localNetwork := ptr.OrEmpty(krt.FetchOne(ctx, a.networks.LocalSystemNamespace.AsCollection()))
			if networkID.String() == localNetwork {
				// We don't coalesce workloads for the local network
				return nil
			}

			svcName := parts[1]
			svc := krt.FetchOne(ctx, a.services.Collection, krt.FilterKey(svcName))
			if svc == nil {
				log.Errorf("Failed to find service %s to coalesce workloads", svcName)
				return nil
			}
			if svc.Scope != model.Global {
				return nil
			}

			capacity := uint32(0)
			for _, wl := range i.Objects {
				if wl.Workload.GetCapacity().GetValue() == 0 {
					capacity++
				} else {
					capacity += wl.Workload.Capacity.GetValue()
				}
			}
			gws := LookupNetworkGateway(ctx, networkID, a.networks.NetworkGateways, a.networks.GatewaysByNetwork)
			meshCfg := krt.FetchOne(ctx, a.meshConfig.AsCollection())
			if meshCfg == nil {
				log.Errorf("Failed to find mesh config for network %s", networkID)
				return nil
			}
			if len(gws) == 0 {
				log.Warnf("No network gateway found for network %s", networkID)
				return nil
			}
			if len(gws) > 1 {
				log.Warnf("Multiple gateways found for network %s, using the first one", networkID)
			}
			gw := gws[0]
			wi := a.createSplitHorizonWorkload(svcName, svc.Service, &gw, capacity, meshCfg)
			wi = ptr.Of(precomputeWorkload(*wi))
			return []model.WorkloadInfo{*wi}
		}, opts.WithName("remoteCoalesedWorkloads")...,
	)

	networkLocalWorkloads := krt.NewCollection(GlobalWorkloads, func(ctx krt.HandlerContext, i model.WorkloadInfo) *model.WorkloadInfo {
		localNetwork := ptr.OrEmpty(krt.FetchOne(ctx, a.networks.LocalSystemNamespace.AsCollection()))
		if i.Workload.Network == localNetwork {
			return &i
		}
		return nil
	}, opts.WithName("NetworkLocalWorkloads")...)

	remoteGwWorkloads := krt.NewCollection(
		a.networks.NetworkGateways,
		func(ctx krt.HandlerContext, g NetworkGateway) *model.WorkloadInfo {
			localNetwork := ptr.OrEmpty(krt.FetchOne(ctx, a.networks.LocalSystemNamespace.AsCollection()))
			if g.Network.String() == localNetwork {
				// We don't want to create split horizon workloads for the local network
				return nil
			}
			meshCfg := krt.FetchOne(ctx, a.meshConfig.AsCollection())
			if meshCfg == nil {
				log.Errorf("Failed to find mesh config for remote gateway %s", g.ResourceName())
				return nil
			}
			wi, err := a.createNetworkGatewayWorkload(g, meshCfg)
			if err != nil {
				log.Errorf("Failed to create network gateway workload for cluster %s: %v", g.Cluster, err)
				return nil
			}
			return ptr.Of(precomputeWorkload(*wi))
		}, opts.WithName("RemoteGatewayWorkloads")...)

	SplitHorizonWorkloads := krt.JoinCollection(
		[]krt.Collection[model.WorkloadInfo]{
			coalescedWorkloads,
			networkLocalWorkloads,
			remoteGwWorkloads,
		}, opts.WithName("SplitHorizonWorkloads")...,
	)
	SplitHorizonWorkloads.RegisterBatch(krt.BatchedEventFilter(
		func(a model.WorkloadInfo) *workloadapi.Workload {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Workload
		},
		PushXdsAddress(a.XDSUpdater, model.WorkloadInfo.ResourceName),
	), false)
	SplitHorizonWorkloadServiceIndex := krt.NewIndex[string, model.WorkloadInfo](
		SplitHorizonWorkloads, "service",
		func(o model.WorkloadInfo) []string {
			return maps.Keys(o.Workload.Services)
		},
	)

	// This is a first pass at creating the collection of services for local workloads.
	// Here, we only include services that have a remote workload.
	// Later, we join with all services, giving this ones here priority.
	SplitHorizonServices := krt.NewCollection(
		WorkloadServiceIndex.AsCollection(),
		func(ctx krt.HandlerContext, i krt.IndexObject[string, model.WorkloadInfo]) *model.ServiceInfo {
			localNetwork := ptr.OrEmpty(krt.FetchOne(ctx, a.networks.LocalSystemNamespace.AsCollection()))
			svc := krt.FetchOne(ctx, a.services.Collection, krt.FilterKey(i.Key))
			if svc == nil {
				log.Errorf("Failed to find service %s for SplitHorizonServices", i.Key)
				return nil
			}
			if svc.Scope != model.Global {
				return nil
			}
			meshCfg := krt.FetchOne(ctx, a.meshConfig.AsCollection())
			if meshCfg == nil {
				log.Errorf("Failed to find mesh config")
				return nil
			}
			sans := sets.String{}
			for _, wl := range i.Objects {
				if wl.Workload.Network == localNetwork {
					continue
				}
				sans.Insert(spiffe.MustGenSpiffeURI(meshCfg.MeshConfig, wl.Workload.Namespace, wl.Workload.ServiceAccount))
			}
			if sans.IsEmpty() {
				// Ignore workloads without remote workloads
				return nil
			}
			sans = sans.Union(sets.New(svc.Service.SubjectAltNames...))

			newSvc := &model.ServiceInfo{
				Service: protomarshal.Clone(svc.Service),
				Scope:   svc.Scope,
			}
			newSvc.Service.SubjectAltNames = sans.UnsortedList()
			return precomputeServicePtr(newSvc)
		}, opts.WithName("SplitHorizonServicesWithWorkloads")...)

	// Give the SplitHorizonServicesWithWorkloads priority as they also have the sans set.
	SplitHorizonServices = krt.JoinCollection(
		[]krt.Collection[model.ServiceInfo]{
			SplitHorizonServices,
			GlobalMergedWorkloadServices,
		},
		opts.WithName("SplitHorizonServices")...)

	SplitHorizonServices.RegisterBatch(krt.BatchedEventFilter(
		func(a model.ServiceInfo) *workloadapi.Service {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Service
		},
		PushXdsAddress(a.XDSUpdater, model.ServiceInfo.ResourceName),
	), false)
	SplitHorizonServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](SplitHorizonServices, "serviceAddress", networkAddressFromService)

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

	a.splitHorizon = splitHorizon{
		workloads:            SplitHorizonWorkloads,
		workloadByServiceKey: SplitHorizonWorkloadServiceIndex,
		services:             SplitHorizonServices,
		servicesByAddress:    SplitHorizonServiceAddressIndex,
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
	clustersCollection krt.Collection[*multicluster.Cluster],
	clusterToCollection krt.TransformationSingle[*multicluster.Cluster, krt.Collection[T]],
	name string,
	opts krt.OptionsBuilder,
) krt.Collection[krt.Collection[T]] {
	globalCollection := krt.NewStaticCollection(
		localCollection,
		[]krt.Collection[T]{localCollection},
		opts.WithName("Global"+name)...,
	)
	cache := newCollectionCacheByClusterFromMetadata[T]()
	clustersCollection.Register(func(e krt.Event[*multicluster.Cluster]) {
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
	remoteCollections := krt.NewCollection(clustersCollection, func(ctx krt.HandlerContext, c *multicluster.Cluster) *krt.Collection[T] {
		// Do this after the fetches just to ensure we stay subscribed
		if existing := cache.Get(c.ID); existing != nil {
			return ptr.Of(existing)
		}
		remoteCollection := clusterToCollection(ctx, c)
		if remoteCollection == nil {
			log.Warnf("no collection for %s returned for cluster %v", name, c.ID)
		} else if !cache.Insert(*remoteCollection) {
			log.Warnf("Failed to insert collection %v into cache for cluster %s due to existing collection", remoteCollection, c.ID)
			return nil
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
		val, ok := col.Metadata()[multicluster.ClusterKRTMetadataKey]
		if !ok {
			panic(fmt.Sprintf("Cluster metadata not set on informer %v", col))
		}
		id, ok := val.(cluster.ID)
		if !ok {
			panic(fmt.Sprintf("Invalid cluster metadata set on collection %v: %v", col, val))
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
			panic(fmt.Sprintf("Cluster metadata not set on collection %v", col))
		}
		id, ok := val.(cluster.ID)
		if !ok {
			panic(fmt.Sprintf("Invalid cluster metadata set on collection %v: %v", col, val))
		}
		return []cluster.ID{id}
	})
}

type simplePort struct {
	servicePort uint32
	targetPort  uint32
}
type portKey struct {
	source    string
	clusterID cluster.ID
}

type simpleNetworkAddress struct {
	network string
	ip      netip.Addr
}

func mergeServiceInfosWithCluster(
	localClusterID cluster.ID,
) func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
	return func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
		svcInfosLen := len(serviceInfos)
		if svcInfosLen == 0 {
			return nil
		}
		if svcInfosLen == 1 {
			return &serviceInfos[0]
		}

		vips := sets.NewWithLength[simpleNetworkAddress](svcInfosLen)
		sans := sets.NewWithLength[string](svcInfosLen)
		ports := map[portKey]sets.Set[simplePort]{}
		var base config.ObjectWithCluster[model.ServiceInfo]
		setBase := false
		workloadPortsToSimplePort := func(p *workloadapi.Port) simplePort {
			return simplePort{
				servicePort: p.ServicePort,
				targetPort:  p.TargetPort,
			}
		}
		for _, obj := range serviceInfos {
			if obj.Object == nil {
				continue
			}

			ports[portKey{
				source:    obj.Object.Source.String(),
				clusterID: obj.ClusterID,
			}] = sets.New(slices.Map(obj.Object.Service.Ports, workloadPortsToSimplePort)...)
			// This flat merge is ok because the VIPs themselves are per-network and we require
			// VIP uniquness within a network
			vips.InsertAll(slices.Map(obj.Object.Service.GetAddresses(), func(a *workloadapi.NetworkAddress) simpleNetworkAddress {
				// We can ignore the err because we know the address is valid
				addr, _ := netip.AddrFromSlice(a.Address)
				return simpleNetworkAddress{
					network: a.Network,
					ip:      addr,
				}
			})...)
			sans.InsertAll(obj.Object.Service.GetSubjectAltNames()...)
			if obj.ClusterID == localClusterID {
				if obj.Object.Source.Kind == kind.ServiceEntry {
					// If there's a service entry that's considered external to the mesh, we
					// prioritize that
					base = obj
				} else if !setBase {
					base = obj
					setBase = true
				}
			}
		}

		if !setBase {
			// No local objects found, so just use the first one
			base = serviceInfos[0]
		}

		basePorts := sets.New(slices.Map(base.Object.Service.Ports, workloadPortsToSimplePort)...)
		for source, portSet := range ports {
			if !basePorts.Equals(portSet) {
				log.Warnf("ServiceInfo derived from %s has mismatched ports. Base ports %v != %v", source, basePorts, portSet)
			}
		}

		// Prevent modifying the underlying workloadapi.Service
		base.Object.Service = protomarshal.Clone(base.Object.Service)

		// TODO: Do we need to merge anything else?

		// VIP order needs to be stable for comparison purposes.
		orderedVips := slices.SortBy(vips.UnsortedList(), func(a simpleNetworkAddress) string {
			return a.network + "/" + a.ip.String()
		})
		base.Object.Service.Addresses = slices.Map(orderedVips, func(a simpleNetworkAddress) *workloadapi.NetworkAddress {
			return &workloadapi.NetworkAddress{
				Network: a.network,
				Address: a.ip.AsSlice(),
			}
		})
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

		// TODO: We should adjust the remote cluster store logic to seamlessly swap out clusters without there
		// being duplicates in our collections. Tracked by https://github.com/istio/istio/issues/49349
		log.Warnf("Duplicate workloadinfos found for %#+v. Trying to take local and then the first if we can't find it", workloadInfos)
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

func (a *index) createNetworkGatewayWorkload(networkGateway NetworkGateway, meshCfg *MeshConfig,
) (*model.WorkloadInfo, error) {
	address, err := netip.ParseAddr(networkGateway.Addr)
	if err != nil {
		return nil, err
	}
	wl := workloadapi.Workload{
		Uid:            networkGateway.ResourceName(),
		Name:           networkGateway.ResourceName(),
		Namespace:      networkGateway.Source.Namespace,
		Network:        networkGateway.Network.String(),
		TunnelProtocol: workloadapi.TunnelProtocol_HBONE,
		TrustDomain:    pickTrustDomain(meshCfg),
		ServiceAccount: networkGateway.ServiceAccount.Name,
		Capacity:       &wrapperspb.UInt32Value{Value: 1},
		WorkloadType:   workloadapi.WorkloadType_DEPLOYMENT, // TODO(stevenjin8): What is the correct type here?
		Addresses:      [][]byte{address.AsSlice()},
		ClusterId:      networkGateway.Cluster.String(),
	}
	return &model.WorkloadInfo{
		Workload: &wl,
		Source:   kind.KubernetesGateway,
		Labels:   labelutil.AugmentLabels(nil, networkGateway.Cluster, "", "", networkGateway.Network),
	}, nil
}

func (a *index) createSplitHorizonWorkload(
	svcNamespacedName string, svc *workloadapi.Service, networkGateway *NetworkGateway, capacity uint32, meshCfg *MeshConfig,
) *model.WorkloadInfo {
	hboneMtlsPort := networkGateway.HBONEPort
	if hboneMtlsPort == 0 {
		hboneMtlsPort = 15008
	}
	uid := generateSplitHorizonWorkloadUID(networkGateway.Network.String(), networkGateway.ResourceName(), svcNamespacedName)
	wl := workloadapi.Workload{
		Uid:            uid,
		Name:           uid, // TODO(stevenjin8): better name?
		Namespace:      svc.Namespace,
		Network:        networkGateway.Network.String(),
		TrustDomain:    pickTrustDomain(meshCfg),
		Capacity:       &wrapperspb.UInt32Value{Value: capacity},
		WorkloadType:   workloadapi.WorkloadType_DEPLOYMENT, // TODO(stevenjin8): What is the correct type here?
		TunnelProtocol: workloadapi.TunnelProtocol_HBONE,
		NetworkGateway: &workloadapi.GatewayAddress{
			Destination:   &workloadapi.GatewayAddress_Address{},
			HboneMtlsPort: hboneMtlsPort,
		},
		ClusterId: networkGateway.Cluster.String(),

		Services: map[string]*workloadapi.PortList{
			svcNamespacedName: {
				Ports: svc.Ports,
			},
		},
	}
	address, err := netip.ParseAddr(networkGateway.Addr)
	if err != nil {
		// Assume address is a hostname
		wl.NetworkGateway.Destination = &workloadapi.GatewayAddress_Hostname{
			Hostname: &workloadapi.NamespacedHostname{
				Namespace: networkGateway.Source.Namespace,
				Hostname:  networkGateway.Addr,
			},
		}
	} else {
		wl.NetworkGateway.Destination = &workloadapi.GatewayAddress_Address{
			Address: &workloadapi.NetworkAddress{
				Network: networkGateway.Network.String(),
				Address: address.AsSlice(),
			},
		}
	}

	wi := &model.WorkloadInfo{
		Workload: &wl,
		Source:   kind.KubernetesGateway,
		Labels:   labelutil.AugmentLabels(nil, networkGateway.Cluster, "", "", networkGateway.Network),
	}
	return wi
}
