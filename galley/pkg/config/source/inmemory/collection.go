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

package inmemory

import (
	"sort"
	"strings"
	"sync"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/scope"
)

// Collection is an in-memory collection that implements event.Source
type Collection struct {
	mu         sync.RWMutex // TODO: We should be able to get rid of this mutex.
	collection collection.Name
	handler    event.Handler
	resources  map[resource.Name]*resource.Entry
	synced     bool
}

var _ event.Source = &Collection{}

// NewCollection returns a new in-memory collection.
func NewCollection(c collection.Name) *Collection {
	scope.Source.Debuga("  Creating in-memory collection: ", c)

	return &Collection{
		collection: c,
		resources:  make(map[resource.Name]*resource.Entry),
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

	c.dispatchEvent(event.FullSyncFor(c.collection))
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
		scope.Source.Debugf("Collection.Dispatch: (collection: %-50v, handler: %T)", c.collection, handler)
	}

	c.handler = event.CombineHandlers(c.handler, handler)
}

// Set the entry in the collection
func (c *Collection) Set(entry *resource.Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	kind := event.Added
	_, found := c.resources[entry.Metadata.Name]
	if found {
		kind = event.Updated
	}

	c.resources[entry.Metadata.Name] = entry

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
				Kind:   event.Deleted,
				Source: c.collection,
				Entry:  entry,
			}

			c.dispatchEvent(e)
		}
	}

	c.resources = make(map[resource.Name]*resource.Entry)
}

func (c *Collection) dispatchEvent(e event.Event) {
	if scope.Source.DebugEnabled() {
		scope.Source.Debugf(">>> Collection.dispatchEvent: (col: %-50s): %v", c.collection, e)
	}
	if c.handler != nil {
		c.handler.Handle(e)
	}
}

func (c *Collection) dispatchFor(entry *resource.Entry, kind event.Kind) {
	e := event.Event{
		Source: c.collection,
		Entry:  entry,
		Kind:   kind,
	}
	c.dispatchEvent(e)
}

// Remove the entry from the collection
func (c *Collection) Remove(n resource.Name) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.resources[n]
	if found {
		e := event.Event{
			Kind:   event.Deleted,
			Source: c.collection,
			Entry:  entry,
		}

		delete(c.resources, n)
		c.dispatchEvent(e)
	}
}

// AllSorted returns all entries in this collection, in sort order.
// Warning: This is not performant!
func (c *Collection) AllSorted() []*resource.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []*resource.Entry
	for _, e := range c.resources {
		result = append(result, e)
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].Metadata.Name.String(), result[j].Metadata.Name.String()) < 0
	})

	return result
}
