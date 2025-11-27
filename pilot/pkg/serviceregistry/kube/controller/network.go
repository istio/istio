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
	"sync"

	"github.com/yl2chen/cidranger"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
)

type networkManager struct {
	sync.RWMutex
	// CIDR ranger based on path-compressed prefix trie
	ranger    cidranger.Ranger
	clusterID cluster.ID

	gatewayResourceClient kclient.Informer[*gatewayv1.Gateway]
	meshNetworksWatcher   mesh.NetworksWatcher

	// Network name for to be used when the meshNetworks fromRegistry nor network label on pod is specified
	// This is defined by a topology.istio.io/network label on the system namespace.
	network network.ID
	// Network name for the registry as specified by the MeshNetworks configmap
	networkFromMeshConfig network.ID
	// map of svc fqdn to partially built network gateways; the actual gateways will be built from these into networkGatewaysBySvc
	// this map just enumerates which networks/ports each Service is a gateway for
	registryServiceNameGateways map[host.Name][]model.NetworkGateway
	// gateways for each service
	networkGatewaysBySvc map[host.Name]model.NetworkGatewaySet
	// gateways from kubernetes Gateway resources
	gatewaysFromResource map[types.UID]model.NetworkGatewaySet
	// we don't want to discover gateways with class "istio-remote" from outside cluster's API servers.
	discoverRemoteGatewayResources bool

	// implements NetworkGatewaysWatcher; we need to call c.NotifyGatewayHandlers when our gateways change
	model.NetworkGatewaysHandler
}

func initNetworkManager(c *Controller, options Options) *networkManager {
	n := &networkManager{
		clusterID:           options.ClusterID,
		meshNetworksWatcher: options.MeshNetworksWatcher,
		// zero values are a workaround structcheck issue: https://github.com/golangci/golangci-lint/issues/826
		ranger:                         nil,
		network:                        "",
		networkFromMeshConfig:          "",
		registryServiceNameGateways:    make(map[host.Name][]model.NetworkGateway),
		networkGatewaysBySvc:           make(map[host.Name]model.NetworkGatewaySet),
		gatewaysFromResource:           make(map[types.UID]model.NetworkGatewaySet),
		discoverRemoteGatewayResources: options.ConfigCluster,
	}
	// initialize the gateway resource client when any feature that uses it is enabled
	if features.MultiNetworkGatewayAPI {
		n.gatewayResourceClient = kclient.NewDelayedInformer[*gatewayv1.Gateway](c.client, gvr.KubernetesGateway, kubetypes.StandardInformer, kubetypes.Filter{})
		// conditionally register this handler
		registerHandlers(c, n.gatewayResourceClient, "Gateways", n.handleGatewayResource, nil)
	}
	return n
}

// setNetworkFromNamespace sets network got from system namespace, returns whether it has changed
func (n *networkManager) setNetworkFromNamespace(ns *v1.Namespace) bool {
	nw := ns.Labels[label.TopologyNetwork.Name]
	n.Lock()
	defer n.Unlock()
	oldDefaultNetwork := n.network
	n.network = network.ID(nw)
	return oldDefaultNetwork != n.network
}

func (n *networkManager) networkFromSystemNamespace() network.ID {
	n.RLock()
	defer n.RUnlock()
	return n.network
}

func (n *networkManager) networkFromMeshNetworks(endpointIP string) network.ID {
	n.RLock()
	defer n.RUnlock()
	if n.networkFromMeshConfig != "" {
		return n.networkFromMeshConfig
	}

	if n.ranger != nil {
		ip := net.ParseIP(endpointIP)
		if ip == nil {
			return ""
		}
		entries, err := n.ranger.ContainingNetworks(ip)
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

// namedRangerEntry for holding network's CIDR and name
type namedRangerEntry struct {
	name    network.ID
	network net.IPNet
}

// Network returns the IPNet for the network
func (n namedRangerEntry) Network() net.IPNet {
	return n.network
}

// onNetworkChange is fired if the default network is changed either via the namespace label or mesh-networks
func (c *Controller) onNetworkChange() {
	// the network for endpoints are computed when we process the events; this will fix the cache
	// NOTE: this must run before the other network watcher handler that creates a force push
	if err := c.syncPods(); err != nil {
		log.Errorf("one or more errors force-syncing pods: %v", err)
	}
	if err := c.endpoints.initializeNamespace(metav1.NamespaceAll, true); err != nil {
		log.Errorf("one or more errors force-syncing endpoints: %v", err)
	}
	if c.reloadNetworkGateways() {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: model.NewReasonStats(model.NetworksTrigger), Forced: true})
	}
	c.NotifyGatewayHandlers()
}

// reloadMeshNetworks will read the mesh networks configuration to setup
// fromRegistry and cidr based network lookups for this registry
func (n *networkManager) reloadMeshNetworks() {
	n.Lock()
	defer n.Unlock()
	ranger := cidranger.NewPCTrieRanger()

	n.networkFromMeshConfig = ""
	n.registryServiceNameGateways = make(map[host.Name][]model.NetworkGateway)

	meshNetworks := n.meshNetworksWatcher.Networks()
	if meshNetworks == nil || len(meshNetworks.Networks) == 0 {
		return
	}
	for id, v := range meshNetworks.Networks {
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
			if n.networkFromMeshConfig != "" {
				log.Warnf("multiple networks specify %s in fromRegistry; endpoints from %s will continue to be treated as part of %s",
					n.clusterID, n.clusterID, n.networkFromMeshConfig)
			} else {
				n.networkFromMeshConfig = network.ID(id)
			}

			// services in this registry matching the registryServiceName and port are part of this network
			for _, gw := range v.Gateways {
				if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
					svc := host.Name(gwSvcName)
					n.registryServiceNameGateways[svc] = append(n.registryServiceNameGateways[svc], model.NetworkGateway{
						Network: network.ID(id),
						Cluster: n.clusterID,
						Port:    gw.GetPort(),
					})
				}
			}
		}

	}
	n.ranger = ranger
}

func (c *Controller) NetworkGateways() []model.NetworkGateway {
	c.networkManager.RLock()
	defer c.networkManager.RUnlock()

	// Merge all the gateways into a single set to eliminate duplicates.
	out := make(model.NetworkGatewaySet)
	for _, gateways := range c.networkGatewaysBySvc {
		out.Merge(gateways)
	}
	for _, gateways := range c.gatewaysFromResource {
		out.Merge(gateways)
	}

	unsorted := out.UnsortedList()
	return model.SortGateways(unsorted)
}

// extractGatewaysFromService checks if the service is a cross-network gateway
// and if it is, updates the controller's gateways.
func (c *Controller) extractGatewaysFromService(svc *model.Service) bool {
	changed := c.networkManager.extractGatewaysInner(svc)
	if changed {
		c.NotifyGatewayHandlers()
	}
	return changed
}

// reloadNetworkGateways performs extractGatewaysFromService for all services registered with the controller.
// It returns whether there is a gateway changed.
// It is called only by `onNetworkChange`.
// It iterates over all services, because mesh networks can be set with a service name.
func (c *Controller) reloadNetworkGateways() bool {
	c.Lock()
	defer c.Unlock()
	gwChanged := false
	for _, svc := range c.servicesMap {
		if c.extractGatewaysInner(svc) {
			gwChanged = true
		}
	}
	return gwChanged
}

// extractGatewaysInner updates the gateway address inferred from the service.
// Returns true if any gateway address changed.
func (n *networkManager) extractGatewaysInner(svc *model.Service) bool {
	n.Lock()
	defer n.Unlock()
	previousGateways := n.networkGatewaysBySvc[svc.Hostname]
	gateways := n.getGatewayDetails(svc)
	// short circuit for most services.
	if len(previousGateways) == 0 && len(gateways) == 0 {
		return false
	}

	newGateways := make(model.NetworkGatewaySet)
	// check if we have node port mappings
	nodePortMap := make(map[uint32]uint32)
	if svc.Attributes.ClusterExternalPorts != nil {
		if npm, exists := svc.Attributes.ClusterExternalPorts[n.clusterID]; exists {
			nodePortMap = npm
		}
	}

	for _, addr := range svc.Attributes.ClusterExternalAddresses.GetAddressesFor(n.clusterID) {
		for _, gw := range gateways {
			// what we now have is a service port. If there is a mapping for cluster external ports,
			// look it up and get the node port for the remote port
			if nodePort, exists := nodePortMap[gw.Port]; exists {
				gw.Port = nodePort
			}

			gw.Cluster = n.clusterID
			gw.Addr = addr
			newGateways.Insert(gw)
		}
	}

	gatewaysChanged := !newGateways.Equals(previousGateways)
	if len(newGateways) > 0 {
		n.networkGatewaysBySvc[svc.Hostname] = newGateways
	} else {
		delete(n.networkGatewaysBySvc, svc.Hostname)
	}

	return gatewaysChanged
}

// getGatewayDetails returns gateways without the address populated, only the network and (unmapped) port for a given service.
func (n *networkManager) getGatewayDetails(svc *model.Service) []model.NetworkGateway {
	// We have different types of E/W gateways - those that use mTLS (those are used in sidecar mode when talking cross networks)
	// and those that use double-HBONE (those are used in ambient mode when talking cross cluster). A gateway service may or may
	// not listen on the mTLS (15443, by default) or HBONE (15008) ports, depending on the mode of operation used by the mesh
	// in the remote cluster. We should not use gateways that don't really listen on the right port.

	// label based gateways
	// TODO label based gateways could support being the gateway for multiple networks
	if nw := svc.Attributes.Labels[label.TopologyNetwork.Name]; nw != "" {
		hbonePort := DefaultNetworkGatewayHBONEPort
		gwPort := DefaultNetworkGatewayPort

		if gwPortStr := svc.Attributes.Labels[label.NetworkingGatewayPort.Name]; gwPortStr != "" {
			port, err := strconv.Atoi(gwPortStr)
			if err != nil {
				log.Warnf("could not parse %q for %s on %s/%s; defaulting to %d",
					gwPortStr, label.NetworkingGatewayPort.Name, svc.Attributes.Namespace, svc.Attributes.Name, DefaultNetworkGatewayPort)
			} else {
				gwPort = port
			}
		}

		_, acceptMTLS := svc.Ports.GetByPort(gwPort)
		_, acceptHBONE := svc.Ports.GetByPort(hbonePort)

		if !acceptMTLS && !acceptHBONE {
			log.Warnf("service %s/%s is labeled as gateway, but does not listen neither on port %d nor on port %d",
				svc.Attributes.Namespace, svc.Attributes.Name, gwPort, hbonePort)
			return nil
		}

		if !acceptMTLS {
			gwPort = 0
		}
		if !acceptHBONE {
			hbonePort = 0
		}
		return []model.NetworkGateway{{Port: uint32(gwPort), HBONEPort: uint32(hbonePort), Network: network.ID(nw)}}
	}

	// meshNetworks registryServiceName+fromRegistry
	if gws, ok := n.registryServiceNameGateways[svc.Hostname]; ok {
		out := append(make([]model.NetworkGateway, 0, len(gws)), gws...)
		return out
	}

	return nil
}

// handleGateway resource adds a NetworkGateway for each combination of address and auto-passthrough listener
// discovering duplicates from the generated Service is not a huge concern as we de-duplicate in NetworkGateways
// which returns a set, although it's not totally efficient.
func (n *networkManager) handleGatewayResource(_ *gatewayv1.Gateway, gw *gatewayv1.Gateway, event model.Event) error {
	if nw := gw.GetLabels()[label.TopologyNetwork.Name]; nw == "" {
		return nil
	}

	// Gateway with istio-remote: only discover this from the config cluster
	// this is a way to reference a gateway that lives in a place that this control plane
	// won't have API server access. Nothing will be deployed for these Gateway resources.
	if !n.discoverRemoteGatewayResources && gw.Spec.GatewayClassName == constants.RemoteGatewayClassName {
		return nil
	}

	gatewaysChanged := false
	n.Lock()
	defer func() {
		n.Unlock()
		if gatewaysChanged {
			n.NotifyGatewayHandlers()
		}
	}()

	previousGateways := n.gatewaysFromResource[gw.UID]

	if event == model.EventDelete {
		gatewaysChanged = len(previousGateways) > 0
		delete(n.gatewaysFromResource, gw.UID)
		return nil
	}

	autoPassthrough := func(l gatewayv1.Listener) bool {
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
	newGateways := model.NetworkGatewaySet{}
	for _, addr := range gw.Spec.Addresses {
		if addr.Type == nil {
			continue
		}
		if addrType := *addr.Type; addrType != gatewayv1.IPAddressType && addrType != gatewayv1.HostnameAddressType {
			continue
		}
		for _, l := range slices.Filter(gw.Spec.Listeners, autoPassthrough) {
			networkGateway := base
			networkGateway.Addr = addr.Value
			networkGateway.Port = uint32(l.Port)
			newGateways.Insert(networkGateway)
		}
		for _, l := range gw.Spec.Listeners {
			if l.Protocol == "HBONE" {
				networkGateway := base
				networkGateway.Addr = addr.Value
				networkGateway.Port = uint32(l.Port)
				networkGateway.HBONEPort = uint32(l.Port)
				newGateways.Insert(networkGateway)
			}
		}
	}
	n.gatewaysFromResource[gw.UID] = newGateways

	if len(previousGateways) != len(newGateways) {
		gatewaysChanged = true
		return nil
	}

	gatewaysChanged = !newGateways.Equals(previousGateways)
	if len(newGateways) > 0 {
		n.gatewaysFromResource[gw.UID] = newGateways
	} else {
		delete(n.gatewaysFromResource, gw.UID)
	}

	return nil
}

func (n *networkManager) HasSynced() bool {
	if n.gatewayResourceClient == nil {
		return true
	}
	return n.gatewayResourceClient.HasSynced()
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
		if svc.Attributes.ClusterExternalAddresses == nil {
			svc.Attributes.ClusterExternalAddresses = &model.AddressMap{}
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
