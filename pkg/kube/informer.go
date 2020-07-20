package kube

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

type InformerFactory interface {
	Start(stopCh <-chan struct{})
	ForResource(resource schema.GroupVersionResource) (InformerLister, error)
	WaitForCacheSync(stopCh <-chan struct{})
}

type Informer interface {
	AddEventHandler(handler cache.ResourceEventHandler)
	AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration)
	AddIndexers(indexers cache.Indexers) error
	HasSynced() bool
}

type Lister interface {
	List(out runtime.Object, opts ...ListOption) error
	Get(key types.NamespacedName, out runtime.Object) error
}

type InformerLister interface {
	Informer
	Lister
}

type multiNamespaceInformerFactory struct {
	namespaceToFactory map[string]informers.SharedInformerFactory
}

var _ InformerFactory = &multiNamespaceInformerFactory{}

type NewInformerFactoryFunc func(namespace string) informers.SharedInformerFactory

func NewInformerFactory(namespaces []string, f NewInformerFactoryFunc) (InformerFactory, error) {
	// TOOD(bison): Dedupe namespaces and check for metav1.NamespaceAll.
	if len(namespaces) < 1 {
		return nil, errors.New("must provide at least one namespace, which may be metav1.NamespaceAll")
	}

	factory := &multiNamespaceInformerFactory{
		namespaceToFactory: make(map[string]informers.SharedInformerFactory),
	}

	for _, ns := range namespaces {
		factory.namespaceToFactory[ns] = f(ns)
	}

	return factory, nil
}

func (f *multiNamespaceInformerFactory) ForResource(resource schema.GroupVersionResource) (InformerLister, error) {
	informer := &multiNamespaceInformer{
		namespaceToInformer: make(map[string]cache.SharedIndexInformer),
		resource:            resource,
	}

	for ns, factory := range f.namespaceToFactory {
		genericInformer, err := factory.ForResource(resource)
		if err != nil {
			return nil, err
		}

		informer.namespaceToInformer[ns] = genericInformer.Informer()
	}

	return informer, nil
}

func (f *multiNamespaceInformerFactory) Start(stopCh <-chan struct{}) {
	for _, factory := range f.namespaceToFactory {
		factory.Start(stopCh)
	}
}

func (f *multiNamespaceInformerFactory) WaitForCacheSync(stopCh <-chan struct{}) {
	var wg sync.WaitGroup

	for i := range f.namespaceToFactory {
		factory := f.namespaceToFactory[i]
		wg.Add(1)
		go func() {
			factory.WaitForCacheSync(stopCh)
			wg.Done()
		}()
	}

	wg.Wait()
}

type multiNamespaceInformer struct {
	namespaceToInformer map[string]cache.SharedIndexInformer
	resource            schema.GroupVersionResource
}

var _ Informer = &multiNamespaceInformer{}

// AddEventHandler adds the handler to each namespaced informer.
func (i *multiNamespaceInformer) AddEventHandler(handler cache.ResourceEventHandler) {
	for _, informer := range i.namespaceToInformer {
		informer.AddEventHandler(handler)
	}
}

// AddEventHandlerWithResyncPeriod adds the handler with a resync period to each
// namespaced informer.
func (i *multiNamespaceInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) {
	for _, informer := range i.namespaceToInformer {
		informer.AddEventHandlerWithResyncPeriod(handler, resyncPeriod)
	}
}

// AddIndexers adds the indexer for each namespaced informer.
func (i *multiNamespaceInformer) AddIndexers(indexers cache.Indexers) error {
	for _, informer := range i.namespaceToInformer {
		err := informer.AddIndexers(indexers)
		if err != nil {
			return err
		}
	}
	return nil
}

// HasSynced checks if each namespaced informer has synced.
func (i *multiNamespaceInformer) HasSynced() bool {
	for _, informer := range i.namespaceToInformer {
		if ok := informer.HasSynced(); !ok {
			return ok
		}
	}
	return true
}

// Get fetches the object names by the given key, and places it in out.
func (i *multiNamespaceInformer) Get(key types.NamespacedName, out runtime.Object) error {
	indexer, err := i.indexerForNamespace(key.Namespace)
	if err != nil {
		return err
	}

	obj, exists, err := indexer.GetByKey(key.String())
	if err != nil {
		return err
	}

	if !exists {
		return kerrors.NewNotFound(i.resource.GroupResource(), key.String())
	}

	outVal := reflect.ValueOf(out)
	objVal := reflect.ValueOf(obj)
	if !objVal.Type().AssignableTo(outVal.Type()) {
		return fmt.Errorf("cache had type %s, but %s was asked for", objVal.Type(), outVal.Type())
	}
	reflect.Indirect(outVal).Set(reflect.Indirect(objVal))

	// TODO(bison): Set GroupVersionKind?
	// out.GetObjectKind().SetGroupVersionKind(groupVersionKind)

	return nil
}

type ListOptions struct {
	namespace string
	selector  labels.Selector
}

func DefaultListOptions() *ListOptions {
	return &ListOptions{
		selector:  labels.Everything(),
		namespace: metav1.NamespaceAll,
	}
}

func (l *ListOptions) ApplyOptions(opts ...ListOption) *ListOptions {
	for _, opt := range opts {
		opt.ApplyToList(l)
	}

	return l
}

type ListOption interface {
	ApplyToList(*ListOptions)
}

type InNamespace string

func (n InNamespace) ApplyToList(opts *ListOptions) {
	opts.namespace = string(n)
}

type MatchingLabelsSelector struct {
	labels.Selector
}

func (m MatchingLabelsSelector) ApplyToList(opts *ListOptions) {
	opts.selector = m
}

// List will list all objects in the informers' caches, across all watched
// namespaces, matching the given label selector and set them in the Items field
// of the provided list object.
func (i *multiNamespaceInformer) List(out runtime.Object, opts ...ListOption) error {
	listOpts := DefaultListOptions().ApplyOptions(opts...)

	var objects []interface{}
	var err error

	if listOpts.namespace != "" {
		if objects, err = i.listNamespace(listOpts.namespace, listOpts.selector); err != nil {
			return err
		}
	} else {
		if objects, err = i.listAllNamespaces(listOpts.selector); err != nil {
			return err
		}
	}

	runtimeObjects := make([]runtime.Object, 0, len(objects))

	for _, obj := range objects {
		runtimeObj, ok := obj.(runtime.Object)
		if !ok {
			return fmt.Errorf("cache contained %T, which is not an Object", obj)
		}
		runtimeObjects = append(runtimeObjects, runtimeObj)
	}

	return apimeta.SetList(out, runtimeObjects)
}

// listAllNamespaces lists objects across all namespaces the informer knows about.
func (i *multiNamespaceInformer) listAllNamespaces(s labels.Selector) ([]interface{}, error) {
	var objects []interface{}

	for ns := range i.namespaceToInformer {
		objs, err := i.listNamespace(ns, s)
		if err != nil {
			return nil, err
		}

		objects = append(objects, objs...)
	}

	return objects, nil
}

// listNamespace lists all objects in the given namespace matching the label selector.
func (i *multiNamespaceInformer) listNamespace(ns string, s labels.Selector) ([]interface{}, error) {
	var objects []interface{}

	indexer, err := i.indexerForNamespace(ns)
	if err != nil {
		return nil, err
	}

	cache.ListAllByNamespace(indexer, ns, s, func(obj interface{}) {
		objects = append(objects, obj)
	})

	return objects, nil
}

// indexerForNamespace returns the indexer from the informer for the given namespace.
func (i *multiNamespaceInformer) indexerForNamespace(ns string) (cache.Indexer, error) {
	var indexer cache.Indexer

	if informer, ok := i.namespaceToInformer[metav1.NamespaceAll]; ok {
		indexer = informer.GetIndexer() // Use the informer for all namespaces.
	} else if informer, ok := i.namespaceToInformer[ns]; ok {
		indexer = informer.GetIndexer() // Use the namespace specific informer.
	} else {
		return nil, fmt.Errorf("namespace %q not found in cache", ns)
	}

	return indexer, nil
}
