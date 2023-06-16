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
	"istio.io/istio/pkg/util/sets"
)

// Index reprensents an index over workload instances from workload entries.
//
// Indexes are thread-safe.
type Index interface {
	// Insert adds/updates given workload instance to the index.
	//
	// Returns previous value in the index, or nil otherwise.
	Insert(*model.WorkloadInstance) *model.WorkloadInstance
	// Delete removes given workload instance from the index.
	//
	// Returns value removed from the index, or nil otherwise.
	Delete(*model.WorkloadInstance) *model.WorkloadInstance
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

// indexKey returns index key for a given workload instance.
func indexKey(wi *model.WorkloadInstance) string {
	return wi.Namespace + "/" + wi.Name
}

// NewIndex returns a new Index instance.
func NewIndex() Index {
	return &index{
		keyFunc:       indexKey,
		keyToInstance: make(map[string]*model.WorkloadInstance),
		ipToKeys:      make(MultiValueMap),
	}
}

// index implements Index.
type index struct {
	mu sync.RWMutex
	// key function
	keyFunc func(*model.WorkloadInstance) string
	// map of namespace/name -> workload instance
	keyToInstance map[string]*model.WorkloadInstance
	// map of ip -> set of namespace/name
	ipToKeys MultiValueMap
}

// Insert implements Index.
func (i *index) Insert(wi *model.WorkloadInstance) *model.WorkloadInstance {
	i.mu.Lock()
	defer i.mu.Unlock()

	key := i.keyFunc(wi)
	// Check to see if the workload entry changed. If it did, clear the old entry
	previous := i.keyToInstance[key]
	if previous != nil && previous.Endpoint.Address != wi.Endpoint.Address {
		i.ipToKeys.Delete(previous.Endpoint.Address, key)
	}
	i.keyToInstance[key] = wi
	if wi.Endpoint.Address != "" {
		i.ipToKeys.Insert(wi.Endpoint.Address, key)
	}
	return previous
}

// Delete implements Index.
func (i *index) Delete(wi *model.WorkloadInstance) *model.WorkloadInstance {
	i.mu.Lock()
	defer i.mu.Unlock()

	key := i.keyFunc(wi)
	previous := i.keyToInstance[key]
	if previous != nil {
		i.ipToKeys.Delete(previous.Endpoint.Address, key)
	}
	i.ipToKeys.Delete(wi.Endpoint.Address, key)
	delete(i.keyToInstance, key)
	return previous
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
	for _, key := range sets.SortedList(keys) {
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
