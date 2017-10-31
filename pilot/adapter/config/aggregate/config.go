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

// Package aggregate implements a type-aggregator for config stores.  The
// aggregate config store multiplexes requests to a configuration store based
// on the type of the configuration objects. The aggregate config store cache
// performs the reverse, by aggregating events from the multiplexed stores and
// dispatching them back to event handlers.
package aggregate

import (
	"errors"
	"fmt"

	"istio.io/pilot/model"
)

// Make creates an aggregate config store from several config stores and
// unifies their descriptors
func Make(stores []model.ConfigStore) (model.ConfigStore, error) {
	union := model.ConfigDescriptor{}
	storeTypes := make(map[string]model.ConfigStore)
	for _, store := range stores {
		for _, descriptor := range store.ConfigDescriptor() {
			union = append(union, descriptor)
			storeTypes[descriptor.Type] = store
		}
	}
	if err := union.Validate(); err != nil {
		return nil, err
	}
	return &store{
		descriptor: union,
		stores:     storeTypes,
	}, nil
}

// MakeCache creates an aggregate config store cache from several config store
// caches.
func MakeCache(caches []model.ConfigStoreCache) (model.ConfigStoreCache, error) {
	stores := make([]model.ConfigStore, 0, len(caches))
	for _, cache := range caches {
		stores = append(stores, cache)
	}
	store, err := Make(stores)
	if err != nil {
		return nil, err
	}
	return &storeCache{
		store:  store,
		caches: caches,
	}, nil
}

type store struct {
	// descriptor is the unified
	descriptor model.ConfigDescriptor

	// stores is a mapping from config type to a store
	stores map[string]model.ConfigStore
}

func (cr *store) ConfigDescriptor() model.ConfigDescriptor {
	return cr.descriptor
}

func (cr *store) Get(typ, name, namespace string) (*model.Config, bool) {
	store, exists := cr.stores[typ]
	if !exists {
		return nil, false
	}
	return store.Get(typ, name, namespace)
}

func (cr *store) List(typ, namespace string) ([]model.Config, error) {
	store, exists := cr.stores[typ]
	if !exists {
		return nil, nil
	}
	return store.List(typ, namespace)
}

func (cr *store) Delete(typ, name, namespace string) error {
	store, exists := cr.stores[typ]
	if !exists {
		return fmt.Errorf("missing type %q", typ)
	}
	return store.Delete(typ, name, namespace)
}

func (cr *store) Create(config model.Config) (string, error) {
	store, exists := cr.stores[config.Type]
	if !exists {
		return "", errors.New("missing type")
	}
	return store.Create(config)
}

func (cr *store) Update(config model.Config) (string, error) {
	store, exists := cr.stores[config.Type]
	if !exists {
		return "", errors.New("missing type")
	}
	return store.Update(config)
}

type storeCache struct {
	store  model.ConfigStore
	caches []model.ConfigStoreCache
}

func (cr *storeCache) ConfigDescriptor() model.ConfigDescriptor {
	return cr.store.ConfigDescriptor()
}

func (cr *storeCache) Get(typ, name, namespace string) (config *model.Config, exists bool) {
	return cr.store.Get(typ, name, namespace)
}

func (cr *storeCache) List(typ, namespace string) ([]model.Config, error) {
	return cr.store.List(typ, namespace)
}

func (cr *storeCache) Create(config model.Config) (string, error) {
	return cr.store.Create(config)
}

func (cr *storeCache) Update(config model.Config) (string, error) {
	return cr.store.Update(config)
}

func (cr *storeCache) Delete(typ, name, namespace string) error {
	return cr.store.Delete(typ, name, namespace)
}

func (cr *storeCache) HasSynced() bool {
	for _, cache := range cr.caches {
		if !cache.HasSynced() {
			return false
		}
	}
	return true
}

func (cr *storeCache) RegisterEventHandler(typ string, handler func(model.Config, model.Event)) {
	for _, cache := range cr.caches {
		if _, exists := cache.ConfigDescriptor().GetByType(typ); exists {
			cache.RegisterEventHandler(typ, handler)
			return
		}
	}
}

func (cr *storeCache) Run(stop <-chan struct{}) {
	for _, cache := range cr.caches {
		go cache.Run(stop)
	}
	<-stop
}
