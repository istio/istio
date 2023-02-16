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

package controllers

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/util/sets"
)

// Index maintains a simple index over an informer
type Index[O runtime.Object, K comparable] struct {
	mu       sync.RWMutex
	objects  map[K]sets.String
	informer cache.SharedIndexInformer
}

// Lookup finds all objects matching a given key
func (i *Index[O, K]) Lookup(k K) []O {
	i.mu.RLock()
	defer i.mu.RUnlock()
	res := make([]O, 0)
	for obj := range i.objects[k] {
		item, f, err := i.informer.GetStore().GetByKey(obj)
		if !f || err != nil {
			// This should be extremely rare, maybe impossible due to the mutex.
			continue
		}
		res = append(res, item.(O))
	}
	return res
}

// CreateIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
func CreateIndex[O runtime.Object, K comparable](
	informer cache.SharedIndexInformer,
	extract func(o O) []K,
) *Index[O, K] {
	idx := Index[O, K]{
		objects:  make(map[K]sets.String),
		informer: informer,
		mu:       sync.RWMutex{},
	}
	addObj := func(obj any) {
		ro := ExtractObject(obj)
		o := ro.(O)
		objectKey := kube.KeyFunc(ro.GetName(), ro.GetNamespace())
		for _, indexKey := range extract(o) {
			sets.InsertOrNew(idx.objects, indexKey, objectKey)
		}
	}
	deleteObj := func(obj any) {
		ro := ExtractObject(obj)
		o := ro.(O)
		objectKey := kube.KeyFunc(ro.GetName(), ro.GetNamespace())
		for _, indexKey := range extract(o) {
			sets.DeleteCleanupLast(idx.objects, indexKey, objectKey)
		}
	}
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			addObj(obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			deleteObj(oldObj)
			addObj(newObj)
		},
		DeleteFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			deleteObj(obj)
		},
	}
	_, _ = informer.AddEventHandler(handler)
	return &idx
}
