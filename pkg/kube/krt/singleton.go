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
	"sync/atomic"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
)

// dummyValue is a placeholder value for use with dummyCollection.
type dummyValue struct{}

func (d dummyValue) ResourceName() string {
	return ""
}

type StaticSingleton[T any] interface {
	Singleton[T]
	Set(*T)
}

func NewStatic[T any](initial *T) StaticSingleton[T] {
	val := new(atomic.Pointer[T])
	val.Store(initial)
	x := &static[T]{
		val:           val,
		eventHandlers: &handlers[T]{},
	}
	return collectionAdapter[T]{x}
}

// static represents a Collection of a single static value. This can be explicitly Set() to override it
type static[T any] struct {
	val           *atomic.Pointer[T]
	eventHandlers *handlers[T]
}

func (d *static[T]) GetKey(k Key[T]) *T {
	return d.val.Load()
}

func (d *static[T]) List(namespace string) []T {
	v := d.val.Load()
	if v == nil {
		return nil
	}
	return []T{*v}
}

func (d *static[T]) Register(f func(o Event[T])) Syncer {
	return registerHandlerAsBatched[T](d, f)
}

func (d *static[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) Syncer {
	d.eventHandlers.Insert(f)
	if runExistingState {
		v := d.val.Load()
		if v != nil {
			f([]Event[T]{{
				New:   v,
				Event: controllers.EventAdd,
			}})
		}
	}
	return alwaysSynced{}
}

func (d *static[T]) Synced() Syncer {
	return alwaysSynced{}
}

func (d *static[T]) Set(now *T) {
	old := d.val.Swap(now)
	if old == now {
		return
	}
	for _, h := range d.eventHandlers.Get() {
		h([]Event[T]{toEvent[T](old, now)})
	}
}

// nolint: unused // (not true, its to implement an interface)
func (d *static[T]) dump() {
	log.Errorf(">>> static[%v]: %+v<<<", ptr.TypeName[T](), d.val.Load())
}

// nolint: unused // (not true, its to implement an interface)
func (d *static[T]) augment(a any) any {
	// not supported in this collection type
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (d *static[T]) name() string {
	return "static"
}

func toEvent[T any](old, now *T) Event[T] {
	if old == nil {
		return Event[T]{
			New:   now,
			Event: controllers.EventAdd,
		}
	} else if now == nil {
		return Event[T]{
			Old:   old,
			Event: controllers.EventDelete,
		}
	}
	return Event[T]{
		New:   now,
		Old:   old,
		Event: controllers.EventUpdate,
	}
}

var _ Collection[dummyValue] = &static[dummyValue]{}

type collectionAdapter[T any] struct {
	c Collection[T]
}

func (c collectionAdapter[T]) Set(t *T) {
	c.c.(*static[T]).Set(t)
}

func (c collectionAdapter[T]) Get() *T {
	// Guaranteed to be 0 or 1 len
	res := c.c.List("")
	if len(res) == 0 {
		return nil
	}
	return &res[0]
}

func (c collectionAdapter[T]) Register(f func(o Event[T])) Syncer {
	return c.c.Register(f)
}

func (c collectionAdapter[T]) AsCollection() Collection[T] {
	return c.c
}

var _ Singleton[any] = &collectionAdapter[any]{}

func NewSingleton[O any](hf TransformationEmpty[O], opts ...CollectionOption) Singleton[O] {
	// dummyCollection provides a trivial collection implementation that always provides a single dummyValue.
	// This is an internal construct exclusively for implementing the "Singleton" pattern.
	// This is so we can represent a singleton (a func() *O) as a collection (a func(I) *O).
	// dummyCollection just returns a single "I" that is ignored.
	dummyCollection := NewStatic[dummyValue](&dummyValue{}).AsCollection()
	col := NewCollection[dummyValue, O](dummyCollection, func(ctx HandlerContext, _ dummyValue) *O {
		return hf(ctx)
	}, opts...)
	return collectionAdapter[O]{col}
}
