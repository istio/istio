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

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

// The aggregate controller does not implement serviceregistry.Instance since it may be comprised of various
// providers and clusters.
var (
	_ model.ServiceDiscovery = &Controller{}
	_ model.Controller       = &Controller{}
)

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	registries []serviceregistry.Instance
	storeLock  sync.RWMutex
	meshHolder mesh.Holder
	running    *atomic.Bool
}

type Options struct {
	MeshHolder mesh.Holder
}

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	return &Controller{
		registries: make([]serviceregistry.Instance, 0),
		meshHolder: opt.MeshHolder,
		running:    atomic.NewBool(false),
	}
}

// AddRegistry adds registries into the aggregated controller
func (c *Controller) AddRegistry(registry serviceregistry.Instance) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()

	c.registries = append(c.registries, registry)
}

// DeleteRegistry deletes specified registry from the aggregated controller
func (c *Controller) DeleteRegistry(clusterID string, providerID serviceregistry.ProviderID) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()

	if len(c.registries) == 0 {
		log.Warnf("Registry list is empty, nothing to delete")
		return
	}
	index, ok := c.getRegistryIndex(clusterID, providerID)
	if !ok {
		log.Warnf("Registry is not found in the registries list, nothing to delete")
		return
	}
	c.registries = append(c.registries[:index], c.registries[index+1:]...)
	log.Infof("Registry for the cluster %s has been deleted.", clusterID)
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

func (c *Controller) getRegistryIndex(clusterID string, provider serviceregistry.ProviderID) (int, bool) {
	for i, r := range c.registries {
		if r.Cluster() == clusterID && r.Provider() == provider {
			return i, true
		}
	}
	return 0, false
}

// Services lists services from all platforms
func (c *Controller) Services() ([]*model.Service, error) {
	// smap is a map of hostname (string) to service, used to identify services that
	// are installed in multiple clusters.
	smap := make(map[host.Name]*model.Service)

	services := make([]*model.Service, 0)
	var errs error
	// Locking Registries list while walking it to prevent inconsistent results
	for _, r := range c.GetRegistries() {
		svcs, err := r.Services()
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}

		if r.Provider() != serviceregistry.Kubernetes {
			services = append(services, svcs...)
		} else {
			for _, s := range svcs {
				sp, ok := smap[s.Hostname]
				if !ok {
					// First time we see a service. The result will have a single service per hostname
					// The first cluster will be listed first, so the services in the primary cluster
					// will be used for default settings. If a service appears in multiple clusters,
					// the order is less clear.
					sp = s
					smap[s.Hostname] = sp
					services = append(services, sp)
				} else {
					// If it is seen second time, that means it is from a different cluster, update cluster VIPs.
					mergeService(sp, s, r.Cluster())
				}
			}
		}
	}
	return services, errs
}

// GetService retrieves a service by hostname if exists
func (c *Controller) GetService(hostname host.Name) (*model.Service, error) {
	var errs error
	var out *model.Service
	for _, r := range c.GetRegistries() {
		service, err := r.GetService(hostname)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		if service == nil {
			continue
		}
		if r.Provider() != serviceregistry.Kubernetes {
			return service, nil
		}
		if out == nil {
			out = service.DeepCopy()
		} else {
			// If we are seeing the service for the second time, it means it is available in multiple clusters.
			mergeService(out, service, r.Cluster())
		}
	}
	return out, errs
}

func mergeService(dst, src *model.Service, srcCluster string) {
	dst.Mutex.Lock()
	if dst.ClusterVIPs == nil {
		dst.ClusterVIPs = make(map[string]string)
	}
	dst.ClusterVIPs[srcCluster] = src.Address
	dst.Mutex.Unlock()
}

// NetworkGateways merges the service-based cross-network gateways from each registry.
func (c *Controller) NetworkGateways() map[string][]*model.Gateway {
	gws := map[string][]*model.Gateway{}
	for _, r := range c.GetRegistries() {
		gwMap := r.NetworkGateways()
		if gwMap == nil {
			continue
		}
		for net, regGws := range gwMap {
			gws[net] = append(gws[net], regGws...)
		}
	}
	return gws
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

func nodeClusterID(node *model.Proxy) string {
	if node.Metadata == nil || node.Metadata.ClusterID == "" {
		return ""
	}
	return node.Metadata.ClusterID
}

// Skip the service registry when there won't be a match
// because the proxy is in a different cluster.
func skipSearchingRegistryForProxy(nodeClusterID string, r serviceregistry.Instance) bool {
	// Always search non-kube (usually serviceentry) registry.
	// Check every registry if cluster ID isn't specified.
	if r.Provider() != serviceregistry.Kubernetes || nodeClusterID == "" {
		return false
	}

	return r.Cluster() != nodeClusterID
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, r := range c.GetRegistries() {
		nodeClusterID := nodeClusterID(node)
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
	// It doesn't make sense for a single proxy to be found in more than one registry.
	// TODO: if otherwise, warning or else what to do about it.
	for _, r := range c.GetRegistries() {
		wlLabels := r.GetProxyWorkloadLabels(proxy)
		if len(wlLabels) > 0 {
			out = append(out, wlLabels...)
			break
		}
	}

	return out
}

// Run starts all the controllers
func (c *Controller) Run(stop <-chan struct{}) {
	for _, r := range c.GetRegistries() {
		go r.Run(stop)
	}
	c.running.Store(true)
	<-stop
	log.Info("Registry Aggregator terminated")
}

// Running returns true after Run has been called. If already running, registries passed to AddRegistry
// should be started outside of this aggregate controller.
func (c *Controller) Running() bool {
	return c.running.Load()
}

// HasSynced returns true when all registries have synced
func (c *Controller) HasSynced() bool {
	for _, r := range c.GetRegistries() {
		if !r.HasSynced() {
			return false
		}
	}
	return true
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) {
	for _, r := range c.GetRegistries() {
		r.AppendServiceHandler(f)
	}
}

func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) {
	for _, r := range c.GetRegistries() {
		r.AppendWorkloadHandler(f)
	}
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
	tds := []string{}
	if c.meshHolder != nil {
		mesh := c.meshHolder.Mesh()
		if mesh != nil {
			tds = mesh.TrustDomainAliases
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
