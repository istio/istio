// Copyright 2016 Google Inc.
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

package model

// Controller defines an event controller loop
type Controller interface {
	// AppendHandler adds a handler for a config resource.
	// Handler executes on the single worker queue and applies all functions for
	// a kind to the notification object until all functions stop returning an error.
	// Note: this method is not thread-safe, please use it before calling Run
	AppendHandler(kind string, f func(*Config, Event) error) error

	// Run until a signal is received
	Run(stop chan struct{})
}

// Event represents a registry update event
type Event int

const (
	// EventAdd is sent when an object is added
	EventAdd Event = 1
	// EventUpdate is sent when an object is modified
	// Captures the modified object
	EventUpdate Event = 2
	// EventDelete is sent when an object is deleted
	// Captures the object at the last known state
	EventDelete Event = 3
)

func (event Event) String() string {
	out := "unknown"
	switch event {
	case EventAdd:
		out = "add"
	case EventUpdate:
		out = "update"
	case EventDelete:
		out = "delete"
	}
	return out
}
