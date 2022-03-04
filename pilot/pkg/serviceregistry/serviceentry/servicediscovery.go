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
	"strconv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/util/workloadinstances"
	"istio.io/istio/pilot/pkg/util/informermetric"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/queue"
	istiolog "istio.io/pkg/log"
)

var (
	_   serviceregistry.Instance = &ServiceEntryStore{}
	log                          = istiolog.RegisterScope("serviceentry", "ServiceEntry registry", 0)
)

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
	podConfigType
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
	clusterID  cluster.ID

	// This lock is to make multi ops on the below stores.
	// For example, in some case, it requires delete all instances and then update new ones.
	// TODO: refactor serviceInstancesStore to remove the lock
	mutex            sync.RWMutex
	serviceInstances serviceInstancesStore
	// NOTE: historically, one index for both WorkloadEntry(s) and Pod(s);
	//       beware of naming collisions
	workloadInstances workloadinstances.Index
	services          serviceStore
	// to make sure the eds update run in serial to prevent stale ones can override new ones
	// There are multiple threads calling edsUpdate.
	// If all share one lock, then all the threads can have an obvious performance downgrade.
	edsQueue queue.Instance

	workloadHandlers []func(*model.WorkloadInstance, model.Event)

	// cb function used to get the networkID according to workload ip and labels.
	getNetworkIDCb func(IP string, labels labels.Instance) network.ID

	processServiceEntry bool

	model.NetworkGatewaysHandler
}

type ServiceDiscoveryOption func(*ServiceEntryStore)

func DisableServiceEntryProcessing() ServiceDiscoveryOption {
	return func(o *ServiceEntryStore) {
		o.processServiceEntry = false
	}
}

func WithClusterID(clusterID cluster.ID) ServiceDiscoveryOption {
	return func(o *ServiceEntryStore) {
		o.clusterID = clusterID
	}
}

func WithNetworkIDCb(cb func(endpointIP string, labels labels.Instance) network.ID) ServiceDiscoveryOption {
	return func(o *ServiceEntryStore) {
		o.getNetworkIDCb = cb
	}
}

// NewServiceDiscovery creates a new ServiceEntry discovery service
func NewServiceDiscovery(
	configController model.ConfigStoreCache,
	store model.IstioConfigStore,
	xdsUpdater model.XDSUpdater,
	options ...ServiceDiscoveryOption,
) *ServiceEntryStore {
	s := &ServiceEntryStore{
		XdsUpdater: xdsUpdater,
		store:      store,
		serviceInstances: serviceInstancesStore{
			ip2instance:   map[string][]*model.ServiceInstance{},
			instances:     map[instancesKey]map[configKey][]*model.ServiceInstance{},
			instancesBySE: map[types.NamespacedName]map[configKey][]*model.ServiceInstance{},
		},
		workloadInstances: workloadinstances.NewIndex(),
		services: serviceStore{
			servicesBySE: map[types.NamespacedName][]*model.Service{},
		},
		edsQueue:            queue.NewQueue(time.Second),
		processServiceEntry: true,
	}
	for _, o := range options {
		o(s)
	}

	if configController != nil {
		if s.processServiceEntry {
			configController.RegisterEventHandler(gvk.ServiceEntry, s.serviceEntryHandler)
		}
		configController.RegisterEventHandler(gvk.WorkloadEntry, s.workloadEntryHandler)
		_ = configController.SetWatchErrorHandler(informermetric.ErrorHandlerForCluster(s.clusterID))
	}
	return s
}

// workloadEntryHandler defines the handler for workload entries
func (s *ServiceEntryStore) workloadEntryHandler(old, curr config.Config, event model.Event) {
	log.Debugf("Handle event %s for workload entry %s/%s", event, curr.Namespace, curr.Name)
	var oldWle *networking.WorkloadEntry
	if old.Spec != nil {
		oldWle = old.Spec.(*networking.WorkloadEntry)
	}
	wle := curr.Spec.(*networking.WorkloadEntry)
	key := configKey{
		kind:      workloadEntryConfigType,
		name:      curr.Name,
		namespace: curr.Namespace,
	}

	// If an entry is unhealthy, we will mark this as a delete instead
	// This ensures we do not track unhealthy endpoints
	if features.WorkloadEntryHealthChecks && !isHealthy(curr) {
		event = model.EventDelete
	}

	wi := s.convertWorkloadEntryToWorkloadInstance(curr, s.Cluster())
	if wi != nil && !wi.DNSServiceEntryOnly {
		// fire off the k8s handlers
		for _, h := range s.workloadHandlers {
			h(wi, event)
		}
	}

	// includes instances new updated or unchanged, in other word it is the current state.
	instancesUpdated := []*model.ServiceInstance{}
	instancesDeleted := []*model.ServiceInstance{}
	fullPush := false
	configsUpdated := map[model.ConfigKey]struct{}{}

	addConfigs := func(se *networking.ServiceEntry, services []*model.Service) {
		// If serviceentry's resolution is DNS, make a full push
		// TODO: maybe cds?
		if se.Resolution == networking.ServiceEntry_DNS || se.Resolution == networking.ServiceEntry_DNS_ROUND_ROBIN {
			fullPush = true
			for key, value := range getUpdatedConfigs(services) {
				configsUpdated[key] = value
			}
		}
	}

	cfgs, _ := s.store.List(gvk.ServiceEntry, curr.Namespace)
	currSes := getWorkloadServiceEntries(cfgs, wle)
	var oldSes map[types.NamespacedName]*config.Config
	if oldWle != nil {
		if reflect.DeepEqual(oldWle.Labels, wle.Labels) {
			oldSes = currSes
		} else {
			oldSes = getWorkloadServiceEntries(cfgs, oldWle)
		}
	}
	unSelected := difference(oldSes, currSes)
	log.Debugf("workloadEntry %s/%s selected %v, unSelected %v serviceEntry", curr.Namespace, curr.Name, currSes, unSelected)
	s.mutex.Lock()
	for namespacedName, cfg := range currSes {
		services := s.services.getServices(namespacedName)
		se := cfg.Spec.(*networking.ServiceEntry)
		if wi.DNSServiceEntryOnly && se.Resolution != networking.ServiceEntry_DNS &&
			se.Resolution != networking.ServiceEntry_DNS_ROUND_ROBIN {
			log.Debugf("skip selecting workload instance %v/%v for DNS service entry %v", wi.Namespace, wi.Name, se.Hosts)
			continue
		}
		instance := s.convertWorkloadEntryToServiceInstances(wle, services, se, &key, s.Cluster())
		instancesUpdated = append(instancesUpdated, instance...)
		addConfigs(se, services)
	}

	for _, namespacedName := range unSelected {
		services := s.services.getServices(namespacedName)
		cfg := oldSes[namespacedName]
		se := cfg.Spec.(*networking.ServiceEntry)
		if wi.DNSServiceEntryOnly && se.Resolution != networking.ServiceEntry_DNS &&
			se.Resolution != networking.ServiceEntry_DNS_ROUND_ROBIN {
			log.Debugf("skip selecting workload instance %v/%v for DNS service entry %v", wi.Namespace, wi.Name, se.Hosts)
			continue
		}
		instance := s.convertWorkloadEntryToServiceInstances(wle, services, se, &key, s.Cluster())
		instancesDeleted = append(instancesDeleted, instance...)
		addConfigs(se, services)
	}

	s.serviceInstances.deleteInstances(key, instancesDeleted)
	if event == model.EventDelete {
		s.workloadInstances.Delete(wi)
		s.serviceInstances.deleteInstances(key, instancesUpdated)
	} else {
		s.workloadInstances.Insert(wi)
		s.serviceInstances.updateInstances(key, instancesUpdated)
	}
	s.mutex.Unlock()

	allInstances := append(instancesUpdated, instancesDeleted...)
	if !fullPush {
		// trigger full xds push to the related sidecar proxy
		if event == model.EventAdd {
			s.XdsUpdater.ProxyUpdate(s.Cluster(), wle.Address)
		}
		s.edsUpdate(allInstances, true)
		return
	}

	// update eds cache only
	s.edsUpdate(allInstances, false)

	pushReq := &model.PushRequest{
		Full:           true,
		ConfigsUpdated: configsUpdated,
		Reason:         []model.TriggerReason{model.EndpointUpdate},
	}
	// trigger a full push
	s.XdsUpdater.ConfigUpdate(pushReq)
}

// getUpdatedConfigs returns related service entries when full push
func getUpdatedConfigs(services []*model.Service) map[model.ConfigKey]struct{} {
	configsUpdated := map[model.ConfigKey]struct{}{}
	for _, svc := range services {
		configsUpdated[model.ConfigKey{
			Kind:      gvk.ServiceEntry,
			Name:      string(svc.Hostname),
			Namespace: svc.Attributes.Namespace,
		}] = struct{}{}
	}
	return configsUpdated
}

// serviceEntryHandler defines the handler for service entries
func (s *ServiceEntryStore) serviceEntryHandler(_, curr config.Config, event model.Event) {
	currentServiceEntry := curr.Spec.(*networking.ServiceEntry)
	cs := convertServices(curr)
	configsUpdated := map[model.ConfigKey]struct{}{}
	key := types.NamespacedName{Namespace: curr.Namespace, Name: curr.Name}

	s.mutex.Lock()
	// If it is add/delete event we should always do a full push. If it is update event, we should do full push,
	// only when services have changed - otherwise, just push endpoint updates.
	var addedSvcs, deletedSvcs, updatedSvcs, unchangedSvcs []*model.Service
	switch event {
	case model.EventUpdate:
		addedSvcs, deletedSvcs, updatedSvcs, unchangedSvcs = servicesDiff(s.services.getServices(key), cs)
		s.services.updateServices(key, cs)
	case model.EventDelete:
		deletedSvcs = cs
		s.services.deleteServices(key)
	case model.EventAdd:
		addedSvcs = cs
		s.services.updateServices(key, cs)
	default:
		// this should not happen
		unchangedSvcs = cs
	}

	shard := model.ShardKeyFromRegistry(s)
	for _, svc := range addedSvcs {
		s.XdsUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, model.EventAdd)
		configsUpdated[makeConfigKey(svc)] = struct{}{}
	}

	for _, svc := range updatedSvcs {
		s.XdsUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, model.EventUpdate)
		configsUpdated[makeConfigKey(svc)] = struct{}{}
	}

	// If service entry is deleted, cleanup endpoint shards for services.
	for _, svc := range deletedSvcs {
		s.XdsUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, model.EventDelete)
		configsUpdated[makeConfigKey(svc)] = struct{}{}
	}

	if len(unchangedSvcs) > 0 {
		// Trigger full push for DNS resolution ServiceEntry in case endpoint changes.
		if currentServiceEntry.Resolution == networking.ServiceEntry_DNS || currentServiceEntry.Resolution == networking.ServiceEntry_DNS_ROUND_ROBIN {
			for _, svc := range unchangedSvcs {
				configsUpdated[makeConfigKey(svc)] = struct{}{}
			}
		}
	}

	serviceInstancesByConfig, serviceInstances := s.buildServiceInstancesForSE(curr, cs)
	oldInstances := s.serviceInstances.getServiceEntryInstances(key)
	for configKey, old := range oldInstances {
		s.serviceInstances.deleteInstances(configKey, old)
	}
	if event == model.EventDelete {
		s.serviceInstances.deleteAllServiceEntryInstances(key)
	} else {
		// Update the indexes with new instances.
		for ckey, value := range serviceInstancesByConfig {
			s.serviceInstances.addInstances(ckey, value)
		}
		s.serviceInstances.updateServiceEntryInstances(key, serviceInstancesByConfig)
	}
	s.mutex.Unlock()

	fullPush := len(configsUpdated) > 0
	// if not full push needed, at least one service unchanged
	if !fullPush {
		s.edsUpdate(serviceInstances, true)
		return
	}

	// When doing a full push, the non DNS added, updated, unchanged services trigger an eds update
	// so that endpoint shards are updated.
	allServices := make([]*model.Service, 0, len(addedSvcs)+len(updatedSvcs)+len(unchangedSvcs))
	nonDNSServices := make([]*model.Service, 0, len(addedSvcs)+len(updatedSvcs)+len(unchangedSvcs))
	allServices = append(allServices, addedSvcs...)
	allServices = append(allServices, updatedSvcs...)
	allServices = append(allServices, unchangedSvcs...)
	for _, svc := range allServices {
		if !(svc.Resolution == model.DNSLB || svc.Resolution == model.DNSRoundRobinLB) {
			nonDNSServices = append(nonDNSServices, svc)
		}
	}
	// non dns service instances
	keys := map[instancesKey]struct{}{}
	for _, svc := range nonDNSServices {
		keys[instancesKey{hostname: svc.Hostname, namespace: curr.Namespace}] = struct{}{}
	}
	// trigger update eds endpoint shards
	s.edsQueue.Push(func() error {
		s.edsUpdateByKeys(keys, false)
		return nil
	})

	pushReq := &model.PushRequest{
		Full:           true,
		ConfigsUpdated: configsUpdated,
		Reason:         []model.TriggerReason{model.ServiceUpdate},
	}
	s.XdsUpdater.ConfigUpdate(pushReq)
}

// WorkloadInstanceHandler defines the handler for service instances generated by other registries
func (s *ServiceEntryStore) WorkloadInstanceHandler(wi *model.WorkloadInstance, event model.Event) {
	log.Debugf("Handle event %s for workload instance (%s/%s) in namespace %s", event,
		wi.Kind, wi.Endpoint.Address, wi.Namespace)
	key := configKey{
		kind:      podConfigType,
		name:      wi.Name,
		namespace: wi.Namespace,
	}
	// Used to indicate if this event was fired for a pod->workloadentry conversion
	// and that the event can be ignored due to no relevant change in the workloadentry
	redundantEventForPod := false

	var addressToDelete string

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// this is from a pod. Store it in separate map so that
	// the refreshIndexes function can use these as well as the store ones.
	switch event {
	case model.EventDelete:
		redundantEventForPod = s.workloadInstances.Delete(wi) == nil
	default: // add or update
		if old := s.workloadInstances.Insert(wi); old != nil {
			if old.Endpoint.Address != wi.Endpoint.Address {
				addressToDelete = old.Endpoint.Address
			}
			// If multiple k8s services select the same pod or a service has multiple ports,
			// we may be getting multiple events ignore them as we only care about the Endpoint IP itself.
			if model.WorkloadInstancesEqual(old, wi) {
				// ignore the update as nothing has changed
				redundantEventForPod = true
			}
		}
	}

	if redundantEventForPod {
		return
	}

	// We will only select entries in the same namespace
	cfgs, _ := s.store.List(gvk.ServiceEntry, wi.Namespace)
	if len(cfgs) == 0 {
		return
	}

	instances := []*model.ServiceInstance{}
	instancesDeleted := []*model.ServiceInstance{}
	for _, cfg := range cfgs {
		se := cfg.Spec.(*networking.ServiceEntry)
		workloadLabels := labels.Collection{wi.Endpoint.Labels}
		if se.WorkloadSelector == nil || !workloadLabels.IsSupersetOf(se.WorkloadSelector.Labels) {
			// Not a match, skip this one
			continue
		}
		seNamespacedName := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}
		services := s.services.getServices(seNamespacedName)
		instance := convertWorkloadInstanceToServiceInstance(wi.Endpoint, services, se)
		instances = append(instances, instance...)
		if addressToDelete != "" {
			for _, i := range instance {
				di := i.DeepCopy()
				di.Endpoint.Address = addressToDelete
				instancesDeleted = append(instancesDeleted, di)
			}
			s.serviceInstances.deleteServiceEntryInstances(seNamespacedName, key)
		} else if event == model.EventDelete {
			s.serviceInstances.deleteServiceEntryInstances(seNamespacedName, key)
		} else {
			s.serviceInstances.updateServiceEntryInstancesPerConfig(seNamespacedName, key, instance)
		}
	}
	if len(instancesDeleted) > 0 {
		s.serviceInstances.deleteInstances(key, instancesDeleted)
	}

	if event == model.EventDelete {
		s.serviceInstances.deleteInstances(key, instances)
	} else {
		s.serviceInstances.updateInstances(key, instances)
	}

	s.edsUpdate(instances, true)
}

func (s *ServiceEntryStore) Provider() provider.ID {
	return provider.External
}

func (s *ServiceEntryStore) Cluster() cluster.ID {
	return s.clusterID
}

// AppendServiceHandler adds service resource event handler. Service Entries does not use these handlers.
func (s *ServiceEntryStore) AppendServiceHandler(_ func(*model.Service, model.Event)) {}

// AppendWorkloadHandler adds instance event handler. Service Entries does not use these handlers.
func (s *ServiceEntryStore) AppendWorkloadHandler(h func(*model.WorkloadInstance, model.Event)) {
	s.workloadHandlers = append(s.workloadHandlers, h)
}

// Run is used by some controllers to execute background jobs after init is done.
func (s *ServiceEntryStore) Run(stopCh <-chan struct{}) {
	s.edsQueue.Run(stopCh)
}

// HasSynced always returns true for SE
func (s *ServiceEntryStore) HasSynced() bool {
	return true
}

// Services list declarations of all services in the system
func (s *ServiceEntryStore) Services() []*model.Service {
	s.mutex.RLock()
	allServices, allocateNeeded := s.services.getAllServices()
	out := make([]*model.Service, 0, len(allServices))
	if allocateNeeded {
		autoAllocateIPs(allServices)
		s.services.allocateNeeded = false
	}
	s.mutex.RUnlock()
	for _, svc := range allServices {
		// shallow copy, copy `AutoAllocatedAddress`
		// if return the pointer directly, there will be a race with `BuildNameTable`
		// nolint: govet
		shallowSvc := *svc
		out = append(out, &shallowSvc)
	}
	return out
}

// GetService retrieves a service by host name if it exists.
// NOTE: The service entry implementation is used only for tests.
func (s *ServiceEntryStore) GetService(hostname host.Name) *model.Service {
	if !s.processServiceEntry {
		return nil
	}
	// TODO(@hzxuzhonghu): only get the specific service instead of converting all the serviceEntries
	services := s.Services()
	for _, service := range services {
		if service.Hostname == hostname {
			return service
		}
	}

	return nil
}

// InstancesByPort retrieves instances for a service on the given ports with labels that
// match any of the supplied labels. All instances match an empty tag list.
func (s *ServiceEntryStore) InstancesByPort(svc *model.Service, port int, labels labels.Collection) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	s.mutex.RLock()
	instanceLists := s.serviceInstances.getByKey(instancesKey{svc.Hostname, svc.Attributes.Namespace})
	s.mutex.RUnlock()
	for _, instance := range instanceLists {
		if labels.HasSubsetOf(instance.Endpoint.Labels) &&
			portMatchSingle(instance, port) {
			out = append(out, instance)
		}
	}

	return out
}

// ResyncEDS will do a full EDS update. This is needed for some tests where we have many configs loaded without calling
// the config handlers.
// This should probably not be used in production code.
func (s *ServiceEntryStore) ResyncEDS() {
	s.mutex.RLock()
	allInstances := s.serviceInstances.getAll()
	s.mutex.RUnlock()
	s.edsUpdateSync(allInstances, true)
}

// edsUpdateSync triggers an EDS cache update for the given instances.
// And triggers a push if `push` is true synchronously.
// This should probably not be used in production code.
func (s *ServiceEntryStore) edsUpdateSync(instances []*model.ServiceInstance, push bool) {
	// Find all keys we need to lookup
	keys := map[instancesKey]struct{}{}
	for _, i := range instances {
		keys[makeInstanceKey(i)] = struct{}{}
	}
	s.edsUpdateByKeys(keys, push)
}

// edsUpdate triggers an EDS cache update for the given instances.
// And triggers a push if `push` is true.
func (s *ServiceEntryStore) edsUpdate(instances []*model.ServiceInstance, push bool) {
	// Find all keys we need to lookup
	keys := map[instancesKey]struct{}{}
	for _, i := range instances {
		keys[makeInstanceKey(i)] = struct{}{}
	}
	s.edsQueue.Push(func() error {
		s.edsUpdateByKeys(keys, push)
		return nil
	})
}

// edsUpdateByKeys will be run in serial within one thread, such that we can
// prevent allinstances got at t1 can override that got at t2 if multi threads running this function
func (s *ServiceEntryStore) edsUpdateByKeys(keys map[instancesKey]struct{}, push bool) {
	allInstances := []*model.ServiceInstance{}
	s.mutex.RLock()
	for key := range keys {
		i := s.serviceInstances.getByKey(key)
		allInstances = append(allInstances, i...)
	}
	s.mutex.RUnlock()

	// This was a delete
	shard := model.ShardKeyFromRegistry(s)
	if len(allInstances) == 0 {
		if push {
			for k := range keys {
				s.XdsUpdater.EDSUpdate(shard, string(k.hostname), k.namespace, nil)
			}
		} else {
			for k := range keys {
				s.XdsUpdater.EDSCacheUpdate(shard, string(k.hostname), k.namespace, nil)
			}
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
				ServiceAccount:  instance.Endpoint.ServiceAccount,
				Network:         instance.Endpoint.Network,
				Locality:        instance.Endpoint.Locality,
				LbWeight:        instance.Endpoint.LbWeight,
				TLSMode:         instance.Endpoint.TLSMode,
				WorkloadName:    instance.Endpoint.WorkloadName,
				Namespace:       instance.Endpoint.Namespace,
			})
	}

	if push {
		for k, eps := range endpoints {
			s.XdsUpdater.EDSUpdate(shard, string(k.hostname), k.namespace, eps)
		}
	} else {
		for k, eps := range endpoints {
			s.XdsUpdater.EDSCacheUpdate(shard, string(k.hostname), k.namespace, eps)
		}
	}
}

// returns true if an instance's port matches with any in the provided list
func portMatchSingle(instance *model.ServiceInstance, port int) bool {
	return port == 0 || port == instance.ServicePort.Port
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
// NOTE: The service objects in these instances do not have the auto allocated IP set.
func (s *ServiceEntryStore) GetProxyServiceInstances(node *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, ip := range node.IPAddresses {
		instances := s.serviceInstances.getByIP(ip)
		for _, i := range instances {
			// Insert all instances for this IP for services within the same namespace This ensures we
			// match Kubernetes logic where Services do not cross namespace boundaries and avoids
			// possibility of other namespaces inserting service instances into namespaces they do not
			// control.
			if node.Metadata.Namespace == "" || i.Service.Attributes.Namespace == node.Metadata.Namespace {
				out = append(out, i)
			}
		}
	}
	return out
}

func (s *ServiceEntryStore) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Collection {
	out := make(labels.Collection, 0)
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, ip := range proxy.IPAddresses {
		instances := s.serviceInstances.getByIP(ip)
		for _, instance := range instances {
			out = append(out, instance.Endpoint.Labels)
		}
	}
	return out
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation
// For service entries using workload entries or mix of workload entries and pods,
// this function returns the appropriate service accounts used by these.
func (s *ServiceEntryStore) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	// service entries with built in endpoints have SANs as a dedicated field.
	// Those with selector labels will have service accounts embedded inside workloadEntries and pods as well.
	return model.GetServiceAccounts(svc, ports, s)
}

func (s *ServiceEntryStore) NetworkGateways() []model.NetworkGateway {
	// TODO implement mesh networks loading logic from kube controller if needed
	return nil
}

func (s *ServiceEntryStore) MCSServices() []model.MCSServiceInfo {
	return nil
}

func servicesDiff(os []*model.Service, ns []*model.Service) ([]*model.Service, []*model.Service, []*model.Service, []*model.Service) {
	var added, deleted, updated, unchanged []*model.Service

	oldServiceHosts := make(map[host.Name]*model.Service, len(os))
	newServiceHosts := make(map[host.Name]*model.Service, len(ns))
	for _, s := range os {
		oldServiceHosts[s.Hostname] = s
	}
	for _, s := range ns {
		newServiceHosts[s.Hostname] = s
	}

	for _, s := range os {
		newSvc, f := newServiceHosts[s.Hostname]
		if !f {
			deleted = append(deleted, s)
		} else if !reflect.DeepEqual(s, newSvc) {
			updated = append(updated, newSvc)
		} else {
			unchanged = append(unchanged, newSvc)
		}
	}

	for _, s := range ns {
		if _, f := oldServiceHosts[s.Hostname]; !f {
			added = append(added, s)
		}
	}

	return added, deleted, updated, unchanged
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
	maxIPs := 255 * 255 // are we going to exceed this limit by processing 64K services?
	x := 0
	for _, svc := range services {
		// we can allocate IPs only if
		// 1. the service has resolution set to static/dns. We cannot allocate
		//   for NONE because we will not know the original DST IP that the application requested.
		// 2. the address is not set (0.0.0.0)
		// 3. the hostname is not a wildcard
		if svc.DefaultAddress == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() &&
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
		Namespace: svc.Attributes.Namespace,
	}
}

// isHealthy checks that the provided WorkloadEntry is healthy. If health checks are not enabled,
// it is assumed to always be healthy
func isHealthy(cfg config.Config) bool {
	if parseHealthAnnotation(cfg.Annotations[status.WorkloadEntryHealthCheckAnnotation]) {
		// We default to false if the condition is not set. This ensures newly created WorkloadEntries
		// are treated as unhealthy until we prove they are healthy by probe success.
		return status.GetBoolConditionFromSpec(cfg, status.ConditionHealthy, false)
	}
	// If health check is not enabled, assume its healthy
	return true
}

func parseHealthAnnotation(s string) bool {
	if s == "" {
		return false
	}
	p, err := strconv.ParseBool(s)
	if err != nil {
		return false
	}
	return p
}

func (s *ServiceEntryStore) buildServiceInstancesForSE(
	curr config.Config,
	services []*model.Service,
) (map[configKey][]*model.ServiceInstance, []*model.ServiceInstance) {
	currentServiceEntry := curr.Spec.(*networking.ServiceEntry)
	var serviceInstances []*model.ServiceInstance
	serviceInstancesByConfig := map[configKey][]*model.ServiceInstance{}
	// for service entry with labels
	if currentServiceEntry.WorkloadSelector != nil {
		selector := workloadinstances.ByServiceSelector(curr.Namespace, labels.Collection{currentServiceEntry.WorkloadSelector.Labels})
		workloadInstances := workloadinstances.FindAllInIndex(s.workloadInstances, selector)
		for _, wi := range workloadInstances {
			if wi.DNSServiceEntryOnly && currentServiceEntry.Resolution != networking.ServiceEntry_DNS &&
				currentServiceEntry.Resolution != networking.ServiceEntry_DNS_ROUND_ROBIN {
				log.Debugf("skip selecting workload instance %v/%v for DNS service entry %v", wi.Namespace, wi.Name,
					currentServiceEntry.Hosts)
				continue
			}
			instances := convertWorkloadInstanceToServiceInstance(wi.Endpoint, services, currentServiceEntry)
			serviceInstances = append(serviceInstances, instances...)
			ckey := configKey{namespace: wi.Namespace, name: wi.Name}
			if wi.Kind == model.PodKind {
				ckey.kind = podConfigType
			} else {
				ckey.kind = workloadEntryConfigType
			}
			serviceInstancesByConfig[ckey] = instances
		}
	} else {
		serviceInstances = s.convertServiceEntryToInstances(curr, services)
		ckey := configKey{
			kind:      serviceEntryConfigType,
			name:      curr.Name,
			namespace: curr.Namespace,
		}
		serviceInstancesByConfig[ckey] = serviceInstances
	}

	return serviceInstancesByConfig, serviceInstances
}
