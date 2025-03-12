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
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

type nestedjoin[T any] struct {
	collectionName           string
	id                       collectionUID
	collections              internalCollection[Collection[T]]
	synced                   <-chan struct{}
	uncheckedOverlap         bool
	syncer                   Syncer
	collectionChangeHandlers []func(collectionChangeEvent[T])
	sync.RWMutex
}

func (j *nestedjoin[T]) GetKey(k string) *T {
	for _, c := range j.collections.List() {
		if r := c.GetKey(k); r != nil {
			return r
		}
	}
	return nil
}

func (j *nestedjoin[T]) List() []T {
	var res []T
	if j.uncheckedOverlap {
		first := true
		for _, c := range j.collections.List() {
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

	for _, c := range j.collections.List() {
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

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) index(extract func(o T) []string) kclient.RawIndexer {
	j.Lock() // Take a write lock since we're also updating the collection change handlers
	defer j.Unlock()
	ji := &dynamicJoinIndexer{indexers: make(map[collectionUID]kclient.RawIndexer)}
	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		ji.indexers[ic.uid()] = ic.index(extract)
	}
	j.registerCollectionChangeHandlerLocked(func(e collectionChangeEvent[T]) {
		ji.Lock()
		defer ji.Unlock()
		switch e.eventType {
		case collectionMembershipEventAdd:
			ji.indexers[e.collectionValue.uid()] = e.collectionValue.index(extract)
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

// Register and RegisterAsBatch are public interfaces for dealing with events from the inner collection.
// This function is for registering event handlers for the outer collection.
func (j *nestedjoin[T]) registerBatchForOuterCollection(f func(o []Event[Collection[T]]), runExistingState bool) HandlerRegistration {
	return j.collections.RegisterBatch(f, runExistingState)
}

func (j *nestedjoin[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched[T](j, f)
}

func (j *nestedjoin[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	j.Lock() // Take a write lock since we're also updating the collection change handlers
	defer j.Unlock()
	sync := dynamicMultiSyncer{
		syncers: make(map[collectionUID]Syncer),
	}
	removes := map[collectionUID]func(){}
	for _, c := range j.collections.List() {
		reg := c.RegisterBatch(f, runExistingState)
		ic := c.(internalCollection[T])
		removes[ic.uid()] = reg.UnregisterHandler
		sync.syncers[ic.uid()] = reg
	}
	djhr := &dynamicJoinHandlerRegistration{
		dynamicMultiSyncer: sync,
		removes:            removes,
	}

	j.registerCollectionChangeHandlerLocked(func(e collectionChangeEvent[T]) {
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
			// do this in a goroutine so we don't block running expensive handlers
			go func() {
				var events []Event[T]
				for _, elem := range e.collectionValue.List() {
					events = append(events, Event[T]{Old: &elem, Event: controllers.EventDelete})
				}
				f(events)
			}()
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

// The caller MUST have the lock when executing this function
func (j *nestedjoin[T]) registerCollectionChangeHandlerLocked(h func(e collectionChangeEvent[T])) {
	j.collectionChangeHandlers = append(j.collectionChangeHandlers, h)
}

func NestedJoinCollection[T any](collections Collection[Collection[T]], opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NestedJoin[%v]", ptr.TypeName[T]())
	}

	ics := collections.(internalCollection[Collection[T]])
	synced := make(chan struct{})

	go func() {
		// Need to make sure the outer collection is synced first
		if !collections.WaitUntilSynced(o.stop) {
			return
		}
		// keithmattix: Can we assume that all inner collections are synced
		// if the outer one is?
		for _, c := range collections.List() {
			if !c.WaitUntilSynced(o.stop) {
				return
			}
		}
		close(synced)
		log.Infof("%v synced", o.name)
	}()

	j := &nestedjoin[T]{
		collectionName:   o.name,
		id:               nextUID(),
		synced:           synced,
		uncheckedOverlap: o.joinUnchecked,
		collections:      ics,
		syncer: channelSyncer{
			name:   o.name,
			synced: synced,
		},
	}

	j.registerBatchForOuterCollection(func(o []Event[Collection[T]]) {
		j.RLock()
		defer j.RUnlock()

		for _, e := range o {
			for _, h := range j.collectionChangeHandlers {
				switch e.Event {
				case controllers.EventAdd:
					if e.New == nil {
						log.Warnf("Event %v has no new value", e.Event)
						continue
					}
					h(collectionChangeEvent[T]{eventType: collectionMembershipEventAdd, collectionValue: any(*e.New).(internalCollection[T])})
				case controllers.EventUpdate:
					// Send a delete and then an add
					h(collectionChangeEvent[T]{
						eventType:       collectionMembershipEventDelete,
						collectionValue: any(*e.Old).(internalCollection[T]),
					})
					h(collectionChangeEvent[T]{
						eventType:       collectionMembershipEventAdd,
						collectionValue: any(*e.New).(internalCollection[T]),
					})
				case controllers.EventDelete:
					if e.Old == nil {
						log.Warnf("Event %v has no new value", e.Event)
						continue
					}
					h(collectionChangeEvent[T]{eventType: collectionMembershipEventDelete, collectionValue: any(*e.Old).(internalCollection[T])})
				}
			}
		}
	}, false)

	return j
}
