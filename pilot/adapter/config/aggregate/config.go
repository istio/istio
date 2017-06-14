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

	"github.com/golang/protobuf/proto"

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
	stores := make([]model.ConfigStore, len(caches))
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

func (cr *store) Get(typ, key string) (proto.Message, bool, string) {
	store, exists := cr.stores[typ]
	if !exists {
		return nil, false, ""
	}
	return store.Get(typ, key)
}

func (cr *store) List(typ string) ([]model.Config, error) {
	store, exists := cr.stores[typ]
	if !exists {
		return nil, nil
	}
	return store.List(typ)
}

func (cr *store) Delete(typ, key string) error {
	store, exists := cr.stores[typ]
	if !exists {
		return fmt.Errorf("missing type %q", typ)
	}
	return store.Delete(typ, key)
}

func (cr *store) Post(config proto.Message) (string, error) {
	schema, exists := cr.descriptor.GetByMessageName(proto.MessageName(config))
	if !exists {
		return "", errors.New("missing type")
	}
	store := cr.stores[schema.Type]
	return store.Post(config)
}

func (cr *store) Put(config proto.Message, oldRevision string) (string, error) {
	schema, exists := cr.descriptor.GetByMessageName(proto.MessageName(config))
	if !exists {
		return "", errors.New("missing type")
	}
	store := cr.stores[schema.Type]
	return store.Put(config, oldRevision)
}

type storeCache struct {
	store  model.ConfigStore
	caches []model.ConfigStoreCache
}

func (cr *storeCache) ConfigDescriptor() model.ConfigDescriptor {
	return cr.store.ConfigDescriptor()
}

func (cr *storeCache) Get(typ, key string) (config proto.Message, exists bool, revision string) {
	return cr.store.Get(typ, key)
}

func (cr *storeCache) List(typ string) ([]model.Config, error) {
	return cr.store.List(typ)
}

func (cr *storeCache) Post(val proto.Message) (string, error) {
	return cr.store.Post(val)
}

func (cr *storeCache) Put(val proto.Message, revision string) (string, error) {
	return cr.store.Put(val, revision)
}

func (cr *storeCache) Delete(typ, key string) error {
	return cr.store.Delete(typ, key)
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
