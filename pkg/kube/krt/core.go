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
	"reflect"

	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

var log = istiolog.RegisterScope("krt", "")

type Collection[T any] interface {
	GetKey(k Key[T]) *T
	List(namespace string) []T
	Register(f func(o Event[T]))
	RegisterBatch(f func(o []Event[T]))
}

type Singleton[T any] interface {
	Get() *T
	Register(f func(o Event[T]))
	AsCollection() Collection[T]
}

func batchedRegister[T any](c Collection[T], f func(o Event[T])) {
	c.RegisterBatch(func(events []Event[T]) {
		for _, o := range events {
			f(o)
		}
	})
}

// erasedCollection is a Collection[T] that has been type-erased so it can be stored in collections
// that do not have type information.
type erasedCollection struct {
	// original stores the original typed Collection
	original any
	// registerFunc registers any Event[any] handler. These will be mapped to Event[T] when connected to the original collection.
	registerFunc func(f func(o []Event[any]))
	// TODO: since we erase things, we lose a lot of context. We should add some ID() or Name() to Collections.
}

func (e erasedCollection) register(f func(o []Event[any])) {
	e.registerFunc(f)
}

func eraseCollection[T any](c Collection[T]) erasedCollection {
	return erasedCollection{
		original: c,
		registerFunc: func(f func(o []Event[any])) {
			c.RegisterBatch(func(o []Event[T]) {
				f(slices.Map(o, castEvent[T, any]))
			})
		},
	}
}

// castEvent converts an Event[I] to Event[O].
// Caller is responsible for making sure these can be type converted.
// Typically this is converting to or from `any`.
func castEvent[I, O any](o Event[I]) Event[O] {
	e := Event[O]{
		Event: o.Event,
	}
	if o.Old != nil {
		e.Old = ptr.Of(any(*o.Old).(O))
	}
	if o.New != nil {
		e.New = ptr.Of(any(*o.New).(O))
	}
	return e
}

// Key is a string, but with a type associated to avoid mixing up keys
type Key[O any] string

type resourceNamer interface {
	ResourceName() string
}

// dependency is a specific thing that can be depdnended on
type dependency struct {
	// The actual collection containing this
	collection erasedCollection
	// Filter over the collection
	filter filter
}

type depper interface {
	// Registers a dependency, returning true if it is finalized
	registerDependency(dependency)
}

type Event[T any] struct {
	// The Old event, on Update or Delete
	Old   *T
	New   *T
	Event controllers.EventType
}

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

func (e Event[T]) Latest() T {
	if e.New != nil {
		return *e.New
	}
	return *e.Old
}

type HandlerContext interface {
	_internalHandler()
}

type (
	DepOption func(*dependency)
	Option    func(map[untypedCollection]dependency)
)

type (
	TransformationEmpty[T any]     func(ctx HandlerContext) *T
	TransformationSingle[I, O any] func(ctx HandlerContext, i I) *O
	TransformationMulti[I, O any]  func(ctx HandlerContext, i I) []O
)

type Equaler[K any] interface {
	Equals(k K) bool
}

func Equal[O any](a, b O) bool {
	ak, ok := any(a).(Equaler[O])
	if ok {
		return ak.Equals(b)
	}
	ao, ok := any(a).(controllers.Object)
	if ok {
		return ao.GetResourceVersion() == any(b).(controllers.Object).GetResourceVersion()
	}
	ap, ok := any(a).(proto.Message)
	if ok {
		if reflect.TypeOf(ap.ProtoReflect().Interface()) == reflect.TypeOf(ap) {
			return proto.Equal(ap, any(b).(proto.Message))
		}
		// If not, this is an embedded proto! Sneaky.
		// TODO: panic? I don't think reflect.DeepEqual on proto is good
	}
	return reflect.DeepEqual(a, b)
}
