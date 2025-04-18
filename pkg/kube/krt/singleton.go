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
	"fmt"
	"sync/atomic"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// dummyValue is a placeholder value for use with dummyCollection.
type dummyValue struct{}

func (d dummyValue) ResourceName() string {
	return ""
}

type StaticSingleton[T any] interface {
	Singleton[T]
	Set(*T)
	MarkSynced()
}

func NewStatic[T any](initial *T, startSynced bool, opts ...CollectionOption) StaticSingleton[T] {
	val := new(atomic.Pointer[T])
	val.Store(initial)
	x := &static[T]{
		val:           val,
		synced:        &atomic.Bool{},
		id:            nextUID(),
		eventHandlers: &handlers[T]{},
	}
	x.synced.Store(startSynced)
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Static[%v]", ptr.TypeName[T]())
	}
	x.collectionName = o.name
	x.syncer = pollSyncer{
		name: x.collectionName,
		f: func() bool {
			return x.synced.Load()
		},
	}
	maybeRegisterCollectionForDebugging(x, o.debugger)
	return collectionAdapter[T]{x}
}

// static represents a Collection of a single static value. This can be explicitly Set() to override it
type static[T any] struct {
	val            *atomic.Pointer[T]
	synced         *atomic.Bool
	id             collectionUID
	eventHandlers  *handlers[T]
	collectionName string
	syncer         Syncer
}

func (d *static[T]) GetKey(k string) *T {
	return d.val.Load()
}

func (d *static[T]) List() []T {
	v := d.val.Load()
	if v == nil {
		return nil
	}
	return []T{*v}
}

func (d *static[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched[T](d, f)
}

func (d *static[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	reg := d.eventHandlers.Insert(f)
	if runExistingState {
		v := d.val.Load()
		if v != nil {
			f([]Event[T]{{
				New:   v,
				Event: controllers.EventAdd,
			}})
		}
	}

	return staticHandler{Syncer: d.syncer, remove: func() {
		d.eventHandlers.Delete(reg)
	}}
}

type staticHandler struct {
	Syncer
	remove func()
}

func (s staticHandler) UnregisterHandler() {
	s.remove()
}

func (d *static[T]) Synced() Syncer {
	return pollSyncer{
		name: d.collectionName,
		f: func() bool {
			return d.synced.Load()
		},
	}
}

func (d *static[T]) HasSynced() bool {
	return d.syncer.HasSynced()
}

func (d *static[T]) WaitUntilSynced(stop <-chan struct{}) bool {
	return d.syncer.WaitUntilSynced(stop)
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
func (d *static[T]) dump() CollectionDump {
	return CollectionDump{
		Outputs: map[string]any{
			"static": d.val.Load(),
		},
		Synced: d.HasSynced(),
	}
}

// nolint: unused // (not true, its to implement an interface)
func (d *static[T]) augment(a any) any {
	// not supported in this collection type
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (d *static[T]) name() string {
	return d.collectionName
}

// nolint: unused // (not true, its to implement an interface)
func (d *static[T]) uid() collectionUID {
	return d.id
}

// nolint: unused // (not true, its to implement an interface)
func (d *static[T]) index(name string, extract func(o T) []string) kclient.RawIndexer {
	panic("TODO")
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

func (c collectionAdapter[T]) MarkSynced() {
	c.c.(*static[T]).synced.Store(true)
}

func (c collectionAdapter[T]) Set(t *T) {
	c.c.(*static[T]).Set(t)
}

func (c collectionAdapter[T]) Get() *T {
	// Guaranteed to be 0 or 1 len
	res := c.c.List()
	if len(res) == 0 {
		return nil
	}
	return &res[0]
}

func (c collectionAdapter[T]) Register(f func(o Event[T])) HandlerRegistration {
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

	// Disable debugging on the internal static collection, else we end up with duplicates
	staticOpts := append(slices.Clone(opts), WithDebugging(nil))
	dummyCollection := NewStatic[dummyValue](&dummyValue{}, true, staticOpts...).AsCollection()
	col := NewCollection[dummyValue, O](dummyCollection, func(ctx HandlerContext, _ dummyValue) *O {
		return hf(ctx)
	}, opts...)
	return collectionAdapter[O]{col}
}

// NewManyFromNothing is a niche Collection type that doesn't have any input dependencies. This is useful where things
// only rely on out-of-band data via RecomputeTrigger, for instance.
func NewManyFromNothing[O any](hf TransformationEmptyToMulti[O], opts ...CollectionOption) Collection[O] {
	// Disable debugging on the internal static collection, else we end up with duplicates
	staticOpts := append(slices.Clone(opts), WithDebugging(nil))
	dummyCollection := NewStatic[dummyValue](&dummyValue{}, true, staticOpts...).AsCollection()
	col := NewManyCollection[dummyValue, O](dummyCollection, func(ctx HandlerContext, _ dummyValue) []O {
		return hf(ctx)
	}, opts...)
	return col
}
