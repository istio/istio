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

package inmemory

import (
	"sort"
	"strings"
	"sync"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

// Collection is an in-memory collection that implements event.Source
type Collection struct {
	mu        sync.RWMutex // TODO: We should be able to get rid of this mutex.
	schema    collection.Schema
	handler   event.Handler
	resources map[resource.FullName]*resource.Instance
	synced    bool
}

var _ event.Source = &Collection{}

// NewCollection returns a new in-memory collection.
func NewCollection(s collection.Schema) *Collection {
	scope.Source.Debuga("  Creating in-memory collection: ", s.Name())

	return &Collection{
		schema:    s,
		resources: make(map[resource.FullName]*resource.Instance),
	}
}

// Start dispatching events for the collection.
func (c *Collection) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.synced = true

	for _, e := range c.resources {
		c.dispatchFor(e, event.Added)
	}

	c.dispatchEvent(event.FullSyncFor(c.schema))
}

// Stop dispatching events and reset internal state.
func (c *Collection) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.synced = false
}

// Dispatch an event handler to receive resource events.
func (c *Collection) Dispatch(handler event.Handler) {
	if scope.Source.DebugEnabled() {
		scope.Source.Debugf("Collection.Dispatch: (collection: %-50v, handler: %T)", c.schema.Name(), handler)
	}

	c.handler = event.CombineHandlers(c.handler, handler)
}

// Set the entry in the collection
func (c *Collection) Set(entry *resource.Instance) {
	c.mu.Lock()
	defer c.mu.Unlock()

	kind := event.Added
	_, found := c.resources[entry.Metadata.FullName]
	if found {
		kind = event.Updated
	}

	c.resources[entry.Metadata.FullName] = entry

	if c.synced {
		c.dispatchFor(entry, kind)
	}
}

// Clear the contents of this collection.
func (c *Collection) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.synced {
		for _, entry := range c.resources {
			e := event.Event{
				Kind:     event.Deleted,
				Source:   c.schema,
				Resource: entry,
			}

			c.dispatchEvent(e)
		}
	}

	c.resources = make(map[resource.FullName]*resource.Instance)
}

func (c *Collection) dispatchEvent(e event.Event) {
	if scope.Source.DebugEnabled() {
		scope.Source.Debugf(">>> Collection.dispatchEvent: (col: %-50s): %v", c.schema.Name(), e)
	}
	if c.handler != nil {
		c.handler.Handle(e)
	}
}

func (c *Collection) dispatchFor(r *resource.Instance, kind event.Kind) {
	e := event.Event{
		Source:   c.schema,
		Resource: r,
		Kind:     kind,
	}
	c.dispatchEvent(e)
}

// Remove the entry from the collection
func (c *Collection) Remove(n resource.FullName) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.resources[n]
	if found {
		e := event.Event{
			Kind:     event.Deleted,
			Source:   c.schema,
			Resource: entry,
		}

		delete(c.resources, n)
		c.dispatchEvent(e)
	}
}

// AllSorted returns all entries in this collection, in sort order.
// Warning: This is not performant!
func (c *Collection) AllSorted() []*resource.Instance {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []*resource.Instance
	for _, e := range c.resources {
		result = append(result, e)
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].Metadata.FullName.String(), result[j].Metadata.FullName.String()) < 0
	})

	return result
}
