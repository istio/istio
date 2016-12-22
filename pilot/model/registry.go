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

import "github.com/golang/protobuf/proto"

// ConfigKey is the identity of the configuration object
type ConfigKey struct {
	// Kind specifies the type of the configuration artifact, e.g. "MyKind"
	Kind string
	// Name of the artifact, e.g. "my-name"
	Name string
	// Namespace provides the name qualifier for Kubernetes, e.g. "default"
	Namespace string
}

// Config object holds the normalized config objects defined by Kind schema
type Config struct {
	ConfigKey
	// Spec holds the configuration struct
	Spec interface{}
	// Status holds the status information (may be null)
	Status interface{}
}

// Registry of the configuration objects
// Object references supplied and returned from this interface should be
// treated as read-only. Modifying them might violate thread-safety.
type Registry interface {
	// Get retrieves a configuration element, bool indicates existence
	Get(key ConfigKey) (*Config, bool)

	// List returns objects for a kind in a namespace ("" namespace implies all)
	List(kind string, namespace string) ([]*Config, error)

	// Put adds an object to the distributed store.
	// This implies that you might not see the effect immediately (e.g. Get
	// might not return the object immediately).
	// Intermittent errors might occur even though the operation succeeds.
	Put(obj *Config) error

	// Delete remotes an object from the distributed store.
	// This implies that you might not see the effect immediately (e.g. Get
	// might not return the object immediately).
	// Intermittent errors might occur even though the operation succeeds.
	Delete(key ConfigKey) error
}

// KindMap defines bijection between Kind name and proto message name
type KindMap map[string]ProtoSchema

// ProtoSchema provides custom validation checks
type ProtoSchema struct {
	// MessageName refers to the protobuf message type name
	MessageName string
	// StatusMessageName refers to the protubuf message type name for the StatusMessageName
	StatusMessageName string
	// Description of the configuration type
	Description string
	// Validate configuration as a protobuf message
	Validate func(o proto.Message) error
}

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

// TODO: Differential computation:
// - generator and distributor should have the notion of the registry delta

// TODO: Configuration dataflow:
// - end-to-end push config to output reload
// - association betweeen generated outputs and where they go is the
//   responsibility of the individual consumers
// - diffing with source references
// - status
