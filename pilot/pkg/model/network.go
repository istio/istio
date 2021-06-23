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

// NetworkGateway is the gateway of a network
type NetworkGateway struct {
	// Network is the ID of the network where this Gateway resides.
	Network NetworkID
	// Cluster is the ID of the k8s cluster where this Gateway resides.
	Cluster ClusterID
	// gateway ip address
	Addr string
	// gateway port
	Port uint32
}

// newNetworkManager creates a new NetworkManager from the Environment by merging
// together the MeshNetworks and ServiceRegistry-specific gateways.
func newNetworkManager(env *Environment) *NetworkManager {
	// Generate the a snapshot of the state of gateways by merging the contents of
	// MeshNetworks and the ServiceRegistries.
	byNetwork := make(map[NetworkID][]*NetworkGateway)
	byNetworkAndCluster := make(map[networkAndCluster][]*NetworkGateway)

	addGateway := func(gateway *NetworkGateway) {
		byNetwork[gateway.Network] = append(byNetwork[gateway.Network], gateway)
		nc := networkAndClusterFor(gateway)
		byNetworkAndCluster[nc] = append(byNetworkAndCluster[nc], gateway)
	}

	// First, load gateways from the static MeshNetworks config.
	meshNetworks := env.Networks()
	if meshNetworks != nil {
		for network, networkConf := range meshNetworks.Networks {
			gws := networkConf.Gateways
			for _, gw := range gws {
				if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
					addGateway(&NetworkGateway{
						Cluster: "", /* TODO(nmittler): Add Cluster to the API */
						Network: NetworkID(network),
						Addr:    gw.GetAddress(),
						Port:    gw.Port,
					})
				} else {
					log.Warnf("Failed parsing gateway address %s in MeshNetworks config. "+
						"Hostnames are not supported for gateways",
						gw.GetAddress())
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

	return &NetworkManager{
		byNetwork:           byNetwork,
		byNetworkAndCluster: byNetworkAndCluster,
	}
}

// NetworkManager provides gateway details for accessing remote networks.
type NetworkManager struct {
	byNetwork           map[NetworkID][]*NetworkGateway
	byNetworkAndCluster map[networkAndCluster][]*NetworkGateway
}

type networkAndCluster struct {
	network NetworkID
	cluster ClusterID
}

func networkAndClusterFor(g *NetworkGateway) networkAndCluster {
	return networkAndCluster{
		network: g.Network,
		cluster: g.Cluster,
	}
}

func (mgr *NetworkManager) IsMultiNetworkEnabled() bool {
	return len(mgr.byNetwork) > 0
}

func (mgr *NetworkManager) AllGateways() []*NetworkGateway {
	out := make([]*NetworkGateway, 0)
	for _, gateways := range mgr.byNetwork {
		out = append(out, gateways...)
	}
	return out
}

func (mgr *NetworkManager) GatewaysForNetwork(network NetworkID) []*NetworkGateway {
	return mgr.byNetwork[network]
}

func (mgr *NetworkManager) GatewaysForNetworkAndCluster(network NetworkID, cluster ClusterID) []*NetworkGateway {
	return mgr.byNetworkAndCluster[networkAndCluster{
		network: network,
		cluster: cluster,
	}]
}
