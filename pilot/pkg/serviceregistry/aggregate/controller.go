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

package aggregate

import (
	"sort"
	"sync"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

// The aggregate controller does not implement serviceregistry.Instance since it may be comprised of various
// providers and clusters.
var (
	_ model.ServiceDiscovery    = &Controller{}
	_ model.AggregateController = &Controller{}
)

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	meshHolder mesh.Holder

	// The lock is used to protect the registries and controller's running status.
	storeLock  sync.RWMutex
	registries []*registryEntry
	// indicates whether the controller has run.
	// if true, all the registries added later should be run manually.
	running bool

	handlers          model.ControllerHandlers
	handlersByCluster map[cluster.ID]*model.ControllerHandlers
	model.NetworkGatewaysHandler
}

type registryEntry struct {
	serviceregistry.Instance
	// stop if not nil is the per-registry stop chan. If null, the server stop chan should be used to Run the registry.
	stop <-chan struct{}
}

type Options struct {
	MeshHolder mesh.Holder
}

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	return &Controller{
		registries:        make([]*registryEntry, 0),
		meshHolder:        opt.MeshHolder,
		running:           false,
		handlersByCluster: map[cluster.ID]*model.ControllerHandlers{},
	}
}

func (c *Controller) addRegistry(registry serviceregistry.Instance, stop <-chan struct{}) {
	c.registries = append(c.registries, &registryEntry{Instance: registry, stop: stop})

	// Observe the registry for events.
	registry.AppendNetworkGatewayHandler(c.NotifyGatewayHandlers)
	registry.AppendServiceHandler(c.handlers.NotifyServiceHandlers)
	registry.AppendServiceHandler(func(service *model.Service, event model.Event) {
		for _, handlers := range c.getClusterHandlers() {
			handlers.NotifyServiceHandlers(service, event)
		}
	})
}

func (c *Controller) getClusterHandlers() []*model.ControllerHandlers {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	out := make([]*model.ControllerHandlers, 0, len(c.handlersByCluster))
	for _, handlers := range c.handlersByCluster {
		out = append(out, handlers)
	}
	return out
}

// AddRegistry adds registries into the aggregated controller.
// If the aggregated controller is already Running, the given registry will never be started.
func (c *Controller) AddRegistry(registry serviceregistry.Instance) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	c.addRegistry(registry, nil)
}

// AddRegistryAndRun adds registries into the aggregated controller and makes sure it is Run.
// If the aggregated controller is running, the given registry is Run immediately.
// Otherwise, the given registry is Run when the aggregate controller is Run, using the given stop.
func (c *Controller) AddRegistryAndRun(registry serviceregistry.Instance, stop <-chan struct{}) {
	if stop == nil {
		log.Warnf("nil stop channel passed to AddRegistryAndRun for registry %s/%s", registry.Provider(), registry.Cluster())
	}
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	c.addRegistry(registry, stop)
	if c.running {
		go registry.Run(stop)
	}
}

// DeleteRegistry deletes specified registry from the aggregated controller
func (c *Controller) DeleteRegistry(clusterID cluster.ID, providerID provider.ID) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()

	if len(c.registries) == 0 {
		log.Warnf("Registry list is empty, nothing to delete")
		return
	}
	index, ok := c.getRegistryIndex(clusterID, providerID)
	if !ok {
		log.Warnf("Registry %s/%s is not found in the registries list, nothing to delete", providerID, clusterID)
		return
	}
	c.registries[index] = nil
	c.registries = append(c.registries[:index], c.registries[index+1:]...)
	log.Infof("%s registry for the cluster %s has been deleted.", providerID, clusterID)
}

// GetRegistries returns a copy of all registries
func (c *Controller) GetRegistries() []serviceregistry.Instance {
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()

	// copy registries to prevent race, no need to deep copy here.
	out := make([]serviceregistry.Instance, len(c.registries))
	for i := range c.registries {
		out[i] = c.registries[i]
	}
	return out
}

func (c *Controller) getRegistryIndex(clusterID cluster.ID, provider provider.ID) (int, bool) {
	for i, r := range c.registries {
		if r.Cluster().Equals(clusterID) && r.Provider() == provider {
			return i, true
		}
	}
	return 0, false
}

// Services lists services from all platforms
func (c *Controller) Services() []*model.Service {
	// smap is a map of hostname (string) to service, used to identify services that
	// are installed in multiple clusters.
	smap := make(map[host.Name]*model.Service)

	services := make([]*model.Service, 0)
	// Locking Registries list while walking it to prevent inconsistent results
	for _, r := range c.GetRegistries() {
		svcs := r.Services()
		if r.Provider() != provider.Kubernetes {
			services = append(services, svcs...)
		} else {
			for _, s := range svcs {
				sp, ok := smap[s.Hostname]
				if !ok {
					// First time we see a service. The result will have a single service per hostname
					// The first cluster will be listed first, so the services in the primary cluster
					// will be used for default settings. If a service appears in multiple clusters,
					// the order is less clear.
					smap[s.Hostname] = s
					services = append(services, s)
				} else {
					// If it is seen second time, that means it is from a different cluster, update cluster VIPs.
					// Note: mutating the service of underlying registry here, should have no effect.
					mergeService(sp, s, r)
				}
			}
		}
	}
	return services
}

// GetService retrieves a service by hostname if exists
func (c *Controller) GetService(hostname host.Name) *model.Service {
	var out *model.Service
	for _, r := range c.GetRegistries() {
		service := r.GetService(hostname)
		if service == nil {
			continue
		}
		if r.Provider() != provider.Kubernetes {
			return service
		}
		if out == nil {
			out = service.DeepCopy()
		} else {
			// If we are seeing the service for the second time, it means it is available in multiple clusters.
			mergeService(out, service, r)
		}
	}
	return out
}

func mergeService(dst, src *model.Service, srcRegistry serviceregistry.Instance) {
	// Prefer the k8s HostVIPs where possible
	clusterID := srcRegistry.Cluster()
	if srcRegistry.Provider() == provider.Kubernetes || len(dst.ClusterVIPs.GetAddressesFor(clusterID)) == 0 {
		newAddresses := src.ClusterVIPs.GetAddressesFor(clusterID)
		dst.ClusterVIPs.SetAddressesFor(clusterID, newAddresses)
	}
}

// NetworkGateways merges the service-based cross-network gateways from each registry.
func (c *Controller) NetworkGateways() []model.NetworkGateway {
	var gws []model.NetworkGateway
	for _, r := range c.GetRegistries() {
		gws = append(gws, r.NetworkGateways()...)
	}
	return gws
}

func (c *Controller) MCSServices() []model.MCSServiceInfo {
	var out []model.MCSServiceInfo
	for _, r := range c.GetRegistries() {
		out = append(out, r.MCSServices()...)
	}
	return out
}

// InstancesByPort retrieves instances for a service on a given port that match
// any of the supplied labels. All instances match an empty label list.
func (c *Controller) InstancesByPort(svc *model.Service, port int, labels labels.Collection) []*model.ServiceInstance {
	var instances []*model.ServiceInstance
	for _, r := range c.GetRegistries() {
		instances = append(instances, r.InstancesByPort(svc, port, labels)...)
	}
	return instances
}

func nodeClusterID(node *model.Proxy) cluster.ID {
	if node.Metadata == nil || node.Metadata.ClusterID == "" {
		return ""
	}
	return node.Metadata.ClusterID
}

// Skip the service registry when there won't be a match
// because the proxy is in a different cluster.
func skipSearchingRegistryForProxy(nodeClusterID cluster.ID, r serviceregistry.Instance) bool {
	// Always search non-kube (usually serviceentry) registry.
	// Check every registry if cluster ID isn't specified.
	if r.Provider() != provider.Kubernetes || nodeClusterID == "" {
		return false
	}

	return !r.Cluster().Equals(nodeClusterID)
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	nodeClusterID := nodeClusterID(node)
	for _, r := range c.GetRegistries() {
		if skipSearchingRegistryForProxy(nodeClusterID, r) {
			log.Debugf("GetProxyServiceInstances(): not searching registry %v: proxy %v CLUSTER_ID is %v",
				r.Cluster(), node.ID, nodeClusterID)
			continue
		}

		instances := r.GetProxyServiceInstances(node)
		if len(instances) > 0 {
			out = append(out, instances...)
		}
	}

	return out
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Collection {
	var out labels.Collection
	clusterID := nodeClusterID(proxy)
	for _, r := range c.GetRegistries() {
		// If proxy clusterID unset, we may find incorrect workload label.
		// This can not happen in k8s env.
		if clusterID == "" {
			wlLabels := r.GetProxyWorkloadLabels(proxy)
			if len(wlLabels) > 0 {
				out = append(out, wlLabels...)
				break
			}
		} else if clusterID == r.Cluster() {
			// find proxy in the specified cluster
			wlLabels := r.GetProxyWorkloadLabels(proxy)
			if len(wlLabels) > 0 {
				out = append(out, wlLabels...)
			}
			break
		}
	}

	return out
}

// Run starts all the controllers
func (c *Controller) Run(stop <-chan struct{}) {
	c.storeLock.Lock()
	for _, r := range c.registries {
		// prefer the per-registry stop channel
		registryStop := stop
		if s := r.stop; s != nil {
			registryStop = s
		}
		go r.Run(registryStop)
	}
	c.running = true
	c.storeLock.Unlock()

	<-stop
	log.Info("Registry Aggregator terminated")
}

// HasSynced returns true when all registries have synced
func (c *Controller) HasSynced() bool {
	for _, r := range c.GetRegistries() {
		if !r.HasSynced() {
			log.Debugf("registry %s is syncing", r.Cluster())
			return false
		}
	}
	return true
}

func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) {
	c.handlers.AppendServiceHandler(f)
}

func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) {
	// Currently, it is not used.
	// Note: take care when you want to enable it, it will register the handlers to all registries
	// c.handlers.AppendWorkloadHandler(f)
}

func (c *Controller) AppendServiceHandlerForCluster(id cluster.ID, f func(*model.Service, model.Event)) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	handler, ok := c.handlersByCluster[id]
	if !ok {
		c.handlersByCluster[id] = &model.ControllerHandlers{}
		handler = c.handlersByCluster[id]
	}
	handler.AppendServiceHandler(f)
}

func (c *Controller) AppendWorkloadHandlerForCluster(id cluster.ID, f func(*model.WorkloadInstance, model.Event)) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	handler, ok := c.handlersByCluster[id]
	if !ok {
		c.handlersByCluster[id] = &model.ControllerHandlers{}
		handler = c.handlersByCluster[id]
	}
	handler.AppendWorkloadHandler(f)
}

func (c *Controller) UnRegisterHandlersForCluster(id cluster.ID) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	delete(c.handlersByCluster, id)
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation.
// The returned list contains all SPIFFE based identities that backs the service.
// This method also expand the results from different registries based on the mesh config trust domain aliases.
// To retain such trust domain expansion behavior, the xDS server implementation should wrap any (even if single)
// service registry by this aggreated one.
// For example,
// - { "spiffe://cluster.local/bar@iam.gserviceaccount.com"}; when annotation is used on corresponding workloads.
// - { "spiffe://cluster.local/ns/default/sa/foo" }; normal kubernetes cases
// - { "spiffe://cluster.local/ns/default/sa/foo", "spiffe://trust-domain-alias/ns/default/sa/foo" };
//   if the trust domain alias is configured.
func (c *Controller) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	out := map[string]struct{}{}
	for _, r := range c.GetRegistries() {
		svcAccounts := r.GetIstioServiceAccounts(svc, ports)
		for _, sa := range svcAccounts {
			out[sa] = struct{}{}
		}
	}
	result := make([]string, 0, len(out))
	for k := range out {
		result = append(result, k)
	}
	tds := make([]string, 0)
	if c.meshHolder != nil {
		m := c.meshHolder.Mesh()
		if m != nil {
			tds = m.TrustDomainAliases
		}
	}
	expanded := spiffe.ExpandWithTrustDomains(result, tds)
	result = make([]string, 0, len(expanded))
	for k := range expanded {
		result = append(result, k)
	}
	// Sort to make the return result deterministic.
	sort.Strings(result)
	return result
}
