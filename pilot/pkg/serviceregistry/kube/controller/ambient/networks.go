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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/krt"
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

func (c networkCollections) HasSynced() bool {
	return c.SystemNamespace.AsCollection().HasSynced() &&
		c.NetworkGateways.HasSynced()
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
		fromGatewayBuilder(options.ClusterID),
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

func fromGatewayBuilder(clusterID cluster.ID) krt.TransformationMulti[*v1beta1.Gateway, NetworkGateway] {
	return func(ctx krt.HandlerContext, gw *v1beta1.Gateway) []NetworkGateway {
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
}
