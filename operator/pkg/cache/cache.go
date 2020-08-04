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

package cache

import (
	"sync"

	"istio.io/istio/operator/pkg/object"
)

// ObjectCache is a cache of objects,
type ObjectCache struct {
	// Cache is a cache keyed by object Hash() function.
	Cache map[string]*object.K8sObject
	Mu    *sync.RWMutex
}

var (
	// objectCaches holds the latest copy of each object applied by the controller. The caches are divided by component
	// name.
	objectCaches   = make(map[string]*ObjectCache)
	objectCachesMu sync.RWMutex
)

// FlushObjectCaches flushes all object caches.
func FlushObjectCaches() {
	objectCachesMu.Lock()
	defer objectCachesMu.Unlock()
	objectCaches = make(map[string]*ObjectCache)
}

// GetCache returns the object Cache for the given name, creating one in the global Cache if needed.
func GetCache(name string) *ObjectCache {
	objectCachesMu.Lock()
	defer objectCachesMu.Unlock()

	// Create and/or get the Cache corresponding to the CR name we're processing. Per name partitioning is required to
	// prune the Cache to remove any objects not in the manifest generated for a given CR.
	if objectCaches[name] == nil {
		objectCaches[name] = &ObjectCache{
			Cache: make(map[string]*object.K8sObject),
			Mu:    &sync.RWMutex{},
		}
	}
	return objectCaches[name]
}

// RemoveObject removes object with objHash in the Cache with the given name from the object Cache.
func RemoveObject(name, objHash string) {
	objectCachesMu.Lock()
	objectCache := objectCaches[name]
	objectCachesMu.Unlock()

	if objectCache != nil {
		objectCache.Mu.Lock()
		delete(objectCache.Cache, objHash)
		objectCache.Mu.Unlock()
	}
}
