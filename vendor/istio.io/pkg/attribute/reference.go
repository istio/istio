// Copyright 2017 Istio Authors
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

package attribute

// Reference is a reference to an attribute.
type Reference struct {
	Name   string
	MapKey string
}

// Presence keeps tracks of the attribute reference outcome.
type Presence int32

// Values for the presence
const (
	ConditionUnspecified Presence = 0
	Absence              Presence = 1
	Exact                Presence = 2
	Regex                Presence = 3
)

// ReferencedAttributeSnapshot keeps track of the attribute reference state for a mutable bag.
// You can snapshot the referenced attributes with SnapshotReferencedAttributes and later
// reinstall them with RestoreReferencedAttributes. Note that a snapshot can only be used
// once, the RestoreReferencedAttributes call is destructive.
type ReferencedAttributeSnapshot struct {
	// ReferenceAttrs is a collection of references to attributes
	ReferencedAttrs map[Reference]Presence
}

// ReferenceTracker for an attribute bag
type ReferenceTracker interface {
	// MapReference records access of a string map record.
	MapReference(name, key string, condition Presence)

	// Reference records access of an attribute by name.
	Reference(name string, condition Presence)

	// Clear the list of referenced attributes being tracked by this bag
	Clear()

	// Restore the list of referenced attributes being tracked by this bag
	Restore(snap ReferencedAttributeSnapshot)

	// Snapshot grabs a snapshot of the currently referenced attributes
	Snapshot() ReferencedAttributeSnapshot
}
