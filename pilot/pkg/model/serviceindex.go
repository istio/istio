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

package model

import (
	"sort"

	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/host"
)

// NamespaceIndex maps namespaces to Services.
type NamespaceIndex interface {
	// Len indicates the number of namespace entries.
	Len() int

	// Service returns the Servie for the given namespace.
	Service(ns string) *Service

	// Namespaces returns all namespaces containing entries, in ascending order.
	Namespaces(includeFilter func(ns string, s *Service) bool) []string
}

type emptyNamespaceIndex struct{}

func (ni emptyNamespaceIndex) Len() int {
	return 0
}

func (ni emptyNamespaceIndex) Service(string) *Service {
	return nil
}

func (ni emptyNamespaceIndex) Namespaces(func(ns string, s *Service) bool) []string {
	return nil
}

type namespaceIndexImpl map[string]*Service

func (ni namespaceIndexImpl) Len() int {
	return len(ni)
}

func (ni namespaceIndexImpl) Service(ns string) *Service {
	if ni == nil {
		return nil
	}
	return ni[ns]
}

var includeAllFilter = func(string, *Service) bool { return true }

func (ni namespaceIndexImpl) Namespaces(includeFilter func(ns string, s *Service) bool) []string {
	if len(ni) == 0 {
		return nil
	}

	if includeFilter == nil {
		includeFilter = includeAllFilter
	}
	out := make([]string, 0, len(ni))
	for ns, s := range ni {
		if includeFilter(ns, s) {
			out = append(out, ns)
		}
	}

	// Sort the result in ascending order.
	if len(out) > 1 {
		sort.Strings(out)
	}
	return out
}

type ServiceInstanceIndex interface {
	IsEmpty() bool
	Instances(port int) []*ServiceInstance
	ForEach(func(*ServiceInstance) bool)
}

type emptyServiceInstanceIndex struct{}

func (si emptyServiceInstanceIndex) IsEmpty() bool {
	return true
}

func (si emptyServiceInstanceIndex) Instances(int) []*ServiceInstance {
	return nil
}

func (si emptyServiceInstanceIndex) ForEach(func(*ServiceInstance) bool) {}

type serviceInstanceIndexImpl map[int][]*ServiceInstance

func (si serviceInstanceIndexImpl) IsEmpty() bool {
	for _, values := range si {
		if len(values) > 0 {
			return false
		}
	}
	return true
}

func (si serviceInstanceIndexImpl) Instances(port int) []*ServiceInstance {
	return si[port]
}

func (si serviceInstanceIndexImpl) ForEach(visitor func(*ServiceInstance) bool) {
	for _, instances := range si {
		for _, inst := range instances {
			if !visitor(inst) {
				return
			}
		}
	}
}

// ServiceIndex provides various methods for retrieving Service information.
type ServiceIndex interface {
	NamespacesForHost(h host.Name) NamespaceIndex
	InstancesForKey(serviceKey string) ServiceInstanceIndex
}

type serviceIndexImpl struct {
	// privateByNamespace are services that can reachable within the same namespace, with exportTo "."
	privateByNamespace map[string][]*Service
	// public are services reachable within the mesh with exportTo "*"
	public []*Service
	// exportedToNamespace are services that were made visible to this namespace
	// by an exportTo explicitly specifying this namespace.
	exportedToNamespace map[string][]*Service

	hostnameAndNamespace map[host.Name]namespaceIndexImpl

	// serviceInstanceIndexesByKey contains a map of service key and instances by port. It is stored here
	// to avoid recomputations during push. This caches instanceByPort calls with empty labels.
	// Call InstancesByPort directly when instances need to be filtered by actual labels.
	serviceInstanceIndexesByKey map[string]serviceInstanceIndexImpl
}

func (si *serviceIndexImpl) InstancesForKey(serviceKey string) ServiceInstanceIndex {
	out := si.serviceInstanceIndexesByKey[serviceKey]
	if out == nil {
		return emptyServiceInstanceIndex{}
	}
	return out
}

func (si *serviceIndexImpl) NamespacesForHost(h host.Name) NamespaceIndex {
	ni := si.hostnameAndNamespace[h]
	if ni == nil {
		return emptyNamespaceIndex{}
	}
	return ni
}

func (si *serviceIndexImpl) setServiceForHostAndNamespace(h host.Name, ns string, s *Service) {
	ni := si.hostnameAndNamespace[h]
	if ni == nil {
		ni = make(namespaceIndexImpl)
		si.hostnameAndNamespace[h] = ni
	}
	ni[ns] = s
}

func (si *serviceIndexImpl) setServiceInstancesForKeyAndPort(key string, port int, instances []*ServiceInstance) {
	index := si.serviceInstanceIndexesByKey[key]
	if index == nil {
		index = make(serviceInstanceIndexImpl)
		si.serviceInstanceIndexesByKey[key] = index
	}
	index[port] = instances
}

func (si *serviceIndexImpl) allNamespaces() sets.Set {
	namespaces := sets.NewSet()
	for _, nsMap := range si.hostnameAndNamespace {
		for ns := range nsMap {
			namespaces.Insert(ns)
		}
	}
	return namespaces
}

// TODO(nmittler): return interface.
func newServiceIndex() *serviceIndexImpl {
	return &serviceIndexImpl{
		public:                      []*Service{},
		privateByNamespace:          map[string][]*Service{},
		exportedToNamespace:         map[string][]*Service{},
		hostnameAndNamespace:        map[host.Name]namespaceIndexImpl{},
		serviceInstanceIndexesByKey: map[string]serviceInstanceIndexImpl{},
	}
}

func SetServiceIndexHostsAndNamespacesForTesting(si ServiceIndex, hostnameAndNamespace map[host.Name]map[string]*Service) {
	castSI := si.(*serviceIndexImpl)

	for k, v := range hostnameAndNamespace {
		castSI.hostnameAndNamespace[k] = v
	}
}

func SetServiceIndexServiceForHostAndNamespaceForTesting(si ServiceIndex, h host.Name, ns string, s *Service) {
	si.(*serviceIndexImpl).setServiceForHostAndNamespace(h, ns, s)
}

func AddServiceIndexPublicServicesForTesting(si ServiceIndex, services ...*Service) {
	castSI := si.(*serviceIndexImpl)
	castSI.public = append(castSI.public, services...)
}

func AddServiceIndexServiceInstancesForTesting(si ServiceIndex, service *Service, instances map[int][]*ServiceInstance) {
	castSI := si.(*serviceIndexImpl)
	svcKey := service.Key()
	for port, inst := range instances {
		index, exists := castSI.serviceInstanceIndexesByKey[svcKey]
		if !exists {
			index = make(serviceInstanceIndexImpl)
			castSI.serviceInstanceIndexesByKey[svcKey] = index
		}
		index[port] = append(index[port], inst...)
	}
}

func NewServiceIndexForPushContextForTesting(pc *PushContext) {
	pc.serviceIndex = newServiceIndex()
}
