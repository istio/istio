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
	"github.com/yl2chen/cidranger"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/slices"
	"k8s.io/apimachinery/pkg/types"
	"net"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
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
	clusterID                      cluster.ID
	stop                           chan struct{}
	discoverRemoteGatewayResources bool

	RecomputeTrigger         *krt.RecomputeTrigger
	SystemNamespaceNetwork   krt.Singleton[string]
	NetworkGateways          krt.Collection[model.NetworkGateway]
	NetworkGatewaysByNetwork krt.Index[network.ID, model.NetworkGateway]
	ParsedMeshNetworks       krt.Singleton[meshNetworks]
}

type meshNetworks struct {
	ranger  cidranger.Ranger
	network network.ID
}

func initNetworkManager(c *Controller, options Options) *networkManager {
	n := &networkManager{
		stop:                           make(chan struct{}),
		clusterID:                      options.ClusterID,
		discoverRemoteGatewayResources: options.ConfigCluster,
	}
	opts := ambient.KrtOptions{
		Stop:     n.stop,
		Debugger: krt.GlobalDebugHandler,
	}
	filter := kclient.Filter{
		ObjectFilter: c.client.ObjectFilter(),
	}
	ConfigMaps := krt.NewInformerFiltered[*v1.ConfigMap](c.client, filter, opts.WithName("ConfigMaps")...)

	RecomputeTrigger := krt.NewRecomputeTrigger(false, krt.WithName("NetworkTrigger"))
	ambOpts := ambient.Options{Revision: options.Revision, SystemNamespace: options.SystemNamespace}
	Namespaces := krt.NewInformer[*v1.Namespace](c.client, opts.WithName("Namespaces")...)
	SystemNamespaceNetwork := krt.NewSingleton[string](func(ctx krt.HandlerContext) *string {
		ns := ptr.Flatten(krt.FetchOne(ctx, Namespaces, krt.FilterKey(options.SystemNamespace)))
		if ns == nil {
			return nil
		}
		nw, f := ns.Labels[label.TopologyNetwork.Name]
		if !f {
			return nil
		}
		return &nw
	})
	MeshNetworks := ambient.MeshNetworksCollection(ConfigMaps, ambOpts, opts)
	ParsedMeshNetworks := krt.NewSingleton(func(ctx krt.HandlerContext) *meshNetworks {
		mn := krt.FetchOne(ctx, MeshNetworks.AsCollection())
		ranger := cidranger.NewPCTrieRanger()
		res := meshNetworks{}

		if len(mn.Networks) == 0 {
			return nil
		}
		for id, v := range mn.Networks {
			// track endpoints items from this registry are a part of this network
			fromRegistry := false
			for _, ep := range v.Endpoints {
				if ep.GetFromCidr() != "" {
					_, nw, err := net.ParseCIDR(ep.GetFromCidr())
					if err != nil {
						log.Warnf("unable to parse CIDR %q for network %s", ep.GetFromCidr(), id)
						continue
					}
					rangerEntry := namedRangerEntry{
						name:    network.ID(id),
						network: *nw,
					}
					_ = ranger.Insert(rangerEntry)
				}
				if ep.GetFromRegistry() != "" && cluster.ID(ep.GetFromRegistry()) == n.clusterID {
					fromRegistry = true
				}
			}

			// fromRegistry field specified this cluster
			if fromRegistry {
				// treat endpoints in this cluster as part of this network
				if res.network != "" {
					log.Warnf("multiple networks specify %s in fromRegistry; endpoints from %s will continue to be treated as part of %s",
						n.clusterID, n.clusterID, res.network)
				} else {
					res.network = network.ID(id)
				}
			}
		}
		res.ranger = ranger
		return &res
	})
	FromMeshNetworks := krt.NewManyCollection(MeshNetworks.AsCollection(), func(ctx krt.HandlerContext, mn ambient.MeshNetworks) []model.NetworkGateway {
		res := []model.NetworkGateway{}
		if len(mn.Networks) == 0 {
			return nil
		}
		for id, v := range mn.Networks {
			// track endpoints items from this registry are a part of this network
			fromRegistry := false
			for _, ep := range v.Endpoints {
				if ep.GetFromRegistry() != "" && cluster.ID(ep.GetFromRegistry()) == n.clusterID {
					fromRegistry = true
				}
			}

			// fromRegistry field specified this cluster
			if fromRegistry {
				// services in this registry matching the registryServiceName and port are part of this network
				for _, gw := range v.Gateways {
					if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
						res = append(res, model.NetworkGateway{
							Network: network.ID(id),
							Cluster: n.clusterID,
							Port:    gw.GetPort(),
						})
					}
				}
			}
		}
		return res
	})
	Nodes := krt.NewInformerFiltered[*v1.Node](c.client, kclient.Filter{
		ObjectFilter:    c.client.ObjectFilter(),
		ObjectTransform: kubeclient.StripNodeUnusedFields,
	}, opts.WithName("Nodes")...)
	NodeAddresses := krt.NewCollection(
		Nodes,
		n.nodeAddressesBuilder(),
		opts.WithName("NodeAddresses")...,
	)
	gatewayClient := kclient.NewDelayedInformer[*v1beta1.Gateway](c.client, gvr.KubernetesGateway, kubetypes.StandardInformer, filter)
	Gateways := krt.WrapClient[*v1beta1.Gateway](gatewayClient, opts.WithName("Gateways")...)
	FromGateway := krt.NewManyCollection(
		Gateways,
		n.fromGatewayBuilder(),
		opts.WithName("FromGateway")...,
	)

	NetworkGateways := krt.JoinCollection([]krt.Collection[model.NetworkGateway]{FromGateway, FromMeshNetworks}, opts.WithName("NetworkGateways")...)
	NetworkGatewaysByNetwork := krt.NewIndex(NetworkGateways, func(o model.NetworkGateway) []network.ID {
		return []network.ID{o.Network}
	})
	ParsedMeshNetworks.Register(func(o krt.Event[meshNetworks]) {
		RecomputeTrigger.TriggerRecomputation()
	})

	// TODO make this proper
	RecomputeTrigger.MarkSynced()
	n.NetworkGateways = NetworkGateways
	n.NetworkGatewaysByNetwork = NetworkGatewaysByNetwork
	n.SystemNamespaceNetwork = SystemNamespaceNetwork
	n.RecomputeTrigger = RecomputeTrigger
	n.ParsedMeshNetworks = ParsedMeshNetworks
	_ = NodeAddresses
	_ = MeshNetworks
	_ = SystemNamespaceNetwork
	// TODO event handlers for this
	return n
}

func (c *Controller) AppendNetworkGatewayHandler(h func()) {
	c.networkManager.NetworkGateways.RegisterBatch(func(o []krt.Event[model.NetworkGateway], initialSync bool) {
		h()
	}, false)
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

func (n *networkManager) fromGatewayBuilder() krt.TransformationMulti[*v1beta1.Gateway, model.NetworkGateway] {
	return func(ctx krt.HandlerContext, gw *v1beta1.Gateway) []model.NetworkGateway {
		if nw := gw.GetLabels()[label.TopologyNetwork.Name]; nw == "" {
			return nil
		}

		// Gateway with istio-remote: only discover this from the config cluster
		// this is a way to reference a gateway that lives in a place that this control plane
		// won't have API server access. Nothing will be deployed for these Gateway resources.
		if !n.discoverRemoteGatewayResources && gw.Spec.GatewayClassName == constants.RemoteGatewayClassName {
			return nil
		}

		autoPassthrough := func(l v1beta1.Listener) bool {
			return kube.IsAutoPassthrough(gw.GetLabels(), l)
		}

		base := model.NetworkGateway{
			Network: network.ID(gw.GetLabels()[label.TopologyNetwork.Name]),
			Cluster: n.clusterID,
			ServiceAccount: types.NamespacedName{
				Namespace: gw.Namespace,
				Name:      kube.GatewaySA(gw),
			},
		}
		gateways := []model.NetworkGateway{}
		for _, addr := range gw.Spec.Addresses {
			if addr.Type == nil {
				continue
			}
			if addrType := *addr.Type; addrType != v1beta1.IPAddressType && addrType != v1beta1.HostnameAddressType {
				continue
			}
			for _, l := range slices.Filter(gw.Spec.Listeners, autoPassthrough) {
				networkGateway := base
				networkGateway.Addr = addr.Value
				networkGateway.Port = uint32(l.Port)
				gateways = append(gateways, networkGateway)
			}
			for _, l := range gw.Spec.Listeners {
				if l.Protocol == "HBONE" {
					networkGateway := base
					networkGateway.Addr = addr.Value
					networkGateway.Port = uint32(l.Port)
					networkGateway.HBONEPort = uint32(l.Port)
					gateways = append(gateways, networkGateway)
				}
			}
		}
		return gateways
	}
}

func (n *networkManager) Network(endpointIP string, labels labels.Instance) network.ID {
	// 1. check the pod/workloadEntry label
	if nw := labels[label.TopologyNetwork.Name]; nw != "" {
		return network.ID(nw)
	}
	if nw := n.SystemNamespaceNetwork.Get(); nw != nil {
		return network.ID(*nw)
	}
	if nw := n.networkFromMeshNetworks(endpointIP); nw != "" {
		return nw
	}
	return ""
}

func (n *networkManager) FetchNetwork(ctx krt.HandlerContext, endpointIP string, labels labels.Instance) network.ID {
	// 1. check the pod/workloadEntry label
	if nw := labels[label.TopologyNetwork.Name]; nw != "" {
		return network.ID(nw)
	}

	if nw := krt.FetchOne(ctx, n.SystemNamespaceNetwork.AsCollection()); nw != nil {
		return network.ID(*nw)
	}
	if nw := n.networkFromMeshNetworks(endpointIP); nw != "" {
		// Mesh networks is out of scope of our dependency tracking, so any change to mesh networks we recompute.
		n.RecomputeTrigger.MarkDependant(ctx)
		return nw
	}
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
	return n.SystemNamespaceNetwork.AsCollection().Synced().HasSynced() &&
		n.NetworkGateways.Synced().HasSynced() &&
		n.ParsedMeshNetworks.AsCollection().Synced().HasSynced()
}

func (n *networkManager) networkFromMeshNetworks(endpointIP string) network.ID {
	mn := n.ParsedMeshNetworks.Get()
	if mn == nil {
		return ""
	}
	if mn.network != "" {
		return mn.network
	}

	if mn.ranger != nil {
		ip := net.ParseIP(endpointIP)
		if ip == nil {
			return ""
		}
		entries, err := mn.ranger.ContainingNetworks(ip)
		if err != nil {
			log.Errorf("error getting cidr ranger entry from endpoint ip %s", endpointIP)
			return ""
		}
		if len(entries) > 1 {
			log.Warnf("Found multiple networks CIDRs matching the endpoint IP: %s. Using the first match.", endpointIP)
		}
		if len(entries) > 0 {
			return (entries[0].(namedRangerEntry)).name
		}
	}
	return ""
}
