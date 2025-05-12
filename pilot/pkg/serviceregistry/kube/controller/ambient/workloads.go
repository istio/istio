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

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

// WorkloadsCollection builds out the core Workload object type used in ambient mode.
// A Workload represents a single addressable unit of compute -- typically a Pod or a VM.
// Workloads can come from a variety of sources; these are joined together to build one complete `Collection[WorkloadInfo]`.
func (a *index) WorkloadsCollection(
	pods krt.Collection[*v1.Pod],
	nodes krt.Collection[Node],
	meshConfig krt.Singleton[MeshConfig],
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	waypoints krt.Collection[Waypoint],
	workloadServices krt.Collection[model.ServiceInfo],
	workloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	endpointSlices krt.Collection[*discovery.EndpointSlice],
	namespaces krt.Collection[*v1.Namespace],
	opts krt.OptionsBuilder,
) krt.Collection[model.WorkloadInfo] {
	WorkloadServicesNamespaceIndex := krt.NewNamespaceIndex(workloadServices)
	EndpointSlicesByIPIndex := endpointSliceAddressIndex(endpointSlices)
	// Workloads coming from pods. There should be one workload for each (running) Pod.
	PodWorkloads := krt.NewCollection(
		pods,
		a.podWorkloadBuilder(
			meshConfig,
			authorizationPolicies,
			peerAuths,
			waypoints,
			workloadServices,
			WorkloadServicesNamespaceIndex,
			endpointSlices,
			EndpointSlicesByIPIndex,
			namespaces,
			nodes,
		),
		opts.WithName("PodWorkloads")...,
	)
	// Workloads coming from workloadEntries. These are 1:1 with WorkloadEntry.
	WorkloadEntryWorkloads := krt.NewCollection(
		workloadEntries,
		a.workloadEntryWorkloadBuilder(meshConfig, authorizationPolicies, peerAuths, waypoints, workloadServices, WorkloadServicesNamespaceIndex, namespaces),
		opts.WithName("WorkloadEntryWorkloads")...,
	)
	// Workloads coming from serviceEntries. These are inlined workloadEntries (under `spec.endpoints`); these serviceEntries will
	// also be generating `workloadapi.Service` definitions in the `ServicesCollection` logic.
	ServiceEntryWorkloads := krt.NewManyCollection(
		serviceEntries,
		a.serviceEntryWorkloadBuilder(meshConfig, authorizationPolicies, peerAuths, waypoints, namespaces),
		opts.WithName("ServiceEntryWorkloads")...,
	)
	// Workloads coming from endpointSlices. These are for *manually added* endpoints. Typically, Kubernetes will insert each pod
	// into the EndpointSlice. This is because Kubernetes has 3 APIs in its model: Service, Pod, and EndpointSlice.
	// In our API, we only have two: Service and Workload.
	// Pod provides much more information than EndpointSlice, so typically we just consume that directly; see method for more details
	// on when we will build from an EndpointSlice.
	EndpointSliceWorkloads := krt.NewManyCollection(
		endpointSlices,
		a.endpointSlicesBuilder(meshConfig, workloadServices),
		opts.WithName("EndpointSliceWorkloads")...)

	NetworkGatewayWorkloads := krt.NewManyFromNothing[model.WorkloadInfo](func(ctx krt.HandlerContext) []model.WorkloadInfo {
		return slices.Map(a.LookupAllNetworkGateway(ctx), convertGateway)
	}, opts.WithName("NetworkGatewayWorkloads")...)

	Workloads := krt.JoinCollection(
		[]krt.Collection[model.WorkloadInfo]{
			PodWorkloads,
			WorkloadEntryWorkloads,
			ServiceEntryWorkloads,
			EndpointSliceWorkloads,
			NetworkGatewayWorkloads,
		},
		// Each collection has its own unique UID as the key. This guarantees an object can exist in only a single collection
		// This enables us to use the JoinUnchecked optimization.
		append(opts.WithName("Workloads"), krt.WithJoinUnchecked())...)
	return Workloads
}

func MergedGlobalWorkloadsCollection(
	localCluster *multicluster.Cluster,
	localWaypoints krt.Collection[Waypoint],
	localNodeLocalities krt.Collection[Node],
	clusters krt.Collection[*multicluster.Cluster],
	workloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	globalPods krt.Collection[krt.Collection[*v1.Pod]],
	podsByCluster krt.Index[cluster.ID, krt.Collection[*v1.Pod]],
	globalNodes krt.Collection[krt.Collection[config.ObjectWithCluster[Node]]],
	nodesByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[Node]]],
	meshConfig krt.Singleton[MeshConfig],
	localAuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	localPeerAuths krt.Collection[*securityclient.PeerAuthentication],
	globalWaypoints krt.Collection[krt.Collection[Waypoint]],
	waypointsByCluster krt.Index[cluster.ID, krt.Collection[Waypoint]],
	localWorkloadServices krt.Collection[model.ServiceInfo],
	globalWorkloadServices krt.Collection[krt.Collection[config.ObjectWithCluster[model.ServiceInfo]]],
	globalWorkloadServicesByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[model.ServiceInfo]]],
	globalEndpointSlices krt.Collection[krt.Collection[*discovery.EndpointSlice]],
	endpointSlicesByCluster krt.Index[cluster.ID, krt.Collection[*discovery.EndpointSlice]],
	globalNamespaces krt.Collection[krt.Collection[*v1.Namespace]],
	namespacesByCluster krt.Index[cluster.ID, krt.Collection[*v1.Namespace]],
	globalNetworks networkCollections,
	localClusterID cluster.ID,
	flags FeatureFlags,
	domainSuffix string,
	opts krt.OptionsBuilder,
) krt.Collection[model.WorkloadInfo] {
	// More setup to do here so we can't use nestedCollectionFromLocalAndRemote
	LocalWorkloadServicesNamespaceIndex := krt.NewNamespaceIndex(localWorkloadServices)
	LocalEndpointSlicesByIPIndex := endpointSliceAddressIndex(localCluster.EndpointSlices())
	LocalPodWorkloads := krt.NewCollection(
		localCluster.Pods(),
		podWorkloadBuilder(
			meshConfig,
			localAuthorizationPolicies,
			localPeerAuths,
			localWaypoints,
			localWorkloadServices,
			LocalWorkloadServicesNamespaceIndex,
			localCluster.EndpointSlices(),
			LocalEndpointSlicesByIPIndex,
			localCluster.Namespaces(),
			localNodeLocalities,
			domainSuffix,
			func(_ krt.HandlerContext) cluster.ID {
				return localCluster.ID
			},
			func(hc krt.HandlerContext) network.ID {
				nw := ptr.OrEmpty(krt.FetchOne(hc, globalNetworks.LocalSystemNamespace.AsCollection()))
				return network.ID(nw)
			},
			globalNetworks.NetworkGateways,
			globalNetworks.GatewaysByNetwork,
			flags,
		),
		opts.WithName("LocalPodWorkloads")...,
	)
	LocalPodWorkloadsWithCluster := krt.MapCollection(
		LocalPodWorkloads,
		wrapObjectWithCluster[model.WorkloadInfo](localCluster.ID),
		opts.WithName("LocalPodWorkloadsWithCluster")...,
	)
	LocalWorkloadEntryWorkloads := krt.NewCollection(
		workloadEntries,
		workloadEntryWorkloadBuilder(
			meshConfig,
			localAuthorizationPolicies,
			localPeerAuths,
			localWaypoints,
			localWorkloadServices,
			LocalWorkloadServicesNamespaceIndex,
			localCluster.Namespaces(),
			func(_ krt.HandlerContext) cluster.ID {
				return localCluster.ID
			},
			func(hc krt.HandlerContext) network.ID {
				nw := ptr.OrEmpty(krt.FetchOne(hc, globalNetworks.LocalSystemNamespace.AsCollection()))
				return network.ID(nw)
			},
			globalNetworks.NetworkGateways,
			globalNetworks.GatewaysByNetwork,
			flags,
		),
		opts.WithName("LocalWorkloadEntryWorkloads")...,
	)
	LocalWorkloadEntryWorkloadsWithCluster := krt.MapCollection(
		LocalWorkloadEntryWorkloads,
		wrapObjectWithCluster[model.WorkloadInfo](localCluster.ID),
		opts.WithName("LocalWorkloadEntryWorkloadsWithCluster")...,
	)
	// Workloads coming from serviceEntries. These are inlined workloadEntries (under `spec.endpoints`); these serviceEntries will
	// also be generating `workloadapi.Service` definitions in the `ServicesCollection` logic.
	LocalServiceEntryWorkloads := krt.NewManyCollection(
		serviceEntries,
		serviceEntryWorkloadBuilder(
			meshConfig,
			localAuthorizationPolicies,
			localPeerAuths,
			localWaypoints,
			localCluster.Namespaces(),
			func(hc krt.HandlerContext) cluster.ID {
				return localCluster.ID
			},
			func(hc krt.HandlerContext) network.ID {
				nw := ptr.OrEmpty(krt.FetchOne(hc, globalNetworks.LocalSystemNamespace.AsCollection()))
				return network.ID(nw)
			},
			globalNetworks.NetworkGateways,
			globalNetworks.GatewaysByNetwork,
			flags,
		),
		opts.WithName("LocalServiceEntryWorkloads")...,
	)
	LocalServiceEntryWorkloadsWithCluster := krt.MapCollection(
		LocalServiceEntryWorkloads,
		wrapObjectWithCluster[model.WorkloadInfo](localCluster.ID),
		opts.WithName("LocalServiceEntryWorkloadsWithCluster")...,
	)
	// Workloads coming from endpointSlices. These are for *manually added* endpoints. Typically, Kubernetes will insert each pod
	// into the EndpointSlice. This is because Kubernetes has 3 APIs in its model: Service, Pod, and EndpointSlice.
	// In our API, we only have two: Service and Workload.
	// Pod provides much more information than EndpointSlice, so typically we just consume that directly; see method for more details
	// on when we will build from an EndpointSlice.
	LocalEndpointSliceWorkloads := krt.NewManyCollection(
		localCluster.EndpointSlices(),
		endpointSlicesBuilder(meshConfig,
			localWorkloadServices,
			domainSuffix,
			func(hc krt.HandlerContext) cluster.ID {
				return localCluster.ID
			},
			func(hc krt.HandlerContext) network.ID {
				nw := ptr.OrEmpty(krt.FetchOne(hc, globalNetworks.LocalSystemNamespace.AsCollection()))
				return network.ID(nw)
			},
		),
		opts.WithName("LocalEndpointSliceWorkloads")...,
	)
	LocalEndpointSliceWorkloadsWithCluster := krt.MapCollection(
		LocalEndpointSliceWorkloads,
		wrapObjectWithCluster[model.WorkloadInfo](localCluster.ID),
		opts.WithName("LocalEndpointSliceWorkloadsWithCluster")...,
	)

	LocalNetworkGatewayWorkloads := krt.NewManyFromNothing[model.WorkloadInfo](func(ctx krt.HandlerContext) []model.WorkloadInfo {
		return slices.Map(LookupAllNetworkGateway(
			ctx,
			globalNetworks.NetworkGateways,
		), convertGateway)
	}, opts.WithName("LocalNetworkGatewayWorkloads")...)
	LocalNetworkGatewayWorkloadsWithCluster := krt.MapCollection(
		LocalNetworkGatewayWorkloads,
		wrapObjectWithCluster[model.WorkloadInfo](localCluster.ID),
		opts.WithName("LocalNetworkGatewayWorkloadsWithCluster")...,
	)
	GlobalWorkloadInfosWithCluster := krt.NewStaticCollection(
		localCluster,
		[]krt.Collection[config.ObjectWithCluster[model.WorkloadInfo]]{
			LocalPodWorkloadsWithCluster,
			LocalWorkloadEntryWorkloadsWithCluster,
			LocalServiceEntryWorkloadsWithCluster,
			LocalEndpointSliceWorkloadsWithCluster,
			LocalNetworkGatewayWorkloadsWithCluster,
		},
		opts.WithName("GlobalWorkloadsInfoWithCluster")...,
	)

	podWorkloadsCache := newCollectionCacheByClusterFromMetadata[config.ObjectWithCluster[model.WorkloadInfo]]()
	workloadEntryWorkloadsCache := newCollectionCacheByClusterFromMetadata[config.ObjectWithCluster[model.WorkloadInfo]]()
	serviceEntryWorkloadsCache := newCollectionCacheByClusterFromMetadata[config.ObjectWithCluster[model.WorkloadInfo]]()
	endpointSliceWorkloadsCache := newCollectionCacheByClusterFromMetadata[config.ObjectWithCluster[model.WorkloadInfo]]()
	networkGatewayWorkloadsCache := newCollectionCacheByClusterFromMetadata[config.ObjectWithCluster[model.WorkloadInfo]]()

	clusters.Register(func(e krt.Event[*multicluster.Cluster]) {
		if e.Event != controllers.EventDelete {
			// The krt transformation functions will take care of adds and updates...
			return
		}

		old := ptr.Flatten(e.Old)
		// Remove any existing collections in the caches for this cluster
		if !podWorkloadsCache.Remove(old.ID) {
			log.Debugf("clusterID %s doesn't exist in cache %v. Removal is a no-op", old.ID, podWorkloadsCache)
		}
		if !workloadEntryWorkloadsCache.Remove(old.ID) {
			log.Debugf("clusterID %s doesn't exist in cache %v. Removal is a no-op", old.ID, workloadEntryWorkloadsCache)
		}
		if !serviceEntryWorkloadsCache.Remove(old.ID) {
			log.Debugf("clusterID %s doesn't exist in cache %v. Removal is a no-op", old.ID, serviceEntryWorkloadsCache)
		}
		if !endpointSliceWorkloadsCache.Remove(old.ID) {
			log.Debugf("clusterID %s doesn't exist in cache %v. Removal is a no-op", old.ID, endpointSliceWorkloadsCache)
		}
		if !networkGatewayWorkloadsCache.Remove(old.ID) {
			log.Debugf("clusterID %s doesn't exist in cache %v. Removal is a no-op", old.ID, networkGatewayWorkloadsCache)
		}
	})
	RemoteWorkloadInfosWithCluster := krt.NewManyCollection(
		clusters,
		func(ctx krt.HandlerContext, c *multicluster.Cluster) []krt.Collection[config.ObjectWithCluster[model.WorkloadInfo]] {
			endpointSlicesPtr := krt.FetchOne(ctx, globalEndpointSlices, krt.FilterIndex(endpointSlicesByCluster, c.ID))
			if endpointSlicesPtr == nil {
				log.Warnf("Cluster %s does not have endpoint slices, skipping global workloads", c.ID)
				return nil
			}
			endpointSlices := *endpointSlicesPtr
			podsPtr := krt.FetchOne(ctx, globalPods, krt.FilterIndex(podsByCluster, c.ID))
			if podsPtr == nil {
				log.Warnf("Cluster %s does not have pods, skipping global workloads", c.ID)
				return nil
			}
			pods := *podsPtr
			waypointsPtr := krt.FetchOne(ctx, globalWaypoints, krt.FilterIndex(waypointsByCluster, c.ID))
			if waypointsPtr == nil {
				log.Warnf("Cluster %s does not have waypoints, skipping global workloads", c.ID)
				return nil
			}
			waypoints := *waypointsPtr
			namespacesPtr := krt.FetchOne(ctx, globalNamespaces, krt.FilterIndex(namespacesByCluster, c.ID))
			if namespacesPtr == nil {
				log.Warnf("Cluster %s does not have namespaces, skipping global workloads", c.ID)
				return nil
			}
			namespaces := *namespacesPtr
			clusteredNodesPtr := krt.FetchOne(ctx, globalNodes, krt.FilterIndex(nodesByCluster, c.ID))
			if clusteredNodesPtr == nil {
				log.Warnf("Cluster %s does not have nodes, skipping global workloads", c.ID)
				return nil
			}
			clusteredNodes := *clusteredNodesPtr

			workloadServicesPtr := krt.FetchOne(ctx, globalWorkloadServices, krt.FilterIndex(globalWorkloadServicesByCluster, c.ID))
			if workloadServicesPtr == nil {
				log.Warnf("Cluster %s does not have workload services, skipping global workloads", c.ID)
				return nil
			}

			existing := []krt.Collection[config.ObjectWithCluster[model.WorkloadInfo]]{
				podWorkloadsCache.Get(c.ID),
				workloadEntryWorkloadsCache.Get(c.ID),
				serviceEntryWorkloadsCache.Get(c.ID),
				endpointSliceWorkloadsCache.Get(c.ID),
				networkGatewayWorkloadsCache.Get(c.ID),
			}

			if slices.Contains(existing, nil) {
				// At least of of these isn't initialized yet; remove everything for this cluster
				podWorkloadsCache.Remove(c.ID)
				workloadEntryWorkloadsCache.Remove(c.ID)
				serviceEntryWorkloadsCache.Remove(c.ID)
				endpointSliceWorkloadsCache.Remove(c.ID)
				networkGatewayWorkloadsCache.Remove(c.ID)
			} else {
				// We have all of the collections in the cache
				log.Info("Using cache for global workloads")
				return existing
			}

			// Now we create everything anew

			nodes := krt.MapCollection(clusteredNodes, unwrapObjectWithCluster, opts.WithName(fmt.Sprintf("NodeLocality[%s]", c.ID))...)

			globalWorkloadServicesWithCluster := *workloadServicesPtr
			globalWorkloadServices := krt.MapCollection(
				globalWorkloadServicesWithCluster,
				unwrapObjectWithCluster[model.ServiceInfo],
				append(
					opts.WithName(fmt.Sprintf("WorkloadServices[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)

			WorkloadServicesNamespaceIndex := krt.NewNamespaceIndex(globalWorkloadServices)
			EndpointSlicesByIPIndex := endpointSliceAddressIndex(endpointSlices)
			// Workloads coming from pods. There should be one workload for each (running) Pod.
			PodWorkloads := krt.NewCollection(
				pods,
				podWorkloadBuilder(
					meshConfig,
					localAuthorizationPolicies,
					localPeerAuths,
					waypoints,
					globalWorkloadServices,
					WorkloadServicesNamespaceIndex,
					endpointSlices,
					EndpointSlicesByIPIndex,
					namespaces,
					nodes,
					domainSuffix,
					func(_ krt.HandlerContext) cluster.ID {
						return c.ID
					},
					func(hc krt.HandlerContext) network.ID {
						nwPtr := krt.FetchOne(ctx, globalNetworks.RemoteSystemNamespaceNetworks, krt.FilterIndex(globalNetworks.SystemNamespaceNetworkByCluster, c.ID))
						if nwPtr == nil {
							log.Warnf("Cluster %s does not have a network, skipping global workloads", c.ID)
							hc.DiscardResult()
							return ""
						}
						nw := *nwPtr
						return network.ID(ptr.OrEmpty(nw.Get()))
					},
					globalNetworks.NetworkGateways,
					globalNetworks.GatewaysByNetwork,
					flags,
				),
				append(
					opts.WithName(fmt.Sprintf("PodWorkloads[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)
			PodWorkloadsWithCluster := krt.MapCollection(
				PodWorkloads,
				wrapObjectWithCluster[model.WorkloadInfo](c.ID),
				append(
					opts.WithName(fmt.Sprintf("PodWorkloadsWithCluster[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)
			// Workloads coming from workloadEntries. These are 1:1 with WorkloadEntry.
			WorkloadEntryWorkloads := krt.NewCollection(
				workloadEntries,
				workloadEntryWorkloadBuilder(
					meshConfig,
					localAuthorizationPolicies,
					localPeerAuths,
					waypoints,
					globalWorkloadServices,
					WorkloadServicesNamespaceIndex,
					namespaces,
					func(_ krt.HandlerContext) cluster.ID {
						return c.ID
					},
					func(hc krt.HandlerContext) network.ID {
						nwPtr := krt.FetchOne(ctx, globalNetworks.RemoteSystemNamespaceNetworks, krt.FilterIndex(globalNetworks.SystemNamespaceNetworkByCluster, c.ID))
						if nwPtr == nil {
							log.Warnf("Cluster %s does not have a network, skipping global workloads", c.ID)
							hc.DiscardResult()
							return ""
						}
						nw := *nwPtr
						return network.ID(*nw.Get())
					},
					globalNetworks.NetworkGateways,
					globalNetworks.GatewaysByNetwork,
					flags,
				),
				append(
					opts.WithName(fmt.Sprintf("WorkloadEntryWorkloads[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)
			WorkloadEntryWorkloadsWithCluster := krt.MapCollection(
				WorkloadEntryWorkloads,
				wrapObjectWithCluster[model.WorkloadInfo](c.ID),
				append(
					opts.WithName(fmt.Sprintf("WorkloadEntryWorkloadsWithCluster[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)
			// Workloads coming from serviceEntries. These are inlined workloadEntries (under `spec.endpoints`); these serviceEntries will
			// also be generating `workloadapi.Service` definitions in the `ServicesCollection` logic.
			ServiceEntryWorkloads := krt.NewManyCollection(
				serviceEntries,
				serviceEntryWorkloadBuilder(
					meshConfig,
					localAuthorizationPolicies,
					localPeerAuths,
					waypoints,
					namespaces,
					func(hc krt.HandlerContext) cluster.ID {
						return c.ID
					},
					func(hc krt.HandlerContext) network.ID {
						nwPtr := krt.FetchOne(ctx, globalNetworks.RemoteSystemNamespaceNetworks, krt.FilterIndex(globalNetworks.SystemNamespaceNetworkByCluster, c.ID))
						if nwPtr == nil {
							log.Warnf("Cluster %s does not have a network, skipping global workloads", c.ID)
							hc.DiscardResult()
							return ""
						}
						nw := *nwPtr
						return network.ID(*nw.Get())
					},
					globalNetworks.NetworkGateways,
					globalNetworks.GatewaysByNetwork,
					flags,
				),
				append(
					opts.WithName(fmt.Sprintf("ServiceEntryWorkloads[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)
			ServiceEntryWorkloadsWithCluster := krt.MapCollection(
				ServiceEntryWorkloads,
				wrapObjectWithCluster[model.WorkloadInfo](c.ID),
				append(
					opts.WithName(fmt.Sprintf("ServiceEntryWorkloadsWithCluster[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)
			// Workloads coming from endpointSlices. These are for *manually added* endpoints. Typically, Kubernetes will insert each pod
			// into the EndpointSlice. This is because Kubernetes has 3 APIs in its model: Service, Pod, and EndpointSlice.
			// In our API, we only have two: Service and Workload.
			// Pod provides much more information than EndpointSlice, so typically we just consume that directly; see method for more details
			// on when we will build from an EndpointSlice.
			EndpointSliceWorkloads := krt.NewManyCollection(
				endpointSlices,
				endpointSlicesBuilder(meshConfig,
					globalWorkloadServices,
					domainSuffix,
					func(hc krt.HandlerContext) cluster.ID {
						return c.ID
					},
					func(hc krt.HandlerContext) network.ID {
						nwPtr := krt.FetchOne(ctx, globalNetworks.RemoteSystemNamespaceNetworks, krt.FilterIndex(globalNetworks.SystemNamespaceNetworkByCluster, c.ID))
						if nwPtr == nil {
							log.Warnf("Cluster %s does not have a network, skipping global workloads", c.ID)
							hc.DiscardResult()
							return ""
						}
						nw := *nwPtr
						return network.ID(*nw.Get())
					},
				),
				append(
					opts.WithName(fmt.Sprintf("EndpointSliceWorkloads[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...)
			EndpointSliceWorkloadsWithCluster := krt.MapCollection(
				EndpointSliceWorkloads,
				wrapObjectWithCluster[model.WorkloadInfo](c.ID),
				append(
					opts.WithName(fmt.Sprintf("EndpointSliceWorkloadsWithCluster[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)

			NetworkGatewayWorkloads := krt.NewManyFromNothing[model.WorkloadInfo](func(ctx krt.HandlerContext) []model.WorkloadInfo {
				return slices.Map(LookupAllNetworkGateway(
					ctx,
					globalNetworks.NetworkGateways,
				), convertGateway)
			}, append(
				opts.WithName(fmt.Sprintf("NetworkGatewayWorkloads[%s]", c.ID)),
				krt.WithMetadata(krt.Metadata{
					multicluster.ClusterKRTMetadataKey: c.ID,
				}),
			)...)
			NetworkGatewayWorkloadsWithCluster := krt.MapCollection(
				NetworkGatewayWorkloads,
				wrapObjectWithCluster[model.WorkloadInfo](c.ID),
				append(
					opts.WithName(fmt.Sprintf("LocalNetworkGatewayWorkloadsWithCluster[%s]", c.ID)),
					krt.WithMetadata(krt.Metadata{
						multicluster.ClusterKRTMetadataKey: c.ID,
					}),
				)...,
			)

			results := map[*collectionCacheByCluster[config.ObjectWithCluster[model.WorkloadInfo]]]bool{
				podWorkloadsCache:            podWorkloadsCache.Insert(PodWorkloadsWithCluster),
				workloadEntryWorkloadsCache:  workloadEntryWorkloadsCache.Insert(WorkloadEntryWorkloadsWithCluster),
				serviceEntryWorkloadsCache:   serviceEntryWorkloadsCache.Insert(ServiceEntryWorkloadsWithCluster),
				endpointSliceWorkloadsCache:  endpointSliceWorkloadsCache.Insert(EndpointSliceWorkloadsWithCluster),
				networkGatewayWorkloadsCache: networkGatewayWorkloadsCache.Insert(NetworkGatewayWorkloadsWithCluster),
			}

			if slices.Contains(maps.Values(results), false) {
				for cache, ok := range results {
					if !ok {
						log.Warnf("Failed to insert collection into cache %v for cluster %s", cache, c.ID)
					}
				}
				return nil
			}

			cols := []krt.Collection[config.ObjectWithCluster[model.WorkloadInfo]]{
				PodWorkloadsWithCluster,
				WorkloadEntryWorkloadsWithCluster,
				ServiceEntryWorkloadsWithCluster,
				EndpointSliceWorkloadsWithCluster,
				NetworkGatewayWorkloadsWithCluster,
			}

			return cols
		},
		opts.WithName("RemoteWorkloadInfosWithCluster")...,
	)

	RemoteWorkloadInfosWithCluster.RegisterBatch(func(o []krt.Event[krt.Collection[config.ObjectWithCluster[model.WorkloadInfo]]]) {
		for _, e := range o {
			l := e.Latest()
			switch e.Event {
			case controllers.EventAdd, controllers.EventUpdate:
				GlobalWorkloadInfosWithCluster.UpdateObject(l)
			case controllers.EventDelete:
				GlobalWorkloadInfosWithCluster.DeleteObject(krt.GetKey(l))
			}
		}
	}, true)
	col := krt.NestedJoinWithMergeCollection(
		GlobalWorkloadInfosWithCluster,
		mergeWorkloadInfosWithCluster(localClusterID),
		opts.WithName("MergedGlobalWorkloadsWithCluster")...,
	)
	return krt.MapCollection(col, unwrapObjectWithCluster[model.WorkloadInfo], opts.WithName("MergedGlobalWorkloads")...)
}

func workloadEntryWorkloadBuilder(
	meshConfig krt.Singleton[MeshConfig],
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	waypoints krt.Collection[Waypoint],
	workloadServices krt.Collection[model.ServiceInfo],
	workloadServicesNamespaceIndex krt.Index[string, model.ServiceInfo],
	namespaces krt.Collection[*v1.Namespace],
	clusterGetter func(krt.HandlerContext) cluster.ID,
	networkGetter func(krt.HandlerContext) network.ID,
	networkGateways krt.Collection[NetworkGateway],
	gatewaysByNetwork krt.Index[network.ID, NetworkGateway],
	flags FeatureFlags,
) krt.TransformationSingle[*networkingclient.WorkloadEntry, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, wle *networkingclient.WorkloadEntry) *model.WorkloadInfo {
		// WLE can put labels in multiple places; normalize this
		wle = serviceentry.ConvertClientWorkloadEntry(wle)
		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
		policies := buildWorkloadPolicies(ctx, authorizationPolicies, peerAuths, meshCfg, wle.Labels, wle.Namespace)

		appTunnel, targetWaypoint := computeWaypoint(ctx, waypoints, namespaces, wle.ObjectMeta)

		fo := []krt.FetchOption{krt.FilterIndex(workloadServicesNamespaceIndex, wle.Namespace), krt.FilterSelectsNonEmpty(wle.GetLabels())}
		if !flags.EnableK8SServiceSelectWorkloadEntries {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source.Kind == kind.ServiceEntry
			}))
		}
		services := krt.Fetch(ctx, workloadServices, fo...)
		network := networkGetter(ctx).String()
		cluster := clusterGetter(ctx)
		if wle.Spec.Network != "" {
			network = wle.Spec.Network
		}

		// enforce traversing waypoints
		policies = append(policies, implicitWaypointPolicies(flags, ctx, waypoints, targetWaypoint, services)...)

		w := &workloadapi.Workload{
			Uid:                   generateWorkloadEntryUID(cluster, wle.Namespace, wle.Name),
			Name:                  wle.Name,
			Namespace:             wle.Namespace,
			Network:               network,
			NetworkGateway:        getNetworkGatewayAddress(ctx, network, networkGateways, gatewaysByNetwork),
			ClusterId:             string(cluster),
			ServiceAccount:        wle.Spec.ServiceAccount,
			Services:              constructServicesFromWorkloadEntry(&wle.Spec, services),
			AuthorizationPolicies: policies,
			Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
			Waypoint:              targetWaypoint.GetAddress(),
			ApplicationTunnel:     appTunnel,
			TrustDomain:           pickTrustDomain(meshCfg),
			Locality:              getWorkloadEntryLocality(&wle.Spec),
		}
		if wle.Spec.Weight > 0 {
			w.Capacity = wrappers.UInt32(wle.Spec.Weight)
		}

		if addr, err := netip.ParseAddr(wle.Spec.Address); err == nil {
			w.Addresses = [][]byte{addr.AsSlice()}
		} else {
			w.Hostname = wle.Spec.Address
		}

		w.WorkloadName = kubelabels.WorkloadNameFromWorkloadEntry(wle.Name, wle.Annotations, wle.Labels)
		w.WorkloadType = workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(wle.Labels, w.WorkloadName)

		setTunnelProtocol(wle.Labels, wle.Annotations, w)
		return precomputeWorkloadPtr(&model.WorkloadInfo{
			Workload:     w,
			Labels:       wle.Labels,
			Source:       kind.WorkloadEntry,
			CreationTime: wle.CreationTimestamp.Time,
		})
	}
}

func (a *index) workloadEntryWorkloadBuilder(
	meshConfig krt.Singleton[MeshConfig],
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	waypoints krt.Collection[Waypoint],
	workloadServices krt.Collection[model.ServiceInfo],
	workloadServicesNamespaceIndex krt.Index[string, model.ServiceInfo],
	namespaces krt.Collection[*v1.Namespace],
) krt.TransformationSingle[*networkingclient.WorkloadEntry, model.WorkloadInfo] {
	return workloadEntryWorkloadBuilder(
		meshConfig,
		authorizationPolicies,
		peerAuths,
		waypoints,
		workloadServices,
		workloadServicesNamespaceIndex,
		namespaces,
		func(ctx krt.HandlerContext) cluster.ID {
			return a.ClusterID
		},
		func(ctx krt.HandlerContext) network.ID {
			return a.Network(ctx)
		},
		a.networks.NetworkGateways,
		a.networks.GatewaysByNetwork,
		a.Flags,
	)
}

func computeWaypoint(
	ctx krt.HandlerContext,
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	workloadMeta metav1.ObjectMeta,
) (*workloadapi.ApplicationTunnel, *Waypoint) {
	var appTunnel *workloadapi.ApplicationTunnel
	var targetWaypoint *Waypoint
	if instancedWaypoint := fetchWaypointForInstance(ctx, waypoints, workloadMeta); instancedWaypoint != nil {
		// we're an instance of a waypoint, set inbound tunnel info if needed
		if db := instancedWaypoint.DefaultBinding; db != nil {
			appTunnel = &workloadapi.ApplicationTunnel{
				Protocol: db.Protocol,
				Port:     db.Port,
			}
		}
	} else if waypoint, err := fetchWaypointForWorkload(ctx, waypoints, namespaces, workloadMeta); err == nil {
		// there is a workload-attached waypoint, point there with a GatewayAddress
		// TODO: report status for workload-attached waypoints
		targetWaypoint = waypoint
	}
	return appTunnel, targetWaypoint
}

func podWorkloadBuilder(
	meshConfig krt.Singleton[MeshConfig],
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	waypoints krt.Collection[Waypoint],
	workloadServices krt.Collection[model.ServiceInfo],
	workloadServicesNamespaceIndex krt.Index[string, model.ServiceInfo],
	endpointSlices krt.Collection[*discovery.EndpointSlice],
	endpointSlicesAddressIndex krt.Index[TargetRef, *discovery.EndpointSlice],
	namespaces krt.Collection[*v1.Namespace],
	nodes krt.Collection[Node],
	domainSuffix string,
	clusterGetter func(krt.HandlerContext) cluster.ID,
	networkGetter func(krt.HandlerContext) network.ID,
	networkGateways krt.Collection[NetworkGateway],
	gatewaysByNetwork krt.Index[network.ID, NetworkGateway],
	flags FeatureFlags,
) krt.TransformationSingle[*v1.Pod, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
		// Pod Is Pending but have a pod IP should be a valid workload, we should build it ,
		// Such as the pod have initContainer which is initialing.
		// See https://github.com/istio/istio/issues/48854
		if kubeutil.CheckPodTerminal(p) {
			return nil
		}
		k8sPodIPs := getPodIPs(p)
		if len(k8sPodIPs) == 0 {
			return nil
		}
		podIPs, err := slices.MapErr(k8sPodIPs, func(e v1.PodIP) ([]byte, error) {
			n, err := netip.ParseAddr(e.IP)
			if err != nil {
				return nil, err
			}
			return n.AsSlice(), nil
		})
		if err != nil {
			// Is this possible? Probably not in typical case, but anyone could put garbage there.
			return nil
		}
		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
		policies := buildWorkloadPolicies(ctx, authorizationPolicies, peerAuths, meshCfg, p.Labels, p.Namespace)
		fo := []krt.FetchOption{krt.FilterIndex(workloadServicesNamespaceIndex, p.Namespace), krt.FilterSelectsNonEmpty(p.GetLabels())}
		if !features.EnableServiceEntrySelectPods {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source.Kind == kind.Service
			}))
		}
		services := krt.Fetch(ctx, workloadServices, fo...)
		services = append(services, matchingServicesWithoutSelectors(ctx, p, services, workloadServices, endpointSlices, endpointSlicesAddressIndex, domainSuffix)...)
		// Logic from https://github.com/kubernetes/kubernetes/blob/7c873327b679a70337288da62b96dd610858181d/staging/src/k8s.io/endpointslice/utils.go#L37
		// Kubernetes has Ready, Serving, and Terminating. We only have a boolean, which is sufficient for our cases
		status := workloadapi.WorkloadStatus_HEALTHY
		if !IsPodReady(p) || p.DeletionTimestamp != nil {
			status = workloadapi.WorkloadStatus_UNHEALTHY
		}
		// We only check the network of the first IP. This should be fine; it is not supported for a single pod to span multiple networks
		network := networkGetter(ctx).String()

		cluster := clusterGetter(ctx)

		appTunnel, targetWaypoint := computeWaypoint(ctx, waypoints, namespaces, p.ObjectMeta)

		// enforce traversing waypoints
		policies = append(policies, implicitWaypointPolicies(flags, ctx, waypoints, targetWaypoint, services)...)

		w := &workloadapi.Workload{
			Uid:                   generatePodUID(cluster, p),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               network,
			NetworkGateway:        getNetworkGatewayAddress(ctx, network, networkGateways, gatewaysByNetwork),
			ClusterId:             cluster.String(),
			Addresses:             podIPs,
			ServiceAccount:        p.Spec.ServiceAccountName,
			Waypoint:              targetWaypoint.GetAddress(),
			Node:                  p.Spec.NodeName,
			ApplicationTunnel:     appTunnel,
			Services:              constructServices(p, services),
			AuthorizationPolicies: policies,
			Status:                status,
			TrustDomain:           pickTrustDomain(meshCfg),
			Locality:              getPodLocality(ctx, nodes, p),
		}

		if p.Spec.HostNetwork {
			w.NetworkMode = workloadapi.NetworkMode_HOST_NETWORK
		}

		w.WorkloadName = workloadName(p)
		w.WorkloadType = workloadapi.WorkloadType_POD // backwards compatibility
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		setTunnelProtocol(p.Labels, p.Annotations, w)
		return precomputeWorkloadPtr(&model.WorkloadInfo{
			Workload:     w,
			Labels:       p.Labels,
			Source:       kind.Pod,
			CreationTime: p.CreationTimestamp.Time,
		})
	}
}

func (a *index) podWorkloadBuilder(
	meshConfig krt.Singleton[MeshConfig],
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	waypoints krt.Collection[Waypoint],
	workloadServices krt.Collection[model.ServiceInfo],
	workloadServicesNamespaceIndex krt.Index[string, model.ServiceInfo],
	endpointSlices krt.Collection[*discovery.EndpointSlice],
	endpointSlicesAddressIndex krt.Index[TargetRef, *discovery.EndpointSlice],
	namespaces krt.Collection[*v1.Namespace],
	nodes krt.Collection[Node],
) krt.TransformationSingle[*v1.Pod, model.WorkloadInfo] {
	return podWorkloadBuilder(
		meshConfig,
		authorizationPolicies,
		peerAuths,
		waypoints,
		workloadServices,
		workloadServicesNamespaceIndex,
		endpointSlices,
		endpointSlicesAddressIndex,
		namespaces,
		nodes,
		a.DomainSuffix,
		func(ctx krt.HandlerContext) cluster.ID {
			return a.ClusterID
		},
		func(ctx krt.HandlerContext) network.ID {
			return a.Network(ctx)
		},
		a.networks.NetworkGateways,
		a.networks.GatewaysByNetwork,
		a.Flags,
	)
}

func getPodIPs(p *v1.Pod) []v1.PodIP {
	k8sPodIPs := p.Status.PodIPs
	if len(k8sPodIPs) == 0 && p.Status.PodIP != "" {
		k8sPodIPs = []v1.PodIP{{IP: p.Status.PodIP}}
	}
	return k8sPodIPs
}

// matchingServicesWithoutSelectors finds all Services that match a given pod that do not use selectors.
// See https://kubernetes.io/docs/concepts/services-networking/service/#services-without-selectors for more info.
// For selector service, we query by the selector elsewhere, so this only handles the services that are NOT already found
// by a selector.
// For EndpointSlices that happen to point to the same IP as the pod, but are not directly bound to the pod (via TargetRef),
// we ignore them here. These will produce a model.Workload directly from the EndpointSlice, but with limited information;
// we do not implicitly merge a Pod with an EndpointSlice just based on IP.
func matchingServicesWithoutSelectors(
	ctx krt.HandlerContext,
	p *v1.Pod,
	alreadyMatchingServices []model.ServiceInfo,
	workloadServices krt.Collection[model.ServiceInfo],
	endpointSlices krt.Collection[*discovery.EndpointSlice],
	endpointSlicesAddressIndex krt.Index[TargetRef, *discovery.EndpointSlice],
	domainSuffix string,
) []model.ServiceInfo {
	var res []model.ServiceInfo
	// Build out our set of already-matched services to avoid double-selecting a service
	seen := sets.NewWithLength[string](len(alreadyMatchingServices))
	for _, s := range alreadyMatchingServices {
		seen.Insert(s.Service.Hostname)
	}
	tr := TargetRef{
		Kind:      gvk.Pod.Kind,
		Namespace: p.Namespace,
		Name:      p.Name,
		UID:       p.UID,
	}
	// For each IP, find any endpointSlices referencing it.
	matchedSlices := krt.Fetch(ctx, endpointSlices, krt.FilterIndex(endpointSlicesAddressIndex, tr))
	for _, es := range matchedSlices {
		serviceName, f := es.Labels[discovery.LabelServiceName]
		if !f {
			// Not for a service; we don't care about it.
			continue
		}
		hostname := string(kube.ServiceHostname(serviceName, es.Namespace, domainSuffix))
		if seen.Contains(hostname) {
			// We already know about this service
			continue
		}
		// This pod is included in the EndpointSlice. We need to fetch the Service object for it, by key.
		serviceKey := es.Namespace + "/" + hostname
		svcs := krt.Fetch(ctx, workloadServices, krt.FilterKey(serviceKey), krt.FilterGeneric(func(a any) bool {
			// Only find Service, not Service Entry
			return a.(model.ServiceInfo).Source.Kind == kind.Service
		}))
		if len(svcs) == 0 {
			// no service found
			continue
		}
		// There SHOULD only be one. This is only for `Service` which has unique hostnames.
		svc := svcs[0]
		res = append(res, svc)
	}
	return res
}

func buildWorkloadPolicies(
	ctx krt.HandlerContext,
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	meshCfg *MeshConfig,
	workloadLabels map[string]string,
	workloadNamespace string,
) []string {
	// We need to filter from the policies that are present, which apply to us.
	// We only want label selector ones; global ones are not attached to the final WorkloadInfo
	// In general we just take all of the policies
	basePolicies := krt.Fetch(
		ctx,
		authorizationPolicies,
		krt.FilterSelects(workloadLabels),
		krt.FilterGeneric(func(a any) bool {
			wa := a.(model.WorkloadAuthorization)
			if wa.Authorization == nil {
				return false // filter policy which are invalid, only exist to hold the error condition
			}
			nsMatch := wa.Authorization.Namespace == meshCfg.RootNamespace || wa.Authorization.Namespace == workloadNamespace
			return nsMatch && wa.GetLabelSelector() != nil
		}),
	)
	policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
		return t.ResourceName()
	}))
	// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
	auths := fetchPeerAuthentications(ctx, peerAuths, meshCfg, workloadNamespace, workloadLabels)
	policies = append(policies, convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
	return policies
}

func serviceEntryWorkloadBuilder(
	meshConfig krt.Singleton[MeshConfig],
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
	clusterGetter func(krt.HandlerContext) cluster.ID,
	networkGetter func(krt.HandlerContext) network.ID,
	networkGateways krt.Collection[NetworkGateway],
	gatewaysByNetwork krt.Index[network.ID, NetworkGateway],
	flags FeatureFlags,
) krt.TransformationMulti[*networkingclient.ServiceEntry, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, se *networkingclient.ServiceEntry) []model.WorkloadInfo {
		eps := se.Spec.Endpoints
		// If we have a DNS service, endpoints are not required
		implicitEndpoints := len(eps) == 0 &&
			(se.Spec.Resolution == networkingv1alpha3.ServiceEntry_DNS || se.Spec.Resolution == networkingv1alpha3.ServiceEntry_DNS_ROUND_ROBIN) &&
			se.Spec.WorkloadSelector == nil
		if len(eps) == 0 && !implicitEndpoints {
			return nil
		}

		nw := networkGetter(ctx)
		cluster := clusterGetter(ctx)
		// here we don't care about the *service* waypoint (hence it is nil); we are only going to use a subset of the info in
		// `allServices` (since we are building workloads here, not services).
		allServices := serviceEntriesInfo(ctx, se, nil, nil, networkGetter)
		if implicitEndpoints {
			eps = slices.Map(allServices, func(si model.ServiceInfo) *networkingv1alpha3.WorkloadEntry {
				return &networkingv1alpha3.WorkloadEntry{Address: si.Service.Hostname}
			})
		}
		if len(eps) == 0 {
			return nil
		}
		res := make([]model.WorkloadInfo, 0, len(eps))

		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())

		for i, wle := range eps {
			services := allServices
			if implicitEndpoints {
				// For implicit endpoints, we generate each one from the hostname it was from.
				// Otherwise, use all.
				// [i] is safe here since we these are constructed to mirror each other
				services = []model.ServiceInfo{allServices[i]}
			}

			policies := buildWorkloadPolicies(ctx, authorizationPolicies, peerAuths, meshCfg, se.Labels, se.Namespace)

			var appTunnel *workloadapi.ApplicationTunnel
			var targetWaypoint *Waypoint
			// Endpoint does not have a real ObjectMeta, so make one
			if !implicitEndpoints {
				objMeta := metav1.ObjectMeta{
					Name:      se.Name,
					Namespace: se.Namespace,
					Labels:    wle.Labels,
				}
				appTunnel, targetWaypoint = computeWaypoint(ctx, waypoints, namespaces, objMeta)
			}

			// enforce traversing waypoints
			policies = append(policies, implicitWaypointPolicies(flags, ctx, waypoints, targetWaypoint, services)...)

			if wle.Network != "" {
				nw = network.ID(wle.Network)
			}
			w := &workloadapi.Workload{
				Uid:                   generateServiceEntryUID(cluster, se.Namespace, se.Name, wle.Address),
				Name:                  se.Name,
				Namespace:             se.Namespace,
				Network:               nw.String(),
				NetworkGateway:        getNetworkGatewayAddress(ctx, nw.String(), networkGateways, gatewaysByNetwork),
				ClusterId:             cluster.String(),
				ServiceAccount:        wle.ServiceAccount,
				Services:              constructServicesFromWorkloadEntry(wle, services),
				AuthorizationPolicies: policies,
				Status:                workloadapi.WorkloadStatus_HEALTHY,
				Waypoint:              targetWaypoint.GetAddress(),
				ApplicationTunnel:     appTunnel,
				TrustDomain:           pickTrustDomain(meshCfg),
				Locality:              getWorkloadEntryLocality(wle),
			}
			if wle.Weight > 0 {
				w.Capacity = wrappers.UInt32(wle.Weight)
			}

			if addr, err := netip.ParseAddr(wle.Address); err == nil {
				w.Addresses = [][]byte{addr.AsSlice()}
			} else {
				w.Hostname = wle.Address
			}

			w.WorkloadName, w.WorkloadType = se.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
			w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(se.Labels, w.WorkloadName)

			setTunnelProtocol(se.Labels, se.Annotations, w)
			res = append(res, precomputeWorkload(model.WorkloadInfo{
				Workload:     w,
				Labels:       se.Labels,
				Source:       kind.WorkloadEntry,
				CreationTime: se.CreationTimestamp.Time,
			}))
		}
		return res
	}
}

func (a *index) serviceEntryWorkloadBuilder(
	meshConfig krt.Singleton[MeshConfig],
	authorizationPolicies krt.Collection[model.WorkloadAuthorization],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	waypoints krt.Collection[Waypoint],
	namespaces krt.Collection[*v1.Namespace],
) krt.TransformationMulti[*networkingclient.ServiceEntry, model.WorkloadInfo] {
	return serviceEntryWorkloadBuilder(
		meshConfig,
		authorizationPolicies,
		peerAuths,
		waypoints,
		namespaces,
		func(ctx krt.HandlerContext) cluster.ID {
			return a.ClusterID
		},
		func(ctx krt.HandlerContext) network.ID {
			return a.Network(ctx)
		},
		a.networks.NetworkGateways,
		a.networks.GatewaysByNetwork,
		a.Flags,
	)
}

func endpointSlicesBuilder(
	meshConfig krt.Singleton[MeshConfig],
	workloadServices krt.Collection[model.ServiceInfo],
	domainSuffix string,
	clusterGetter func(krt.HandlerContext) cluster.ID,
	networkGetter func(krt.HandlerContext) network.ID,
) krt.TransformationMulti[*discovery.EndpointSlice, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, es *discovery.EndpointSlice) []model.WorkloadInfo {
		// EndpointSlices carry port information and a list of IPs.
		// We only care about EndpointSlices that are for a Service.
		// Otherwise, it is just an arbitrary bag of IP addresses for some user-specific purpose, which doesn't have a clear
		// usage for us (if it had some additional info like service account, etc, then perhaps it would be useful).
		serviceName, f := es.Labels[discovery.LabelServiceName]
		if !f {
			return nil
		}
		if es.AddressType == discovery.AddressTypeFQDN {
			// Currently we do not support FQDN. In theory, we could, but its' support in Kubernetes entirely is questionable and
			// may be removed in the near future.
			return nil
		}
		var res []model.WorkloadInfo
		seen := sets.New[string]()
		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())

		// The slice must be for a single service, based on the label above.
		serviceKey := es.Namespace + "/" + string(kube.ServiceHostname(serviceName, es.Namespace, domainSuffix))
		svcs := krt.Fetch(ctx, workloadServices, krt.FilterKey(serviceKey), krt.FilterGeneric(func(a any) bool {
			// Only find Service, not Service Entry
			return a.(model.ServiceInfo).Source.Kind == kind.Service
		}))
		if len(svcs) == 0 {
			// no service found
			return nil
		}
		// There SHOULD only be one. This is only Service which has unique hostnames.
		svc := svcs[0]

		// Translate slice ports to our port.
		pl := &workloadapi.PortList{Ports: make([]*workloadapi.Port, 0, len(es.Ports))}
		for _, p := range es.Ports {
			// We must have name and port (Kubernetes should always set these)
			if p.Name == nil {
				continue
			}
			if p.Port == nil {
				continue
			}
			// We only support TCP for now
			if p.Protocol == nil || *p.Protocol != v1.ProtocolTCP {
				continue
			}
			// Endpoint slice port has name (service port name, not containerPort) and port (targetPort)
			// We need to join with the Service port list to translate the port name to
			for _, svcPort := range svc.Service.Ports {
				portName := svc.PortNames[int32(svcPort.ServicePort)]
				if portName.PortName != *p.Name {
					continue
				}
				pl.Ports = append(pl.Ports, &workloadapi.Port{
					ServicePort: svcPort.ServicePort,
					TargetPort:  uint32(*p.Port),
				})
				break
			}
		}
		services := map[string]*workloadapi.PortList{
			serviceKey: pl,
		}

		// Each endpoint in the slice is going to create a Workload
		for _, ep := range es.Endpoints {
			if ep.TargetRef != nil && ep.TargetRef.Kind == gvk.Pod.Kind {
				// Normal case; this is a slice for a pod. We already handle pods, with much more information, so we can skip them
				continue
			}
			// This should not be possible
			if len(ep.Addresses) == 0 {
				continue
			}
			// We currently only support 1 address. Kubernetes will never set more (IPv4 and IPv6 will be two slices), so its mostly undefined.
			key := ep.Addresses[0]
			if seen.InsertContains(key) {
				// Shouldn't happen. Make sure our UID is actually unique
				log.Warnf("IP address %v seen twice in %v/%v", key, es.Namespace, es.Name)
				continue
			}
			health := workloadapi.WorkloadStatus_UNHEALTHY
			if ep.Conditions.Ready == nil || *ep.Conditions.Ready {
				health = workloadapi.WorkloadStatus_HEALTHY
			}
			// Translate our addresses.
			// Note: users may put arbitrary addresses here. It is recommended by Kubernetes to not
			// give untrusted users EndpointSlice write access.
			addresses, err := slices.MapErr(ep.Addresses, func(e string) ([]byte, error) {
				n, err := netip.ParseAddr(e)
				if err != nil {
					log.Warnf("invalid address in endpointslice %v: %v", e, err)
					return nil, err
				}
				return n.AsSlice(), nil
			})
			if err != nil {
				// If any invalid, skip
				continue
			}
			network := networkGetter(ctx)
			cluster := clusterGetter(ctx)
			w := &workloadapi.Workload{
				Uid:         cluster.String() + "/discovery.k8s.io/EndpointSlice/" + es.Namespace + "/" + es.Name + "/" + key,
				Name:        es.Name,
				Namespace:   es.Namespace,
				Addresses:   addresses,
				Hostname:    "",
				Network:     network.String(),
				TrustDomain: pickTrustDomain(meshCfg),
				Services:    services,
				Status:      health,
				ClusterId:   cluster.String(),
				// For opaque endpoints, we do not know anything about them. They could be overlapping with other IPs, so treat it
				// as a shared address rather than a unique one.
				NetworkMode:           workloadapi.NetworkMode_HOST_NETWORK,
				AuthorizationPolicies: nil, // Not support. This can only be used for outbound, so not relevant
				ServiceAccount:        "",  // Unknown. TODO: make this possible to express in ztunnel
				Waypoint:              nil, // Not supported. In theory, we could allow it as an EndpointSlice label, but there is no real use case.
				Locality:              nil, // Not supported. We could maybe, there is a "zone", but it doesn't seem to be well supported
			}
			res = append(res, precomputeWorkload(model.WorkloadInfo{
				Workload:     w,
				Labels:       nil,
				Source:       kind.EndpointSlice,
				CreationTime: es.CreationTimestamp.Time,
			}))
		}

		return res
	}
}

func (a *index) endpointSlicesBuilder(
	meshConfig krt.Singleton[MeshConfig],
	workloadServices krt.Collection[model.ServiceInfo],
) krt.TransformationMulti[*discovery.EndpointSlice, model.WorkloadInfo] {
	return endpointSlicesBuilder(
		meshConfig,
		workloadServices,
		a.DomainSuffix,
		func(ctx krt.HandlerContext) cluster.ID {
			return a.ClusterID
		},
		func(ctx krt.HandlerContext) network.ID {
			return a.Network(ctx)
		},
	)
}

func setTunnelProtocol(labels, annotations map[string]string, w *workloadapi.Workload) {
	if annotations[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionEnabled {
		// Configured for override
		w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
	}
	// Otherwise supports tunnel directly
	if model.SupportsTunnel(labels, model.TunnelHTTP) {
		w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		w.NativeTunnel = true
	}
}

func pickTrustDomain(mesh *MeshConfig) string {
	if td := mesh.GetTrustDomain(); td != "cluster.local" {
		return td
	}
	return ""
}

func fetchPeerAuthentications(
	ctx krt.HandlerContext,
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	meshCfg *MeshConfig,
	ns string,
	matchLabels map[string]string,
) []*securityclient.PeerAuthentication {
	return krt.Fetch(ctx, peerAuths, krt.FilterGeneric(func(a any) bool {
		pol := a.(*securityclient.PeerAuthentication)
		if pol.Namespace == meshCfg.GetRootNamespace() && pol.Spec.Selector == nil {
			return true
		}
		if pol.Namespace != ns {
			return false
		}
		sel := pol.Spec.Selector
		if sel == nil {
			return true // No selector matches everything
		}
		return labels.Instance(sel.MatchLabels).SubsetOf(matchLabels)
	}))
}

func constructServicesFromWorkloadEntry(p *networkingv1alpha3.WorkloadEntry, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Service.Namespace, svc.Service.Hostname)
		pl := &workloadapi.PortList{}
		res[n] = pl
		for _, port := range svc.Service.Ports {
			targetPort := port.TargetPort
			// Named targetPort has different semantics from Service vs ServiceEntry
			if svc.Source.Kind == kind.Service {
				// Service has explicit named targetPorts.
				if named, f := svc.PortNames[int32(port.ServicePort)]; f && named.TargetPortName != "" {
					// This port is a named target port, look it up
					tv, ok := p.Ports[named.TargetPortName]
					if !ok {
						// We needed an explicit port, but didn't find one - skip this port
						continue
					}
					targetPort = tv
				}
			} else {
				// ServiceEntry has no explicit named targetPorts; targetPort only allows a number
				// Instead, there is name matching between the port names
				if named, f := svc.PortNames[int32(port.ServicePort)]; f {
					// get port name or target port
					tv, ok := p.Ports[named.PortName]
					if ok {
						// if we match one, override it. Otherwise, use the service port
						targetPort = tv
					} else if targetPort == 0 {
						targetPort = port.ServicePort
					}
				}
			}
			pl.Ports = append(pl.Ports, &workloadapi.Port{
				ServicePort: port.ServicePort,
				TargetPort:  targetPort,
			})
		}
	}
	return res
}

func workloadName(pod *v1.Pod) string {
	objMeta, _ := kubeutil.GetWorkloadMetaFromPod(pod)
	return objMeta.Name
}

func constructServices(p *v1.Pod, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Service.Namespace, svc.Service.Hostname)
		pl := &workloadapi.PortList{
			Ports: make([]*workloadapi.Port, 0, len(svc.Service.Ports)),
		}
		res[n] = pl
		for _, port := range svc.Service.Ports {
			targetPort := port.TargetPort
			// The svc.Ports represents the workloadapi.Service, which drops the port name info and just has numeric target Port.
			// TargetPort can be 0 which indicates its a named port. Check if its a named port and replace with the real targetPort if so.
			if named, f := svc.PortNames[int32(port.ServicePort)]; f && named.TargetPortName != "" {
				// Pods only match on TargetPort names
				tp, ok := FindPortName(p, named.TargetPortName)
				if !ok {
					// Port not present for this workload. Exclude the port entirely
					continue
				}
				targetPort = uint32(tp)
			}

			pl.Ports = append(pl.Ports, &workloadapi.Port{
				ServicePort: port.ServicePort,
				TargetPort:  targetPort,
			})
		}
	}
	return res
}

func getPodLocality(ctx krt.HandlerContext, Nodes krt.Collection[Node], pod *v1.Pod) *workloadapi.Locality {
	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#late-initialization
	node := krt.FetchOne(ctx, Nodes, krt.FilterKey(pod.Spec.NodeName))
	if node == nil {
		if pod.Spec.NodeName != "" {
			log.Warnf("unable to get node %q for pod %q/%q", pod.Spec.NodeName, pod.Namespace, pod.Name)
		}
		return nil
	}
	return node.Locality
}

func getWorkloadEntryLocality(p *networkingv1alpha3.WorkloadEntry) *workloadapi.Locality {
	region, zone, subzone := labelutil.SplitLocalityLabel(p.GetLocality())
	if region == "" && zone == "" && subzone == "" {
		return nil
	}
	return &workloadapi.Locality{
		Region:  region,
		Zone:    zone,
		Subzone: subzone,
	}
}

func implicitWaypointPolicies(
	flags FeatureFlags,
	ctx krt.HandlerContext,
	Waypoints krt.Collection[Waypoint],
	waypoint *Waypoint,
	services []model.ServiceInfo,
) []string {
	if !flags.DefaultAllowFromWaypoint {
		return nil
	}
	serviceWaypointKeys := slices.MapFilter(services, func(si model.ServiceInfo) *string {
		if si.Waypoint.ResourceName == "" || (waypoint != nil && waypoint.ResourceName() == si.Waypoint.ResourceName) {
			return nil
		}
		return ptr.Of(si.Waypoint.ResourceName)
	})
	if len(serviceWaypointKeys) == 0 {
		if waypoint != nil {
			n := implicitWaypointPolicyName(flags, waypoint)
			if n != "" {
				return []string{waypoint.Namespace + "/" + n}
			}
		}
		return nil
	}
	waypoints := krt.Fetch(ctx, Waypoints, krt.FilterKeys(serviceWaypointKeys...))
	if waypoint != nil {
		waypoints = append(waypoints, *waypoint)
	}

	return slices.MapFilter(waypoints, func(w Waypoint) *string {
		policy := implicitWaypointPolicyName(flags, &w)
		if policy == "" {
			return nil
		}
		return ptr.Of(w.Namespace + "/" + policy)
	})
}

func gatewayUID(gw model.NetworkGateway) string {
	return "NetworkGateway/" + string(gw.Network) + "/" + gw.Addr + "/" + strconv.Itoa(int(gw.HBONEPort))
}

// convertGateway always converts a NetworkGateway into a Workload.
// Workloads have a NetworkGateway field, which is effectively a pointer to another object (Service or Workload); in order
// to facilitate this we need to translate our Gateway model down into a WorkloadInfo ztunnel can understand.
func convertGateway(gw NetworkGateway) model.WorkloadInfo {
	wl := &workloadapi.Workload{
		Uid:            gatewayUID(gw.NetworkGateway),
		ServiceAccount: gw.ServiceAccount.Name,
		Namespace:      gw.ServiceAccount.Namespace,
		Network:        gw.Network.String(),
	}

	if ip, err := netip.ParseAddr(gw.Addr); err == nil {
		wl.Addresses = append(wl.Addresses, ip.AsSlice())
	} else {
		wl.Hostname = gw.Addr
	}

	return precomputeWorkload(model.WorkloadInfo{Workload: wl})
}

func getNetworkGatewayAddress(
	ctx krt.HandlerContext,
	n string,
	networkGateways krt.Collection[NetworkGateway],
	gatewaysByNetwork krt.Index[network.ID, NetworkGateway],
) *workloadapi.GatewayAddress {
	if networks := LookupNetworkGateway(ctx, network.ID(n), networkGateways, gatewaysByNetwork); len(networks) > 0 {
		// Currently only support one, so find the first one that is valid
		for _, net := range networks {
			if net.HBONEPort == 0 {
				continue
			}
			ip, err := netip.ParseAddr(net.Addr)
			if err != nil {
				// This is a hostname...
				return &workloadapi.GatewayAddress{
					Destination: &workloadapi.GatewayAddress_Hostname{
						// probably use from Cidr instead?
						Hostname: &workloadapi.NamespacedHostname{
							Namespace: net.ServiceAccount.Namespace,
							Hostname:  net.Addr,
						},
					},
					HboneMtlsPort: net.HBONEPort,
				}
			}
			// Else it must be an IP
			return &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					// probably use from Cidr instead?
					Address: &workloadapi.NetworkAddress{
						Network: net.Network.String(),
						Address: ip.AsSlice(),
					},
				},
				HboneMtlsPort: net.HBONEPort,
			}
		}
	}
	return nil
}

// TargetRef is a subset of the Kubernetes ObjectReference which has some fields we don't care about
type TargetRef struct {
	Kind      string
	Namespace string
	Name      string
	UID       types.UID
}

func (t TargetRef) String() string {
	return t.Kind + "/" + t.Namespace + "/" + t.Name + "/" + string(t.UID)
}

// endpointSliceAddressIndex builds an index from IP Address
func endpointSliceAddressIndex(EndpointSlices krt.Collection[*discovery.EndpointSlice]) krt.Index[TargetRef, *discovery.EndpointSlice] {
	return krt.NewIndex(EndpointSlices, "targetRef", func(es *discovery.EndpointSlice) []TargetRef {
		if es.AddressType == discovery.AddressTypeFQDN {
			// Currently we do not support FQDN.
			return nil
		}
		_, f := es.Labels[discovery.LabelServiceName]
		if !f {
			// Not for a service; we don't care about it.
			return nil
		}
		res := make([]TargetRef, 0, len(es.Endpoints))
		for _, ep := range es.Endpoints {
			if ep.TargetRef == nil || ep.TargetRef.Kind != gvk.Pod.Kind {
				// We only want pods here
				continue
			}
			tr := TargetRef{
				Kind:      ep.TargetRef.Kind,
				Namespace: ep.TargetRef.Namespace,
				Name:      ep.TargetRef.Name,
				UID:       ep.TargetRef.UID,
			}
			res = append(res, tr)
		}
		return res
	})
}

func precomputeWorkloadPtr(w *model.WorkloadInfo) *model.WorkloadInfo {
	return ptr.Of(precomputeWorkload(*w))
}

func precomputeWorkload(w model.WorkloadInfo) model.WorkloadInfo {
	addr := workloadToAddress(w.Workload)
	w.MarshaledAddress = protoconv.MessageToAny(addr)
	w.AsAddress = model.AddressInfo{
		Address:   addr,
		Marshaled: w.MarshaledAddress,
	}
	return w
}
