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

package provider

import (
	"fmt"

	"istio.io/istio/galley/pkg/model/resource"
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

// Event represents a change that occured against a resource in the source config system.
type Event struct {
	Kind EventKind
	Id   resource.VersionedKey
}

func (e Event) String() string {
	return fmt.Sprintf("[Event](%d: %v)", e.Kind, e.Id)
}