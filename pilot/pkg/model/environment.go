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
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/trustbundle"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/util/sets"
)

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// AmbientIndexes provides access to ambient indexes, which are used to
	// lookup information about ambient services and workloads.
	AmbientIndexes

	// Config interface for listing routing rules
	ConfigStore

	// Watcher is the watcher for the mesh config (to be merged into the config store)
	Watcher

	// NetworksWatcher (loaded from a config map) provides information about the
	// set of networks inside a mesh and how to route to endpoints in each
	// network. Each network provides information about the endpoints in a
	// routable L3 network. A single routable L3 network can have one or more
	// service registries.
	NetworksWatcher mesh.NetworksWatcher

	NetworkManager *NetworkManager

	// mutex used for protecting Environment.pushContext
	mutex sync.RWMutex
	// pushContext holds information during push generation. It is reset on config change, at the beginning
	// of the pushAll. It will hold all errors and stats and possibly caches needed during the entire cache computation.
	// DO NOT USE EXCEPT FOR TESTS AND HANDLING OF NEW CONNECTIONS.
	// ALL USE DURING A PUSH SHOULD USE THE ONE CREATED AT THE
	// START OF THE PUSH, THE GLOBAL ONE MAY CHANGE AND REFLECT A DIFFERENT
	// CONFIG AND PUSH
	pushContext *PushContext

	// DomainSuffix provides a default domain for the Istio server.
	DomainSuffix string

	// TrustBundle: List of Mesh TrustAnchors
	TrustBundle *trustbundle.TrustBundle

	clusterLocalServices ClusterLocalProvider

	CredentialsController credentials.MulticlusterController

	GatewayAPIController GatewayController

	// EndpointShards for a service. This is a global (per-server) list, built from
	// incremental updates. This is keyed by service and namespace
	EndpointIndex *EndpointIndex

	// Cache for XDS resources.
	Cache XdsCache
}

func (e *Environment) Mesh() *meshconfig.MeshConfig {
	if e != nil && e.Watcher != nil {
		return e.Watcher.Mesh()
	}
	return nil
}

func (e *Environment) MeshNetworks() *meshconfig.MeshNetworks {
	if e != nil && e.NetworksWatcher != nil {
		return e.NetworksWatcher.Networks()
	}
	return nil
}

// SetPushContext sets the push context with lock protected
func (e *Environment) SetPushContext(pc *PushContext) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.pushContext = pc
}

// PushContext returns the push context with lock protected
func (e *Environment) PushContext() *PushContext {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.pushContext
}

// GetDiscoveryAddress parses the DiscoveryAddress specified via MeshConfig.
func (e *Environment) GetDiscoveryAddress() (host.Name, string, error) {
	proxyConfig := mesh.DefaultProxyConfig()
	if e.Mesh().DefaultConfig != nil {
		proxyConfig = e.Mesh().DefaultConfig
	}
	hostname, port, err := net.SplitHostPort(proxyConfig.DiscoveryAddress)
	if err != nil {
		return "", "", fmt.Errorf("invalid Istiod Address: %s, %v", proxyConfig.DiscoveryAddress, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("invalid Istiod Port: %s, %s, %v", port, proxyConfig.DiscoveryAddress, err)
	}
	return host.Name(hostname), port, nil
}

func (e *Environment) AddMeshHandler(h func()) {
	if e != nil && e.Watcher != nil {
		e.Watcher.AddMeshHandler(h)
	}
}

func (e *Environment) AddNetworksHandler(h func()) {
	if e != nil && e.NetworksWatcher != nil {
		e.NetworksWatcher.AddNetworksHandler(h)
	}
}

func (e *Environment) AddMetric(metric monitoring.Metric, key string, proxyID, msg string) {
	if e != nil {
		e.PushContext().AddMetric(metric, key, proxyID, msg)
	}
}

// Init initializes the Environment for use.
func (e *Environment) Init() {
	// Use a default DomainSuffix, if none was provided.
	if len(e.DomainSuffix) == 0 {
		e.DomainSuffix = constants.DefaultClusterLocalDomain
	}

	// Create the cluster-local service registry.
	e.clusterLocalServices = NewClusterLocalProvider(e)
}

func (e *Environment) InitNetworksManager(updater XDSUpdater) (err error) {
	e.NetworkManager, err = NewNetworkManager(e, updater)
	return
}

func (e *Environment) ClusterLocal() ClusterLocalProvider {
	return e.clusterLocalServices
}

func (e *Environment) GetProxyConfigOrDefault(ns string, labels, annotations map[string]string, meshConfig *meshconfig.MeshConfig) *meshconfig.ProxyConfig {
	push := e.PushContext()
	if push != nil && push.ProxyConfigs != nil {
		if generatedProxyConfig := push.ProxyConfigs.EffectiveProxyConfig(
			&NodeMetadata{
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			}, meshConfig); generatedProxyConfig != nil {
			return generatedProxyConfig
		}
	}
	return mesh.DefaultProxyConfig()
}

type AmbientIndexes interface {
	ServicesWithWaypoint(key string) []ServiceWaypointInfo
	AddressInformation(addresses sets.String) ([]AddressInfo, sets.String)
	AdditionalPodSubscriptions(
		proxy *Proxy,
		allAddresses sets.String,
		currentSubs sets.String,
	) sets.String
	Policies(requested sets.Set[ConfigKey]) []WorkloadAuthorization
	ServicesForWaypoint(WaypointKey) []ServiceInfo
	WorkloadsForWaypoint(WaypointKey) []WorkloadInfo
}

// WaypointKey is a multi-address extension of NetworkAddress which is commonly used for lookups in AmbientIndex
// We likely need to consider alternative keying options internally such as hostname as we look to expand beyond istio-waypoint
// This extension can ideally support that type of lookup in the interface without introducing scope creep into things
// like NetworkAddress
type WaypointKey struct {
	Namespace string
	Hostnames []string

	Network   string
	Addresses []string
}

// WaypointKeyForProxy builds a key from a proxy to lookup
func WaypointKeyForProxy(node *Proxy) WaypointKey {
	return waypointKeyForProxy(node, false)
}

func WaypointKeyForNetworkGatewayProxy(node *Proxy) WaypointKey {
	return waypointKeyForProxy(node, true)
}

func waypointKeyForProxy(node *Proxy, externalAddresses bool) WaypointKey {
	key := WaypointKey{
		Namespace: node.ConfigNamespace,
		Network:   node.Metadata.Network.String(),
	}
	for _, svct := range node.ServiceTargets {
		key.Hostnames = append(key.Hostnames, svct.Service.Hostname.String())

		var ips []string
		if externalAddresses {
			ips = svct.Service.Attributes.ClusterExternalAddresses.GetAddressesFor(node.GetClusterID())
		} else {
			ips = svct.Service.ClusterVIPs.GetAddressesFor(node.GetClusterID())
		}
		// if we find autoAllocated addresses then ips should contain constants.UnspecifiedIP which should not be used
		foundAutoAllocated := false
		if svct.Service.AutoAllocatedIPv4Address != "" {
			key.Addresses = append(key.Addresses, svct.Service.AutoAllocatedIPv4Address)
			foundAutoAllocated = true
		}
		if svct.Service.AutoAllocatedIPv6Address != "" {
			key.Addresses = append(key.Addresses, svct.Service.AutoAllocatedIPv6Address)
			foundAutoAllocated = true
		}
		if !foundAutoAllocated {
			key.Addresses = append(key.Addresses, ips...)
		}
	}
	return key
}

// NoopAmbientIndexes provides an implementation of AmbientIndexes that always returns nil, to easily "skip" it.
type NoopAmbientIndexes struct{}

func (u NoopAmbientIndexes) AddressInformation(sets.String) ([]AddressInfo, sets.String) {
	return nil, nil
}

func (u NoopAmbientIndexes) AdditionalPodSubscriptions(
	*Proxy,
	sets.String,
	sets.String,
) sets.String {
	return nil
}

func (u NoopAmbientIndexes) Policies(sets.Set[ConfigKey]) []WorkloadAuthorization {
	return nil
}

func (u NoopAmbientIndexes) ServicesForWaypoint(WaypointKey) []ServiceInfo {
	return nil
}

func (u NoopAmbientIndexes) Waypoint(string, string) []netip.Addr {
	return nil
}

func (u NoopAmbientIndexes) WorkloadsForWaypoint(WaypointKey) []WorkloadInfo {
	return nil
}

func (u NoopAmbientIndexes) ServicesWithWaypoint(string) []ServiceWaypointInfo {
	return nil
}

var _ AmbientIndexes = NoopAmbientIndexes{}
