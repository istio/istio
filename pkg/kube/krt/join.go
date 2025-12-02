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
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type join[T any] struct {
	collectionName   string
	id               collectionUID
	collections      []internalCollection[T]
	synced           <-chan struct{}
	uncheckedOverlap bool
	syncer           Syncer
	metadata         Metadata

	// mu protects both eventHandlers and processedState
	// but mu and processedState are ignored when uncheckedOverlap is true
	mu sync.RWMutex
	// processedState tracks objects we've processed via handleSubCollectionEvents
	// This ensures RegisterBatch only sends events for objects that have been fully processed,
	// avoiding duplicates from in-flight sub-collection events
	processedState map[string]*T
	eventHandlers  *handlerSet[T]

	stop <-chan struct{}
}

func (j *join[T]) GetKey(k string) *T {
	for _, c := range j.collections {
		if r := c.GetKey(k); r != nil {
			return r
		}
	}
	return nil
}

func (j *join[T]) List() []T {
	var res []T
	if j.uncheckedOverlap {
		first := true
		for _, c := range j.collections {
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
	for _, c := range j.collections {
		objs := c.List()
		// As an optimization, take the first (non-empty) result as-is without copying
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

func (j *join[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched[T](j, f)
}

func (j *join[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	// Fast path for unchecked overlap: use List() directly without locking or tracking processedState
	if j.uncheckedOverlap {
		var initialEvents []Event[T]
		if runExistingState {
			for _, obj := range j.List() {
				objCopy := obj
				initialEvents = append(initialEvents, Event[T]{
					New:   &objCopy,
					Event: controllers.EventAdd,
				})
			}
		}
		reg := j.eventHandlers.Insert(f, j, initialEvents, j.stop)
		return reg
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// If we need to run existing state, gather it from processedState instead of List()
	// This prevents race where we send events for objects that haven't been processed
	// by handleSubCollectionEvents yet (they're in sub-collection state but event is in-flight)
	var initialEvents []Event[T]
	if runExistingState {
		for _, obj := range j.processedState {
			initialEvents = append(initialEvents, Event[T]{
				New:   obj,
				Event: controllers.EventAdd,
			})
		}
	}

	// Register the handler and send initial events if needed
	// eventHandlers has its own lock, sub-collection handlers are already registered in the constructor
	reg := j.eventHandlers.Insert(f, j, initialEvents, j.stop)
	return reg
}

// handleSubCollectionEvents processes events from a sub-collection, refreshing them
// based on the current state of all collections, then distributes to registered handlers.
func (j *join[T]) handleSubCollectionEvents(events []Event[T], sourceCollectionIdx int) {
	// Fast path for unchecked overlap: no conflict resolution, no state tracking, no locking
	if j.uncheckedOverlap {
		j.eventHandlers.Distribute(events, !j.HasSynced())
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	refreshedEvents := j.refreshEvents(events, sourceCollectionIdx)

	if len(refreshedEvents) == 0 {
		return
	}

	// Update processedState to track what we've processed
	// This is used by RegisterBatch to provide consistent initial state
	for _, ev := range refreshedEvents {
		key := GetKey(ev.Latest())
		if ev.Event == controllers.EventDelete {
			delete(j.processedState, key)
		} else {
			// For Add and Update, store the new object pointer (no copy)
			j.processedState[key] = ev.New
		}
	}

	j.eventHandlers.Distribute(refreshedEvents, !j.HasSynced())
}

func (j *join[T]) getFromColIdx(idx int, key string) *T {
	if idx < 0 || idx >= len(j.collections) {
		if EnableAssertions {
			panic("join: getFromColIdx: index out of range:" + fmt.Sprint(idx) + " len: " + fmt.Sprint(len(j.collections)))
		}
		return nil
	}
	obj := j.collections[idx].GetKey(key)
	if obj == nil {
		return nil
	}

	// HACK: StaticCollectoin (which we use in test) does not care what key you pass to Get
	if GetKey(*obj) != key {
		if EnableAssertions {
			panic("join: getFromColIdx: collection returned object with wrong key. Wanted: " + key + " got: " + GetKey(*obj))
		}
		return nil
	}

	return obj
}

// refreshEvents refreshes events by checking the current state of all collections
// to determine which collection is authoritative for each key. This implements
// conflict resolution without storing objects.
func (j *join[T]) refreshEvents(events []Event[T], sourceCollectionIdx int) []Event[T] {
	var result []Event[T]

	for _, ev := range events {
		key := GetKey(ev.Latest())

		// Check if any higher-priority collection (0...sourceCollectionIdx-1) has this key
		hasHigherPriority := false

		for i := range sourceCollectionIdx {
			if o := j.getFromColIdx(i, key); o != nil {
				hasHigherPriority = true
				break
			}
		}

		if ev.Event == controllers.EventDelete {
			if hasHigherPriority {
				// Drop delete event - we weren't using this collection's version anyway
				continue
			}

			// Collection sourceCollectionIdx was authoritative. Check for fallback in lower-priority collections.
			var fallbackObj *T
			for i := sourceCollectionIdx + 1; i < len(j.collections); i++ {
				if obj := j.getFromColIdx(i, key); obj != nil {
					fallbackObj = obj
					break
				}
			}

			if fallbackObj != nil {
				// Convert DELETE to UPDATE - fallback to lower-priority collection's version
				result = append(result, Event[T]{
					Event: controllers.EventUpdate,
					Old:   ev.Old,
					New:   fallbackObj, // pointer from collection, not a copy!
				})
			} else {
				// No fallback found, send DELETE
				result = append(result, ev)
			}
		} else {
			// ADD or UPDATE event
			if hasHigherPriority {
				// Drop event - higher priority collection owns this key
				continue
			}

			// No higher-priority collection has it, so this collection is now authoritative
			// Check if a lower-priority collection had this key before
			var oldObj *T
			for i := sourceCollectionIdx + 1; i < len(j.collections); i++ {
				if obj := j.getFromColIdx(i, key); obj != nil {
					oldObj = obj
					break
				}
			}

			if oldObj != nil && ev.Event == controllers.EventAdd {
				// Convert ADD to UPDATE - we're replacing a lower-priority collection's version
				result = append(result, Event[T]{
					Event: controllers.EventUpdate,
					Old:   oldObj,
					New:   ev.New,
				})
			} else {
				// Forward the event as-is
				result = append(result, ev)
			}
		}
	}

	return result
}

// nolint: unused // (not true, its to implement an interface)
func (j *join[T]) augment(a any) any {
	// not supported in this collection type
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (j *join[T]) name() string { return j.collectionName }

// nolint: unused // (not true, its to implement an interface)
func (j *join[T]) uid() collectionUID { return j.id }

// nolint: unused // (not true, its to implement an interface)
func (j *join[I]) dump() CollectionDump {
	// Dump should not be used on join; instead its preferred to enroll each individual collection. Maybe reconsider
	// in the future if there is a need
	return CollectionDump{}
}

// nolint: unused // (not true)
type joinIndexer[T any] struct {
	indexers []indexer[T]
}

// nolint: unused // (not true)
func (j joinIndexer[T]) Lookup(key string) []T {
	var res []T
	first := true
	for _, i := range j.indexers {
		l := i.Lookup(key)
		if len(l) > 0 && first {
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
func (j *join[T]) index(name string, extract func(o T) []string) indexer[T] {
	ji := joinIndexer[T]{indexers: make([]indexer[T], 0, len(j.collections))}
	for _, c := range j.collections {
		ji.indexers = append(ji.indexers, c.index(name, extract))
	}
	return ji
}

func (j *join[T]) Synced() Syncer {
	return channelSyncer{
		name:   j.collectionName,
		synced: j.synced,
	}
}

func (j *join[T]) HasSynced() bool {
	return j.syncer.HasSynced()
}

func (j *join[T]) WaitUntilSynced(stop <-chan struct{}) bool {
	return j.syncer.WaitUntilSynced(stop)
}

func (j *join[T]) Metadata() Metadata {
	return j.metadata
}

// JoinCollection combines multiple Collection[T] into a single
// Collection[T]. Key conflicts are resolved by picking the item
// produced by the first collections in the list of input collections.
func JoinCollection[T any](cs []Collection[T], opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Join[%v]", ptr.TypeName[T]())
	}
	synced := make(chan struct{})
	c := slices.Map(cs, func(e Collection[T]) internalCollection[T] {
		return e.(internalCollection[T])
	})
	go func() {
		for _, c := range c {
			if !c.WaitUntilSynced(o.stop) {
				return
			}
		}
		close(synced)
		log.Infof("%v synced", o.name)
	}()
	if o.stop == nil {
		panic("no stop channel")
	}
	j := &join[T]{
		collectionName:   o.name,
		id:               nextUID(),
		synced:           synced,
		collections:      c,
		uncheckedOverlap: o.joinUnchecked,
		eventHandlers:    newHandlerSet[T](),
		processedState:   make(map[string]*T),
		stop:             o.stop,
		syncer: channelSyncer{
			name:   o.name,
			synced: synced,
		},
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	// Register handlers on sub-collections ONCE during construction
	// These handlers will process events from sub-collections and distribute to registered handlers
	var subHandlerRegs []HandlerRegistration
	for idx, subCol := range c {
		collectionIdx := idx // capture for closure
		handler := func(events []Event[T]) {
			j.handleSubCollectionEvents(events, collectionIdx)
		}
		// Don't run existing state - we handle that in RegisterBatch
		reg := subCol.RegisterBatch(handler, false)
		subHandlerRegs = append(subHandlerRegs, reg)
	}

	// Clean up sub-collection handlers when this collection's stop is closed
	go func() {
		<-o.stop
		for _, reg := range subHandlerRegs {
			reg.UnregisterHandler()
		}
	}()

	return j
}
