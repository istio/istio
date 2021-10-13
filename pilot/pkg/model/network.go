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
	"sort"
	"strings"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/network"
)

// NetworkGateway is the gateway of a network
type NetworkGateway struct {
	// Network is the ID of the network where this Gateway resides.
	Network network.ID
	// Cluster is the ID of the k8s cluster where this Gateway resides.
	Cluster cluster.ID
	// gateway ip address
	Addr string
	// gateway port
	Port uint32
}

// NewNetworkManager creates a new NetworkManager from the Environment by merging
// together the MeshNetworks and ServiceRegistry-specific gateways.
func NewNetworkManager(env *Environment) *NetworkManager {
	// Generate the a snapshot of the state of gateways by merging the contents of
	// MeshNetworks and the ServiceRegistries.

	// Store all gateways in a set initially to eliminate duplicates.
	gatewaySet := make(map[NetworkGateway]struct{})

	// First, load gateways from the static MeshNetworks config.
	meshNetworks := env.Networks()
	if meshNetworks != nil {
		for nw, networkConf := range meshNetworks.Networks {
			gws := networkConf.Gateways
			for _, gw := range gws {
				if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
					gatewaySet[NetworkGateway{
						Cluster: "", /* TODO(nmittler): Add Cluster to the API */
						Network: network.ID(nw),
						Addr:    gw.GetAddress(),
						Port:    gw.Port,
					}] = struct{}{}
				} else {
					log.Warnf("Failed parsing gateway address %s in MeshNetworks config. "+
						"Hostnames are not supported for gateways",
						gw.GetAddress())
				}
			}
		}
	}

	// Second, load registry-specific gateways.
	for _, gw := range env.NetworkGateways() {
		if gwIP := net.ParseIP(gw.Addr); gwIP != nil {
			// - the internal map of label gateways - these get deleted if the service is deleted, updated if the ip changes etc.
			// - the computed map from meshNetworks (triggered by reloadNetworkLookup, the ported logic from getGatewayAddresses)
			gatewaySet[gw] = struct{}{}
		} else {
			log.Warnf("Failed parsing gateway address %s from Service Registry. "+
				"Hostnames are not supported for gateways",
				gw.Addr)
		}
	}

	// Now populate the maps by network and by network+cluster.
	byNetwork := make(map[network.ID][]NetworkGateway)
	byNetworkAndCluster := make(map[networkAndCluster][]NetworkGateway)
	for gw := range gatewaySet {
		byNetwork[gw.Network] = append(byNetwork[gw.Network], gw)
		nc := networkAndClusterForGateway(&gw)
		byNetworkAndCluster[nc] = append(byNetworkAndCluster[nc], gw)
	}

	gwNum := []int{}
	// Sort the gateways in byNetwork, and also calculate the max number
	// of gateways per network.
	for k, gws := range byNetwork {
		byNetwork[k] = SortGateways(gws)
		gwNum = append(gwNum, len(gws))
	}

	// Sort the gateways in byNetworkAndCluster.
	for k, gws := range byNetworkAndCluster {
		byNetworkAndCluster[k] = SortGateways(gws)
		gwNum = append(gwNum, len(gws))
	}

	lcmVal := 1
	// calculate lcm
	for _, num := range gwNum {
		lcmVal = lcm(lcmVal, num)
	}

	return &NetworkManager{
		lcm:                 uint32(lcmVal),
		byNetwork:           byNetwork,
		byNetworkAndCluster: byNetworkAndCluster,
	}
}

// NetworkManager provides gateway details for accessing remote networks.
type NetworkManager struct {
	// least common multiple of gateway number of {per network, per cluster}
	lcm                 uint32
	byNetwork           map[network.ID][]NetworkGateway
	byNetworkAndCluster map[networkAndCluster][]NetworkGateway
}

func (mgr *NetworkManager) IsMultiNetworkEnabled() bool {
	return len(mgr.byNetwork) > 0
}

// GetLCM returns the least common multiple of the number of gateways per network.
func (mgr *NetworkManager) GetLCM() uint32 {
	return mgr.lcm
}

func (mgr *NetworkManager) AllGateways() []NetworkGateway {
	out := make([]NetworkGateway, 0)
	for _, gateways := range mgr.byNetwork {
		out = append(out, gateways...)
	}

	return SortGateways(out)
}

func (mgr *NetworkManager) GatewaysByNetwork() map[network.ID][]NetworkGateway {
	out := make(map[network.ID][]NetworkGateway)
	for k, v := range mgr.byNetwork {
		out[k] = append(make([]NetworkGateway, 0, len(v)), v...)
	}
	return out
}

func (mgr *NetworkManager) GatewaysForNetwork(nw network.ID) []NetworkGateway {
	return mgr.byNetwork[nw]
}

func (mgr *NetworkManager) GatewaysForNetworkAndCluster(nw network.ID, c cluster.ID) []NetworkGateway {
	return mgr.byNetworkAndCluster[networkAndClusterFor(nw, c)]
}

type networkAndCluster struct {
	network network.ID
	cluster cluster.ID
}

func networkAndClusterForGateway(g *NetworkGateway) networkAndCluster {
	return networkAndClusterFor(g.Network, g.Cluster)
}

func networkAndClusterFor(nw network.ID, c cluster.ID) networkAndCluster {
	return networkAndCluster{
		network: nw,
		cluster: c,
	}
}

func SortGateways(gws []NetworkGateway) []NetworkGateway {
	// Sort the array so that it's stable.
	sort.SliceStable(gws, func(i, j int) bool {
		if cmp := strings.Compare(gws[i].Addr, gws[j].Addr); cmp < 0 {
			return true
		}
		return gws[i].Port < gws[j].Port
	})
	return gws
}

// greatest common divisor of x and y
func gcd(x, y int) int {
	var tmp int
	for {
		tmp = x % y
		if tmp > 0 {
			x = y
			y = tmp
		} else {
			return y
		}
	}
}

// least common multiple of x and y
func lcm(x, y int) int {
	return x * y / gcd(x, y)
}
