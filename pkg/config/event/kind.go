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
)

// Kind is the type of an event for a resource collection.
type Kind int

const (
	// None is a sentinel value. Should not be used.
	None Kind = iota

	// Added indicates that a new resource has been added to a collection.
	Added

	// Updated indicates that an existing resource has been updated in a collection.
	Updated

	// Deleted indicates an existing resource has been deleted from a collection.
	Deleted

	// FullSync indicates that the source has completed the publishing of initial state of a collection as a series
	// of add events. Events after FullSync are incremental change events that were applied to the origin collection.
	FullSync

	// Reset indicates that the originating event.Source had a change that cannot be recovered from (e.g. CRDs have
	// changed). It indicates that the listener should abandon its internal state and restart. This is a source-level
	// event and applies to all collections.
	Reset
)

// String implements Stringer.String
func (k Kind) String() string {
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
	case Reset:
		return "Reset"
	default:
		return fmt.Sprintf("<<Unknown Kind %d>>", k)
	}
}
