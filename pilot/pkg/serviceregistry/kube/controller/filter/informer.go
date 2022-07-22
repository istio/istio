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

package filter

import (
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
)

type FilteredSharedIndexInformer interface {
	AddEventHandler(handler cache.ResourceEventHandler)
	GetIndexer() cache.Indexer
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
	w.SharedIndexInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if !w.filterFunc(obj) {
				return
			}
			handler.OnAdd(obj)
		},
		UpdateFunc: func(old, new any) {
			if !w.filterFunc(new) {
				return
			}
			handler.OnUpdate(old, new)
		},
		DeleteFunc: func(obj any) {
			if !w.filterFunc(obj) {
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
	if w.filterFunc(item) {
		return item, true, nil
	}
	return nil, false, nil
}
