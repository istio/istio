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
	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/config/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/mcp/snapshot"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// State is the in-memory state of Galley.
type State struct {
	schema *resource.Schema

	// version counter is a nonce that generates unique ids for each updated view of State.
	versionCounter int64

	// entries for per-kind State.
	entriesLock sync.Mutex
	entries     map[resource.Kind]*kindState
}

// per-kind State.
type kindState struct {
	// The version number for the current State of the object. Every time entries or versions change,
	// the version number also change
	version  int64
	entries  map[string]*mcp.Envelope
	versions map[string]resource.Version
}

func newState(schema *resource.Schema) *State {
	return &State{
		schema:  schema,
		entries: make(map[resource.Kind]*kindState),
	}
}

func (s *State) apply(event resource.Event) bool {
	if _, ok := s.schema.LookupByKind(event.ID.Kind); !ok {
		scope.Errorf("Received an source event for unknown kind: %v", event)
		return false
	}

	pks := s.getKindState(event.ID.Kind)

	switch event.Kind {
	case resource.Added, resource.Updated:

		// Check to see if the version has changed.
		if curVersion := pks.versions[event.ID.FullName]; curVersion == event.ID.Version {
			scope.Debugf("Received event for the current, known version: %v", event)
			return false
		}

		// TODO: Check for content-wise equality

		entry, ok := s.envelopeResource(event)
		if !ok {
			return false
		}

		pks.entries[event.ID.FullName] = entry
		pks.versions[event.ID.FullName] = event.ID.Version

	case resource.Deleted:
		delete(pks.entries, event.ID.FullName)
		delete(pks.versions, event.ID.FullName)

	default:
		scope.Errorf("Unknown event kind: %v", event.Kind)
		return false
	}

	s.versionCounter++
	pks.version = s.versionCounter

	return true
}

func (s *State) getKindState(kind resource.Kind) *kindState {
	s.entriesLock.Lock()
	defer s.entriesLock.Unlock()

	pks, found := s.entries[kind]
	if !found {
		pks = &kindState{
			entries:  make(map[string]*mcp.Envelope),
			versions: make(map[string]resource.Version),
		}
		s.entries[kind] = pks
	}

	return pks
}

func (s *State) buildSnapshot() snapshot.Snapshot {
	s.entriesLock.Lock()
	defer s.entriesLock.Unlock()

	sn := snapshot.NewInMemory()

	for kind, state := range s.entries {
		entries := make([]*mcp.Envelope, 0, len(state.entries))
		for _, entry := range state.entries {
			entries = append(entries, entry)
		}

		version := fmt.Sprintf("%d", state.version)
		sn.Set(string(kind), version, entries)
	}

	sn.Freeze()

	return sn
}

func (s *State) envelopeResource(event resource.Event) (*mcp.Envelope, bool) {
	info, _ := s.schema.LookupByKind(event.ID.Kind)

	serialized, err := proto.Marshal(event.Item)
	if err != nil {
		scope.Errorf("Error serializing proto from source event: %v", event)
		return nil, false
	}

	entry := &mcp.Envelope{
		Metadata: &mcp.Metadata{
			Name: event.ID.FullName,
		},
		Resource: &types.Any{
			TypeUrl: info.TypeURL,
			Value:   serialized,
		},
	}

	return entry, true
}
