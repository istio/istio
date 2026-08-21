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
	"cmp"

	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

var log = istiolog.RegisterScope("krt", "")

type Metadata map[string]any

// Collection is the core resource type for krt, representing a collection of objects. Items can be listed, fetched,
// or watched for changes. The implementation is kept behind a private interface so Collection can also expose generic
// convenience methods.
type Collection[T any] struct {
	internalCollection[T]
}

func newCollection[T any](c internalCollection[T]) Collection[T] {
	return Collection[T]{internalCollection: c}
}

func (t Collection[T]) Fetch(ctx HandlerContext, opts ...FetchOption) []T {
	return Fetch(ctx, t, opts...)
}

func (t Collection[T]) FetchOne(ctx HandlerContext, opts ...FetchOption) *T {
	return FetchOne(ctx, t, opts...)
}

func (t Collection[T]) FetchOrList(ctx HandlerContext, opts ...FetchOption) []T {
	return FetchOrList(ctx, t, opts...)
}

func (t Collection[T]) List() []T {
	return t.internalCollection.ListFiltered(nil)
}

// ListFiltered returns the collection items accepted by filter. A nil filter returns all items.
func (t Collection[T]) ListFiltered(filter func(T) bool) []T {
	return t.internalCollection.ListFiltered(filter)
}

func (t Collection[T]) ListSorted() []T {
	return t.ListSortedBy(func(t T) string {
		return GetKey(t)
	})
}

func (t Collection[T]) ListSortedBy[K cmp.Ordered](extract func(T) K) []T {
	return slices.SortBy(t.List(), extract)
}

// Register adds an event watcher to the collection. Any time an item in the collection changes, the handler is called.
func (t Collection[T]) Register(f func(Event[T])) HandlerRegistration {
	return registerHandlerAsBatched(t.internalCollection, f)
}

func (t Collection[T]) internal() internalCollection[T] {
	return t.internalCollection
}

// IsNil reports whether the collection has no underlying implementation.
func (t Collection[T]) IsNil() bool {
	return t.internalCollection == nil
}

// collectionTrait is the implementation contract wrapped by Collection.
type collectionTrait[T any] interface {
	// GetKey returns an object by its key, if present. Otherwise, nil is returned.
	GetKey(k string) *T

	// ListFiltered returns objects matching filter. A nil filter returns all objects.
	// Order of the list is undefined.
	ListFiltered(filter func(T) bool) []T

	// EventStream provides event handling capabilities for the collection, allowing clients to subscribe to changes
	// and receive notifications when objects are added, modified, or removed.
	EventStream[T]

	// Metadata returns the metadata associated with this collection.
	// This can be used to store and retrieve arbitrary key-value pairs
	// that provide additional context or configuration for the collection.
	Metadata() Metadata
}

// EventStream provides a link between the underlying collection
// and its clients.
// The EventStream does not publish events for retrigger operations
// where the resultant object of type T is equal to an existing
// object in the collection.
//
// On initial sync, events will be published to registered clients
// as the Collection is populated.
type EventStream[T any] interface {
	Syncer

	// RegisterBatch registers a handler that accepts multiple events at once. This can be useful as an optimization.
	// Otherwise, behaves the same as Register.
	// Additionally, skipping the default behavior of "send all current state through the handler" can be turned off.
	// This is important when we register in a handler itself, which would cause duplicative events.
	// Handlers MUST not mutate the event list.
	RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration
}

// internalCollection extends collectionTrait for internal usage. All collection implementations must implement this
// type, but the additional methods remain hidden from external users by the Collection wrapper.
type internalCollection[T any] interface {
	collectionTrait[T]

	// Name is a human facing name for this collection.
	// Note this may not be universally unique
	name() string
	// Uid is an internal unique ID for this collection. MUST be globally unique
	uid() collectionUID

	dump() CollectionDump

	// Augment mutates an object for use in various function calls. See WithObjectAugmentation
	augment(any) any

	// Create a new index into the collection
	index(name string, extract func(o T) []string) indexer[T]
}

type indexer[T any] interface {
	Lookup(key string) []T
}

type uidable interface {
	uid() collectionUID
}

// Singleton is a special Collection that only ever has a single object. They can be converted to the Collection where convenient,
// but when using directly offer a more ergonomic API
type Singleton[T any] interface {
	// Get returns the object, or nil if there is none.
	Get() *T
	// Register adds an event watcher to the object. Any time it changes, the handler will be called
	Register(f func(o Event[T])) HandlerRegistration
	AsCollection() Collection[T]
	Metadata() Metadata
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

type HandlerRegistration interface {
	Syncer
	UnregisterHandler()
}

// HandlerContext is an opaque type passed into transformation functions.
// This can be used with Fetch to dynamically query for resources.
// Note: this doesn't expose Fetch as a method, as Go generics do not support arbitrary generic types on methods.
type HandlerContext interface {
	// DiscardResult triggers the result of this invocation to be skipped
	// This allows a collection to mark that the current state is *invalid* and should use the last-known state.
	//
	// Note: this differs from returning `nil`, which would otherwise wipe out the last known state.
	//
	// Note: if the current resource has never been computed, the result will not be discarded if it is non-nil. This allows
	// setting a default. For example, you may always return a static default config if the initial results are invalid,
	// but not revert to this config if later results are invalid. Results can unconditionally be discarded by returning nil.
	DiscardResult()
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
	// TransformationMultiStatus represents a one-to-many relationship between I and O, along with an output Status about I.
	TransformationMultiStatus[I, IStatus, O any] func(ctx HandlerContext, i I) (*IStatus, []O)
	// TransformationSingleStatus represents a one-to-one relationship between I and O, along with an output Status about I.
	TransformationSingleStatus[I, IStatus, O any] func(ctx HandlerContext, i I) (*IStatus, *O)
	// TransformationEmptyToMulti represents a singleton operator that returns a set of objects. There are no inputs.
	TransformationEmptyToMulti[T any] func(ctx HandlerContext) []T
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
