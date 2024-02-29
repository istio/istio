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
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// manyCollection builds a mapping from I->O.
// This can be built from transformation functions of I->*O or I->[]O; both are implemented by this same struct.
// Locking used here is somewhat complex. We use two locks, mu and recomputeMu.
//   - mu is responsible for locking the actual data we are storing. List()/Get() calls will lock this.
//   - recomputeMu is responsible for ensuring there is mutually exclusive access to recomputation. Typically, in a controller
//     pattern this would be accomplished by a queue. However, these add operational and performance overhead that is not required here.
//     Instead, we ensure at most one goroutine is recomputing things at a time.
//     This avoids two dependency updates happening concurrently and writing events out of order.
type manyCollection[I, O any] struct {
	// collectionName provides the collectionName for this collection.
	collectionName string
	// parent is the input collection we are building off of.
	parent Collection[I]

	// log is a logger for the collection, with additional labels already added to identify it.
	log *istiolog.Scope

	// recomputeMu blocks a recomputation of I->O.
	recomputeMu sync.Mutex

	// mu protects all items grouped below.
	// This is acquired for reads and writes of data.
	// This can be acquired with recomputeMu held, but only with strict ordering (mu inside recomputeMu)
	mu              sync.Mutex
	collectionState multiIndex[I, O]
	// collectionDependencies specifies the set of collections we depend on from within the transformation functions (via Fetch).
	// These are keyed by the internal hash() function on collections.
	// Note this does not include `parent`, which is the *primary* dependency declared outside of transformation functions.
	collectionDependencies sets.Set[untypedCollection]
	// Stores a map of I -> secondary dependencies (added via Fetch)
	objectDependencies map[Key[I]][]dependency

	// eventHandlers is a list of event handlers registered for the collection. On any changes, each will be notified.
	eventHandlers *handlers[O]

	transformation TransformationMulti[I, O]

	// augmentation allows transforming an object into another for usage throughout the library. See WithObjectAugmentation.
	augmentation func(a any) any
	synced       chan struct{}
	stop         <-chan struct{}
}

var _ internalCollection[any] = &manyCollection[any, any]{}

type handlers[O any] struct {
	mu   sync.RWMutex
	h    []func(o []Event[O])
	init bool
}

func (o *handlers[O]) MarkInitialized() []func(o []Event[O]) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.init = true
	return slices.Clone(o.h)
}

func (o *handlers[O]) Insert(f func(o []Event[O])) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.h = append(o.h, f)
	return !o.init
}

func (o *handlers[O]) Get() []func(o []Event[O]) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return slices.Clone(o.h)
}

type untypedCollection = any

// multiIndex stores input and output objects.
// Each input and output can be looked up by its key.
// Additionally, a mapping of input key -> output keys stores the transformation.
type multiIndex[I, O any] struct {
	outputs  map[Key[O]]O
	inputs   map[Key[I]]I
	mappings map[Key[I]]sets.Set[Key[O]]
}

func (h *manyCollection[I, O]) Synced() Syncer {
	return channelSyncer{
		name:   h.collectionName,
		synced: h.synced,
	}
}

// nolint: unused // (not true, its to implement an interface)
func (h *manyCollection[I, O]) dump() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log.Errorf(">>> BEGIN DUMP")
	for k, deps := range h.objectDependencies {
		for _, dep := range deps {
			h.log.Errorf("Dependencies for: %v: %v (%v)", k, dep.collection.name, dep.filter)
		}
	}
	for i, os := range h.collectionState.mappings {
		h.log.Errorf("Input %v -> %v", i, os.UnsortedList())
	}
	for os, o := range h.collectionState.outputs {
		h.log.Errorf("Output %v -> %v", os, o)
	}
	h.log.Errorf("<<< END DUMP")
}

// nolint: unused // (not true, its to implement an interface)
func (h *manyCollection[I, O]) augment(a any) any {
	if h.augmentation != nil {
		return h.augmentation(a)
	}
	return a
}

// onPrimaryInputEvent takes a list of I's that changed and reruns the handler over them.
// This is called either when I directly changes, or if a secondary dependency changed. In this case, we compute which I's depended
// on the secondary dependency, and call onPrimaryInputEvent with them
func (h *manyCollection[I, O]) onPrimaryInputEvent(items []Event[I]) {
	h.recomputeMu.Lock()
	defer h.recomputeMu.Unlock()
	// Between the events being enqueued and now, the input may have changed. Update with latest info.
	// Note we now have the recomputeMu so this is safe; any futures calls will do the same so always have up-to-date information.
	for idx, ev := range items {
		iKey := GetKey(ev.Latest())
		iObj := h.parent.GetKey(iKey)
		if iObj == nil {
			ev.Event = controllers.EventDelete
			if ev.Old == nil {
				// This was an add, now its a Delete. Make sure we don't have Old and New nil, which we claim to be illegal
				ev.Old = ev.New
			}
			ev.New = nil
		} else {
			ev.New = iObj
		}
		items[idx] = ev
	}
	h.onPrimaryInputEventLocked(items)
}

// onPrimaryInputEventLocked takes a list of I's that changed and reruns the handler over them.
// This should be called with recomputeMu acquired.
func (h *manyCollection[I, O]) onPrimaryInputEventLocked(items []Event[I]) {
	var events []Event[O]
	recomputedResults := make([]map[Key[O]]O, len(items))
	for idx, a := range items {
		if a.Event == controllers.EventDelete {
			// handled below, with full lock...
			continue
		}
		i := a.Latest()
		iKey := GetKey(i)

		ctx := &collectionDependencyTracker[I, O]{h, nil, iKey}
		results := slices.GroupUnique(h.transformation(ctx, i), GetKey[O])
		recomputedResults[idx] = results
		// Update the I -> Dependency mapping
		h.objectDependencies[iKey] = ctx.d
	}

	// Now acquire the full lock. Note we still have recomputeMu held!
	h.mu.Lock()
	for idx, a := range items {
		i := a.Latest()
		iKey := GetKey(i)
		if a.Event == controllers.EventDelete {
			for oKey := range h.collectionState.mappings[iKey] {
				oldRes, f := h.collectionState.outputs[oKey]
				if !f {
					h.log.WithLabels("oKey", oKey).Errorf("invalid event, deletion of non-existent object")
					continue
				}
				e := Event[O]{
					Event: controllers.EventDelete,
					Old:   &oldRes,
				}
				events = append(events, e)
				delete(h.collectionState.outputs, oKey)
				if h.log.DebugEnabled() {
					h.log.WithLabels("res", oKey).Debugf("handled delete")
				}
			}
			delete(h.collectionState.mappings, iKey)
			delete(h.collectionState.inputs, iKey)
			delete(h.objectDependencies, iKey)
		} else {
			results := recomputedResults[idx]
			newKeys := sets.New(maps.Keys(results)...)
			oldKeys := h.collectionState.mappings[iKey]
			h.collectionState.mappings[iKey] = newKeys
			h.collectionState.inputs[iKey] = i
			allKeys := newKeys.Copy().Merge(oldKeys)
			// We have now built up a set of I -> []O
			// and found the previous I -> []O mapping
			for key := range allKeys {
				// Find new O object
				newRes, newExists := results[key]
				// Find the old O object
				oldRes, oldExists := h.collectionState.outputs[key]
				e := Event[O]{}
				if newExists && oldExists {
					if equal(newRes, oldRes) {
						// NOP change, skip
						continue
					}
					e.Event = controllers.EventUpdate
					e.New = &newRes
					e.Old = &oldRes
					h.collectionState.outputs[key] = newRes
				} else if newExists {
					e.Event = controllers.EventAdd
					e.New = &newRes
					h.collectionState.outputs[key] = newRes
				} else {
					e.Event = controllers.EventDelete
					e.Old = &oldRes
					delete(h.collectionState.outputs, key)
				}

				if h.log.DebugEnabled() {
					h.log.WithLabels("res", key, "type", e.Event).Debugf("handled")
				}
				events = append(events, e)
			}
		}
	}
	h.mu.Unlock()

	// Short circuit if we have nothing to do
	if len(events) == 0 {
		return
	}
	handlers := h.eventHandlers.Get()

	if h.log.DebugEnabled() {
		h.log.WithLabels("events", len(events), "handlers", len(handlers)).Debugf("calling handlers")
	}
	for _, handler := range handlers {
		handler(slices.Clone(events))
	}
}

// WithName allows explicitly naming a controller. This is a best practice to make debugging easier.
// If not set, a default name is picked.
func WithName(name string) CollectionOption {
	return func(c *collectionOptions) {
		c.name = name
	}
}

// WithObjectAugmentation allows transforming an object into another for usage throughout the library.
// Currently this applies to things like Name, Namespace, Labels, LabelSelector, etc. Equals is not currently supported,
// but likely in the future.
// The intended usage is to add support for these fields to collections of types that do not implement the appropriate interfaces.
// The conversion function can convert to a embedded struct with extra methods added:
//
//	type Wrapper struct { Object }
//	func (w Wrapper) ResourceName() string { return ... }
//	WithObjectAugmentation(func(o any) any { return Wrapper{o.(Object)} })
func WithObjectAugmentation(fn func(o any) any) CollectionOption {
	return func(c *collectionOptions) {
		c.augmentation = fn
	}
}

// WithStop sets a custom stop channel so a collection can be terminated when the channel is closed
func WithStop(stop <-chan struct{}) CollectionOption {
	return func(c *collectionOptions) {
		c.stop = stop
	}
}

// NewCollection transforms a Collection[I] to a Collection[O] by applying the provided transformation function.
// This applies for one-to-one relationships between I and O.
// For zero-to-one, use NewSingleton. For one-to-many, use NewManyCollection.
func NewCollection[I, O any](c Collection[I], hf TransformationSingle[I, O], opts ...CollectionOption) Collection[O] {
	// For implementation simplicity, represent TransformationSingle as a TransformationMulti so we can share an implementation.
	hm := func(ctx HandlerContext, i I) []O {
		res := hf(ctx, i)
		if res == nil {
			return nil
		}
		return []O{*res}
	}
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Collection[%v,%v]", ptr.TypeName[I](), ptr.TypeName[O]())
	}
	return newManyCollection[I, O](c, hm, o)
}

// NewManyCollection transforms a Collection[I] to a Collection[O] by applying the provided transformation function.
// This applies for one-to-many relationships between I and O.
// For zero-to-one, use NewSingleton. For one-to-one, use NewCollection.
func NewManyCollection[I, O any](c Collection[I], hf TransformationMulti[I, O], opts ...CollectionOption) Collection[O] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("ManyCollection[%v,%v]", ptr.TypeName[I](), ptr.TypeName[O]())
	}
	return newManyCollection[I, O](c, hf, o)
}

func newManyCollection[I, O any](cc Collection[I], hf TransformationMulti[I, O], opts collectionOptions) Collection[O] {
	c := cc.(internalCollection[I])
	h := &manyCollection[I, O]{
		transformation:         hf,
		collectionName:         opts.name,
		log:                    log.WithLabels("owner", opts.name),
		parent:                 c,
		collectionDependencies: sets.New[any](),
		objectDependencies:     map[Key[I]][]dependency{},
		collectionState: multiIndex[I, O]{
			inputs:   map[Key[I]]I{},
			outputs:  map[Key[O]]O{},
			mappings: map[Key[I]]sets.Set[Key[O]]{},
		},
		eventHandlers: &handlers[O]{},
		augmentation:  opts.augmentation,
		synced:        make(chan struct{}),
		stop:          opts.stop,
	}
	go func() {
		// Wait for primary dependency to be ready
		if !c.Synced().WaitUntilSynced(h.stop) {
			return
		}
		// Now, register our handler. This will call Add() for the initial state
		h.eventHandlers.MarkInitialized()
		handlerReg := c.RegisterBatch(func(events []Event[I]) {
			if log.DebugEnabled() {
				h.log.WithLabels("dep", "primary", "batch", len(events)).
					Debugf("got event")
			}
			h.onPrimaryInputEvent(events)
		}, true)
		if !handlerReg.WaitUntilSynced(h.stop) {
			return
		}
		close(h.synced)
		h.log.Infof("%v synced", h.name())
	}()
	return h
}

// Handler is called when a dependency changes. We will take as inputs the item that changed.
// Then we find all of our own values (I) that changed and onPrimaryInputEvent() them
func (h *manyCollection[I, O]) onSecondaryDependencyEvent(sourceCollection any, events []Event[any]) {
	h.recomputeMu.Lock()
	defer h.recomputeMu.Unlock()
	// A secondary dependency changed...
	// Got an event. Now we need to find out who depends on it..
	changedInputKeys := sets.Set[Key[I]]{}
	// Check old and new
	for _, ev := range events {
		// We have a possibly dependant object changed. For each input object, see if it depends on the object.
		// This can be by name or the entire type.
		// objectRelations stores each input key to dependency specification.
		for iKey, dependencies := range h.objectDependencies {
			if changed := h.objectChanged(iKey, dependencies, sourceCollection, ev); changed {
				changedInputKeys.Insert(iKey)
			}
		}
	}
	h.log.Debugf("event size %v, impacts %v objects", len(events), len(changedInputKeys))

	toRun := make([]Event[I], 0, len(changedInputKeys))
	// Now we have the set of input keys that changed. We need to recompute all of these.
	// While we could just do that manually, to re-use code, we will convert these into Event[I] and use the same logic as
	// we would if the input itself changed.
	for i := range changedInputKeys {
		iObj := h.parent.GetKey(i)
		if iObj == nil {
			// Object no longer found means it has been deleted.
			h.log.Debugf("parent deletion %v", i)
			for oKey := range h.collectionState.mappings[i] {
				_, f := h.collectionState.outputs[oKey]
				if !f {
					// Typically happens when O has multiple parents
					log.WithLabels("iKey", i, "oKey", oKey).Errorf("BUG, inconsistent")
					continue
				}
				e := Event[I]{
					Event: controllers.EventDelete,
					Old:   ptr.Of(h.collectionState.inputs[i]),
				}
				toRun = append(toRun, e)
			}
		} else {
			// Typically an EventUpdate should have Old and New. We only have New here.
			// In practice, this is an internal surface only so we just make sure onPrimaryInputEvent handles this.
			toRun = append(toRun, Event[I]{
				Event: controllers.EventUpdate,
				New:   iObj,
			})
		}
	}
	h.onPrimaryInputEventLocked(toRun)
}

func (h *manyCollection[I, O]) objectChanged(iKey Key[I], dependencies []dependency, sourceCollection any, ev Event[any]) bool {
	for _, dep := range dependencies {
		if dep.collection.original != sourceCollection {
			continue
		}
		// For each input, we will check if it depends on this event.
		// We use Items() to check both the old and new object; we will recompute if either matched
		for _, item := range ev.Items() {
			match := dep.filter.Matches(item)
			if h.log.DebugEnabled() {
				h.log.WithLabels("item", iKey, "match", match).Debugf("dependency change for collection %T", sourceCollection)
			}
			if match {
				// Its a match! Return now. We don't need to check all dependencies, since we just need to find if any of them changed
				return true
			}
		}
	}
	return false
}

func (h *manyCollection[I, O]) _internalHandler() {
}

func (h *manyCollection[I, O]) GetKey(k Key[O]) (res *O) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rf, f := h.collectionState.outputs[k]
	if f {
		return &rf
	}
	return nil
}

func (h *manyCollection[I, O]) List(namespace string) (res []O) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if namespace == "" {
		res = maps.Values(h.collectionState.outputs)
	} else {
		// Future improvement: shard outputs by namespace so we can query more efficiently
		for _, v := range h.collectionState.outputs {
			if getNamespace(v) == namespace {
				res = append(res, v)
			}
		}
		return
	}
	return
}

func (h *manyCollection[I, O]) Register(f func(o Event[O])) Syncer {
	return registerHandlerAsBatched[O](h, f)
}

func (h *manyCollection[I, O]) RegisterBatch(f func(o []Event[O]), runExistingState bool) Syncer {
	if !h.eventHandlers.Insert(f) && runExistingState {
		// Already started. Pause everything, and run through the handler.
		h.recomputeMu.Lock()
		defer h.recomputeMu.Unlock()
		h.mu.Lock()
		events := make([]Event[O], 0, len(h.collectionState.outputs))
		for _, o := range h.collectionState.outputs {
			o := o
			events = append(events, Event[O]{
				New:   &o,
				Event: controllers.EventAdd,
			})
		}
		h.mu.Unlock()
		if len(events) > 0 {
			f(events)
		}
		// We handle events in sequence here, so its always synced at this point/
		return alwaysSynced{}
	}
	return channelSyncer{
		name:   h.collectionName + " handler",
		synced: h.synced,
	}
}

func (h *manyCollection[I, O]) name() string {
	return h.collectionName
}

// collectionDependencyTracker tracks, for a single transformation call, all dependencies registered.
// These are inserted on each call to Fetch().
// Once the transformation function is complete, the set of dependencies for the provided input will be replaced
// with the set accumulated here.
//
// Note: this is used instead of passing manyCollection to the transformation function directly because we want to build up some state
// for a given transformation call at once, then apply it in a single transaction to the manyCollection.
type collectionDependencyTracker[I, O any] struct {
	*manyCollection[I, O]
	d   []dependency
	key Key[I]
}

func (i *collectionDependencyTracker[I, O]) name() string {
	return fmt.Sprintf("%s{%s}", i.collectionName, i.key)
}

// registerDependency track a dependency. This is in the context of a specific input I type, as we create a collectionDependencyTracker
// per I.
func (i *collectionDependencyTracker[I, O]) registerDependency(d dependency) {
	i.d = append(i.d, d)

	// For any new collections we depend on, start watching them if its the first time we have watched them.
	if !i.collectionDependencies.InsertContains(d.collection.original) {
		i.log.WithLabels("collection", d.collection.name).Debugf("register new dependency")
		d.collection.synced.WaitUntilSynced(i.stop)
		d.collection.register(func(o []Event[any]) {
			i.onSecondaryDependencyEvent(d.collection.original, o)
		})
	}
}

func (i *collectionDependencyTracker[I, O]) _internalHandler() {
}
