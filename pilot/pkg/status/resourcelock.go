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

package status

import (
	"context"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceMutex struct {
	masterLock sync.RWMutex
	cache      map[lockResource]*cacheEntry
}

type cacheEntry struct {
	// the runlock ensures only one routine is writing status for a given resource at a time
	runLock sync.Mutex
	// the cachelock protects writes and reads to the cache values
	cacheLock sync.Mutex
	// the cacheVale represents the latest version of the resource, including ResourceVersion
	cacheVal *Resource
	// the cacheProgress represents the latest version of the Progress
	cacheProgress *Progress
	countLock     sync.RWMutex
	// track how many routines are running for this resource.  always <= 2
	routineCount int32
	// track if this resource has been deleted, and if so, stop writing updates for it
	deleted bool
}

func (ce *cacheEntry) shouldRun() (result bool) {
	ce.countLock.RLock()
	count := ce.routineCount
	ce.countLock.RUnlock()
	if count < 2 {
		// increment and return true
		ce.countLock.Lock()
		if ce.routineCount < 2 {
			ce.routineCount += 1
			result = true
		}
		ce.countLock.Unlock()
	}
	return
}

func (ce *cacheEntry) decrementCount() (result bool) {
	ce.countLock.Lock()
	ce.routineCount -= 1
	ce.countLock.Unlock()
}

func (rl *ResourceMutex) Delete(r Resource) {
	f := rl.retrieveEntry(convert(r))
	f.cacheLock.Lock()
	defer f.cacheLock.Unlock()
	f.deleted = true
}

// OncePerResource will run the target function at most once per resource, with up to one waiting routine per resource
// we only want one update per resource running concurrently to prevent overwhelming the API server, but we also
// don't want to stack up goroutines when the API server is overwhelmed.  This limits us to 2*resourceCount goroutines
// while ensuring that when a goroutine begins, it gets the latest status values for the resource.
func (rl *ResourceMutex) OncePerResource(ctx context.Context, r Resource, p Progress, target func(ctx context.Context, config Resource, state Progress)) {
	lr := convert(r)
	f := rl.retrieveEntry(lr)
	// every call updates the cache, so whenever the standby routine fires, it runs with the latest values
	f.cacheLock.Lock()
	f.cacheVal = &r
	f.cacheProgress = &p
	f.cacheLock.Unlock()
	if f.shouldRun() {
		go func() {
			f.runLock.Lock()
			f.cacheLock.Lock()
			if f.deleted {
				f.decrementCount()
				delete(rl.cache, lr)
				f.cacheLock.Unlock()
				f.runLock.Unlock()
				return
			}
			finalResource := f.cacheVal
			finalProgress := f.cacheProgress
			f.cacheLock.Unlock()
			target(ctx, *finalResource, *finalProgress)
			f.decrementCount()
			f.runLock.Unlock()
		}()
	}
}

func convert(i Resource) lockResource {
	return lockResource{
		GroupVersionResource: i.GroupVersionResource,
		Namespace:            i.Namespace,
		Name:                 i.Name,
	}
}

// returns value indicating if init was necessary
func (r *ResourceMutex) init() bool {
	if r.cache == nil {
		r.masterLock.Lock()
		defer r.masterLock.Unlock()
		// double check, per pattern
		if r.cache == nil {
			r.cache = make(map[lockResource]*cacheEntry)
		}
		return true
	}
	return false
}

func (r *ResourceMutex) retrieveEntry(i lockResource) *cacheEntry {
	if !r.init() {
		r.masterLock.RLock()
		if result, ok := r.cache[i]; ok {
			r.masterLock.RUnlock()
			return result
		}
		// transition to write lock
		r.masterLock.RUnlock()
	}
	r.masterLock.Lock()
	defer r.masterLock.Unlock()
	r.cache[i] = &cacheEntry{}
	return r.cache[i]
}

type lockResource struct {
	schema.GroupVersionResource
	Namespace string
	Name      string
}
