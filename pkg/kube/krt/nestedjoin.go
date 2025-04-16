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
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

type nestedjoin[T any] struct {
	collectionName           string
	id                       collectionUID
	collections              internalCollection[Collection[T]]
	synced                   <-chan struct{}
	syncer                   Syncer
	collectionChangeHandlers []func(collectionChangeEvent[T])
	merge                    func(ts []T) *T
	metadata                 Metadata
	sync.RWMutex

	seenFirstAddForKey map[string]struct{}
	// Use a separate mutex so we can have memory-safe operations regardless of
	// whether the collection change handler list is being read/written to or not
	seenFirstAddForKeyMu sync.Mutex
	// Keep a stop channel so we can check if nested collections are synced
	stop <-chan struct{}
}

func (j *nestedjoin[T]) GetKey(k string) *T {
	var found []T
	for _, c := range j.collections.List() {
		if r := c.GetKey(k); r != nil {
			if j.merge == nil {
				return r
			}
			found = append(found, *r)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return j.merge(found)
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
	res := map[Key[T]][]T{}
	for _, c := range j.collections.List() {
		for _, i := range c.List() {
			key := getTypedKey(i)
			res[key] = append(res[key], i)
		}
	}

	var l []T
	for _, ts := range res {
		m := j.merge(ts)
		if m != nil {
			l = append(l, *m)
		}
	}

	return l
}

func (j *nestedjoin[T]) List() []T {
	if j.merge != nil {
		j.mergeList()
	}

	return j.quickList()
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) index(name string, extract func(o T) []string) kclient.RawIndexer {
	ji := &dynamicJoinIndexer{indexers: make(map[collectionUID]kclient.RawIndexer)}
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

func (j *nestedjoin[T]) Metadata() Metadata {
	return j.metadata
}

// handleCollectionChangeEventLocked is run every time there is a modification to
// the set of collections we have (e.g. a collection is added, deleted, or updated).
// It is run while holding the lock on j, so we can safely mutate state within the
// body of the function
func (j *nestedjoin[T]) handleCollectionChangeEventLocked(djhr *dynamicJoinHandlerRegistration, e collectionChangeEvent[T], handler func(o []Event[T])) {
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
		}), true)
		djhr.removes[e.collectionValue.uid()] = reg.UnregisterHandler
		djhr.syncers[e.collectionValue.uid()] = reg
	case collectionMembershipEventDelete:
		// Unregister the handler for this collection
		remover := djhr.removes[e.collectionValue.uid()]
		syncer := djhr.syncers[e.collectionValue.uid()]
		if remover == nil {
			log.Warnf("Collection %v not found in %v", e.collectionValue.uid(), j.name())
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
		items := make(map[Key[T]][]T, len(oldItems))
		// First loop through the collection to get the items by their keys
		for _, c := range oldItems {
			key := getTypedKey(c)
			items[key] = append(items[key], c)
		}
		// Now loop through the keys and compare them to our current list of collections
		// to see if it's actually deleted
		for key, ts := range maps.SeqStable(items) {
			res := j.GetKey(string(key))
			m := j.merge(ts)
			// If the result is nil, then it was deleted
			if res == nil {
				// Send a delete event for the merged version of this key
				events = append(events, Event[T]{Old: m, Event: controllers.EventDelete})
				// Remove the key from the seenFirstAddForKey map
				j.seenFirstAddForKeyMu.Lock()
				delete(j.seenFirstAddForKey, string(key))
				j.seenFirstAddForKeyMu.Unlock()
				continue
			}
			// There are some versions of this key still in the overall collection
			// send an update with the new merged version
			events = append(events, Event[T]{Old: m, New: res, Event: controllers.EventUpdate})
		}
		handler(events)
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
				merged := j.GetKey(string(key))
				if merged == nil {
					// This shouldn't happen, log it
					log.Warnf("NestedJoinCollection: Merged item %v is nil after a collection update. Falling back to collection specific version", key)
					merged = &i
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

	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		reg := c.RegisterBatch(j.handleInnerCollectionEvent(f), runExistingState)
		removes[ic.uid()] = reg.UnregisterHandler
		syncers[ic.uid()] = reg
	}
	djhr := &dynamicJoinHandlerRegistration{
		syncers: syncers,
		removes: removes,
	}

	// We register to get notified if a collection within our set of collections is modified
	j.registerCollectionChangeHandler(func(e collectionChangeEvent[T]) {
		j.handleCollectionChangeEventLocked(djhr, e, f)
	})

	return djhr
}

func (j *nestedjoin[T]) handleInnerCollectionEvent(handler func(o []Event[T])) func(o []Event[T]) {
	return func(events []Event[T]) {
		// Lock the map during this entire handler for readability and to ensure events remain in-order
		// across collections)
		j.seenFirstAddForKeyMu.Lock()
		defer j.seenFirstAddForKeyMu.Unlock()
		mergedEvents := make([]Event[T], 0, len(events))
		for _, i := range events {
			key := GetKey(i.Latest())
			merged := j.GetKey(key)
			switch i.Event {
			case controllers.EventDelete:
				mergedEvents = append(mergedEvents, getMergedDelete(i, merged))
				if merged == nil {
					// Remove the key from the seenFirstAddForKey map. It's unlikely that
					// we would have two adds in different sub-collections at the exact same time
					// but handle it just in case
					delete(j.seenFirstAddForKey, key)
				}
			case controllers.EventAdd:
				// If we haven't seen an add for this key before, this should be a real add.
				// This is to prevent the case where the collection source starts its initial sync
				// with duplicate keys in different collections. Without this check, both events would
				// look like updates becaues GetKey() would return the merged version that differs
				// from the original event object.
				if _, ok := j.seenFirstAddForKey[key]; !ok {
					j.seenFirstAddForKey[key] = struct{}{}
					mergedEvents = append(mergedEvents, Event[T]{
						Event: controllers.EventAdd,
						Old:   nil,
						New:   merged,
					})
					continue
				}
				mergedEvents = append(mergedEvents, getMergedAdd(i, merged))
			case controllers.EventUpdate:
				mergedEvents = append(mergedEvents, getMergedUpdate(i, merged))
			}
		}
		handler(mergedEvents)
	}
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
			if remover == nil {
				log.Warnf("Collection %v not found in %v", e.collectionValue.uid(), j.name())
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
func (j *nestedjoin[I]) dump() CollectionDump {
	// TODO: We should actually implement this
	return CollectionDump{}
}

// The passed in handler is executed while holding the lock, so
// it MUST NOT take the lock itself.
func (j *nestedjoin[T]) registerCollectionChangeHandler(h func(e collectionChangeEvent[T])) {
	j.Lock()
	j.collectionChangeHandlers = append(j.collectionChangeHandlers, h)
	j.Unlock()
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
		merge:              merge,
		seenFirstAddForKey: make(map[string]struct{}),
		stop:               o.stop,
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	reg := collections.RegisterBatch(func(o []Event[Collection[T]]) {
		j.RLock()
		defer j.RUnlock()

		// Each event goes to all handlers first to preserve ordering
		for _, e := range o {
			for _, h := range j.collectionChangeHandlers {
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
