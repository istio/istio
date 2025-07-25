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
	"sync"

	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/util/sets"
)

type collectionLister[T any] interface {
	getCollections() []Collection[T]
}

type mergejoin[T any] struct {
	id             collectionUID
	collectionName string
	collections    collectionLister[T]

	// log is a logger for the collection, with additional labels already added to identify it.
	log *istiolog.Scope
	// internal indexes
	indexes map[string]joinCollectionIndex2[T]
	// eventHandlers is a list of event handlers registered for the collection. On any changes, each will be notified.
	eventHandlers *handlerSet[T]
	// outputs is a cache of the merged results
	outputs map[Key[T]]T
	mu      sync.RWMutex // protects indexes, mergeCache, and eventHandlers

	merge func(ts []T) *T

	synced   chan struct{}
	stop     <-chan struct{}
	queue    queue.Instance
	metadata Metadata

	syncer Syncer
}

type joinCollectionIndex2[T any] struct {
	extract func(o T) []string
	index   map[string]sets.Set[Key[T]]
	parent  *mergejoin[T]
}

func (c joinCollectionIndex2[T]) Lookup(key string) []T {
	c.parent.mu.RLock()
	defer c.parent.mu.RUnlock()
	keys := c.index[key]

	res := make([]T, 0, len(keys))
	for k := range keys {
		v, f := c.parent.outputs[k]
		if !f {
			log.WithLabels("key", k).Errorf("invalid index state, object does not exist")
			continue
		}
		res = append(res, v)
	}
	return res
}

func (c joinCollectionIndex2[T]) delete(o T, oKey Key[T]) {
	oldIndexKeys := c.extract(o)
	for _, oldIndexKey := range oldIndexKeys {
		sets.DeleteCleanupLast(c.index, oldIndexKey, oKey)
	}
}

func (c joinCollectionIndex2[T]) update(ev Event[T], oKey Key[T]) {
	if ev.Old != nil {
		c.delete(*ev.Old, oKey)
	}
	if ev.New != nil {
		newIndexKeys := c.extract(*ev.New)
		for _, newIndexKey := range newIndexKeys {
			sets.InsertOrNew(c.index, newIndexKey, oKey)
		}
	}
}

func (j *mergejoin[T]) Metadata() Metadata {
	return j.metadata
}

// nolint: unused // (not true, its to implement an interface)
func (j *mergejoin[T]) augment(a any) any {
	// not supported in this collection type
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (j *mergejoin[T]) name() string { return j.collectionName }

// nolint: unused // (not true, its to implement an interface)
func (j *mergejoin[T]) uid() collectionUID { return j.id }

func (j *mergejoin[T]) Synced() Syncer {
	return channelSyncer{
		name:   j.collectionName,
		synced: j.synced,
	}
}

// nolint: unused // (not true)
func (j *mergejoin[T]) index(name string, extract func(o T) []string) indexer[T] {
	j.mu.Lock()
	defer j.mu.Unlock()
	if idx, ok := j.indexes[name]; ok {
		return idx
	}

	idx := joinCollectionIndex2[T]{
		extract: extract,
		index:   make(map[string]sets.Set[Key[T]]),
		parent:  j,
	}
	for k, v := range j.outputs {
		idx.update(Event[T]{
			Old:   nil,
			New:   &v,
			Event: controllers.EventAdd,
		}, k)
	}
	j.indexes[name] = idx
	return idx
}

func (j *mergejoin[T]) HasSynced() bool {
	return j.syncer.HasSynced()
}

func (j *mergejoin[T]) WaitUntilSynced(s <-chan struct{}) bool {
	return j.syncer.WaitUntilSynced(s)
}

func (j *mergejoin[T]) List() []T {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return maps.Values(j.outputs)
}

func (j *mergejoin[T]) GetKey(k string) *T {
	j.mu.RLock()
	defer j.mu.RUnlock()

	rf, f := j.outputs[Key[T](k)]
	if f {
		return &rf
	}
	return nil
}

func (j *mergejoin[T]) onSubCollectionEventHandler(o []Event[T]) {
	var events []Event[T]
	j.mu.Lock()
	defer j.mu.Unlock()
	// Between the events being enqueued and now, the input may have changed. Update with latest info.

	items := j.refreshEventsLocked(o)

	for _, ev := range items {
		obj := ev.Latest()
		objKey := getTypedKey(obj)
		if ev.Event == controllers.EventDelete {
			oldRes, f := j.outputs[objKey]
			if !f {
				j.log.WithLabels("iKey", objKey).Errorf("invalid event, deletion of non-existent object")
				continue
			}
			e := Event[T]{
				Event: controllers.EventDelete,
				Old:   &oldRes,
			}
			events = append(events, e)
			delete(j.outputs, objKey)
			for _, index := range j.indexes {
				index.delete(oldRes, objKey)
			}
			if j.log.DebugEnabled() {
				j.log.WithLabels("res", objKey).Debugf("handled delete")
			}
		} else {
			// We can trust these events as authoritative because we checked the state of the collections
			// in refreshEvents.
			newObj := ev.New
			oldObj := ev.Old
			if oldObj != nil && newObj != nil {
				// Update event
				if Equal(ev.New, ev.Old) {
					// NOP change, skip
					continue
				}
				j.outputs[objKey] = *newObj
			} else if newObj != nil {
				// Add event
				j.outputs[objKey] = *newObj
			} else {
				if oldObj == nil && EnableAssertions {
					panic(fmt.Sprintf("newObj and oldObj are both nil; how did we get here? for key %v", objKey))
				}
				j.log.WithLabels("iKey", objKey).Errorf("invalid event, Old has value but New is nil for non Delete event")
				continue
			}
		}

		for _, index := range j.indexes {
			index.update(ev, objKey)
		}

		if j.log.DebugEnabled() {
			j.log.WithLabels("res", objKey, "type", ev.Event).Debugf("handled")
		}
		events = append(events, ev)
	}

	// Short circuit if we have nothing to do
	if len(events) == 0 {
		return
	}
	if j.log.DebugEnabled() {
		j.log.WithLabels("events", len(events)).Debugf("calling handlers")
	}
	j.eventHandlers.Distribute(events, !j.HasSynced())
}

func (j *mergejoin[T]) refreshEventsLocked(items []Event[T]) []Event[T] {
	// Refreshing events is different for merged collections because we need to
	// calculate the merge for the key of each item.
	for idx, ev := range items {
		iKey := getTypedKey(ev.Latest())
		iObj := j.calculateMerged(string(iKey))
		if iObj == nil {
			ev.Event = controllers.EventDelete
			if ev.Old == nil {
				// This was an add, now its a Delete (because state changed between enqueue and now).
				// Make sure we don't have Old and New nil, which we claim to be illegal.
				ev.Old = ev.New
			}
			ev.New = nil
			items[idx] = ev
			continue
		}
		existing, ok := j.outputs[iKey]
		if !ok {
			switch ev.Event {
			case controllers.EventAdd:
				items[idx] = Event[T]{Event: controllers.EventAdd, New: iObj}
			default:
				// Not finding the key in our cache for update or delete events is probably a bug
				msg := fmt.Sprintf("POTENTIAL BUG: key %s not found in cache for event %s", iKey, ev.Event)
				if EnableAssertions {
					panic(msg)
				}
				j.log.Debug(msg)
				// Convert the update into an add since that's what it is to us
				items[idx] = Event[T]{Event: controllers.EventAdd, New: iObj}
			}
			continue
		}

		// We know about this key, so this will always be an update.
		items[idx] = Event[T]{
			Event: controllers.EventUpdate,
			Old:   &existing,
			New:   iObj,
		}
	}

	return items
}

func (j *mergejoin[T]) calculateMerged(k string) *T {
	var found []T
	for _, c := range j.collections.getCollections() {
		if r := c.GetKey(k); r != nil {
			found = append(found, *r)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return j.merge(found)
}

func (j *mergejoin[T]) updateIndexLocked(e Event[T], key Key[T]) {
	switch e.Event {
	case controllers.EventAdd, controllers.EventUpdate:
		for _, index := range j.indexes {
			index.update(e, key)
		}
	case controllers.EventDelete:
		for _, index := range j.indexes {
			index.delete(*e.Old, key)
		}
	}
}
