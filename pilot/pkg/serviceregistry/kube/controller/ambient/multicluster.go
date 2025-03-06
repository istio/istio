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

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
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

	globalPods := krt.NewCollection(clusters, collectionFromCluster[*v1.Pod](filter, "Pods", opts, func(c *Cluster) kclient.Filter {
		return kclient.Filter{
			ObjectFilter:    c.Client.ObjectFilter(),
			ObjectTransform: kubeclient.StripPodUnusedFields,
		}
	}))
	globalPodsByCluster := informerIndexByCluster(globalPods)

	globalServices := krt.NewCollection(clusters, collectionFromCluster[*v1.Service](filter, "Services", opts, nil))
	globalServicesByCluster := informerIndexByCluster(globalServices)

	globalServiceEntries := krt.NewCollection(clusters, collectionFromCluster[*networkingclient.ServiceEntry](filter, "ServiceEntries", opts, nil))
	globalServiceEntriesByCluster := informerIndexByCluster(globalServiceEntries)

	globalWorkloadEntries := krt.NewCollection(clusters, collectionFromCluster[*networkingclient.WorkloadEntry](filter, "WorkloadEntries", opts, nil))
	globalWorkloadEntriesByCluster := informerIndexByCluster(globalWorkloadEntries)

	globalGateways := krt.NewCollection(clusters, collectionFromCluster[*v1beta1.Gateway](filter, "Gateways", opts, nil))
	globalGatewaysByCluster := informerIndexByCluster(globalGateways)

	globalGatewayClasses := krt.NewCollection(clusters, collectionFromCluster[*v1beta1.GatewayClass](filter, "GatewayClasses", opts, nil))
	globalGatewayClassesByCluster := informerIndexByCluster(globalGatewayClasses)

	namespaces := krt.NewCollection(clusters, collectionFromCluster[*v1.Namespace](filter, "Namespaces", opts, nil))
	namespacesByCluster := informerIndexByCluster(namespaces)

	endpointSlices := krt.NewCollection(clusters, collectionFromCluster[*discovery.EndpointSlice](filter, "EndpointSlice", opts, nil))
	endpointSlicesByCluster := informerIndexByCluster(endpointSlices)

	nodes := krt.NewCollection(clusters, collectionFromCluster[*v1.Node](filter, "Nodes", opts, func(c *Cluster) kclient.Filter {
		return kclient.Filter{
			ObjectFilter:    c.Client.ObjectFilter(),
			ObjectTransform: kubeclient.StripNodeUnusedFields,
		}
	}))
	nodesByCluster := informerIndexByCluster(nodes)

	// Set up collections for remote clusters
	GlobalNetworks := buildGlobalNetworkCollections(
		clusters,
		namespacesByCluster,
		globalGatewaysByCluster,
		options,
		opts,
	)
	a.globalNetworks = GlobalNetworks
}

func collectionFromCluster[T controllers.ComparableObject](
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
		wrappedClient := krt.WrapClient[T](client, opts.WithName("informer/"+name)...)
		objWithCluster := krt.NewCollection(wrappedClient, func(ctx krt.HandlerContext, obj T) *config.ObjectWithCluster[T] {
			return &config.ObjectWithCluster[T]{ClusterID: c.ID, Object: &obj}
		}, opts.WithName(name+"WithCluster")...)
		return &objWithCluster
	}
}

func informerIndexByCluster[T controllers.ComparableObject](
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
