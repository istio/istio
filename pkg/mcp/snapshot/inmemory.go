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

// Set the values for a given type. If Set is called after a call to Freeze, then this method panics.
func (b *InMemoryBuilder) Set(typeURL string, version string, resources []*mcp.Resource) {
	b.snapshot.resources[typeURL] = resources
	b.snapshot.versions[typeURL] = version
}

// SetEntry sets a single entry. Note that this is a slow operation, as update requires scanning
// through existing entries.
func (b *InMemoryBuilder) SetEntry(typeURL, name, version string, createTime time.Time, labels,
	annotations map[string]string, m proto.Message) error {
	contents, err := proto.Marshal(m)
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
		Body: &types.Any{
			Value:   contents,
			TypeUrl: typeURL,
		},
	}

	entries := b.snapshot.resources[typeURL]

	for i, prev := range entries {
		if prev.Metadata.Name == e.Metadata.Name {
			entries[i] = e
			return nil
		}
	}

	entries = append(entries, e)
	b.snapshot.resources[typeURL] = entries
	return nil
}

// DeleteEntry deletes the entry with the given typeuRL, name
func (b *InMemoryBuilder) DeleteEntry(typeURL string, name string) {

	entries, found := b.snapshot.resources[typeURL]
	if !found {
		return
	}

	for i, e := range entries {
		if e.Metadata.Name == name {
			if len(entries) == 1 {
				delete(b.snapshot.resources, typeURL)
				delete(b.snapshot.versions, typeURL)
				return
			}

			entries = append(entries[:i], entries[i+1:]...)
			b.snapshot.resources[typeURL] = entries

			return
		}
	}
}

// SetVersion sets the version for the given type URL.
func (b *InMemoryBuilder) SetVersion(typeURL string, version string) {
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
func (s *InMemory) Resources(typeURL string) []*mcp.Resource {
	return s.resources[typeURL]
}

// Version is an implementation of Snapshot.Version
func (s *InMemory) Version(typeURL string) string {
	return s.versions[typeURL]
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

	var messages []string
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
