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
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type indexedDependencyType uint8

const (
	unknownIndexType indexedDependencyType = iota
	indexType        indexedDependencyType = iota
	getKeyType       indexedDependencyType = iota
)

var allIndexedDependencyTypes = []indexedDependencyType{indexType, getKeyType}

type dependencyState[I any] struct {
	// collectionDependencies specifies the set of collections we depend on from within the transformation functions (via Fetch).
	// These are keyed by the internal uid() function on collections.
	// Note this does not include `parent`, which is the *primary* dependency declared outside of transformation functions.
	collectionDependencies sets.Set[collectionUID]
	// Stores a map of I -> secondary dependencies (added via Fetch)
	objectDependencies  map[Key[I]][]*dependency
	indexedDependencies map[indexedDependency]sets.Set[Key[I]]
	// indexedDependenciesExtractor stores a map of [collection,fetch type] => an extractor to get change keys.
	// Note that a given collection can have multiple Fetches, but they are limited to those of the different kind.
	// I.e. you can do a `Fetch(c1, FilterIndex()) + Fetch(c1, FilterKey())` but not `Fetch(c1, FilterIndex(idxA)) + Fetch(c1, FilterIndex(idxB))`.
	// Multiple `FetchIndex` within a single transformation must use the same index.
	// This only applies within a single transformation; it is fine to fetch the the same `c1` in any way from different collections.
	indexedDependenciesExtractor map[extractorKey]objectKeyExtractor
}

type extractorKey struct {
	uid collectionUID
	typ indexedDependencyType
}

func (i dependencyState[I]) update(key Key[I], deps []*dependency) {
	// Update the I -> Dependency mapping
	i.objectDependencies[key] = deps
	for _, d := range deps {
		if depKeys, typ, extractor, ok := d.filter.reverseIndexKey(); ok {
			for _, depKey := range depKeys {
				k := indexedDependency{
					id:  d.id,
					key: depKey,
					typ: typ,
				}
				sets.InsertOrNew(i.indexedDependencies, k, key)
				kk := extractorKey{
					uid: d.id,
					typ: typ,
				}

				i.indexedDependenciesExtractor[kk] = extractor
			}
		}
	}
}

func (i dependencyState[I]) delete(key Key[I]) {
	old, f := i.objectDependencies[key]
	if !f {
		return
	}
	delete(i.objectDependencies, key)
	for _, d := range old {
		if depKeys, typ, _, ok := d.filter.reverseIndexKey(); ok {
			for _, depKey := range depKeys {
				k := indexedDependency{
					id:  d.id,
					key: depKey,
					typ: typ,
				}
				sets.DeleteCleanupLast(i.indexedDependencies, k, key)
			}
		}
	}
}

func (i dependencyState[I]) changedInputKeys(sourceCollection collectionUID, events []Event[any]) sets.Set[Key[I]] {
	changedInputKeys := sets.Set[Key[I]]{}
	// Check old and new
	for _, ev := range events {
		// We have a possibly dependant object changed. For each input object, see if it depends on the object.
		// Naively, we can look through every item in this collection and check if it matches the filter. However, this is
		// inefficient, especially when the dependency changes frequently and the collection is large.
		// Where possible, we utilize the reverse-indexing to get the precise list of potentially changed objects.
		foundAny := false
		for _, idxTypes := range allIndexedDependencyTypes {
			ekey := extractorKey{uid: sourceCollection, typ: idxTypes}
			if extractor, f := i.indexedDependenciesExtractor[ekey]; f {
				foundAny = true
				// We have a reverse index
				for _, item := range ev.Items() {
					// Find all the reverse index keys for this object. For each key we will find impacted input objects.
					keys := extractor(item)
					for _, key := range keys {
						for iKey := range i.indexedDependencies[indexedDependency{id: sourceCollection, key: key, typ: idxTypes}] {
							if changedInputKeys.Contains(iKey) {
								// We may have already found this item, skip it
								continue
							}
							dependencies := i.objectDependencies[iKey]
							if changed := objectChanged(dependencies, sourceCollection, ev, true); changed {
								changedInputKeys.Insert(iKey)
							}
						}
					}
				}
			}
		}
		if !foundAny {
			for iKey, dependencies := range i.objectDependencies {
				if changed := objectChanged(dependencies, sourceCollection, ev, false); changed {
					changedInputKeys.Insert(iKey)
				}
			}
		}
	}
	return changedInputKeys
}

func objectChanged(dependencies []*dependency, sourceCollection collectionUID, ev Event[any], preFiltered bool) bool {
	for _, dep := range dependencies {
		id := dep.id
		if id != sourceCollection {
			continue
		}
		// For each input, we will check if it depends on this event.
		// We use Items() to check both the old and new object; we will recompute if either matched
		for _, item := range ev.Items() {
			match := dep.filter.Matches(item, preFiltered)
			if match {
				// Its a match! Return now. We don't need to check all dependencies, since we just need to find if any of them changed
				return true
			}
		}
	}
	return false
}

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
	id             collectionUID
	// parent is the input collection we are building off of.
	parent Collection[I]

	// log is a logger for the collection, with additional labels already added to identify it.
	log *istiolog.Scope
	// This can be acquired with blockNewEvents held, but only with strict ordering (mu inside blockNewEvents)
	// mu protects all items grouped below.
	// This is acquired for reads and writes of data.
	mu              sync.Mutex
	collectionState multiIndex[I, O]
	dependencyState dependencyState[I]

	// internal indexes
	indexes []collectionIndex[I, O]

	// eventHandlers is a list of event handlers registered for the collection. On any changes, each will be notified.
	eventHandlers *handlerSet[O]

	transformation TransformationMulti[I, O]

	// augmentation allows transforming an object into another for usage throughout the library. See WithObjectAugmentation.
	augmentation func(a any) any
	synced       chan struct{}
	stop         <-chan struct{}
	queue        queue.Instance

	// onPrimaryInputEventHandler is a specialized internal handler that runs synchronously when a primary input changes
	onPrimaryInputEventHandler func(o []Event[I])

	syncer Syncer
}

type collectionIndex[I, O any] struct {
	extract func(o O) []string
	index   map[string]sets.Set[Key[O]]
	parent  *manyCollection[I, O]
}

func (c collectionIndex[I, O]) Lookup(key string) []any {
	c.parent.mu.Lock()
	defer c.parent.mu.Unlock()
	keys := c.index[key]
	res := make([]any, 0, len(keys))
	for k := range keys {
		v, f := c.parent.collectionState.outputs[k]
		if !f {
			log.WithLabels("key", k).Errorf("invalid index state, object does not exist")
			continue
		}
		res = append(res, v)
	}
	return res
}

func (c collectionIndex[I, O]) delete(o O, oKey Key[O]) {
	oldIndexKeys := c.extract(o)
	for _, oldIndexKey := range oldIndexKeys {
		sets.DeleteCleanupLast(c.index, oldIndexKey, oKey)
	}
}

func (c collectionIndex[I, O]) update(ev Event[O], oKey Key[O]) {
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

var _ internalCollection[any] = &manyCollection[any, any]{}

type handlers[O any] struct {
	mu   sync.RWMutex
	h    []*singletonHandlerRegistration[O]
	init bool
}

type singletonHandlerRegistration[O any] struct {
	fn func(o []Event[O])
}

func (o *handlers[O]) MarkInitialized() []func(o []Event[O]) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.init = true
	return slices.Map(o.h, func(e *singletonHandlerRegistration[O]) func(o []Event[O]) {
		return e.fn
	})
}

func (o *handlers[O]) Insert(f func(o []Event[O])) *singletonHandlerRegistration[O] {
	o.mu.Lock()
	defer o.mu.Unlock()
	reg := &singletonHandlerRegistration[O]{fn: f}
	o.h = append(o.h, reg)
	return reg
}

func (o *handlers[O]) Delete(toRemove *singletonHandlerRegistration[O]) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.h = slices.FilterInPlace(o.h, func(s *singletonHandlerRegistration[O]) bool {
		return s != toRemove
	})
}

func (o *handlers[O]) Get() []func(o []Event[O]) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return slices.Map(o.h, func(e *singletonHandlerRegistration[O]) func(o []Event[O]) {
		return e.fn
	})
}

// multiIndex stores input and output objects.
// Each input and output can be looked up by its key.
// Additionally, a mapping of input key -> output keys stores the transformation.
type multiIndex[I, O any] struct {
	outputs  map[Key[O]]O
	inputs   map[Key[I]]I
	mappings map[Key[I]]sets.Set[Key[O]]
}

func (h *manyCollection[I, O]) HasSynced() bool {
	return h.syncer.HasSynced()
}

func (h *manyCollection[I, O]) WaitUntilSynced(stop <-chan struct{}) bool {
	return h.syncer.WaitUntilSynced(stop)
}

// nolint: unused // (not true, its to implement an interface)
func (h *manyCollection[I, O]) dump() CollectionDump {
	h.mu.Lock()
	defer h.mu.Unlock()

	inputs := make(map[string]InputDump, len(h.collectionState.inputs))
	for k, v := range h.collectionState.mappings {
		output := make([]string, 0, len(v))
		for vv := range v {
			output = append(output, string(vv))
		}
		slices.Sort(output)
		inputs[string(k)] = InputDump{
			Outputs:      output,
			Dependencies: nil, // filled later
		}
	}
	for k, deps := range h.dependencyState.objectDependencies {
		depss := make([]string, 0, len(deps))
		for _, dep := range deps {
			depss = append(depss, dep.collectionName)
		}
		slices.Sort(depss)
		cur := inputs[string(k)]
		cur.Dependencies = depss
		inputs[string(k)] = cur
	}

	return CollectionDump{
		Outputs:         eraseMap(h.collectionState.outputs),
		Inputs:          inputs,
		InputCollection: h.parent.(internalCollection[I]).name(),
		Synced:          h.HasSynced(),
	}
}

// nolint: unused // (not true, its to implement an interface)
func (h *manyCollection[I, O]) augment(a any) any {
	if h.augmentation != nil {
		return h.augmentation(a)
	}
	return a
}

// nolint: unused // (not true)
func (h *manyCollection[I, O]) index(extract func(o O) []string) kclient.RawIndexer {
	idx := collectionIndex[I, O]{
		extract: extract,
		index:   make(map[string]sets.Set[Key[O]]),
		parent:  h,
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, v := range h.collectionState.outputs {
		idx.update(Event[O]{
			Old:   nil,
			New:   &v,
			Event: controllers.EventAdd,
		}, k)
	}
	h.indexes = append(h.indexes, idx)
	return idx
}

// onPrimaryInputEvent takes a list of I's that changed and reruns the handler over them.
// This is called either when I directly changes, or if a secondary dependency changed. In this case, we compute which I's depended
// on the secondary dependency, and call onPrimaryInputEvent with them
func (h *manyCollection[I, O]) onPrimaryInputEvent(items []Event[I]) {
	// Between the events being enqueued and now, the input may have changed. Update with latest info.
	// Note we now have the `blockNewEvents` lock so this is safe; any futures calls will do the same so always have up-to-date information.
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

	pendingDepStateUpdates := make(map[Key[I]]*collectionDependencyTracker[I, O], len(items))
	for idx, a := range items {
		if a.Event == controllers.EventDelete {
			// handled below, with full lock...
			continue
		}
		i := a.Latest()
		iKey := getTypedKey(i)

		ctx := &collectionDependencyTracker[I, O]{manyCollection: h, key: iKey}
		results := slices.GroupUnique(h.transformation(ctx, i), getTypedKey[O])
		recomputedResults[idx] = results
		// Store new dependency state, to insert in the next loop under the lock
		pendingDepStateUpdates[iKey] = ctx
	}

	// Now acquire the full lock. Note we still have recomputeMu held!
	h.mu.Lock()
	defer h.mu.Unlock()
	for idx, a := range items {
		i := a.Latest()
		iKey := getTypedKey(i)
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
				for _, index := range h.indexes {
					index.delete(oldRes, oKey)
				}
				if h.log.DebugEnabled() {
					h.log.WithLabels("res", oKey).Debugf("handled delete")
				}
			}
			delete(h.collectionState.mappings, iKey)
			delete(h.collectionState.inputs, iKey)
			h.dependencyState.delete(iKey)
		} else {
			ctx := pendingDepStateUpdates[iKey]
			results := recomputedResults[idx]
			if ctx.discardUpdate {
				// Called when the collection explicitly calls DiscardResult() on the context.
				// This is typically used when we want to retain the last-correct state.
				_, alreadyHasAResult := h.collectionState.mappings[iKey]
				nowHasAResult := len(results) > 0
				if alreadyHasAResult || !nowHasAResult {
					h.log.WithLabels("iKey", iKey).Debugf("discarding result")
					continue
				}
				h.log.WithLabels("iKey", iKey).Debugf("would discard result, but it is the first so including it")
			}
			h.dependencyState.update(iKey, ctx.d)
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
					if Equal(newRes, oldRes) {
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

				for _, index := range h.indexes {
					index.update(e, key)
				}

				if h.log.DebugEnabled() {
					h.log.WithLabels("res", key, "type", e.Event).Debugf("handled")
				}
				events = append(events, e)
			}
		}
	}

	// Short circuit if we have nothing to do
	if len(events) == 0 {
		return
	}
	if h.log.DebugEnabled() {
		h.log.WithLabels("events", len(events)).Debugf("calling handlers")
	}
	h.eventHandlers.Distribute(events, !h.HasSynced())
}

// WithJoinUnchecked enables an optimization for join collections, where keys are not deduplicated across collections.
// This option can only be used when joined collections are disjoint: keys overlapping between collections is undefined behavior
func WithJoinUnchecked() CollectionOption {
	return func(c *collectionOptions) {
		c.joinUnchecked = true
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
	return newManyCollection[I, O](c, hm, o, nil)
}

// NewManyCollection transforms a Collection[I] to a Collection[O] by applying the provided transformation function.
// This applies for one-to-many relationships between I and O.
// For zero-to-one, use NewSingleton. For one-to-one, use NewCollection.
func NewManyCollection[I, O any](c Collection[I], hf TransformationMulti[I, O], opts ...CollectionOption) Collection[O] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("ManyCollection[%v,%v]", ptr.TypeName[I](), ptr.TypeName[O]())
	}
	return newManyCollection[I, O](c, hf, o, nil)
}

func newManyCollection[I, O any](
	cc Collection[I],
	hf TransformationMulti[I, O],
	opts collectionOptions,
	onPrimaryInputEventHandler func([]Event[I]),
) Collection[O] {
	c := cc.(internalCollection[I])

	h := &manyCollection[I, O]{
		transformation: hf,
		collectionName: opts.name,
		id:             nextUID(),
		log:            log.WithLabels("owner", opts.name),
		parent:         c,
		dependencyState: dependencyState[I]{
			collectionDependencies:       sets.New[collectionUID](),
			objectDependencies:           map[Key[I]][]*dependency{},
			indexedDependencies:          map[indexedDependency]sets.Set[Key[I]]{},
			indexedDependenciesExtractor: map[extractorKey]func(o any) []string{},
		},
		collectionState: multiIndex[I, O]{
			inputs:   map[Key[I]]I{},
			outputs:  map[Key[O]]O{},
			mappings: map[Key[I]]sets.Set[Key[O]]{},
		},
		eventHandlers:              newHandlerSet[O](),
		augmentation:               opts.augmentation,
		synced:                     make(chan struct{}),
		stop:                       opts.stop,
		onPrimaryInputEventHandler: onPrimaryInputEventHandler,
	}
	h.syncer = channelSyncer{
		name:   h.collectionName,
		synced: h.synced,
	}
	maybeRegisterCollectionForDebugging(h, opts.debugger)

	// Create our queue. When it syncs (that is, all items that were present when Run() was called), we mark ourselves as synced.
	h.queue = queue.NewWithSync(func() {
		close(h.synced)
		h.log.Infof("%v synced", h.name())
	}, h.collectionName)

	// Finally, async wait for the primary to be synced. Once it has, we know it has enqueued the initial state.
	// After this, we can run our queue.
	// The queue will process the initial state and mark ourselves as synced (from the NewWithSync callback)
	go h.runQueue()

	return h
}

func (h *manyCollection[I, O]) runQueue() {
	c := h.parent
	// Wait for primary dependency to be ready
	if !c.WaitUntilSynced(h.stop) {
		return
	}
	// Now register to our primary collection. On any event, we will enqueue the update.
	syncer := c.RegisterBatch(func(o []Event[I]) {
		if h.onPrimaryInputEventHandler != nil {
			h.onPrimaryInputEventHandler(o)
		}
		h.queue.Push(func() error {
			h.onPrimaryInputEvent(o)
			return nil
		})
	}, true)
	// Wait for everything initial state to be enqueued
	if !syncer.WaitUntilSynced(h.stop) {
		return
	}
	h.queue.Run(h.stop)
}

// Handler is called when a dependency changes. We will take as inputs the item that changed.
// Then we find all of our own values (I) that changed and onPrimaryInputEvent() them
func (h *manyCollection[I, O]) onSecondaryDependencyEvent(sourceCollection collectionUID, events []Event[any]) {
	// A secondary dependency changed...
	// Got an event. Now we need to find out who depends on it..
	changedInputKeys := h.dependencyState.changedInputKeys(sourceCollection, events)
	h.log.Debugf("event size %v, impacts %v objects", len(events), changedInputKeys.UnsortedList())

	toRun := make([]Event[I], 0, len(changedInputKeys))
	// Now we have the set of input keys that changed. We need to recompute all of these.
	// While we could just do that manually, to re-use code, we will convert these into Event[I] and use the same logic as
	// we would if the input itself changed.
	for i := range changedInputKeys {
		iObj := h.parent.GetKey(string(i))
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

// nolint: unused // it is used to implement interface
func (h *manyCollection[I, O]) _internalHandler() {
}

func (h *manyCollection[I, O]) GetKey(k string) (res *O) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rf, f := h.collectionState.outputs[Key[O](k)]
	if f {
		return &rf
	}
	return nil
}

func (h *manyCollection[I, O]) List() (res []O) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Values(h.collectionState.outputs)
}

func (h *manyCollection[I, O]) Register(f func(o Event[O])) HandlerRegistration {
	return registerHandlerAsBatched[O](h, f)
}

func (h *manyCollection[I, O]) RegisterBatch(f func(o []Event[O]), runExistingState bool) HandlerRegistration {
	if !runExistingState {
		// If we don't to run the initial state this is simple, we just register the handler.
		return h.eventHandlers.Insert(f, h, nil, h.stop)
	}
	// We need to run the initial state, but we don't want to get duplicate events.
	// We should get "ADD initialObject1, ADD initialObjectN, UPDATE someLaterUpdate" without mixing the initial ADDs
	// To do this we block any new event processing
	// Get initial state
	h.mu.Lock()
	defer h.mu.Unlock()

	events := make([]Event[O], 0, len(h.collectionState.outputs))
	for _, o := range h.collectionState.outputs {
		events = append(events, Event[O]{
			New:   &o,
			Event: controllers.EventAdd,
		})
	}

	// Send out all the initial objects to the handler. We will then unlock the new events so it gets the future updates.
	return h.eventHandlers.Insert(f, h, events, h.stop)
}

func (h *manyCollection[I, O]) name() string {
	return h.collectionName
}

// nolint: unused // (not true, its to implement an interface)
func (h *manyCollection[I, O]) uid() collectionUID {
	return h.id
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
	d             []*dependency
	key           Key[I]
	discardUpdate bool
}

func (i *collectionDependencyTracker[I, O]) name() string {
	return fmt.Sprintf("%s{%s}", i.collectionName, i.key)
}

// registerDependency track a dependency. This is in the context of a specific input I type, as we create a collectionDependencyTracker
// per I.
func (i *collectionDependencyTracker[I, O]) registerDependency(
	d *dependency,
	syncer Syncer,
	register func(f erasedEventHandler) Syncer,
) {
	i.d = append(i.d, d)

	// For any new collections we depend on, start watching them if its the first time we have watched them.
	if !i.dependencyState.collectionDependencies.InsertContains(d.id) {
		i.log.WithLabels("collection", d.collectionName).Debugf("register new dependency")
		syncer.WaitUntilSynced(i.stop)
		register(func(o []Event[any]) {
			i.queue.Push(func() error {
				i.onSecondaryDependencyEvent(d.id, o)
				return nil
			})
		}).WaitUntilSynced(i.stop)
	}
}

func (i *collectionDependencyTracker[I, O]) _internalHandler() {
}

func (i *collectionDependencyTracker[I, O]) DiscardResult() {
	i.discardUpdate = true
}
