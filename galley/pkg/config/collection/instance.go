// Copyright 2019 Istio Authors
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

package collection

import (
	"sync"

	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// ChangeNotifierFn is called when a collection instance changes.
type ChangeNotifierFn func()

// Instance is collection of resources, indexed by name.
type Instance struct {
	mu          sync.RWMutex // TODO: This lock will most likely cause contention. We should investigate whether removing it would help.
	collection  collection.Name
	generation  int64
	entries     map[resource.Name]*resource.Entry
	copyOnWrite bool
}

// New returns a new collection.Instance
func New(collection collection.Name) *Instance {
	return &Instance{
		collection: collection,
		entries:    make(map[resource.Name]*resource.Entry),
	}
}

// Name of the collection
func (c *Instance) Name() collection.Name {
	return c.collection
}

// Get the instance with the given name
func (c *Instance) Get(name resource.Name) *resource.Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[name]
}

// Generation of the current state of the collection.Instance
func (c *Instance) Generation() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.generation
}

// Size returns the number of items in the set
func (c *Instance) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// ForEach executes the given function for each entry
func (c *Instance) ForEach(fn func(e *resource.Entry) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.entries {
		if !fn(e) {
			break
		}
	}
}

// Set an entry in the collection
func (c *Instance) Set(r *resource.Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doCopyOnWrite()
	c.generation++
	c.entries[r.Metadata.Name] = r
}

// Remove an entry from the collection.
func (c *Instance) Remove(n resource.Name) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doCopyOnWrite()
	c.generation++
	delete(c.entries, n)
}

// Clear the contents of this instance.
func (c *Instance) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doCopyOnWrite()
	c.generation++
	c.entries = make(map[resource.Name]*resource.Entry)
}

func (c *Instance) doCopyOnWrite() { // TODO: we should optimize copy-on write.
	if !c.copyOnWrite {
		return
	}

	m := make(map[resource.Name]*resource.Entry)
	for k, v := range c.entries {
		m[k] = v
	}
	c.entries = m
	c.copyOnWrite = false
}

// Clone the instance
func (c *Instance) Clone() *Instance {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.copyOnWrite = true
	return &Instance{
		collection:  c.collection,
		generation:  c.generation,
		entries:     c.entries,
		copyOnWrite: true,
	}
}
