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

package serviceentry

import (
	"fmt"
	"reflect"
	"sync"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/pkg/log"
)

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
	workloadInstanceConfigType
)

// configKey unique identifies a config object managed by this registry (ServiceEntry and WorkloadEntry)
type configKey struct {
	kind      externalConfigType
	name      string
	namespace string
}

// ServiceEntryStore communicates with ServiceEntry CRDs and monitors for changes
type ServiceEntryStore struct { // nolint:golint
	XdsUpdater model.XDSUpdater
	store      model.IstioConfigStore

	storeMutex sync.RWMutex

	ip2instance map[string][]*model.ServiceInstance
	// Endpoints table
	instances map[instancesKey]map[configKey][]*model.ServiceInstance
	// workload instances from kubernetes pods - map of ip -> workload instance
	workloadInstancesByIP map[string]*model.WorkloadInstance
	// Stores a map of workload instance name/namespace to address
	workloadInstancesIPsByName map[string]string
	// seWithSelectorByNamespace keeps track of ServiceEntries with selectors, keyed by namespaces
	seWithSelectorByNamespace map[string][]servicesWithEntry
	changeMutex               sync.RWMutex
	refreshIndexes            bool
	instanceHandlers          []func(*model.ServiceInstance, model.Event)
	workloadHandlers          []func(*model.WorkloadInstance, model.Event)
}

// NewServiceDiscovery creates a new ServiceEntry discovery service
func NewServiceDiscovery(configController model.ConfigStoreCache, store model.IstioConfigStore, xdsUpdater model.XDSUpdater) *ServiceEntryStore {
	s := &ServiceEntryStore{
		XdsUpdater:                 xdsUpdater,
		store:                      store,
		ip2instance:                map[string][]*model.ServiceInstance{},
		instances:                  map[instancesKey]map[configKey][]*model.ServiceInstance{},
		workloadInstancesByIP:      map[string]*model.WorkloadInstance{},
		workloadInstancesIPsByName: map[string]string{},
		refreshIndexes:             true,
	}
	if configController != nil {
		configController.RegisterEventHandler(gvk.ServiceEntry, s.serviceEntryHandler)
		configController.RegisterEventHandler(gvk.WorkloadEntry, s.workloadEntryHandler)
	}
	return s
}

// workloadEntryHandler defines the handler for workload entries
// kube registry controller also calls this function indirectly via the Share interface
// When invoked via the kube registry controller, the old object is nil as the registry
// controller does its own deduping and has no notion of object versions
func (s *ServiceEntryStore) workloadEntryHandler(old, curr model.Config, event model.Event) {
	wle := curr.Spec.(*networking.WorkloadEntry)
	key := configKey{
		kind:      workloadEntryConfigType,
		name:      curr.Name,
		namespace: curr.Namespace,
	}

	// fire off the k8s handlers
	if len(s.workloadHandlers) > 0 {
		si := convertWorkloadEntryToWorkloadInstance(curr)
		if si != nil {
			for _, h := range s.workloadHandlers {
				h(si, event)
			}
		}
	}

	s.storeMutex.RLock()
	// We will only select entries in the same namespace
	entries := s.seWithSelectorByNamespace[curr.Namespace]
	s.storeMutex.RUnlock()

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
		instance := convertWorkloadEntryToServiceInstances(wle, se.services, se.entry)
		instances = append(instances, instance...)
	}

	if event != model.EventDelete {
		s.updateExistingInstances(key, instances)
	} else {
		s.deleteExistingInstances(key, instances)
	}

	s.edsUpdate(instances)
}

// serviceEntryHandler defines the handler for service entries
func (s *ServiceEntryStore) serviceEntryHandler(old, curr model.Config, event model.Event) {
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
	case model.EventAdd:
		addedSvcs = cs
	default:
		// this should not happen
		unchangedSvcs = cs
	}

	for _, svc := range addedSvcs {
		s.XdsUpdater.SvcUpdate(s.Cluster(), string(svc.Hostname), svc.Attributes.Namespace, model.EventAdd)
		configsUpdated[makeConfigKey(svc)] = struct{}{}
	}

	for _, svc := range updatedSvcs {
		s.XdsUpdater.SvcUpdate(s.Cluster(), string(svc.Hostname), svc.Attributes.Namespace, model.EventUpdate)
		configsUpdated[makeConfigKey(svc)] = struct{}{}
	}

	// If service entry is deleted, cleanup endpoint shards for services.
	for _, svc := range deletedSvcs {
		s.XdsUpdater.SvcUpdate(s.Cluster(), string(svc.Hostname), svc.Attributes.Namespace, model.EventDelete)
		configsUpdated[makeConfigKey(svc)] = struct{}{}
	}

	if len(unchangedSvcs) > 0 {
		currentServiceEntry := curr.Spec.(*networking.ServiceEntry)
		oldServiceEntry := old.Spec.(*networking.ServiceEntry)
		// If this service entry had endpoints with IPs (i.e. resolution STATIC), then we do EDS update.
		// If the service entry had endpoints with FQDNs (i.e. resolution DNS), then we need to do
		// full push (as fqdn endpoints go via strict_dns clusters in cds).
		// Non DNS service entries are sent via EDS. So we should compare and update if such endpoints change.
		if currentServiceEntry.Resolution == networking.ServiceEntry_DNS {
			if !reflect.DeepEqual(currentServiceEntry.Endpoints, oldServiceEntry.Endpoints) {
				// fqdn endpoints have changed. Need full push
				for _, svc := range unchangedSvcs {
					configsUpdated[makeConfigKey(svc)] = struct{}{}
				}
			}
		}

	}

	fullPush := len(configsUpdated) > 0
	// if not full push needed, at least one service unchanged
	if !fullPush {
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
		s.updateExistingInstances(key, instances)
		s.edsUpdate(instances)
		return
	}

	// Recomputing the index here is too expensive - lazy build when it is needed.
	// Only recompute indexes if services have changed.
	s.changeMutex.Lock()
	s.refreshIndexes = true
	s.changeMutex.Unlock()

	// When doing a full push, the non DNS added, updated, unchanged services trigger an eds update
	// so that endpoint shards are updated.
	allServices := make([]*model.Service, 0, len(addedSvcs)+len(updatedSvcs)+len(unchangedSvcs))
	nonDNSServices := make([]*model.Service, 0, len(addedSvcs)+len(updatedSvcs)+len(unchangedSvcs))
	allServices = append(allServices, addedSvcs...)
	allServices = append(allServices, updatedSvcs...)
	allServices = append(allServices, unchangedSvcs...)
	for _, svc := range allServices {
		if svc.Resolution != model.DNSLB {
			nonDNSServices = append(nonDNSServices, svc)
		}
	}
	// non dns service instances
	keys := map[instancesKey]struct{}{}
	for _, svc := range nonDNSServices {
		keys[instancesKey{hostname: svc.Hostname, namespace: curr.Namespace}] = struct{}{}
	}
	// update eds endpoint shards
	s.edsUpdateByKeys(keys)

	pushReq := &model.PushRequest{
		Full:           true,
		ConfigsUpdated: configsUpdated,
		Reason:         []model.TriggerReason{model.ServiceUpdate},
	}
	s.XdsUpdater.ConfigUpdate(pushReq)
}

// WorkloadInstanceHandler defines the handler for service instances generated by other registries
func (s *ServiceEntryStore) WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
	key := configKey{
		kind:      workloadInstanceConfigType,
		name:      si.Name,
		namespace: si.Namespace,
	}
	// Used to indicate if this event was fired for a pod->workloadentry conversion
	// and that the event can be ignored due to no relevant change in the workloadentry
	redundantEventForPod := false

	var addressToDelete string

	s.storeMutex.Lock()
	// this is from a pod. Store it in separate map so that
	// the refreshIndexes function can use these as well as the store ones.
	k := si.Name + "~" + si.Namespace
	switch event {
	case model.EventDelete:
		if _, exists := s.workloadInstancesByIP[si.Endpoint.Address]; !exists {
			// multiple delete events for the same pod (succeeded/failed/unknown status repeating).
			redundantEventForPod = true
		} else {
			delete(s.workloadInstancesByIP, si.Endpoint.Address)
			delete(s.workloadInstancesIPsByName, k)
		}
	default: // add or update
		// Check to see if the workload entry changed. If it did, clear the old entry
		existing := s.workloadInstancesIPsByName[k]
		if existing != "" && existing != si.Endpoint.Address {
			delete(s.workloadInstancesByIP, existing)
			addressToDelete = existing
		}
		if old, exists := s.workloadInstancesByIP[si.Endpoint.Address]; exists {
			// If multiple k8s services select the same pod or a service has multiple ports,
			// we may be getting multiple events ignore them as we only care about the Endpoint IP itself.
			if model.WorkloadInstancesEqual(old, si) {
				// ignore the udpate as nothing has changed
				redundantEventForPod = true
			}
		}
		s.workloadInstancesByIP[si.Endpoint.Address] = si
		s.workloadInstancesIPsByName[k] = si.Endpoint.Address
	}
	// We will only select entries in the same namespace
	entries := s.seWithSelectorByNamespace[si.Namespace]
	s.storeMutex.Unlock()

	// nothing useful to do.
	if len(entries) == 0 || redundantEventForPod {
		return
	}

	log.Debugf("Handle event %s for service instance (from %s) in namespace %s", event,
		si.Endpoint.Address, si.Namespace)
	instances := []*model.ServiceInstance{}
	instancesDeleted := []*model.ServiceInstance{}
	for _, se := range entries {
		workloadLabels := labels.Collection{si.Endpoint.Labels}
		if !workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
			// Not a match, skip this one
			continue
		}
		instance := convertWorkloadInstanceToServiceInstance(si.Endpoint, se.services, se.entry)
		instances = append(instances, instance...)
		if addressToDelete != "" {
			for _, i := range instance {
				di := i.DeepCopy()
				di.Endpoint.Address = addressToDelete
				instancesDeleted = append(instancesDeleted, di)
			}
		}
	}

	if len(instancesDeleted) > 0 {
		s.deleteExistingInstances(key, instancesDeleted)
	}

	if event != model.EventDelete {
		s.updateExistingInstances(key, instances)
	} else {
		s.deleteExistingInstances(key, instances)
	}

	s.edsUpdate(instances)
}

func (s *ServiceEntryStore) Provider() serviceregistry.ProviderID {
	return serviceregistry.External
}

func (s *ServiceEntryStore) Cluster() string {
	// DO NOT ASSIGN CLUSTER ID to non-k8s registries. This will prevent service entries with multiple
	// VIPs or CIDR ranges in the address field
	return ""
}

// AppendServiceHandler adds service resource event handler. Service Entries does not use these handlers.
func (s *ServiceEntryStore) AppendServiceHandler(_ func(*model.Service, model.Event)) error {
	return nil
}

// AppendInstanceHandler adds instance event handler. Service Entries does not use these handlers.
func (s *ServiceEntryStore) AppendInstanceHandler(h func(*model.ServiceInstance, model.Event)) error {
	s.instanceHandlers = append(s.instanceHandlers, h)
	return nil
}

// AppendWorkloadHandler adds instance event handler. Service Entries does not use these handlers.
func (s *ServiceEntryStore) AppendWorkloadHandler(h func(*model.WorkloadInstance, model.Event)) error {
	s.workloadHandlers = append(s.workloadHandlers, h)
	return nil
}

// Run is used by some controllers to execute background jobs after init is done.
func (s *ServiceEntryStore) Run(_ <-chan struct{}) {}

// HasSynced always returns true for SE
func (s *ServiceEntryStore) HasSynced() bool {
	return true
}

// Services list declarations of all services in the system
func (s *ServiceEntryStore) Services() ([]*model.Service, error) {
	services := make([]*model.Service, 0)
	for _, cfg := range s.store.ServiceEntries() {
		services = append(services, convertServices(cfg)...)
	}

	return autoAllocateIPs(services), nil
}

// GetService retrieves a service by host name if it exists
// THIS IS A LINEAR SEARCH WHICH CAUSES ALL SERVICE ENTRIES TO BE RECONVERTED -
// DO NOT USE
// NOTE: This does not auto allocate IPs. The service entry implementation is used only for tests.
func (s *ServiceEntryStore) GetService(hostname host.Name) (*model.Service, error) {
	for _, service := range s.getServices() {
		if service.Hostname == hostname {
			return service, nil
		}
	}

	return nil, nil
}

func (s *ServiceEntryStore) getServices() []*model.Service {
	services := make([]*model.Service, 0)
	for _, cfg := range s.store.ServiceEntries() {
		services = append(services, convertServices(cfg)...)
	}
	return services
}

// InstancesByPort retrieves instances for a service on the given ports with labels that
// match any of the supplied labels. All instances match an empty tag list.
func (s *ServiceEntryStore) InstancesByPort(svc *model.Service, port int,
	labels labels.Collection) ([]*model.ServiceInstance, error) {
	s.maybeRefreshIndexes()

	s.storeMutex.RLock()
	defer s.storeMutex.RUnlock()

	out := make([]*model.ServiceInstance, 0)

	instanceLists, found := s.instances[instancesKey{svc.Hostname, svc.Attributes.Namespace}]
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

// Resync EDS will do a full EDS update. This is needed for some tests where we have many configs loaded without calling
// the config handlers.
// This should probably not be used in production code.
func (s *ServiceEntryStore) ResyncEDS() {
	s.maybeRefreshIndexes()
	allInstances := []*model.ServiceInstance{}
	for _, imap := range s.instances {
		for _, i := range imap {
			allInstances = append(allInstances, i...)
		}
	}
	s.edsUpdate(allInstances)
}

// edsUpdate triggers an EDS cache update for the given instances.
// And triggers a push if `push` is true.
func (s *ServiceEntryStore) edsUpdate(instances []*model.ServiceInstance) {
	// must call it here to refresh s.instances if necessary
	// otherwise may get no instances or miss some new addes instances
	s.maybeRefreshIndexes()
	// Find all keys we need to lookup
	keys := map[instancesKey]struct{}{}
	for _, i := range instances {
		keys[makeInstanceKey(i)] = struct{}{}
	}
	s.edsUpdateByKeys(keys)
}

func (s *ServiceEntryStore) edsUpdateByKeys(keys map[instancesKey]struct{}) {
	// must call it here to refresh s.instances if necessary
	// otherwise may get no instances or miss some new addess instances
	s.maybeRefreshIndexes()
	allInstances := []*model.ServiceInstance{}
	s.storeMutex.RLock()
	for key := range keys {
		for _, i := range s.instances[key] {
			allInstances = append(allInstances, i...)
		}
	}
	s.storeMutex.RUnlock()

	// This was a delete
	if len(allInstances) == 0 {
		for k := range keys {
			_ = s.XdsUpdater.EDSUpdate(s.Cluster(), string(k.hostname), k.namespace, nil)
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
				EndpointPort:    instance.Endpoint.EndpointPort,
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
		_ = s.XdsUpdater.EDSUpdate(s.Cluster(), string(k.hostname), k.namespace, eps)
	}
}

// maybeRefreshIndexes will iterate all ServiceEntries, convert to ServiceInstance (expensive),
// and populate the 'by host' and 'by ip' maps, if needed.
func (s *ServiceEntryStore) maybeRefreshIndexes() {
	s.changeMutex.RLock()
	refreshNeeded := s.refreshIndexes
	s.changeMutex.RUnlock()

	if !refreshNeeded {
		return
	}

	di := map[instancesKey]map[configKey][]*model.ServiceInstance{}
	dip := map[string][]*model.ServiceInstance{}

	seWithSelectorByNamespace := map[string][]servicesWithEntry{}
	for _, cfg := range s.store.ServiceEntries() {
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

	// We need to take a full lock here, rather than just a read lock and then later updating s.instances
	// otherwise, what may happen is both the refresh thread and workload entry/pod handler both generate their own
	// view of s.instances and then write them, leading to inconsistent state. This lock ensures that both threads do
	// a full R+W before the other can start, rather than R,R,W,W.
	s.storeMutex.Lock()
	for _, workloadInstance := range s.workloadInstancesByIP {
		key := configKey{
			kind:      workloadInstanceConfigType,
			name:      workloadInstance.Name,
			namespace: workloadInstance.Namespace,
		}

		instances := []*model.ServiceInstance{}
		// We will only select entries in the same namespace
		entries := seWithSelectorByNamespace[workloadInstance.Namespace]
		for _, se := range entries {
			workloadLabels := labels.Collection{workloadInstance.Endpoint.Labels}
			if !workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
				// Not a match, skip this one
				continue
			}
			instance := convertWorkloadInstanceToServiceInstance(workloadInstance.Endpoint, se.services, se.entry)
			instances = append(instances, instance...)
		}
		updateInstances(key, instances, di, dip)
	}

	wles, err := s.store.List(gvk.WorkloadEntry, model.NamespaceAll)
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
			updateInstances(key, convertWorkloadEntryToServiceInstances(wle, se.services, se.entry), di, dip)
		}
	}

	s.seWithSelectorByNamespace = seWithSelectorByNamespace
	s.instances = di
	s.ip2instance = dip
	s.storeMutex.Unlock()

	// Without this pilot becomes very unstable even with few 100 ServiceEntry objects - the N_clusters * N_update generates too much garbage ( yaml to proto)
	// This is reset on any change in ServiceEntries that needs index recomputation.
	s.changeMutex.Lock()
	s.refreshIndexes = false
	s.changeMutex.Unlock()
}

func (s *ServiceEntryStore) deleteExistingInstances(ckey configKey, instances []*model.ServiceInstance) {
	s.storeMutex.Lock()
	defer s.storeMutex.Unlock()

	deleteInstances(ckey, instances, s.instances, s.ip2instance)
}

// This method is not concurrent safe.
func deleteInstances(key configKey, instances []*model.ServiceInstance, instancemap map[instancesKey]map[configKey][]*model.ServiceInstance,
	ip2instance map[string][]*model.ServiceInstance) {
	for _, i := range instances {
		delete(instancemap[makeInstanceKey(i)], key)
		delete(ip2instance, i.Endpoint.Address)
	}
}

// updateExistingInstances updates the indexes (by host, byip maps) for the passed in instances.
func (s *ServiceEntryStore) updateExistingInstances(ckey configKey, instances []*model.ServiceInstance) {
	s.storeMutex.Lock()
	defer s.storeMutex.Unlock()
	// First, delete the existing instances to avoid leaking memory.
	deleteInstances(ckey, instances, s.instances, s.ip2instance)
	// Update the indexes with new instances.
	updateInstances(ckey, instances, s.instances, s.ip2instance)
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
// NOTE: The service objects in these instances do not have the auto allocated IP set.
func (s *ServiceEntryStore) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	s.maybeRefreshIndexes()

	s.storeMutex.RLock()
	defer s.storeMutex.RUnlock()

	out := make([]*model.ServiceInstance, 0)

	for _, ip := range node.IPAddresses {
		instances, found := s.ip2instance[ip]
		if found {
			out = append(out, instances...)
		}
	}
	return out, nil
}

func (s *ServiceEntryStore) GetProxyWorkloadLabels(proxy *model.Proxy) (labels.Collection, error) {
	s.maybeRefreshIndexes()

	s.storeMutex.RLock()
	defer s.storeMutex.RUnlock()

	out := make(labels.Collection, 0)

	for _, ip := range proxy.IPAddresses {
		instances, found := s.ip2instance[ip]
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
func (s *ServiceEntryStore) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	// service entries with built in endpoints have SANs as a dedicated field.
	// Those with selector labels will have service accounts embedded inside workloadEntries and pods as well.
	return model.GetServiceAccounts(svc, ports, s)
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

// Automatically allocates IPs for service entry services WITHOUT an
// address field if the hostname is not a wildcard, or when resolution
// is not NONE. The IPs are allocated from the reserved Class E subnet
// (240.240.0.0/16) that is not reachable outside the pod. When DNS
// capture is enabled, Envoy will resolve the DNS to these IPs. The
// listeners for TCP services will also be set up on these IPs. The
// IPs allocated to a service entry may differ from istiod to istiod
// but it does not matter because these IPs only affect the listener
// IPs on a given proxy managed by a given istiod.
//
// NOTE: If DNS capture is not enabled by the proxy, the automatically
// allocated IP addresses do not take effect.
//
// The current algorithm to allocate IPs is deterministic across all istiods.
// At stable state, given two istiods with exact same set of services, there should
// be no change in XDS as the algorithm is just a dumb iterative one that allocates sequentially.
//
// TODO: Rather than sequentially allocate IPs, switch to a hash based allocation mechanism so that
// deletion of the oldest service entry does not cause change of IPs for all other service entries.
// Currently, the sequential allocation will result in unnecessary XDS reloads (lds/rds) when a
// service entry with auto allocated IP is deleted. We are trading off a perf problem (xds reload)
// for a usability problem (e.g., multiple cloud SQL or AWS RDS tcp services with no VIPs end up having
// the same port, causing traffic to go to the wrong place). Once we move to a deterministic hash-based
// allocation with deterministic collision resolution, the perf problem will go away. If the collision guarantee
// cannot be made within the IP address space we have (which is about 64K services), then we may need to
// have the sequential allocation algorithm as a fallback when too many collisions take place.
func autoAllocateIPs(services []*model.Service) []*model.Service {
	// i is everything from 240.240.0.(j) to 240.240.255.(j)
	// j is everything from 240.240.(i).1 to 240.240.(i).254
	// we can capture this in one integer variable.
	// given X, we can compute i by X/255, and j is X%255
	// To avoid allocating 240.240.(i).255, if X % 255 is 0, increment X.
	// For example, when X=510, the resulting IP would be 240.240.2.0 (invalid)
	// So we bump X to 511, so that the resulting IP is 240.240.2.1
	maxIPs := 255 * 255 // are we going to exceeed this limit by processing 64K services?
	x := 0
	for _, svc := range services {
		// we can allocate IPs only if
		// 1. the service has resolution set to static/dns. We cannot allocate
		//   for NONE because we will not know the original DST IP that the application requested.
		// 2. the address is not set (0.0.0.0)
		// 3. the hostname is not a wildcard
		if svc.Address == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() &&
			svc.Resolution != model.Passthrough {
			x++
			if x%255 == 0 {
				x++
			}
			if x >= maxIPs {
				log.Errorf("out of IPs to allocate for service entries")
				return services
			}
			thirdOctet := x / 255
			fourthOctet := x % 255
			svc.AutoAllocatedAddress = fmt.Sprintf("240.240.%d.%d", thirdOctet, fourthOctet)
		}
	}
	return services
}

func makeConfigKey(svc *model.Service) model.ConfigKey {
	return model.ConfigKey{
		Kind:      gvk.ServiceEntry,
		Name:      string(svc.Hostname),
		Namespace: svc.Attributes.Namespace}
}
