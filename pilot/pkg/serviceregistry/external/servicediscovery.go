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

package external

import (
	"reflect"
	"sync"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
)

// TODO: move this out of 'external' package. Either 'serviceentry' package or
// merge with aggregate (caching, events), and possibly merge both into the
// config directory, for a single top-level cache and event system.

var serviceEntryKind = collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind()
var workloadEntryKind = collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind()

var _ serviceregistry.Instance = &ServiceEntryStore{}

// instancesKey acts as a key to identify all instances for a given hostname/namespace pair
// This is mostly used as an index
type instancesKey struct {
	hostname  host.Name
	namespace string
}

func makeInstanceKey(i *model.ServiceInstance) instancesKey {
	return instancesKey{i.Service.Hostname, i.Service.Attributes.Namespace}
}

type externalConfigType int

const (
	serviceEntryConfigType externalConfigType = iota
	workloadEntryConfigType
)

// configKey unique identifies a config object managed by this registry (ServiceEntry and WorkloadEntry)
type configKey struct {
	kind      externalConfigType
	name      string
	namespace string
}

// ServiceEntryStore communicates with ServiceEntry CRDs and monitors for changes
type ServiceEntryStore struct {
	XdsUpdater model.XDSUpdater
	store      model.IstioConfigStore

	storeMutex sync.RWMutex

	ip2instance map[string][]*model.ServiceInstance
	// Endpoints table
	instances map[instancesKey]map[configKey][]*model.ServiceInstance
	// seWithSelectorByNamespace keeps track of ServiceEntries with selectors, keyed by namespaces
	seWithSelectorByNamespace map[string][]servicesWithEntry
	changeMutex               sync.RWMutex
	refreshIndexes            bool
}

// NewServiceDiscovery creates a new ServiceEntry discovery service
func NewServiceDiscovery(configController model.ConfigStoreCache, store model.IstioConfigStore, xdsUpdater model.XDSUpdater) *ServiceEntryStore {
	c := &ServiceEntryStore{
		XdsUpdater:     xdsUpdater,
		store:          store,
		ip2instance:    map[string][]*model.ServiceInstance{},
		instances:      map[instancesKey]map[configKey][]*model.ServiceInstance{},
		refreshIndexes: true,
	}
	if configController != nil {
		configController.RegisterEventHandler(serviceEntryKind, getServiceEntryHandler(c))
		configController.RegisterEventHandler(workloadEntryKind, getWorkloadEntryHandler(c))
	}
	return c
}

// getWorkloadEntryHandler defines the handler for workload entries
func getWorkloadEntryHandler(c *ServiceEntryStore) func(model.Config, model.Config, model.Event) {
	return func(old, curr model.Config, event model.Event) {
		log.Debugf("Handle event %s for workload entry %s in namespace %s", event, curr.Name, curr.Namespace)

		wle := curr.Spec.(*networking.WorkloadEntry)

		c.storeMutex.RLock()
		// We will only select entries in the same namespace
		entries := c.seWithSelectorByNamespace[curr.Namespace]
		c.storeMutex.RUnlock()

		instances := []*model.ServiceInstance{}
		// For deletes we keep this list empty, which will trigger a deletion
		for _, se := range entries {
			workloadLabels := labels.Collection{wle.Labels}
			if !workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
				// Not a match, skip this one
				continue
			}
			instance := convertWorkloadInstances(wle, se.services, se.entry)
			instances = append(instances, instance...)
		}

		key := configKey{
			kind:      workloadEntryConfigType,
			name:      curr.Name,
			namespace: curr.Namespace,
		}

		if event != model.EventDelete {
			c.updateExistingInstances(key, instances)
		} else {
			c.deleteExistingInstances(key, instances)
		}

		c.edsUpdate(instances)
	}
}

// getServiceEntryHandler defines the handler for service entries
func getServiceEntryHandler(c *ServiceEntryStore) func(model.Config, model.Config, model.Event) {
	return func(old, curr model.Config, event model.Event) {
		// TODO(yonka): Apply full-push scoping to external ServiceEntryStore.

		cs := convertServices(curr)

		// If it is add/delete event we should always do a full push. If it is update event, we should do full push,
		// only when services have changed - otherwise, just push endpoint updates.
		fp := true
		if event == model.EventUpdate {
			// This is not needed, update should always have old populated, but just in case.
			if old.Spec != nil {
				os := convertServices(old)
				fp = servicesChanged(os, cs) || selectorChanged(old, curr)
			} else {
				log.Warnf("Spec is not available in the old service entry during update, proceeding with full push %v", old)
			}
		}

		// If service entry is deleted, cleanup endpoint shards for services.
		if event == model.EventDelete {
			for _, svc := range cs {
				c.XdsUpdater.SvcUpdate(c.Cluster(), string(svc.Hostname), svc.Attributes.Namespace, event)
			}
		}

		// Recomputing the index here is too expensive - lazy build when it is needed.
		c.changeMutex.Lock()
		c.refreshIndexes = fp // Only recompute indexes if services have changed.
		c.changeMutex.Unlock()

		if fp {
			pushReq := &model.PushRequest{
				Full:           true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{},
				Reason:         []model.TriggerReason{model.ServiceUpdate},
			}
			c.XdsUpdater.ConfigUpdate(pushReq)
		} else {
			instances := convertInstances(curr, cs)

			key := configKey{
				kind:      serviceEntryConfigType,
				name:      curr.Name,
				namespace: curr.Namespace,
			}
			// If only instances have changed, just update the indexes for the changed instances.
			c.updateExistingInstances(key, instances)
			c.edsUpdate(instances)
		}
	}
}

func (d *ServiceEntryStore) Provider() serviceregistry.ProviderID {
	return serviceregistry.External
}

func (d *ServiceEntryStore) Cluster() string {
	return ""
}

// AppendServiceHandler adds service resource event handler. Service Entries does not use these handlers.
func (d *ServiceEntryStore) AppendServiceHandler(_ func(*model.Service, model.Event)) error {
	return nil
}

// AppendInstanceHandler adds instance event handler. Service Entries does not use these handlers.
func (d *ServiceEntryStore) AppendInstanceHandler(_ func(*model.ServiceInstance, model.Event)) error {
	return nil
}

// Run is used by some controllers to execute background jobs after init is done.
func (d *ServiceEntryStore) Run(_ <-chan struct{}) {}

// Services list declarations of all services in the system
func (d *ServiceEntryStore) Services() ([]*model.Service, error) {
	services := make([]*model.Service, 0)
	for _, cfg := range d.store.ServiceEntries() {
		services = append(services, convertServices(cfg)...)
	}

	return services, nil
}

// GetService retrieves a service by host name if it exists
// THIS IS A LINEAR SEARCH WHICH CAUSES ALL SERVICE ENTRIES TO BE RECONVERTED -
// DO NOT USE
func (d *ServiceEntryStore) GetService(hostname host.Name) (*model.Service, error) {
	for _, service := range d.getServices() {
		if service.Hostname == hostname {
			return service, nil
		}
	}

	return nil, nil
}

func (d *ServiceEntryStore) getServices() []*model.Service {
	services := make([]*model.Service, 0)
	for _, cfg := range d.store.ServiceEntries() {
		services = append(services, convertServices(cfg)...)
	}
	return services
}

// ManagementPorts retrieves set of health check ports by instance IP.
// This does not apply to Service Entry registry, as Service entries do not
// manage the service instances.
func (d *ServiceEntryStore) ManagementPorts(_ string) model.PortList {
	return nil
}

// WorkloadHealthCheckInfo retrieves set of health check info by instance IP.
// This does not apply to Service Entry registry, as Service entries do not
// manage the service instances.
func (d *ServiceEntryStore) WorkloadHealthCheckInfo(_ string) model.ProbeList {
	return nil
}

// InstancesByPort retrieves instances for a service on the given ports with labels that
// match any of the supplied labels. All instances match an empty tag list.
func (d *ServiceEntryStore) InstancesByPort(svc *model.Service, port int,
	labels labels.Collection) ([]*model.ServiceInstance, error) {
	d.maybeRefreshIndexes()

	d.storeMutex.RLock()
	defer d.storeMutex.RUnlock()

	out := make([]*model.ServiceInstance, 0)

	instanceLists, found := d.instances[instancesKey{svc.Hostname, svc.Attributes.Namespace}]
	if found {
		for _, instances := range instanceLists {
			for _, instance := range instances {
				if instance.Service.Hostname == svc.Hostname &&
					labels.HasSubsetOf(instance.Endpoint.Labels) &&
					portMatchSingle(instance, port) {
					out = append(out, instance)
				}
			}
		}
	}

	return out, nil
}

// servicesWithEntry contains a ServiceEntry and associated model.Services
// This is used only as a key to a map, not intended for external usage
type servicesWithEntry struct {
	entry    *networking.ServiceEntry
	services []*model.Service
}

// edsUpdate triggers an EDS update for the given instances
func (d *ServiceEntryStore) edsUpdate(instances []*model.ServiceInstance) {
	allInstances := []*model.ServiceInstance{}

	// Find all keys we need to lookup
	keys := map[instancesKey]struct{}{}
	for _, i := range instances {
		keys[makeInstanceKey(i)] = struct{}{}
	}

	d.maybeRefreshIndexes()

	d.storeMutex.RLock()
	for key := range keys {
		for _, i := range d.instances[key] {
			allInstances = append(allInstances, i...)
		}
	}
	d.storeMutex.RUnlock()

	// This was a delete
	if len(allInstances) == 0 {
		for k := range keys {
			_ = d.XdsUpdater.EDSUpdate(d.Cluster(), string(k.hostname), k.namespace, nil)
		}
		return
	}

	endpoints := make(map[instancesKey][]*model.IstioEndpoint)
	for _, instance := range allInstances {
		port := instance.ServicePort
		key := makeInstanceKey(instance)
		endpoints[key] = append(endpoints[key],
			&model.IstioEndpoint{
				Address:         instance.Endpoint.Address,
				EndpointPort:    uint32(port.Port),
				ServicePortName: port.Name,
				Labels:          instance.Endpoint.Labels,
				UID:             instance.Endpoint.UID,
				ServiceAccount:  instance.Endpoint.ServiceAccount,
				Network:         instance.Endpoint.Network,
				Locality:        instance.Endpoint.Locality,
				LbWeight:        instance.Endpoint.LbWeight,
				TLSMode:         instance.Endpoint.TLSMode,
			})
	}

	for k, eps := range endpoints {
		_ = d.XdsUpdater.EDSUpdate(d.Cluster(), string(k.hostname), k.namespace, eps)
	}
}

// maybeRefreshIndexes will iterate all ServiceEntries, convert to ServiceInstance (expensive),
// and populate the 'by host' and 'by ip' maps, if needed.
func (d *ServiceEntryStore) maybeRefreshIndexes() {
	d.changeMutex.RLock()
	refreshNeeded := d.refreshIndexes
	d.changeMutex.RUnlock()

	if !refreshNeeded {
		return
	}

	di := map[instancesKey]map[configKey][]*model.ServiceInstance{}
	dip := map[string][]*model.ServiceInstance{}

	seWithSelectorByNamespace := map[string][]servicesWithEntry{}
	for _, cfg := range d.store.ServiceEntries() {
		key := configKey{
			kind:      serviceEntryConfigType,
			name:      cfg.Name,
			namespace: cfg.Namespace,
		}
		updateInstances(key, convertInstances(cfg, nil), di, dip)
		services := convertServices(cfg)

		se := cfg.Spec.(*networking.ServiceEntry)
		// If we have a workload selector, we will add all instances from WorkloadEntries. Otherwise, we continue
		if se.WorkloadSelector != nil {
			seWithSelectorByNamespace[cfg.Namespace] = append(seWithSelectorByNamespace[cfg.Namespace], servicesWithEntry{se, services})
		}
	}

	wles, err := d.store.List(workloadEntryKind, model.NamespaceAll)
	if err != nil {
		log.Errorf("Error listing workload entries: %v", err)
	}
	for _, wcfg := range wles {
		wle := wcfg.Spec.(*networking.WorkloadEntry)
		key := configKey{
			kind:      workloadEntryConfigType,
			name:      wcfg.Name,
			namespace: wcfg.Namespace,
		}
		// We will only select entries in the same namespace
		entries := seWithSelectorByNamespace[wcfg.Namespace]
		for _, se := range entries {
			workloadLabels := labels.Collection{wle.Labels}
			if !workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
				// Not a match, skip this one
				continue
			}
			updateInstances(key, convertWorkloadInstances(wle, se.services, se.entry), di, dip)
		}
	}

	d.storeMutex.Lock()
	d.instances = di
	d.ip2instance = dip
	d.seWithSelectorByNamespace = seWithSelectorByNamespace
	d.storeMutex.Unlock()

	// Without this pilot becomes very unstable even with few 100 ServiceEntry objects - the N_clusters * N_update generates too much garbage ( yaml to proto)
	// This is reset on any change in ServiceEntries that needs index recomputation.
	d.changeMutex.Lock()
	d.refreshIndexes = false
	d.changeMutex.Unlock()
}

func (d *ServiceEntryStore) deleteExistingInstances(ckey configKey, instances []*model.ServiceInstance) {
	d.storeMutex.Lock()
	defer d.storeMutex.Unlock()

	for _, i := range instances {
		delete(d.instances[makeInstanceKey(i)], ckey)
		delete(d.ip2instance, i.Endpoint.Address)
	}
}

// updateExistingInstances updates the indexes (by host, byip maps) for the passed in instances.
func (d *ServiceEntryStore) updateExistingInstances(ckey configKey, instances []*model.ServiceInstance) {
	d.storeMutex.Lock()
	defer d.storeMutex.Unlock()

	// First, delete the existing instances to avoid leaking memory.
	for _, i := range instances {
		delete(d.instances[makeInstanceKey(i)], ckey)
		delete(d.ip2instance, i.Endpoint.Address)
	}

	// Update the indexes with new instances.
	updateInstances(ckey, instances, d.instances, d.ip2instance)
}

// updateInstances updates the instance data to the passed in maps.
func updateInstances(key configKey, instances []*model.ServiceInstance, instancemap map[instancesKey]map[configKey][]*model.ServiceInstance,
	ip2instance map[string][]*model.ServiceInstance) {
	for _, instance := range instances {
		ikey := makeInstanceKey(instance)
		out, found := instancemap[ikey][key]
		if !found {
			out = []*model.ServiceInstance{}
		}
		out = append(out, instance)
		if _, f := instancemap[ikey]; !f {
			instancemap[ikey] = map[configKey][]*model.ServiceInstance{}
		}
		instancemap[ikey][key] = out
		byip, found := ip2instance[instance.Endpoint.Address]
		if !found {
			byip = []*model.ServiceInstance{}
		}
		byip = append(byip, instance)
		ip2instance[instance.Endpoint.Address] = byip
	}
}

// returns true if an instance's port matches with any in the provided list
func portMatchSingle(instance *model.ServiceInstance, port int) bool {
	return port == 0 || port == instance.ServicePort.Port
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (d *ServiceEntryStore) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	d.maybeRefreshIndexes()

	d.storeMutex.RLock()
	defer d.storeMutex.RUnlock()

	out := make([]*model.ServiceInstance, 0)

	for _, ip := range node.IPAddresses {
		instances, found := d.ip2instance[ip]
		if found {
			out = append(out, instances...)
		}
	}
	return out, nil
}

func (d *ServiceEntryStore) GetProxyWorkloadLabels(proxy *model.Proxy) (labels.Collection, error) {
	d.maybeRefreshIndexes()

	d.storeMutex.RLock()
	defer d.storeMutex.RUnlock()

	out := make(labels.Collection, 0)

	for _, ip := range proxy.IPAddresses {
		instances, found := d.ip2instance[ip]
		if found {
			for _, instance := range instances {
				out = append(out, instance.Endpoint.Labels)
			}
		}
	}
	return out, nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation TODOg
func (d *ServiceEntryStore) GetIstioServiceAccounts(*model.Service, []int) []string {
	//for service entries, there is no istio auth, no service accounts, etc. It is just a
	// service, with service instances, and dns.
	return nil
}

// This method compares if services have changed, that needs full push.
func servicesChanged(os []*model.Service, ns []*model.Service) bool {
	// Length of services have changed, needs full push.
	if len(os) != len(ns) {
		return true
	}
	oldservicehosts := make(map[string]*model.Service, len(os))
	newservicehosts := make(map[string]*model.Service, len(ns))

	for _, s := range os {
		oldservicehosts[string(s.Hostname)] = s
	}
	for _, s := range ns {
		newservicehosts[string(s.Hostname)] = s
	}
	for host, service := range oldservicehosts {
		if !reflect.DeepEqual(service, newservicehosts[host]) {
			return true
		}
	}
	return false
}

// This method compares if the selector on a service entry has changed, meaning that it needs full push.
func selectorChanged(old, curr model.Config) bool {
	o := old.Spec.(*networking.ServiceEntry)
	n := curr.Spec.(*networking.ServiceEntry)
	return !reflect.DeepEqual(o.WorkloadSelector, n.WorkloadSelector)
}
