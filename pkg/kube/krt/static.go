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
	"sync"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

type StaticCollection[T any] struct {
	*staticList[T]
}

type staticList[T any] struct {
	mu       sync.RWMutex
	vals     map[string]T
	handlers []func(o []Event[T], initialSync bool)
	id       collectionUID
}

func NewStaticCollection[T any](vals []T) StaticCollection[T] {
	res := make(map[string]T, len(vals))
	for _, v := range vals {
		res[GetKey(v)] = v
	}
	return StaticCollection[T]{
		staticList: &staticList[T]{
			vals: res,
			id:   nextUID(),
		},
	}
}

// DeleteObject deletes an object from the collection.
func (s *staticList[T]) DeleteObject(k string) {
	s.mu.Lock() // Unlocked in runEventLocked
	old, f := s.vals[k]
	if f {
		delete(s.vals, k)
		s.runEventsLocked([]Event[T]{{
			Old:   &old,
			Event: controllers.EventDelete,
		}})
	} else {
		s.mu.Unlock()
	}
}

// DeleteObjects deletes all objects matching the provided filter
func (s StaticCollection[T]) DeleteObjects(filter func(obj T) bool) {
	s.mu.Lock() // Unlocked in runEventLocked
	var removed []Event[T]
	for k, v := range s.vals {
		if filter(v) {
			delete(s.vals, k)
			removed = append(removed, Event[T]{
				Old:   &v,
				Event: controllers.EventDelete,
			})
		}
	}
	if len(removed) > 0 {
		s.runEventsLocked(removed)
	} else {
		s.mu.Unlock()
	}
}

// UpdateObject adds or updates an object into the collection.
func (s *staticList[T]) UpdateObject(obj T) {
	s.mu.Lock() // Unlocked in runEventLocked
	k := GetKey(obj)
	old, f := s.vals[k]
	s.vals[k] = obj
	if f {
		s.runEventsLocked([]Event[T]{{
			Old:   &old,
			New:   &obj,
			Event: controllers.EventUpdate,
		}})
	} else {
		s.runEventsLocked([]Event[T]{{
			New:   &obj,
			Event: controllers.EventAdd,
		}})
	}
}

func (s *staticList[T]) GetKey(k string) *T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if o, f := s.vals[k]; f {
		return &o
	}
	return nil
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) name() string {
	return "staticList"
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) uid() collectionUID {
	return s.id
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) dump() CollectionDump {
	return CollectionDump{
		Outputs: eraseMap(slices.GroupUnique(s.List(), getTypedKey)),
	}
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) augment(a any) any {
	return a
}

// runEventLocked sends an event to all handlers. This must be called locked, and will unlock the mutex
func (s *staticList[T]) runEventsLocked(ev []Event[T]) {
	handlers := slices.Clone(s.handlers)
	s.mu.Unlock()
	for _, h := range handlers {
		h(ev, false)
	}
}

// nolint: unused // (not true)
type staticListIndex[T any] struct {
	extract func(o T) []string
	parent  *staticList[T]
}

// nolint: unused // (not true)
func (s staticListIndex[T]) Lookup(key string) []any {
	var res []any
	s.parent.mu.RLock()
	defer s.parent.mu.RUnlock()
	for _, v := range s.parent.vals {
		have := s.extract(v)
		if slices.Contains(have, key) {
			res = append(res, v)
		}
	}
	return res
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) index(extract func(o T) []string) kclient.RawIndexer {
	return staticListIndex[T]{
		extract: extract,
		parent:  s,
	}
}

func (s *staticList[T]) List() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Values(s.vals)
}

func (s *staticList[T]) Register(f func(o Event[T])) Syncer {
	return registerHandlerAsBatched(s, f)
}

func (s *staticList[T]) Synced() Syncer {
	// We are always synced in the static collection since the initial state must be provided upfront
	return alwaysSynced{}
}

func (s *staticList[T]) RegisterBatch(f func(o []Event[T], initialSync bool), runExistingState bool) Syncer {
	s.mu.Lock()
	s.handlers = append(s.handlers, f)
	var objs []T
	if runExistingState {
		objs = maps.Values(s.vals)
	}
	s.mu.Unlock()

	if runExistingState {
		// Run handler out of the lock
		f(slices.Map(objs, func(e T) Event[T] {
			return Event[T]{
				New:   &e,
				Event: controllers.EventAdd,
			}
		}), true)
	}

	// We are always synced in the static collection since the initial state must be provided upfront
	return alwaysSynced{}
}

var _ internalCollection[any] = &staticList[any]{}
