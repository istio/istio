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

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/config/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// InMemory Snapshot implementation
type InMemory struct {
	envelopes map[resource.TypeURL][]*mcp.Envelope
	versions  map[resource.TypeURL]resource.Version
}

var _ Snapshot = &InMemory{}

// InMemoryBuilder is a builder for an InMemory snapshot.
type InMemoryBuilder struct {
	snapshot *InMemory
}

// NewInMemoryBuilder creates and returns a new InMemoryBuilder.
func NewInMemoryBuilder() *InMemoryBuilder {
	snapshot := &InMemory{
		envelopes: make(map[resource.TypeURL][]*mcp.Envelope),
		versions:  make(map[resource.TypeURL]resource.Version),
	}

	return &InMemoryBuilder{
		snapshot: snapshot,
	}
}

// Set the values for a given type. If Set is called after a call to Freeze, then this method panics.
func (b *InMemoryBuilder) Set(typeURL resource.TypeURL, version resource.Version, resources []*mcp.Envelope) {
	b.snapshot.envelopes[typeURL] = resources
	b.snapshot.versions[typeURL] = version
}

// SetEntry sets a single entry. Note that this is a slow operation, as update requires scanning
// through existing entries.
func (b *InMemoryBuilder) SetEntry(typeURL resource.TypeURL, name string, m proto.Message) error {
	contents, err := proto.Marshal(m)
	if err != nil {
		return err
	}

	e := &mcp.Envelope{
		Metadata: &mcp.Metadata{
			Name: name,
		},
		Resource: &types.Any{
			Value:   contents,
			TypeUrl: typeURL.String(),
		},
	}

	entries := b.snapshot.envelopes[typeURL]

	for i, prev := range entries {
		if prev.Metadata.Name == e.Metadata.Name {
			entries[i] = e
			return nil
		}
	}

	entries = append(entries, e)
	b.snapshot.envelopes[typeURL] = entries
	return nil
}

// DeleteEntry deletes the entry with the given typeURL, name
func (b *InMemoryBuilder) DeleteEntry(typeURL resource.TypeURL, name string) {

	entries, found := b.snapshot.envelopes[typeURL]
	if !found {
		return
	}

	for i, e := range entries {
		if e.Metadata.Name == name {
			if len(entries) == 1 {
				delete(b.snapshot.envelopes, typeURL)
				delete(b.snapshot.versions, typeURL)
				return
			}

			entries = append(entries[:i], entries[i+1:]...)
			b.snapshot.envelopes[typeURL] = entries

			return
		}
	}
}

// SetVersion ets the version for the given type URL.
func (b *InMemoryBuilder) SetVersion(typeURL resource.TypeURL, version resource.Version) {
	b.snapshot.versions[typeURL] = version
}

// Build the snapshot and return.
func (b *InMemoryBuilder) Build() *InMemory {
	sn := b.snapshot

	// Avoid mutation after build
	b.snapshot = nil

	return sn
}

// Resources is an implementation of Snapshot.Resources
func (s *InMemory) Resources(typeURL resource.TypeURL) []*mcp.Envelope {
	return s.envelopes[typeURL]
}

// Version is an implementation of Snapshot.Version
func (s *InMemory) Version(typeURL resource.TypeURL) resource.Version {
	return s.versions[typeURL]
}

// Clone this snapshot.
func (s *InMemory) Clone() *InMemory {
	c := &InMemory{
		envelopes: make(map[resource.TypeURL][]*mcp.Envelope),
		versions:  make(map[resource.TypeURL]resource.Version),
	}

	for k, v := range s.versions {
		c.versions[k] = v
	}

	for k, v := range s.envelopes {
		envs := make([]*mcp.Envelope, len(v))
		for i, e := range v {
			envs[i] = proto.Clone(e).(*mcp.Envelope)
		}
		c.envelopes[k] = envs
	}

	return c
}

// Builder returns a new builder instance, based on the contents of this snapshot.
func (s *InMemory) Builder() *InMemoryBuilder {
	snapshot := s.Clone()

	return &InMemoryBuilder{
		snapshot: snapshot,
	}
}

func (s *InMemory) String() string {
	var b bytes.Buffer

	var typeUrls []resource.TypeURL
	for typeURL := range s.envelopes {
		typeUrls = append(typeUrls, typeURL)
	}
	sort.Slice(typeUrls, func(i, j int) bool {
		return strings.Compare(typeUrls[i].String(), typeUrls[j].String()) < 0
	})

	for i, n := range typeUrls {
		fmt.Fprintf(&b, "[%d] (%s @%s)\n", i, n, s.versions[n])

		envs := s.envelopes[n]

		// Avoid mutating the original data
		entries := make([]*mcp.Envelope, len(envs))
		copy(entries, envs)
		sort.Slice(entries, func(i, j int) bool {
			return strings.Compare(entries[i].Metadata.Name, entries[j].Metadata.Name) == -1
		})

		for j, entry := range entries {
			fmt.Fprintf(&b, "  [%d] (%s)\n", j, entry.Metadata.Name)
		}
	}

	return b.String()
}
