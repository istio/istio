/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package informerfactory provides a "factory" to generate informers. This allows users to create the
// same informers in multiple different locations, while still using the same underlying resources.
// Additionally, aggregate operations like Start, Shutdown, and Wait are available.
// Kubernetes core has informer factories with very similar logic. However, this has a few problems that
// spurred a fork:
// * Factories are per package. That means we have ~6 distinct factories, which makes management a hassle.
// * Across these, the factories are often inconsistent in functionality. Changes to these takes >4 months.
// * Lack of functionality we want (see below).
//
// Added functionality:
// * Single factory for any type, including dynamic informers, meta informers, typed informers, etc.
// * TODO: ability to run a single informer rather than all of them.
// * TODO: ability to create multiple informers of the same type but with different filters.
package informerfactory

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/util/sets"
)

// NewInformerFunc returns a SharedIndexInformer.
type NewInformerFunc func() cache.SharedIndexInformer

// InformerFactory provides access to a shared informer factory
type InformerFactory interface {
	// Start initializes all requested informers. They are handled in goroutines
	// which run until the stop channel gets closed.
	Start(stopCh <-chan struct{})

	// InformerFor returns the SharedIndexInformer the provided type.
	InformerFor(resource schema.GroupVersionResource, newFunc NewInformerFunc) cache.SharedIndexInformer

	// WaitForCacheSync blocks until all started informers' caches were synced
	// or the stop channel gets closed.
	WaitForCacheSync(stopCh <-chan struct{}) bool

	// Shutdown marks a factory as shutting down. At that point no new
	// informers can be started anymore and Start will return without
	// doing anything.
	//
	// In addition, Shutdown blocks until all goroutines have terminated. For that
	// to happen, the close channel(s) that they were started with must be closed,
	// either before Shutdown gets called or while it is waiting.
	//
	// Shutdown may be called multiple times, even concurrently. All such calls will
	// block until all goroutines have terminated.
	Shutdown()
}

// NewSharedInformerFactory constructs a new instance of informerFactory for all namespaces.
func NewSharedInformerFactory() InformerFactory {
	return &informerFactory{
		informers:        map[informerKey]cache.SharedIndexInformer{},
		startedInformers: sets.New[informerKey](),
	}
}

type informerKey struct {
	gvr schema.GroupVersionResource
}

type informerFactory struct {
	lock      sync.Mutex
	informers map[informerKey]cache.SharedIndexInformer
	// startedInformers is used for tracking which informers have been started.
	// This allows Start() to be called multiple times safely.
	startedInformers sets.Set[informerKey]

	// wg tracks how many goroutines were started.
	wg sync.WaitGroup
	// shuttingDown is true when Shutdown has been called. It may still be running
	// because it needs to wait for goroutines.
	shuttingDown bool
}

var _ InformerFactory = &informerFactory{}

func (f *informerFactory) InformerFor(resource schema.GroupVersionResource, newFunc NewInformerFunc) cache.SharedIndexInformer {
	f.lock.Lock()
	defer f.lock.Unlock()

	key := informerKey{
		gvr: resource,
	}
	informer, exists := f.informers[key]
	if exists {
		return informer
	}

	informer = newFunc()
	f.informers[key] = informer

	return informer
}

// Start initializes all requested informers.
func (f *informerFactory) Start(stopCh <-chan struct{}) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.shuttingDown {
		return
	}

	for informerType, informer := range f.informers {
		if !f.startedInformers.Contains(informerType) {
			f.wg.Add(1)
			// We need a new variable in each loop iteration,
			// otherwise the goroutine would use the loop variable
			// and that keeps changing.
			informer := informer
			go func() {
				defer f.wg.Done()
				informer.Run(stopCh)
			}()
			f.startedInformers.Insert(informerType)
		}
	}
}

// WaitForCacheSync waits for all started informers' cache were synced.
func (f *informerFactory) WaitForCacheSync(stopCh <-chan struct{}) bool {
	informers := func() []cache.SharedIndexInformer {
		f.lock.Lock()
		defer f.lock.Unlock()
		informers := make([]cache.SharedIndexInformer, 0, len(f.informers))
		for informerKey, informer := range f.informers {
			if f.startedInformers.Contains(informerKey) {
				informers = append(informers, informer)
			}
		}
		return informers
	}()

	for _, informer := range informers {
		if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {
			return false
		}
	}
	return true
}

func (f *informerFactory) Shutdown() {
	// Will return immediately if there is nothing to wait for.
	defer f.wg.Wait()

	f.lock.Lock()
	defer f.lock.Unlock()
	f.shuttingDown = true
}
