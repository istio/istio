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

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type StaticCollection[T any] struct {
	*staticList[T]
}

type staticList[T any] struct {
	mu             sync.RWMutex
	vals           map[string]T
	eventHandlers  *handlerSet[T]
	id             collectionUID
	stop           <-chan struct{}
	collectionName string
	syncer         Syncer
	metadata       Metadata
	indexes        map[string]staticListIndex[T]
}

func NewStaticCollection[T any](synced Syncer, vals []T, opts ...CollectionOption) StaticCollection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Static[%v]", ptr.TypeName[T]())
	}

	res := make(map[string]T, len(vals))
	for _, v := range vals {
		res[GetKey(v)] = v
	}

	if synced == nil {
		synced = alwaysSynced{}
	}

	sl := &staticList[T]{
		eventHandlers:  newHandlerSet[T](),
		vals:           res,
		id:             nextUID(),
		stop:           o.stop,
		collectionName: o.name,
		syncer:         synced,
		indexes:        make(map[string]staticListIndex[T]),
	}

	if o.metadata != nil {
		sl.metadata = o.metadata
	}

	c := StaticCollection[T]{
		staticList: sl,
	}
	maybeRegisterCollectionForDebugging[T](c, o.debugger)
	return c
}

// DeleteObject deletes an object from the collection.
func (s *staticList[T]) DeleteObject(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, f := s.vals[k]
	if f {
		delete(s.vals, k)
		for _, index := range s.indexes {
			index.delete(old, k)
		}
		s.eventHandlers.Distribute([]Event[T]{{
			Old:   &old,
			Event: controllers.EventDelete,
		}}, false)
	}
}

// DeleteObjects deletes all objects matching the provided filter
func (s StaticCollection[T]) DeleteObjects(filter func(obj T) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed []Event[T]
	for k, v := range s.vals {
		if filter(v) {
			delete(s.vals, k)
			for _, index := range s.indexes {
				index.delete(v, k)
			}
			removed = append(removed, Event[T]{
				Old:   &v,
				Event: controllers.EventDelete,
			})
		}
	}
	if len(removed) > 0 {
		s.eventHandlers.Distribute(removed, false)
	}
}

func (s StaticCollection[T]) Reset(newState []T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var updates []Event[T]
	nv := map[string]T{}
	for _, incoming := range newState {
		k := GetKey(incoming)
		nv[k] = incoming
		if old, f := s.vals[k]; f {
			if !Equal(old, incoming) {
				ev := Event[T]{
					Old:   &old,
					New:   &incoming,
					Event: controllers.EventUpdate,
				}
				for _, index := range s.indexes {
					index.update(ev, k)
				}
				updates = append(updates, ev)
			}
		} else {
			ev := Event[T]{
				New:   &incoming,
				Event: controllers.EventAdd,
			}
			for _, index := range s.indexes {
				index.update(ev, k)
			}
			updates = append(updates, ev)
		}
		delete(s.vals, k)
	}
	for k, remaining := range s.vals {
		for _, index := range s.indexes {
			index.delete(remaining, k)
		}
		updates = append(updates, Event[T]{
			Old:   &remaining,
			Event: controllers.EventDelete,
		})
	}
	s.vals = nv
	if len(updates) > 0 {
		s.eventHandlers.Distribute(updates, false)
	}
}

// UpdateObject adds or updates an object into the collection.
func (s *staticList[T]) UpdateObject(obj T) {
	s.updateObject(obj, false)
}

// ConditionalUpdateObject adds or updates an object into the collection.
func (s *staticList[T]) ConditionalUpdateObject(obj T) {
	s.updateObject(obj, true)
}

func (s *staticList[T]) updateObject(obj T, conditional bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := GetKey(obj)
	old, f := s.vals[k]
	s.vals[k] = obj
	if f {
		if conditional && Equal(old, obj) {
			return
		}

		ev := Event[T]{
			Old:   &old,
			New:   &obj,
			Event: controllers.EventUpdate,
		}
		for _, index := range s.indexes {
			index.update(ev, k)
		}
		s.eventHandlers.Distribute([]Event[T]{ev}, false)
	} else {
		ev := Event[T]{
			New:   &obj,
			Event: controllers.EventAdd,
		}
		for _, index := range s.indexes {
			index.update(ev, k)
		}
		s.eventHandlers.Distribute([]Event[T]{ev}, false)
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

func (s *staticList[T]) Metadata() Metadata {
	return s.metadata
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) name() string {
	return s.collectionName
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) uid() collectionUID {
	return s.id
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) dump() CollectionDump {
	return CollectionDump{
		Outputs: eraseMap(slices.GroupUnique(s.List(), getTypedKey)),
		Synced:  s.HasSynced(),
	}
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) augment(a any) any {
	return a
}

// nolint: unused // (not true)
type staticListIndex[T any] struct {
	extract func(o T) []string
	index   map[string]sets.Set[string]
	parent  *staticList[T]
}

// nolint: unused // (not true)
func (s staticListIndex[T]) Lookup(key string) []T {
	s.parent.mu.RLock()
	defer s.parent.mu.RUnlock()
	keys := s.index[key]

	res := make([]T, 0, len(keys))
	for k := range keys {
		v, f := s.parent.vals[k]
		if !f {
			log.WithLabels("key", k).Errorf("invalid index state, object does not exist")
			continue
		}
		res = append(res, v)
	}
	return res
}

func (s staticListIndex[T]) delete(o T, oKey string) {
	oldIndexKeys := s.extract(o)
	for _, oldIndexKey := range oldIndexKeys {
		sets.DeleteCleanupLast(s.index, oldIndexKey, oKey)
	}
}

func (s staticListIndex[T]) update(ev Event[T], oKey string) {
	if ev.Old != nil {
		s.delete(*ev.Old, oKey)
	}
	if ev.New != nil {
		newIndexKeys := s.extract(*ev.New)
		for _, newIndexKey := range newIndexKeys {
			sets.InsertOrNew(s.index, newIndexKey, oKey)
		}
	}
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) index(name string, extract func(o T) []string) indexer[T] {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx, ok := s.indexes[name]; ok {
		return idx
	}

	idx := staticListIndex[T]{
		extract: extract,
		index:   make(map[string]sets.Set[string]),
		parent:  s,
	}

	for k, v := range s.vals {
		idx.update(Event[T]{
			Old:   nil,
			New:   &v,
			Event: controllers.EventAdd,
		}, k)
	}
	s.indexes[name] = idx

	return idx
}

func (s *staticList[T]) List() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Values(s.vals)
}

func (s *staticList[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched(s, f)
}

func (s *staticList[T]) HasSynced() bool {
	return s.syncer.HasSynced()
}

func (s *staticList[T]) WaitUntilSynced(stop <-chan struct{}) bool {
	return s.syncer.WaitUntilSynced(stop)
}

func (s *staticList[T]) Synced() Syncer {
	// We are always synced in the static collection since the initial state must be provided upfront
	return alwaysSynced{}
}

func (s *staticList[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	s.mu.Lock()
	defer s.mu.Unlock()
	var objs []Event[T]
	if runExistingState {
		for _, v := range s.vals {
			objs = append(objs, Event[T]{
				New:   &v,
				Event: controllers.EventAdd,
			})
		}
	}
	return s.eventHandlers.Insert(f, s.Synced(), objs, s.stop)
}

var _ internalCollection[any] = &staticList[any]{}
