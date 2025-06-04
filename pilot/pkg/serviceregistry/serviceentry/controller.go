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
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/util/workloadinstances"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

var (
	_   serviceregistry.Instance = &Controller{}
	log                          = istiolog.RegisterScope("serviceentry", "ServiceEntry registry")
)

var (
	prime  = 65011     // Used for secondary hash function.
	maxIPs = 256 * 254 // Maximum possible IPs for address allocation.
)

// instancesKey acts as a key to identify all instances for a given hostname/namespace pair
// This is mostly used as an index
type instancesKey struct {
	hostname  host.Name
	namespace string
}

type octetPair struct {
	thirdOctet  int
	fourthOctet int
}

func makeInstanceKey(i *model.ServiceInstance) instancesKey {
	return instancesKey{i.Service.Hostname, i.Service.Attributes.Namespace}
}

type configType int

const (
	serviceEntryConfigType configType = iota
	workloadEntryConfigType
	podConfigType
)

// configKeyWithParent is a superset of configKey that also encodes the parent resource. For instance, if something comes
// from a ServiceEntry selector, the parent is the ServiceEntry
// This is used to distinguish between 1 config (Pod/SE) selected by 2 different parents (ServiceEntry).
type configKeyWithParent struct {
	configKey
	parent types.NamespacedName
}

// configKey unique identifies a config object managed by this registry (ServiceEntry and WorkloadEntry)
type configKey struct {
	kind      configType
	name      string
	namespace string
}

// Controller communicates with ServiceEntry CRDs and monitors for changes.
type Controller struct {
	XdsUpdater model.XDSUpdater

	store     model.ConfigStore
	clusterID cluster.ID

	// This lock is to make multi ops on the below stores. For example, in some case,
	// it requires delete all instances and then update new ones.
	mutex sync.RWMutex

	serviceInstances serviceInstancesStore
	// NOTE: historically, one index for both WorkloadEntry(s) and Pod(s);
	//       beware of naming collisions
	workloadInstances workloadinstances.Index
	services          serviceStore

	// To make sure the eds update run in serial to prevent stale ones can override new ones
	// when edsUpdate is called concurrently.
	// If all share one lock, then all the threads can have an obvious performance downgrade.
	edsQueue queue.Instance

	workloadHandlers []func(*model.WorkloadInstance, model.Event)

	// callback function used to get the networkID according to workload ip and labels.
	networkIDCallback func(IP string, labels labels.Instance) network.ID

	// Indicates whether this controller is for workload entries.
	workloadEntryController bool

	meshWatcher mesh.Watcher

	model.NoopAmbientIndexes
	model.NetworkGatewaysHandler
}

type Option func(*Controller)

func WithClusterID(clusterID cluster.ID) Option {
	return func(o *Controller) {
		o.clusterID = clusterID
	}
}

func WithNetworkIDCb(cb func(endpointIP string, labels labels.Instance) network.ID) Option {
	return func(o *Controller) {
		o.networkIDCallback = cb
	}
}

// NewController creates a new ServiceEntry discovery service.
func NewController(configController model.ConfigStoreController, xdsUpdater model.XDSUpdater,
	meshConfig mesh.Watcher,
	options ...Option,
) *Controller {
	s := newController(configController, xdsUpdater, meshConfig, options...)
	if configController != nil {
		configController.RegisterEventHandler(gvk.ServiceEntry, s.serviceEntryHandler)
		configController.RegisterEventHandler(gvk.WorkloadEntry, s.workloadEntryHandler)
	}
	return s
}

// NewWorkloadEntryController creates a new WorkloadEntry discovery service.
func NewWorkloadEntryController(configController model.ConfigStoreController, xdsUpdater model.XDSUpdater,
	meshConfig mesh.Watcher,
	options ...Option,
) *Controller {
	s := newController(configController, xdsUpdater, meshConfig, options...)
	// Disable service entry processing for workload entry controller.
	s.workloadEntryController = true

	if configController != nil {
		configController.RegisterEventHandler(gvk.WorkloadEntry, s.workloadEntryHandler)
	}
	return s
}

func newController(store model.ConfigStore, xdsUpdater model.XDSUpdater, meshConfig mesh.Watcher, options ...Option) *Controller {
	s := &Controller{
		XdsUpdater:  xdsUpdater,
		store:       store,
		meshWatcher: meshConfig,
		serviceInstances: serviceInstancesStore{
			ip2instance:            map[string][]*model.ServiceInstance{},
			instances:              map[instancesKey]map[configKeyWithParent][]*model.ServiceInstance{},
			instancesBySE:          map[types.NamespacedName]map[configKey][]*model.ServiceInstance{},
			instancesByHostAndPort: sets.New[hostPort](),
		},
		workloadInstances: workloadinstances.NewIndex(),
		services: serviceStore{
			servicesBySE: map[types.NamespacedName][]*model.Service{},
		},
		edsQueue: queue.NewQueue(time.Second),
	}
	for _, o := range options {
		o(s)
	}
	return s
}

// ConvertClientWorkloadEntry merges the metadata.labels and spec.labels
func ConvertClientWorkloadEntry(cfg *clientnetworking.WorkloadEntry) *clientnetworking.WorkloadEntry {
	if cfg.Spec.Labels == nil {
		// Short circuit, we don't have to do any conversion
		return cfg
	}
	cfg = cfg.DeepCopy()
	// Set both fields to be the merged result, so either can be used
	cfg.Spec.Labels = maps.MergeCopy(cfg.Spec.Labels, cfg.Labels)
	cfg.Labels = cfg.Spec.Labels

	return cfg
}

// ConvertWorkloadEntry convert wle from Config.Spec and populate the metadata labels into it.
func ConvertWorkloadEntry(cfg config.Config) *networking.WorkloadEntry {
	wle := cfg.Spec.(*networking.WorkloadEntry)
	if wle == nil {
		return nil
	}

	// we will merge labels from metadata with spec, with precedence to the metadata
	labels := maps.MergeCopy(wle.Labels, cfg.Labels)
	// shallow copy
	copied := protomarshal.ShallowClone(wle)
	copied.Labels = labels
	return copied
}

// workloadEntryHandler defines the handler for workload entries
func (s *Controller) workloadEntryHandler(old, curr config.Config, event model.Event) {
	log.Debugf("Handle event %s for workload entry %s/%s", event, curr.Namespace, curr.Name)
	var oldWle *networking.WorkloadEntry
	if old.Spec != nil {
		oldWle = ConvertWorkloadEntry(old)
	}
	wle := ConvertWorkloadEntry(curr)
	curr.Spec = wle
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
		s.NotifyWorkloadInstanceHandlers(wi, event)
	}

	allInstances := []*model.ServiceInstance{}
	fullPush := false
	configsUpdated := sets.New[model.ConfigKey]()

	addConfigs := func(se *networking.ServiceEntry, services []*model.Service) {
		// If serviceentry's resolution is DNS, make a full push
		// TODO: maybe cds?
		if isDNSTypeServiceEntry(se) {
			fullPush = true
			for key, value := range getUpdatedConfigs(services) {
				configsUpdated[key] = value
			}
		}
	}

	cfgs := s.store.List(gvk.ServiceEntry, curr.Namespace)
	currSes := getWorkloadServiceEntries(cfgs, wle)
	var oldSes map[types.NamespacedName]*config.Config
	if oldWle != nil {
		if labels.Instance(oldWle.Labels).Equals(curr.Labels) {
			oldSes = currSes
		} else {
			// labels update should trigger proxy update
			s.XdsUpdater.ProxyUpdate(s.Cluster(), wle.Address)
			oldSes = getWorkloadServiceEntries(cfgs, oldWle)
		}
	}
	unSelected := difference(oldSes, currSes)
	log.Debugf("workloadEntry %s/%s selected %v, unSelected %v serviceEntry", curr.Namespace, curr.Name, currSes, unSelected)
	s.mutex.Lock()
	for namespacedName, cfg := range currSes {
		services := s.services.getServices(namespacedName)
		se := cfg.Spec.(*networking.ServiceEntry)
		if wi.DNSServiceEntryOnly && !isDNSTypeServiceEntry(se) {
			log.Debugf("skip selecting workload instance %v/%v for DNS service entry %v", wi.Namespace, wi.Name, se.Hosts)
			continue
		}
		instance := s.convertWorkloadEntryToServiceInstances(wle, services, se, &key, s.Cluster())
		allInstances = append(allInstances, instance...)
		parentKey := configKeyWithParent{
			configKey: key,
			parent:    namespacedName,
		}
		if event == model.EventDelete {
			s.serviceInstances.deleteServiceEntryInstances(namespacedName, key)
			s.serviceInstances.deleteInstanceKeys(parentKey, instance)
		} else {
			s.serviceInstances.updateInstances(parentKey, instance)
			s.serviceInstances.updateServiceEntryInstancesPerConfig(namespacedName, key, instance)
		}
		addConfigs(se, services)
	}

	for _, namespacedName := range unSelected {
		services := s.services.getServices(namespacedName)
		cfg := oldSes[namespacedName]
		se := cfg.Spec.(*networking.ServiceEntry)
		if wi.DNSServiceEntryOnly && !isDNSTypeServiceEntry(se) {
			log.Debugf("skip selecting workload instance %v/%v for DNS service entry %v", wi.Namespace, wi.Name, se.Hosts)
			continue
		}
		instance := s.convertWorkloadEntryToServiceInstances(wle, services, se, &key, s.Cluster())
		parentKey := configKeyWithParent{
			configKey: key,
			parent:    namespacedName,
		}
		allInstances = append(allInstances, instance...)
		s.serviceInstances.deleteServiceEntryInstances(namespacedName, key)
		s.serviceInstances.deleteInstanceKeys(parentKey, instance)
		addConfigs(se, services)
	}

	if event == model.EventDelete {
		s.workloadInstances.Delete(wi)
	} else {
		s.workloadInstances.Insert(wi)
	}
	s.mutex.Unlock()

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

	// TODO: if len(configsUpdated) == 0 we're firing a Forced push for compatibility because no configs have been updated,
	// we should actually skip the push.
	pushReq := &model.PushRequest{
		Full:           true,
		ConfigsUpdated: configsUpdated,
		Reason:         model.NewReasonStats(model.EndpointUpdate),
		Forced:         len(configsUpdated) == 0,
	}

	// trigger a full push
	s.XdsUpdater.ConfigUpdate(pushReq)
}

func (s *Controller) NotifyWorkloadInstanceHandlers(wi *model.WorkloadInstance, event model.Event) {
	for _, h := range s.workloadHandlers {
		h(wi, event)
	}
}

// getUpdatedConfigs returns related service entries when full push
func getUpdatedConfigs(services []*model.Service) sets.Set[model.ConfigKey] {
	configsUpdated := sets.NewWithLength[model.ConfigKey](len(services))
	for _, svc := range services {
		configsUpdated.Insert(model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      string(svc.Hostname),
			Namespace: svc.Attributes.Namespace,
		})
	}
	return configsUpdated
}

// serviceEntryHandler defines the handler for service entries
func (s *Controller) serviceEntryHandler(old, curr config.Config, event model.Event) {
	log.Debugf("Handle event %s for service entry %s/%s", event, curr.Namespace, curr.Name)
	currentServiceEntry := curr.Spec.(*networking.ServiceEntry)
	cs := convertServices(curr)
	key := curr.NamespacedName()

	s.mutex.Lock()
	// If it is add/delete event we should always do a full push. If it is update event, we should do full push,
	// only when services have changed - otherwise, just push endpoint updates.
	var addedSvcs, deletedSvcs, updatedSvcs, unchangedSvcs []*model.Service
	switch event {
	case model.EventUpdate:
		addedSvcs, deletedSvcs, updatedSvcs, unchangedSvcs = servicesDiff(s.services.getServices(key), cs)
		oldServiceEntry := old.Spec.(*networking.ServiceEntry)
		// Also check if target ports are changed since they are not included in `model.Service`
		if !slices.EqualFunc(oldServiceEntry.Ports, currentServiceEntry.Ports, func(a, b *networking.ServicePort) bool {
			return a.TargetPort == b.TargetPort
		}) {
			// Note: If the length of ports is changed, unchangedSvcs will be nil, this is an no-op
			updatedSvcs = append(updatedSvcs, unchangedSvcs...)
			unchangedSvcs = nil
		}
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

	serviceInstancesByConfig, serviceInstances := s.buildServiceInstances(curr, cs)
	oldInstances := s.serviceInstances.getServiceEntryInstances(key)
	for configKey, old := range oldInstances {
		s.serviceInstances.deleteInstanceKeys(configKeyWithParent{configKey: configKey, parent: key}, old)
	}
	if event == model.EventDelete {
		s.serviceInstances.deleteAllServiceEntryInstances(key)
	} else {
		// Update the indexes with new instances.
		for ckey, value := range serviceInstancesByConfig {
			s.serviceInstances.addInstances(configKeyWithParent{configKey: ckey, parent: key}, value)
		}
		s.serviceInstances.updateServiceEntryInstances(key, serviceInstancesByConfig)
	}

	shard := model.ShardKeyFromRegistry(s)

	configsUpdated := sets.NewWithLength[model.ConfigKey](len(addedSvcs) + len(updatedSvcs) + len(deletedSvcs) + len(unchangedSvcs))
	for _, svc := range addedSvcs {
		s.XdsUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, model.EventAdd)
		configsUpdated.Insert(makeConfigKey(svc))
	}

	for _, svc := range updatedSvcs {
		s.XdsUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, model.EventUpdate)
		configsUpdated.Insert(makeConfigKey(svc))
	}
	// If service entry is deleted, call SvcUpdate to cleanup endpoint shards for services.
	for _, svc := range deletedSvcs {
		instanceKey := instancesKey{namespace: svc.Attributes.Namespace, hostname: svc.Hostname}
		// There can be multiple service entries of same host reside in same namespace.
		// Delete endpoint shards only if there are no service instances.
		if s.serviceInstances.countByKey(instanceKey) == 0 {
			s.XdsUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, model.EventDelete)
		} else {
			// If there are some endpoints remaining for the host, add svc to updatedSvcs to trigger eds cache update
			updatedSvcs = append(updatedSvcs, svc)
		}
		configsUpdated.Insert(makeConfigKey(svc))
	}

	// If a service is updated and is not part of updatedSvcs, that means its endpoints might have changed.
	// If this service entry had endpoints with IPs (i.e. resolution STATIC), then we do EDS update.
	// If the service entry had endpoints with FQDNs (i.e. resolution DNS), then we need to do
	// full push (as fqdn endpoints go via strict_dns clusters in cds).
	if len(unchangedSvcs) > 0 {
		if isDNSTypeServiceEntry(currentServiceEntry) {
			for _, svc := range unchangedSvcs {
				configsUpdated.Insert(makeConfigKey(svc))
			}
		}
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
	allServices = append(allServices, addedSvcs...)
	allServices = append(allServices, updatedSvcs...)
	allServices = append(allServices, unchangedSvcs...)

	// non dns service instances
	keys := sets.NewWithLength[instancesKey](len(allServices))
	for _, svc := range allServices {
		keys.Insert(instancesKey{hostname: svc.Hostname, namespace: curr.Namespace})
	}

	s.queueEdsEvent(keys, false)

	// TODO: if len(configsUpdated) == 0 we're firing a Forced push for compatibility because no configs have been updated,
	// we should actually skip the push.
	pushReq := &model.PushRequest{
		Full:           true,
		ConfigsUpdated: configsUpdated,
		Reason:         model.NewReasonStats(model.ServiceUpdate),
		Forced:         len(configsUpdated) == 0,
	}
	s.XdsUpdater.ConfigUpdate(pushReq)
}

// all of this function can be executed under a read lock and can avoid a write lock for most cases
func (s *Controller) affectedServiceEntries(wi *model.WorkloadInstance, event model.Event) []config.Config {
	var oldWi *model.WorkloadInstance
	// Used to indicate if this event was fired for a pod->workloadentry conversion
	// and that the event can be ignored due to no relevant change in the workloadentry
	redundantEventForPod := false

	// Used to indicate if the wi labels changed and we need to recheck all instances
	labelsChanged := false

	s.mutex.RLock()
	oldWi = s.workloadInstances.Get(wi)
	switch event {
	case model.EventDelete:
		redundantEventForPod = oldWi == nil
	default: // add or update
		if oldWi != nil {
			// If multiple k8s services select the same pod or a service has multiple ports,
			// we may be getting multiple events ignore them as we only care about the Endpoint IP itself.
			if model.WorkloadInstancesEqual(oldWi, wi) {
				// ignore the update as nothing has changed
				redundantEventForPod = true
			}
			// Check if the old labels still match the new labels. If they don't then we need to
			// refresh the list of instances for this wi
			if !oldWi.Endpoint.Labels.Equals(wi.Endpoint.Labels) {
				labelsChanged = true
			}
		}
	}
	s.mutex.RUnlock()

	if redundantEventForPod {
		return nil
	}

	// We will only select entries in the same namespace
	cfgs := s.store.List(gvk.ServiceEntry, wi.Namespace)
	if len(cfgs) == 0 {
		return nil
	}

	res := make([]config.Config, 0, len(cfgs))
	for _, cfg := range cfgs {
		se := cfg.Spec.(*networking.ServiceEntry)
		if se.WorkloadSelector == nil || (!labelsChanged && !labels.Instance(se.WorkloadSelector.Labels).Match(wi.Endpoint.Labels)) {
			// If the labels didn't change. And the new SE doesn't match then the old didn't match either and we can skip processing it.
			continue
		}

		res = append(res, cfg)
	}

	return res
}

// WorkloadInstanceHandler defines the handler for service instances generated by other registries
func (s *Controller) WorkloadInstanceHandler(wi *model.WorkloadInstance, event model.Event) {
	log.Debugf("Handle event %s for workload instance (%s/%v) in namespace %s", event,
		wi.Kind, wi.Endpoint.Addresses, wi.Namespace)
	key := configKey{
		kind:      podConfigType,
		name:      wi.Name,
		namespace: wi.Namespace,
	}

	cfgs := s.affectedServiceEntries(wi, event)
	if len(cfgs) == 0 {
		return
	}

	var addressesToDelete []string
	var oldWi *model.WorkloadInstance

	s.mutex.Lock()
	// this is from a pod. Store it in separate map so that
	// the refreshIndexes function can use these as well as the store ones.
	switch event {
	case model.EventDelete:
		s.workloadInstances.Delete(wi)
	default: // add or update
		if oldWi = s.workloadInstances.Insert(wi); oldWi != nil {
			if oldWi.Endpoint.FirstAddressOrNil() != wi.Endpoint.FirstAddressOrNil() {
				addressesToDelete = oldWi.Endpoint.Addresses
			}
		}
	}

	instances := []*model.ServiceInstance{}
	instancesDeleted := []*model.ServiceInstance{}
	configsUpdated := sets.New[model.ConfigKey]()
	fullPush := false
	for _, cfg := range cfgs {
		se := cfg.Spec.(*networking.ServiceEntry)
		cpKey := configKeyWithParent{configKey: key, parent: config.NamespacedName(cfg)}

		// If we are here, then there are 3 possible cases :
		// Case 1 : The new wi is a subset of se
		// Case 2 : The labelsChanged and the new wi is still a subset of se
		// Case 3 : The labelsChanged and the new wi is NOT a subset of se anymore

		seNamespacedName := cfg.NamespacedName()
		services := s.services.getServices(seNamespacedName)
		currInstance := convertWorkloadInstanceToServiceInstance(wi, services, se)

		// We check if the wi is still a subset of se. This would cover Case 1 and Case 2 from above.
		if labels.Instance(se.WorkloadSelector.Labels).Match(wi.Endpoint.Labels) {
			// If the workload instance still matches. We take care of the possible events.
			instances = append(instances, currInstance...)
			if len(addressesToDelete) > 0 {
				cfgInstancesDeleted := []*model.ServiceInstance{}
				for _, i := range currInstance {
					di := i.DeepCopy()
					di.Endpoint.Addresses = addressesToDelete
					cfgInstancesDeleted = append(cfgInstancesDeleted, di)
				}
				s.serviceInstances.deleteInstanceKeys(cpKey, cfgInstancesDeleted)
				s.serviceInstances.deleteServiceEntryInstances(seNamespacedName, key)
			} else if event == model.EventDelete {
				s.serviceInstances.deleteServiceEntryInstances(seNamespacedName, key)
			} else {
				s.serviceInstances.updateServiceEntryInstancesPerConfig(seNamespacedName, key, currInstance)
			}
			if event == model.EventDelete {
				s.serviceInstances.deleteInstanceKeys(cpKey, currInstance)
			} else {
				s.serviceInstances.updateInstances(cpKey, currInstance)
			}
			// If serviceentry's resolution is DNS, make a full push
			// TODO: maybe cds?
			if isDNSTypeServiceEntry(se) &&
				se.WorkloadSelector != nil {

				fullPush = true
				for _, inst := range currInstance {
					configsUpdated[model.ConfigKey{
						Kind:      kind.ServiceEntry,
						Name:      string(inst.Service.Hostname),
						Namespace: cfg.Namespace,
					}] = struct{}{}
				}
			}
		} else if labels.Instance(se.WorkloadSelector.Labels).Match(oldWi.Endpoint.Labels) {
			// If we're here, it means that the labels changed and the new labels don't match the SE anymore (Case 3 from above) and the oldWi did
			// match the SE.
			// Since the instance doesn't match the SE anymore. We remove it from the list.
			oldInstance := convertWorkloadInstanceToServiceInstance(oldWi, services, se)
			instancesDeleted = append(instancesDeleted, oldInstance...)
			s.serviceInstances.deleteInstanceKeys(cpKey, oldInstance)
			s.serviceInstances.deleteServiceEntryInstances(seNamespacedName, key)
		}
	}

	s.mutex.Unlock()

	s.edsUpdate(append(instances, instancesDeleted...), true)

	// ServiceEntry with WorkloadEntry results in STRICT_DNS cluster with hardcoded endpoints
	// need to update CDS to refresh endpoints
	// https://github.com/istio/istio/issues/39505
	if fullPush {
		log.Debugf("Full push triggered during event %s for workload instance (%s/%v) in namespace %s", event,
			wi.Kind, wi.Endpoint.Addresses, wi.Namespace)
		// TODO: if len(configsUpdated) == 0 we're firing a Forced push for compatibility because no configs have been updated,
		// we should actually skip the push.
		pushReq := &model.PushRequest{
			Full:           true,
			ConfigsUpdated: configsUpdated,
			Reason:         model.NewReasonStats(model.EndpointUpdate),
			Forced:         len(configsUpdated) == 0,
		}
		s.XdsUpdater.ConfigUpdate(pushReq)
	}
}

func (s *Controller) Provider() provider.ID {
	return provider.External
}

func (s *Controller) Cluster() cluster.ID {
	return s.clusterID
}

// AppendServiceHandler adds service resource event handler. Service Entries does not use these handlers.
func (s *Controller) AppendServiceHandler(_ model.ServiceHandler) {}

// AppendWorkloadHandler adds instance event handler. Service Entries does not use these handlers.
func (s *Controller) AppendWorkloadHandler(h func(*model.WorkloadInstance, model.Event)) {
	s.workloadHandlers = append(s.workloadHandlers, h)
}

// Run is used by some controllers to execute background jobs after init is done.
func (s *Controller) Run(stopCh <-chan struct{}) {
	s.edsQueue.Run(stopCh)
}

// HasSynced always returns true for SE
func (s *Controller) HasSynced() bool {
	return true
}

// Services list declarations of all services in the system
func (s *Controller) Services() []*model.Service {
	s.mutex.Lock()
	allServices := s.services.getAllServices()
	if s.services.allocateNeeded {
		autoAllocateIPs(allServices)
		s.services.allocateNeeded = false
	}
	s.mutex.Unlock()
	for i, svc := range allServices {
		// shallow copy, copy `AutoAllocatedIPv4Address` and `AutoAllocatedIPv6Address`
		// if return the pointer directly, there will be a race with `BuildNameTable`
		// nolint: govet
		shallowSvc := *svc
		allServices[i] = &shallowSvc
	}
	return allServices
}

// GetService retrieves a service by host name if it exists.
// NOTE: The service entry implementation is used only for tests.
func (s *Controller) GetService(hostname host.Name) *model.Service {
	if s.workloadEntryController {
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

// ResyncEDS will do a full EDS update. This is needed for some tests where we have many configs loaded without calling
// the config handlers.
// This should probably not be used in production code.
func (s *Controller) ResyncEDS() {
	s.mutex.RLock()
	allInstances := s.serviceInstances.getAll()
	s.mutex.RUnlock()
	s.edsUpdate(allInstances, true)
	// HACK to workaround Service syncing after WorkloadEntry: https://github.com/istio/istio/issues/45114
	s.workloadInstances.ForEach(func(wi *model.WorkloadInstance) {
		if wi.Kind == model.WorkloadEntryKind {
			s.NotifyWorkloadInstanceHandlers(wi, model.EventAdd)
		}
	})
}

// edsUpdate triggers an EDS push serially such that we can prevent all instances
// got at t1 can accidentally override that got at t2 if multiple threads are
// running this function. Queueing ensures latest updated wins.
func (s *Controller) edsUpdate(instances []*model.ServiceInstance, pushEds bool) {
	// Find all keys we need to lookup
	keys := sets.NewWithLength[instancesKey](len(instances))
	for _, i := range instances {
		keys.Insert(makeInstanceKey(i))
	}
	s.queueEdsEvent(keys, pushEds)
}

// queueEdsEvent processes eds events sequentially for the passed keys and invokes the passed function.
func (s *Controller) queueEdsEvent(keys sets.Set[instancesKey], pushEds bool) {
	// wait for the cache update finished
	waitCh := make(chan struct{})
	// trigger update eds endpoint shards
	s.edsQueue.Push(func() error {
		defer close(waitCh)
		xdsUpdateFn := s.XdsUpdater.EDSCacheUpdate
		if pushEds {
			xdsUpdateFn = s.XdsUpdater.EDSUpdate
		}
		s.doEdsUpdate(keys, xdsUpdateFn)
		return nil
	})
	select {
	case <-waitCh:
		return
	// To prevent goroutine leak in tests
	// in case the queue is stopped but the task has not been executed..
	case <-s.edsQueue.Closed():
		return
	}
}

func (s *Controller) doEdsUpdate(keys sets.Set[instancesKey], xdsUpdateFn model.EdsUpdateFn) {
	endpoints := s.buildEndpoints(keys)
	shard := model.ShardKeyFromRegistry(s)

	for k := range keys {
		if eps, ok := endpoints[k]; ok {
			// Update the cache with the generated endpoints.
			xdsUpdateFn(shard, string(k.hostname), k.namespace, eps)
		} else {
			// Handle deletions by sending a nil endpoints update.
			xdsUpdateFn(shard, string(k.hostname), k.namespace, nil)
		}
	}
}

// buildEndpoints builds endpoints for the instance keys.
func (s *Controller) buildEndpoints(keys map[instancesKey]struct{}) map[instancesKey][]*model.IstioEndpoint {
	var endpoints map[instancesKey][]*model.IstioEndpoint
	allInstances := []*model.ServiceInstance{}
	s.mutex.RLock()
	for key := range keys {
		i := s.serviceInstances.getByKey(key)
		allInstances = append(allInstances, i...)
	}
	s.mutex.RUnlock()

	if len(allInstances) > 0 {
		endpoints = make(map[instancesKey][]*model.IstioEndpoint)
		for _, instance := range allInstances {
			key := makeInstanceKey(instance)
			endpoints[key] = append(endpoints[key], instance.Endpoint)
		}

	}
	return endpoints
}

// GetProxyServiceTargets lists service targets co-located with a given proxy
// NOTE: The service objects in these instances do not have the auto allocated IP set.
func (s *Controller) GetProxyServiceTargets(node *model.Proxy) []model.ServiceTarget {
	out := make([]model.ServiceTarget, 0)
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, ip := range node.IPAddresses {
		instances := s.serviceInstances.getByIP(ip)
		for _, i := range instances {
			// Insert all instances for this IP for services within the same namespace. This ensures we
			// match Kubernetes logic where Services do not cross namespace boundaries and avoids
			// possibility of other namespaces inserting service instances into namespaces they do not
			// control.
			if node.Metadata.Namespace == "" || i.Service.Attributes.Namespace == node.Metadata.Namespace {
				out = append(out, model.ServiceInstanceToTarget(i))
			}
		}
	}
	return out
}

func (s *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Instance {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, ip := range proxy.IPAddresses {
		instances := s.serviceInstances.getByIP(ip)
		for _, i := range instances {
			// Insert first instances for this IP for services within the same namespace. This ensures we
			// match Kubernetes logic where Services do not cross namespace boundaries and avoids
			// possibility of other namespaces inserting service instances into namespaces they do not
			// control.
			// All instances should have the same labels so we just return the first
			if proxy.Metadata.Namespace == "" || i.Service.Attributes.Namespace == proxy.Metadata.Namespace {
				return i.Endpoint.Labels
			}
		}
	}
	return nil
}

func (s *Controller) NetworkGateways() []model.NetworkGateway {
	// TODO implement mesh networks loading logic from kube controller if needed
	return nil
}

func (s *Controller) MCSServices() []model.MCSServiceInfo {
	return nil
}

func servicesDiff(os []*model.Service, ns []*model.Service) ([]*model.Service, []*model.Service, []*model.Service, []*model.Service) {
	var added, deleted, updated, unchanged []*model.Service

	oldServiceHosts := make(map[host.Name]*model.Service, len(os))
	for _, s := range os {
		oldServiceHosts[s.Hostname] = s
	}

	for _, s := range ns {
		oldSvc, ok := oldServiceHosts[s.Hostname]
		if ok && s.Equals(oldSvc) {
			unchanged = append(unchanged, s)
		} else if ok {
			updated = append(updated, s)
		} else {
			added = append(added, s)
		}
		delete(oldServiceHosts, s.Hostname)
	}
	deleted = maps.Values(oldServiceHosts)

	return added, deleted, updated, unchanged
}

// Automatically allocates IPs for service entry services WITHOUT an
// address field if the hostname is not a wildcard, or when resolution
// is not NONE. The IPs are allocated from the reserved Class E subnet
// (240.240.0.0/16) that is not reachable outside the pod or reserved
// Benchmarking IP range (2001:2::/48) in RFC5180. When DNS
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
func autoAllocateIPs(services []*model.Service) []*model.Service {
	// if we are using the IP Autoallocate controller then we can short circuit this
	if features.EnableIPAutoallocate {
		return services
	}
	hashedServices := make([]*model.Service, maxIPs)
	hash := fnv.New32a()
	// First iterate through the range of services and determine its position by hash
	// so that we can deterministically allocate an IP.
	// We use "Double Hashning" for collision detection.
	// The hash algorithm is
	// - h1(k) = Sum32 hash of the service key (namespace + "/" + hostname)
	// - Check if we have an empty slot for h1(x) % MAXIPS. Use it if available.
	// - If there is a collision, apply second hash i.e. h2(x) = PRIME - (Key % PRIME)
	//   where PRIME is the max prime number below MAXIPS.
	// - Calculate new hash iteratively till we find an empty slot with (h1(k) + i*h2(k)) % MAXIPS
	j := 0
	for _, svc := range services {
		// we can allocate IPs only if
		// 1. the service has resolution set to static/dns. We cannot allocate
		//   for NONE because we will not know the original DST IP that the application requested.
		// 2. the address is not set (0.0.0.0)
		// 3. the hostname is not a wildcard
		if svc.DefaultAddress == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() &&
			svc.Resolution != model.Passthrough {
			if j >= maxIPs {
				log.Errorf("out of IPs to allocate for service entries. maxips:= %d", maxIPs)
				break
			}
			// First hash is calculated by hashing the service key i.e. (namespace + "/" + hostname).
			hash.Write([]byte(makeServiceKey(svc)))
			s := hash.Sum32()
			firstHash := s % uint32(maxIPs)
			// Check if there is a service with this hash first. If there is no service
			// at this location - then we can safely assign this position for this service.
			if hashedServices[firstHash] == nil {
				hashedServices[firstHash] = svc
			} else {
				// This means we have a collision. Resolve collision by "DoubleHashing".
				i := uint32(1)
				secondHash := uint32(prime) - (s % uint32(prime))
				for {
					nh := (s + i*secondHash) % uint32(maxIPs-1)
					if hashedServices[nh] == nil {
						hashedServices[nh] = svc
						break
					}
					i++
				}
			}
			hash.Reset()
			j++
		}
	}

	x := 0
	hnMap := make(map[string]octetPair)
	for _, svc := range hashedServices {
		x++
		if svc == nil {
			// There is no service in the slot. Just increment x and move forward.
			continue
		}
		n := makeServiceKey(svc)
		// To avoid allocating 240.240.(i).255, if X % 255 is 0, increment X.
		// For example, when X=510, the resulting IP would be 240.240.2.0 (invalid)
		// So we bump X to 511, so that the resulting IP is 240.240.2.1
		if x%255 == 0 {
			x++
		}
		if v, ok := hnMap[n]; ok {
			log.Debugf("Reuse IP for domain %s", n)
			setAutoAllocatedIPs(svc, v)
		} else {
			var thirdOctect, fourthOctect int
			thirdOctect = x / 255
			fourthOctect = x % 255
			pair := octetPair{thirdOctect, fourthOctect}
			setAutoAllocatedIPs(svc, pair)
			hnMap[n] = pair
		}
	}
	return services
}

func makeServiceKey(svc *model.Service) string {
	return svc.Attributes.Namespace + "/" + svc.Hostname.String()
}

func setAutoAllocatedIPs(svc *model.Service, octets octetPair) {
	a := octets.thirdOctet
	b := octets.fourthOctet
	svc.AutoAllocatedIPv4Address = fmt.Sprintf("240.240.%d.%d", a, b)
	if a == 0 {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x", b)
	} else {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x%x", a, b)
	}
}

func makeConfigKey(svc *model.Service) model.ConfigKey {
	return model.ConfigKey{
		Kind:      kind.ServiceEntry,
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

func (s *Controller) buildServiceInstances(
	curr config.Config,
	services []*model.Service,
) (map[configKey][]*model.ServiceInstance, []*model.ServiceInstance) {
	currentServiceEntry := curr.Spec.(*networking.ServiceEntry)
	var serviceInstances []*model.ServiceInstance
	serviceInstancesByConfig := map[configKey][]*model.ServiceInstance{}
	// for service entry with labels
	if currentServiceEntry.WorkloadSelector != nil {
		selector := workloadinstances.ByServiceSelector(curr.Namespace, currentServiceEntry.WorkloadSelector.Labels)
		workloadInstances := workloadinstances.FindAllInIndex(s.workloadInstances, selector)
		for _, wi := range workloadInstances {
			if wi.DNSServiceEntryOnly && !isDNSTypeServiceEntry(currentServiceEntry) {
				log.Debugf("skip selecting workload instance %v/%v for DNS service entry %v", wi.Namespace, wi.Name,
					currentServiceEntry.Hosts)
				continue
			}
			instances := convertWorkloadInstanceToServiceInstance(wi, services, currentServiceEntry)
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
