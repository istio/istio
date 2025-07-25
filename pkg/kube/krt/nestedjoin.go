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
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// Note: there's a goroutine per event handler, so we can't put state relevant
// to any keys in the collection itself (otherwise, different handlers will
// read/mutate the same state and see a different view of the world).
type nestedjoin[T any] struct {
	id                       collectionUID
	collectionName           string
	collections              internalCollection[Collection[T]]
	synced                   <-chan struct{}
	syncer                   Syncer
	collectionChangeHandlers *collectionChangeHandlers[T]
	metadata                 Metadata

	// Keep a stop channel so we can check if nested collections are synced
	stop <-chan struct{}
}

type collectionChangeHandlers[T any] struct {
	handlers []func(collectionChangeEvent[T])
	sync.RWMutex
}

func (j *nestedjoin[T]) Metadata() Metadata {
	return j.metadata
}

func (j *nestedjoin[T]) List() []T {
	var res []T
	var found sets.String
	first := true
	for _, c := range j.collections.List() {
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

func (j *nestedjoin[T]) GetKey(k string) *T {
	for _, c := range j.collections.List() {
		if r := c.GetKey(k); r != nil {
			return r
		}
	}
	return nil
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin[T]) index(name string, extract func(o T) []string) indexer[T] {
	ji := &dynamicJoinIndexer[T]{indexers: make(map[collectionUID]indexer[T])}
	for _, c := range j.collections.List() {
		ic := c.(internalCollection[T])
		ji.indexers[ic.uid()] = ic.index(name, extract)
	}
	j.registerCollectionChangeHandler(func(e collectionChangeEvent[T]) {
		ji.Lock()
		defer ji.Unlock()
		switch e.eventType {
		case collectionMembershipEventAdd:
			ji.indexers[e.collectionValue.uid()] = e.collectionValue.index(name, extract)
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

func (j *nestedjoin[T]) handleCollectionChangeEvent(
	djhr *dynamicJoinHandlerRegistration,
	e collectionChangeEvent[T],
	handler func(o []Event[T]),
) {
	djhr.Lock()
	defer djhr.Unlock()

	switch e.eventType {
	case collectionMembershipEventAdd:
		log.Infof("NestedJoinCollection: Adding collection %s with uid %d", e.collectionValue.name(), e.collectionValue.uid())
		// We always want to send events for existing state when a new collection is added
		reg := e.collectionValue.RegisterBatch(func(o []Event[T]) {
			djhr.RLock()
			defer djhr.RUnlock()
			if _, ok := djhr.removes[e.collectionValue.uid()]; !ok {
				// The remover has been called; don't handle this event
				return
			}
			handler(o)
		}, true) // Always run existing state
		djhr.removes[e.collectionValue.uid()] = reg.UnregisterHandler
		djhr.syncers[e.collectionValue.uid()] = reg
	case collectionMembershipEventDelete:
		// Unregister the handler for this collection
		remover := djhr.removes[e.collectionValue.uid()]
		syncer := djhr.syncers[e.collectionValue.uid()]
		if remover == nil || syncer == nil {
			return
		}

		// krt schedules a callback to notify the event handler machinery that a collection is synced
		// if a handler is registered before then. If the collection is removed before the callback
		// is called, we can get a panic for trying to send on a closed channel, so wait for the collection
		// to be synced before unregistering the handler.
		if !syncer.WaitUntilSynced(j.stop) {
			log.Warnf("Collection %v (uid %v) was not synced before unregistering", e.collectionValue.name(), e.collectionValue.uid())
			return
		}
		remover()
		delete(djhr.removes, e.collectionValue.uid())
		delete(djhr.syncers, e.collectionValue.uid())

		// Now we must send a final set of remove events for each object in the collection
		oldItems := e.collectionValue.List()
		events := slices.Map(oldItems, func(i T) Event[T] {
			return Event[T]{Old: &i, Event: controllers.EventDelete}
		})
		handler(events)
		return

	case collectionMembershipEventUpdate:
		oldItems := e.oldCollectionValue.List()
		events := slices.Map(oldItems, func(i T) Event[T] {
			return Event[T]{Old: &i, Event: controllers.EventUpdate, New: j.GetKey(GetKey(i))}
		})
		handler(events)
		return
	}
}

func (j *nestedjoin[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
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

	// We register to get notified if a collection within our set of collections is modified
	j.registerCollectionChangeHandler(func(e collectionChangeEvent[T]) {
		j.handleCollectionChangeEvent(djhr, e, f)
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
func (j *nestedjoin[T]) dump() CollectionDump {
	innerCols := j.collections.List()
	dumpsByCollectionUID := make(map[string]InputDump, len(innerCols))
	for _, c := range innerCols {
		if c == nil {
			continue
		}
		ic := c.(internalCollection[T])
		icDump := ic.dump()
		dumpsByCollectionUID[GetKey(ic)] = InputDump{
			Outputs:      maps.Keys(icDump.Outputs),
			Dependencies: append(maps.Keys(icDump.Inputs), icDump.InputCollection),
		}
	}
	return CollectionDump{
		Outputs: eraseMap(slices.GroupUnique(j.List(), getTypedKey)),
		Synced:  j.HasSynced(),
		Inputs:  dumpsByCollectionUID,
	}
}

func (j *nestedjoin[T]) registerCollectionChangeHandler(h func(e collectionChangeEvent[T])) {
	j.collectionChangeHandlers.Lock()
	j.collectionChangeHandlers.handlers = append(j.collectionChangeHandlers.handlers, h)
	j.collectionChangeHandlers.Unlock()
}

// NestedJoinCollection creates a new collection of collections of T. Duplicate keys across the collections will *not*
// be merged (see NestedJoinWithMergeCollection2 for that) and access operations (e.g. GetKey and List) will perform a
// best effort stable ordering of the list of elements returned; however, this ordering will not be persistent across
// istiod restarts.
func NestedJoinCollection[T any](collections Collection[Collection[T]], opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NestedJoin[%v]", ptr.TypeName[T]())
	}

	ics := collections.(internalCollection[Collection[T]])
	synced := make(chan struct{})

	j := &nestedjoin[T]{
		synced:         synced,
		id:             nextUID(),
		collectionName: o.name,

		collections: ics,
		syncer: channelSyncer{
			name:   o.name,
			synced: synced,
		},
		collectionChangeHandlers: &collectionChangeHandlers[T]{
			handlers: make([]func(collectionChangeEvent[T]), 0),
		},
		stop: o.stop,
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	if o.metadata != nil {
		j.metadata = o.metadata
	}

	reg := collections.RegisterBatch(func(o []Event[Collection[T]]) {
		j.collectionChangeHandlers.RLock()
		defer j.collectionChangeHandlers.RUnlock()

		// Each event goes to all handlers first to preserve ordering
		log.Infof("Received %d collection change events for collection %s", len(o), j.collectionName)
		for _, e := range o {
			for _, h := range j.collectionChangeHandlers.handlers {
				switch e.Event {
				case controllers.EventAdd:
					h(collectionChangeEvent[T]{eventType: collectionMembershipEventAdd, collectionValue: any(*e.New).(internalCollection[T])})
				case controllers.EventUpdate:
					// TODO: What does a collection/informer update even look like?
					// I'm assuming it's something like a change in informer filter
					// or something and the uid stays the same.
					h(collectionChangeEvent[T]{
						eventType:          collectionMembershipEventDelete,
						collectionValue:    any(*e.New).(internalCollection[T]),
						oldCollectionValue: any(*e.Old).(internalCollection[T]),
					})
				case controllers.EventDelete:
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
