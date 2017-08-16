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

package store

import (
	"context"
	"errors"

	"github.com/gogo/protobuf/proto"
)

// ErrNotFound is the error to be returned when the given key does not exist in the storage.
var ErrNotFound = errors.New("not found")

// Key represents the key to identify a resource in the store.
type Key struct {
	Kind      string
	Namespace string
	Name      string
}

// Event represents an event. Used by Store2.Watch.
type Event struct {
	Key
	Type ChangeType
}

// Validator defines the interface to validate a new change.
type Validator interface {
	Validate(t ChangeType, key Key, spec proto.Message) bool
}

// Store2 defines the access to the storage for mixer.
// TODO: rename to Store.
type Store2 interface {
	// SetValidator sets the validator for the store.
	SetValidator(v Validator)

	// RegisterKind registers a kind to be structured as the given proto.Message.
	// This will be used for structural validations.
	RegisterKind(kind string, spec proto.Message)

	// Init initializes the connection with the storage backend. Any further registrations
	// of kind through RegisterKind() will be ignored after Init(). The connection will
	// be closed after ctx is done.
	Init(ctx context.Context) error

	// Watch creates a channel to receive the events on the given kinds.
	Watch(ctx context.Context, kinds []string) (<-chan Event, error)

	// Get returns a resource's spec to the key.
	Get(key Key, spec proto.Message) error

	// List returns the whole mapping from key to resource specs in the store.
	List() map[Key]proto.Message

	// ListKeys returns the list of all keys in the store.
	ListKeys() []Key

	// Put creates or updates the spec for the certain key.
	Put(key Key, spec proto.Message) error

	// Delete delets the resource for the key.
	Delete(key Key) error
}
