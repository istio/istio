// Copyright 2017 Istio Authors
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

// Package crd provides the store interface to config resources stored as
// kubernetes custom resource definitions (CRDs).
package crd

import (
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
)

const (
	// API group / version for istio config.
	apiGroup        = "config.istio.io"
	apiVersion      = "v1alpha2"
	apiGroupVersion = apiGroup + "/" + apiVersion
)

const (
	// initWaiterInterval is the interval to check if the initial data is ready
	// in the cache.
	initWaiterInterval = time.Millisecond

	// crdRetryTimeout is the default timeout duration to retry initialization
	// of the caches when some CRDs are missing. The timeout can be customized
	// through "retry-timeout" query parameter in the config URL,
	// like k8s://?retry-timeout=1m
	crdRetryTimeout = time.Second * 30
)

// When retrying happens on initializing caches, it shouldn't log the message for
// every retry, it may flood the log messages if the initialization is never satisfied.
// see also https://github.com/istio/istio/issues/3138
const logPerRetries = 100

type listerWatcherBuilderInterface interface {
	build(res metav1.APIResource) cache.ListerWatcher
}

func waitForSynced(donec chan struct{}, informers map[string]cache.SharedInformer) <-chan struct{} {
	out := make(chan struct{})
	go func() {
		tick := time.NewTicker(initWaiterInterval)
	loop:
		for len(informers) > 0 {
			select {
			case <-donec:
				break loop
			case <-tick.C:
				for k, i := range informers {
					if i.HasSynced() {
						delete(informers, k)
					}
				}
			}
		}
		tick.Stop()
		close(out)
	}()
	return out
}

// Store offers store.StoreBackend interface through kubernetes custom resource definitions.
type Store struct {
	conf         *rest.Config
	ns           map[string]bool
	retryTimeout time.Duration
	donec        chan struct{}

	cacheMutex sync.Mutex
	caches     map[string]cache.Store

	watchMutex sync.RWMutex
	watchCh    chan store.BackendEvent

	// They are used to inject testing interfaces.
	discoveryBuilder     func(conf *rest.Config) (discovery.DiscoveryInterface, error)
	listerWatcherBuilder func(conf *rest.Config) (listerWatcherBuilderInterface, error)

	*probe.Probe

	// The interval to wait between the attempt to initialize caches. This is not const
	// to allow changing the value for unittests.
	retryInterval time.Duration
}

var _ store.Backend = new(Store)
var _ probe.SupportsProbe = new(Store)

// Stop implements store.Backend interface.
func (s *Store) Stop() {
	close(s.donec)
}

// continuallyCheckAndCreateCaches checks the presence of custom resource
// definitions through the discovery API, and then create caches through
// lwBuilder which is in kinds. It retries until all kinds are cached or the
// retryDone channel is closed.
func (s *Store) continuallyCheckAndCacheResources(
	kinds []string,
	retryDone chan struct{},
	d discovery.DiscoveryInterface,
	lwBuilder listerWatcherBuilderInterface) {

	uncheckedKindsSet := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		uncheckedKindsSet[kind] = true
	}

	informers := map[string]cache.SharedInformer{}
	retryCount := 0
loop:
	for len(uncheckedKindsSet) > 0 {
		select {
		case <-retryDone:
			break loop
		default:
		}

		if log.DebugEnabled() && retryCount%logPerRetries == 1 {
			remainingKinds := make([]string, 0, len(uncheckedKindsSet))
			for kind := range uncheckedKindsSet {
				remainingKinds = append(remainingKinds, kind)
			}
			log.Debugf("Retrying to fetch config: %+v", remainingKinds)
		}
		time.Sleep(s.retryInterval)
		retryCount++

		resources, err := d.ServerResourcesForGroupVersion(apiGroupVersion)
		if err != nil {
			log.Errorf("Failed to obtain resources for CRD: %v", err)
		}

		for _, resource := range resources.APIResources {
			kind := resource.Kind
			if _, ok := uncheckedKindsSet[kind]; ok {
				s.cacheResource(resource, lwBuilder, informers)
				delete(uncheckedKindsSet, kind)
			}
		}
	}
	<-waitForSynced(retryDone, informers)
}

// Init implements store.StoreBackend interface.
func (s *Store) Init(kinds []string) (err error) {
	d, err := s.discoveryBuilder(s.conf)
	if err != nil {
		return
	}
	lwBuilder, err := s.listerWatcherBuilder(s.conf)
	if err != nil {
		return
	}
	s.caches = make(map[string]cache.Store, len(kinds))
	timeout := time.After(s.retryTimeout)
	timeoutdone := make(chan struct{})
	s.retryInterval = time.Second / 2
	go func() {
		<-timeout
		close(timeoutdone)
	}()

	uncheckedKindsSet := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		uncheckedKindsSet[kind] = true
	}

	resources, err := d.ServerResourcesForGroupVersion(apiGroupVersion)
	if err != nil {
		log.Errorf("Failed to obtain resources for CRD: %v", err)
	}

	informers := map[string]cache.SharedInformer{}
	for _, resource := range resources.APIResources {
		kind := resource.Kind
		if _, ok := uncheckedKindsSet[kind]; ok {
			s.cacheResource(resource, lwBuilder, informers)
			delete(uncheckedKindsSet, kind)
		}
	}
	<-waitForSynced(timeoutdone, informers)
	s.declareReadiness()

	// unregisteredKinds are kinds which are not yet resources. These are not
	// required to declare readiness, but will be checked for periodically and
	// indefinitely.
	unregisteredKinds := make([]string, 0, len(uncheckedKindsSet))
	for kind := range uncheckedKindsSet {
		unregisteredKinds = append(unregisteredKinds, kind)
	}
	go s.continuallyCheckAndCacheResources(
		unregisteredKinds, s.donec, d, lwBuilder)

	return nil
}

// cacheResource creates the cache for resource through lwBuilder and adds it to
// informers, provided resource is not already in the caches.
func (s *Store) cacheResource(
	resource metav1.APIResource,
	lwBuilder listerWatcherBuilderInterface,
	informers map[string]cache.SharedInformer) {

	kind := resource.Kind
	if _, ok := s.caches[kind]; !ok {
		cacheListerWatcher := lwBuilder.build(resource)
		informer := cache.NewSharedInformer(
			cacheListerWatcher, &unstructured.Unstructured{}, 0)
		s.cacheMutex.Lock()
		s.caches[kind] = informer.GetStore()
		s.cacheMutex.Unlock()
		informers[kind] = informer
		informer.AddEventHandler(s)
		go informer.Run(s.donec)
	}
}

func (s *Store) declareReadiness() {
	log.Debugf("Mixer store declaring readiness.")
	s.SetAvailable(nil)
}

// Watch implements store.Backend interface.
func (s *Store) Watch() (<-chan store.BackendEvent, error) {
	ch := make(chan store.BackendEvent)
	s.watchMutex.Lock()
	s.watchCh = ch
	s.watchMutex.Unlock()
	return ch, nil
}

// Get implements store.Backend interface.
func (s *Store) Get(key store.Key) (*store.BackEndResource, error) {
	if s.ns != nil && !s.ns[key.Namespace] {
		return nil, store.ErrNotFound
	}
	s.cacheMutex.Lock()
	c, ok := s.caches[key.Kind]
	s.cacheMutex.Unlock()
	if !ok {
		return nil, store.ErrNotFound
	}
	req := &unstructured.Unstructured{}
	req.SetName(key.Name)
	req.SetNamespace(key.Namespace)
	obj, exists, err := c.Get(req)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, store.ErrNotFound
	}
	uns, _ := obj.(*unstructured.Unstructured)
	return ToBackEndResource(uns), nil
}

// ToBackEndResource converts an unstructured k8s resource into a mixer backend resource.
func ToBackEndResource(uns *unstructured.Unstructured) *store.BackEndResource {
	spec, _ := uns.UnstructuredContent()["spec"].(map[string]interface{})
	return &store.BackEndResource{
		Metadata: store.ResourceMeta{
			Name:        uns.GetName(),
			Namespace:   uns.GetNamespace(),
			Labels:      uns.GetLabels(),
			Annotations: uns.GetAnnotations(),
			Revision:    uns.GetResourceVersion(),
		},
		Spec: spec,
	}
}

// List implements store.Backend interface.
func (s *Store) List() map[store.Key]*store.BackEndResource {
	result := make(map[store.Key]*store.BackEndResource)
	s.cacheMutex.Lock()
	for kind, c := range s.caches {
		for _, obj := range c.List() {
			uns := obj.(*unstructured.Unstructured)
			key := store.Key{Kind: kind, Name: uns.GetName(), Namespace: uns.GetNamespace()}
			if s.ns != nil && !s.ns[key.Namespace] {
				continue
			}
			result[key] = ToBackEndResource(uns)
		}
	}
	s.cacheMutex.Unlock()
	return result
}

func toEvent(t store.ChangeType, obj interface{}) store.BackendEvent {
	uns := obj.(*unstructured.Unstructured)
	key := store.Key{Kind: uns.GetKind(), Namespace: uns.GetNamespace(), Name: uns.GetName()}
	return store.BackendEvent{
		Type:  t,
		Key:   key,
		Value: ToBackEndResource(uns),
	}
}

func (s *Store) dispatch(ev store.BackendEvent) {
	s.watchMutex.RLock()
	defer s.watchMutex.RUnlock()
	if s.watchCh == nil {
		return
	}
	select {
	case <-s.donec:
	case s.watchCh <- ev:
	}
}

// OnAdd implements cache.ResourceEventHandler interface.
func (s *Store) OnAdd(obj interface{}) {
	ev := toEvent(store.Update, obj)
	if s.ns == nil || s.ns[ev.Key.Namespace] {
		s.dispatch(ev)
	}
}

// OnUpdate implements cache.ResourceEventHandler interface.
func (s *Store) OnUpdate(oldObj, newObj interface{}) {
	ev := toEvent(store.Update, newObj)
	if s.ns == nil || s.ns[ev.Key.Namespace] {
		s.dispatch(ev)
	}
}

// OnDelete implements cache.ResourceEventHandler interface.
func (s *Store) OnDelete(obj interface{}) {
	ev := toEvent(store.Delete, obj)
	ev.Value = nil
	if s.ns == nil || s.ns[ev.Key.Namespace] {
		s.dispatch(ev)
	}
}
