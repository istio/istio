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

	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type Index[K comparable, O any] interface {
	Lookup(k K) []O
	AsCollection(opts ...CollectionOption) Collection[IndexObject[K, O]]
	objectHasKey(obj O, k K) bool
	extractKeys(o O) []K
}

type IndexObject[K comparable, O any] struct {
	Key     K
	Objects []O
}

func (i IndexObject[K, O]) ResourceName() string {
	return toString(i.Key)
}

// NewNamespaceIndex is a small helper to index a collection by namespace
func NewNamespaceIndex[O Namespacer](c Collection[O]) Index[string, O] {
	return NewIndex(c, cache.NamespaceIndex, func(o O) []string {
		return []string{o.GetNamespace()}
	})
}

// NewIndex creates a simple index, keyed by key K, over a collection for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
// Different collection implementations may reuse existing indexes with the same name.
// Informer collections will always share the same index, other collections only share indexes if
// they are created on the same collection instance.
func NewIndex[K comparable, O any](
	c Collection[O],
	name string,
	extract func(o O) []K,
) Index[K, O] {
	idx := c.(internalCollection[O]).index(name, func(o O) []string {
		return slices.Map(extract(o), func(e K) string {
			return toString(e)
		})
	})

	return index[K, O]{idx, c, extract}
}

type index[K comparable, O any] struct {
	kclient.RawIndexer
	c       Collection[O]
	extract func(o O) []K
}

func WithIndexCollectionFromString[K any](f func(string) K) CollectionOption {
	return func(c *collectionOptions) {
		c.indexCollectionFromString = func(s string) any {
			return f(s)
		}
	}
}

// AsCollection does a best-effort approximation of turning an index into a Collection. This is intended to be used as a
// primary input with NewCollection or similar transformations.
// This has some limitations that impact usage *outside* of NewCollection:
// * List() is not allowed.
// * Building an index is not allowed
// * Events are not 100% precise; only Add and Delete events are triggered. Updates will be `Add` events.
// The intended use case for this is to do merging within a collection (like a SQL 'group by').
func (i index[K, O]) AsCollection(opts ...CollectionOption) Collection[IndexObject[K, O]] {
	o := buildCollectionOptions(opts...)

	c := indexCollection[K, O]{
		idx:            i,
		id:             nextUID(),
		collectionName: fmt.Sprintf("index/%s", o.name),
		fromKey:        o.indexCollectionFromString,
	}
	if c.fromKey == nil {
		if _, ok := any(ptr.Empty[K]()).(string); !ok {
			// This is a limitation of the way the API is encoded, unfortunately.
			panic("index.AsCollection requires a string key or WithIndexCollectionFromString to be set")
		}
		c.fromKey = func(s string) any {
			return s
		}
	}
	maybeRegisterCollectionForDebugging(c, o.debugger)
	return c
}

// nolint: unused // (not true)
func (i index[K, O]) objectHasKey(obj O, k K) bool {
	for _, got := range i.extract(obj) {
		if got == k {
			return true
		}
	}
	return false
}

// nolint: unused // (not true)
func (i index[K, O]) extractKeys(o O) []K {
	return i.extract(o)
}

// Lookup finds all objects matching a given key
func (i index[K, O]) Lookup(k K) []O {
	if i.RawIndexer == nil {
		return nil
	}
	res := i.RawIndexer.Lookup(toString(k))
	return slices.Map(res, func(e any) O {
		return e.(O)
	})
}

func toString(rk any) string {
	tk, ok := rk.(string)
	if !ok {
		return rk.(fmt.Stringer).String()
	}
	return tk
}

type indexCollection[K comparable, O any] struct {
	idx index[K, O]
	id  collectionUID
	// nolint: unused // (not true, its to implement an interface)
	collectionName string
	fromKey        func(string) any
}

// nolint: unused // (not true, its to implement an interface)
func (i indexCollection[K, O]) name() string {
	return i.collectionName
}

// nolint: unused // (not true, its to implement an interface)
func (i indexCollection[K, O]) uid() collectionUID {
	return i.id
}

// nolint: unused // (not true, its to implement an interface)
func (i indexCollection[K, O]) dump() CollectionDump {
	return CollectionDump{
		Outputs:         i.dumpOutput(),
		InputCollection: i.idx.c.(internalCollection[O]).name(),
		Synced:          i.HasSynced(),
	}
}

// nolint: unused // (not true, its to implement an interface)
func (i indexCollection[K, O]) augment(a any) any {
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (i indexCollection[K, O]) index(name string, extract func(o IndexObject[K, O]) []string) kclient.RawIndexer {
	panic("an index cannot be indexed")
}

func (i indexCollection[K, O]) GetKey(k string) *IndexObject[K, O] {
	tk := i.fromKey(k).(K)
	objs := i.idx.Lookup(tk)
	return &IndexObject[K, O]{
		Key:     tk,
		Objects: objs,
	}
}

func (i indexCollection[K, O]) List() []IndexObject[K, O] {
	panic("an index collection cannot be listed")
}

// dumpOutput dumps the current state. This has no synchronization, so it's not perfect.
// This will not result in a Go level data-race, but can give incorrect information so is best-effort only.
// nolint: unused // (not true...)
func (i indexCollection[K, O]) dumpOutput() map[string]any {
	o := i.idx.c.List()
	keys := sets.New[K]()
	for _, oo := range o {
		keys.InsertAll(i.idx.extractKeys(oo)...)
	}
	res := map[string]any{}
	for k := range keys {
		ks := toString(k)
		res[ks] = *i.GetKey(ks)
	}
	return res
}

func (i indexCollection[K, O]) WaitUntilSynced(stop <-chan struct{}) bool {
	return i.idx.c.WaitUntilSynced(stop)
}

func (i indexCollection[K, O]) HasSynced() bool {
	return i.idx.c.HasSynced()
}

func (i indexCollection[K, O]) Register(f func(o Event[IndexObject[K, O]])) HandlerRegistration {
	return i.RegisterBatch(func(events []Event[IndexObject[K, O]]) {
		for _, o := range events {
			f(o)
		}
	}, true)
}

func (i indexCollection[K, O]) RegisterBatch(f func(o []Event[IndexObject[K, O]]), runExistingState bool) HandlerRegistration {
	return i.idx.c.RegisterBatch(func(o []Event[O]) {
		allKeys := sets.New[K]()
		for _, ev := range o {
			if ev.Old != nil {
				allKeys.InsertAll(i.idx.extractKeys(*ev.Old)...)
			}
			if ev.New != nil {
				allKeys.InsertAll(i.idx.extractKeys(*ev.New)...)
			}
		}
		downstream := make([]Event[IndexObject[K, O]], 0, len(allKeys))
		for key := range allKeys {
			v := i.GetKey(toString(key))
			// Due to the semantics around indexes, we cannot reasonably compute exactly correctly.
			// However, we don't really need to: simply triggering an Add/Delete is close enough to work.
			// Building a collection from an indexCollection only uses the events to determine the changed keys, which is
			// available with this information.
			if len(v.Objects) == 0 {
				downstream = append(downstream, Event[IndexObject[K, O]]{
					Old:   &IndexObject[K, O]{Key: key, Objects: nil},
					Event: controllers.EventDelete,
				})
			} else {
				downstream = append(downstream, Event[IndexObject[K, O]]{
					New:   v,
					Event: controllers.EventAdd,
				})
			}
		}
		f(downstream)
	}, runExistingState)
}
