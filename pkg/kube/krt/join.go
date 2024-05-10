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
	collectionName string
	id             collectionUID
	collections    []internalCollection[T]
	synced         <-chan struct{}
}

func (j *join[T]) GetKey(k Key[T]) *T {
	for _, c := range j.collections {
		if r := c.GetKey(k); r != nil {
			return r
		}
	}
	return nil
}

func (j *join[T]) List() []T {
	res := []T{}
	found := sets.New[Key[T]]()
	for _, c := range j.collections {
		for _, i := range c.List() {
			key := GetKey(i)
			if !found.InsertContains(key) {
				// Only keep it if it is the first time we saw it, as our merging mechanism is to keep the first one
				res = append(res, i)
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
func (j *join[I]) dump() {
	log.Errorf("> BEGIN DUMP (join %v)", j.collectionName)
	for _, c := range j.collections {
		c.dump()
	}
	log.Errorf("< END DUMP (join %v)", j.collectionName)
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
		collectionName: o.name,
		id:             nextUID(),
		synced:         synced,
		collections:    c,
	}
}
