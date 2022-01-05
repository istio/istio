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
	"reflect"
	"sort"
	"strings"
	"sync"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/sets"
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

type NetworkGatewaysWatcher interface {
	NetworkGateways() []NetworkGateway
	AppendNetworkGatewayHandler(h func())
}

// NetworkGatewaysHandler can be embedded to easily implement NetworkGatewaysWatcher.
type NetworkGatewaysHandler struct {
	handlers []func()
}

func (ngh *NetworkGatewaysHandler) AppendNetworkGatewayHandler(h func()) {
	ngh.handlers = append(ngh.handlers, h)
}

func (ngh *NetworkGatewaysHandler) NotifyGatewayHandlers() {
	for _, handler := range ngh.handlers {
		handler()
	}
}

// NewNetworkManager creates a new NetworkManager from the Environment by merging
// together the MeshNetworks and ServiceRegistry-specific gateways.
func NewNetworkManager(env *Environment, xdsUpdater XDSUpdater) *NetworkManager {
	mgr := &NetworkManager{env: env, xdsUpdater: xdsUpdater}
	env.AddNetworksHandler(mgr.reloadAndPush)
	env.AppendNetworkGatewayHandler(mgr.reloadAndPush)
	mgr.reload()
	return mgr
}

func (mgr *NetworkManager) reloadAndPush() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	oldGateways := make(NetworkGatewaySet)
	for _, gateway := range mgr.allGateways() {
		oldGateways.Add(gateway)
	}
	changed := !mgr.reload().Equals(oldGateways)

	if changed && mgr.xdsUpdater != nil {
		log.Infof("gateways changed, triggering push")
		mgr.xdsUpdater.ConfigUpdate(&PushRequest{Full: true, Reason: []TriggerReason{NetworksTrigger}})
	}
}

func (mgr *NetworkManager) reload() NetworkGatewaySet {
	log.Infof("reloading network gateways")

	// Generate a snapshot of the state of gateways by merging the contents of
	// MeshNetworks and the ServiceRegistries.

	// Store all gateways in a set initially to eliminate duplicates.
	gatewaySet := make(NetworkGatewaySet)

	// First, load gateways from the static MeshNetworks config.
	meshNetworks := mgr.env.Networks()
	if meshNetworks != nil {
		for nw, networkConf := range meshNetworks.Networks {
			for _, gw := range networkConf.Gateways {
				if gw.GetAddress() == "" {
					// registryServiceName addresses will be populated via kube service registry
					continue
				}
				gatewaySet[NetworkGateway{
					Cluster: "", /* TODO(nmittler): Add Cluster to the API */
					Network: network.ID(nw),
					Addr:    gw.GetAddress(),
					Port:    gw.Port,
				}] = struct{}{}
			}
		}
	}

	// Second, load registry-specific gateways.
	for _, gw := range mgr.env.NetworkGateways() {
		// - the internal map of label gateways - these get deleted if the service is deleted, updated if the ip changes etc.
		// - the computed map from meshNetworks (triggered by reloadNetworkLookup, the ported logic from getGatewayAddresses)
		gatewaySet[gw] = struct{}{}
	}

	mgr.resolveHostnameGateways(gatewaySet)

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

	mgr.lcm = uint32(lcmVal)
	mgr.byNetwork = byNetwork
	mgr.byNetworkAndCluster = byNetworkAndCluster

	return gatewaySet
}

func (mgr *NetworkManager) resolveHostnameGateways(gatewaySet map[NetworkGateway]struct{}) {
	// filter the list of gateways to resolve
	hostnameGateways := map[string][]NetworkGateway{}
	names := sets.NewSet()
	for gw := range gatewaySet {
		if gwIP := net.ParseIP(gw.Addr); gwIP != nil {
			continue
		}
		delete(gatewaySet, gw)
		if !features.ResolveHostnameGateways {
			log.Warnf("Failed parsing gateway address %s from Service Registry. "+
				"Set RESOLVE_HOSTNAME_GATEWAYS on istiod to enable resolving hostnames in the control plane.",
				gw.Addr)
			continue
		}
		hostnameGateways[gw.Addr] = append(hostnameGateways[gw.Addr], gw)
		names.Insert(gw.Addr)
	}

	// resolve each hostname TODO a future change implements more robust resolution, with support for TTL based refresh
	for host, gwsForHost := range hostnameGateways {
		resolved, err := net.ResolveIPAddr("ip4", host)
		if err != nil {
			log.Warnf("could not resolve hostname %q for %d gateways: %v", host, len(gwsForHost), err)
		}
		// expand each resolved address into a NetworkGateway
		for _, gw := range gwsForHost {
			// copy the base gateway to preserve the port/network, but update with the resolved IP
			resolvedGw := gw
			resolvedGw.Addr = resolved.IP.String()
			gatewaySet[resolvedGw] = struct{}{}
		}
	}
}

// NetworkManager provides gateway details for accessing remote networks.
type NetworkManager struct {
	env *Environment
	// exported for test
	xdsUpdater XDSUpdater

	// least common multiple of gateway number of {per network, per cluster}
	mu                  sync.RWMutex
	lcm                 uint32
	byNetwork           map[network.ID][]NetworkGateway
	byNetworkAndCluster map[networkAndCluster][]NetworkGateway
}

func (mgr *NetworkManager) IsMultiNetworkEnabled() bool {
	if mgr == nil {
		return false
	}
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return len(mgr.byNetwork) > 0
}

// GetLBWeightScaleFactor returns the least common multiple of the number of gateways per network.
func (mgr *NetworkManager) GetLBWeightScaleFactor() uint32 {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return mgr.lcm
}

func (mgr *NetworkManager) AllGateways() []NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return mgr.allGateways()
}

func (mgr *NetworkManager) allGateways() []NetworkGateway {
	if mgr.byNetwork == nil {
		return nil
	}
	out := make([]NetworkGateway, 0)
	for _, gateways := range mgr.byNetwork {
		out = append(out, gateways...)
	}
	return SortGateways(out)
}

func (mgr *NetworkManager) GatewaysByNetwork() map[network.ID][]NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	if mgr.byNetwork == nil {
		return nil
	}
	out := make(map[network.ID][]NetworkGateway)
	for k, v := range mgr.byNetwork {
		out[k] = append(make([]NetworkGateway, 0, len(v)), v...)
	}
	return out
}

func (mgr *NetworkManager) GatewaysForNetwork(nw network.ID) []NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	if mgr.byNetwork == nil {
		return nil
	}
	return mgr.byNetwork[nw]
}

func (mgr *NetworkManager) GatewaysForNetworkAndCluster(nw network.ID, c cluster.ID) []NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	if mgr.byNetwork == nil {
		return nil
	}
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

// NetworkGatewaySet is a helper to manage a set of NetworkGateway instances.
type NetworkGatewaySet map[NetworkGateway]struct{}

func (s NetworkGatewaySet) Equals(other NetworkGatewaySet) bool {
	if len(s) != len(other) {
		return false
	}
	// deepequal won't catch nil-map == empty map
	if len(s) == 0 && len(other) == 0 {
		return true
	}
	return reflect.DeepEqual(s, other)
}

func (s NetworkGatewaySet) Add(gw NetworkGateway) {
	s[gw] = struct{}{}
}

func (s NetworkGatewaySet) AddAll(other NetworkGatewaySet) {
	for gw := range other {
		s.Add(gw)
	}
}

func (s NetworkGatewaySet) ToArray() []NetworkGateway {
	gws := make([]NetworkGateway, 0, len(s))
	for gw := range s {
		gws = append(gws, gw)
	}

	// Sort the array so that it's stable.
	gws = SortGateways(gws)
	return gws
}
