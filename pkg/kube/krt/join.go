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
	merge            func(ts []T) *T

	// mergedCache is a cache of the merged results for each key
	// This is used to ensure we have accurate eventing when dealing
	// with merged collections (e.g. that event.Old is set correctly).
	// This will prevent unnecessary xDS pushes; without it, we'd
	// Old != New (merged) would always be true and we'd push.
	mergedCache  map[mergedCacheKey]mergedCacheEntry[T]
	sync.RWMutex // protects mergedCache
}

type mergedCacheKey struct {
	handlerID string // An empty handler id corresponds to the collection itself (e.g. during GetKey() or List())
	key       string
}

type mergedCacheEntry[T any] struct {
	prev    *T
	current *T // Must always be set of there's an entry in the map
}

func (j *join[T]) quickGetKey(k string) *T {
	for _, c := range j.collections {
		if r := c.GetKey(k); r != nil {
			if j.merge == nil {
				return r
			}
		}
	}

	return nil
}

// TODO: Switch to mergedCache implementation
func (j *join[T]) GetKey(k string) *T {
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

func (j *join[T]) quickList() []T {
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

func (j *join[T]) mergeList() []T {
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

func (j *join[T]) List() []T {
	if j.merge != nil {
		j.mergeList()
	}

	return j.quickList()
}

func (j *join[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched[T](j, f)
}

func (j *join[T]) registerBatchUnmerged(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	sync := multiSyncer{}
	removes := []func(){}
	for _, c := range j.collections {
		reg := c.RegisterBatch(f, runExistingState)
		removes = append(removes, reg.UnregisterHandler)
		sync.syncers = append(sync.syncers, reg)
	}
	return joinHandlerRegistration{
		Syncer:  sync,
		removes: removes,
	}
}

func (j *join[T]) calculateMerged(k string) *T {
	var found []T
	for _, c := range j.collections {
		if r := c.GetKey(k); r != nil {
			found = append(found, *r)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return j.merge(found)
}

func maybeInitMergeCacheForHandlerLocked[T any](cache map[mergedCacheKey]mergedCacheEntry[T], handlerID string) {
	// First check to see if there's been any merge cache entry before us
	// by checking the empty handler mergeKey
	for key, entry := range cache {
		// no-op
		if key.handlerID != "" {
			continue
		}

		// There are entries in the cache; copy them to our handlerID
		cache[mergedCacheKey{key: key.key, handlerID: handlerID}] = entry
	}
}

func updateMergeCacheLocked[T any](cache map[mergedCacheKey]mergedCacheEntry[T], merged *T, key, handlerID string) mergedCacheEntry[T] {
	if merged == nil {
		// First get the existing value from the cache (if it exists)
		var old *T
		entry, ok := cache[mergedCacheKey{key: key, handlerID: handlerID}]
		if ok && entry.current != nil {
			old = entry.current
		}

		delete(cache, mergedCacheKey{key: key, handlerID: handlerID})
		// Eagerly keep collection reads up to date; delete the collection entry too
		delete(cache, mergedCacheKey{key: key})
		return mergedCacheEntry[T]{prev: old}
	}
	// Now we know this is either an add or an update
	var updatedEntry mergedCacheEntry[T]
	if entry, ok := cache[mergedCacheKey{key: key, handlerID: handlerID}]; ok {
		if entry.current != nil {
			entry.prev = entry.current
			entry.current = merged
			updatedEntry = entry
		}
	} else {
		updatedEntry = mergedCacheEntry[T]{current: merged}
	}

	cache[mergedCacheKey{key: key, handlerID: handlerID}] = updatedEntry
	// It's probably simpler to just always set the collection entry multiple times
	// TODO: Ensure old vlues don't stick around for too long and prevent garbage collection
	cache[mergedCacheKey{key: key}] = updatedEntry
	return updatedEntry
}

func (j *join[T]) updateMergedCache(key, handlerID string, merged *T) mergedCacheEntry[T] {
	j.Lock()
	defer j.Unlock()
	return updateMergeCacheLocked(j.mergedCache, merged, key, handlerID)
}

func handleInnerCollectionEvent[T any](
	handler func(o []Event[T]),
	handlerID string,
	getMergedForKey func(key string) *T,
	updateMergedCache func(key, handlerID string, merged *T) mergedCacheEntry[T],
) func(o []Event[T]) {
	return func(events []Event[T]) {
		mergedEvents := make([]Event[T], 0, len(events))
		changedKeys := make([]string, 0, len(events))
		// When we calculate the merged value for a given key, we're looking at the present state
		// of our set of collections, not the state at the time of the event. Therefore, it's possible
		// that the event state is stale and the merged value is different. Therefore, we should just
		// operate on the key and not the event itself.
		for _, i := range events {
			key := GetKey(i.Latest())
		}
		// Loop through all of the keys that changed and create an event based on the currente state
		// and our cached entries.
		for _, key := range changedKeys {
			merged := getMergedForKey(key)
			entry := updateMergedCache(key, handlerID, merged)
			var e Event[T]
			switch {
			case entry.current == nil && entry.prev == nil:
				msg := "Merged (nested) join collection: Received event for key %s in handler %s but it's not longer in our set of collections. Skipping..."
				log.Warnf(msg, key, handlerID)
				continue
			// No current entry in our cache for this handler. This key was deleted across all our collections
				e = Event[T]{
					Event: controllers.EventDelete,
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
			mergedEvents = append(mergedEvents, e)
		}
		if len(mergedEvents) > 0 {
			// Calling the handler can actually race for nested collections in the case where two collections
			// with the same key are added close to each other. This will cause the handler to be called twice
			// (once for each collection) with the same key.
			handler(mergedEvents)
		}
	}
}

func (j *join[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	if j.merge == nil {
		return j.registerBatchUnmerged(f, runExistingState)
	}
	sync := multiSyncer{}
	removes := []func(){}
	handlerID := strconv.FormatUint(globalUIDCounter.Inc(), 10)
	j.Lock()
	maybeInitMergeCacheForHandlerLocked(j.mergedCache, handlerID)
	j.Unlock()

	for _, c := range j.collections {
		reg := c.RegisterBatch(handleInnerCollectionEvent(f, handlerID, j.calculateMerged, j.updateMergedCache), runExistingState)
		removes = append(removes, reg.UnregisterHandler)
		sync.syncers = append(sync.syncers, reg)
	}
	return joinHandlerRegistration{
		Syncer:  sync,
		removes: removes,
	}
}

type joinHandlerRegistration struct {
	Syncer
	removes []func()
}

func (j joinHandlerRegistration) UnregisterHandler() {
	for _, remover := range j.removes {
		remover()
	}
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
// Collection[T], picking the first object found when duplicates are found.
// Access operations (e.g. GetKey and List) will perform a best effort stable ordering
// of the list of elements returned; however, this ordering will not be persistent across
// istiod restarts.
func JoinCollection[T any](cs []Collection[T], opts ...CollectionOption) Collection[T] {
	return JoinWithMergeCollection[T](cs, nil, opts...)
}

// JoinWithMergeCollection combines multiple Collection[T] into a single
// Collection[T] merging equal objects into one record
// in the resulting Collection based on the provided merge function.
//
// The merge function *cannot* assume a stable ordering of the list of elements passed to it. Therefore, access operations (e.g. GetKey and List) will
// will only be deterministic if the merge function is deterministic. The merge function should return nil if no value should be returned.
func JoinWithMergeCollection[T any](cs []Collection[T], merge func(ts []T) *T, opts ...CollectionOption) Collection[T] {
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

	if o.joinUnchecked && merge != nil {
		log.Warn("JoinWithMergeCollection: unchecked overlap is ineffective with a merge function")
		o.joinUnchecked = false
	}
	j := &join[T]{
		collectionName:   o.name,
		id:               nextUID(),
		synced:           synced,
		collections:      c,
		uncheckedOverlap: o.joinUnchecked,
		syncer: channelSyncer{
			name:   o.name,
			synced: synced,
		},
		merge:       merge,
		mergedCache: make(map[mergedCacheKey]mergedCacheEntry[T]),
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	return j
}
