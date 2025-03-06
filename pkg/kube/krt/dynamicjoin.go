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
	"sync"
	"sync/atomic"

	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/util/sets"
)

type DynamicCollection[T any] interface {
	Collection[T]
	AddOrUpdateCollection(key string, c Collection[T])
	RemoveCollection(key string)
}

type dynamicjoin[T any] struct {
	collectionName   string
	id               collectionUID
	collectionsByKey map[string]internalCollection[T]
	sync.RWMutex
	synced           *atomic.Bool
	uncheckedOverlap bool
	syncer           Syncer
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

func (j *dynamicjoin[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched[T](j, f)
}

func (j *dynamicjoin[T]) RegisterBatch(f func(o []Event[T], initialSync bool), runExistingState bool) HandlerRegistration {
	sync := multiSyncer{}
	removes := []func(){}
	for _, c := range maps.SeqStable(j.collectionsByKey) {
		reg := c.RegisterBatch(f, runExistingState)
		removes = append(removes, reg.UnregisterHandler)
		sync.syncers = append(sync.syncers, reg)
	}
	return joinHandlerRegistration{
		Syncer:  sync,
		removes: removes,
	}
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

// nolint: unused // (not true)
type dynamicJoinIndexer struct {
	indexers []kclient.RawIndexer
}

// nolint: unused // (not true)
func (j dynamicJoinIndexer) Lookup(key string) []any {
	var res []any
	first := true
	for _, i := range j.indexers {
		l := i.Lookup(key)
		if len(l) > 0 && first {
			// TODO: add option to merge slices
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
	ji := joinIndexer{indexers: make([]kclient.RawIndexer, 0, len(j.collectionsByKey))}
	for _, c := range maps.SeqStable(j.collectionsByKey) {
		ji.indexers = append(ji.indexers, c.index(extract))
	}
	return ji
}

func (j *dynamicjoin[T]) Synced() Syncer {
	// We use a poll syncer here because the member collections of dynamicjoin can
	// change; it's not a one-and-done sync check, so channels are inappropriate.
	return pollSyncer{
		name: j.collectionName,
		f:    func() bool { return j.synced.Load() },
	}
}

func (j *dynamicjoin[T]) HasSynced() bool {
	return j.syncer.HasSynced()
}

func (j *dynamicjoin[T]) WaitUntilSynced(stop <-chan struct{}) bool {
	return j.syncer.WaitUntilSynced(stop)
}

func (j *dynamicjoin[T]) AddOrUpdateCollection(key string, c Collection[T], stop <-chan struct{}) {
	j.Lock()
	ic := c.(internalCollection[T])
	j.collectionsByKey[key] = ic
	j.Unlock()

	jSynced := j.synced.Load()
	if jSynced && !ic.HasSynced() {
		j.synced.Store(false)
		go func(s <-chan struct{}) {
			if !ic.WaitUntilSynced(stop) {
				return
			}
			j.synced.Store(true)
			log.Infof("%v re-synced", j.name)
		}(stop)
	}
}

func (j *dynamicjoin[T]) RemoveCollection(key string) {
	j.Lock()
	defer j.Unlock()

	if _, ok := j.collectionsByKey[key]; !ok {
		log.Warnf("Collection %v not found in %v", key, j.name)
		return
	}
	delete(j.collectionsByKey, key)
}
