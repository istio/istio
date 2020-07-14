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

package event

import (
	"fmt"

	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

var _ fmt.Stringer = Event{}

// Event represents a change that occurred against a resource in the source config system.
type Event struct {
	Kind Kind

	// Source collection that this event is emanating from.
	Source collection.Schema

	// A single entry, in case the event is Added, Updated or Deleted.
	Resource *resource.Instance
}

// SourceName is a utility method that returns the name of the source. If nil, returns "".
func (e *Event) SourceName() collection.Name {
	if e.Source != nil {
		return e.Source.Name()
	}
	return ""
}

// IsSource checks whether the event has the appropriate source and returns false if it does not.
func (e *Event) IsSource(s collection.Name) bool {
	return e.SourceName() == s
}

// IsSourceAny checks whether the event has the appropriate source and returns false if it does not.
func (e *Event) IsSourceAny(names ...collection.Name) bool {
	name := e.SourceName()
	for _, n := range names {
		if n == name {
			return true
		}
	}

	return false
}

// WithSource returns a new event with the source changed to the given collection.Name, if the event.Kind != Reset.
func (e *Event) WithSource(s collection.Schema) Event {
	if e.Kind == Reset {
		return *e
	}

	r := *e
	r.Source = s
	return r
}

// Clone creates a deep clone of the event.
func (e *Event) Clone() Event {
	entry := e.Resource
	if entry != nil {
		entry = entry.Clone()
	}
	return Event{
		Resource: entry,
		Source:   e.Source,
		Kind:     e.Kind,
	}
}

// String implements Stringer.String
func (e Event) String() string {
	switch e.Kind {
	case Added, Updated, Deleted:
		return fmt.Sprintf("[Event](%s: %v/%v)", e.Kind.String(), e.SourceName(), e.Resource.Metadata.FullName)
	case FullSync:
		return fmt.Sprintf("[Event](%s: %v)", e.Kind.String(), e.SourceName())
	default:
		return fmt.Sprintf("[Event](%s)", e.Kind.String())
	}
}

// FullSyncFor creates a FullSync event for the given source.
func FullSyncFor(source collection.Schema) Event {
	return Event{Kind: FullSync, Source: source}
}

// AddFor creates an Add event for the given source and entry.
func AddFor(source collection.Schema, r *resource.Instance) Event {
	return Event{Kind: Added, Source: source, Resource: r}
}

// UpdateFor creates an Update event for the given source and entry.
func UpdateFor(source collection.Schema, r *resource.Instance) Event {
	return Event{Kind: Updated, Source: source, Resource: r}
}

// DeleteForResource creates a Deleted event for the given source and entry.
func DeleteForResource(source collection.Schema, r *resource.Instance) Event {
	return Event{Kind: Deleted, Source: source, Resource: r}
}

// DeleteFor creates a Delete event for the given source and name.
func DeleteFor(source collection.Schema, name resource.FullName, v resource.Version) Event {
	return DeleteForResource(source, &resource.Instance{
		Metadata: resource.Metadata{
			FullName: name,
			Version:  v,
		},
	})
}
