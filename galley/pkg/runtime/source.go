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

package runtime

import (
	"fmt"
	"sync"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/galley/pkg/runtime/resource"
)

// Source to be implemented by a source configuration provider.
type Source interface {
	// Start the source interface. This returns a channel that the runtime will listen to for configuration
	// change events. The initial state of the underlying config store should be reflected as a series of
	// Added events, followed by a FullSync event.
	Start() (chan resource.Event, error)

	// Stop the source interface. Upon return from this method, the channel should not be accumulating any
	// more events.
	Stop()
}

// InMemorySource is an implementation of source.Interface.
type InMemorySource struct {
	stateLock sync.Mutex

	items map[resource.Key]resource.Entry

	ch chan resource.Event

	versionCtr int64
}

var _ Source = &InMemorySource{}

// NewInMemorySource returns a new instance of InMemorySource.
func NewInMemorySource() *InMemorySource {
	return &InMemorySource{
		items:      make(map[resource.Key]resource.Entry),
		versionCtr: 1,
	}
}

// Start implements source.Interface.Start
func (s *InMemorySource) Start() (chan resource.Event, error) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.ch != nil {
		return nil, fmt.Errorf("already started")
	}
	s.ch = make(chan resource.Event, 1024)

	// publish current items
	for _, item := range s.items {
		s.ch <- resource.Event{Kind: resource.Added, ID: item.ID}
	}
	s.ch <- resource.Event{Kind: resource.FullSync}

	return s.ch, nil
}

// Stop implements source.Interface.Stop
func (s *InMemorySource) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.ch == nil {
		return
	}

	close(s.ch)
	s.ch = nil
}

// Set the value in the in-memory store.
func (s *InMemorySource) Set(k resource.Key, item proto.Message) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	_, found := s.items[k]

	v := s.nextVersion()
	s.items[k] = resource.Entry{ID: resource.VersionedKey{Key: k, Version: v}, Item: item}

	kind := resource.Added
	if found {
		kind = resource.Updated
	}

	if s.ch != nil {
		s.ch <- resource.Event{Kind: kind, ID: resource.VersionedKey{Key: k, Version: v}, Item: item}
	}
}

// Delete a value in the in-memory store.
func (s *InMemorySource) Delete(k resource.Key) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	_, found := s.items[k]
	if !found {
		return
	}

	v := s.nextVersion()

	delete(s.items, k)

	s.ch <- resource.Event{Kind: resource.Deleted, ID: resource.VersionedKey{Key: k, Version: v}}
}

// Get a value in the in-memory store.
func (s *InMemorySource) Get(key resource.Key) (resource.Entry, error) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	r := s.items[key]
	return r, nil
}

func (s *InMemorySource) nextVersion() resource.Version {
	v := s.versionCtr
	s.versionCtr++
	return resource.Version(fmt.Sprintf("v%d", v))
}
