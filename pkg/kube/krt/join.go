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

type eventSyncMap struct {
	keys map[string]struct{}
	sync.Mutex
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

func getMergedDelete[T any](e Event[T], merged, old *T) Event[T] {
	if merged == nil {
		// This is expected if the item is globally deleted
		// across all collections. Use the original delete
		// event.
		return e
	}
	if old == nil {
		log.Warnf("Given value is nil for merged delete handling %#+v for %s", e, ptr.TypeName[T]())
		old = e.Old
	}
	// There are items for this key in other collections. This delete is actually
	// an update. Use the given old value as the old value. If it's nil, fall back
	// to the old value in the event.

	return Event[T]{
		Event: controllers.EventUpdate,
		Old:   old,
		New:   merged,
	}
}

func getMergedAdd[T any](e Event[T], merged, old *T) Event[T] {
	// Merged should never be nil after an add; log it in case we come across this
	// in the future.
	if merged == nil {
		log.Warnf("JoinCollection: merge function returned nil for add event %v", e)
	}

	if old == nil {
		log.Warnf("Given value is nil for merged add -> update %#+v for %s", e, ptr.TypeName[T]())
		old = e.Old
	}
	// This is an update triggered by the add of a duplicate item.
	// We use the added item as the old value as a best effort.
	return Event[T]{
		Event: controllers.EventUpdate,
		Old:   old,
		New:   merged,
	}
}

func getMergedUpdate[T any](e Event[T], merged, old *T) Event[T] {
	if merged == nil {
		log.Warnf("JoinCollection: merge function returned nil for update event %v", e)
	}

	if old == nil {
		log.Warnf("Given value is nil for merged update handling %#+v for %s", e, ptr.TypeName[T]())
		old = e.Old
	}
	// This is an update triggered by the add of a duplicate key.
	// We use the added item as the old value as a best effort.
	return Event[T]{
		Event: controllers.EventUpdate,
		Old:   old,
		New:   merged,
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

func (j *join[T]) updateMergedCache(key, handlerID string, merged *T) mergedCacheEntry[T] {
	j.Lock()
	defer j.Unlock()
	if merged == nil {
		// This is a legit delete; remove it from the cache
		delete(j.mergedCache, mergedCacheKey{key: key, handlerID: handlerID})
		// Eagerly keep collection reads up to date; delete the collection entry too
		delete(j.mergedCache, mergedCacheKey{key: key})
		return mergedCacheEntry[T]{}
	}
	// Now we know this is either an add or an update
	var updatedEntry mergedCacheEntry[T]
	if entry, ok := j.mergedCache[mergedCacheKey{key: key, handlerID: handlerID}]; ok {
		if entry.current != nil {
			entry.prev = entry.current
			entry.current = merged
			updatedEntry = entry
		}
	} else {
		updatedEntry = mergedCacheEntry[T]{current: merged}
	}
	j.mergedCache[mergedCacheKey{key: key, handlerID: handlerID}] = updatedEntry
	// It's probably simpler to just always set the collection entry multiple times
	// TODO: Ensure old vlues don't stick around for too long and prevent garbage collection
	j.mergedCache[mergedCacheKey{key: key}] = updatedEntry
	return updatedEntry
}

func (j *join[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	if j.merge == nil {
		return j.registerBatchUnmerged(f, runExistingState)
	}
	sync := multiSyncer{}
	removes := []func(){}
	handlerID := strconv.FormatUint(globalUIDCounter.Inc(), 10)

	// This is tricky because each handler has its own goroutine and we don't want to get
	// multiple adds if a resource is added to multiple collections in the join at the same time.
	// We want an add (for the first one) and then an update, and we want this to happen for each handler
	// meaning we can't use the join struct to synchronize. Instead, we created a map per handler
	// (note: not per handler per inner collection; 1 map for all collections).
	// No need for a lock since each handler has its own queue/goroutine.
	seenFirstAddForKey := &eventSyncMap{
		keys: make(map[string]struct{}),
	}
	for _, c := range j.collections {
		reg := c.RegisterBatch(func(o []Event[T]) {
			// Lock the map during this entire handler for readability and to ensure events remain in-order
			// across collections
			seenFirstAddForKey.Lock()
			defer seenFirstAddForKey.Unlock()
			mergedEvents := make([]Event[T], 0, len(o))
			for _, i := range o {
				key := GetKey(i.Latest())
				merged := j.calculateMerged(key)
				entry := j.updateMergedCache(key, handlerID, merged)
				old := entry.prev
				switch i.Event {
				case controllers.EventDelete:
					mergedEvents = append(mergedEvents, getMergedDelete(i, merged, old))
					if merged == nil {
						// Remove the key from the seenFirstAddForKey map. It's unlikely that
						// we would have two adds in different sub-collections at the exact same time
						// but handle it just in case
						delete(seenFirstAddForKey.keys, key)
					}
				case controllers.EventAdd:
					// If we haven't seen an add for this key before, this should be a real add
					// regardless. This is to prevent the case where the collection source starts
					// its initial sync with duplicate keys in different collections. Without this check,
					// both events would look like updates.
					if _, ok := seenFirstAddForKey.keys[key]; !ok {
						// We haven't seen an add for this key before, so we need to take a write lock
						seenFirstAddForKey.keys[key] = struct{}{}
						mergedEvents = append(mergedEvents, Event[T]{
							Event: controllers.EventAdd,
							Old:   nil,
							New:   merged,
						})
						continue
					}
					mergedEvents = append(mergedEvents, getMergedAdd(i, merged, old))
				case controllers.EventUpdate:
					mergedEvents = append(mergedEvents, getMergedUpdate(i, merged, old))
				}
			}
			f(mergedEvents)
		}, runExistingState)
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
