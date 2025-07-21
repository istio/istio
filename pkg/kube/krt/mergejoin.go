// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package krt

import (
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
)

type mergejoin[T any] struct {
	// mergeCache is a cache of the merged results for each key
	// This is used to ensure we have accurate eventing when dealing
	// with merged collections (e.g. that event.Old is set correctly).
	// This will prevent unnecessary xDS pushes; without it, we'd
	// Old != New (merged) would always be true and we'd push.
	id             collectionUID
	collectionName string

	collections collectionLister[T]
	mergeCache  map[mergeCacheKey]mergeCacheEntry[T]
	// internal indexes
	indexes          map[string]joinCollectionIndex[T]
	uncheckedOverlap bool

	merge        func(ts []T) *T
	sync.RWMutex // protects mergeCache
	handlers     sets.Set[uint64]
}

// Registration handle is used to coordinate events within the same handler
// (i.e. caller of Register[Batch])
type registrationHandle struct {
	id uint64
	sync.Mutex
}

type mergeCollection[T any] interface {
	internalCollection[T]
	registerBatchForHandler(f func(o []Event[T]), runExistingState bool, handle *registrationHandle) HandlerRegistration
	getKeyForHandler(k string, handlerID uint64) *T
	listForHandler(handlerID uint64) []T
}

type mergeIndexer[T any] interface {
	indexer[T]
	lookupForHandler(key string, handlerID uint64) []T
}

type mergeCacheKey struct {
	handlerID uint64 // Same base type as collectionUID, but different semantics
	key       string
}

type mergeCacheEntry[T any] struct {
	prev    *T
	current *T // Must always be set if there's an entry in the map
}

func (j *mergejoin[T]) quickGetKey(k string) *T {
	for _, c := range j.collections.getCollections() {
		if r := c.GetKey(k); r != nil {
			return r
		}
	}
	return nil
}

func (j *mergejoin[T]) GetKey(k string) *T {
	if j.merge == nil {
		return j.quickGetKey(k)
	}
	return j.getKeyForHandler(k, uint64(j.id))
}

func (j *mergejoin[T]) getKeyForHandler(k string, handlerID uint64) *T {
	if j.merge == nil {
		return j.quickGetKey(k)
	}
	j.RLock()
	defer j.RUnlock()
	return j.getKeyForHandlerLocked(k, handlerID)
}

func (j *mergejoin[T]) getKeyForHandlerLocked(k string, handlerID uint64) *T {
	// Check the cache first
	if entry, ok := j.mergeCache[mergeCacheKey{key: k, handlerID: handlerID}]; ok {
		if entry.current != nil {
			return entry.current
		}
		log.Warnf("Merged key %s in collection %s is nil in the cache during a get operation", k, j.collectionName)
	}
	log.Infof("Could not find key %s in collection %s. Mergecache state is %s", k, j.collectionName, spew.Sprint(j.mergeCache))

	return nil
}

func (j *mergejoin[T]) quickList() []T {
	var res []T
	if j.uncheckedOverlap {
		first := true
		for _, c := range j.collections.getCollections() {
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
	for _, c := range j.collections.getCollections() {
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

func (j *mergejoin[T]) mergeList() []T {
	j.RLock()
	defer j.RUnlock()

	return j.mergeListLocked(uint64(j.id))
}

func (j *mergejoin[T]) mergeListLocked(handlerID uint64) []T {
	// TODO: Should we fall back to manually computing the merge and saving it in the cache?
	// My gut says no; we want one source of truth
	var l []T
	for key, item := range j.mergeCache {
		if key.handlerID != handlerID {
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

func (j *mergejoin[T]) listForHandler(handlerID uint64) []T {
	if j.merge == nil {
		return j.quickList()
	}
	j.RLock()
	defer j.RUnlock()
	return j.mergeListLocked(handlerID)

}

func (j *mergejoin[T]) List() []T {
	if j.merge != nil {
		j.mergeList()
	}

	return j.quickList()
}

func (j *mergejoin[T]) index(name string, extract func(o T) []string) mergeIndexer[T] {
	j.Lock()
	defer j.Unlock()

	idx := joinCollectionIndex[T]{
		extract: extract,
		index:   make(map[string]sets.Set[handlerLocalKey[T]]),
		parent:  j,
	}
	for _, v := range j.mergeListLocked(uint64(j.id)) {
		for _, handlerID := range j.handlers.UnsortedList() {
			k := getTypedKey(v)
			idx.update(Event[T]{
				Old:   nil,
				New:   &v,
				Event: controllers.EventAdd,
			}, handlerLocalKey[T]{
				Key:       k,
				handlerID: handlerID,
			})
		}
	}
	j.indexes[name] = idx
	return idx
}

func (j *mergejoin[T]) calculateMerged(k string) *T {
	var found []T
	for _, c := range j.collections.getCollections() {
		if r := c.GetKey(k); r != nil {
			found = append(found, *r)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return j.merge(found)
}

func (j *mergejoin[T]) maybeInitMergeCacheForHandlerLocked(handlerID uint64) {
	// TODO: Pre-init indexes as well under the lock so that either index() sees our handler
	// or we see the new index
	// First check to see if there's been any merge cache entry before us
	// by checking the empty handler mergeKey
	for key, entry := range j.mergeCache {
		// no-op
		if key.handlerID != uint64(j.id) || entry.current == nil {
			continue
		}

		// There are entries in the cache; copy them to our handlerID in the mergeCache
		j.mergeCache[mergeCacheKey{key: key.key, handlerID: handlerID}] = entry
		// For each index, copy the entries for this handlerID
		for _, index := range j.indexes {
			tk := Key[T](key.key)
			index.update(Event[T]{
				Old:   nil, // Old is only used to clear out previous values so not useful here
				New:   entry.current,
				Event: controllers.EventAdd,
			}, handlerLocalKey[T]{
				Key:       tk,
				handlerID: handlerID,
			})
		}
	}
}

func (j *mergejoin[T]) updateMergeCache(key string, handlerID uint64, merged *T) mergeCacheEntry[T] {
	j.Lock()
	defer j.Unlock()
	return j.updateMergeCacheLocked(key, handlerID, merged)
}

func (j *mergejoin[T]) updateMergeCacheLocked(key string, handlerID uint64, merged *T) mergeCacheEntry[T] {
	if merged == nil {
		// First get the existing value from the cache (if it exists)
		var old *T
		entry, ok := j.mergeCache[mergeCacheKey{key: key, handlerID: handlerID}]
		if ok && entry.current != nil {
			old = entry.current
		}

		delete(j.mergeCache, mergeCacheKey{key: key, handlerID: handlerID})
		log.Infof("Removing key %s from merge cache for handler %d", key, handlerID)
		return mergeCacheEntry[T]{prev: old}
	}
	// Now we know this is either an add or an update
	var updatedEntry mergeCacheEntry[T]
	if entry, ok := j.mergeCache[mergeCacheKey{key: key, handlerID: handlerID}]; ok {
		if entry.current != nil {
			entry.prev = entry.current
			entry.current = merged
			updatedEntry = entry
		}
	} else {
		updatedEntry = mergeCacheEntry[T]{current: merged}
	}

	j.mergeCache[mergeCacheKey{key: key, handlerID: handlerID}] = updatedEntry
	log.Infof("Updating merge cache for key %s in handler %d with entry: %s", key, handlerID, cmp.Diff(updatedEntry.prev, updatedEntry.current, protocmp.Transform()))
	return updatedEntry
}

func (j *mergejoin[T]) handleInnerCollectionEvent(
	handler func(o []Event[T]),
	handle *registrationHandle,
) func(o []Event[T]) {
	return func(events []Event[T]) {
		handle.Lock()
		defer handle.Unlock()
		handlerID := handle.id
		mergedEvents := make([]Event[T], 0, len(events))
		changedKeys := make([]string, 0, len(events))
		// When we calculate the merged value for a given key, we're looking at the present state
		// of our set of collections, not the state at the time of the event. Therefore, it's possible
		// that the event state is stale and the merged value is different. Therefore, we should just
		// operate on the key and not the event itself.
		for _, i := range events {
			key := GetKey(i.Latest())
			if key == "" {
				log.Warnf("Received event for empty key in handler %d, skipping...", handlerID)
				continue
			}
			log.Infof("Received raw event %s for key %s in handler %d:\n%s", i.Event, key, handlerID, cmp.Diff(i.Old, i.New, protocmp.Transform()))
			changedKeys = append(changedKeys, key)
		}
		// Loop through all of the keys that changed and create an event based on the current state
		// and our cached entries.
		for _, key := range changedKeys {
			merged := j.calculateMerged(key)
			// --CONTENDED ACCESS TO THE CACHE-
			// Each handler writes to the cache across multiple goroutines
			// which leads to flaky behavior.
			entry := j.updateMergeCache(key, handlerID, merged)
			var e Event[T]
			switch {
			case entry.current == nil && entry.prev == nil:
				msg := "Merged (nested) join collection: Received event for key %s in handler %d but it's no longer in our set of collections. Skipping..."
				log.Infof(msg, key, handlerID)
				continue
			// No current entry in our cache for this handler. This key was deleted across all our collections
			case entry.current == nil:
				e = Event[T]{
					Event: controllers.EventDelete,
					Old:   entry.prev,
				}
			// No previous entry in our cache for this handler.
			// This key was added for the first time across all our collections.
			case entry.prev == nil:
				e = Event[T]{
					Event: controllers.EventAdd,
					New:   merged,
				}
			// We have both a current and previous entry in our cache for this handler.
			// This key was updated due to a change in one of our collections
			default:
				e = Event[T]{
					Event: controllers.EventUpdate,
					Old:   entry.prev,
					New:   merged,
				}
			}

			log.Infof("Updating index for key %s in handler %d with event %s", key, handlerID, e.Event)
			j.updateIndex(e, handlerLocalKey[T]{
				Key:       Key[T](key),
				handlerID: handlerID,
			})

			log.Infof("Merged event %s for key %s in handler %d:\n %s", e.Event, key, handlerID, cmp.Diff(e.Old, e.New, protocmp.Transform()))
			mergedEvents = append(mergedEvents, e)
		}
		// Need the lock to run the handler to guarantee events are received in order
		if len(mergedEvents) > 0 {
			// Calling the handler can actually race for nested collections in the case where two collections
			// with the same key are added close to each other. This will cause the handler to be called twice
			// (once for each collection) with the same key.
			handler(mergedEvents)
			log.Infof("Finished executing handler %d with %d merged events", handlerID, len(mergedEvents))
		}
	}
}

func (j *mergejoin[T]) updateIndexLocked(e Event[T], key handlerLocalKey[T]) {
	switch e.Event {
	case controllers.EventAdd, controllers.EventUpdate:
		for _, index := range j.indexes {
			index.update(e, key)
		}
	case controllers.EventDelete:
		for _, index := range j.indexes {
			index.delete(*e.Old, key)
		}
	}
}

func (j *mergejoin[T]) updateIndex(e Event[T], key handlerLocalKey[T]) {
	// This is a no-op if the mergejoin doesn't have any indexes
	if len(j.indexes) == 0 {
		return
	}
	j.Lock()
	defer j.Unlock()
	j.updateIndexLocked(e, key)
}

func (nci joinCollectionIndex[T]) Lookup(key string) []T {
	return nci.lookupForHandler(key, uint64(nci.parent.id))
}

func (nci joinCollectionIndex[T]) lookupForHandler(key string, handlerID uint64) []T {
	nci.parent.RLock()
	defer nci.parent.RUnlock()
	handlerKeys := nci.index[key]
	keys := sets.New[Key[T]]()
	for _, key := range handlerKeys.UnsortedList() {
		if key.handlerID != handlerID {
			continue
		}
		keys.Insert(key.Key)
	}
	if handlerID != uint64(nci.parent.id) {
		log.Infof("Lookup for key %s in index %s for handler %d has %d keys: %s", key, nci.parent.collectionName, handlerID, len(keys), keys)
	}
	res := make([]T, 0, len(keys))
	for k := range keys {
		v, f := nci.parent.mergeCache[mergeCacheKey{key: string(k), handlerID: handlerID}]
		if !f {
			log.WithLabels("key", k, "handlerID", nci.parent.id).Errorf("invalid index state, object does not exist")
			continue
		}
		res = append(res, *v.current)
	}
	return res
}

func (nci joinCollectionIndex[T]) delete(o T, oKey handlerLocalKey[T]) {
	oldIndexKeys := nci.extract(o)
	for _, oldIndexKey := range oldIndexKeys {
		sets.DeleteCleanupLast(nci.index, oldIndexKey, oKey)
	}
}

func (nci joinCollectionIndex[T]) update(ev Event[T], oKey handlerLocalKey[T]) {
	if ev.Old != nil {
		nci.delete(*ev.Old, oKey)
	}
	if ev.New != nil {
		newIndexKeys := nci.extract(*ev.New)
		for _, newIndexKey := range newIndexKeys {
			sets.InsertOrNew(nci.index, newIndexKey, oKey)
		}
	}
}

var (
	_ mergeCollection[any] = &nestedjoin[any]{}
	_ mergeCollection[any] = &join[any]{}
)
