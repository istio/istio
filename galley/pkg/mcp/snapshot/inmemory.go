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
	resources map[string][]*mcp.Envelope
	versions  map[string]string
}

var _ Snapshot = &InMemory{}

// NewInMemory creates a new InMemory snapshot implementation
func NewInMemory() *InMemory {
	return &InMemory{
		resources: make(map[string][]*mcp.Envelope),
		versions:  make(map[string]string),
	}
}

// Resources is an implementation of Snapshot.Resources
func (s *InMemory) Resources(typ string) []*mcp.Envelope {
	return s.resources[typ]
}

// Version is an implementation of Snapshot.Version
func (s *InMemory) Version(typ string) string {
	return s.versions[typ]
}

// Set the values for a given type.
func (s *InMemory) Set(typ string, version string, resources []*mcp.Envelope) {
	s.resources[typ] = resources
	s.versions[typ] = version
}
