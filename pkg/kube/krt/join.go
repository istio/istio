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

	"istio.io/istio/pkg/kube/kclient"
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

func (j *join[T]) Register(f func(o Event[T])) Syncer {
	return registerHandlerAsBatched[T](j, f)
}

func (j *join[T]) RegisterBatch(f func(o []Event[T], initialSync bool), runExistingState bool) Syncer {
	sync := multiSyncer{}
	for _, c := range j.collections {
		sync.syncers = append(sync.syncers, c.RegisterBatch(f, runExistingState))
	}
	return sync
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
type joinIndexer struct {
	indexers []kclient.RawIndexer
}

// nolint: unused // (not true)
func (j joinIndexer) Lookup(key string) []any {
	var res []any
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
func (j *join[T]) index(extract func(o T) []string) kclient.RawIndexer {
	ji := joinIndexer{indexers: make([]kclient.RawIndexer, 0, len(j.collections))}
	for _, c := range j.collections {
		ji.indexers = append(ji.indexers, c.index(extract))
	}
	return ji
}

func (j *join[T]) Synced() Syncer {
	return channelSyncer{
		name:   j.collectionName,
		synced: j.synced,
	}
}

// JoinCollection combines multiple Collection[T] into a single
// Collection[T] merging equal objects into one record
// in the resulting Collection
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
			if !c.Synced().WaitUntilSynced(o.stop) {
				return
			}
		}
		close(synced)
		log.Infof("%v synced", o.name)
	}()
	// TODO: in the future, we could have a custom merge function. For now, since we just take the first, we optimize around that case
	return &join[T]{
		collectionName:   o.name,
		id:               nextUID(),
		synced:           synced,
		collections:      c,
		uncheckedOverlap: o.joinUnchecked,
	}
}
