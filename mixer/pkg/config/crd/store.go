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
	// defaultResyncPeriod is the resync period for the k8s cache.
	// TODO: allow customization.
	defaultResyncPeriod = time.Minute

	// initWaiterInterval is the interval to check if the initial data is ready
	// in the cache.
	initWaiterInterval = time.Millisecond

	// crdRetryTimeout is the default timeout duration to retry initialization
	// of the caches when some CRDs are missing. The timeout can be customized
	// through "retry-timeout" query parameter in the config URL,
	// like k8s://?retry-timeout=1m
	crdRetryTimeout = time.Second * 30
)

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
	caches       map[string]cache.Store
	watchCtx     context.Context
	watchCh      chan store.BackendEvent
	retryTimeout time.Duration

	// They are used to inject testing interfaces.
	discoveryBuilder     func(conf *rest.Config) (discovery.DiscoveryInterface, error)
	listerWatcherBuilder func(conf *rest.Config) (listerWatcherBuilderInterface, error)
}

var _ store.Store2Backend = &Store{}

// Init implements store.Store2Backend interface.
func (s *Store) Init(ctx context.Context, kinds []string) error {
	kindsSet := map[string]bool{}
	for _, kind := range kinds {
		kindsSet[kind] = true
	}
	d, err := s.discoveryBuilder(s.conf)
	if err != nil {
		return err
	}
	lwBuilder, err := s.listerWatcherBuilder(s.conf)
	if err != nil {
		return err
	}
	s.caches = make(map[string]cache.Store, len(kinds))
	crdCtx, cancel := context.WithTimeout(ctx, s.retryTimeout)
	defer cancel()
	retry := false
	informers := map[string]cache.SharedInformer{}
	for len(s.caches) < len(kinds) {
		if crdCtx.Err() != nil {
			// TODO: runs goroutines for remaining kinds.
			break
		}
		if retry {
			glog.V(3).Infof("Retrying to fetch config...")
		}
		resources, err := d.ServerResourcesForGroupVersion(apiGroupVersion)
		if err != nil {
			glog.V(3).Infof("Failed to obtain resources for CRD: %v", err)
			continue
		}
		for _, res := range resources.APIResources {
			if _, ok := s.caches[res.Kind]; ok {
				continue
			}
			if _, ok := kindsSet[res.Kind]; ok {
				cl := lwBuilder.build(res)
				informer := cache.NewSharedInformer(cl, &unstructured.Unstructured{}, defaultResyncPeriod)
				s.caches[res.Kind] = informer.GetStore()
				informers[res.Kind] = informer
				informer.AddEventHandler(s)
				go informer.Run(ctx.Done())
			}
		}
		retry = true
	}
	if len(s.caches) > 0 {
		<-waitForSynced(ctx, informers)
	}
	return nil
}

// Watch implements store.Store2Backend interface.
func (s *Store) Watch(ctx context.Context) (<-chan store.BackendEvent, error) {
	s.watchCtx = ctx
	s.watchCh = make(chan store.BackendEvent)
	return s.watchCh, nil
}

// Get implements store.Store2Backend interface.
func (s *Store) Get(key store.Key) (map[string]interface{}, error) {
	c, ok := s.caches[key.Kind]
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
	val, _ := obj.(*unstructured.Unstructured).UnstructuredContent()["spec"].(map[string]interface{})
	return val, nil
}

// List implements store.Store2Backend interface.
func (s *Store) List() map[store.Key]map[string]interface{} {
	result := map[store.Key]map[string]interface{}{}
	for kind, c := range s.caches {
		for _, obj := range c.List() {
			uns := obj.(*unstructured.Unstructured)
			val, _ := uns.UnstructuredContent()["spec"].(map[string]interface{})
			key := store.Key{Kind: kind, Name: uns.GetName(), Namespace: uns.GetNamespace()}
			result[key] = val
		}
	}
	return result
}

func toEvent(t store.ChangeType, obj interface{}) store.BackendEvent {
	uns := obj.(*unstructured.Unstructured)
	val, _ := uns.UnstructuredContent()["spec"].(map[string]interface{})
	return store.BackendEvent{
		Type:  t,
		Key:   store.Key{Kind: uns.GetKind(), Namespace: uns.GetNamespace(), Name: uns.GetName()},
		Value: val,
	}
}

func (s *Store) dispatch(ev store.BackendEvent) {
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
