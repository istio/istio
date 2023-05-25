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

	"golang.org/x/exp/slices"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/controllers"
)

// DerivedCollection defines a collection of resources. Unlike an informer, which reads from Kubernetes,
// this is intended to be used for derived resources. For instance, we could combine a Pod and
// Service into a ServiceInstance.
type DerivedCollection[T comparable] struct {
	compute  func(name types.NamespacedName) T
	equality func(a, b T) bool
	mu       sync.RWMutex
	objects  map[types.NamespacedName]T
	handlers []cache.ResourceEventHandler
}

// NewDerivedCollection creates a new collection.
// required: compute, used to build the resource.
// Optional: equality; if set, we will not trigger events if the previous and new result is the same.
func NewDerivedCollection[T comparable](compute func(name types.NamespacedName) T, equality func(a, b T) bool) *DerivedCollection[T] {
	return &DerivedCollection[T]{
		compute:  compute,
		equality: equality,
		objects:  map[types.NamespacedName]T{},
	}
}

// Ensure we implement the same interface to make things a bit more abstract.
var _ Informer[any] = &DerivedCollection[any]{}

func (f *DerivedCollection[T]) equals(a, b T) bool {
	if f.equality == nil {
		// No equality defined, just assume its always changed
		return false
	}
	return f.equality(a, b)
}

func (f *DerivedCollection[T]) List(namespace string, selector klabels.Selector) []T {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := []T{}
	for k, v := range f.objects {
		// TODO support selector
		if namespace == "" || k.Namespace == namespace {
			res = append(res, v)
		}
	}
	return res
}

func (f *DerivedCollection[T]) ListUnfiltered(namespace string, selector klabels.Selector) []T {
	return f.List(namespace, selector)
}

func (f *DerivedCollection[T]) HasSynced() bool {
	// TODO: we have no meaningful way to implement this
	return true
}

func (f *DerivedCollection[T]) ShutdownHandlers() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers = nil
}

func (f *DerivedCollection[T]) Get(name, namespace string) T {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.objects[types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}]
}

func (f *DerivedCollection[T]) AddEventHandler(h cache.ResourceEventHandler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers = append(f.handlers, h)
}

func (f *DerivedCollection[T]) RecomputeE(name types.NamespacedName) error {
	f.Recompute(name)
	return nil
}

func (f *DerivedCollection[T]) Recompute(name types.NamespacedName) {
	res := f.compute(name)
	f.mu.Lock()
	old, oldExists := f.objects[name]
	var event model.Event
	if controllers.IsNil(res) {
		if !oldExists {
			// No object for this key, and no existing object. Nothing to do
			f.mu.Unlock()
			return
		}
		// This is a delete
		event = model.EventDelete
		delete(f.objects, name)
	} else if oldExists {
		event = model.EventUpdate
		if f.equals(old, res) {
			// no action needed, it was a NOP change
			f.mu.Unlock()
			return
		}
		f.objects[name] = res
	} else {
		event = model.EventAdd
		f.objects[name] = res
	}
	handlers := slices.Clone(f.handlers)
	f.mu.Unlock()
	for _, h := range handlers {
		switch event {
		case model.EventAdd:
			h.OnAdd(res, false)
		case model.EventDelete:
			h.OnDelete(old)
		case model.EventUpdate:
			h.OnUpdate(old, res)
		}
	}
}

func (f *DerivedCollection[T]) Start(stop <-chan struct{}) {
}
