//  Copyright Istio Authors
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
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
)

// InMemory Snapshot implementation
type InMemory struct {
	resources map[string][]*mcp.Resource
	versions  map[string]string
}

var _ Snapshot = &InMemory{}

// InMemoryBuilder is a builder for an InMemory snapshot.
type InMemoryBuilder struct {
	snapshot *InMemory
}

// NewInMemoryBuilder creates and returns a new InMemoryBuilder.
func NewInMemoryBuilder() *InMemoryBuilder {
	snapshot := &InMemory{
		resources: make(map[string][]*mcp.Resource),
		versions:  make(map[string]string),
	}

	return &InMemoryBuilder{
		snapshot: snapshot,
	}
}

// Set the values for a given collection. If Set is called after a call to Freeze, then this method panics.
func (b *InMemoryBuilder) Set(collection, version string, resources []*mcp.Resource) {
	b.snapshot.resources[collection] = resources
	b.snapshot.versions[collection] = version
}

// SetEntry sets a single entry. Note that this is a slow operation, as update requires scanning
// through existing entries.
func (b *InMemoryBuilder) SetEntry(collection, name, version string, createTime time.Time, labels,
	annotations map[string]string, m proto.Message) error {
	body, err := types.MarshalAny(m)
	if err != nil {
		return err
	}

	createTimeProto, err := types.TimestampProto(createTime)
	if err != nil {
		return err
	}

	e := &mcp.Resource{
		Metadata: &mcp.Metadata{
			Name:        name,
			CreateTime:  createTimeProto,
			Labels:      labels,
			Annotations: annotations,
			Version:     version,
		},
		Body: body,
	}

	entries := b.snapshot.resources[collection]

	for i, prev := range entries {
		if prev.Metadata.Name == e.Metadata.Name {
			entries[i] = e
			return nil
		}
	}

	entries = append(entries, e)
	b.snapshot.resources[collection] = entries
	return nil
}

// DeleteEntry deletes the named entry within the given collection.
func (b *InMemoryBuilder) DeleteEntry(collection string, name string) {

	entries, found := b.snapshot.resources[collection]
	if !found {
		return
	}

	for i, e := range entries {
		if e.Metadata.Name == name {
			if len(entries) == 1 {
				delete(b.snapshot.resources, collection)
				delete(b.snapshot.versions, collection)
				return
			}

			entries = append(entries[:i], entries[i+1:]...)
			b.snapshot.resources[collection] = entries

			return
		}
	}
}

// SetVersion sets the version for the given collection
func (b *InMemoryBuilder) SetVersion(collection string, version string) {
	b.snapshot.versions[collection] = version
}

// Build the snapshot and return.
func (b *InMemoryBuilder) Build() *InMemory {
	sn := b.snapshot

	// Avoid mutation after build
	b.snapshot = nil

	return sn
}

// Resources is an implementation of Snapshot.Resources
func (s *InMemory) Resources(collection string) []*mcp.Resource {
	return s.resources[collection]
}

// Version is an implementation of Snapshot.Version
func (s *InMemory) Version(collection string) string {
	return s.versions[collection]
}

// Collections is an implementation of Snapshot.Collections
func (s *InMemory) Collections() []string {
	result := make([]string, 0, len(s.resources))
	for col := range s.resources {
		result = append(result, col)
	}
	return result
}

// Clone this snapshot.
func (s *InMemory) Clone() *InMemory {
	c := &InMemory{
		resources: make(map[string][]*mcp.Resource),
		versions:  make(map[string]string),
	}

	for k, v := range s.versions {
		c.versions[k] = v
	}

	for k, v := range s.resources {
		envs := make([]*mcp.Resource, len(v))
		for i, e := range v {
			envs[i] = proto.Clone(e).(*mcp.Resource)
		}
		c.resources[k] = envs
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

	messages := make([]string, 0, len(s.resources))
	for message := range s.resources {
		messages = append(messages, message)
	}
	sort.Strings(messages)

	for i, n := range messages {
		_, _ = fmt.Fprintf(&b, "[%d] (%s @%s)\n", i, n, s.versions[n])

		envs := s.resources[n]

		// Avoid mutating the original data
		entries := make([]*mcp.Resource, len(envs))
		copy(entries, envs)
		sort.Slice(entries, func(i, j int) bool {
			return strings.Compare(entries[i].Metadata.Name, entries[j].Metadata.Name) == -1
		})

		for j, entry := range entries {
			_, _ = fmt.Fprintf(&b, "  [%d] (%s)\n", j, entry.Metadata.Name)
		}
	}

	return b.String()
}
