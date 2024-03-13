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

package krt

import (
	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/istio/pkg/log"
)

var log = istiolog.RegisterScope("krt", "")

// Collection is the core resource type for krt, representing a collection of objects. Items can be listed, or fetched
// directly. Most importantly, consumers can subscribe to events when objects change.
type Collection[T any] interface {
	// GetKey returns an object by it's key, if present. Otherwise, nil is returned.
	GetKey(k Key[T]) *T

	// List returns all objects in the queried namespace.
	// Order of the list is undefined.
	// Note: not all T types have a "Namespace"; a non-empty namespace is only valid for types that do have a namespace.
	List(namespace string) []T

	EventStream[T]
}

type EventStream[T any] interface {
	// Register adds an event watcher to the collection. Any time an item in the collection changes, the handler will be
	// called. Typically, usage of Register is done internally in krt via composition of Collections with Transformations
	// (NewCollection, NewManyCollection, NewSingleton); however, at boundaries of the system (connecting to something not
	// using krt), registering directly is expected.
	Register(f func(o Event[T])) Syncer

	// Synced returns a Syncer which can be used to determine if the collection has synced. Once its synced, all dependencies have
	// been processed, and all handlers have been called with the results.
	Synced() Syncer

	// RegisterBatch registers a handler that accepts multiple events at once. This can be useful as an optimization.
	// Otherwise, behaves the same as Register.
	// Additionally, skipping the default behavior of "send all current state through the handler" can be turned off.
	// This is important when we register in a handler itself, which would cause duplicative events.
	RegisterBatch(f func(o []Event[T]), runExistingState bool) Syncer
}

// internalCollection is a superset of Collection for internal usage. All collections must implement this type, but
// we only expose some functions to external users for simplicity.
type internalCollection[T any] interface {
	Collection[T]

	// Name is a human facing name for this collection.
	// Note this may not be universally unique
	name() string

	dump()

	// Augment mutates an object for use in various function calls. See WithObjectAugmentation
	augment(any) any
}

// Singleton is a special Collection that only ever has a single object. They can be converted to the Collection where convenient,
// but when using directly offer a more ergonomic API
type Singleton[T any] interface {
	// Get returns the object, or nil if there is none.
	Get() *T
	// Register adds an event watcher to the object. Any time it changes, the handler will be called
	Register(f func(o Event[T])) Syncer
	AsCollection() Collection[T]
}

// Event represents a point in time change for a collection.
type Event[T any] struct {
	// Old object, set on Update or Delete.
	Old *T
	// New object, set on Add or Update
	New *T
	// Event is the change type
	Event controllers.EventType
}

// Items returns both the Old and New object, if present.
func (e Event[T]) Items() []T {
	res := make([]T, 0, 2)
	if e.Old != nil {
		res = append(res, *e.Old)
	}
	if e.New != nil {
		res = append(res, *e.New)
	}
	return res
}

// Latest returns only the latest object (New for add/update, Old for delete).
func (e Event[T]) Latest() T {
	if e.New != nil {
		return *e.New
	}
	return *e.Old
}

// HandlerContext is an opaque type passed into transformation functions.
// This can be used with Fetch to dynamically query for resources.
// Note: this doesn't expose Fetch as a method, as Go generics do not support arbitrary generic types on methods.
type HandlerContext interface {
	// _internalHandler is an interface that can only be implemented by this package.
	_internalHandler()
}

// FetchOption is a functional argument type that can be passed to Fetch.
// These are all created by the various Filter* functions
type FetchOption func(*dependency)

// CollectionOption is a functional argument type that can be passed to Collection constructors.
type CollectionOption func(*collectionOptions)

// Transformations represent functions that derive some output types from an input type.
type (
	// TransformationEmpty represents a singleton operation. There is always a single output.
	// Note this can still depend on other types, via Fetch.
	TransformationEmpty[T any] func(ctx HandlerContext) *T
	// TransformationSingle represents a one-to-one relationship between I and O.
	TransformationSingle[I, O any] func(ctx HandlerContext, i I) *O
	// TransformationMulti represents a one-to-many relationship between I and O.
	TransformationMulti[I, O any] func(ctx HandlerContext, i I) []O
)

// Key is a string, but with a type associated to avoid mixing up keys
type Key[O any] string

// ResourceNamer is an optional interface that can be implemented by collection types.
// If implemented, this can be used to determine the Key for an object
type ResourceNamer interface {
	ResourceName() string
}

// Equaler is an optional interface that can be implemented by collection types.
// If implemented, this will be used to determine if an object changed.
type Equaler[K any] interface {
	Equals(k K) bool
}

// LabelSelectorer is an optional interface that can be implemented by collection types.
// If implemented, this will be used to determine an objects' LabelSelectors
type LabelSelectorer interface {
	GetLabelSelector() map[string]string
}

// Labeler is an optional interface that can be implemented by collection types.
// If implemented, this will be used to determine an objects' Labels
type Labeler interface {
	GetLabels() map[string]string
}

// Namer is an optional interface that can be implemented by collection types.
// If implemented, this will be used to determine an objects' Name.
type Namer interface {
	GetName() string
}

// Namespacer is an optional interface that can be implemented by collection types.
// If implemented, this will be used to determine an objects' Namespace.
type Namespacer interface {
	GetNamespace() string
}
