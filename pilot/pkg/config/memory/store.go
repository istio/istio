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

// Package memory provides an in-memory volatile config store implementation
package memory

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/kube/krt"
)

var (
	errNotFound      = errors.New("item not found")
	errAlreadyExists = errors.New("item already exists")
	// TODO: can we make this compatible with kerror.IsConflict without imports the library?
	errConflict = errors.New("conflicting resource version, try again")
)

const ResourceVersion string = "ResourceVersion"

// Make creates an in-memory config store from a config schemas
// It is with validation
func Make(schemas collection.Schemas) *Store {
	return newStore(schemas, false)
}

// MakeSkipValidation creates an in-memory config store from a config schemas
// It is without validation
func MakeSkipValidation(schemas collection.Schemas) *Store {
	return newStore(schemas, true)
}

type syncer struct {
	synced chan struct{}
}

func (c *syncer) WaitUntilSynced(stop <-chan struct{}) bool {
	select {
	case <-c.synced:
		return true
	case <-stop:
		return false
	}
}

func (c *syncer) HasSynced() bool {
	select {
	case <-c.synced:
		return true
	default:
		return false
	}
}

func (c *syncer) MarkSynced() {
	close(c.synced)
}

func newStore(
	schemas collection.Schemas,
	skipValidation bool,
) *Store {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "memory", krt.GlobalDebugHandler)
	out := Store{
		schemas:        schemas,
		data:           make(map[config.GroupVersionKind]kindStore),
		skipValidation: skipValidation,
		syncer:         &syncer{make(chan struct{})},
		stop:           stop,
	}
	for _, s := range schemas.All() {
		collection := krt.NewStaticCollection[config.Config](out.syncer, nil, opts.WithName(s.Kind())...)
		index := krt.NewNamespaceIndex(collection)
		out.data[s.GroupVersionKind()] = kindStore{
			collection: collection,
			index:      index,
		}
	}
	return &out
}

type kindStore struct {
	collection krt.StaticCollection[config.Config]
	index      krt.Index[string, config.Config]
}

type Store struct {
	schemas        collection.Schemas
	data           map[config.GroupVersionKind]kindStore
	skipValidation bool
	mutex          sync.RWMutex
	syncer         *syncer
	stop           chan struct{}
}

func (cr *Store) hasSynced() bool {
	for _, data := range cr.data {
		if !data.collection.HasSynced() {
			return false
		}
	}

	return true
}

func (cr *Store) Schemas() collection.Schemas {
	return cr.schemas
}

func (cr *Store) Get(kind config.GroupVersionKind, name, namespace string) *config.Config {
	cr.mutex.RLock()
	defer cr.mutex.RUnlock()
	data, ok := cr.data[kind]
	if !ok {
		return nil
	}

	key := name
	if namespace != "" {
		key = namespace + "/" + name
	}

	return data.collection.GetKey(key)
}

func (cr *Store) List(kind config.GroupVersionKind, namespace string) []config.Config {
	cr.mutex.RLock()
	defer cr.mutex.RUnlock()
	data, exists := cr.data[kind]
	if !exists {
		return nil
	}

	if namespace == "" {
		return data.collection.List()
	}

	return data.index.Lookup(namespace)
}

func (cr *Store) Delete(kind config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	data, ok := cr.data[kind]
	if !ok {
		return fmt.Errorf("unknown type %v", kind)
	}

	key := name
	if namespace != "" {
		key = namespace + "/" + name
	}

	cfg := data.collection.GetKey(key)
	if cfg == nil {
		return errNotFound
	}

	data.collection.DeleteObject(key)

	return nil
}

func (cr *Store) Create(cfg config.Config) (string, error) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	kind := cfg.GroupVersionKind
	s, ok := cr.schemas.FindByGroupVersionKind(kind)
	if !ok {
		return "", fmt.Errorf("unknown type %v", kind)
	}
	if !cr.skipValidation {
		if _, err := s.ValidateConfig(cfg); err != nil {
			return "", err
		}
	}

	data := cr.data[kind]
	obj := data.collection.GetKey(krt.GetKey(cfg))

	if obj == nil {
		tnow := time.Now()
		if cfg.ResourceVersion == "" {
			cfg.ResourceVersion = tnow.String()
		}
		// Set the creation timestamp, if not provided.
		if cfg.CreationTimestamp.IsZero() {
			cfg.CreationTimestamp = tnow
		}

		data.collection.UpdateObject(cfg)

		return cfg.ResourceVersion, nil
	}
	return "", errAlreadyExists
}

func (cr *Store) Update(cfg config.Config) (string, error) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	kind := cfg.GroupVersionKind
	s, ok := cr.schemas.FindByGroupVersionKind(kind)
	if !ok {
		return "", fmt.Errorf("unknown type %v", kind)
	}
	if !cr.skipValidation {
		if _, err := s.ValidateConfig(cfg); err != nil {
			return "", err
		}
	}

	data := cr.data[kind]
	existing := data.collection.GetKey(krt.GetKey(cfg))
	if existing == nil {
		return "", errNotFound
	}
	if hasConflict(*existing, cfg) {
		return "", errConflict
	}
	if cfg.Annotations != nil && cfg.Annotations[ResourceVersion] != "" {
		cfg.ResourceVersion = cfg.Annotations[ResourceVersion]
		delete(cfg.Annotations, ResourceVersion)
	} else {
		cfg.ResourceVersion = time.Now().String()
	}

	data.collection.UpdateObject(cfg)
	return cfg.ResourceVersion, nil
}

func (cr *Store) UpdateStatus(cfg config.Config) (string, error) {
	return cr.Update(cfg)
}

func (cr *Store) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	gvk := orig.GroupVersionKind
	s, ok := cr.schemas.FindByGroupVersionKind(gvk)
	if !ok {
		return "", fmt.Errorf("unknown type %v", gvk)
	}

	cfg, _ := patchFn(orig)
	if !cr.skipValidation {
		if _, err := s.ValidateConfig(cfg); err != nil {
			return "", err
		}
	}

	data, ok := cr.data[gvk]
	if !ok {
		return "", errNotFound
	}

	existing := data.collection.GetKey(krt.GetKey(cfg))
	if existing == nil {
		return "", errNotFound
	}

	rev := time.Now().String()
	cfg.ResourceVersion = rev
	data.collection.UpdateObject(cfg)

	return rev, nil
}

// hasConflict checks if the two resources have a conflict, which will block Update calls
func hasConflict(existing, replacement config.Config) bool {
	if replacement.ResourceVersion == "" {
		// We don't care about resource version, so just always overwrite
		return false
	}
	// We set a resource version but its not matched, it is a conflict
	if replacement.ResourceVersion != existing.ResourceVersion {
		return true
	}
	return false
}
