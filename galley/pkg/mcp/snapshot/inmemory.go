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

package snapshot

import mcp "istio.io/api/config/mcp/v1alpha1"

// InMemory Snapshot implementation
type InMemory struct {
	envelopes map[string][]*mcp.Envelope
	versions  map[string]string

	frozen bool
}

var _ Snapshot = &InMemory{}

// NewInMemory creates a new InMemory snapshot implementation
func NewInMemory() *InMemory {
	return &InMemory{
		envelopes: make(map[string][]*mcp.Envelope),
		versions:  make(map[string]string),
	}
}

// Envelopes is an implementation of Snapshot.Envelopes
func (s *InMemory) Resources(typ string) []*mcp.Envelope {
	return s.envelopes[typ]
}

// Version is an implementation of Snapshot.Version
func (s *InMemory) Version(typ string) string {
	return s.versions[typ]
}

// Set the values for a given type. If Set is called after a call to Freeze, then this method panics.
func (s *InMemory) Set(typ string, version string, resources []*mcp.Envelope) {
	if s.frozen {
		panic("InMemory.Set: Snapshot is frozen")
	}

	s.envelopes[typ] = resources
	s.versions[typ] = version
}

// Freeze the snapshot, so that it won't get mutated anymore.
func (s *InMemory) Freeze() {
	s.frozen = true
}
