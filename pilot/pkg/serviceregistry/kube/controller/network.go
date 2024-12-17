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

package controller

import (
	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/network"
	v1 "k8s.io/api/core/v1"
	"net"
)

// nodeAddresses represents a kubernetes node that is reachable externally
type nodeAddresses struct {
	address string
	labels  labels.Instance
}

func (n nodeAddresses) Equals(other nodeAddresses) bool {
	return n.address == other.address && n.labels.Equals(other.labels)
}

type networkManager struct {
	clusterID cluster.ID
	stop chan struct{}
}

func initNetworkManager(c *Controller, options Options) *networkManager {
	n := &networkManager{
		stop: make(chan struct{}),
		clusterID: c.clusterID,
	}
	opts := ambient.KrtOptions{
		Stop:     n.stop,
		Debugger: krt.GlobalDebugHandler,
	}
	filter := kclient.Filter{
		ObjectFilter: c.client.ObjectFilter(),
	}
	ConfigMaps := krt.NewInformerFiltered[*v1.ConfigMap](c.client, filter, opts.WithName("ConfigMaps")...)

	ambOpts := ambient.Options{Revision: options.Revision, SystemNamespace: options.SystemNamespace}
	MeshNetworks := ambient.MeshNetworksCollection(ConfigMaps, ambOpts, opts)
	Nodes := krt.NewInformerFiltered[*v1.Node](c.client, kclient.Filter{
		ObjectFilter:    c.client.ObjectFilter(),
		ObjectTransform: kubeclient.StripNodeUnusedFields,
	}, opts.WithName("Nodes")...)
	NodeAddresses := krt.NewCollection(
		Nodes,
		n.nodeAddressesBuilder(),
		opts.WithName("NodeAddresses")...,
	)
	_ = NodeAddresses
	_ = MeshNetworks
	// TODO event handlers for this
	return n
}


func (c *Controller) AppendNetworkGatewayHandler(h func()) {
	//TODO implement me
	panic("implement me")
}

func (n *networkManager) nodeAddressesBuilder() krt.TransformationSingle[*v1.Node, nodeAddresses] {
	return func(ctx krt.HandlerContext, node *v1.Node) *nodeAddresses {
		n := nodeAddresses{labels: node.Labels}
		for _, address := range node.Status.Addresses {
			if address.Type == v1.NodeExternalIP && address.Address != "" {
				n.address = address.Address
				break
			}
		}
		if n.address == "" {
			return nil
		}
		return &n
	}
}

func (n *networkManager) Network(endpointIP string, labels labels.Instance) network.ID {
	// 1. check the pod/workloadEntry label
	if nw := labels[label.TopologyNetwork.Name]; nw != "" {
		return network.ID(nw)
	}

	// 2. check the system namespace labels
	//if nw := c.networkFromSystemNamespace(); nw != "" {
	//	return nw
	//}

	// 3. check the meshNetworks config
	//if nw := c.networkFromMeshNetworks(endpointIP); nw != "" {
	//	return nw
	//}

	return ""
}

// namedRangerEntry for holding network's CIDR and name
type namedRangerEntry struct {
	name    network.ID
	network net.IPNet
}

// Network returns the IPNet for the network
func (n namedRangerEntry) Network() net.IPNet {
	return n.network
}

func (c *Controller) NetworkGateways() []model.NetworkGateway {
	return nil
}

func (n *networkManager) HasSynced() bool {
	return false
}
