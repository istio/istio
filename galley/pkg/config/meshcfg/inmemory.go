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

package meshcfg

import (
	"sync"

	"github.com/gogo/protobuf/proto"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
)

// InMemorySource is an event.InMemorySource implementation for meshconfig. When the mesh config is first set, add & fullsync events
// will be published. Otherwise a reset event will be sent.
type InMemorySource struct {
	mu      sync.Mutex
	current *v1alpha1.MeshConfig

	handlers event.Handler

	synced  bool
	started bool
}

var _ event.Source = &InMemorySource{}

// NewInmemory returns a new meshconfig.InMemorySource.
func NewInmemory() *InMemorySource {
	return &InMemorySource{
		current: Default(),
	}
}

// Dispatch implements event.Dispatcher
func (s *InMemorySource) Dispatch(handler event.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = event.CombineHandlers(s.handlers, handler)
}

// Start implements event.InMemorySource
func (s *InMemorySource) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		// Already started
		return
	}
	s.started = true

	if s.synced {
		s.send(event.Added)
		s.send(event.FullSync)
	}
}

// Stop implements event.InMemorySource
func (s *InMemorySource) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = false
}

// Set new meshconfig
func (s *InMemorySource) Set(cfg *v1alpha1.MeshConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg = proto.Clone(cfg).(*v1alpha1.MeshConfig)
	s.current = cfg

	if s.started {
		if !s.synced {
			s.send(event.Added)
			s.send(event.FullSync)
		} else {
			s.send(event.Reset)
		}
	}

	s.synced = true
}

// IsSynced indicates that the InMemorySource has been given a Mesh config at least once.
func (s *InMemorySource) IsSynced() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.synced
}

func (s *InMemorySource) send(k event.Kind) {
	// must be called under lock
	e := event.Event{
		Kind:   k,
		Source: collections.IstioMeshV1Alpha1MeshConfig,
	}

	switch k {
	case event.Added, event.Updated:
		e.Resource = &resource.Instance{
			Metadata: resource.Metadata{
				FullName: ResourceName,
				Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
			},
			Message: proto.Clone(s.current),
		}
	}

	s.handlers.Handle(e)
}
