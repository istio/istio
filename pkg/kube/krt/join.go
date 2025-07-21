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

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type join[T any] struct {
	collections []internalCollection[T]
	synced      <-chan struct{}
	syncer      Syncer
	metadata    Metadata
	*mergejoin[T]
}

func (j *join[T]) getCollections() []Collection[T] {
	// This is used by the collection lister to get the collections for this join
	// so it can be used in a nested join.
	return slices.Map(j.collections, func(e internalCollection[T]) Collection[T] {
		return e
	})
}

func (j *join[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched(j, f)
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

func (j *join[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	h := &registrationHandle{
		id: globalUIDCounter.Inc(),
	}
	return j.registerBatchForHandler(f, runExistingState, h)
}

func (j *join[T]) registerBatchForHandler(f func(o []Event[T]), runExistingState bool, handle *registrationHandle) HandlerRegistration {
	if j.merge == nil {
		return j.registerBatchUnmerged(f, runExistingState)
	}
	j.Lock()
	defer j.Unlock()
	sync := multiSyncer{}
	j.handlers.Insert(handle.id)
	removes := []func(){}
	// Only init this new handler with the collection's state if we're not running existing state.
	// Handlers that run existing state will catch up once they get all of their events.
	if !runExistingState && handle.id != uint64(j.uid()) {
		j.maybeInitMergeCacheForHandlerLocked(handle.id)
	}

	for _, c := range j.collections {
		reg := c.RegisterBatch(j.handleInnerCollectionEvent(f, handle), runExistingState)
		removes = append(removes, reg.UnregisterHandler)
		sync.syncers = append(sync.syncers, reg)
	}
	return joinHandlerRegistration{
		Syncer:    sync,
		removes:   removes,
		handlerID: handle.id,
	}
}

type joinHandlerRegistration struct {
	Syncer
	removes   []func()
	handlerID uint64
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
			// TODO: Add support for merging
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
	if j.merge == nil {
		ji := joinIndexer[T]{indexers: make([]indexer[T], 0, len(j.collections))}
		for _, c := range j.collections {
			ji.indexers = append(ji.indexers, c.index(name, extract))
		}
		return ji
	}

	return j.mergejoin.index(name, extract)
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

	if o.joinUnchecked && merge != nil {
		log.Warn("JoinWithMergeCollection: unchecked overlap is ineffective with a merge function")
		o.joinUnchecked = false
	}
	j := &join[T]{
		synced:      synced,
		collections: c,
		syncer: channelSyncer{
			name:   o.name,
			synced: synced,
		},
		mergejoin: &mergejoin[T]{
			id:               nextUID(),
			collectionName:   o.name,
			mergeCache:       make(map[mergeCacheKey]mergeCacheEntry[T]),
			indexes:          make(map[string]joinCollectionIndex[T]),
			uncheckedOverlap: o.joinUnchecked,
			handlers:         sets.New[uint64](),
			merge:            merge,
		},
	}

	j.mergejoin.collections = j

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	j.registerBatchForHandler(func(_ []Event[T]) {

	}, true, &registrationHandle{
		id: uint64(j.id),
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

	return j
}
