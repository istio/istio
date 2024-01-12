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
	"sync"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

// Index maintains a simple index over an informer
type Index[I any, K comparable] struct {
	mu      sync.RWMutex
	objects map[K]sets.Set[Key[I]]
	c       Collection[I]
}

// Lookup finds all objects matching a given key
func (i *Index[I, K]) Lookup(k K) []I {
	i.mu.RLock()
	defer i.mu.RUnlock()
	var res []I
	for obj := range i.objects[k] {
		item := i.c.GetKey(obj)
		if item == nil {
			log.Errorf("Impossible missing item for %v", obj)
			// This should be extremely rare, maybe impossible due to the mutex.
			continue
		}
		res = append(res, *item)
	}
	return res
}

func (i *Index[I, K]) Dump() {
	i.mu.RLock()
	defer i.mu.RUnlock()
	log.Errorf("> BEGIN DUMP (index %v[%T])", i.c.(internalCollection[I]).name(), ptr.TypeName[K]())
	for k, v := range i.objects {
		log.Errorf("key %v: %v", k, v.UnsortedList())
	}
	log.Errorf("< END DUMP (index %v[%T]", i.c.(internalCollection[I]).name(), ptr.TypeName[K]())
}

// CreateIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// NewInformer.AddIndex, but is easier to use and can be added after an informer has already started.
func CreateIndex[I any, K comparable](
	c Collection[I],
	extract func(o I) []K,
) *Index[I, K] {
	idx := Index[I, K]{
		objects: make(map[K]sets.Set[Key[I]]),
		c:       c,
		mu:      sync.RWMutex{},
	}
	c.Register(func(o Event[I]) {
		idx.mu.Lock()
		defer idx.mu.Unlock()

		if o.Old != nil {
			obj := *o.Old
			key := GetKey(obj)
			for _, indexKey := range extract(obj) {
				sets.DeleteCleanupLast(idx.objects, indexKey, key)
			}
		}
		if o.New != nil {
			obj := *o.New
			key := GetKey(obj)
			for _, indexKey := range extract(obj) {
				sets.InsertOrNew(idx.objects, indexKey, key)
			}
		}
	})

	return &idx
}
