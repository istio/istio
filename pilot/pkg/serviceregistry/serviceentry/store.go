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
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/util/sets"
)

type hostPort struct {
	host string
	port int
}

// stores all the service instances from SE, WLE and pods
type serviceInstancesStore struct {
	ip2instance map[string][]*model.ServiceInstance
	// service instances by hostname -> config
	instances map[instancesKey]map[configKey][]*model.ServiceInstance
	// instances only for serviceentry
	instancesBySE map[types.NamespacedName]map[configKey][]*model.ServiceInstance
	// instancesByHostAndPort tells whether the host has instances.
	// This is used to validate that we only have one instance for DNS_ROUNDROBIN_LB.
	instancesByHostAndPort sets.Set[hostPort]
}

func (s *serviceInstancesStore) getByIP(ip string) []*model.ServiceInstance {
	return s.ip2instance[ip]
}

func (s *serviceInstancesStore) getAll() []*model.ServiceInstance {
	all := []*model.ServiceInstance{}
	for _, instances := range s.ip2instance {
		all = append(all, instances...)
	}
	return all
}

func (s *serviceInstancesStore) getByKey(key instancesKey) []*model.ServiceInstance {
	all := []*model.ServiceInstance{}
	for _, instances := range s.instances[key] {
		all = append(all, instances...)
	}
	return all
}

func (s *serviceInstancesStore) deleteInstances(key configKey, instances []*model.ServiceInstance) {
	for _, i := range instances {
		ikey := makeInstanceKey(i)
		hostPort := hostPort{ikey.hostname.String(), i.ServicePort.Port}
		s.instancesByHostAndPort.Delete(hostPort)
		delete(s.instances[makeInstanceKey(i)], key)
		delete(s.ip2instance, i.Endpoint.Address)
	}
}

// addInstances add the instances to the store.
func (s *serviceInstancesStore) addInstances(key configKey, instances []*model.ServiceInstance) {
	for _, instance := range instances {
		ikey := makeInstanceKey(instance)
		hostPort := hostPort{ikey.hostname.String(), instance.ServicePort.Port}
		// For DNSRoundRobinLB resolution type, check if service instances already exist and do not add
		// if it already exist. This can happen if two Service Entries are created with same host name,
		// resolution as DNS_ROUND_ROBIN and with same/different endpoints.
		if instance.Service.Resolution == model.DNSRoundRobinLB &&
			s.instancesByHostAndPort.Contains(hostPort) {
			log.Debugf("skipping service %s from service entry %s with DnsRoundRobinLB. A service entry with the same host "+
				"already exists. Only one locality lb end point is allowed for DnsRoundRobinLB services.",
				ikey.hostname, key.name+"/"+key.namespace)
			continue
		}
		if _, f := s.instances[ikey]; !f {
			s.instances[ikey] = map[configKey][]*model.ServiceInstance{}
		}
		s.instancesByHostAndPort.Insert(hostPort)
		s.instances[ikey][key] = append(s.instances[ikey][key], instance)
		if instance.Endpoint.Address != "" {
			s.ip2instance[instance.Endpoint.Address] = append(s.ip2instance[instance.Endpoint.Address], instance)
		}
	}
}

func (s *serviceInstancesStore) updateInstances(key configKey, instances []*model.ServiceInstance) {
	// first delete
	s.deleteInstances(key, instances)

	// second add
	s.addInstances(key, instances)
}

func (s *serviceInstancesStore) getServiceEntryInstances(key types.NamespacedName) map[configKey][]*model.ServiceInstance {
	return s.instancesBySE[key]
}

func (s *serviceInstancesStore) updateServiceEntryInstances(key types.NamespacedName, instances map[configKey][]*model.ServiceInstance) {
	s.instancesBySE[key] = instances
}

func (s *serviceInstancesStore) updateServiceEntryInstancesPerConfig(key types.NamespacedName, cKey configKey, instances []*model.ServiceInstance) {
	if s.instancesBySE[key] == nil {
		s.instancesBySE[key] = map[configKey][]*model.ServiceInstance{}
	}
	s.instancesBySE[key][cKey] = instances
}

func (s *serviceInstancesStore) deleteServiceEntryInstances(key types.NamespacedName, cKey configKey) {
	delete(s.instancesBySE[key], cKey)
}

func (s *serviceInstancesStore) deleteAllServiceEntryInstances(key types.NamespacedName) {
	delete(s.instancesBySE, key)
}

// stores all the services converted from serviceEntries
type serviceStore struct {
	// services keeps track of all services - mainly used to return from Services() to avoid reconversion.
	servicesBySE   map[types.NamespacedName][]*model.Service
	allocateNeeded bool
}

// getAllServices return all the services.
func (s *serviceStore) getAllServices() []*model.Service {
	var out []*model.Service
	for _, svcs := range s.servicesBySE {
		out = append(out, svcs...)
	}
	return model.SortServicesByCreationTime(out)
}

func (s *serviceStore) getServices(key types.NamespacedName) []*model.Service {
	return s.servicesBySE[key]
}

func (s *serviceStore) deleteServices(key types.NamespacedName) {
	delete(s.servicesBySE, key)
}

func (s *serviceStore) updateServices(key types.NamespacedName, services []*model.Service) {
	s.servicesBySE[key] = services
	s.allocateNeeded = true
}
