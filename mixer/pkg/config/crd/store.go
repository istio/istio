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

// Package crd provides the store interface to config resources stored as
// kubernetes custom resource definitions (CRDs).
package crd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/watch"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"
)

const (
	// crdRetryTimeout is the default timeout duration to retry initialization
	// of the caches when some CRDs are missing. The timeout can be customized
	// through "retry-timeout" query parameter in the config URL,
	// like k8s://?retry-timeout=1m
	crdRetryTimeout = time.Second * 30

	// crdRetryInterval is the default retry interval between the attempt to
	// initialize caches
	crdRetryInterval = time.Second

	// crdBgRetryInterval is the default retry interval between the attempts to
	// initialize cache for crd kinds that are not ready at store initialization.
	crdBgRetryInterval = time.Second * 10

	// ConfigAPIGroup is the API group for the config CRDs.
	ConfigAPIGroup = "config.istio.io"
	// ConfigAPIVersion is the API version for the config CRDs.
	ConfigAPIVersion = "v1alpha2"
)

type listerWatcherBuilderInterface interface {
	build(res metav1.APIResource) dynamic.ResourceInterface
}

// Store offers store.StoreBackend interface through kubernetes custom resource definitions.
type Store struct {
	conf            *rest.Config
	ns              map[string]bool
	retryTimeout    time.Duration
	donec           chan struct{}
	apiGroupVersion string

	cacheMutex sync.RWMutex
	caches     map[string]cache.Store
	informers  map[string]cache.SharedInformer

	watchMutex sync.RWMutex
	watchCh    chan store.BackendEvent

	// They are used to inject testing interfaces.
	discoveryBuilder     func(conf *rest.Config) (discovery.DiscoveryInterface, error)
	listerWatcherBuilder func(conf *rest.Config) (listerWatcherBuilderInterface, error)

	*probe.Probe

	// The interval to wait between the attempt to initialize caches. This is not const
	// to allow changing the value for unittests.
	retryInterval time.Duration

	// The interval to wait between the attempt to initialize caches for crd kinds that
	// are not ready at store initialization. This is not const to allow changing the
	// value for unittests.
	bgRetryInterval time.Duration

	// criticalkinds are the kinds that are critical for mixer function and must be ready
	// for store initialization.
	criticalKinds []string
}

var _ store.Backend = new(Store)
var _ probe.SupportsProbe = new(Store)

// Stop implements store.Backend interface.
func (s *Store) Stop() {
	close(s.donec)
}

// checkAndCreateCaches checks the presence of custom resource definitions through the discovery API,
// and then create caches through lwBUilder which is in kinds.
// Returns the created shared informers, and the list of kinds which are not created yet.
func (s *Store) checkAndCreateCaches(
	d discovery.DiscoveryInterface,
	lwBuilder listerWatcherBuilderInterface,
	kinds []string) []string {
	kindsSet := map[string]bool{}
	for _, k := range kinds {
		kindsSet[k] = true
	}
	groupVersion := ConfigAPIGroup + "/" + ConfigAPIVersion
	if s.apiGroupVersion != "" {
		groupVersion = s.apiGroupVersion
	}

	resources, err := d.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		log.Warnf("Failed to obtain resources for CRD: %v", err)
		return kinds
	}
	s.cacheMutex.Lock()
	for _, res := range resources.APIResources {
		if _, ok := s.caches[res.Kind]; ok {
			continue
		}
		if _, ok := kindsSet[res.Kind]; ok {
			res.Group = ConfigAPIGroup
			res.Version = ConfigAPIVersion
			cl := lwBuilder.build(res)
			informer := cache.NewSharedInformer(
				&cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return cl.List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (w watch.Interface, err error) {
						return cl.Watch(context.TODO(), options)
					},
				},
				&unstructured.Unstructured{}, 0)
			s.caches[res.Kind] = informer.GetStore()
			s.informers[res.Kind] = informer
			delete(kindsSet, res.Kind)
			informer.AddEventHandler(s)
			go informer.Run(s.donec)
		}
	}
	s.cacheMutex.Unlock()

	remaining := make([]string, 0, len(kindsSet))
	for k := range kindsSet {
		remaining = append(remaining, k)
	}
	if len(remaining) > 0 {
		err = fmt.Errorf("not yet ready: %+v", remaining)
	}
	s.SetAvailable(err)
	return remaining
}

// Init implements store.StoreBackend interface.
func (s *Store) Init(kinds []string) error {
	d, err := s.discoveryBuilder(s.conf)
	if err != nil {
		return err
	}
	lwBuilder, err := s.listerWatcherBuilder(s.conf)
	if err != nil {
		return err
	}

	log.Infof("Kinds: %#v", kinds)

	s.caches = make(map[string]cache.Store, len(kinds))
	s.informers = make(map[string]cache.SharedInformer, len(kinds))
	remaining := s.checkAndCreateCaches(d, lwBuilder, kinds)
	timeout := time.After(s.retryTimeout)
	ticker := time.NewTicker(s.retryInterval)
	defer ticker.Stop()
	stopRetry := false
	for len(s.extractCriticalKinds(remaining)) != 0 && !stopRetry {
		select {
		case <-timeout:
			stopRetry = true
		case <-ticker.C:
			remaining = s.checkAndCreateCaches(d, lwBuilder, remaining)
		}
	}
	ticker.Stop()
	if len(remaining) > 0 {
		if cks := s.extractCriticalKinds(remaining); len(cks) != 0 {
			return fmt.Errorf("failed to discover critical kinds: %v", cks)
		}
		log.Warnf("Failed to discover kinds: %v, start retry in background", remaining)
		go s.retryCreateCache(d, lwBuilder, remaining)
	}
	return nil
}

func (s *Store) extractCriticalKinds(r []string) []string {
	cks := make([]string, 0, len(r))
	for _, k := range r {
		for _, ck := range s.criticalKinds {
			if ck == k {
				cks = append(cks, ck)
			}
		}
	}
	return cks
}

func (s *Store) retryCreateCache(
	d discovery.DiscoveryInterface,
	lwBuilder listerWatcherBuilderInterface,
	kinds []string) {
	remaining := kinds
	ticker := time.NewTicker(s.bgRetryInterval)
	defer ticker.Stop()
	stopRetry := false

	for len(remaining) != 0 && !stopRetry {
		select {
		case <-s.donec:
			stopRetry = true
		case <-ticker.C:
			rm := s.checkAndCreateCaches(d, lwBuilder, remaining)
			if len(rm) < len(remaining) {
				log.Debugf("discovered %v new kinds, remaining undiscovered kinds: %v", len(remaining)-len(rm), rm)
			}
			remaining = rm
		}
	}
}

// WaitForSynced implements store.WaitForSynced interface
func (s *Store) WaitForSynced(timeout time.Duration) error {
	stop := time.After(timeout)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-stop:
			return fmt.Errorf("exceeded timeout %v", timeout)
		case <-tick.C:
			synced := true
			for _, i := range s.informers {
				if !i.HasSynced() {
					synced = false
					break
				}
			}
			if synced {
				return nil
			}
		}
	}
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
	s.cacheMutex.RLock()
	c, ok := s.caches[key.Kind]
	s.cacheMutex.RUnlock()
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
	s.cacheMutex.RLock()
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
	s.cacheMutex.RUnlock()
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
