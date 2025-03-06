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
	merge            func(ts []T) *T

	seenFirstAddForKey map[string]struct{}
	sync.Mutex         // protects seenFirstAddForKey
}

func (j *join[T]) GetKey(k string) *T {
	var found []T
	for _, c := range j.collections {
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
	unmergedByKey := map[Key[T]][]T{}
	for _, c := range j.collections {
		for _, i := range c.List() {
			key := getTypedKey(i)
			unmergedByKey[key] = append(unmergedByKey[key], i)
		}
	}

	merged := make([]T, 0, len(unmergedByKey))
	for _, ts := range unmergedByKey {
		m := j.merge(ts)
		if m != nil {
			merged = append(merged, *m)
		}
	}

	return merged
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

func getMergedDelete[T any](e Event[T], merged *T) Event[T] {
	if merged == nil {
		// This is expected if the item is globally deleted
		// across all collections. Use the original delete
		// event.
		return e
	}
	// There are items for this key in other collections. This delete is actually
	// an update. The Old isn't 100% accurate since we can't see what the old merged
	// value was, but handlers probably shouldn't be doing manual diffing to the point
	// where this would actually matter.
	return Event[T]{
		Event: controllers.EventUpdate,
		Old:   e.Old,
		New:   merged,
	}
}

func getMergedAdd[T any](e Event[T], merged *T) Event[T] {
	// Merged should never be nil after an add; log it in case we come across this
	// in the future.
	if merged == nil {
		log.Warnf("JoinCollection: merge function returned nil for add event %v", e)
	}
	// This is an update triggered by the add of a duplicate item.
	// We use the added item as the old value as a best effort.
	return Event[T]{
		Event: controllers.EventUpdate,
		Old:   e.New,
		New:   merged,
	}
}

func getMergedUpdate[T any](e Event[T], merged *T) Event[T] {
	if merged == nil {
		log.Warnf("JoinCollection: merge function returned nil for update event %v", e)
	}
	// This is an update triggered by the add of a duplicate item.
	// We use the added item as the old value as a best effort.
	return Event[T]{
		Event: controllers.EventUpdate,
		Old:   e.Old,
		New:   merged,
	}
}

func (j *join[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	if j.merge == nil {
		return j.registerBatchUnmerged(f, runExistingState)
	}
	sync := multiSyncer{}
	removes := []func(){}

	for _, c := range j.collections {
		reg := c.RegisterBatch(func(o []Event[T]) {
			// Lock the map during this entire handler for readability and to ensure events remain in-order
			// across collections
			j.Lock()
			defer j.Unlock()
			mergedEvents := make([]Event[T], 0, len(o))
			for _, i := range o {
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
					// If we haven't seen an add for this key before, this should be a real add
					// regardless. This is to prevent the case where the collection source starts
					// its initial sync with duplicate keys in different collections. Without this check,
					// both events would look like updates.
					if _, ok := j.seenFirstAddForKey[key]; !ok {
						// We haven't seen an add for this key before, so we need to take a write lock
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
		o.joinUnchecked = false
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
		merge:              merge,
		seenFirstAddForKey: make(map[string]struct{}),
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	return j
}
