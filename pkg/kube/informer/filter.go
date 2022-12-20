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

package informer

import (
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube"
)

type FilteredSharedIndexInformer interface {
	AddEventHandler(handler cache.ResourceEventHandler)
	GetIndexer() cache.Indexer
	List(namespace string) ([]any, error)
	SetWatchErrorHandler(handler cache.WatchErrorHandler) error
	HasSynced() bool
	Run(stopCh <-chan struct{})
}

type filteredSharedIndexInformer struct {
	filterFunc func(obj any) bool
	cache.SharedIndexInformer
	filteredIndexer *filteredIndexer
}

// NewFilteredSharedIndexInformer wraps a SharedIndexInformer's handlers and indexer with a filter predicate,
// which scopes the processed objects to only those that satisfy the predicate
func NewFilteredSharedIndexInformer(
	filterFunc func(obj any) bool,
	sharedIndexInformer cache.SharedIndexInformer,
) FilteredSharedIndexInformer {
	_ = sharedIndexInformer.SetTransform(kube.StripUnusedFields)
	return &filteredSharedIndexInformer{
		filterFunc:          filterFunc,
		SharedIndexInformer: sharedIndexInformer,
		filteredIndexer:     newFilteredIndexer(filterFunc, sharedIndexInformer.GetIndexer()),
	}
}

// AddEventHandler filters incoming objects before forwarding to event handler
func (w *filteredSharedIndexInformer) AddEventHandler(handler cache.ResourceEventHandler) {
	_, _ = w.SharedIndexInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if w.filterFunc != nil && !w.filterFunc(obj) {
				return
			}
			handler.OnAdd(obj)
		},
		UpdateFunc: func(old, new any) {
			if w.filterFunc != nil && !w.filterFunc(new) {
				return
			}
			handler.OnUpdate(old, new)
		},
		DeleteFunc: func(obj any) {
			if w.filterFunc != nil && !w.filterFunc(obj) {
				return
			}
			handler.OnDelete(obj)
		},
	})
}

func (w *filteredSharedIndexInformer) HasSynced() bool {
	w.SharedIndexInformer.GetStore()
	return w.SharedIndexInformer.HasSynced()
}

func (w *filteredSharedIndexInformer) Run(stopCh <-chan struct{}) {
	w.SharedIndexInformer.Run(stopCh)
}

func (w *filteredSharedIndexInformer) GetIndexer() cache.Indexer {
	return w.filteredIndexer
}

func (w *filteredSharedIndexInformer) List(namespace string) ([]any, error) {
	if namespace == model.NamespaceAll {
		return w.filteredIndexer.List(), nil
	}
	return w.filteredIndexer.ByIndex("namespace", namespace)
}

func (w *filteredSharedIndexInformer) SetWatchErrorHandler(handler cache.WatchErrorHandler) error {
	_ = w.SharedIndexInformer.SetWatchErrorHandler(handler)
	return nil
}

type filteredIndexer struct {
	filterFunc func(obj any) bool
	cache.Indexer
}

func newFilteredIndexer(
	filterFunc func(obj any) bool,
	indexer cache.Indexer,
) *filteredIndexer {
	return &filteredIndexer{
		filterFunc: filterFunc,
		Indexer:    indexer,
	}
}

func (w filteredIndexer) List() []any {
	unfiltered := w.Indexer.List()
	if w.filterFunc == nil {
		return unfiltered
	}
	var filtered []any
	for _, obj := range unfiltered {
		if w.filterFunc(obj) {
			filtered = append(filtered, obj)
		}
	}
	return filtered
}

func (w filteredIndexer) GetByKey(key string) (item any, exists bool, err error) {
	item, exists, err = w.Indexer.GetByKey(key)
	if !exists || err != nil {
		return nil, exists, err
	}
	if w.filterFunc == nil || w.filterFunc(item) {
		return item, true, nil
	}
	return nil, false, nil
}

func (w filteredIndexer) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	unfiltered, err := w.Indexer.ByIndex(indexName, indexedValue)
	if err != nil {
		return nil, err
	}
	if w.filterFunc == nil {
		return unfiltered, nil
	}
	var filtered []any
	for _, obj := range unfiltered {
		if w.filterFunc(obj) {
			filtered = append(filtered, obj)
		}
	}
	return filtered, nil
}
