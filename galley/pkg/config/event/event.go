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

package event

import (
	"fmt"

	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// Event represents a change that occurred against a resource in the source config system.
type Event struct {
	Kind Kind

	// The collection that this event is emanating from.
	Source collection.Name

	// A single entry, in case the event is Added, Updated or Deleted.
	Entry *resource.Entry
}

// IsSource checks whether the event has the appropriate source and returns false if it does not.
func (e *Event) IsSource(s collection.Name) bool {
	return e.Source == s
}

// IsSourceAny checks whether the event has the appropriate source and returns false if it does not.
func (e *Event) IsSourceAny(names ...collection.Name) bool {
	for _, n := range names {
		if n == e.Source {
			return true
		}
	}

	return false
}

// WithSource returns a new event with the source changed to the given collection.Name, if the event.Kind != Reset.
func (e *Event) WithSource(s collection.Name) Event {
	if e.Kind == Reset {
		return *e
	}

	r := *e
	r.Source = s
	return r
}

// Clone creates a deep clone of the event.
func (e *Event) Clone() Event {
	entry := e.Entry
	if entry != nil {
		entry = entry.Clone()
	}
	return Event{
		Entry:  entry,
		Source: e.Source,
		Kind:   e.Kind,
	}
}

// String implements Stringer.String
func (e Event) String() string {
	switch e.Kind {
	case Added, Updated, Deleted:
		return fmt.Sprintf("[Event](%s: %v/%v)", e.Kind.String(), e.Source, e.Entry.Metadata.Name)
	case FullSync:
		return fmt.Sprintf("[Event](%s: %v)", e.Kind.String(), e.Source)
	default:
		return fmt.Sprintf("[Event](%s)", e.Kind.String())
	}
}

// FullSyncFor creates a FullSync event for the given source.
func FullSyncFor(source collection.Name) Event {
	return Event{Kind: FullSync, Source: source}
}

// AddFor creates an Add event for the given source and entry.
func AddFor(source collection.Name, r *resource.Entry) Event {
	return Event{Kind: Added, Source: source, Entry: r}
}

// UpdateFor creates an Update event for the given source and entry.
func UpdateFor(source collection.Name, r *resource.Entry) Event {
	return Event{Kind: Updated, Source: source, Entry: r}
}

// DeleteForResource creates a Deleted event for the given source and entry.
func DeleteForResource(source collection.Name, r *resource.Entry) Event {
	return Event{Kind: Deleted, Source: source, Entry: r}
}

// DeleteFor creates a Delete event for the given source and name.
func DeleteFor(source collection.Name, name resource.Name, v resource.Version) Event {
	return DeleteForResource(source, &resource.Entry{
		Metadata: resource.Metadata{
			Name:    name,
			Version: v,
		},
	})
}
