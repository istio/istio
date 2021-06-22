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

package model

import (
	"net"
)

const (
	noCluster = ""
)

// NetworkID is the unique identifier for a network.
type NetworkID string

func (id NetworkID) Equals(other NetworkID) bool {
	return SameOrEmpty(string(id), string(other))
}

// ClusterID is the unique identifier for a k8s cluster.
type ClusterID string

func (id ClusterID) Equals(other ClusterID) bool {
	return SameOrEmpty(string(id), string(other))
}

// Gateway is the gateway of a network
type Gateway struct {
	// Network is the ID of the network where this Gateway resides.
	Network NetworkID
	// Cluster is the ID of the k8s cluster where this Gateway resides.
	Cluster ClusterID
	// gateway ip address
	Addr string
	// gateway port
	Port uint32
}

// Gateways provides information about network Gateway resources throughout the mesh.
type Gateways interface {
	// IsMultiNetworkEnabled returns true if one or more Gateways are configured.
	IsMultiNetworkEnabled() bool

	// All returns all configured Gateway resources in the entire mesh.
	All() []*Gateway

	// ForNetwork returns all Gateway resources configured for the given network.
	ForNetwork(network NetworkID) []*Gateway

	// ForNetworkAndCluster returns all Gateway resources configured for the given network and cluster.
	ForNetworkAndCluster(network NetworkID, cluster ClusterID) []*Gateway
}

// newGateways creates a new Gateways from the Environment by merging
// together the MeshNetworks and ServiceRegistry-specific gateways.
func newGateways(env *Environment) Gateways {
	// Generate the a snapshot of the state of gateways by merging the contents of
	// MeshNetworks and the ServiceRegistries.
	gatewayMap := make(map[NetworkID]map[ClusterID][]*Gateway)

	addGateway := func(gateway *Gateway) {
		// Get (or create) an entry for the network.
		gatewaysByCluster := gatewayMap[gateway.Network]
		if gatewaysByCluster == nil {
			gatewaysByCluster = make(map[ClusterID][]*Gateway)
			gatewayMap[gateway.Network] = gatewaysByCluster
		}

		gatewaysByCluster[gateway.Cluster] = append(gatewaysByCluster[gateway.Cluster], gateway)
		if gateway.Cluster != noCluster {
			// Also make sure this gateway appears in the global list of all gateways for the network.
			gatewaysByCluster[noCluster] = append(gatewaysByCluster[noCluster], gateway)
		}
	}

	// First, load gateways from the static MeshNetworks config.
	meshNetworks := env.Networks()
	if meshNetworks != nil {
		for network, networkConf := range meshNetworks.Networks {
			gws := networkConf.Gateways
			for _, gw := range gws {
				if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
					addGateway(&Gateway{
						Cluster: noCluster, /* TODO(nmittler): Add Cluster to the API */
						Network: NetworkID(network),
						Addr:    gw.GetAddress(),
						Port:    gw.Port,
					})
				}
			}
		}
	}

	// Second, load registry-specific gateways.
	for _, gateway := range env.NetworkGateways() {
		// - the internal map of label gateways - these get deleted if the service is deleted, updated if the ip changes etc.
		// - the computed map from meshNetworks (triggered by reloadNetworkLookup, the ported logic from getGatewayAddresses)
		addGateway(gateway)
	}

	return &gatewaysImpl{
		gateways: gatewayMap,
	}
}

type gatewaysImpl struct {
	gateways map[NetworkID]map[ClusterID][]*Gateway
}

func (gws *gatewaysImpl) IsMultiNetworkEnabled() bool {
	return len(gws.gateways) > 0
}

func (gws *gatewaysImpl) All() []*Gateway {
	out := make([]*Gateway, 0)
	for _, byCluster := range gws.gateways {
		out = append(out, byCluster[noCluster]...)
	}
	return out
}

func (gws *gatewaysImpl) ForNetwork(network NetworkID) []*Gateway {
	return gws.ForNetworkAndCluster(network, noCluster)
}

func (gws *gatewaysImpl) ForNetworkAndCluster(network NetworkID, cluster ClusterID) []*Gateway {
	gatewaysByCluster := gws.gateways[network]
	if gatewaysByCluster != nil {
		return gatewaysByCluster[cluster]
	}
	return nil
}
