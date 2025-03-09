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
	"sync/atomic"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

type dynamicjoinwithmerge[T any] struct {
	dynamicjoin[T]
	cachedMergeView map[string]T
	dirty           bool
}

func (j *dynamicjoinwithmerge[T]) GetKey(k string) *T {
	j.RLock()
	if !j.dirty { // don't use isDirty() as it takes an unnecessary extra lock
		if r, ok := j.cachedMergeView[k]; ok {
			j.RUnlock()
			return ptr.Of(r)
		}
	}
	var found []T
	for _, c := range maps.SeqStable(j.collectionsByKey) {
		if r := c.GetKey(k); r != nil {
			if j.merge == nil {
				return r
			}
			found = append(found, *r)
		}
	}
	j.RUnlock()
	if len(found) == 0 {
		return nil
	}
	f := j.merge(found)
	j.Lock()
	j.cachedMergeView[k] = f
	j.dirty = false
	j.Unlock()
	return ptr.Of(f)
}

func (j *dynamicjoinwithmerge[T]) List() []T {
	if j.merge != nil {
		return j.mergeList()
	}
	return j.quickList()
}

func (j *dynamicjoinwithmerge[T]) quickList() []T {
	j.RLock()
	defer j.RUnlock()
	var res []T
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

func (j *dynamicjoinwithmerge[T]) mergeList() []T {
	j.RLock()
	if !j.dirty {
		j.RUnlock()
		return maps.Values(j.cachedMergeView)
	}

	res := map[Key[T]][]T{}
	for _, c := range maps.SeqStable(j.collectionsByKey) {
		for _, i := range c.List() {
			key := getTypedKey(i)
			res[key] = append(res[key], i)
		}
	}

	var l []T
	j.Lock()
	defer j.Unlock()
	for key, ts := range res {
		vals := j.merge(ts)
		j.cachedMergeView[string(key)] = vals
		l = append(l, j.merge(ts))
	}
	j.dirty = false
	return l
}

// CachedDynamicJoinCollectionWithMerge is the same as a dynamic join with a custom merge function,
// but its implementation is a bit more efficient. It caches the merged view of the collection,
// so that it doesn't have to recompute the merge every time. This is useful if the merge function
// is expensive to compute, or if the collection is large and the merge function is called often.
func CachedDynamicJoinWithMergeCollection[T any](cs []Collection[T], merge func(ts []T) T, opts ...CollectionOption) DynamicCollection[T] {
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
	if o.joinUnchecked && merge != nil {
		log.Warn("DynamicJoinWithMergeCollection: unchecked overlap is ineffective with a merge function")
	}
	// Create a shallow copy of the collection map before returning it
	// to avoid data races or locking in the waituntilsync goroutine
	collections := maps.Clone(c)
	dj := &dynamicjoinwithmerge[T]{
		dynamicjoin: dynamicjoin[T]{
			collectionName:   o.name,
			id:               nextUID(),
			collectionsByKey: collections,
			synced:           &synced,
			uncheckedOverlap: o.joinUnchecked,
			syncer: pollSyncer{
				name: o.name,
				f:    synced.Load,
			},
			merge: merge,
		},
		cachedMergeView: make(map[string]T),
		dirty:           true,
	}

	// NOTE: There's no ordering guarantees with event handlers, so this isn't guaranteed
	// to run before other event handlers. Hopefully eventual consistency is ok?
	// TODO: Consider creating a timer that sets dirty every so often. It's hacky
	// but could be useful for long running processes that don't get many events.
	dj.Register(func(_ Event[T]) {
		dj.Lock()
		defer dj.Unlock()
		dj.dirty = true
	})

	return dj
}
