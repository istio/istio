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

package kclient

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
)

// Index maintains a simple index over an informer
type Index[K comparable, O controllers.ComparableObject] struct {
	mu      sync.RWMutex
	objects map[K]sets.Set[types.NamespacedName]
	client  Informer[O]
}

// Lookup finds all objects matching a given key
func (i *Index[K, O]) Lookup(k K) []O {
	i.mu.RLock()
	defer i.mu.RUnlock()
	res := make([]O, 0)
	for obj := range i.objects[k] {
		item := i.client.Get(obj.Name, obj.Namespace)
		if controllers.IsNil(item) {
			// This should be extremely rare, maybe impossible due to the mutex.
			continue
		}
		res = append(res, item)
	}
	return res
}

// CreateIndexWithDelegate creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
// An additional ResourceEventHandler can be passed in that is guaranteed to happen *after* the index is updated.
// This allows the delegate to depend on the contents of the index.
// TODO(https://github.com/kubernetes/kubernetes/pull/117046) remove this.
func CreateIndexWithDelegate[K comparable, O controllers.ComparableObject](
	client Informer[O],
	extract func(o O) []K,
	delegate cache.ResourceEventHandler,
) *Index[K, O] {
	idx := Index[K, O]{
		objects: make(map[K]sets.Set[types.NamespacedName]),
		client:  client,
		mu:      sync.RWMutex{},
	}
	addObj := func(obj any) {
		ro := controllers.ExtractObject(obj)
		o := ro.(O)
		objectKey := config.NamespacedName(o)
		for _, indexKey := range extract(o) {
			sets.InsertOrNew(idx.objects, indexKey, objectKey)
		}
	}
	deleteObj := func(obj any) {
		ro := controllers.ExtractObject(obj)
		o := ro.(O)
		objectKey := config.NamespacedName(o)
		for _, indexKey := range extract(o) {
			sets.DeleteCleanupLast(idx.objects, indexKey, objectKey)
		}
	}
	handler := cache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(obj any, initialList bool) {
			idx.mu.Lock()
			addObj(obj)
			idx.mu.Unlock()
			if delegate != nil {
				delegate.OnAdd(obj, initialList)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			idx.mu.Lock()
			deleteObj(oldObj)
			addObj(newObj)
			idx.mu.Unlock()
			if delegate != nil {
				delegate.OnUpdate(oldObj, newObj)
			}
		},
		DeleteFunc: func(obj any) {
			idx.mu.Lock()
			deleteObj(obj)
			idx.mu.Unlock()
			if delegate != nil {
				delegate.OnDelete(obj)
			}
		},
	}
	client.AddEventHandler(handler)
	return &idx
}

// CreateIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
func CreateIndex[K comparable, O controllers.ComparableObject](
	client Informer[O],
	extract func(o O) []K,
) *Index[K, O] {
	return CreateIndexWithDelegate(client, extract, nil)
}
