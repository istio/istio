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

// Controller defines an event controller loop.
// Used in combination with registry, the controller guarantees the following
// consistency requirements:
// - registry view in the controller is as AT LEAST as fresh as the moment
// notification arrives, but MAY BE more fresh (e.g. "delete" cancels
// "add" event).
type Controller interface {
	// AppendHandler appends a handler for a config resource.  Handlers execute
	// on the single worker queue in the order they are appended.  Handlers
	// receive the notification event and the associated object.  Note: this
	// method is not thread-safe, please use it before calling Run
	AppendHandler(kind string, f func(*Config, Event)) error

	// AppendServiceHandler
	AppendServiceHandler(f func(*Service, Event)) error

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
