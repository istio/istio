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
	"strconv"
	"sync"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// Note: there's a goroutine per event handler, so we can't put state relevant
// to any keys in the collection itself (otherwise, different handlers will
// read/mutate the same state and see a different view of the world).
type nestedjoin[T any] struct {
	collectionName           string
	id                       collectionUID
	collections              internalCollection[Collection[T]]
	synced                   <-chan struct{}
	syncer                   Syncer
	collectionChangeHandlers *collectionChangeHandlers[T]
	merge                    func(ts []T) *T
	metadata                 Metadata

	// mergedCache is a cache of the merged results for each key
	// This is used to ensure we have accurate eventing when dealing
	// with merged collections (e.g. that event.Old is set correctly).
	// This will prevent unnecessary xDS pushes; without it, we'd
	// Old != New (merged) would always be true and we'd push.
	mergedCache  map[mergedCacheKey]mergedCacheEntry[T]
	sync.RWMutex // protects mergedCache

	// Keep a stop channel so we can check if nested collections are synced
	stop <-chan struct{}
}

type collectionChangeHandlers[T any] struct {
	handlers []func(collectionChangeEvent[T])
	sync.RWMutex
}

func (j *nestedjoin[T]) quickGetKey(k string) *T {
	for _, c := range j.collections.List() {
		if r := c.GetKey(k); r != nil {
			return r
		}
	}

	return nil
}

func (j *nestedjoin[T]) GetKey(k string) *T {
	if j.merge == nil {
		return j.quickGetKey(k)
	}

	j.RLock()
	defer j.RUnlock()
	// Check the cache first
	if entry, ok := j.mergedCache[mergedCacheKey{key: k, handlerID: ""}]; ok {
		if entry.current != nil {
			return entry.current
		}
		log.Warnf("Merged key %s in collection %s is nil in the cache during a get operation", k, j.collectionName)
	}
	return nil
}

func (j *nestedjoin[T]) quickList() []T {
	var res []T
	var found sets.String
	first := true

	// We need stable ordering so we'll loop through the outer collections first
	// saving state as we go
	collectionsByUID := make(map[collectionUID]Collection[T])
	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		collectionsByUID[ic.uid()] = ic
	}

	// Now loop through the collections in UID order
	for _, c := range maps.SeqStable(collectionsByUID) {
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

func (j *nestedjoin[T]) mergeList() []T {
	j.RLock()
	defer j.RUnlock()

	// TODO: Should we fall back to manually computing the merge and saving it in the cache?
	// My gut says no; we want one source of truth
	var l []T
	for key, item := range j.mergedCache {
		if key.handlerID != "" {
			continue
		}
		if item.current != nil {
			l = append(l, *item.current)
		} else {
			log.Warnf("Merged key %s in collection %s is nil in the cache during a list operation", key, j.collectionName)
		}
	}

	return l
}

func (j *nestedjoin[T]) List() []T {
	if j.merge == nil {
		return j.quickList()
	}

	return j.mergeList()
}

func (j *nestedjoin[T]) Metadata() Metadata {
	return j.metadata
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) index(name string, extract func(o T) []string) indexer[T] {
	ji := &dynamicJoinIndexer[T]{indexers: make(map[collectionUID]indexer[T])}
	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		ji.indexers[ic.uid()] = ic.index(name, extract)
	}
	j.registerCollectionChangeHandler(func(e collectionChangeEvent[T]) {
		ji.Lock()
		defer ji.Unlock()
		switch e.eventType {
		case collectionMembershipEventAdd:
			ji.indexers[e.collectionValue.uid()] = e.collectionValue.index(name, extract)
		case collectionMembershipEventDelete:
			delete(ji.indexers, e.collectionValue.uid())
		}
	})
	return ji
}

func (j *nestedjoin[T]) Synced() Syncer {
	return channelSyncer{
		name:   j.collectionName,
		synced: j.synced,
	}
}

func (j *nestedjoin[T]) HasSynced() bool {
	return j.syncer.HasSynced()
}

func (j *nestedjoin[T]) WaitUntilSynced(stop <-chan struct{}) bool {
	return j.syncer.WaitUntilSynced(stop)
}

func (j *nestedjoin[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched(j, f)
}

// handleCollectionChangeEventLocked is run every time there is a modification to
// the set of collections we have (e.g. a collection is added, deleted, or updated).
// It is run while holding the lock on j, so we can safely mutate state within the
// body of the function
func (j *nestedjoin[T]) handleCollectionChangeEventLocked(
	djhr *dynamicJoinHandlerRegistration,
	e collectionChangeEvent[T],
	handler func(o []Event[T]),
	handlerID string,
) {
	djhr.Lock()
	defer djhr.Unlock()
	// This entire function is executed while holding the lock on j, so we can freely
	// mutate state within the main function body
	switch e.eventType {
	case collectionMembershipEventAdd:
		// We always want to send events for existing state when a new collection is added
		reg := e.collectionValue.RegisterBatch(j.handleInnerCollectionEvent(func(o []Event[T]) {
			djhr.RLock()
			defer djhr.RUnlock()
			if _, ok := djhr.removes[e.collectionValue.uid()]; !ok {
				// The remover has been called; don't handle this event
				return
			}
			handler(o)
		}, handlerID), true) // Always run existing state
		djhr.removes[e.collectionValue.uid()] = reg.UnregisterHandler
		djhr.syncers[e.collectionValue.uid()] = reg
	case collectionMembershipEventDelete:
		// Unregister the handler for this collection
		remover := djhr.removes[e.collectionValue.uid()]
		syncer := djhr.syncers[e.collectionValue.uid()]
		if remover == nil {
			return
		}
		if syncer == nil {
			return
		}
		// krt schedules a callback to notify the event handler machinery that a collection is synced
		// if a handler is registered before then. If the collection is removed before the callback
		// is called, we can get a panic for trying to send on a closed channel, so wait for the collection
		// to be synced before unregistering the handler.
		if !syncer.WaitUntilSynced(j.stop) {
			log.Warnf("Collection %v (uid %v) was not synced before unregistering", e.collectionValue.name(), e.collectionValue.uid())
			return
		}
		remover()
		delete(djhr.removes, e.collectionValue.uid())
		delete(djhr.syncers, e.collectionValue.uid())

		// Now we must send a final set of remove events for each object in the collection
		var events []Event[T]
		// We're merging so this is a bit more complicated
		oldItems := e.collectionValue.List()
		// Short circuit; send delete events for all items in the collection
		if j.merge == nil {
			events := slices.Map(oldItems, func(i T) Event[T] {
				return Event[T]{Old: &i, Event: controllers.EventDelete}
			})
			handler(events)
			return
		}
		items := make(map[Key[T]][]T, len(oldItems))
		// First loop through the collection to get the deleted items by their keys
		for _, c := range oldItems {
			key := getTypedKey(c)
			items[key] = append(items[key], c)
		}
		// Now loop through the keys and compare them to our current list of collections
		// to see if it's actually deleted
		for key, ts := range maps.SeqStable(items) {
			keyString := string(key)
			res := j.calculateMerged(keyString)
			// Always update the cache on a collection delete
			// Use the pointer value of the handler as the handlerID
			entry := j.updateMergedCache(keyString, handlerID, res)
			m := j.merge(ts)
			// If the result is nil, then it was deleted
			if res == nil {
				// Send a delete event for the merged version of this key
				events = append(events, Event[T]{Old: m, Event: controllers.EventDelete})
				continue
			}
			// There are some versions of this key still in the overall collection
			// send an update with the new merged version and the old version from
			// the cache
			events = append(events, Event[T]{Old: entry.prev, New: res, Event: controllers.EventUpdate})
		}
		handler(events)
	case collectionMembershipEventUpdate:
		// Get all of the elements in the old collection
		oldItems := e.oldCollectionValue.List()
		// Convert it to a map for easy lookup
		oldItemsMap := make(map[Key[T]]T, len(oldItems))
		if j.merge == nil {
			// Short-circuit; send update events for all items in the collection
			events := slices.Map(oldItems, func(i T) Event[T] {
				return Event[T]{Old: &i, Event: controllers.EventUpdate, New: j.GetKey(GetKey(i))}
			})
			handler(events)
			return
		}
		for _, i := range oldItems {
			key := getTypedKey(i)
			oldItemsMap[key] = i
		}
		// Now loop through the new collection and compare it to the old one
		seen := sets.NewWithLength[string](len(oldItems))
		finalEvents := make([]Event[T], 0, len(oldItems))
		for _, i := range e.collectionValue.List() {
			key := getTypedKey(i)
			// If we see it in the old collection, then it's an update
			if oldItem, ok := oldItemsMap[key]; ok {
				seen.Insert(string(key))
				// Don't need to pass i since the collection still exists and has been updated
				merged := j.calculateMerged(string(key))
				if merged == nil {
					// This shouldn't happen, log it
					log.Warnf("NestedJoinCollection: Merged item %v is nil after a collection update. Falling back to collection specific version", key)
					merged = &i
				} else {
					// Update the cache with the new merged version
					entry := j.updateMergedCache(string(key), handlerID, merged)
					oldItem = ptr.OrEmpty(entry.prev)
				}
				// Send an update event for the merged version of this key
				finalEvents = append(finalEvents, Event[T]{Old: &oldItem, New: merged, Event: controllers.EventUpdate})
				// Delete it from the old items map
				delete(oldItemsMap, key)
			} else {
				if seen.Contains(string(key)) {
					// This is a duplicate item in the new collection, skip it
					log.Warnf("NestedJoinCollection: Duplicate item %v in updated collection, skipping", key)
					continue
				}
				// This is a new item
				finalEvents = append(finalEvents, Event[T]{New: &i, Event: controllers.EventAdd})
			}
		}
		// Now loop through the old items map and send delete events for any items that
		// are no longer in the new collection
		for _, i := range maps.SeqStable(oldItemsMap) {
			finalEvents = append(finalEvents, Event[T]{Old: &i, Event: controllers.EventDelete})
		}

		handler(finalEvents)
	}
}

func (j *nestedjoin[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	if j.merge == nil {
		return j.registerBatchUnmerged(f, runExistingState)
	}
	syncers := make(map[collectionUID]Syncer)
	removes := map[collectionUID]func(){}
	// Create a unique handler ID for this context
	handlerID := strconv.FormatUint(globalUIDCounter.Inc(), 10)

	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		reg := c.RegisterBatch(j.handleInnerCollectionEvent(f, handlerID), runExistingState)
		removes[ic.uid()] = reg.UnregisterHandler
		syncers[ic.uid()] = reg
	}
	djhr := &dynamicJoinHandlerRegistration{
		syncers: syncers,
		removes: removes,
	}

	// We register to get notified if a collection within our set of collections is modified
	j.registerCollectionChangeHandler(func(e collectionChangeEvent[T]) {
		j.handleCollectionChangeEventLocked(djhr, e, f, handlerID)
	})

	return djhr
}

func (j *nestedjoin[T]) calculateMerged(k string) *T {
	var found []T
	for _, c := range j.collections.List() {
		if r := c.GetKey(k); r != nil {
			found = append(found, *r)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return j.merge(found)
}

func (j *nestedjoin[T]) updateMergedCache(key, handlerID string, merged *T) mergedCacheEntry[T] {
	j.Lock()
	defer j.Unlock()
	return updateMergeCacheLocked(j.mergedCache, merged, key, handlerID)
}

func (j *nestedjoin[T]) handleInnerCollectionEvent(handler func(o []Event[T]), handlerID string) func(o []Event[T]) {
	return handleInnerCollectionEvent(handler, handlerID, j.calculateMerged, j.updateMergedCache)
}

func (j *nestedjoin[T]) registerBatchUnmerged(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	syncers := make(map[collectionUID]Syncer)
	removes := map[collectionUID]func(){}
	for _, c := range j.collections.List() {
		reg := c.RegisterBatch(f, runExistingState)
		ic := c.(internalCollection[T])
		removes[ic.uid()] = reg.UnregisterHandler
		syncers[ic.uid()] = reg
	}
	djhr := &dynamicJoinHandlerRegistration{
		syncers: syncers,
		removes: removes,
	}

	j.registerCollectionChangeHandler(func(e collectionChangeEvent[T]) {
		djhr.Lock()
		defer djhr.Unlock()
		switch e.eventType {
		case collectionMembershipEventAdd:
			reg := e.collectionValue.RegisterBatch(f, runExistingState)
			djhr.removes[e.collectionValue.uid()] = reg.UnregisterHandler
			djhr.syncers[e.collectionValue.uid()] = reg
		case collectionMembershipEventDelete:
			// Unregister the handler for this collection
			remover := djhr.removes[e.collectionValue.uid()]
			syncer := djhr.syncers[e.collectionValue.uid()]
			if remover == nil {
				return
			}
			if syncer == nil {
				return
			}
			// krt schedules a callback to notify the event handler machinery that a collection is synced
			// if a handler is registered before then. If the collection is removed before the callback
			// is called, we can get a panic for trying to send on a closed channel, so wait for the collection
			// to be synced before unregistering the handler.
			if !syncer.WaitUntilSynced(j.stop) {
				log.Warnf("Collection %v (uid %v) was not synced before unregistering", e.collectionValue.name(), e.collectionValue.uid())
				return
			}
			remover()
			delete(djhr.removes, e.collectionValue.uid())
			delete(djhr.syncers, e.collectionValue.uid())

			// Now send a final set of remove events for each object in the collection
			var events []Event[T]
			for _, elem := range e.collectionValue.List() {
				events = append(events, Event[T]{Old: &elem, Event: controllers.EventDelete})
			}
			f(events)
			return
		case collectionMembershipEventUpdate:
			// Get all of the elements in the old collection
			oldItems := e.oldCollectionValue.List()
			// Convert it to a sparse map for easy lookup
			oldItemsMap := make(map[Key[T]]T, len(oldItems))
			for _, i := range oldItems {
				key := getTypedKey(i)
				oldItemsMap[key] = i
			}
			// Now loop through the new collection and compare it to the old one
			seen := sets.NewWithLength[string](len(oldItems))
			finalEvents := make([]Event[T], 0, len(oldItems))
			for _, i := range e.collectionValue.List() {
				key := getTypedKey(i)
				// If we see it in the old collection, then it's an update
				if oldItem, ok := oldItemsMap[key]; ok {
					seen.Insert(string(key))
					// Send an update event for the new version of this key
					finalEvents = append(finalEvents, Event[T]{Old: &oldItem, New: &i, Event: controllers.EventUpdate})
					// Delete it from the old items map
					delete(oldItemsMap, key)
				} else {
					if seen.Contains(string(key)) {
						// This is a duplicate item in the new collection, skip it
						log.Warnf("NestedJoinCollection: Duplicate item %v in updated collection, skipping", key)
						continue
					}
					// This is a new item
					finalEvents = append(finalEvents, Event[T]{New: &i, Event: controllers.EventAdd})
				}
			}
			// Now loop through the old items map and send delete events for any items that
			// are no longer in the new collection
			for _, i := range maps.SeqStable(oldItemsMap) {
				finalEvents = append(finalEvents, Event[T]{Old: &i, Event: controllers.EventDelete})
			}

			f(finalEvents)
		}
	})

	return djhr
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) augment(a any) any {
	// not supported in this collection type
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) name() string { return j.collectionName }

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) uid() collectionUID { return j.id }

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) dump() CollectionDump {
	innerCols := j.collections.List()
	dumpsByCollectionUID := make(map[string]InputDump, len(innerCols))
	for _, c := range innerCols {
		if c == nil {
			continue
		}
		ic := c.(internalCollection[T])
		icDump := ic.dump()
		dumpsByCollectionUID[GetKey(ic)] = InputDump{
			Outputs:      maps.Keys(icDump.Outputs),
			Dependencies: append(maps.Keys(icDump.Inputs), icDump.InputCollection),
		}
	}
	return CollectionDump{
		Outputs: eraseMap(slices.GroupUnique(j.List(), getTypedKey)),
		Synced:  j.HasSynced(),
		Inputs:  dumpsByCollectionUID,
	}
}

// The passed in handler is executed while holding the lock, so
// it MUST NOT take the lock itself.
func (j *nestedjoin[T]) registerCollectionChangeHandler(h func(e collectionChangeEvent[T])) {
	j.collectionChangeHandlers.Lock()
	j.collectionChangeHandlers.handlers = append(j.collectionChangeHandlers.handlers, h)
	j.collectionChangeHandlers.Unlock()
}

// NestedJoinCollection creates a new collection of collections of T. Duplicate keys across the collections will *not*
// be merged (see NestedJoinWithMergeCollection for that) and access operations (e.g. GetKey and List) will perform a
// best effort stable ordering of the list of elements returned; however, this ordering will not be persistent across
// istiod restarts.
func NestedJoinCollection[T any](collections Collection[Collection[T]], opts ...CollectionOption) Collection[T] {
	return NestedJoinWithMergeCollection(collections, nil, opts...)
}

// NestedJoinWithMergeCollection creates a new collection of collections of T that combines duplicates with the specified merge function.
// The merge function *cannot* assume a stable ordering of the list of elements passed to it. Therefore, access operations (e.g. GetKey and List) will
// will only be deterministic if the merge function is deterministic. The merge function should return nil if no value should be returned.
func NestedJoinWithMergeCollection[T any](collections Collection[Collection[T]], merge func(ts []T) *T, opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NestedJoin[%v]", ptr.TypeName[T]())
	}

	ics := collections.(internalCollection[Collection[T]])
	synced := make(chan struct{})

	j := &nestedjoin[T]{
		collectionName: o.name,
		id:             nextUID(),
		synced:         synced,
		collections:    ics,
		syncer: channelSyncer{
			name:   o.name,
			synced: synced,
		},
		collectionChangeHandlers: &collectionChangeHandlers[T]{
			handlers: make([]func(collectionChangeEvent[T]), 0),
		},
		mergedCache: make(map[mergedCacheKey]mergedCacheEntry[T]),
		merge:       merge,
		stop:        o.stop,
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	reg := collections.RegisterBatch(func(o []Event[Collection[T]]) {
		j.collectionChangeHandlers.RLock()
		defer j.collectionChangeHandlers.RUnlock()

		// Each event goes to all handlers first to preserve ordering
		for _, e := range o {
			for _, h := range j.collectionChangeHandlers.handlers {
				switch e.Event {
				case controllers.EventAdd:
					if e.New == nil {
						log.Warnf("Event %v has no new value", e.Event)
						continue
					}
					h(collectionChangeEvent[T]{eventType: collectionMembershipEventAdd, collectionValue: any(*e.New).(internalCollection[T])})
				// TODO: What does a collection/informer update even look like?
				// I'm assuming it's something like a change in informer filter
				// or something and the uid stays the same.
				case controllers.EventUpdate:
					if e.New == nil || e.Old == nil {
						log.Warnf("Event %v either has no New value or no Old value", e.Event)
						continue
					}
					h(collectionChangeEvent[T]{
						eventType:          collectionMembershipEventDelete,
						collectionValue:    any(*e.New).(internalCollection[T]),
						oldCollectionValue: any(*e.Old).(internalCollection[T]),
					})
				case controllers.EventDelete:
					if e.Old == nil {
						log.Warnf("Event %v has no old value", e.Event)
						continue
					}
					h(collectionChangeEvent[T]{eventType: collectionMembershipEventDelete, collectionValue: any(*e.Old).(internalCollection[T])})
				}
			}
		}
	}, false)

	go func() {
		// Need to make sure the outer collection is synced first
		if !collections.WaitUntilSynced(o.stop) || !reg.WaitUntilSynced(o.stop) {
			return
		}
		for _, c := range collections.List() {
			if !c.WaitUntilSynced(o.stop) {
				return
			}
		}
		close(synced)
		log.Infof("%v synced", o.name)
	}()

	return j
}
