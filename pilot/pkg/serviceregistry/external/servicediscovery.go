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
	foreignInstanceConfigType
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
	// service instances from kubernetes pods - map of ip -> service instance]=
	foreignRegistryInstancesByIP map[string]*model.ServiceInstance
	// seWithSelectorByNamespace keeps track of ServiceEntries with selectors, keyed by namespaces
	seWithSelectorByNamespace map[string][]servicesWithEntry
	changeMutex               sync.RWMutex
	refreshIndexes            bool
}

// NewServiceDiscovery creates a new ServiceEntry discovery service
func NewServiceDiscovery(configController model.ConfigStoreCache, store model.IstioConfigStore, xdsUpdater model.XDSUpdater) *ServiceEntryStore {
	c := &ServiceEntryStore{
		XdsUpdater:                   xdsUpdater,
		store:                        store,
		ip2instance:                  map[string][]*model.ServiceInstance{},
		instances:                    map[instancesKey]map[configKey][]*model.ServiceInstance{},
		foreignRegistryInstancesByIP: map[string]*model.ServiceInstance{},
		refreshIndexes:               true,
	}
	if configController != nil {
		configController.RegisterEventHandler(serviceEntryKind, getServiceEntryHandler(c))
		configController.RegisterEventHandler(workloadEntryKind, getWorkloadEntryHandler(c))
	}
	return c
}

// getWorkloadEntryHandler defines the handler for workload entries
// kube registry controller also calls this function indirectly via the Share interface
// When invoked via the kube registry controller, the old object is nil as the registry
// controller does its own deduping and has no notion of object versions
func getWorkloadEntryHandler(c *ServiceEntryStore) func(model.Config, model.Config, model.Event) {
	return func(old, curr model.Config, event model.Event) {

		wle := curr.Spec.(*networking.WorkloadEntry)
		key := configKey{
			kind:      workloadEntryConfigType,
			name:      curr.Name,
			namespace: curr.Namespace,
		}

		c.storeMutex.RLock()
		// We will only select entries in the same namespace
		entries := c.seWithSelectorByNamespace[curr.Namespace]
		c.storeMutex.RUnlock()

		// if there are no service entries, return now to avoid taking unnecessary locks
		if len(entries) == 0 {
			return
		}
		log.Debugf("Handle event %s for workload entry %s in namespace %s", event, curr.Name, curr.Namespace)
		instances := []*model.ServiceInstance{}

		for _, se := range entries {
			workloadLabels := labels.Collection{wle.Labels}
			if !workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
				// Not a match, skip this one
				continue
			}
			instance := convertWorkloadInstances(wle, se.services, se.entry)
			instances = append(instances, instance...)
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
		cs := convertServices(curr)
		configsUpdated := map[model.ConfigKey]struct{}{}

		// If it is add/delete event we should always do a full push. If it is update event, we should do full push,
		// only when services have changed - otherwise, just push endpoint updates.
		var addedSvcs, deletedSvcs, updatedSvcs, unchangedSvcs []*model.Service

		switch event {
		case model.EventUpdate:
			os := convertServices(old)
			if selectorChanged(old, curr) {
				// Consider all services are updated.
				mark := make(map[host.Name]*model.Service, len(cs))
				for _, svc := range cs {
					mark[svc.Hostname] = svc
					updatedSvcs = append(updatedSvcs, svc)
				}
				for _, svc := range os {
					if _, f := mark[svc.Hostname]; !f {
						updatedSvcs = append(updatedSvcs, svc)
					}
				}
			} else {
				addedSvcs, deletedSvcs, updatedSvcs, unchangedSvcs = servicesDiff(os, cs)
			}
		case model.EventDelete:
			deletedSvcs = cs
			// If service entry is deleted, cleanup endpoint shards for services.
			for _, svc := range cs {
				c.XdsUpdater.SvcUpdate(c.Cluster(), string(svc.Hostname), svc.Attributes.Namespace, event)
			}
		case model.EventAdd:
			addedSvcs = cs
		default:
			// this should not happen
			unchangedSvcs = cs
		}

		for _, svcs := range [][]*model.Service{addedSvcs, deletedSvcs, updatedSvcs} {
			for _, svc := range svcs {
				configsUpdated[model.ConfigKey{
					Kind:      model.ServiceEntryKind,
					Name:      string(svc.Hostname),
					Namespace: svc.Attributes.Namespace}] = struct{}{}
			}
		}

		if len(unchangedSvcs) > 0 {
			// If this service entry had endpoints with IPs (i.e. resolution STATIC), then we do EDS update.
			// If the service entry had endpoints with FQDNs (i.e. resolution DNS), then we need to do
			// full push (as fqdn endpoints go via strict_dns clusters in cds).
			currentServiceEntry := curr.Spec.(*networking.ServiceEntry)
			oldServiceEntry := old.Spec.(*networking.ServiceEntry)
			if currentServiceEntry.Resolution == networking.ServiceEntry_DNS {
				if !reflect.DeepEqual(currentServiceEntry.Endpoints, oldServiceEntry.Endpoints) {
					// fqdn endpoints have changed. Need full push
					for _, svc := range unchangedSvcs {
						configsUpdated[model.ConfigKey{
							Kind:      model.ServiceEntryKind,
							Name:      string(svc.Hostname),
							Namespace: svc.Attributes.Namespace}] = struct{}{}
					}
				}
			}
		}

		fullPush := len(configsUpdated) > 0
		// Recomputing the index here is too expensive - lazy build when it is needed.
		c.changeMutex.Lock()
		c.refreshIndexes = fullPush // Only recompute indexes if services have changed.
		c.changeMutex.Unlock()

		if len(unchangedSvcs) > 0 && !fullPush {
			// IP endpoints in a STATIC service entry has changed. We need EDS update
			// If will do full-push, leave the edsUpdate to that.
			// XXX We should do edsUpdate for all unchangedSvcs since we begin to calculate service
			// data according to this "configsUpdated" and thus remove the "!willFullPush" condition.
			instances := convertInstances(curr, unchangedSvcs)
			key := configKey{
				kind:      serviceEntryConfigType,
				name:      curr.Name,
				namespace: curr.Namespace,
			}
			// If only instances have changed, just update the indexes for the changed instances.
			c.updateExistingInstances(key, instances)
			c.edsUpdate(instances)
			return
		}

		if fullPush {
			pushReq := &model.PushRequest{
				Full:           true,
				ConfigsUpdated: configsUpdated,
				Reason:         []model.TriggerReason{model.ServiceUpdate},
			}
			c.XdsUpdater.ConfigUpdate(pushReq)
		}
	}
}

// GetForeignServiceInstanceHandler defines the handler for service instances generated by other registries
func (d *ServiceEntryStore) GetForeignServiceInstanceHandler() func(*model.ServiceInstance, model.Event) {
	return func(si *model.ServiceInstance, event model.Event) {
		key := configKey{
			kind:      foreignInstanceConfigType,
			name:      si.Endpoint.Address,
			namespace: si.Service.Attributes.Namespace,
		}
		// Used to indicate if this event was fired for a pod->workloadentry conversion
		// and that the event can be ignored due to no relevant change in the workloadentry
		redundantEventForPod := false

		d.storeMutex.Lock()
		// We will only select entries in the same namespace
		entries := d.seWithSelectorByNamespace[si.Service.Attributes.Namespace]

		// this is from a pod. Store it in separate map so that
		// the refreshIndexes function can use these as well as the store ones.
		switch event {
		case model.EventDelete:
			if _, exists := d.foreignRegistryInstancesByIP[si.Endpoint.Address]; !exists {
				// multiple delete events for the same pod (succeeded/failed/unknown status repeating).
				redundantEventForPod = true
			} else {
				delete(d.foreignRegistryInstancesByIP, si.Endpoint.Address)
			}
		default: // add or update
			if old, exists := d.foreignRegistryInstancesByIP[si.Endpoint.Address]; exists {
				// If multiple k8s services select the same pod, we may be getting multiple events
				// ignore them as we only care about the Endpoint itself.
				if reflect.DeepEqual(old.Endpoint, si.Endpoint) {
					// ignore the udpate as nothing has changed
					redundantEventForPod = true
				}
			}
			d.foreignRegistryInstancesByIP[si.Endpoint.Address] = si
		}
		d.storeMutex.Unlock()

		// nothing useful to do.
		if len(entries) == 0 || redundantEventForPod {
			return
		}

		log.Debugf("Handle event %s for service instance (from %s) in namespace %s", event,
			si.Service.Hostname, si.Service.Attributes.Namespace)
		instances := []*model.ServiceInstance{}

		for _, se := range entries {
			workloadLabels := labels.Collection{si.Endpoint.Labels}
			if !workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
				// Not a match, skip this one
				continue
			}
			instance := convertForeignServiceInstances(si, se.services, se.entry)
			instances = append(instances, instance...)
		}

		if event != model.EventDelete {
			d.updateExistingInstances(key, instances)
		} else {
			d.deleteExistingInstances(key, instances)
		}

		d.edsUpdate(instances)
	}
}

func (d *ServiceEntryStore) Provider() serviceregistry.ProviderID {
	return serviceregistry.External
}

func (d *ServiceEntryStore) Cluster() string {
	// DO NOT ASSIGN CLUSTER ID to non-k8s registries. This will prevent service entries with multiple
	// VIPs or CIDR ranges in the address field
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

// HasSynced always returns true for SE
func (d *ServiceEntryStore) HasSynced() bool {
	return true
}

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

	d.storeMutex.RLock()
	for ip, foreignInstance := range d.foreignRegistryInstancesByIP {
		key := configKey{
			kind:      foreignInstanceConfigType,
			name:      ip,
			namespace: foreignInstance.Service.Attributes.Namespace,
		}

		instances := []*model.ServiceInstance{}
		// We will only select entries in the same namespace
		entries := seWithSelectorByNamespace[foreignInstance.Service.Attributes.Namespace]
		for _, se := range entries {
			workloadLabels := labels.Collection{foreignInstance.Endpoint.Labels}
			if !workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
				// Not a match, skip this one
				continue
			}
			instance := convertForeignServiceInstances(foreignInstance, se.services, se.entry)
			instances = append(instances, instance...)
		}
		updateInstances(key, instances, di, dip)
	}

	d.storeMutex.RUnlock()

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
	// First, delete the existing instances to avoid leaking memory.
	d.deleteExistingInstances(ckey, instances)

	d.storeMutex.Lock()
	defer d.storeMutex.Unlock()
	// Update the indexes with new instances.
	updateInstances(ckey, instances, d.instances, d.ip2instance)
}

// updateInstances updates the instance data to the passed in maps.
// This is not concurrent safe.
func updateInstances(key configKey, instances []*model.ServiceInstance, instancemap map[instancesKey]map[configKey][]*model.ServiceInstance,
	ip2instance map[string][]*model.ServiceInstance) {
	for _, instance := range instances {
		ikey := makeInstanceKey(instance)
		if _, f := instancemap[ikey]; !f {
			instancemap[ikey] = map[configKey][]*model.ServiceInstance{}
		}
		instancemap[ikey][key] = append(instancemap[ikey][key], instance)
		ip2instance[instance.Endpoint.Address] = append(ip2instance[instance.Endpoint.Address], instance)
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

// GetIstioServiceAccounts implements model.ServiceAccounts operation
// For service entries using workload entries or mix of workload entries and pods,
// this function returns the appropriate service accounts used by these.
func (d *ServiceEntryStore) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	// service entries with built in endpoints have SANs as a dedicated field.
	// Those with selector labels will have service accounts embedded inside workloadEntries and pods as well.
	return model.GetServiceAccounts(svc, ports, d)
}

func servicesDiff(os []*model.Service, ns []*model.Service) ([]*model.Service, []*model.Service, []*model.Service, []*model.Service) {
	var added, deleted, updated, unchanged []*model.Service

	oldServiceHosts := make(map[string]*model.Service, len(os))
	newServiceHosts := make(map[string]*model.Service, len(ns))
	for _, s := range os {
		oldServiceHosts[string(s.Hostname)] = s
	}
	for _, s := range ns {
		newServiceHosts[string(s.Hostname)] = s
	}

	for name, oldSvc := range oldServiceHosts {
		newSvc, f := newServiceHosts[name]
		if !f {
			deleted = append(deleted, oldSvc)
		} else if !reflect.DeepEqual(oldSvc, newSvc) {
			updated = append(updated, newSvc)
		} else {
			unchanged = append(unchanged, newSvc)
		}
	}
	for name, newSvc := range newServiceHosts {
		if _, f := oldServiceHosts[name]; !f {
			added = append(added, newSvc)
		}
	}

	return added, deleted, updated, unchanged
}

// This method compares if the selector on a service entry has changed, meaning that it needs full push.
func selectorChanged(old, curr model.Config) bool {
	o := old.Spec.(*networking.ServiceEntry)
	n := curr.Spec.(*networking.ServiceEntry)
	return !reflect.DeepEqual(o.WorkloadSelector, n.WorkloadSelector)
}
