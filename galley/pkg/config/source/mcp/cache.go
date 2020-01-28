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

package mcp

import (
	"fmt"
	"sync"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	resource2 "istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/mcp/sink"
)

var _ event.Source = &cache{}

// cache is an in-memory cache for a single collection.
type cache struct {
	mu                 sync.RWMutex
	schema             collection.Schema
	handler            event.Handler
	resources          map[resource.FullName]*resource.Instance
	synced             bool
	started            bool
	fullUpdateReceived bool
}

func newCache(c collection.Schema) *cache {
	scope.Source.Debuga("  Creating mcp cache for collection: ", c)

	return &cache{
		schema:    c,
		resources: make(map[resource.FullName]*resource.Instance),
	}
}

func (c *cache) apply(change *sink.Change) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Make sure the event is for this collection. This will always be the case in practice.
	if c.schema.Name().String() != change.Collection {
		return fmt.Errorf("failed applying change for unexpected collection (%v)", change.Collection)
	}

	if change.Incremental {
		return c.applyIncremental(change)
	}
	return c.applyFull(change)
}

func (c *cache) applyFull(change *sink.Change) error {
	// For full updates, save off the old resources and clear out the map.
	oldResources := c.resources
	c.resources = make(map[resource.FullName]*resource.Instance)
	c.fullUpdateReceived = true

	// Add/update resources.
	for _, obj := range change.Objects {
		e, err := deserializeEntry(obj, c.schema.Resource())
		if err != nil {
			return fmt.Errorf("failed parsing entry for collection (%v): %v", c.schema.Name(), err)
		}

		// Notify the handler if we've already synced.
		if c.synced {
			if _, ok := oldResources[e.Metadata.FullName]; ok {
				c.dispatchFor(e, event.Updated)
				delete(oldResources, e.Metadata.FullName)
			} else {
				c.dispatchFor(e, event.Added)
			}
		}

		c.resources[e.Metadata.FullName] = e
	}

	if c.synced {
		// Send Delete events for all remaining resources in oldResources
		for _, removed := range oldResources {
			c.dispatchFor(removed, event.Deleted)
		}
	}

	// Sync if necessary.
	c.sync()

	return nil
}

func (c *cache) applyIncremental(change *sink.Change) error {
	if !c.synced {
		return fmt.Errorf("mcp: incremental change received before full for collection (%v)", change.Collection)
	}

	// Add/update resources.
	for _, obj := range change.Objects {
		e, err := deserializeEntry(obj, c.schema.Resource())
		if err != nil {
			return fmt.Errorf("failed parsing entry for collection (%v): %v", c.schema.Name(), err)
		}

		// Notify the handler if we've already synced.
		if c.synced {
			if _, ok := c.resources[e.Metadata.FullName]; ok {
				c.dispatchFor(e, event.Updated)
			} else {
				c.dispatchFor(e, event.Added)
			}
		}

		c.resources[e.Metadata.FullName] = e
	}

	// Remove resources.
	for _, removed := range change.Removed {
		name, err := resource.ParseFullName(removed)
		if err != nil {
			return fmt.Errorf("failed removing resource due to parsing error: %v", err)
		}

		if e, ok := c.resources[name]; ok {
			if c.synced {
				c.dispatchFor(e, event.Deleted)
			}
			delete(c.resources, name)
		} else {
			return fmt.Errorf("incremental update attempting to delete missing resource (%v)", name)
		}
	}

	return nil
}

// Start dispatching events for the collection.
func (c *cache) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.started = true
	c.sync()
}

// Stop dispatching events and reset internal state.
func (c *cache) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.started = false
	c.fullUpdateReceived = false
	c.synced = false
}

// Dispatch an event handler to receive resource events.
func (c *cache) Dispatch(handler event.Handler) {
	if scope.Source.DebugEnabled() {
		scope.Source.Debugf("cache.Dispatch: (collection: %-50v, handler: %T)", c.schema.Name(), handler)
	}

	c.handler = event.CombineHandlers(c.handler, handler)
}

func (c *cache) sync() {
	if c.synced || !c.started || !c.fullUpdateReceived {
		return
	}
	c.synced = true

	for _, e := range c.resources {
		c.dispatchFor(e, event.Added)
	}

	c.dispatchEvent(event.FullSyncFor(c.schema))
}

func (c *cache) dispatchEvent(e event.Event) {
	if scope.Source.DebugEnabled() {
		scope.Source.Debugf(">>> cache.dispatchEvent: (col: %-50s): %v", c.schema.Name(), e)
	}
	if c.handler != nil {
		c.handler.Handle(e)
	}
}

func (c *cache) dispatchFor(entry *resource.Instance, kind event.Kind) {
	e := event.Event{
		Source:   c.schema,
		Resource: entry,
		Kind:     kind,
	}
	c.dispatchEvent(e)
}

func deserializeEntry(obj *sink.Object, s resource2.Schema) (*resource.Instance, error) {
	metadata, err := resource.DeserializeMetadata(obj.Metadata, s)
	if err != nil {
		return nil, err
	}
	return &resource.Instance{
		Metadata: metadata,
		Message:  obj.Body,
		Origin:   defaultOrigin,
	}, nil
}
