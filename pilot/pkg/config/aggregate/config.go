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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/kube/krt"
)

var errorUnsupported = errors.New("unsupported operation: the config aggregator is read-only")

// makeStore creates an aggregate config store from several config stores and
// unifies their descriptors
func makeStore(stores []model.ConfigStoreController, writer model.ConfigStoreController) (*store, error) {
	stop := make(chan struct{})
	union := collection.NewSchemasBuilder()
	storeTypes := make(map[config.GroupVersionKind][]model.ConfigStoreController)
	for _, store := range stores {
		for _, s := range store.Schemas().All() {
			if len(storeTypes[s.GroupVersionKind()]) == 0 {
				if err := union.Add(s); err != nil {
					return nil, err
				}
			}
			storeTypes[s.GroupVersionKind()] = append(storeTypes[s.GroupVersionKind()], store)
		}
	}

	schemas := union.Build()
	if err := schemas.Validate(); err != nil {
		return nil, err
	}

	kopts := krt.NewOptionsBuilder(stop, "aggregate", krt.GlobalDebugHandler)
	kindStores := make(map[config.GroupVersionKind]kindStore)
	for _, schema := range schemas.All() {
		gvk := schema.GroupVersionKind()
		schemaCollections := make([]krt.Collection[config.Config], 0, len(storeTypes[gvk]))
		for _, store := range storeTypes[gvk] {
			collection := store.KrtCollection(gvk)
			if collection != nil {
				schemaCollections = append(schemaCollections, collection)
			}
		}
		if len(schemaCollections) != 0 {
			collection := krt.JoinCollection(schemaCollections, kopts.WithName(gvk.Kind)...)
			kindStores[gvk] = kindStore{
				collection: collection,
				index:      krt.NewNamespaceIndex(collection),
			}
		}
	}

	result := &store{
		schemas:    schemas,
		stores:     storeTypes,
		kindStores: kindStores,
		writer:     writer,
		stop:       stop,
	}

	return result, nil
}

// MakeWriteableCache creates an aggregate config store cache from several config store caches. An additional
// `writer` config store is passed, which may or may not be part of `caches`.
func MakeWriteableCache(caches []model.ConfigStoreController, writer model.ConfigStoreController) (model.ConfigStoreController, error) {
	store, err := makeStore(caches, writer)
	if err != nil {
		return nil, err
	}
	return &storeCache{
		store:  store,
		caches: caches,
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
	stores map[config.GroupVersionKind][]model.ConfigStoreController

	kindStores map[config.GroupVersionKind]kindStore

	writer model.ConfigStoreController
	stop   chan struct{}
}

type kindStore struct {
	collection krt.Collection[config.Config]
	index      krt.Index[string, config.Config]
}

func (cr *store) Schemas() collection.Schemas {
	return cr.schemas
}

// Get the first config found in the stores.
func (cr *store) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	if kindStore, ok := cr.kindStores[typ]; ok {
		key := name
		if len(namespace) > 0 {
			key = namespace + "/" + name
		}

		return kindStore.collection.GetKey(key)
	}

	return nil
}

// List all configs in the stores.
func (cr *store) List(typ config.GroupVersionKind, namespace string) []config.Config {
	if kindStore, ok := cr.kindStores[typ]; ok {
		if namespace == model.NamespaceAll {
			return kindStore.collection.List()
		}
		return kindStore.index.Lookup(namespace)
	}

	return nil
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

type storeCache struct {
	*store
	caches []model.ConfigStoreController
}

func (cr *storeCache) HasSynced() bool {
	for _, cache := range cr.caches {
		if !cache.HasSynced() {
			return false
		}
	}

	for _, kindStore := range cr.kindStores {
		if !kindStore.collection.HasSynced() {
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

func (cr *storeCache) Run(stop <-chan struct{}) {
	for _, cache := range cr.caches {
		go cache.Run(stop)
	}
	<-stop
	close(cr.stop)
}

func (cr *storeCache) KrtCollection(gvk config.GroupVersionKind) krt.Collection[config.Config] {
	if kindStore, ok := cr.store.kindStores[gvk]; ok {
		return kindStore.collection
	}

	return nil
}
