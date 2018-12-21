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

import "istio.io/istio/galley/pkg/runtime/resource"

// EntryCollection is an efficient collection for entries.
type EntryCollection struct {
	generation int64
	resources  map[resource.FullName]resource.Entry
}

// NewEntryCollection returns a new EntryCollection
func NewEntryCollection() *EntryCollection {
	return &EntryCollection{
		generation: 0,
		resources:  make(map[resource.FullName]resource.Entry),
	}
}

// Generation is a unique id that changes every time the collection changes.
func (c *EntryCollection) Generation() int64 {
	return c.generation
}

// Names returns the set of known names.
func (c *EntryCollection) Names() []resource.FullName {
	result := make([]resource.FullName, 0, len(c.resources))
	for n := range c.resources {
		result = append(result, n)
	}
	return result
}

// Item returns the named item from the collection
func (c *EntryCollection) Item(name resource.FullName) resource.Entry {
	return c.resources[name]
}

// Set resource in the collection. If this has caused collection change (i.e. add or update w/ different version #)
// then it returns true
func (c *EntryCollection) Set(entry resource.Entry) bool {
	previous, exists := c.resources[entry.ID.FullName]
	updated := !exists || previous.ID.Version != entry.ID.Version
	c.resources[entry.ID.FullName] = entry
	if updated {
		c.generation++
	}
	return updated
}

// Remove resource from the collection. Returns true if the resource was actually removed.
func (c *EntryCollection) Remove(key resource.FullName) bool {
	_, found := c.resources[key]
	delete(c.resources, key)
	if found {
		c.generation++
	}
	return found
}

// Count returns number of items in the collection
func (c *EntryCollection) Count() int {
	return len(c.resources)
}

// ForEachItem applies the given function to each item in the collection
func (c *EntryCollection) ForEachItem(fn func(e resource.Entry)) {
	for _, item := range c.resources {
		fn(item)
	}
}
