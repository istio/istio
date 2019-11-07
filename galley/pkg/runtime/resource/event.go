// Copyright 2018 Istio Authors
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

package resource

import (
	"fmt"
)

// EventKind is the type of an event.
type EventKind int

const (
	// None is a sentinel value. Should not be used.
	None EventKind = iota

	// Added indicates that a new resource has been added.
	Added

	// Updated indicates that an existing resource has been updated.
	Updated

	// Deleted indicates an existing resource has been deleted.
	Deleted

	// FullSync indicates that the initial state of the store has been published as a series of Added events.
	// Events after FullSync are actual change events that the source-store has encountered.
	FullSync
)

var (
	// FullSyncEvent is a special event representing a FullSync.
	FullSyncEvent = Event{Kind: FullSync}
)

// Event represents a change that occurred against a resource in the source config system.
type Event struct {
	Kind EventKind

	// A single entry, in case the event is Added, Updated or Deleted.
	Entry Entry
}

// EventHandler function.
type EventHandler func(event Event)

// String implements Stringer.String
func (k EventKind) String() string {
	switch k {
	case None:
		return "None"
	case Added:
		return "Added"
	case Updated:
		return "Updated"
	case Deleted:
		return "Deleted"
	case FullSync:
		return "FullSync"
	default:
		return fmt.Sprintf("<<Unknown EventKind %d>>", k)
	}
}

// String implements Stringer.String
func (e Event) String() string {
	switch e.Kind {
	case Added, Updated, Deleted:
		return fmt.Sprintf("[Event](%s: %v)", e.Kind.String(), e.Entry.ID)
	case FullSync:
		return fmt.Sprintf("[Event](%s)", e.Kind.String())
	default:
		return fmt.Sprintf("[Event](%s)", e.Kind.String())
	}
}
