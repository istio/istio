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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/kube/multicluster"
	v1 "k8s.io/api/core/v1"
)

type ObjectWithCluster[T any] struct {
	ClusterID cluster.ID
	Object    *T
}

func (a *index) buildGlobalCollections(options Options, opts krt.OptionsBuilder) {
	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	clusters := buildRemoteClustersCollection(
		options,
		opts,
		multicluster.DefaultBuildClientsFromConfig,
		filter,
	)

	globalPods := krt.NewCollection(clusters, func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[ObjectWithCluster[*v1.Pod]] {
		podsClient := kclient.NewFiltered[*v1.Pod](c.Client, filter)
		pods := krt.WrapClient[*v1.Pod](podsClient, opts.WithName("informer/Pods")...)
		a := krt.NewCollection(pods, func(ctx krt.HandlerContext, pod *v1.Pod) *ObjectWithCluster[*v1.Pod] {
			return &ObjectWithCluster[*v1.Pod]{ClusterID: c.ID, Object: &pod}
		})
		return &a
	})

	

	// Set up collections for remote clusters
	remoteServiceInfos := krt.NewCollection(clusters, clusterToServiceInfos(filter, opts))
	remoteWorkloadInfos := krt.NewCollection(clusters, clusterToWorkloadInfos)
	remoteWaypoints := krt.NewCollection(clusters, clusterToWaypoints)
	// TODO: Add merge
	GlobalServiceInfos := krt.NestedJoinWithMergeCollection(
		remoteServiceInfos,
		mergeServiceInfos,
		opts.WithName("GlobalServiceInfos")...,
	)
	GlobalWorkloads := krt.NestedJoinWithMergeCollection(
		remoteWorkloadInfos,
		mergeWorkloadInfos,
		opts.WithName("GlobalWorkloads")...,
	)
	GlobalWaypoints := krt.NestedJoinWithMergeCollection(
		remoteWaypoints,
		mergeWaypoints,
		opts.WithName("GlobalWaypoints")...,
	)
	// TODO: Add other indexes
	a.globalServices = servicesCollection{
		Collection: GlobalServiceInfos,
		ByAddress:  krt.NewIndex[networkAddress, model.ServiceInfo](GlobalServiceInfos, networkAddressFromService),
	}
	a.globalWorkloads = workloadsCollection{
		Collection: GlobalWorkloads,
		ByAddress:  krt.NewIndex[networkAddress, model.WorkloadInfo](GlobalWorkloads, networkAddressFromWorkload),
	}
	a.globalWaypoints = waypointsCollection{
		Collection: GlobalWaypoints,
	}
}

func clusterToServiceInfos(
	filter kclient.Filter,
	opts krt.OptionsBuilder,
) func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[ObjectWithCluster[model.ServiceInfo]] {
	return func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[ObjectWithCluster[model.ServiceInfo]] {
		servicesClient := kclient.NewFiltered[*v1.Service](c.Client, filter)
		Services := krt.WrapClient[*v1.Service](servicesClient, opts.WithName("informer/Services")...)
		serviceEntries := kclient.NewDelayedInformer[*networkingclient.ServiceEntry](c.Client,
			gvr.ServiceEntry, kubetypes.StandardInformer, filter)
		ServiceEntries := krt.WrapClient[*networkingclient.ServiceExntry](serviceEntries, opts.WithName("informer/ServiceEntries")...)

		Waypoints := a.WaypointsCollection(Gateways, GatewayClasses, Pods, opts)
		a.ServicesCollection(Services, ServiceEntries, Waypoints, Namespaces, opts)
		return nil
	}
}
func mergeServiceInfos(ts []model.ServiceInfo) model.ServiceInfo {

}

func clusterToWorkloadInfos(ctx krt.HandlerContext, c *Cluster) *krt.Collection[model.WorkloadInfo] {
}

func mergeWorkloadInfos(ts []model.WorkloadInfo) model.WorkloadInfo {

}

func clusterToWaypoints(ctx krt.HandlerContext, c *Cluster) *krt.Collection[Waypoint] {
}

func mergeWaypoints(ts []Waypoint) Waypoint {

}
