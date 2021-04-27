// Copyright 2017 Istio Authors
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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

// Registry specifies the collection of service registry related interfaces
type Registry struct {
	// Name is the type of the registry - Kubernetes, Consul, etc.
	Name serviceregistry.ServiceRegistry
	// ClusterID is used when multiple registries of the same type are used,
	// for example in the case of K8S multicluster.
	ClusterID string
	model.Controller
	model.ServiceDiscovery
}

// TODO: rename Name to Type and ClusterID to Name ?

var (
	clusterAddressesMutex sync.Mutex
)

// The aggregate controller does not implement serviceregistry.Instance since it may be comprised of various
// providers and clusters.
var _ model.ServiceDiscovery = &Controller{}
var _ model.Controller = &Controller{}

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	registries         []Registry
	storeLock          sync.RWMutex
	trustDomainAliases []string
}

// NewController creates a new Aggregate controller
func NewController(trustDomainAliases []string) *Controller {
	return &Controller{
		registries:         []Registry{},
		trustDomainAliases: trustDomainAliases,
	}
}

// AddRegistry adds registries into the aggregated controller
func (c *Controller) AddRegistry(registry Registry) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()

	registries := c.registries
	registries = append(registries, registry)
	c.registries = registries
}

// DeleteRegistry deletes specified registry from the aggregated controller
func (c *Controller) DeleteRegistry(clusterID string) {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()

	if len(c.registries) == 0 {
		log.Warnf("Registry list is empty, nothing to delete")
		return
	}
	index, ok := c.GetRegistryIndex(clusterID)
	if !ok {
		log.Warnf("Registry is not found in the registries list, nothing to delete")
		return
	}
	registries := c.registries
	registries = append(registries[:index], registries[index+1:]...)
	c.registries = registries
	log.Infof("Registry for the cluster %s has been deleted.", clusterID)
}

// GetRegistries returns a copy of all registries
func (c *Controller) GetRegistries() []Registry {
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()

	return c.registries
}

// GetRegistryIndex returns the index of a registry
func (c *Controller) GetRegistryIndex(clusterID string) (int, bool) {
	for i, r := range c.registries {
		if r.ClusterID == clusterID {
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
		// Race condition: multiple threads may call Services, and multiple services
		// may modify one of the service's cluster ID
		clusterAddressesMutex.Lock()
		if r.ClusterID == "" { // Should we instead check for registry name to be on safe side?
			// If the service is does not have a cluster ID (consul, ServiceEntries, CloudFoundry, etc.)
			// Do not bother checking for the cluster ID.
			// DO NOT ASSIGN CLUSTER ID to non-k8s registries. This will prevent service entries with multiple
			// VIPs or CIDR ranges in the address field
			services = append(services, svcs...)
		} else {
			// This is K8S typically
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
				}

				sp.Mutex.Lock()
				// If the registry has a cluster ID, keep track of the cluster and the
				// local address inside the cluster.
				if sp.ClusterVIPs == nil {
					sp.ClusterVIPs = make(map[string]string)
				}
				sp.ClusterVIPs[r.ClusterID] = s.Address

				if s.Attributes.ClusterExternalAddresses != nil && len(s.Attributes.ClusterExternalAddresses[r.ClusterID]) > 0 {
					if sp.Attributes.ClusterExternalAddresses == nil {
						sp.Attributes.ClusterExternalAddresses = make(map[string][]string)
					}
					sp.Attributes.ClusterExternalAddresses[r.ClusterID] = s.Attributes.ClusterExternalAddresses[r.ClusterID]
				}
				sp.Mutex.Unlock()
			}
		}
		clusterAddressesMutex.Unlock()
	}
	return services, errs
}

// GetService retrieves a service by hostname if exists
func (c *Controller) GetService(hostname host.Name) (*model.Service, error) {
	var errs error
	for _, r := range c.GetRegistries() {
		service, err := r.GetService(hostname)
		if err != nil {
			errs = multierror.Append(errs, err)
		} else if service != nil {
			if errs != nil {
				log.Warnf("GetService() found match but encountered an error: %v", errs)
			}
			return service, nil
		}

	}
	return nil, errs
}

// ManagementPorts retrieves set of health check ports by instance IP
// Return on the first hit.
func (c *Controller) ManagementPorts(addr string) model.PortList {
	for _, r := range c.GetRegistries() {
		if portList := r.ManagementPorts(addr); portList != nil {
			return portList
		}
	}
	return nil
}

// WorkloadHealthCheckInfo returne the health check information for IP addr
// Return on the first hit.
func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	for _, r := range c.GetRegistries() {
		if probeList := r.WorkloadHealthCheckInfo(addr); probeList != nil {
			return probeList
		}
	}
	return nil
}

// InstancesByPort retrieves instances for a service on a given port that match
// any of the supplied labels. All instances match an empty label list.
func (c *Controller) InstancesByPort(svc *model.Service, port int,
	labels labels.Collection) ([]*model.ServiceInstance, error) {
	var instances, tmpInstances []*model.ServiceInstance
	var errs error
	for _, r := range c.GetRegistries() {
		var err error
		tmpInstances, err = r.InstancesByPort(svc, port, labels)
		if err != nil {
			errs = multierror.Append(errs, err)
		} else if len(tmpInstances) > 0 {
			if errs != nil {
				log.Warnf("Instances() found match but encountered an error: %v", errs)
			}
			instances = append(instances, tmpInstances...)
		}
	}
	if len(instances) > 0 {
		errs = nil
	}
	return instances, errs
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)
	var errs error
	// It doesn't make sense for a single proxy to be found in more than one registry.
	// TODO: if otherwise, warning or else what to do about it.
	for _, r := range c.GetRegistries() {
		instances, err := r.GetProxyServiceInstances(node)
		if err != nil {
			errs = multierror.Append(errs, err)
		} else if len(instances) > 0 {
			out = append(out, instances...)
			node.ClusterID = r.ClusterID
			break
		}
	}

	if len(out) > 0 {
		if errs != nil {
			log.Debugf("GetProxyServiceInstances() found match but encountered an error: %v", errs)
		}
		return out, nil
	}

	return out, errs
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) (labels.Collection, error) {
	var out labels.Collection
	var errs error
	// It doesn't make sense for a single proxy to be found in more than one registry.
	// TODO: if otherwise, warning or else what to do about it.
	for _, r := range c.GetRegistries() {
		wlLabels, err := r.GetProxyWorkloadLabels(proxy)
		if err != nil {
			errs = multierror.Append(errs, err)
		} else if len(wlLabels) > 0 {
			out = append(out, wlLabels...)
			break
		}
	}

	if len(out) > 0 {
		if errs != nil {
			log.Warnf("GetProxyWorkloadLabels() found match but encountered an error: %v", errs)
		}
		return out, nil
	}

	return out, errs
}

// Run starts all the controllers
func (c *Controller) Run(stop <-chan struct{}) {

	for _, r := range c.GetRegistries() {
		go r.Run(stop)
	}

	<-stop
	log.Info("Registry Aggregator terminated")
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	for _, r := range c.GetRegistries() {
		if err := r.AppendServiceHandler(f); err != nil {
			log.Infof("Fail to append service handler to adapter %s", r.Name)
			return err
		}
	}
	return nil
}

// AppendInstanceHandler implements a service instance catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	for _, r := range c.GetRegistries() {
		if err := r.AppendInstanceHandler(f); err != nil {
			log.Infof("Fail to append instance handler to adapter %s", r.Name)
			return err
		}
	}
	return nil
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
	result := []string{}
	for k := range out {
		result = append(result, k)
	}
	expanded := spiffe.ExpandWithTrustDomains(result, c.trustDomainAliases)
	result = []string{}
	for k := range expanded {
		result = append(result, k)
	}
	// Sort to make the return result deterministic.
	sort.Strings(result)
	return result
}
