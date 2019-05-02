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

package internal

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// clientListWatcherFunc knows how to create a ListWatcher
type createListWatcherFunc func(gvk schema.GroupVersionKind, ip *specificInformersMap) (*cache.ListWatch, error)

// newSpecificInformersMap returns a new specificInformersMap (like
// the generical InformersMap, except that it doesn't implement WaitForCacheSync).
func newSpecificInformersMap(config *rest.Config,
	scheme *runtime.Scheme,
	mapper meta.RESTMapper,
	resync time.Duration,
	namespace string,
	createListWatcher createListWatcherFunc) *specificInformersMap {
	ip := &specificInformersMap{
		config:            config,
		Scheme:            scheme,
		mapper:            mapper,
		informersByGVK:    make(map[schema.GroupVersionKind]*MapEntry),
		codecs:            serializer.NewCodecFactory(scheme),
		paramCodec:        runtime.NewParameterCodec(scheme),
		resync:            resync,
		createListWatcher: createListWatcher,
		namespace:         namespace,
	}
	return ip
}

// MapEntry contains the cached data for an Informer
type MapEntry struct {
	// Informer is the cached informer
	Informer cache.SharedIndexInformer

	// CacheReader wraps Informer and implements the CacheReader interface for a single type
	Reader CacheReader
}

// specificInformersMap create and caches Informers for (runtime.Object, schema.GroupVersionKind) pairs.
// It uses a standard parameter codec constructed based on the given generated Scheme.
type specificInformersMap struct {
	// Scheme maps runtime.Objects to GroupVersionKinds
	Scheme *runtime.Scheme

	// config is used to talk to the apiserver
	config *rest.Config

	// mapper maps GroupVersionKinds to Resources
	mapper meta.RESTMapper

	// informersByGVK is the cache of informers keyed by groupVersionKind
	informersByGVK map[schema.GroupVersionKind]*MapEntry

	// codecs is used to create a new REST client
	codecs serializer.CodecFactory

	// paramCodec is used by list and watch
	paramCodec runtime.ParameterCodec

	// stop is the stop channel to stop informers
	stop <-chan struct{}

	// resync is the frequency the informers are resynced
	resync time.Duration

	// mu guards access to the map
	mu sync.RWMutex

	// start is true if the informers have been started
	started bool

	// createClient knows how to create a client and a list object,
	// and allows for abstracting over the particulars of structured vs
	// unstructured objects.
	createListWatcher createListWatcherFunc

	// namespace is the namespace that all ListWatches are restricted to
	// default or empty string means all namespaces
	namespace string
}

// Start calls Run on each of the informers and sets started to true.  Blocks on the stop channel.
// It doesn't return start because it can't return an error, and it's not a runnable directly.
func (ip *specificInformersMap) Start(stop <-chan struct{}) {
	func() {
		ip.mu.Lock()
		defer ip.mu.Unlock()

		// Set the stop channel so it can be passed to informers that are added later
		ip.stop = stop

		// Start each informer
		for _, informer := range ip.informersByGVK {
			go informer.Informer.Run(stop)
		}

		// Set started to true so we immediately start any informers added later.
		ip.started = true
	}()
	<-stop
}

// HasSyncedFuncs returns all the HasSynced functions for the informers in this map.
func (ip *specificInformersMap) HasSyncedFuncs() []cache.InformerSynced {
	ip.mu.RLock()
	defer ip.mu.RUnlock()
	syncedFuncs := make([]cache.InformerSynced, 0, len(ip.informersByGVK))
	for _, informer := range ip.informersByGVK {
		syncedFuncs = append(syncedFuncs, informer.Informer.HasSynced)
	}
	return syncedFuncs
}

// Get will create a new Informer and add it to the map of specificInformersMap if none exists.  Returns
// the Informer from the map.
func (ip *specificInformersMap) Get(gvk schema.GroupVersionKind, obj runtime.Object) (*MapEntry, error) {
	// Return the informer if it is found
	i, ok := func() (*MapEntry, bool) {
		ip.mu.RLock()
		defer ip.mu.RUnlock()
		i, ok := ip.informersByGVK[gvk]
		return i, ok
	}()
	if ok {
		return i, nil
	}

	// Do the mutex part in its own function so we can use defer without blocking pieces that don't
	// need to be locked
	var sync bool
	i, err := func() (*MapEntry, error) {
		ip.mu.Lock()
		defer ip.mu.Unlock()

		// Check the cache to see if we already have an Informer.  If we do, return the Informer.
		// This is for the case where 2 routines tried to get the informer when it wasn't in the map
		// so neither returned early, but the first one created it.
		var ok bool
		i, ok := ip.informersByGVK[gvk]
		if ok {
			return i, nil
		}

		// Create a NewSharedIndexInformer and add it to the map.
		var lw *cache.ListWatch
		lw, err := ip.createListWatcher(gvk, ip)
		if err != nil {
			return nil, err
		}
		ni := cache.NewSharedIndexInformer(lw, obj, ip.resync, cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		})
		i = &MapEntry{
			Informer: ni,
			Reader:   CacheReader{indexer: ni.GetIndexer(), groupVersionKind: gvk},
		}
		ip.informersByGVK[gvk] = i

		// Start the Informer if need by
		// TODO(seans): write thorough tests and document what happens here - can you add indexers?
		// can you add eventhandlers?
		if ip.started {
			sync = true
			go i.Informer.Run(ip.stop)
		}
		return i, nil
	}()
	if err != nil {
		return nil, err
	}

	if sync {
		// Wait for it to sync before returning the Informer so that folks don't read from a stale cache.
		if !cache.WaitForCacheSync(ip.stop, i.Informer.HasSynced) {
			return nil, fmt.Errorf("failed waiting for %T Informer to sync", obj)
		}
	}

	return i, err
}

// newListWatch returns a new ListWatch object that can be used to create a SharedIndexInformer.
func createStructuredListWatch(gvk schema.GroupVersionKind, ip *specificInformersMap) (*cache.ListWatch, error) {
	// Kubernetes APIs work against Resources, not GroupVersionKinds.  Map the
	// groupVersionKind to the Resource API we will use.
	mapping, err := ip.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}

	client, err := apiutil.RESTClientForGVK(gvk, ip.config, ip.codecs)
	if err != nil {
		return nil, err
	}
	listGVK := gvk.GroupVersion().WithKind(gvk.Kind + "List")
	listObj, err := ip.Scheme.New(listGVK)
	if err != nil {
		return nil, err
	}

	// Create a new ListWatch for the obj
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			res := listObj.DeepCopyObject()
			isNamespaceScoped := ip.namespace != "" && mapping.Scope.Name() != meta.RESTScopeNameRoot
			err := client.Get().NamespaceIfScoped(ip.namespace, isNamespaceScoped).Resource(mapping.Resource.Resource).VersionedParams(&opts, ip.paramCodec).Do().Into(res)
			return res, err
		},
		// Setup the watch function
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			// Watch needs to be set to true separately
			opts.Watch = true
			isNamespaceScoped := ip.namespace != "" && mapping.Scope.Name() != meta.RESTScopeNameRoot
			return client.Get().NamespaceIfScoped(ip.namespace, isNamespaceScoped).Resource(mapping.Resource.Resource).VersionedParams(&opts, ip.paramCodec).Watch()
		},
	}, nil
}

func createUnstructuredListWatch(gvk schema.GroupVersionKind, ip *specificInformersMap) (*cache.ListWatch, error) {
	// Kubernetes APIs work against Resources, not GroupVersionKinds.  Map the
	// groupVersionKind to the Resource API we will use.
	mapping, err := ip.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(ip.config)
	if err != nil {
		return nil, err
	}

	// Create a new ListWatch for the obj
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			if ip.namespace != "" && mapping.Scope.Name() != meta.RESTScopeNameRoot {
				return dynamicClient.Resource(mapping.Resource).Namespace(ip.namespace).List(opts)
			}
			return dynamicClient.Resource(mapping.Resource).List(opts)
		},
		// Setup the watch function
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			// Watch needs to be set to true separately
			opts.Watch = true
			if ip.namespace != "" && mapping.Scope.Name() != meta.RESTScopeNameRoot {
				return dynamicClient.Resource(mapping.Resource).Namespace(ip.namespace).Watch(opts)
			}
			return dynamicClient.Resource(mapping.Resource).Watch(opts)
		},
	}, nil
}
