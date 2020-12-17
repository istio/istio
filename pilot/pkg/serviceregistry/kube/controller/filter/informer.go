package filter

import (
	"k8s.io/client-go/tools/cache"
)

type filteredSharedIndexInformer struct {
	filterFunc func(obj interface{}) bool
	cache.SharedIndexInformer
	filteredIndexer *filteredIndexer
}

// wrap a SharedIndexInformer's handlers and indexer with a filter predicate,
// which scopes the processed objects to only those that satisfy the predicate
func NewFilteredSharedIndexInformer(
	filterFunc func(obj interface{}) bool,
	sharedIndexInformer cache.SharedIndexInformer,
) *filteredSharedIndexInformer {
	return &filteredSharedIndexInformer{
		filterFunc:          filterFunc,
		SharedIndexInformer: sharedIndexInformer,
		filteredIndexer:     newFilteredIndexer(filterFunc, sharedIndexInformer.GetIndexer()),
	}
}

// filter incoming objects before forwarding to event handler
func (w *filteredSharedIndexInformer) AddEventHandler(handler cache.ResourceEventHandler) {
	w.SharedIndexInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if !w.filterFunc(obj) {
				return
			}
			handler.OnAdd(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			if !w.filterFunc(new) {
				return
			}
			handler.OnUpdate(old, new)
		},
		DeleteFunc: func(obj interface{}) {
			if !w.filterFunc(obj) {
				return
			}
			handler.OnDelete(obj)
		},
	})
}

func (w *filteredSharedIndexInformer) GetIndexer() cache.Indexer {
	return w.filteredIndexer
}

type filteredIndexer struct {
	filterFunc func(obj interface{}) bool
	cache.Indexer
}

func newFilteredIndexer(
	filterFunc func(obj interface{}) bool,
	indexer cache.Indexer,
) *filteredIndexer {
	return &filteredIndexer{
		filterFunc: filterFunc,
		Indexer:    indexer,
	}
}
func (w filteredIndexer) List() []interface{} {
	unfiltered := w.Indexer.List()
	var filtered []interface{}
	for _, obj := range unfiltered {
		if w.filterFunc(obj) {
			filtered = append(filtered, obj)
		}
	}
	return filtered
}
