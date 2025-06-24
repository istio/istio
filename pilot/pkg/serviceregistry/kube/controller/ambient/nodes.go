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
	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/krt"
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

func GlobalNodesCollection(
	nodes krt.Collection[krt.Collection[config.ObjectWithCluster[*v1.Node]]],
	stop <-chan struct{},
	opts ...krt.CollectionOption,
) krt.Collection[krt.Collection[config.ObjectWithCluster[Node]]] {
	return krt.NewCollection(
		nodes,
		func(ctx krt.HandlerContext, col krt.Collection[config.ObjectWithCluster[*v1.Node]]) *krt.Collection[config.ObjectWithCluster[Node]] {
			clusterID := col.Metadata()[multicluster.ClusterKRTMetadataKey]
			if clusterID == nil {
				panic("cluster metadata is nil for Node collection")
			}
			nc := krt.NewCollection(col, func(ctx krt.HandlerContext, obj config.ObjectWithCluster[*v1.Node]) *config.ObjectWithCluster[Node] {
				k := ptr.Flatten(obj.Object)
				if k == nil {
					log.Warnf("Node %s is nil, skipping", obj.ClusterID)
					return nil
				}
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

				return &config.ObjectWithCluster[Node]{
					ClusterID: obj.ClusterID,
					Object:    node,
				}
			}, append(opts, krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: clusterID,
			}))...)
			return ptr.Of(nc)
		},
		opts...)
}

// NodesCollection maps a node to it's locality.
// In many environments, nodes change frequently causing excessive recomputation of workloads.
// By making an intermediate collection we can reduce the times we need to trigger dependants (locality should ~never change).
func NodesCollection(nodes krt.Collection[*v1.Node], opts ...krt.CollectionOption) krt.Collection[Node] {
	return krt.NewCollection(nodes, func(ctx krt.HandlerContext, k *v1.Node) *Node {
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
	}, opts...)
}
