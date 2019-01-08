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

package flow

import (
	"istio.io/istio/galley/pkg/runtime/resource"
)

// Table is an in-memory store for resource name keyed data. It uses versions to detect changes.
type Table struct {
	generation int64
	resources  map[resource.FullName]interface{}
	versions   map[resource.FullName]resource.Version
	listener tableChangeListener
}

type tableChangeListener interface {
	tableChanged(t *Table)
}

// NewTable returns new Table instance.
func NewTable() *Table {
	return &Table{
		generation: 0,
		resources:  make(map[resource.FullName]interface{}),
		versions:   make(map[resource.FullName]resource.Version),
	}
}

// Generation is a unique id that changes every time the table changes.
func (c *Table) Generation() int64 {
	return c.generation
}

// Names returns the set of known names.
func (c *Table) Names() []resource.FullName {
	result := make([]resource.FullName, 0, len(c.resources))
	for n := range c.resources {
		result = append(result, n)
	}
	return result
}

// Item returns the named item from the table
func (c *Table) Item(name resource.FullName) interface{} {
	return c.resources[name]
}

// Item returns the named item from the table
func (c *Table) Version(name resource.FullName) resource.Version {
	return c.versions[name]
}

// Get returns the current set of items.
func (c *Table) Get() []interface{} {
	result := make([]interface{}, 0, len(c.resources))

	for _, v := range c.resources {
		result = append(result, v)
	}

	return result
}

// Set resource in the table. If this has caused table change (i.e. add or update w/ different version #)
// then it returns true
func (c *Table) Set(key resource.VersionedKey, iface interface{}) bool {
	previous, exists := c.versions[key.FullName]
	updated := !exists || previous != key.Version
	c.versions[key.FullName] = key.Version
	c.resources[key.FullName] = iface

	if updated {
		c.generation++
	}
	return updated
}

// Remove resource from the table. Returns true if the resource was actually removed.
func (c *Table) Remove(key resource.FullName) bool {
	_, found := c.resources[key]
	delete(c.resources, key)
	delete(c.versions, key)
	if found {
		c.generation++
	}
	return found
}

// Count returns number of items in the table
func (c *Table) Count() int {
	return len(c.resources)
}

// ForEachItem applies the given function to each item in the table
func (c *Table) ForEachItem(fn func(i interface{})) {
	for _, item := range c.resources {
		fn(item)
	}
}

func (c *Table) setTableChangeListener(t tableChangeListener) {
	c.listener = t
}