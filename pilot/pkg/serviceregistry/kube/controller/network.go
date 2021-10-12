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
	"net"
	"reflect"
	"strconv"

	"github.com/yl2chen/cidranger"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/network"
)

// namedRangerEntry for holding network's CIDR and name
type namedRangerEntry struct {
	name    network.ID
	network net.IPNet
}

// Network returns the IPNet for the network
func (n namedRangerEntry) Network() net.IPNet {
	return n.network
}

func (c *Controller) onNetworkChanged() {
	// the network for endpoints are computed when we process the events; this will fix the cache
	// NOTE: this must run before the other network watcher handler that creates a force push
	if err := c.syncPods(); err != nil {
		log.Errorf("one or more errors force-syncing pods: %v", err)
	}
	if err := c.syncEndpoints(); err != nil {
		log.Errorf("one or more errors force-syncing endpoints: %v", err)
	}
	c.reloadNetworkGateways()
}

// reloadNetworkLookup refreshes the meshNetworks configuration, network for each endpoint, and
// recomputes network gateways.
func (c *Controller) reloadNetworkLookup() {
	c.reloadMeshNetworks()
	c.onNetworkChanged()
}

// reloadMeshNetworks will read the mesh networks configuration to setup
// fromRegistry and cidr based network lookups for this registry
func (c *Controller) reloadMeshNetworks() {
	c.Lock()
	defer c.Unlock()
	c.networkForRegistry = ""
	ranger := cidranger.NewPCTrieRanger()

	c.networkForRegistry = ""
	c.registryServiceNameGateways = map[host.Name]uint32{}

	meshNetworks := c.opts.NetworksWatcher.Networks()
	if meshNetworks == nil || len(meshNetworks.Networks) == 0 {
		return
	}
	for n, v := range meshNetworks.Networks {
		// track endpoints items from this registry are a part of this network
		for _, ep := range v.Endpoints {
			if ep.GetFromCidr() != "" {
				_, nw, err := net.ParseCIDR(ep.GetFromCidr())
				if err != nil {
					log.Warnf("unable to parse CIDR %q for network %s", ep.GetFromCidr(), n)
					continue
				}
				rangerEntry := namedRangerEntry{
					name:    network.ID(n),
					network: *nw,
				}
				_ = ranger.Insert(rangerEntry)
			}
			if ep.GetFromRegistry() != "" && cluster.ID(ep.GetFromRegistry()) == c.Cluster() {
				if c.networkForRegistry != "" {
					log.Warnf("multiple networks specify %s in fromRegistry, only first network %s will use %s",
						c.Cluster(), c.networkForRegistry, c.Cluster())
				} else {
					c.networkForRegistry = network.ID(n)
				}
			}
		}

		// track which services from this registry act as gateways for what networks
		if c.networkForRegistry == network.ID(n) {
			for _, gw := range v.Gateways {
				if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
					c.registryServiceNameGateways[host.Name(gwSvcName)] = gw.Port
				}
			}
		}

	}
	c.ranger = ranger
}

func (c *Controller) NetworkGateways() []model.NetworkGateway {
	c.RLock()
	defer c.RUnlock()

	if c.networkGateways == nil || len(c.networkGateways) == 0 {
		return nil
	}

	// Merge all the gateways into a single set to eliminate duplicates.
	out := make(gatewaySet)
	for _, byNetwork := range c.networkGateways {
		for _, gateways := range byNetwork {
			out.addAll(gateways)
		}
	}

	return out.toArray()
}

// extractGatewaysFromService checks if the service is a cross-network gateway
// and if it is, updates the controller's gateways.
func (c *Controller) extractGatewaysFromService(svc *model.Service) bool {
	c.Lock()
	defer c.Unlock()
	return c.extractGatewaysInner(svc)
}

// reloadNetworkGateways performs extractGatewaysFromService for all services registered with the controller.
func (c *Controller) reloadNetworkGateways() {
	c.Lock()
	defer c.Unlock()
	gwsChanged := false
	for _, svc := range c.servicesMap {
		if c.extractGatewaysInner(svc) {
			gwsChanged = true
			break
		}
	}
	if gwsChanged {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.NetworksTrigger}})
	}
}

// extractGatewaysInner performs the logic for extractGatewaysFromService without locking the controller.
// Returns true if any gateways changed.
func (c *Controller) extractGatewaysInner(svc *model.Service) bool {
	gwPort, nw := c.getGatewayDetails(svc)
	if gwPort == 0 || nw == "" {
		// TODO detect if this previously had the gateway label so we can cleanup the old value
		// not a gateway
		return false
	}

	if c.networkGateways[svc.Hostname] == nil {
		c.networkGateways[svc.Hostname] = make(map[network.ID]gatewaySet)
	}
	// Create the entry for this network, if doesn't exist.
	if c.networkGateways[svc.Hostname][nw] == nil {
		c.networkGateways[svc.Hostname][nw] = make(gatewaySet)
	}

	newGateways := make(gatewaySet)

	// TODO(landow) ClusterExternalAddresses doesn't need to get used outside of the kube controller, and spreads
	// TODO(cont)   logic between ConvertService, extractGatewaysInner, and updateServiceNodePortAddresses.
	if !svc.Attributes.ClusterExternalAddresses.IsEmpty() {
		// check if we have node port mappings
		if svc.Attributes.ClusterExternalPorts != nil {
			if nodePortMap, exists := svc.Attributes.ClusterExternalPorts[c.Cluster()]; exists {
				// what we now have is a service port. If there is a mapping for cluster external ports,
				// look it up and get the node port for the remote port
				if nodePort, exists := nodePortMap[gwPort]; exists {
					gwPort = nodePort
				}
			}
		}
		ips := svc.Attributes.ClusterExternalAddresses.GetAddressesFor(c.Cluster())
		for _, ip := range ips {
			newGateways.add(model.NetworkGateway{
				Cluster: c.Cluster(),
				Network: nw,
				Addr:    ip,
				Port:    gwPort,
			})
		}
	}

	previousGateways := c.networkGateways[svc.Hostname][nw]
	gatewaysChanged := !newGateways.equals(previousGateways)
	c.networkGateways[svc.Hostname][nw] = newGateways

	return gatewaysChanged
}

// getGatewayDetails finds the port and network to use for cross-network traffic on the given service.
// Zero values are returned if the service is not a cross-network gateway.
func (c *Controller) getGatewayDetails(svc *model.Service) (uint32, network.ID) {
	// label based gateways
	if nw := svc.Attributes.Labels[label.TopologyNetwork.Name]; nw != "" {
		if gwPortStr := svc.Attributes.Labels[IstioGatewayPortLabel]; gwPortStr != "" {
			if gwPort, err := strconv.Atoi(gwPortStr); err == nil {
				return uint32(gwPort), network.ID(nw)
			}
			log.Warnf("could not parse %q for %s on %s/%s; defaulting to %d",
				gwPortStr, IstioGatewayPortLabel, svc.Attributes.Namespace, svc.Attributes.Name, DefaultNetworkGatewayPort)
		}
		return DefaultNetworkGatewayPort, network.ID(nw)
	}

	// meshNetworks registryServiceName+fromRegistry
	if port, ok := c.registryServiceNameGateways[svc.Hostname]; ok {
		return port, c.networkForRegistry
	}

	return 0, ""
}

// updateServiceNodePortAddresses updates ClusterExternalAddresses for Services of nodePort type
func (c *Controller) updateServiceNodePortAddresses(svcs ...*model.Service) bool {
	// node event, update all nodePort gateway services
	if len(svcs) == 0 {
		svcs = c.getNodePortGatewayServices()
	}
	// no nodePort gateway service found, no update
	if len(svcs) == 0 {
		return false
	}
	for _, svc := range svcs {
		c.RLock()
		nodeSelector := c.nodeSelectorsForServices[svc.Hostname]
		c.RUnlock()
		// update external address
		var nodeAddresses []string
		for _, n := range c.nodeInfoMap {
			if nodeSelector.SubsetOf(n.labels) {
				nodeAddresses = append(nodeAddresses, n.address)
			}
		}
		svc.Attributes.ClusterExternalAddresses.SetAddressesFor(c.Cluster(), nodeAddresses)
		// update gateways that use the service
		c.extractGatewaysFromService(svc)
	}
	return true
}

// getNodePortServices returns nodePort type gateway service
func (c *Controller) getNodePortGatewayServices() []*model.Service {
	c.RLock()
	defer c.RUnlock()
	out := make([]*model.Service, 0, len(c.nodeSelectorsForServices))
	for hostname := range c.nodeSelectorsForServices {
		svc := c.servicesMap[hostname]
		if svc != nil {
			out = append(out, svc)
		}
	}

	return out
}

// gatewaySet is a helper to manage a set of NetworkGateway instances.
type gatewaySet map[model.NetworkGateway]struct{}

func (s gatewaySet) equals(other gatewaySet) bool {
	if len(s) != len(other) {
		return false
	}
	return reflect.DeepEqual(s, other)
}

func (s gatewaySet) add(gw model.NetworkGateway) {
	s[gw] = struct{}{}
}

func (s gatewaySet) addAll(other gatewaySet) {
	for gw := range other {
		s.add(gw)
	}
}

func (s gatewaySet) toArray() []model.NetworkGateway {
	gws := make([]model.NetworkGateway, 0, len(s))
	for gw := range s {
		gw := gw
		gws = append(gws, gw)
	}

	// Sort the array so that it's stable.
	gws = model.SortGateways(gws)

	out := make([]model.NetworkGateway, len(gws))
	for i := range gws {
		out[i] = gws[i]
	}
	return out
}
