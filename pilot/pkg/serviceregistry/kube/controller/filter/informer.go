package filter

import (
	"k8s.io/client-go/tools/cache"
)

type FilteredSharedIndexInformer interface {
	AddEventHandler(handler cache.ResourceEventHandler)
	GetIndexer() cache.Indexer
	HasSynced() bool
	Run(stopCh <-chan struct{})
}

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

func (w filteredIndexer) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists, err = w.Indexer.GetByKey(key)
	if !exists || err != nil {
		return nil, exists, err
	}
	if w.filterFunc(item) {
		return item, true, nil
	}
	return nil, false, nil
}
