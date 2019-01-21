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

// Package aggregate implements a read-only aggregator for config stores.
package aggregate

import (
	"errors"
	"fmt"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
)

var errorUnsupported = errors.New("unsupported operation: the config aggregator is read-only")

// Make creates an aggregate config store from several config stores and
// unifies their descriptors
func Make(stores []model.ConfigStore) (model.ConfigStore, error) {
	union := model.ConfigDescriptor{}
	storeTypes := make(map[string][]model.ConfigStore)
	for _, store := range stores {
		for _, descriptor := range store.ConfigDescriptor() {
			if len(storeTypes[descriptor.Type]) == 0 {
				union = append(union, descriptor)
			}
			storeTypes[descriptor.Type] = append(storeTypes[descriptor.Type], store)
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
		ConfigStore: store,
		caches:      caches,
	}, nil
}

type store struct {
	// descriptor is the unified
	descriptor model.ConfigDescriptor

	// stores is a mapping from config type to a store
	stores map[string][]model.ConfigStore
}

func (cr *store) ConfigDescriptor() model.ConfigDescriptor {
	return cr.descriptor
}

// Get the first config found in the stores.
func (cr *store) Get(typ, name, namespace string) *model.Config {
	for _, store := range cr.stores[typ] {
		config := store.Get(typ, name, namespace)
		if config != nil {
			return config
		}
	}
	return nil
}

// List all configs in the stores.
func (cr *store) List(typ, namespace string) ([]model.Config, error) {
	if len(cr.stores[typ]) == 0 {
		return nil, fmt.Errorf("missing type %q", typ)
	}
	var errs *multierror.Error
	var configs []model.Config
	// Used to remove duplicated config
	configMap := make(map[string]struct{})

	for _, store := range cr.stores[typ] {
		storeConfigs, err := store.List(typ, namespace)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
		for _, config := range storeConfigs {
			key := config.Type + config.Namespace + config.Name
			if _, exist := configMap[key]; exist {
				continue
			}
			configs = append(configs, config)
			configMap[key] = struct{}{}
		}
	}
	return configs, errs.ErrorOrNil()
}

func (cr *store) Delete(typ, name, namespace string) error {
	return errorUnsupported
}

func (cr *store) Create(config model.Config) (string, error) {
	return "", errorUnsupported
}

func (cr *store) Update(config model.Config) (string, error) {
	return "", errorUnsupported
}

type storeCache struct {
	model.ConfigStore
	caches []model.ConfigStoreCache
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
		}
	}
}

func (cr *storeCache) Run(stop <-chan struct{}) {
	for _, cache := range cr.caches {
		go cache.Run(stop)
	}
	<-stop
}
