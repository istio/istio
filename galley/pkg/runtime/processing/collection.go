//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import (
	"istio.io/istio/galley/pkg/runtime/resource"
)

// Collection is an in-memory store for resource name keyed data. It uses versions to detect changes.
type Collection struct {
	generation int64
	resources  map[resource.FullName]interface{}
	versions   map[resource.FullName]resource.Version
}

// NewCollection returns new Collection instance.
func NewCollection() *Collection {
	return &Collection{
		generation: 0,
		resources:  make(map[resource.FullName]interface{}),
		versions:   make(map[resource.FullName]resource.Version),
	}
}

// Generation is a unique id that changes every time the collection changes.
func (c *Collection) Generation() int64 {
	return c.generation
}

// Names returns the set of known names.
func (c *Collection) Names() []resource.FullName {
	result := make([]resource.FullName, 0, len(c.resources))
	for n := range c.resources {
		result = append(result, n)
	}
	return result
}

// Item returns the named item from the collection
func (c *Collection) Item(name resource.FullName) interface{} {
	return c.resources[name]
}

// Get returns the current set of items.
func (c *Collection) Get() []interface{} {
	result := make([]interface{}, len(c.resources))
	i := 0
	for _, v := range c.resources {
		result[i] = v
		i++
	}

	return result
}

// Set resource in the collection. If this has caused collection change (i.e. add or update w/ different version #)
// then it returns true
func (c *Collection) Set(key resource.VersionedKey, iface interface{}) bool {
	previous, exists := c.versions[key.FullName]
	updated := !exists || previous != key.Version
	c.versions[key.FullName] = key.Version
	c.resources[key.FullName] = iface
	return updated
}

// Remove resource from the collection. Returns true if the resource was actually removed.
func (c *Collection) Remove(key resource.FullName) bool {
	_, found := c.resources[key]
	delete(c.resources, key)
	delete(c.versions, key)
	return found
}

// Count returns number of items in the collection
func (c *Collection) Count() int {
	return len(c.resources)
}

// ForEachItem applies the given function to each item in the collection
func (c *Collection) ForEachItem(fn func(i interface{})) {
	for _, item := range c.resources {
		fn(item)
	}
}
