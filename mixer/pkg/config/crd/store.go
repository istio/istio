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
	"context"
	"sync"
	"time"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"istio.io/mixer/pkg/config/store"
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

// The interval to wait between the attempt to initialize caches. This is not const
// to allow changing the value for unittests.
var retryInterval = time.Second / 2

type listerWatcherBuilderInterface interface {
	build(res metav1.APIResource) cache.ListerWatcher
}

func waitForSynced(ctx context.Context, informers map[string]cache.SharedInformer) <-chan struct{} {
	out := make(chan struct{})
	go func() {
		tick := time.NewTicker(initWaiterInterval)
	loop:
		for len(informers) > 0 {
			select {
			case <-ctx.Done():
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

// Store offers store.Store2Backend interface through kubernetes custom resource definitions.
type Store struct {
	conf         *rest.Config
	ns           map[string]bool
	retryTimeout time.Duration

	cacheMutex sync.Mutex
	caches     map[string]cache.Store

	watchMutex sync.RWMutex
	watchCtx   context.Context
	watchCh    chan store.BackendEvent

	// They are used to inject testing interfaces.
	discoveryBuilder     func(conf *rest.Config) (discovery.DiscoveryInterface, error)
	listerWatcherBuilder func(conf *rest.Config) (listerWatcherBuilderInterface, error)
}

var _ store.Store2Backend = &Store{}

// checkAndCreateCaches checks the presence of custom resource definitions through the discovery API,
// and then create caches through lwBUilder which is in kinds. It retries within the timeout duration.
// If the timeout duration is 0, it waits forever (which should be done within a goroutine).
// Returns the created shared informers, and the list of kinds which are not created yet.
func (s *Store) checkAndCreateCaches(
	ctx context.Context,
	timeout time.Duration,
	d discovery.DiscoveryInterface,
	lwBuilder listerWatcherBuilderInterface,
	kinds []string) (informers map[string]cache.SharedInformer, remaining []string) {
	kindsSet := map[string]bool{}
	for _, k := range kinds {
		kindsSet[k] = true
	}
	informers = map[string]cache.SharedInformer{}
	retry := false
	retryCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		retryCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	for added := 0; added < len(kinds); {
		if retryCtx.Err() != nil {
			break
		}
		if retry {
			glog.V(4).Infof("Retrying to fetch config...")
			time.Sleep(retryInterval)
		}
		resources, err := d.ServerResourcesForGroupVersion(apiGroupVersion)
		if err != nil {
			glog.V(3).Infof("Failed to obtain resources for CRD: %v", err)
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
				go informer.Run(ctx.Done())
				added++
			}
		}
		s.cacheMutex.Unlock()
		retry = true
	}
	remaining = make([]string, 0, len(kindsSet))
	for k := range kindsSet {
		remaining = append(remaining, k)
	}
	return informers, remaining
}

// Init implements store.Store2Backend interface.
func (s *Store) Init(ctx context.Context, kinds []string) error {
	d, err := s.discoveryBuilder(s.conf)
	if err != nil {
		return err
	}
	lwBuilder, err := s.listerWatcherBuilder(s.conf)
	if err != nil {
		return err
	}
	s.caches = make(map[string]cache.Store, len(kinds))
	informers, remainingKinds := s.checkAndCreateCaches(ctx, s.retryTimeout, d, lwBuilder, kinds)
	if len(remainingKinds) > 0 {
		// Wait asynchronously for other kinds.
		go s.checkAndCreateCaches(ctx, 0, d, lwBuilder, remainingKinds)
	}
	<-waitForSynced(ctx, informers)
	return nil
}

// Watch implements store.Store2Backend interface.
func (s *Store) Watch(ctx context.Context) (<-chan store.BackendEvent, error) {
	ch := make(chan store.BackendEvent)
	s.watchMutex.Lock()
	s.watchCtx = ctx
	s.watchCh = ch
	s.watchMutex.Unlock()
	return ch, nil
}

// Get implements store.Store2Backend interface.
func (s *Store) Get(key store.Key) (*store.BackEndResource, error) {
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
	return backEndResource(uns), nil
}

func backEndResource(uns *unstructured.Unstructured) *store.BackEndResource {
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

// List implements store.Store2Backend interface.
func (s *Store) List() map[store.Key]*store.BackEndResource {
	result := make(map[store.Key]*store.BackEndResource)
	s.cacheMutex.Lock()
	for kind, c := range s.caches {
		for _, obj := range c.List() {
			uns := obj.(*unstructured.Unstructured)
			key := store.Key{Kind: kind, Name: uns.GetName(), Namespace: uns.GetNamespace()}
			result[key] = backEndResource(uns)
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
		Value: backEndResource(uns),
	}
}

func (s *Store) dispatch(ev store.BackendEvent) {
	s.watchMutex.RLock()
	defer s.watchMutex.RUnlock()
	if s.watchCtx == nil {
		return
	}
	select {
	case <-s.watchCtx.Done():
	case s.watchCh <- ev:
	}
}

// OnAdd implements cache.ResourceEventHandler interface.
func (s *Store) OnAdd(obj interface{}) {
	s.dispatch(toEvent(store.Update, obj))
}

// OnUpdate implements cache.ResourceEventHandler interface.
func (s *Store) OnUpdate(oldObj, newObj interface{}) {
	s.dispatch(toEvent(store.Update, newObj))
}

// OnDelete implements cache.ResourceEventHandler interface.
func (s *Store) OnDelete(obj interface{}) {
	ev := toEvent(store.Delete, obj)
	ev.Value = nil
	s.dispatch(ev)
}
