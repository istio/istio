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
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) buildGlobalCollections(
	_ *multicluster.Cluster,
	_ krt.Collection[*securityclient.AuthorizationPolicy],
	_ krt.Collection[*securityclient.PeerAuthentication],
	_ krt.Collection[*v1beta1.GatewayClass],
	_ krt.Collection[*networkingclient.WorkloadEntry],
	_ krt.Collection[*networkingclient.ServiceEntry],
	_ kclient.Informer[*networkingclient.ServiceEntry],
	_ kclient.Informer[*v1.Service],
	_ kclient.Informer[*securityclient.AuthorizationPolicy],
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

	// These first collections can't be merged since the Kubernetes APIs don't have enough room
	// for e.g. duplicate IPs, etc. So we keep around collections of collections and indexes per cluster.
	// TODO: come back and see if we use all of these indexes
	allPodInformers := krt.NewCollection(clusters, informerForCluster[*v1.Pod](
		filter,
		"Pods",
		opts,
		func(c *Cluster) kclient.Filter {
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
		func(c *Cluster) kclient.Filter {
			return kclient.Filter{
				ObjectFilter:    c.Client.ObjectFilter(),
				ObjectTransform: kubeclient.StripNodeUnusedFields,
			}
		},
	), opts.WithName("AllNodes")...)
	nodeInformersByCluster := informerIndexByCluster(allNodes)
	globalNodes := krt.NewCollection(clusters, collectionFromCluster[*v1.Node]("Nodes", nodeInformersByCluster, opts))
	globalNodesByCluster := nestedCollectionIndexByCluster(globalNodes)

	// Set up collections for remote clusters
	GlobalNetworks := buildGlobalNetworkCollections(
		clusters,
		globalNamespacesByCluster,
		globalGatewaysByCluster,
		options,
		opts,
	)
	a.globalNetworks = GlobalNetworks
	GlobalWaypoints := GlobalWaypointsCollection(
		clusters,
		globalGatewaysByCluster,
		globalGatewayClassesByCluster,
		globalPodsByCluster,
		GlobalNetworks.SystemNamespaceNetworkByCluster,
		opts,
	)
	WaypointsPerCluster := nestedCollectionIndexByCluster(GlobalWaypoints)

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
}

func informerForCluster[T controllers.ComparableObject](
	filter kclient.Filter,
	name string,
	opts krt.OptionsBuilder,
	filterTransform func(c *multicluster.Cluster) kclient.Filter,
) krt.TransformationSingle[*multicluster.Cluster, krt.Collection[config.ObjectWithCluster[T]]] {
	return func(ctx krt.HandlerContext, c *multicluster.Cluster) *krt.Collection[config.ObjectWithCluster[T]] {
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
) krt.TransformationSingle[*Cluster, krt.Collection[config.ObjectWithCluster[T]]] {
	return func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[config.ObjectWithCluster[T]] {
		clients := informerIndex.Lookup(c.ID)
		if len(clients) == 0 {
			return nil
		}
		objWithCluster := krt.NewCollection(clients[0], func(ctx krt.HandlerContext, obj T) *config.ObjectWithCluster[T] {
			return &config.ObjectWithCluster[T]{ClusterID: c.ID, Object: &obj}
		}, opts.WithName(name+"WithCluster")...)
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
	return krt.NewIndex[cluster.ID, krt.Collection[config.ObjectWithCluster[T]]](
		collection,
		"informerByCluster",
		func(col krt.Collection[config.ObjectWithCluster[T]]) []cluster.ID {
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

func mergeServiceInfosWithCluster(localClusterID cluster.ID) func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
	return func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {

		if len(serviceInfos) == 0 {
			return nil
		}
		if len(serviceInfos) == 1 {
			return &serviceInfos[0]
		}

		var vips []*workloadapi.NetworkAddress
		var base config.ObjectWithCluster[model.ServiceInfo]
		for _, obj := range serviceInfos {
			if obj.Object == nil {
				continue
			}
			// This is the base object that we'll add to
			if obj.ClusterID == localClusterID {
				base = obj
				vips = append(vips, obj.Object.Service.Addresses...)
				continue
			}
			vips = append(vips, obj.Object.Service.Addresses...)
		}

		// TODO: merge the rest of the fields
		base.Object.Service.Addresses = vips

		return &base
	}
}
