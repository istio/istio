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
	"sync"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
)

type dummyValue struct{}

func (d dummyValue) ResourceName() string {
	return ""
}

type dummyCollection struct {}

func (d dummyCollection) GetKey(k Key[dummyValue]) *dummyValue {
	return ptr.Of(dummyValue{})
}

func (d dummyCollection) List(namespace string) []dummyValue {
	return []dummyValue{{}}
}

func (d dummyCollection) Register(f func(o Event[dummyValue])) Syncer {
	return registerHandlerAsBatched[dummyValue](d, f)
}

func (d dummyCollection) RegisterBatch(f func(o []Event[dummyValue])) Syncer {
	f([]Event[dummyValue]{{
		New:   ptr.Of(dummyValue{}),
		Event: controllers.EventAdd,
	}})
	return alwaysSynced{}
}

func (d dummyCollection) Name() string {
	return "empty"
}

func (d dummyCollection) Synced() Syncer {
	return alwaysSynced{}
}

var _ Collection[dummyValue] = dummyCollection{}

// singletonAdapter exposes a singleton as a collection
type singletonAdapter[T any] struct {
	s Singleton[T]
}

func (s singletonAdapter[T]) RegisterBatch(f func(o []Event[T])) Syncer {
	return s.s.Register(func(o Event[T]) {
		f([]Event[T]{o})
	})
}

func (s singletonAdapter[T]) Register(f func(o Event[T])) Syncer {
	return s.s.Register(f)
}

func (s singletonAdapter[T]) GetKey(k Key[T]) *T {
	return s.s.Get()
}

func (s singletonAdapter[T]) Synced() Syncer {
	// TODO: this is wrong
	return alwaysSynced{}
}

func (s singletonAdapter[T]) List(namespace string) []T {
	res := s.s.Get()
	if res == nil {
		return nil
	}
	return []T{*res}
}

func (s singletonAdapter[T]) Name() string {
	return s.s.Name()
}

var _ Collection[any] = &singletonAdapter[any]{}

func (h *singleton[T]) execute() {
	res := h.handle(h)
	oldRes := h.state.Swap(res)
	updated := !equal(res, oldRes)
	if updated {
		for _, handler := range h.handlers.Get() {
			event := controllers.EventUpdate
			if oldRes == nil {
				event = controllers.EventAdd
			} else if res == nil {
				event = controllers.EventDelete
			}
			handler([]Event[T]{{
				Old:   oldRes,
				New:   res,
				Event: event,
			}})
		}
	}
}

type collectionAdapter[T any] struct {
	c Collection[T]
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

func (c collectionAdapter[T]) Name() string {
	return c.c.Name()
}

func (c collectionAdapter[T]) AsCollection() Collection[T] {
	return c.c
}

var _ Singleton[any] = &collectionAdapter[any]{}

func NewSingleton2[O any](hf TransformationEmpty[O], opts ...CollectionOption) Singleton[O] {
	col := NewCollection[dummyValue, O](dummyCollection{}, func(ctx HandlerContext, _ dummyValue) *O {
		return hf(ctx)
	}, opts...)
	return collectionAdapter[O]{col}
}
func NewSingleton[T any](hf TransformationEmpty[T], opts ...CollectionOption) Singleton[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Singleton[%v]", ptr.TypeName[T]())
	}
	synced := make(chan struct{})
	h := &singleton[T]{
		name:     o.name,
		handle:   hf,
		deps:     map[untypedCollection]dependency{},
		state:    atomic.NewPointer[T](nil),
		handlers: &handlers[T]{},
		synced:   synced,
	}
	log := log.WithLabels("owner", h.name)
	// Populate initial state. It is a singleton so we don't have any hard dependencies
	h.execute()
	mu := sync.Mutex{}
	for _, dep := range h.deps {
		dep := dep
		log.Debugf("insert dependency, filter: %+v", dep.filter)
		dep.collection.register(func(events []Event[any]) {
			mu.Lock()
			defer mu.Unlock()
			matched := false
			for _, o := range events {
				log.Debugf("got event %v", o.Event)
				switch o.Event {
				case controllers.EventAdd:
					if dep.filter.Matches(*o.New) {
						log.Debugf("Add match %v", getName(*o.New))
						matched = true
						break
					}
					log.Debugf("Add no match %v", getName(*o.New))
				case controllers.EventDelete:
					if dep.filter.Matches(*o.Old) {
						log.Debugf("delete match %v", getName(*o.Old))
						matched = true
						break
					}
					log.Debugf("Add no match %v", getName(*o.Old))
				case controllers.EventUpdate:
					if dep.filter.Matches(*o.New) {
						log.Debugf("Update match %v", getName(*o.New))
						matched = true
						break
					} else if dep.filter.Matches(*o.Old) {
						log.Debugf("Update no match, but used to %v", getName(*o.New))
						matched = true
						break
					}
					log.Debugf("Update no change")
				}
			}
			if matched {
				h.execute()
			}
		})
	}

	close(synced)
	return h
}

type singleton[T any] struct {
	name     string
	deps     map[untypedCollection]dependency
	handle   TransformationEmpty[T]
	handlers *handlers[T]
	state    *atomic.Pointer[T]
	synced   chan struct{}
}

func (h *singleton[T]) Name() string {
	return h.name
}

func (h *singleton[T]) _internalHandler() {
}

func (h *singleton[T]) AsCollection() Collection[T] {
	return singletonAdapter[T]{h}
}

func (h *singleton[T]) Synced() Syncer {
	// TODO: this is wrong
	return alwaysSynced{}
}

func (h *singleton[T]) Register(f func(o Event[T])) Syncer {
	return registerHandlerAsBatched[T](h, f)
}

func (h *singleton[T]) RegisterBatch(f func(o []Event[T])) Syncer {
	h.handlers.Insert(f)
	// TODO: this is wrong
	return alwaysSynced{}
}

// registerDependency creates a
func (h *singleton[T]) registerDependency(d dependency) {
	h.deps[d.collection.original] = d
}

func (h *singleton[T]) Get() *T {
	return h.state.Load()
}

func (h *singleton[T]) GetKey(k Key[T]) *T {
	return h.state.Load()
}

func (h *singleton[T]) List(namespace string) []T {
	v := h.state.Load()
	if v == nil {
		return nil
	}
	return []T{*v}
}
