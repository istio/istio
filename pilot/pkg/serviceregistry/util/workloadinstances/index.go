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

package workloadinstances

import (
	"sync"

	"istio.io/istio/pilot/pkg/model"
)

// Index reprensents an index over workload instances from workload entries.
//
// Indexes are thread-safe.
type Index interface {
	// Insert adds given workload instance to the index.
	Insert(*model.WorkloadInstance)
	// Delete removes given workload instance from the index.
	Delete(*model.WorkloadInstance)
	// GetByIP returns a list of all workload instances associated with a
	// given IP address. The list is ordered by namespace/name.
	//
	// There are several use cases where multiple workload instances might
	// have the same IP address:
	// 1) there are multiple Istio Proxies running on a single host, e.g.
	//    in 'router' mode or even in 'sidecar' mode.
	// 2) workload instances have the same IP but different networks
	GetByIP(string) []*model.WorkloadInstance
	// Empty returns whether the index is empty.
	Empty() bool
	// ForEach iterates over all workload instances in the index.
	ForEach(func(*model.WorkloadInstance))
}

// NewIndex returns a new Index instance.
func NewIndex() Index {
	return &index{
		keyToInstance: make(map[string]*model.WorkloadInstance),
		ipToKeys:      make(MultiValueMap),
	}
}

// index implements Index.
type index struct {
	mu sync.RWMutex
	// map of namespace/name -> workload instance
	keyToInstance map[string]*model.WorkloadInstance
	// map of ip -> set of namespace/name
	ipToKeys MultiValueMap
}

func indexKey(name, namespace string) string {
	return namespace + "/" + name
}

func indexKeyOf(wi *model.WorkloadInstance) string {
	return indexKey(wi.Name, wi.Namespace)
}

// Insert implements Index.
func (i *index) Insert(wi *model.WorkloadInstance) {
	i.mu.Lock()
	defer i.mu.Unlock()

	key := indexKeyOf(wi)
	// Check to see if the workload entry changed. If it did, clear the old entry
	previous := i.keyToInstance[key]
	if previous != nil && previous.Endpoint.Address != wi.Endpoint.Address {
		i.ipToKeys.Delete(previous.Endpoint.Address, key)
	}
	i.keyToInstance[key] = wi
	i.ipToKeys.Insert(wi.Endpoint.Address, key)
}

// Delete implements Index.
func (i *index) Delete(wi *model.WorkloadInstance) {
	i.mu.Lock()
	defer i.mu.Unlock()

	key := indexKeyOf(wi)
	i.ipToKeys.Delete(wi.Endpoint.Address, key)
	delete(i.keyToInstance, key)
}

// GetByIP implements Index.
func (i *index) GetByIP(ip string) []*model.WorkloadInstance {
	i.mu.RLock()
	defer i.mu.RUnlock()

	keys := i.ipToKeys[ip]
	if len(keys) == 0 {
		return nil
	}
	instances := make([]*model.WorkloadInstance, 0, len(keys))
	for _, key := range keys.SortedList() {
		if instance, exists := i.keyToInstance[key]; exists {
			instances = append(instances, instance)
		}
	}
	return instances
}

// Empty implements Index.
func (i *index) Empty() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return len(i.keyToInstance) == 0
}

// ForEach iterates over all workload instances in the index.
func (i *index) ForEach(fn func(*model.WorkloadInstance)) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	for _, instance := range i.keyToInstance {
		fn(instance)
	}
}
