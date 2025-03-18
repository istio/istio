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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
)

type NetworkGateway struct {
	model.NetworkGateway
	Source types.NamespacedName
}

func (n NetworkGateway) ResourceName() string {
	return n.Source.String() + "/" + n.Addr
}

type networkCollections struct {
	SystemNamespace   krt.Singleton[string]
	NetworkGateways   krt.Collection[NetworkGateway]
	GatewaysByNetwork krt.Index[network.ID, NetworkGateway]
}

type globalNetworkCollections struct {
	// SystemNamespaceNetworkByCluster is an index of cluster ID to the system namespace network
	// for that cluster.
	SystemNamespaceNetworkByCluster krt.Index[cluster.ID, krt.Singleton[string]]
	// NetworkGateways contains all the network gateways we know of in the mesh
	NetworkGateways krt.Collection[NetworkGateway]
	// GatewaysByNetwork is a map of network ID to all the gateways in that network
	GatewaysByNetwork krt.Index[network.ID, NetworkGateway]
}

func (c networkCollections) HasSynced() bool {
	return c.SystemNamespace.AsCollection().HasSynced() &&
		c.NetworkGateways.HasSynced()
}

func buildGlobalNetworkCollections(
	clusters krt.Collection[*Cluster],
	namespacesByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[*v1.Namespace]]],
	gatewaysByCluster krt.Index[cluster.ID, krt.Collection[config.ObjectWithCluster[*v1beta1.Gateway]]],
	options Options,
	opts krt.OptionsBuilder,
) globalNetworkCollections {
	SystemNamespaceNetwork := krt.NewCollection(
		clusters,
		func(ctx krt.HandlerContext, c *Cluster) *krt.Singleton[string] {
			namespacesCollection := namespacesByCluster.Lookup(c.ID)
			if len(namespacesCollection) < 1 {
				return nil
			}
			namespaces := namespacesCollection[0]
			nsWrapper := krt.FetchOne(ctx, namespaces, krt.FilterKey(options.SystemNamespace))
			if nsWrapper == nil {
				return nil
			}
			ns := ptr.Flatten(nsWrapper.Object)
			nw, f := ns.Labels[label.TopologyNetwork.Name]
			if !f {
				return nil
			}
			s := krt.NewSingleton(func(ctx krt.HandlerContext) *string {
				return &nw
			}, opts.With(
				krt.WithName(fmt.Sprintf("SystemNamespaceNetwork[%s]", c.ID)),
				krt.WithMetadata(krt.Metadata{
					ClusterKRTMetadataKey: c.ID,
				}),
			)...)

			return &s
		},
	)

	SystemNamespaceNetworkByCluster := krt.NewIndex(SystemNamespaceNetwork, func(o krt.Singleton[string]) []cluster.ID {
		val, ok := o.Metadata()[ClusterKRTMetadataKey]
		if !ok {
			log.Warnf("Cluster metadata not set on collection %v", o)
			return nil
		}
		id, ok := val.(cluster.ID)
		if !ok {
			log.Warnf("Invalid cluster metadata set on collection %v: %v", o, val)
			return nil
		}
		return []cluster.ID{id}
	})

	GlobalNetworkGateways := krt.NewCollection(
		clusters,
		func(ctx krt.HandlerContext, c *Cluster) *krt.Collection[config.ObjectWithCluster[NetworkGateway]] {
			gatewaysCollection := gatewaysByCluster.Lookup(c.ID)
			if gatewaysCollection == nil || len(gatewaysCollection) < 1 {
				return nil
			}
			gateways := gatewaysCollection[0]
			networkGateways := krt.NewManyCollection(
				gateways,
				func(ctx krt.HandlerContext, gw config.ObjectWithCluster[*v1beta1.Gateway]) []config.ObjectWithCluster[NetworkGateway] {
					innerGw := ptr.Flatten(gw.Object)
					if innerGw == nil {
						return nil
					}
					return k8sGatewayWithClusterToNetworkGateways(c.ID, innerGw, options.ClusterID)
				},
				opts.WithName(fmt.Sprintf("NetworkGateways[%s]", c.ID))...,
			)
			return &networkGateways
		},
	)
	MergedNetworkGatewaysWithCluster := krt.NestedJoinWithMergeCollection(
		GlobalNetworkGateways,
		func(gateways []config.ObjectWithCluster[NetworkGateway]) *config.ObjectWithCluster[NetworkGateway] {
			if len(gateways) == 0 {
				return nil
			}
			for _, gw := range gateways {
				if gw.Object == nil {
					continue
				}
				if gw.ClusterID == options.ClusterID {
					// If we have a duplicate resource name and address (which forms the key),
					// we want to use the one from the local cluster.
					return &gw
				}
			}

			// No local cluster found, return the first one
			return &gateways[0]
		},
		opts.WithName("MergedGlobalNetworkGatewaysWithCluster")...,
	)

	MergedNetworkGateways := krt.MapCollection(MergedNetworkGatewaysWithCluster, unwrapObjectWithCluster, opts.WithName("MergedGlobalNetworkGateways")...)

	GatewaysByNetwork := krt.NewIndex(MergedNetworkGateways, func(o NetworkGateway) []network.ID {
		return []network.ID{o.Network}
	})

	return globalNetworkCollections{
		SystemNamespaceNetworkByCluster: SystemNamespaceNetworkByCluster,
		NetworkGateways:                 MergedNetworkGateways,
		GatewaysByNetwork:               GatewaysByNetwork,
	}
}

func buildNetworkCollections(
	namespaces krt.Collection[*v1.Namespace],
	gateways krt.Collection[*v1beta1.Gateway],
	options Options,
	opts krt.OptionsBuilder,
) networkCollections {
	SystemNamespaceNetwork := krt.NewSingleton(func(ctx krt.HandlerContext) *string {
		ns := ptr.Flatten(krt.FetchOne(ctx, namespaces, krt.FilterKey(options.SystemNamespace)))
		if ns == nil {
			return nil
		}
		nw, f := ns.Labels[label.TopologyNetwork.Name]
		if !f {
			return nil
		}
		return &nw
	}, opts.WithName("SystemNamespaceNetwork")...)
	NetworkGateways := krt.NewManyCollection(
		gateways,
		fromGatewayBuilder(options.ClusterID, options.ClusterID),
		opts.WithName("NetworkGateways")...,
	)
	GatewaysByNetwork := krt.NewIndex(NetworkGateways, "network", func(o NetworkGateway) []network.ID {
		return []network.ID{o.Network}
	})

	return networkCollections{
		SystemNamespace:   SystemNamespaceNetwork,
		NetworkGateways:   NetworkGateways,
		GatewaysByNetwork: GatewaysByNetwork,
	}
}

func k8sGatewayWithClusterToNetworkGateways(
	clusterID cluster.ID,
	gw *v1beta1.Gateway,
	localClusterID cluster.ID,
) []config.ObjectWithCluster[NetworkGateway] {
	gateways := k8sGatewayToNetworkGateways(clusterID, gw, localClusterID)
	var networkGateways []config.ObjectWithCluster[NetworkGateway]
	for _, gateway := range gateways {
		networkGateways = append(networkGateways, config.ObjectWithCluster[NetworkGateway]{
			ClusterID: clusterID,
			Object:    &gateway,
		})
	}

	return networkGateways
}

func remoteK8sGatewayToNetworkGateways(clusterID cluster.ID, gw *v1beta1.Gateway) []NetworkGateway {
	netLabel := gw.GetLabels()[label.TopologyNetwork.Name]
	if netLabel == "" {
		return nil
	}

	// This is a remote east/west gateway, so check that the gateway class is correct
	if gw.Spec.GatewayClassName != constants.EastWestGatewayClassName {
		return nil
	}

	base := model.NetworkGateway{
		Network: network.ID(netLabel),
		Cluster: clusterID,
		ServiceAccount: types.NamespacedName{
			Namespace: gw.Namespace,
			Name:      kube.GatewaySA(gw),
		},
	}
	gateways := []NetworkGateway{}
	source := config.NamespacedName(gw)
	for _, addr := range gw.Spec.Addresses {
		if addr.Type == nil {
			continue
		}
		if addrType := *addr.Type; addrType != v1beta1.IPAddressType && addrType != v1beta1.HostnameAddressType {
			continue
		}
		for _, l := range gw.Spec.Listeners {
			tlsModeTerminate := l.TLS != nil && l.TLS.Mode != nil && *l.TLS.Mode == gatewayv1.TLSModeTerminate
			if l.Protocol == "HBONE" && tlsModeTerminate {
				networkGateway := base
				networkGateway.Addr = addr.Value
				gateways = append(gateways, NetworkGateway{
					NetworkGateway: networkGateway,
					Source:         source,
				})
				break
			}
		}
	}
	return gateways
}

func k8sGatewayToNetworkGateways(clusterID cluster.ID, gw *v1beta1.Gateway, localClusterID cluster.ID) []NetworkGateway {
	if clusterID != localClusterID {
		// This is a gateway in a remote cluster, use differnet logic
		return remoteK8sGatewayToNetworkGateways(clusterID, gw)
	}

	netLabel := gw.GetLabels()[label.TopologyNetwork.Name]
	if netLabel == "" {
		return nil
	}

	if gw.Spec.GatewayClassName != constants.RemoteGatewayClassName {
		return nil
	}

	base := model.NetworkGateway{
		Network: network.ID(netLabel),
		Cluster: clusterID,
		ServiceAccount: types.NamespacedName{
			Namespace: gw.Namespace,
			Name:      kube.GatewaySA(gw),
		},
	}
	gateways := []NetworkGateway{}
	source := config.NamespacedName(gw)
	for _, addr := range gw.Spec.Addresses {
		if addr.Type == nil {
			continue
		}
		if addrType := *addr.Type; addrType != v1beta1.IPAddressType && addrType != v1beta1.HostnameAddressType {
			continue
		}
		for _, l := range gw.Spec.Listeners {
			if l.Protocol == "HBONE" {
				networkGateway := base
				networkGateway.Addr = addr.Value
				networkGateway.Port = uint32(l.Port)
				networkGateway.HBONEPort = uint32(l.Port)
				gateways = append(gateways, NetworkGateway{
					NetworkGateway: networkGateway,
					Source:         source,
				})
				break
			}
		}
	}
	return gateways
}

func fromGatewayBuilder(clusterID, localClusterID cluster.ID) krt.TransformationMulti[*v1beta1.Gateway, NetworkGateway] {
	return func(ctx krt.HandlerContext, gw *v1beta1.Gateway) []NetworkGateway {
		return k8sGatewayToNetworkGateways(clusterID, gw, localClusterID)
	}
}
