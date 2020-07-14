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
	"fmt"
	"sync"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
)

var inMemoryNameDiscriminator int64

// Source is an in-memory processor.Source implementation.
type Source struct {
	mu      sync.Mutex
	started bool

	collections map[collection.Name]*Collection
	name        string
}

var _ event.Source = &Source{}

// New returns a new in-memory source, based on given collections.
func New(collections collection.Schemas) *Source {
	name := fmt.Sprintf("inmemory-%d", inMemoryNameDiscriminator)
	inMemoryNameDiscriminator++

	all := collections.All()
	scope.Source.Debugf("Creating new in-memory source (collections: %d)", len(all))

	s := &Source{
		collections: make(map[collection.Name]*Collection),
		name:        name,
	}

	for _, c := range all {
		s.collections[c.Name()] = NewCollection(c)
	}

	return s
}

// Dispatch implements event.Source
func (s *Source) Dispatch(h event.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.collections {
		c.Dispatch(h)
	}
}

// Start implements processor.Source
func (s *Source) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return
	}

	for _, c := range s.collections {
		c.Start()
	}

	s.started = true
}

// Stop implements processor.Source
func (s *Source) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	s.started = false

	for _, c := range s.collections {
		c.Stop()
	}
}

// Clear contents of this source
func (s *Source) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.collections {
		c.Clear()
	}
}

// Get returns the named collection.
func (s *Source) Get(collection collection.Name) *Collection {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.collections[collection]
}
