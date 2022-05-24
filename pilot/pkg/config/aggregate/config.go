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

// Package aggregate implements a read-only aggregator for config stores.
package aggregate

import (
	"errors"

	"github.com/hashicorp/go-multierror"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/util/sets"
)

var errorUnsupported = errors.New("unsupported operation: the config aggregator is read-only")

// makeStore creates an aggregate config store from several config stores and
// unifies their descriptors
func makeStore(stores []model.ConfigStore, writer model.ConfigStore) (model.ConfigStore, error) {
	union := collection.NewSchemasBuilder()
	storeTypes := make(map[config.GroupVersionKind][]model.ConfigStore)
	for _, store := range stores {
		for _, s := range store.Schemas().All() {
			if len(storeTypes[s.Resource().GroupVersionKind()]) == 0 {
				if err := union.Add(s); err != nil {
					return nil, err
				}
			}
			storeTypes[s.Resource().GroupVersionKind()] = append(storeTypes[s.Resource().GroupVersionKind()], store)
		}
	}

	schemas := union.Build()
	if err := schemas.Validate(); err != nil {
		return nil, err
	}
	result := &store{
		schemas: schemas,
		stores:  storeTypes,
		writer:  writer,
	}

	return result, nil
}

// MakeWriteableCache creates an aggregate config store cache from several config store caches. An additional
// `writer` config store is passed, which may or may not be part of `caches`.
func MakeWriteableCache(caches []model.ConfigStoreController, writer model.ConfigStore) (model.ConfigStoreController, error) {
	stores := make([]model.ConfigStore, 0, len(caches))
	for _, cache := range caches {
		stores = append(stores, cache)
	}
	store, err := makeStore(stores, writer)
	if err != nil {
		return nil, err
	}
	return &storeCache{
		ConfigStore: store,
		caches:      caches,
	}, nil
}

// MakeCache creates an aggregate config store cache from several config store
// caches.
func MakeCache(caches []model.ConfigStoreController) (model.ConfigStoreController, error) {
	return MakeWriteableCache(caches, nil)
}

type store struct {
	// schemas is the unified
	schemas collection.Schemas

	// stores is a mapping from config type to a store
	stores map[config.GroupVersionKind][]model.ConfigStore

	writer model.ConfigStore
}

func (cr *store) Schemas() collection.Schemas {
	return cr.schemas
}

// Get the first config found in the stores.
func (cr *store) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	for _, store := range cr.stores[typ] {
		config := store.Get(typ, name, namespace)
		if config != nil {
			return config
		}
	}
	return nil
}

// List all configs in the stores.
func (cr *store) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	if len(cr.stores[typ]) == 0 {
		return nil, nil
	}
	var errs *multierror.Error
	var configs []config.Config
	// Used to remove duplicated config
	configMap := sets.New()

	for _, store := range cr.stores[typ] {
		storeConfigs, err := store.List(typ, namespace)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
		for _, cfg := range storeConfigs {
			key := cfg.GroupVersionKind.Kind + cfg.Namespace + cfg.Name
			if !configMap.Contains(key) {
				configs = append(configs, cfg)
				configMap.Insert(key)
			}
		}
	}
	return configs, errs.ErrorOrNil()
}

func (cr *store) Delete(typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	if cr.writer == nil {
		return errorUnsupported
	}
	return cr.writer.Delete(typ, name, namespace, resourceVersion)
}

func (cr *store) Create(c config.Config) (string, error) {
	if cr.writer == nil {
		return "", errorUnsupported
	}
	return cr.writer.Create(c)
}

func (cr *store) Update(c config.Config) (string, error) {
	if cr.writer == nil {
		return "", errorUnsupported
	}
	return cr.writer.Update(c)
}

func (cr *store) UpdateStatus(c config.Config) (string, error) {
	if cr.writer == nil {
		return "", errorUnsupported
	}
	return cr.writer.UpdateStatus(c)
}

func (cr *store) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	if cr.writer == nil {
		return "", errorUnsupported
	}
	return cr.writer.Patch(orig, patchFn)
}

type storeCache struct {
	model.ConfigStore
	caches []model.ConfigStoreController
}

func (cr *storeCache) HasSynced() bool {
	for _, cache := range cr.caches {
		if !cache.HasSynced() {
			return false
		}
	}
	return true
}

func (cr *storeCache) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	for _, cache := range cr.caches {
		if _, exists := cache.Schemas().FindByGroupVersionKind(kind); exists {
			cache.RegisterEventHandler(kind, handler)
		}
	}
}

func (cr *storeCache) SetWatchErrorHandler(handler func(r *cache.Reflector, err error)) error {
	var errs error
	for _, cache := range cr.caches {
		if err := cache.SetWatchErrorHandler(handler); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return errs
}

func (cr *storeCache) Run(stop <-chan struct{}) {
	for _, cache := range cr.caches {
		go cache.Run(stop)
	}
	<-stop
}
