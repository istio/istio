// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package krt

import (
	"fmt"

	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type nestedjoinmerge[T any] struct {
	*mergejoin[T]
	collections internalCollection[Collection[T]]
	regs        map[collectionUID]HandlerRegistration // registrations for the sub-collections, used to unsubscribe when the collection is deleted
}

var (
	_ internalCollection[any] = &nestedjoinmerge[any]{}
)

func (j *nestedjoinmerge[T]) Register(f func(e Event[T])) HandlerRegistration {
	return registerHandlerAsBatched(j, f)
}

func (j *nestedjoinmerge[T]) RegisterBatch(f func(e []Event[T]), runExistingState bool) HandlerRegistration {
	if !runExistingState {
		// If we don't to run the initial state this is simple, we just register the handler.
		j.mu.Lock()
		defer j.mu.Unlock()
		return j.eventHandlers.Insert(f, j, nil, j.stop)
	}

	// We need to run the initial state, but we don't want to get duplicate events.
	// We should get "ADD initialObject1, ADD initialObjectN, UPDATE someLaterUpdate" without mixing the initial ADDs
	// Create ADDs for the current state of the merge cache
	j.mu.RLock()
	defer j.mu.RUnlock()

	events := make([]Event[T], 0, len(j.outputs))
	for _, o := range j.outputs {
		events = append(events, Event[T]{
			New:   &o,
			Event: controllers.EventAdd,
		})
	}

	// Send out all the initial objects to the handler. We will then unlock the new events so it gets the future updates.
	return j.eventHandlers.Insert(f, j, events, j.stop)
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoinmerge[T]) dump() CollectionDump {
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

func (j *nestedjoinmerge[T]) getCollections() []Collection[T] {
	// This is used by the collection lister to get the collections for this join
	// so it can be used in a nested join.
	return j.collections.List()
}

func NestedJoinWithMergeCollection[T any](collections Collection[Collection[T]], merge func(ts []T) *T, opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NestedJoin[%v]", ptr.TypeName[T]())
	}

	ics := collections.(internalCollection[Collection[T]])
	synced := make(chan struct{})

	j := &nestedjoinmerge[T]{
		mergejoin: &mergejoin[T]{
			id:             nextUID(),
			collectionName: o.name,
			log:            log.WithLabels("owner", o.name),
			outputs:        make(map[Key[T]]T),
			indexes:        make(map[string]joinCollectionIndex2[T]),
			eventHandlers:  newHandlerSet[T](),
			merge:          merge,
			synced:         synced,
			stop:           o.stop,
		},
		collections: ics,
		regs:        make(map[collectionUID]HandlerRegistration),
	}

	j.mergejoin.collections = j
	j.syncer = channelSyncer{
		name:   j.collectionName,
		synced: j.synced,
	}

	maybeRegisterCollectionForDebugging(j, o.debugger)

	// Create our queue. When it syncs (that is, all items that were present when Run() was called), we mark ourselves as synced.
	j.queue = queue.NewWithSync(func() {
		close(j.synced)
		j.log.Infof("%v synced (uid %v)", j.name(), j.uid())
	}, j.collectionName)

	// Subscribe to when collections are added or removed.
	// Don't run existing because we want to ensure the first set
	// of collections passed to us are synced before we mark
	// ourselves as synced. Do this before returning so collections
	// aren't added between now and when runQueue is called.
	subscriptionFunc := func(events []Event[T]) {
		j.queue.Push(func() error {
			j.onSubCollectionEventHandler(events)
			return nil
		})
	}
	reg := j.collections.RegisterBatch(func(o []Event[Collection[T]]) {
		for _, e := range o {
			o := e.Latest()
			switch e.Event {
			case controllers.EventAdd:
				// When a collection is added, subscribe to its events
				reg := o.RegisterBatch(subscriptionFunc, true)
				j.mu.Lock()
				j.regs[o.(internalCollection[T]).uid()] = reg
				j.mu.Unlock()
			case controllers.EventUpdate:
				j.handleCollectionUpdate(e)
			case controllers.EventDelete:
				j.handleCollectionDelete(e)
			}
		}
	}, false)
	initialCollections := j.collections.List()
	// Finally, async wait for the primary to be synced. Once it has, we know it has enqueued the initial state.
	// After this, we can run our queue.
	// The queue will process the initial state and mark ourselves as synced (from the NewWithSync callback)
	go j.runQueue(initialCollections, subscriptionFunc, reg)

	return j
}

func (j *nestedjoinmerge[T]) runQueue(initialCollections []Collection[T], subscriptionFunc func([]Event[T]), reg HandlerRegistration) {
	// Wait for the container of collections to be synced before we start processing events.
	j.mu.Lock()
	if !j.collections.WaitUntilSynced(j.stop) {
		return
	}

	// Now that we've subscribed, process the current set of collections.
	for _, c := range initialCollections {
		// Save these registrations so we can unsubscribe later if the collection is deleted.
		// Ensure each sub-collection is synced before we're marked as synced.
		j.regs[c.(internalCollection[T]).uid()] = c.RegisterBatch(subscriptionFunc, true)
	}

	regs := append([]HandlerRegistration{reg}, maps.Values(j.regs)...)
	j.mu.Unlock()

	syncers := slices.Map(regs, func(r HandlerRegistration) cache.InformerSynced {
		return r.HasSynced
	})

	if !kube.WaitForCacheSync(j.collectionName, j.stop, syncers...) {
		return
	}
	j.queue.Run(j.stop)
}

func (j *nestedjoinmerge[T]) handleCollectionUpdate(e Event[Collection[T]]) {
	// Get all of the elements in the old collection
	oldCollectionValue := *e.Old
	newCollectionValue := *e.New
	// Wait for the new collection to be synced before we process the update.
	if !newCollectionValue.WaitUntilSynced(j.stop) {
		log.Warnf("NestedJoinCollection: Collection %s not synced, skipping update event", newCollectionValue.(internalCollection[T]).uid())
	}
	// Stop the world and update our outputs with new state for everything in the collection.
	j.mu.Lock()
	defer j.mu.Unlock()

	oldItems := oldCollectionValue.List()
	// Convert it to a map for easy lookup
	oldItemsMap := make(map[Key[T]]T, len(oldItems))
	for _, i := range oldItems {
		key := getTypedKey(i)
		oldItemsMap[key] = i
	}
	// Now loop through the new collection and compare it to the old one
	seen := sets.NewWithLength[string](len(oldItems))
	finalEvents := make([]Event[T], 0, len(oldItems))
	for _, i := range newCollectionValue.List() {
		key := getTypedKey(i)
		// If we see it in the old collection, then it's an update
		if oldItem, ok := oldItemsMap[key]; ok {
			seen.Insert(string(key))
			// Don't need to pass i since the new collection is in our list of collections
			// merged is guaranteed to be non-nil since newCollectionValue is a part of
			// j's collection of collections.
			merged := j.calculateMerged(string(key))
			// Guaranteed to be in the outputs map since this was in oldItems
			oldItem = j.outputs[key]
			if Equal(oldItem, *merged) {
				// no-op, the item is unchanged
				continue
			}
			// Update the cache with the new merged version
			j.outputs[key] = *merged
			// Send an update event for the merged version of this key
			finalEvents = append(finalEvents, Event[T]{Old: &oldItem, New: merged, Event: controllers.EventUpdate})
			// Delete it from the old items map since we've seen it
			delete(oldItemsMap, key)
		} else {
			if seen.Contains(string(key)) {
				// This is a duplicate item in the new collection, skip it
				log.Warnf("NestedJoinCollection: Duplicate item %v in updated collection, skipping", key)
				continue
			}
			// This is a new item in the new collection, but it might not be a new item in the overall collection.
			// Recalculate the merged version of this key just to be sure. Again, calculateMerged is guaranteed to be non-nil
			// since newCollectionValue is a part of j's collection of collections.
			merged := j.calculateMerged(string(key))
			j.outputs[key] = *merged
			finalEvents = append(finalEvents, Event[T]{New: merged, Event: controllers.EventAdd})
		}
	}

	// Now loop through the old items map and delete any items whose merged value
	// is nil. Send updates for the items that are still present in the outputs.
	for key, i := range maps.SeqStable(oldItemsMap) {
		existing, ok := j.outputs[key]
		if !ok {
			// This is a bug; the old items map should only contain items that are in the outputs.
			msg := fmt.Sprintf("BUG: Expected to find key %v in outputs during a collection update in %s, but it was not found", key, j.collectionName)
			if EnableAssertions {
				panic(msg)
			} else {
				j.log.Warn(msg)
			}
		}
		// send deletes if the key isn't present at all in our collections
		merged := j.calculateMerged(string(key))
		if merged == nil {
			finalEvents = append(finalEvents, Event[T]{Old: &existing, Event: controllers.EventDelete})
			delete(j.outputs, getTypedKey(i))
			continue
		}

		if Equal(existing, *merged) {
			// no-op, the item is unchanged
			continue
		}
		// If the merged value is not nil, then we have an update event
		j.outputs[key] = *merged
		finalEvents = append(finalEvents, Event[T]{Old: &existing, New: merged, Event: controllers.EventUpdate})
	}

	// Update the indexes
	for _, e := range finalEvents {
		j.updateIndexLocked(e, getTypedKey(e.Latest()))
	}

	// Now send these events to the event handlers
	j.eventHandlers.Distribute(finalEvents, !j.HasSynced())
}

func (j *nestedjoinmerge[T]) handleCollectionDelete(e Event[Collection[T]]) {
	j.mu.Lock()
	defer j.mu.Unlock()
	// Get all of the elements in the old collection
	oldCollectionValue := *e.Old

	cc := e.Latest().(internalCollection[T])
	// Unsubscribe from the collection
	if reg, ok := j.regs[cc.uid()]; ok {
		reg.UnregisterHandler()
		delete(j.regs, cc.uid())
	} else {
		j.log.Warnf("NestedJoinCollection: No registration found for collection %s during delete event", cc.uid())
	}

	// Now we must send a final set of remove events for each object in the collection
	var events []Event[T]

	oldItems := oldCollectionValue.List()

	items := sets.NewWithLength[Key[T]](len(oldItems))
	// First loop through the collection to get the deleted items by their keys
	for _, c := range oldItems {
		key := getTypedKey(c)
		items.Insert(key)
	}

	// Now loop through the keys and compare them to our current list of collections
	// to see if it's actually deleted
	for key := range items {
		keyString := string(key)
		res := j.calculateMerged(keyString)
		// Always update the cache on a collection delete
		oldItem, ok := j.outputs[key]
		var e Event[T]
		// We don't see this in our cache, so this is a real delete
		if res == nil {
			// Send a delete event for the merged version of this key
			// Use the merge of the old items as the old value
			if !ok {
				// This shouldn't happen; log it and fall back to the event's old Item
				j.log.Warnf("NestedJoinCollection: No item found in outputs for key %s during collection delete, sending delete event with event old value", keyString)
				oldItem = *oldCollectionValue.GetKey(keyString)
			}
			delete(j.outputs, key)
			if j.log.DebugEnabled() {
				j.log.WithLabels("res", key).Debugf("handled delete")
			}
			e = Event[T]{Old: &oldItem, Event: controllers.EventDelete}
		} else {
			if !ok {
				// If we don't have the old item, then this is actually an add (something must have changed in the time after enqueue)
				e = Event[T]{New: res, Event: controllers.EventAdd}
			} else {
				// There are some versions of this key still in the overall collection
				// send an update with the new merged version and the old version from
				// the cache
				e = Event[T]{Old: &oldItem, New: res, Event: controllers.EventUpdate}
			}
			j.outputs[key] = *res
		}

		// Update the index
		j.updateIndexLocked(e, key)
		events = append(events, e)
		log.Infof("Merged event %s due to collection delete for key %s in collection %s", e.Event, keyString, j.collectionName)
	}

	// Now send these events to the event handlers
	j.eventHandlers.Distribute(events, !j.HasSynced())
}
