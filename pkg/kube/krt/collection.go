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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// manyCollection builds a mapping from I->O.
// This can be built from transformation functions of I->*O or I->[]O; both are implemented by this same struct.
type manyCollection[I, O any] struct {
	// parent is the input collection we are building off of.
	parent Collection[I]

	// log is a logger for the collection, with additional labels already added to identify it.
	log *istiolog.Scope

	// mu protects all items grouped below
	mu              sync.Mutex
	collectionState multiIndex[I, O]
	// collectionDependencies specifies the set of collections we depend on from within the transformation functions (via Fetch).
	// These are keyed by the internal hash() function on collections.
	// Note this does not include `parent`, which is the *primary* dependency declared outside of transformation functions.
	collectionDependencies sets.String
	// Stores a map of I -> Things depending on it
	objectRelations map[Key[I]]dependencies

	handlersMu sync.RWMutex
	// eventHandlers is a list of event handlers registered for the collection. On any changes, each will be notified.
	eventHandlers []func(o []Event[O])

	handle TransformationMulti[I, O]
}

// multiIndex stores input and output objects.
// Each input and output can be looked up by its key.
// Additionally, a mapping of input key -> output keys stores the transformation.
type multiIndex[I, O any] struct {
	outputs  map[Key[O]]O
	inputs   map[Key[I]]I
	mappings map[Key[I]]sets.Set[Key[O]]
}

func (h *manyCollection[I, O]) Dump() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log.Errorf(">>> BEGIN DUMP")
	for k, v := range h.objectRelations {
		for kk, vv := range v.dependencies {
			h.log.Errorf("Dependencies for: %v: %v -> %v (%v)", k, kk, vv.key, vv.filter)
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

// onUpdate takes a list of I's that changed and reruns the handler over them.
func (h *manyCollection[I, O]) onUpdate(items []Event[any]) {
	var events []Event[O]
	for _, a := range items {
		i := a.Latest().(I)

		iKey := GetKey(i)
		log := h.log.WithLabels("key", iKey)

		h.mu.Lock()
		// Find dependencies for this input; insert empty set if we don't have any
		d, f := h.objectRelations[iKey]
		if !f {
			d = dependencies{
				dependencies: map[depKey]dependency{},
				finalized:    true, // TODO: set this to true at some point
			}
			h.objectRelations[iKey] = d
		}
		// Give them a context for this specific input
		if a.Event == controllers.EventDelete {
			for oKey := range h.collectionState.mappings[iKey] {
				oldRes, f := h.collectionState.outputs[oKey]
				if !f {
					// Typically happens when O has multiple parents
					log.WithLabels("okey", oKey).Errorf("BUG, inconsistent")
					continue
				}
				e := Event[O]{
					Event: controllers.EventDelete,
					Old:   &oldRes,
				}
				events = append(events, e)
				delete(h.collectionState.outputs, oKey)
				log.WithLabels("res", oKey).Debugf("handled delete")
			}
			// TODO: we need to clean up h.collectionState.objects somehow. We don't know if we have exclusive
			// mapping from I -> O I think
			delete(h.collectionState.mappings, iKey)
			delete(h.collectionState.inputs, iKey)
			h.mu.Unlock()
		} else {
			h.mu.Unlock()
			ctx := &indexedManyCollection[I, O]{h, d}
			// Handler shouldn't be called with lock
			results := slices.GroupUnique(h.handle(ctx, i), GetKey[O])
			h.mu.Lock()
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

				log.WithLabels("res", key, "type", e.Event).Debugf("handled")
				events = append(events, e)
			}
			h.mu.Unlock()
		}
	}
	if len(events) == 0 {
		return
	}
	h.handlersMu.RLock()
	handlers := slices.Clone(h.eventHandlers)
	h.handlersMu.RUnlock()

	h.log.WithLabels("events", len(events), "handlers", len(handlers)).Debugf("calling handlers")
	for _, handler := range handlers {
		handler(events)
	}
}

// NewCollection transforms a Collection[I] to a Collection[O] by applying the provided transformation function.
// This applies for one-to-one relationships between I and O.
// For zero-to-one, use NewSingleton. For one-to-many, use NewManyCollection.
func NewCollection[I, O any](c Collection[I], hf TransformationSingle[I, O]) Collection[O] {
	// For implementation simplicity, represent TransformationSingle as a TransformationMulti so we can share an implementation.
	hm := func(ctx HandlerContext, i I) []O {
		res := hf(ctx, i)
		if res == nil {
			return nil
		}
		return []O{*res}
	}
	return newManyCollection[I, O](c, hm, fmt.Sprintf("Collection[%v,%v]", ptr.TypeName[I](), ptr.TypeName[O]()))
}

// NewManyCollection transforms a Collection[I] to a Collection[O] by applying the provided transformation function.
// This applies for one-to-many relationships between I and O.
// For zero-to-one, use NewSingleton. For one-to-one, use NewCollection.
func NewManyCollection[I, O any](c Collection[I], hf TransformationMulti[I, O]) Collection[O] {
	return newManyCollection[I, O](c, hf, fmt.Sprintf("ManyCollection[%v,%v]", ptr.TypeName[I](), ptr.TypeName[O]()))
}

func newManyCollection[I, O any](c Collection[I], hf TransformationMulti[I, O], name string) Collection[O] {

	h := &manyCollection[I, O]{
		handle:                 hf,
		log:                    log.WithLabels("owner", name),
		parent:                 c,
		collectionDependencies: sets.New[string](),
		objectRelations:        map[Key[I]]dependencies{},
		collectionState: multiIndex[I, O]{
			inputs:   map[Key[I]]I{},
			outputs:  map[Key[O]]O{},
			mappings: map[Key[I]]sets.Set[Key[O]]{},
		},
	}
	// TODO: wait for dependencies to be ready
	// Build up the initial state
	h.onUpdate(slices.Map(c.List(metav1.NamespaceAll), func(t I) Event[any] {
		return Event[any]{
			New:   ptr.Of(any(t)),
			Event: controllers.EventAdd,
		}
	}))
	// Setup primary manyCollection. On any change, trigger only that one
	c.RegisterBatch(func(events []Event[I]) {
		if log.DebugEnabled() {
			log := h.log.WithLabels("dep", "primary")
			log.WithLabels("batch", len(events)).Debugf("got event")
		}
		h.onUpdate(slices.Map(events, castEvent[I, any]))
	})
	return h
}

// Handler is called when a dependency changes. We will take as inputs the item that changed.
// Then we find all of our own values (I) that changed and onUpdate() them
func (h *manyCollection[I, O]) onDependencyEvent(events []Event[any]) {
	h.mu.Lock()
	// Got an event. Now we need to find out who depends on it..
	ks := sets.Set[Key[I]]{}
	// Check old and new
	for _, ev := range events {
		// We have a possibly dependant object changed. For each input object, see if it depends on the object.
		// This can be by name or the entire type.
		for i, v := range h.objectRelations {
			log := h.log.WithLabels("item", i)
			for _, item := range ev.Items() {
				if HasName(item) {
					named := depKey{
						name:  GetName(item),
						dtype: GetTypeOf(item),
					}
					if d, f := v.dependencies[named]; f {
						match := d.filter.Matches(item)
						log.WithLabels("match", match).Debugf("event for %v", named)
						if match {
							ks.Insert(i)
							break
						}
					}
				}
				unnamed := depKey{
					dtype: GetTypeOf(item),
				}
				if d, f := v.dependencies[unnamed]; f {
					match := d.filter.Matches(item)
					log.WithLabels("match", match).Debugf("dependency change collection %v", unnamed.dtype)
					if match {
						ks.Insert(i)
						break
					}
				}
			}
		}
	}
	h.log.Debugf("manyCollection event size %v, trigger %v dependencies", len(events), len(ks))
	toRun := make([]Event[any], 0, len(ks))
	for i := range ks {
		ii := h.parent.GetKey(i)
		if ii == nil {
			h.log.Debugf("parent deletion %v", i)
			for oKey := range h.collectionState.mappings[i] {
				_, f := h.collectionState.outputs[oKey]
				if !f {
					// Typically happens when O has multiple parents
					log.WithLabels("ikey", i, "okey", oKey).Errorf("BUG, inconsistent")
					continue
				}
				e := Event[any]{
					Event: controllers.EventDelete,
					Old:   ptr.Of(any(h.collectionState.inputs[i])),
				}
				toRun = append(toRun, e)
			}
		} else {
			toRun = append(toRun, Event[any]{
				Event: controllers.EventUpdate,
				// TODO: is Update without old legal?
				New: ptr.Of(any(*ii)),
				// Old: Ptr(any(*ev.Old)),
			})
		}
	}
	h.mu.Unlock()
	h.onUpdate(toRun)
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
		slices.SortBy(res, GetKey[O])
	} else {
		// TODO: implement properly using collectionState.namespace
		res = Filter(maps.Values(h.collectionState.outputs), func(o O) bool {
			return GetNamespace(o) == namespace
		})
		slices.SortBy(res, GetKey[O])
		return
	}
	return
}

func (h *manyCollection[I, O]) Register(f func(o Event[O])) {
	batchedRegister[O](h, f)
}

func (h *manyCollection[I, O]) RegisterBatch(f func(o []Event[O])) {
	h.handlersMu.Lock()
	defer h.handlersMu.Unlock()
	// TODO: locking here is probably not reliable to avoid duplicate events
	h.eventHandlers = append(h.eventHandlers, f)
	// Send all existing objects through handler
	objs := slices.Map(h.List(metav1.NamespaceAll), func(t O) Event[O] {
		return Event[O]{
			New:   ptr.Of(t),
			Event: controllers.EventAdd,
		}
	})
	if len(objs) > 0 {
		f(objs)
	}
}

type indexedManyCollection[I, O any] struct {
	h *manyCollection[I, O]
	d dependencies
}

// registerDependency creates a
func (i *indexedManyCollection[I, O]) registerDependency(d dependency) bool {
	i.h.mu.Lock()
	_, exists := i.d.dependencies[d.key]
	if exists && !i.d.finalized {
		// TODO: make manyCollection handle this and add it back
		// panic(fmt.Sprintf("dependency already registered, %+v", d.key))
	}
	if !exists && i.d.finalized {
		// TODO: make manyCollection handle this and add it back
		// panic(fmt.Sprintf("dependency registered after initialization, %+v", d.key))
	}
	i.d.dependencies[d.key] = d
	c := d.collection
	if !i.h.collectionDependencies.InsertContains(c.hash()) {
		i.h.log.Debugf("register manyCollection %T", c)
		i.h.mu.Unlock()
		c.register(i.h.onDependencyEvent)
	} else {
		i.h.mu.Unlock()
	}

	return i.d.finalized
}

func (i *indexedManyCollection[I, O]) _internalHandler() {
}
