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
// * Ability to create multiple informers of the same type but with different filters.
// * Ability to run a single informer rather than all of them.
package informerfactory

import (
	"fmt"
	"runtime/debug"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// NewInformerFunc returns a SharedIndexInformer.
type NewInformerFunc func() cache.SharedIndexInformer

type StartableInformer struct {
	Informer cache.SharedIndexInformer
	start    func(stopCh <-chan struct{})
}

func (s StartableInformer) Start(stopCh <-chan struct{}) {
	s.start(stopCh)
}

// InformerFactory provides access to a shared informer factory
type InformerFactory interface {
	// Start initializes all requested informers. They are handled in goroutines
	// which run until the stop channel gets closed.
	Start(stopCh <-chan struct{})

	// InformerFor returns the SharedIndexInformer the provided type.
	InformerFor(resource schema.GroupVersionResource, opts kubetypes.InformerOptions, newFunc NewInformerFunc) StartableInformer

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
		informers:        map[informerKey]builtInformer{},
		startedInformers: sets.New[informerKey](),
	}
}

// InformerKey represents a unique informer
type informerKey struct {
	gvr           schema.GroupVersionResource
	labelSelector string
	fieldSelector string
	informerType  kubetypes.InformerType
	namespace     string
}

type builtInformer struct {
	informer        cache.SharedIndexInformer
	objectTransform func(obj any) (any, error)
}

type informerFactory struct {
	lock      sync.Mutex
	informers map[informerKey]builtInformer
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

func (f *informerFactory) InformerFor(resource schema.GroupVersionResource, opts kubetypes.InformerOptions, newFunc NewInformerFunc) StartableInformer {
	f.lock.Lock()
	defer f.lock.Unlock()

	key := informerKey{
		gvr:           resource,
		labelSelector: opts.LabelSelector,
		fieldSelector: opts.FieldSelector,
		informerType:  opts.InformerType,
		namespace:     opts.Namespace,
	}
	inf, exists := f.informers[key]
	if exists {
		checkInformerOverlap(inf, resource, opts)
		return f.makeStartableInformer(inf.informer, key)
	}

	informer := newFunc()
	f.informers[key] = builtInformer{
		informer:        informer,
		objectTransform: opts.ObjectTransform,
	}

	return f.makeStartableInformer(informer, key)
}

func allowedOverlap(resource schema.GroupVersionResource) bool {
	// We register an optimized Pod watcher for standard flow, but for the experimental analysis feature we need the full pod,
	// so we start another watch.
	// We may want to reconsider this if the analysis feature becomes stable.
	return features.EnableAnalysis && resource == gvr.Pod
}

func checkInformerOverlap(inf builtInformer, resource schema.GroupVersionResource, opts kubetypes.InformerOptions) {
	if fmt.Sprintf("%p", inf.objectTransform) == fmt.Sprintf("%p", opts.ObjectTransform) {
		return
	}
	l := log.Warnf
	if features.EnableUnsafeAssertions && !allowedOverlap(resource) {
		l = log.Fatalf
	}
	l("for type %v, registered conflicting ObjectTransform. Stack: %v", resource, string(debug.Stack()))
}

func (f *informerFactory) makeStartableInformer(informer cache.SharedIndexInformer, key informerKey) StartableInformer {
	return StartableInformer{
		Informer: informer,
		start: func(stopCh <-chan struct{}) {
			f.startOne(stopCh, key)
		},
	}
}

func (f *informerFactory) startOne(stopCh <-chan struct{}, informerType informerKey) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.shuttingDown {
		return
	}

	informer, ff := f.informers[informerType]
	if !ff {
		panic(fmt.Sprintf("bug: informer key %+v not found", informerType))
	}
	if !f.startedInformers.Contains(informerType) {
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			informer.informer.Run(stopCh)
		}()
		f.startedInformers.Insert(informerType)
	}
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
				informer.informer.Run(stopCh)
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
				informers = append(informers, informer.informer)
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
