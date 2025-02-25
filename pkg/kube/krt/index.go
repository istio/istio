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

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type Index[K comparable, O any] interface {
	Lookup(k K) []O
	AsCollection() Collection[IndexObject[K, O]]
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
	return NewIndex(c, func(o O) []string {
		return []string{o.GetNamespace()}
	})
}

// NewIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
func NewIndex[K comparable, O any](
	c Collection[O],
	extract func(o O) []K,
) Index[K, O] {
	idx := c.(internalCollection[O]).index(func(o O) []string {
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

func (i index[K, O]) name() string {
	return "todo"
}

func (i index[K, O]) uid() collectionUID {
	// TODO implement me
	panic("implement me")
}

func (i index[K, O]) dump() CollectionDump {
	return CollectionDump{
		Outputs:         nil,
		InputCollection: "",
		Inputs:          nil,
	}
}

func (i index[K, O]) augment(a any) any {
	// TODO implement me
	panic("implement me")
}

func (i index[K, O]) index(extract func(o IndexObject[K, O]) []string) kclient.RawIndexer {
	// TODO implement me
	panic("implement me")
}

func (i index[K, O]) GetKey(k string) *IndexObject[K, O] {
	tk := any(k).(K)
	objs := i.Lookup(tk) // TODO: allow non-string key types
	return &IndexObject[K, O]{
		Key:     tk,
		Objects: objs,
	}
}

func (i index[K, O]) List() []IndexObject[K, O] {
	panic("implement me")
}

func (i index[K, O]) WaitUntilSynced(stop <-chan struct{}) bool {
	return i.c.WaitUntilSynced(stop)
}

func (i index[K, O]) HasSynced() bool {
	return i.c.HasSynced()
}

func (i index[K, O]) Register(f func(o Event[IndexObject[K, O]])) Syncer {
	return i.RegisterBatch(func(events []Event[IndexObject[K, O]], initialSync bool) {
		for _, o := range events {
			f(o)
		}
	}, true)
}

func (i index[K, O]) RegisterBatch(f func(o []Event[IndexObject[K, O]], initialSync bool), runExistingState bool) Syncer {
	return i.c.RegisterBatch(func(o []Event[O], initialSync bool) {
		allKeys := sets.New[K]()
		for _, ev := range o {
			if ev.Old != nil {
				allKeys.InsertAll(i.extractKeys(*ev.Old)...)
			}
			if ev.New != nil {
				allKeys.InsertAll(i.extractKeys(*ev.New)...)
			}
		}
		downstream := make([]Event[IndexObject[K, O]], 0, len(allKeys))
		for key := range allKeys {
			v := i.GetKey(toString(key))
			if len(v.Objects) == 0 {
				downstream = append(downstream, Event[IndexObject[K, O]]{
					Old:   &IndexObject[K, O]{Key: key, Objects: nil},
					New:   nil, // TODO this is not correct
					Event: controllers.EventDelete,
				})
			} else {
				downstream = append(downstream, Event[IndexObject[K, O]]{
					Old:   nil,
					New:   v, // TODO this is not correct
					Event: controllers.EventAdd,
				})
			}
		}
		f(downstream, initialSync)
	}, runExistingState)
}

func (i index[K, O]) AsCollection() Collection[IndexObject[K, O]] {
	return i
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
