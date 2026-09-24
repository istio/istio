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

	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/workloadapi"
)

type Node struct {
	Name     string
	Locality *workloadapi.Locality
}

func (n Node) ResourceName() string {
	return n.Name
}

func (n Node) Equals(o Node) bool {
	return n.Name == o.Name &&
		protoconv.Equals(n.Locality, o.Locality)
}

// nodeLocality maps a Kubernetes node to the minimal locality information we track.
func nodeLocality(k *v1.Node) *Node {
	node := &Node{
		Name: k.Name,
	}
	region := k.GetLabels()[v1.LabelTopologyRegion]
	zone := k.GetLabels()[v1.LabelTopologyZone]
	subzone := k.GetLabels()[label.TopologySubzone.Name]

	if region != "" || zone != "" || subzone != "" {
		node.Locality = &workloadapi.Locality{
			Region:  region,
			Zone:    zone,
			Subzone: subzone,
		}
	}

	return node
}

// GlobalNodesCollection builds a nested collection of per-cluster node locality collections.
func GlobalNodesCollection(
	localCluster *multicluster.Cluster,
	localNodeLocality krt.Collection[Node],
	ctrl *multicluster.Controller,
	opts krt.OptionsBuilder,
) krt.Collection[krt.Collection[krt.ObjectWithCluster[Node]]] {
	localNodeLocalityWithCluster := krt.MapCollection(
		localNodeLocality,
		wrapObjectWithCluster[Node](localCluster.ID),
		append(
			opts.WithName("LocalNodeLocalityWithCluster"),
			krt.WithMetadata(krt.Metadata{multicluster.ClusterKRTMetadataKey: localCluster.ID}),
		)...,
	)
	return multicluster.NestedCollectionFromLocalAndRemote(
		ctrl,
		localNodeLocalityWithCluster,
		func(ctx krt.HandlerContext, c *multicluster.Cluster) *krt.Collection[krt.ObjectWithCluster[Node]] {
			if !kube.WaitForCacheSync(fmt.Sprintf("ambient/informer/nodes[%s]", c.ID), opts.Stop(), c.Nodes().HasSynced) {
				log.Warnf("Failed to sync nodes informer for cluster %s", c.ID)
				return nil
			}
			clusterOpts := []krt.CollectionOption{
				krt.WithName(fmt.Sprintf("ambient/NodeLocalityWithCluster[%s]", c.ID)),
				krt.WithDebugging(opts.Debugger()),
				krt.WithStop(c.GetStop()),
				krt.WithMetadata(krt.Metadata{multicluster.ClusterKRTMetadataKey: c.ID}),
			}
			nodes := krt.NewCollection(c.Nodes(), func(ctx krt.HandlerContext, k *v1.Node) *krt.ObjectWithCluster[Node] {
				return &krt.ObjectWithCluster[Node]{
					ClusterID: c.ID,
					Object:    nodeLocality(k),
				}
			}, clusterOpts...)
			return ptr.Of(nodes)
		},
		"NodeLocalityWithCluster",
		opts,
	)
}

// NodesCollection maps a node to it's locality.
// In many environments, nodes change frequently causing excessive recomputation of workloads.
// By making an intermediate collection we can reduce the times we need to trigger dependants (locality should ~never change).
func NodesCollection(nodes krt.Collection[*v1.Node], opts ...krt.CollectionOption) krt.Collection[Node] {
	return krt.NewCollection(nodes, func(ctx krt.HandlerContext, k *v1.Node) *Node {
		return nodeLocality(k)
	}, opts...)
}
