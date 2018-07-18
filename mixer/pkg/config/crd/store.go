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
	"fmt"
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
	// initWaiterInterval is the interval to check if the initial data is ready
	// in the cache.
	initWaiterInterval = time.Millisecond

	// crdRetryTimeout is the default timeout duration to retry initialization
	// of the caches when some CRDs are missing. The timeout can be customized
	// through "retry-timeout" query parameter in the config URL,
	// like k8s://?retry-timeout=1m
	crdRetryTimeout = time.Second * 30

	// ConfigAPIGroup is the API group for the config CRDs.
	ConfigAPIGroup = "config.istio.io"
	// ConfigAPIVersion is the API version for the config CRDs.
	ConfigAPIVersion = "v1alpha2"
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
	conf            *rest.Config
	ns              map[string]bool
	retryTimeout    time.Duration
	donec           chan struct{}
	apiGroupVersion string

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

// checkAndCreateCaches checks the presence of custom resource definitions through the discovery API,
// and then create caches through lwBUilder which is in kinds. It retries as long as retryDone channel
// is open.
// Returns the created shared informers, and the list of kinds which are not created yet.
func (s *Store) checkAndCreateCaches(
	retryDone chan struct{},
	d discovery.DiscoveryInterface,
	lwBuilder listerWatcherBuilderInterface,
	kinds []string) []string {
	kindsSet := map[string]bool{}
	for _, k := range kinds {
		kindsSet[k] = true
	}
	informers := map[string]cache.SharedInformer{}
	retryCount := 0
loop:
	for added := 0; added < len(kinds); {
		select {
		case <-retryDone:
			break loop
		default:
		}
		if retryCount > 0 {
			if retryCount%logPerRetries == 1 {
				remainingKeys := make([]string, 0, len(kinds))
				for k := range kindsSet {
					remainingKeys = append(remainingKeys, k)
				}
				log.Debugf("Retrying to fetch config: %+v", remainingKeys)
			}
			time.Sleep(s.retryInterval)
		}
		retryCount++
		groupVersion := ConfigAPIGroup + "/" + ConfigAPIVersion
		if s.apiGroupVersion != "" {
			groupVersion = s.apiGroupVersion
		}
		resources, err := d.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			log.Debugf("Failed to obtain resources for CRD: %v", err)
			continue
		}
		s.cacheMutex.Lock()
		for _, res := range resources.APIResources {

			if _, ok := s.caches[res.Kind]; ok {
				continue
			}
			if _, ok := kindsSet[res.Kind]; ok {
				cl := lwBuilder.build(res)
				informer := cache.NewSharedInformer(cl, &unstructured.Unstructured{}, 0)
				s.caches[res.Kind] = informer.GetStore()
				informers[res.Kind] = informer
				delete(kindsSet, res.Kind)
				informer.AddEventHandler(s)
				go informer.Run(s.donec)
				added++
			}
		}
		s.cacheMutex.Unlock()
	}
	<-waitForSynced(retryDone, informers)
	remaining := make([]string, 0, len(kindsSet))
	for k := range kindsSet {
		remaining = append(remaining, k)
	}
	var err error
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
	s.caches = make(map[string]cache.Store, len(kinds))
	timeout := time.After(s.retryTimeout)
	timeoutdone := make(chan struct{})
	s.retryInterval = time.Second / 2
	go func() {
		<-timeout
		close(timeoutdone)
	}()
	remainingKinds := s.checkAndCreateCaches(timeoutdone, d, lwBuilder, kinds)
	if len(remainingKinds) > 0 {
		// Wait asynchronously for other kinds.
		go s.checkAndCreateCaches(s.donec, d, lwBuilder, remainingKinds)
	}
	return nil
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
