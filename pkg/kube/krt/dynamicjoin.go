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
	"sync/atomic"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

// DynamicCollection is the interface for a collection that can have
// other collections added or removed at runtime. Member collections
// are keyed by the collection name.
type DynamicCollection[T any] interface {
	Collection[T]
	AddOrUpdateCollection(c Collection[T])
	RemoveCollection(c Collection[T])
}

type dynamicjoin[T any] struct {
	collectionName   string
	id               collectionUID
	collectionsByKey map[string]internalCollection[T]
	sync.RWMutex
	synced                   *atomic.Bool
	uncheckedOverlap         bool
	syncer                   Syncer
	collectionChangeHandlers []func(collectionChangeEvent[T])
}

func (j *dynamicjoin[T]) GetKey(k string) *T {
	j.RLock()
	defer j.RUnlock()

	for _, c := range maps.SeqStable(j.collectionsByKey) {
		if r := c.GetKey(k); r != nil {
			return r
		}
	}
	return nil
}

func (j *dynamicjoin[T]) List() []T {
	var res []T
	j.RLock()
	defer j.RUnlock()
	if j.uncheckedOverlap {
		first := true
		for _, c := range maps.SeqStable(j.collectionsByKey) {
			objs := c.List()
			// As an optimization, take the first (non-empty) result as-is without copying
			if len(objs) > 0 && first {
				res = objs
				first = false
			} else {
				// After the first, safely merge into the result
				res = append(res, objs...)
			}
		}
		return res
	}
	var found sets.String
	first := true

	for _, c := range maps.SeqStable(j.collectionsByKey) {
		objs := c.List()
		// As an optimization, take the first (non-empty) result as-is without copying
		// TODO: Implement custom merge out of the hot path
		if len(objs) > 0 && first {
			res = objs
			first = false
			found = sets.NewWithLength[string](len(objs))
			for _, i := range objs {
				found.Insert(GetKey(i))
			}
		} else {
			// After the first, safely merge into the result
			for _, i := range objs {
				key := GetKey(i)
				if !found.InsertContains(key) {
					// Only keep it if it is the first time we saw it, as our merging mechanism is to keep the first one
					res = append(res, i)
				}
			}
		}
	}
	return res
}

type dynamicMultiSyncer struct {
	syncers map[string]Syncer
}

func (s *dynamicMultiSyncer) HasSynced() bool {
	for _, syncer := range s.syncers {
		if !syncer.HasSynced() {
			return false
		}
	}
	return true
}

func (s *dynamicMultiSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	for _, syncer := range s.syncers {
		if !syncer.WaitUntilSynced(stop) {
			return false
		}
	}
	return true
}

type dynamicJoinHandlerRegistration struct {
	dynamicMultiSyncer
	removes map[string]func()
	sync.RWMutex
}

func (hr *dynamicJoinHandlerRegistration) UnregisterHandler() {
	hr.RLock()
	removes := hr.removes
	hr.RUnlock()
	// Unregister all the handlers
	for _, remover := range removes {
		remover()
	}
}

func (j *dynamicjoin[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched[T](j, f)
}

func (j *dynamicjoin[T]) RegisterBatch(f func(o []Event[T], initialSync bool), runExistingState bool) HandlerRegistration {
	j.RLock()
	defer j.RUnlock()
	sync := dynamicMultiSyncer{
		syncers: make(map[string]Syncer, len(j.collectionsByKey)),
	}
	removes := map[string]func(){}
	for _, c := range maps.SeqStable(j.collectionsByKey) {
		reg := c.RegisterBatch(f, runExistingState)
		removes[c.name()] = reg.UnregisterHandler
		sync.syncers[c.name()] = reg
	}
	djhr := &dynamicJoinHandlerRegistration{
		dynamicMultiSyncer: sync,
		removes:            removes,
	}
	j.registerCollectionChangeHandlerLocked(func(e collectionChangeEvent[T]) {
		djhr.Lock()
		defer djhr.Unlock()
		switch e.eventType {
		case indexEventCollectionAddOrUpdate:
			reg := e.collectionValue.RegisterBatch(f, runExistingState)
			djhr.removes[e.collectionName] = reg.UnregisterHandler
			djhr.syncers[e.collectionName] = reg
		case indexEventCollectionDelete:
			// Unregister the handler for this collection
			remover := djhr.removes[e.collectionName]
			if remover == nil {
				log.Warnf("Collection %v not found in %v", e.collectionName, j.name())
				return
			}
			remover()
			delete(djhr.removes, e.collectionName)
			delete(djhr.syncers, e.collectionName)

			// Now send a final set of remove events for each object in the collection
			// do this in a goroutine so we don't block running expensive handlers
			go func() {
				var events []Event[T]
				for _, elem := range e.collectionValue.List() {
					events = append(events, Event[T]{Old: &elem, Event: controllers.EventDelete})
				}
				f(events, false)
			}()
		}
	})
	return djhr
}

// nolint: unused // (not true, its to implement an interface)
func (j *dynamicjoin[T]) augment(a any) any {
	// not supported in this collection type
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (j *dynamicjoin[T]) name() string { return j.collectionName }

// nolint: unused // (not true, its to implement an interface)
func (j *dynamicjoin[T]) uid() collectionUID { return j.id }

// nolint: unused // (not true, its to implement an interface)
func (j *dynamicjoin[I]) dump() CollectionDump {
	// TODO: We should actually implement this
	return CollectionDump{}
}

type indexEventType int

const (
	indexEventCollectionAddOrUpdate indexEventType = iota
	indexEventCollectionDelete
)

type collectionChangeEvent[T any] struct {
	eventType       indexEventType
	collectionName  string
	collectionValue internalCollection[T]
}

// nolint: unused // (not true)
type dynamicJoinIndexer struct {
	indexers map[string]kclient.RawIndexer
	sync.RWMutex
}

// nolint: unused // (not true)
func (j *dynamicJoinIndexer) Lookup(key string) []any {
	var res []any
	first := true
	j.RLock()
	defer j.RUnlock() // keithmattix: we're probably fine to defer as long as we don't have nested dynamic indexers
	for _, i := range j.indexers {
		l := i.Lookup(key)
		if len(l) > 0 && first {
			// TODO: add option to merge slices
			// This is probably not going to be performant if we need
			// to do lots of merges. Benchmark and optimize later.
			// Optimization: re-use the first returned slice
			res = l
			first = false
		} else {
			res = append(res, l...)
		}
	}
	return res
}

// nolint: unused // (not true, its to implement an interface)
func (j *dynamicjoin[T]) index(extract func(o T) []string) kclient.RawIndexer {
	j.Lock() // Take a write lock since we're also updating the collection change handlers
	defer j.Unlock()
	ji := &dynamicJoinIndexer{indexers: make(map[string]kclient.RawIndexer, len(j.collectionsByKey))}
	for _, c := range maps.SeqStable(j.collectionsByKey) {
		ji.indexers[c.name()] = c.index(extract)
	}
	j.registerCollectionChangeHandlerLocked(func(e collectionChangeEvent[T]) {
		ji.Lock()
		defer ji.Unlock()
		switch e.eventType {
		case indexEventCollectionAddOrUpdate:
			ji.indexers[e.collectionName] = e.collectionValue.index(extract)
		case indexEventCollectionDelete:
			delete(ji.indexers, e.collectionName)
		}
	})
	return ji
}

func (j *dynamicjoin[T]) Synced() Syncer {
	// We use a poll syncer here because the member collections of dynamicjoin can
	// change; it's not a one-and-done sync check, so channels are inappropriate.
	return pollSyncer{
		name: j.collectionName,
		f:    j.synced.Load,
	}
}

func (j *dynamicjoin[T]) HasSynced() bool {
	return j.syncer.HasSynced()
}

func (j *dynamicjoin[T]) WaitUntilSynced(stop <-chan struct{}) bool {
	return j.syncer.WaitUntilSynced(stop)
}

// The caller MUST have the lock when executing this function
func (j *dynamicjoin[T]) registerCollectionChangeHandlerLocked(h func(e collectionChangeEvent[T])) {
	if j.collectionChangeHandlers == nil {
		j.collectionChangeHandlers = make([]func(collectionChangeEvent[T]), 0)
	}
	j.collectionChangeHandlers = append(j.collectionChangeHandlers, h)
}

// AddOrUpdateCollection adds a collection to the join. When this function returns
// the passed collection will start influencing event handling
func (j *dynamicjoin[T]) AddOrUpdateCollection(c Collection[T]) {
	j.Lock()
	ic := c.(internalCollection[T])
	j.collectionsByKey[ic.name()] = ic

	// Add the new collection to the other handler logic
	for _, h := range j.collectionChangeHandlers {
		h(collectionChangeEvent[T]{
			eventType:       indexEventCollectionAddOrUpdate,
			collectionName:  ic.name(),
			collectionValue: ic,
		})
	}
	j.Unlock()

	// Change the sync status of our join collection if the new
	// collection isn't synced
	jSynced := j.synced.Load()
	if jSynced && !ic.HasSynced() {
		j.synced.Store(false)
		// Wait for the new collection to finish syncing
		go func() {
			stop := make(chan struct{}) // we're in a separate goroutine so the stop channel isn't the most relevant
			if !ic.WaitUntilSynced(stop) {
				return
			}
			j.synced.Store(true)
			log.Infof("%v re-synced", j.name())
		}()
	}
}

// RemoveCollection removes a collection from the join. When this function returns
// the passed collection will no longer influence any event handling
func (j *dynamicjoin[T]) RemoveCollection(c Collection[T]) {
	name := c.(internalCollection[T]).name()
	j.Lock()

	oldCollection, ok := j.collectionsByKey[name]
	if !ok {
		log.Warnf("Collection %v not found in %v", name, j.name())
		return
	}
	delete(j.collectionsByKey, name)
	handlers := j.collectionChangeHandlers
	j.Unlock() // don't defer so we don't have to wait for handler execution
	for _, h := range handlers {
		h(collectionChangeEvent[T]{
			eventType:       indexEventCollectionDelete,
			collectionName:  name,
			collectionValue: oldCollection,
		})
	}
}

// JoinCollection combines multiple Collection[T] into a single
// Collection[T] merging equal objects into one record
// in the resulting Collection
func DynamicJoinCollection[T any](cs []Collection[T], opts ...CollectionOption) DynamicCollection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("DynamicJoin[%v]", ptr.TypeName[T]())
	}
	synced := atomic.Bool{}
	synced.Store(false)
	c := make(map[string]internalCollection[T], len(cs))
	for _, collection := range cs {
		ic := collection.(internalCollection[T])
		c[ic.name()] = ic
	}
	go func() {
		for _, c := range c {
			if !c.WaitUntilSynced(o.stop) {
				return
			}
		}
		synced.Store(true)
		log.Infof("%v synced", o.name)
	}()
	// Create a shallow copy of the collection map before returning it
	// to avoid data races or locking in the waituntilsync goroutine
	collections := maps.Clone(c)
	return &dynamicjoin[T]{
		collectionName:   o.name,
		id:               nextUID(),
		collectionsByKey: collections,
		synced:           &synced,
		uncheckedOverlap: o.joinUnchecked,
		syncer: pollSyncer{
			name: o.name,
			f:    synced.Load,
		},
	}
}
