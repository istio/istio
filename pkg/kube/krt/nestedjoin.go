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
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

type nestedjoin[T any] struct {
	collectionName           string
	id                       collectionUID
	collections              internalCollection[Collection[T]]
	synced                   <-chan struct{}
	syncer                   Syncer
	collectionChangeHandlers []func(collectionChangeEvent[T])
	merge                    func(ts []T) *T

	sync.RWMutex
}

func (j *nestedjoin[T]) GetKey(k string) *T {
	var found []T
	for _, c := range j.collections.List() {
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

func (j *nestedjoin[T]) quickList() []T {
	var res []T
	var found sets.String
	first := true

	// We need stable ordering so we'll loop through the outer collections first
	// saving state as we go
	collectionsByUID := make(map[collectionUID]Collection[T])
	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		collectionsByUID[ic.uid()] = ic
	}

	// Now loop through the collections in UID order
	for _, c := range maps.SeqStable(collectionsByUID) {
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

func (j *nestedjoin[T]) mergeList() []T {
	res := map[Key[T]][]T{}
	for _, c := range j.collections.List() {
		for _, i := range c.List() {
			key := getTypedKey(i)
			res[key] = append(res[key], i)
		}
	}

	var l []T
	for _, ts := range res {
		m := j.merge(ts)
		if m != nil {
			l = append(l, *m)
		}
	}

	return l
}

func (j *nestedjoin[T]) List() []T {
	if j.merge != nil {
		j.mergeList()
	}

	return j.quickList()
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

func (j *nestedjoin[T]) Register(f func(o Event[T])) HandlerRegistration {
	return registerHandlerAsBatched(j, f)
}

func (j *nestedjoin[T]) registerBatchUnmerged(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	j.Lock() // Take a write lock since we're also updating the collection change handlers
	defer j.Unlock()
	syncers := make(map[collectionUID]Syncer)
	removes := map[collectionUID]func(){}
	for _, c := range j.collections.List() {
		reg := c.RegisterBatch(f, runExistingState)
		ic := c.(internalCollection[T])
		removes[ic.uid()] = reg.UnregisterHandler
		syncers[ic.uid()] = reg
	}
	djhr := &dynamicJoinHandlerRegistration{
		syncers: syncers,
		removes: removes,
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
			var events []Event[T]
			for _, elem := range e.collectionValue.List() {
				events = append(events, Event[T]{Old: &elem, Event: controllers.EventDelete})
			}
			f(events)
			return
		}
	})

	return djhr
}

func (j *nestedjoin[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	if j.merge == nil {
		return j.registerBatchUnmerged(f, runExistingState)
	}
	j.Lock() // Take a write lock since we're also updating the collection change handlers
	defer j.Unlock()
	syncers := make(map[collectionUID]Syncer)
	removes := map[collectionUID]func(){}

	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		reg := c.RegisterBatch(func(o []Event[T]) {
			mergedEvents := make([]Event[T], 0, len(o))
			for _, i := range o {
				key := GetKey(i.Latest())
				merged := j.GetKey(key)
				switch i.Event {
				case controllers.EventDelete:
					mergedEvents = append(mergedEvents, getMergedDelete(i, merged))
				case controllers.EventAdd:
					mergedEvents = append(mergedEvents, getMergedAdd(i, merged))
				case controllers.EventUpdate:
					mergedEvents = append(mergedEvents, getMergedUpdate(i, merged))
				}
				f(mergedEvents)
			}
		}, runExistingState)
		removes[ic.uid()] = reg.UnregisterHandler
		syncers[ic.uid()] = reg
	}
	djhr := &dynamicJoinHandlerRegistration{
		syncers: syncers,
		removes: removes,
	}

	j.registerCollectionChangeHandlerLocked(func(e collectionChangeEvent[T]) {
		djhr.Lock()
		defer djhr.Unlock()
		switch e.eventType {
		case collectionMembershipEventAdd:
			reg := e.collectionValue.RegisterBatch(func(o []Event[T]) {
				mergedEvents := make([]Event[T], 0, len(o))
				for _, i := range o {
					key := GetKey(i.Latest())
					merged := j.GetKey(key)
					switch i.Event {
					case controllers.EventDelete:
						mergedEvents = append(mergedEvents, getMergedDelete(i, merged))
					case controllers.EventAdd:
						mergedEvents = append(mergedEvents, getMergedAdd(i, merged))
					case controllers.EventUpdate:
						mergedEvents = append(mergedEvents, getMergedUpdate(i, merged))
					}
				}
				f(mergedEvents)
			}, runExistingState)
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
			var events []Event[T]
			if j.merge == nil {
				for _, elem := range e.collectionValue.List() {
					events = append(events, Event[T]{Old: &elem, Event: controllers.EventDelete})
				}
				f(events)
				return
			}
			// We're merging so this is a bit more complicated
			items := make(map[Key[T]][]T)
			// First loop through the collection to get the items by their keys
			for _, c := range e.collectionValue.List() {
				key := getTypedKey(c)
				items[key] = append(items[key], c)
			}
			// Now loop through the keys and compare them to our current list of collections
			// to see if it's actually deleted
			for key, ts := range maps.SeqStable(items) {
				res := j.GetKey(string(key))
				m := j.merge(ts)
				// If the result is nil, then it was deleted
				if res == nil {
					// Send a delete event for the merged version nof this key
					events = append(events, Event[T]{Old: m, Event: controllers.EventDelete})
					continue
				}
				// There are some versions of this key still in the overall collection
				// send an update with the new merged version
				events = append(events, Event[T]{Old: m, New: res, Event: controllers.EventUpdate})
			}
			f(events)
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

// NestedJoinCollection creates a new collection of collections of T. Duplicate keys across the collections will *not*
// be merged (see NestedJoinWithMergeCollection for that) and access operations (e.g. GetKey and List) will perform a
// best effort stable ordering of the list of elements returned; however, this ordering will not be persistent across
// istiod restarts.
func NestedJoinCollection[T any](collections Collection[Collection[T]], opts ...CollectionOption) Collection[T] {
	return NestedJoinWithMergeCollection(collections, nil, opts...)
}

// NestedJoinWithMergeCollection creates a new collection of collections of T that combines duplicates with the specified merge function.
// The merge function *cannot* assume a stable ordering of the list of elements passed to it. Therefore, access operations (e.g. GetKey and List) will
// will only be deterministic if the merge function is deterministic. The merge function should return nil if no value should be returned.
func NestedJoinWithMergeCollection[T any](collections Collection[Collection[T]], merge func(ts []T) *T, opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NestedJoin[%v]", ptr.TypeName[T]())
	}

	ics := collections.(internalCollection[Collection[T]])
	synced := make(chan struct{})

	j := &nestedjoin[T]{
		collectionName: o.name,
		id:             nextUID(),
		synced:         synced,
		collections:    ics,
		syncer: channelSyncer{
			name:   o.name,
			synced: synced,
		},
		merge: merge,
	}

	reg := collections.RegisterBatch(func(o []Event[Collection[T]]) {
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
				// TODO: What does a collection/informer update even look like? New KubeConfig?
				case controllers.EventUpdate:
					if e.New == nil || e.Old == nil {
						log.Warnf("Event %v either has no New value or no Old value", e.Event)
						continue
					}
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

	go func() {
		// Need to make sure the outer collection is synced first
		if !collections.WaitUntilSynced(o.stop) || !reg.WaitUntilSynced(o.stop) {
			return
		}
		for _, c := range collections.List() {
			if !c.WaitUntilSynced(o.stop) {
				return
			}
		}
		close(synced)
		log.Infof("%v synced", o.name)
	}()

	return j
}
