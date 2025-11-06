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
	"sync"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// The aggregate controller does not implement serviceregistry.Instance since it may be comprised of various
// providers and clusters.
var (
	_ model.ServiceDiscovery    = &Controller{}
	_ model.AggregateController = &Controller{}
)

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	meshHolder      mesh.Holder
	configClusterID cluster.ID

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

func (c *Controller) ServicesForWaypoint(key model.WaypointKey) []model.ServiceInfo {
	if !features.EnableAmbient {
		return nil
	}
	var res []model.ServiceInfo
	for _, p := range c.GetRegistries() {
		// If this is a kubernetes registry that isn't the local cluster, skip it.
		if p.Cluster() != c.configClusterID && p.Provider() == provider.Kubernetes {
			// Only return workloads for the same cluster as the config cluster.
			continue
		}
		res = append(res, p.ServicesForWaypoint(key)...)
	}
	return res
}

func (c *Controller) ServicesWithWaypoint(key string) []model.ServiceWaypointInfo {
	if !features.EnableAmbient {
		return nil
	}
	var res []model.ServiceWaypointInfo
	for _, p := range c.GetRegistries() {
		// If this is a kubernetes registry that isn't the local cluster, skip it.
		if p.Cluster() != c.configClusterID && p.Provider() == provider.Kubernetes {
			// Only return workloads for the same cluster as the config cluster.
			continue
		}
		res = append(res, p.ServicesWithWaypoint(key)...)
	}
	return res
}

func (c *Controller) WorkloadsForWaypoint(key model.WaypointKey) []model.WorkloadInfo {
	if !features.EnableAmbientWaypoints {
		return nil
	}
	var res []model.WorkloadInfo
	for _, p := range c.GetRegistries() {
		// If this is a kubernetes registry that isn't the local cluster, skip it.
		if p.Cluster() != c.configClusterID && p.Provider() == provider.Kubernetes {
			// Only return workloads for the same cluster as the config cluster.
			continue
		}
		res = append(res, p.WorkloadsForWaypoint(key)...)
	}
	return res
}

func (c *Controller) AdditionalPodSubscriptions(proxy *model.Proxy, addr, cur sets.String) sets.String {
	if !features.EnableAmbient {
		return nil
	}
	res := sets.New[string]()
	for _, p := range c.GetRegistries() {
		// If this is a kubernetes registry that isn't the local cluster, skip it.
		if p.Cluster() != c.configClusterID && p.Provider() == provider.Kubernetes {
			// Only return workloads for the same cluster as the config cluster.
			continue
		}
		res = res.Merge(p.AdditionalPodSubscriptions(proxy, addr, cur))
	}
	return res
}

func (c *Controller) Policies(requested sets.Set[model.ConfigKey]) []model.WorkloadAuthorization {
	var res []model.WorkloadAuthorization
	if !features.EnableAmbient {
		return res
	}
	for _, p := range c.GetRegistries() {
		// If this is a kubernetes registry that isn't the local cluster, skip it.
		if p.Cluster() != c.configClusterID && p.Provider() == provider.Kubernetes {
			// Only return workloads for the same cluster as the config cluster.
			continue
		}
		res = append(res, p.Policies(requested)...)
	}
	return res
}

func (c *Controller) AddressInformation(addresses sets.String) ([]model.AddressInfo, sets.String) {
	if !features.EnableAmbient {
		return nil, nil
	}
	var i []model.AddressInfo
	var removed sets.String
	foundRegistryCount := 0
	for _, p := range c.GetRegistries() {
		// If this is a kubernetes registry that isn't the local cluster, skip it.
		if p.Cluster() != c.configClusterID && p.Provider() == provider.Kubernetes {
			continue
		}
		wis, r := p.AddressInformation(addresses)
		if len(wis) == 0 && len(r) == 0 {
			continue
		}
		foundRegistryCount++
		if foundRegistryCount == 1 {
			// first registry: use the data structures they provided, to avoid a copy
			removed = r
			i = wis
		} else {
			i = append(i, wis...)
			removed.Merge(r)
		}
	}
	if foundRegistryCount > 1 {
		// We may have 'removed' it in one registry but found it in another
		// As an optimization, we skip this in the common case of only one registry
		for _, wl := range i {
			// TODO(@hzxuzhonghu) This is not right for workload, we may search workload by ip, but the resource name is uid.
			if removed.Contains(wl.ResourceName()) {
				removed.Delete(wl.ResourceName())
			}
		}
	}
	return i, removed
}

func (c *Controller) ServiceInfo(key string) *model.ServiceInfo {
	if !features.EnableAmbientMultiNetwork {
		return nil
	}
	for _, p := range c.GetRegistries() {
		// When it comes to service info in ambient multicluster setup, only the local cluster matter.
		if p.Cluster() == c.configClusterID && p.Provider() == provider.Kubernetes {
			return p.ServiceInfo(key)
		}
	}
	return nil
}

type registryEntry struct {
	serviceregistry.Instance
	// stop if not nil is the per-registry stop chan. If null, the server stop chan should be used to Run the registry.
	stop <-chan struct{}
}

type Options struct {
	MeshHolder      mesh.Holder
	ConfigClusterID cluster.ID
}

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	return &Controller{
		registries:        make([]*registryEntry, 0),
		configClusterID:   opt.ConfigClusterID,
		meshHolder:        opt.MeshHolder,
		running:           false,
		handlersByCluster: map[cluster.ID]*model.ControllerHandlers{},
	}
}

func (c *Controller) addRegistry(registry serviceregistry.Instance, stop <-chan struct{}) {
	added := false
	if registry.Provider() == provider.Kubernetes {
		for i, r := range c.registries {
			if r.Provider() != provider.Kubernetes {
				// insert the registry in the position of the first non kubernetes registry
				c.registries = slices.Insert(c.registries, i, &registryEntry{Instance: registry, stop: stop})
				added = true
				break
			}
		}
	}
	if !added {
		c.registries = append(c.registries, &registryEntry{Instance: registry, stop: stop})
	}

	// Observe the registry for events.
	registry.AppendNetworkGatewayHandler(c.NotifyGatewayHandlers)
	registry.AppendServiceHandler(c.handlers.NotifyServiceHandlers)
	registry.AppendServiceHandler(func(prev, curr *model.Service, event model.Event) {
		for _, handlers := range c.getClusterHandlers() {
			handlers.NotifyServiceHandlers(prev, curr, event)
		}
	})
}

func (c *Controller) getClusterHandlers() []*model.ControllerHandlers {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	return maps.Values(c.handlersByCluster)
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
	// smap is a map of hostname (string) to service index, used to identify services that
	// are installed in multiple clusters.
	smap := make(map[host.Name]int)
	index := 0
	services := make([]*model.Service, 0)
	// Locking Registries list while walking it to prevent inconsistent results
	for _, r := range c.GetRegistries() {
		svcs := r.Services()
		if r.Provider() != provider.Kubernetes {
			index += len(svcs)
			services = append(services, svcs...)
		} else {
			for _, s := range svcs {
				previous, ok := smap[s.Hostname]
				if !ok {
					// First time we see a service. The result will have a single service per hostname
					// The first cluster will be listed first, so the services in the primary cluster
					// will be used for default settings. If a service appears in multiple clusters,
					// the order is less clear.
					smap[s.Hostname] = index
					index++
					services = append(services, s)
				} else {
					// We must deepcopy before merge, and after merging, the ClusterVips length will be >= 2.
					// This is an optimization to prevent deepcopy multi-times
					if services[previous].ClusterVIPs.Len() < 2 {
						// Deep copy before merging, otherwise there is a case
						// a service in remote cluster can be deleted, but the ClusterIP left.
						services[previous] = services[previous].DeepCopy()
					}
					// If it is seen second time, that means it is from a different cluster, update cluster VIPs.
					mergeService(services[previous], s, r)
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

// mergeService only merges two clusters' k8s services
func mergeService(dst, src *model.Service, srcRegistry serviceregistry.Instance) {
	if !src.Ports.Equals(dst.Ports) {
		log.Debugf("service %s defined from cluster %s is different from others", src.Hostname, srcRegistry.Cluster())
	}
	// Prefer the k8s HostVIPs where possible
	clusterID := srcRegistry.Cluster()
	if len(dst.ClusterVIPs.GetAddressesFor(clusterID)) == 0 {
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

// GetProxyServiceTargets lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceTargets(node *model.Proxy) []model.ServiceTarget {
	out := make([]model.ServiceTarget, 0)
	nodeClusterID := nodeClusterID(node)
	for _, r := range c.GetRegistries() {
		if skipSearchingRegistryForProxy(nodeClusterID, r) {
			log.Debugf("GetProxyServiceTargets(): not searching registry %v: proxy %v CLUSTER_ID is %v",
				r.Cluster(), node.ID, nodeClusterID)
			continue
		}

		instances := r.GetProxyServiceTargets(node)
		if len(instances) > 0 {
			out = append(out, instances...)
		}
	}

	if len(out) == 0 {
		log.Debugf("GetProxyServiceTargets(): no service targets found for proxy %s with clusterID %s",
			node.ID, nodeClusterID.String())
	}

	return out
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Instance {
	clusterID := nodeClusterID(proxy)
	for _, r := range c.GetRegistries() {
		// If proxy clusterID unset, we may find incorrect workload label.
		// This can not happen in k8s env.
		if clusterID == "" || clusterID == r.Cluster() {
			lbls := r.GetProxyWorkloadLabels(proxy)
			if lbls != nil {
				return lbls
			}
		}
	}

	return nil
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

func (c *Controller) AppendServiceHandler(f model.ServiceHandler) {
	c.handlers.AppendServiceHandler(f)
}

func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) {
	// Currently, it is not used.
	// Note: take care when you want to enable it, it will register the handlers to all registries
	// c.handlers.AppendWorkloadHandler(f)
}

func (c *Controller) AppendServiceHandlerForCluster(id cluster.ID, f model.ServiceHandler) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	handler, ok := c.handlersByCluster[id]
	if !ok {
		c.handlersByCluster[id] = &model.ControllerHandlers{}
		handler = c.handlersByCluster[id]
	}
	handler.AppendServiceHandler(f)
}

func (c *Controller) UnRegisterHandlersForCluster(id cluster.ID) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	delete(c.handlersByCluster, id)
}
