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
	"strconv"

	"github.com/yl2chen/cidranger"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/network"
)

type multinetwork struct {
	// CIDR ranger based on path-compressed prefix trie
	ranger cidranger.Ranger

	// Network name for to be used when the meshNetworks fromRegistry nor network label on pod is specified
	// This is defined by a topology.istio.io/network label on the system namespace.
	network network.ID
	// Network name for the registry as specified by the MeshNetworks configmap
	networkForRegistry network.ID
	// map of svc fqdn to partially built network gateways; the actual gateways will be built from these into networkGatewaysBySvc
	// this map just enumerates which networks/ports each Service is a gateway for
	registryServiceNameGateways map[host.Name][]model.NetworkGateway
	// gateways for each service
	networkGatewaysBySvc map[host.Name]model.NetworkGatewaySet
	// implements NetworkGatewaysWatcher; we need to call c.NotifyGatewayHandlers when our gateways change
	model.NetworkGatewaysHandler
}

func initMultinetwork() multinetwork {
	return multinetwork{
		// zero values are a workaround structcheck issue: https://github.com/golangci/golangci-lint/issues/826
		ranger:                      nil,
		network:                     "",
		networkForRegistry:          "",
		registryServiceNameGateways: make(map[host.Name][]model.NetworkGateway),
		networkGatewaysBySvc:        make(map[host.Name]model.NetworkGatewaySet),
	}
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

// onDefaultNetworkChange is fired if the default network is changed either via the namespace label or mesh-networks
func (c *Controller) onDefaultNetworkChange() {
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
	c.onDefaultNetworkChange()
}

// reloadMeshNetworks will read the mesh networks configuration to setup
// fromRegistry and cidr based network lookups for this registry
func (c *Controller) reloadMeshNetworks() {
	c.Lock()
	defer c.Unlock()
	c.networkForRegistry = ""
	ranger := cidranger.NewPCTrieRanger()

	c.networkForRegistry = ""
	c.registryServiceNameGateways = make(map[host.Name][]model.NetworkGateway)

	meshNetworks := c.opts.NetworksWatcher.Networks()
	if meshNetworks == nil || len(meshNetworks.Networks) == 0 {
		return
	}
	for n, v := range meshNetworks.Networks {
		// track endpoints items from this registry are a part of this network
		fromRegistry := false
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
				fromRegistry = true
			}
		}

		// fromRegistry field specified this cluster
		if fromRegistry {
			// treat endpoints in this cluster as part of this network
			if c.networkForRegistry != "" {
				log.Warnf("multiple networks specify %s in fromRegistry; endpoints from %s will continue to be treated as part of %s",
					c.Cluster(), c.Cluster(), c.networkForRegistry)
			} else {
				c.networkForRegistry = network.ID(n)
			}

			// services in this registry matching the registryServiceName and port are part of this network
			for _, gw := range v.Gateways {
				if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
					svc := host.Name(gwSvcName)
					c.registryServiceNameGateways[svc] = append(c.registryServiceNameGateways[svc], model.NetworkGateway{
						Network: network.ID(n),
						Cluster: c.Cluster(),
						Port:    gw.GetPort(),
					})
				}
			}
		}

	}
	c.ranger = ranger
}

func (c *Controller) NetworkGateways() []model.NetworkGateway {
	c.RLock()
	defer c.RUnlock()

	if len(c.networkGatewaysBySvc) == 0 {
		return nil
	}

	// Merge all the gateways into a single set to eliminate duplicates.
	out := make(model.NetworkGatewaySet)
	for _, gateways := range c.networkGatewaysBySvc {
		out.AddAll(gateways)
	}

	return out.ToArray()
}

// extractGatewaysFromService checks if the service is a cross-network gateway
// and if it is, updates the controller's gateways.
func (c *Controller) extractGatewaysFromService(svc *model.Service) bool {
	c.Lock()
	changed := c.extractGatewaysInner(svc)
	c.Unlock()
	if changed {
		c.NotifyGatewayHandlers()
	}
	return changed
}

// reloadNetworkGateways performs extractGatewaysFromService for all services registered with the controller.
func (c *Controller) reloadNetworkGateways() {
	c.Lock()
	gwsChanged := false
	for _, svc := range c.servicesMap {
		if c.extractGatewaysInner(svc) {
			gwsChanged = true
			break
		}
	}
	c.Unlock()
	if gwsChanged {
		c.NotifyGatewayHandlers()
		// TODO ConfigUpdate via gateway handler
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.NetworksTrigger}})
	}
}

// extractGatewaysInner performs the logic for extractGatewaysFromService without locking the controller.
// Returns true if any gateways changed.
func (c *Controller) extractGatewaysInner(svc *model.Service) bool {
	newGateways := make(model.NetworkGatewaySet)

	// check if we have node port mappings
	nodePortMap := make(map[uint32]uint32)
	if svc.Attributes.ClusterExternalPorts != nil {
		if npm, exists := svc.Attributes.ClusterExternalPorts[c.Cluster()]; exists {
			nodePortMap = npm
		}
	}

	gateways := c.getGatewayDetails(svc)

	for _, addr := range svc.Attributes.ClusterExternalAddresses.GetAddressesFor(c.Cluster()) {
		for _, gw := range gateways {
			// what we now have is a service port. If there is a mapping for cluster external ports,
			// look it up and get the node port for the remote port
			if nodePort, exists := nodePortMap[gw.Port]; exists {
				gw.Port = nodePort
			}

			gw.Cluster = c.Cluster()
			gw.Addr = addr
			newGateways.Add(gw)
		}
	}

	previousGateways := c.networkGatewaysBySvc[svc.Hostname]
	gatewaysChanged := !newGateways.Equals(previousGateways)

	if len(newGateways) > 0 {
		c.networkGatewaysBySvc[svc.Hostname] = newGateways
	} else {
		delete(c.networkGatewaysBySvc, svc.Hostname)
	}

	return gatewaysChanged
}

// getGatewayDetails returns gateways without the address populated, only the network and (unmapped) port for a given service.
func (c *Controller) getGatewayDetails(svc *model.Service) []model.NetworkGateway {
	// TODO should we start checking if svc's Ports contain the gateway port?

	// label based gateways
	// TODO label based gateways could support being the gateway for multiple networks
	if nw := svc.Attributes.Labels[label.TopologyNetwork.Name]; nw != "" {
		if gwPortStr := svc.Attributes.Labels[annotation.NetworkingGatewayPort.Name]; gwPortStr != "" {
			if gwPort, err := strconv.Atoi(gwPortStr); err == nil {
				return []model.NetworkGateway{{Port: uint32(gwPort), Network: network.ID(nw)}}
			}
			log.Warnf("could not parse %q for %s on %s/%s; defaulting to %d",
				gwPortStr, annotation.NetworkingGatewayPort.Name, svc.Attributes.Namespace, svc.Attributes.Name, DefaultNetworkGatewayPort)
		}
		return []model.NetworkGateway{{Port: DefaultNetworkGatewayPort, Network: network.ID(nw)}}
	}

	// meshNetworks registryServiceName+fromRegistry
	if gws, ok := c.registryServiceNameGateways[svc.Hostname]; ok {
		out := append(make([]model.NetworkGateway, 0, len(gws)), gws...)
		return out
	}

	return nil
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
