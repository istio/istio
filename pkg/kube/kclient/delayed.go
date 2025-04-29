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
	"sync/atomic"

	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// delayedClient is a client wrapper that initially starts with an "empty client",
// but can later be swapped with a real client.
// The "empty client" returns empty responses for all reads, and fails all writes.
type delayedClient[T controllers.ComparableObject] struct {
	inf *atomic.Pointer[Informer[T]]

	delayed kubetypes.DelayedFilter

	hm       sync.Mutex
	handlers []delayedHandler
	indexers []delayedIndex[T]
	started  <-chan struct{}
}

type delayedHandler struct {
	cache.ResourceEventHandler
	hasSynced delayedHandlerRegistration
}

type delayedHandlerRegistration struct {
	hasSynced *atomic.Pointer[func() bool]
}

func (r delayedHandlerRegistration) HasSynced() bool {
	if s := r.hasSynced.Load(); s != nil {
		return (*s)()
	}
	return false
}

type delayedIndex[T any] struct {
	name    string
	indexer *atomic.Pointer[RawIndexer]
	extract func(o T) []string
}

func (d delayedIndex[T]) Lookup(key string) []interface{} {
	if c := d.indexer.Load(); c != nil {
		return (*c).Lookup(key)
	}
	// Not ready yet, return nil
	return nil
}

func (s *delayedClient[T]) Index(name string, extract func(o T) []string) RawIndexer {
	if c := s.inf.Load(); c != nil {
		return (*c).Index(name, extract)
	}
	s.hm.Lock()
	defer s.hm.Unlock()
	di := delayedIndex[T]{name: name, indexer: new(atomic.Pointer[RawIndexer]), extract: extract}
	s.indexers = append(s.indexers, di)
	return di
}

func (s *delayedClient[T]) Get(name, namespace string) T {
	if c := s.inf.Load(); c != nil {
		return (*c).Get(name, namespace)
	}
	return ptr.Empty[T]()
}

func (s *delayedClient[T]) List(namespace string, selector klabels.Selector) []T {
	if c := s.inf.Load(); c != nil {
		return (*c).List(namespace, selector)
	}
	return nil
}

func (s *delayedClient[T]) ListUnfiltered(namespace string, selector klabels.Selector) []T {
	if c := s.inf.Load(); c != nil {
		return (*c).ListUnfiltered(namespace, selector)
	}
	return nil
}

func (s *delayedClient[T]) AddEventHandler(h cache.ResourceEventHandler) cache.ResourceEventHandlerRegistration {
	if c := s.inf.Load(); c != nil {
		return (*c).AddEventHandler(h)
	}
	s.hm.Lock()
	defer s.hm.Unlock()

	hasSynced := delayedHandlerRegistration{hasSynced: new(atomic.Pointer[func() bool])}
	hasSynced.hasSynced.Store(ptr.Of(s.delayed.HasSynced))
	s.handlers = append(s.handlers, delayedHandler{
		ResourceEventHandler: h,
		hasSynced:            hasSynced,
	})
	return hasSynced
}

func (s *delayedClient[T]) HasSynced() bool {
	if c := s.inf.Load(); c != nil {
		return (*c).HasSynced()
	}
	// If we haven't loaded the informer yet, we want to check if the delayed filter is synced.
	// This ensures that at startup, we only return HasSynced=true if we are sure the CRD is not ready.
	hs := s.delayed.HasSynced()
	return hs
}

func (s *delayedClient[T]) HasSyncedIgnoringHandlers() bool {
	if c := s.inf.Load(); c != nil {
		return (*c).HasSyncedIgnoringHandlers()
	}
	// If we haven't loaded the informer yet, we want to check if the delayed filter is synced.
	// This ensures that at startup, we only return HasSynced=true if we are sure the CRD is not ready.
	hs := s.delayed.HasSynced()
	return hs
}

func (s *delayedClient[T]) ShutdownHandlers() {
	if c := s.inf.Load(); c != nil {
		(*c).ShutdownHandlers()
	} else {
		s.hm.Lock()
		defer s.hm.Unlock()
		s.handlers = nil
	}
}

func (s *delayedClient[T]) ShutdownHandler(registration cache.ResourceEventHandlerRegistration) {
	if c := s.inf.Load(); c != nil {
		(*c).ShutdownHandlers()
	} else {
		s.hm.Lock()
		defer s.hm.Unlock()
		s.handlers = slices.FilterInPlace(s.handlers, func(handler delayedHandler) bool {
			return handler.hasSynced != registration
		})
	}
}

func (s *delayedClient[T]) Start(stop <-chan struct{}) {
	if c := s.inf.Load(); c != nil {
		(*c).Start(stop)
	}
	s.hm.Lock()
	defer s.hm.Unlock()
	s.started = stop
}

var _ Informer[controllers.Object] = &delayedClient[controllers.Object]{}

func (s *delayedClient[T]) set(inf Informer[T]) {
	if inf != nil {
		s.inf.Swap(&inf)
		s.hm.Lock()
		defer s.hm.Unlock()
		for _, h := range s.handlers {
			reg := inf.AddEventHandler(h)
			h.hasSynced.hasSynced.Store(ptr.Of(reg.HasSynced))
		}
		s.handlers = nil
		for _, i := range s.indexers {
			res := inf.Index(i.name, i.extract)
			i.indexer.Store(&res)
		}
		s.indexers = nil
		if s.started != nil {
			inf.Start(s.started)
		}
	}
}

type delayedFilter struct {
	Watcher  kubetypes.CrdWatcher
	Resource schema.GroupVersionResource
}

func (d *delayedFilter) HasSynced() bool {
	return d.Watcher.HasSynced()
}

func (d *delayedFilter) KnownOrCallback(f func(stop <-chan struct{})) bool {
	return d.Watcher.KnownOrCallback(d.Resource, f)
}

func newDelayedFilter(resource schema.GroupVersionResource, watcher kubetypes.CrdWatcher) *delayedFilter {
	return &delayedFilter{
		Watcher:  watcher,
		Resource: resource,
	}
}
