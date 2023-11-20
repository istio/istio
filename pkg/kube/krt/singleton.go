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

// singletonAdapter exposes a singleton as a collection
type singletonAdapter[T any] struct {
	s Singleton[T]
}

func (s singletonAdapter[T]) RegisterBatch(f func(o []Event[T])) {
	s.s.Register(func(o Event[T]) {
		f([]Event[T]{o})
	})
}

func (s singletonAdapter[T]) Register(f func(o Event[T])) {
	s.s.Register(f)
}

func (s singletonAdapter[T]) GetKey(k Key[T]) *T {
	return s.s.Get()
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
		for _, handler := range h.handlers {
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

func NewSingleton[T any](hf TransformationEmpty[T], opts ...CollectionOption) Singleton[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Singleton[%v]", ptr.TypeName[T]())
	}
	h := &singleton[T]{
		name:   o.name,
		handle: hf,
		deps:   map[untypedCollection]dependency{},
		state:  atomic.NewPointer[T](nil),
	}
	log := log.WithLabels("owner", h.name)
	// TODO: wait for dependencies to be ready
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
					} else {
						log.Debugf("Add no match %v", getName(*o.New))
					}
				case controllers.EventDelete:
					if dep.filter.Matches(*o.Old) {
						log.Debugf("delete match %v", getName(*o.Old))
						matched = true
						break
					} else {
						log.Debugf("Add no match %v", getName(*o.Old))
					}
				case controllers.EventUpdate:
					if dep.filter.Matches(*o.New) {
						log.Debugf("Update match %v", getName(*o.New))
						matched = true
						break
					} else if dep.filter.Matches(*o.Old) {
						log.Debugf("Update no match, but used to %v", getName(*o.New))
						matched = true
						break
					} else {
						log.Debugf("Update no change")
					}
				}
			}
			if matched {
				h.execute()
			}
		})
	}
	return h
}

type singleton[T any] struct {
	name       string
	deps       map[untypedCollection]dependency
	handle     TransformationEmpty[T]
	handlersMu sync.RWMutex
	handlers   []func(o []Event[T])
	state      *atomic.Pointer[T]
}

func (h *singleton[T]) Name() string {
	return h.name
}

func (h *singleton[T]) _internalHandler() {
}

func (h *singleton[T]) AsCollection() Collection[T] {
	return singletonAdapter[T]{h}
}

func (h *singleton[T]) Register(f func(o Event[T])) {
	registerHandlerAsBatched[T](h, f)
}

func (h *singleton[T]) RegisterBatch(f func(o []Event[T])) {
	h.handlersMu.Lock()
	defer h.handlersMu.Unlock()
	// TODO: locking here is probably not reliable to avoid duplicate events
	// Send all existing objects through handler
	//objs := slices.Map(h.List(metav1.NamespaceAll), func(t T) Event[T] {
	//	return Event[T]{
	//		New:   ptr.Of(t),
	//		Event: controllers.EventAdd,
	//	}
	//})
	//if len(objs) > 0 {
	//	f(objs)
	//}
	h.handlers = append(h.handlers, f)
}

// registerDependency creates a
func (h *singleton[T]) registerDependency(d dependency) {
	h.deps[d.collection.original] = d
}

func (h *singleton[T]) Get() *T {
	return h.state.Load()
}

func (h *singleton[T]) GetKey(k Key[T]) *T {
	// TODO implement me
	panic("implement me")
}

func (h *singleton[T]) List(namespace string) []T {
	// TODO: use namespace?
	return ptr.ToList(h.Get())
}
