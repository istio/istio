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

var _ internalCollection[any] = &nestedjoinmerge[any]{}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoinmerge[T]) dump() CollectionDump {
	innerCols := j.collections.ListFiltered(nil)
	dumpsByCollectionUID := make(map[string]InputDump, len(innerCols))
	for _, c := range innerCols {
		if c.internalCollection == nil {
			continue
		}
		ic := c.internal()
		icDump := ic.dump()
		dumpsByCollectionUID[GetKey(ic)] = InputDump{
			Outputs:      maps.Keys(icDump.Outputs),
			Dependencies: append(maps.Keys(icDump.Inputs), icDump.InputCollection),
		}
	}
	return CollectionDump{
		Outputs: eraseMap(slices.GroupUnique(j.ListFiltered(nil), getTypedKey)),
		Synced:  j.HasSynced(),
		Inputs:  dumpsByCollectionUID,
	}
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoinmerge[T]) getCollections() []Collection[T] {
	// This is used by the collection lister to get the collections for this join
	// so it can be used in a nested join.
	return j.collections.ListFiltered(nil)
}

func NestedJoinWithMergeCollection[T any](collections Collection[Collection[T]], merge func(ts []T) *T, opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NestedJoinWithMerge[%v]", ptr.TypeName[T]())
	}

	ics := collections.internal()
	synced := make(chan struct{})

	j := &nestedjoinmerge[T]{
		mergejoin: &mergejoin[T]{
			id:             nextUID(),
			collectionName: o.name,
			log:            log.WithLabels("owner", o.name),
			outputs:        make(map[Key[T]]T),
			indexes:        make(map[string]joinCollectionIndex[T]),
			eventHandlers:  newHandlerSet[T](),
			metadata:       o.metadata,
			merge:          merge,
			synced:         synced,
			stop:           o.stop,
			debugger:       o.debugger,
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

	// Async: subscribe to the container, then wait for it (and everything it holds) to be ready before we
	// start processing. The queue will process the initial state and mark ourselves as synced (from the
	// NewWithSync callback).
	go j.runQueue()

	return newCollection[T](j)
}

func (j *nestedjoinmerge[T]) runQueue() {
	defer maybeUnregisterCollectionFromDebugger(j, j.debugger)

	reg := j.collections.RegisterBatch(func(e []Event[Collection[T]]) {
		for _, ev := range e {
			switch ev.Event {
			case controllers.EventAdd:
				j.mu.Lock()
				j.updateSubscriptionLocked(nil, ev.Latest().internal(), true)
				j.mu.Unlock()
			case controllers.EventUpdate:
				j.handleCollectionUpdate(ev)
			case controllers.EventDelete:
				j.handleCollectionDelete(ev)
			}
		}
	}, true)

	// We subscribed to the container collection (reg) and to each sub-collection (j.regs); all of
	// these may outlive us. Once we are stopped, unregister them (and their goroutines) so we don't
	// leak them.
	defer j.unregisterAll(reg)

	// Wait for the initial sub-collections to be registered.
	if !reg.WaitUntilSynced(j.stop) {
		return
	}

	j.mu.RLock()
	syncers := slices.Map(maps.Values(j.regs), func(r HandlerRegistration) cache.InformerSynced {
		return r.HasSynced
	})
	j.mu.RUnlock()

	// wait for all initial sub-collections to be synced before we start processing events.
	if !kube.WaitForCacheSync(j.collectionName, j.stop, syncers...) {
		return
	}
	j.queue.Run(j.stop)
}

func (j *nestedjoinmerge[T]) stopped() bool {
	select {
	case <-j.stop:
		return true
	default:
		return false
	}
}

// updateSubscriptionLocked drops an old subscription if present and adds a new one for the updated collection if present.
//
// runExistingState controls whether the new subscription replays the collection's current contents. Pass
// false only when the caller reads that state itself, after this returns.
//
// must always be called with j.mu held.
func (j *nestedjoinmerge[T]) updateSubscriptionLocked(old, updated internalCollection[T], runExistingState bool) {
	// avoid registering new subscriptions if the collection is stopped.
	if j.stopped() {
		return
	}
	if old != nil && updated != nil && old.uid() == updated.uid() {
		return
	}
	// create new handler before removing the old one, so we don't miss any events.
	if updated != nil {
		j.regs[updated.uid()] = updated.RegisterBatch(func(events []Event[T]) {
			j.queue.Push(func() error {
				j.onSubCollectionEventHandler(events)
				return nil
			})
		}, runExistingState)
	}
	if old != nil {
		if oldReg, found := j.regs[old.uid()]; found {
			oldReg.UnregisterHandler()
			delete(j.regs, old.uid())
		} else {
			j.log.Warnf("NestedJoinWithMergeCollection: No registration found for collection %v", old.uid())
		}
	}
}

// unregisterAll unregisters the container collection subscription along with all currently tracked
// sub-collection subscriptions. It is only called once the collection has stopped, which is what stops
// the container's handler from taking new sub-collection subscriptions afterwards (see
// updateSubscriptionLocked).
func (j *nestedjoinmerge[T]) unregisterAll(containerReg HandlerRegistration) {
	j.mu.Lock()
	defer j.mu.Unlock()
	// Unregister the container subscription first so no further sub-collection subscriptions are added.
	containerReg.UnregisterHandler()
	for uid, reg := range j.regs {
		reg.UnregisterHandler()
		// we need to delete the entry to play nice with handleCollectionDelete
		delete(j.regs, uid)
	}
}

func (j *nestedjoinmerge[T]) handleCollectionUpdate(e Event[Collection[T]]) {
	innerCollection := e.Latest().internal()
	log.Debugf("NestedJoinWithMergeCollection: Collection %s (uid %s) updated, recalculating merged values", innerCollection.name(), innerCollection.uid())
	// Get all of the elements in the old collection
	oldCollectionValue := *e.Old
	newCollectionValue := *e.New
	// Wait for the new collection to be synced before we process the update.
	if !newCollectionValue.WaitUntilSynced(j.stop) {
		log.Warnf("NestedJoinWithMergeCollection: Collection %s not synced, skipping update event", newCollectionValue.uid())
	}
	// Stop the world and update our outputs with new state for everything in the collection.
	j.mu.Lock()
	defer j.mu.Unlock()

	oldItems := oldCollectionValue.List()

	// drop the old collection subscription and add the new one.
	// we intentionally don't run the existing state for the new collection because we are going to recalculate everything anyway.
	// TODO: even though we drop the old subscription after List is called
	// we still might have missed Delete events for items that are not present in the oldItems list.
	j.updateSubscriptionLocked(oldCollectionValue.internal(), newCollectionValue.internal(), false)
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
				log.Warnf("NestedJoinWithMergeCollection: Duplicate item %v in updated collection, skipping", key)
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
				msg += fmt.Sprintf(" in %s(%T)", j.collectionName, j)
				panic(msg)
			}
			j.log.Warn(msg)
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

	// Now we must send a final set of remove events for each object in the collection
	var events []Event[T]

	oldItems := oldCollectionValue.List()
	// Unsubscribe from the collection
	// TODO: even though we drop the old subscription after List is called
	// we still might have missed Delete events for items that are not present in the oldItems list.
	j.updateSubscriptionLocked(e.Latest().internal(), nil, false)

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
				msg := "NestedJoinWithMergeCollection: No item found in outputs for key %s during collection delete, sending delete event with event old value"
				j.log.Warnf(msg, keyString)
				oldItem = *oldCollectionValue.GetKey(keyString)
			}
			delete(j.outputs, key)
			if j.log.DebugEnabled() {
				j.log.WithLabels("res", key).Debugf("handled delete")
			}
			e = Event[T]{Old: &oldItem, Event: controllers.EventDelete}
		} else {
			if !ok {
				// If we don't have the old item, then this is actually an add
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
	}

	// Now send these events to the event handlers
	j.eventHandlers.Distribute(events, !j.HasSynced())
}
