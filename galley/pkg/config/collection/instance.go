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

package collection

import (
	"sync"

	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

// Instance is collection of resources, indexed by name.
type Instance struct {
	mu          sync.RWMutex // TODO: This lock will most likely cause contention. We should investigate whether removing it would help.
	schema      collection.Schema
	generation  int64
	resources   map[resource.FullName]*resource.Instance
	copyOnWrite bool
}

// New returns a new collection.Instance
func New(collection collection.Schema) *Instance {
	return &Instance{
		schema:    collection,
		resources: make(map[resource.FullName]*resource.Instance),
	}
}

// Name of the collection
func (c *Instance) Name() collection.Name {
	return c.schema.Name()
}

// Schema for the collection.
func (c *Instance) Schema() collection.Schema {
	return c.schema
}

// Get the instance with the given name
func (c *Instance) Get(name resource.FullName) *resource.Instance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resources[name]
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
	return len(c.resources)
}

// ForEach executes the given function for each entry
func (c *Instance) ForEach(fn func(e *resource.Instance) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.resources {
		if !fn(e) {
			break
		}
	}
}

// Set an entry in the collection
func (c *Instance) Set(r *resource.Instance) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doCopyOnWrite()
	c.generation++
	c.resources[r.Metadata.FullName] = r
}

// Remove an entry from the collection.
func (c *Instance) Remove(n resource.FullName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doCopyOnWrite()
	c.generation++
	delete(c.resources, n)
}

// Clear the contents of this instance.
func (c *Instance) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doCopyOnWrite()
	c.generation++
	c.resources = make(map[resource.FullName]*resource.Instance)
}

func (c *Instance) doCopyOnWrite() { // TODO: we should optimize copy-on write.
	if !c.copyOnWrite {
		return
	}

	m := make(map[resource.FullName]*resource.Instance)
	for k, v := range c.resources {
		m[k] = v
	}
	c.resources = m
	c.copyOnWrite = false
}

// Clone the instance
func (c *Instance) Clone() *Instance {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.copyOnWrite = true
	return &Instance{
		schema:      c.schema,
		generation:  c.generation,
		resources:   c.resources,
		copyOnWrite: true,
	}
}
