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
	"sync"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
)

// stores all the service instances from SE, WLE and pods
type serviceInstancesStore struct {
	mutex       sync.RWMutex
	ip2instance map[string][]*model.ServiceInstance
	// // service instances by config keys
	// instancesByKey map[configKey][]*model.ServiceInstance
	// // Endpoints hostname -> config
	// instances map[instancesKey][]configKey
	instances map[instancesKey]map[configKey][]*model.ServiceInstance

	// instances only for serviceentry
	instancesBySE map[string]map[configKey][]*model.ServiceInstance
}

func (s *serviceInstancesStore) getByIP(ip string) []*model.ServiceInstance {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.ip2instance[ip]
}

func (s *serviceInstancesStore) getAll() []*model.ServiceInstance {
	all := []*model.ServiceInstance{}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, instances := range s.ip2instance {
		all = append(all, instances...)
	}
	return all
}

func (s *serviceInstancesStore) getByKey(key instancesKey) []*model.ServiceInstance {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	all := []*model.ServiceInstance{}
	for _, instances := range s.instances[key] {
		all = append(all, instances...)
	}
	return all
}

func (s *serviceInstancesStore) deleteInstances(instances []*model.ServiceInstance) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, i := range instances {
		delete(s.instances, makeInstanceKey(i))
		delete(s.ip2instance, i.Endpoint.Address)
	}
}

func (s *serviceInstancesStore) deleteInstancesFor(key configKey, instances []*model.ServiceInstance) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, i := range instances {
		delete(s.instances[makeInstanceKey(i)], key)
		delete(s.ip2instance, i.Endpoint.Address)
	}
}

// updateInstances updates the instance data to the store.
func (s *serviceInstancesStore) updateInstances(key configKey, instances []*model.ServiceInstance) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, instance := range instances {
		ikey := makeInstanceKey(instance)
		if _, f := s.instances[ikey]; !f {
			s.instances[ikey] = map[configKey][]*model.ServiceInstance{}
		}
		s.instances[ikey][key] = append(s.instances[ikey][key], instance)
		s.ip2instance[instance.Endpoint.Address] = append(s.ip2instance[instance.Endpoint.Address], instance)
	}
}

func (s *serviceInstancesStore) updateServiceEntryInstances(key string, instances map[configKey][]*model.ServiceInstance) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.instancesBySE[key] = instances
}

func (s *serviceInstancesStore) updateServiceEntryInstancesPerConfig(key string, cKey configKey, instances []*model.ServiceInstance) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.instancesBySE[key] == nil {
		s.instancesBySE[key] = map[configKey][]*model.ServiceInstance{}
	}
	s.instancesBySE[key][cKey] = instances
}

func (s *serviceInstancesStore) deleteServiceEntryInstances(key string, cKey configKey) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.instancesBySE[key] != nil {
		delete(s.instancesBySE[key], cKey)
	}
}

func (s *serviceInstancesStore) deleteAllServiceEntryInstances(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.instancesBySE, key)
}

func (s *serviceInstancesStore) getByServiceEntry(key string) (out []*model.ServiceInstance) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for _, instances := range s.instancesBySE[key] {
		out = append(out, instances...)
	}
	return out
}

// stores all the workload instances from pods
type workloadInstancesStore struct {
	mutex sync.RWMutex
	// workload instances from kubernetes pods - map of ip -> workload instance
	workloadInstancesByIP map[string]*model.WorkloadInstance
	// Stores a map of workload instance name/namespace to address
	workloadInstancesIPsByName map[string]string
}

func (w *workloadInstancesStore) getByIP(ip string) *model.WorkloadInstance {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.workloadInstancesByIP[ip]
}

func (w *workloadInstancesStore) list(namespace string, selector labels.Collection) (out []*model.WorkloadInstance) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	for _, wi := range w.workloadInstancesByIP {
		if wi.Endpoint.Namespace != namespace {
			continue
		}
		if selector.HasSubsetOf(wi.Endpoint.Labels) {
			out = append(out, wi)
		}
	}
	return out
}

func (w *workloadInstancesStore) getIPByName(name string) string {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.workloadInstancesIPsByName[name]
}

func (w *workloadInstancesStore) delete(name string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	ip := w.workloadInstancesIPsByName[name]
	delete(w.workloadInstancesByIP, ip)
	delete(w.workloadInstancesIPsByName, name)
}

func (w *workloadInstancesStore) update(wi *model.WorkloadInstance) {
	if wi == nil {
		return
	}
	key := keyFunc(wi.Namespace, wi.Name)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	// delete workloadInstancesByIP in case workloadEntry IP changes.
	ip := w.workloadInstancesIPsByName[key]
	delete(w.workloadInstancesByIP, ip)
	w.workloadInstancesByIP[wi.Endpoint.Address] = wi
	w.workloadInstancesIPsByName[key] = wi.Endpoint.Address
}

// stores all the services and serviceEntries
type serviceStore struct {
	mutex sync.RWMutex
	// services keeps track of all services - mainly used to return from Services() to avoid reconversion.
	servicesBySE      map[string][]*model.Service
	seByWorkloadEntry map[configKey][]types.NamespacedName
}

func (s *serviceStore) getServiceEntries(key configKey) []types.NamespacedName {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.seByWorkloadEntry[key]
}

func (s *serviceStore) getServices(key string) []*model.Service {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.servicesBySE[key]
}

func (s *serviceStore) deleteServices(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.servicesBySE, key)
}

func (s *serviceStore) updateServices(key string, services []*model.Service) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.servicesBySE[key] = services
}

func (s *serviceStore) deleteServiceEntry(key configKey) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.seByWorkloadEntry, key)
}

func (s *serviceStore) updateServiceEntry(key configKey, ses []types.NamespacedName) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.seByWorkloadEntry[key] = ses
}
