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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
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
func Make(schemas collection.Schemas) model.ConfigStore {
	return newStore(schemas, false)
}

// MakeSkipValidation creates an in-memory config store from a config schemas
// It is without validation
func MakeSkipValidation(schemas collection.Schemas) model.ConfigStore {
	return newStore(schemas, true)
}

func newStore(schemas collection.Schemas, skipValidation bool) model.ConfigStore {
	out := store{
		schemas:        schemas,
		data:           make(map[config.GroupVersionKind]map[string]map[string]any),
		skipValidation: skipValidation,
	}
	for _, s := range schemas.All() {
		out.data[s.GroupVersionKind()] = make(map[string]map[string]any)
	}
	return &out
}

type store struct {
	schemas        collection.Schemas
	data           map[config.GroupVersionKind]map[string]map[string]any
	skipValidation bool
	mutex          sync.RWMutex
}

func (cr *store) Schemas() collection.Schemas {
	return cr.schemas
}

func (cr *store) Get(kind config.GroupVersionKind, name, namespace string) *config.Config {
	cr.mutex.RLock()
	defer cr.mutex.RUnlock()
	_, ok := cr.data[kind]
	if !ok {
		return nil
	}

	ns, exists := cr.data[kind][namespace]
	if !exists {
		return nil
	}

	out, exists := ns[name]
	if !exists {
		return nil
	}
	config := out.(config.Config)

	return &config
}

func (cr *store) List(kind config.GroupVersionKind, namespace string) []config.Config {
	cr.mutex.RLock()
	defer cr.mutex.RUnlock()
	data, exists := cr.data[kind]
	if !exists {
		return nil
	}

	var size int
	if namespace == "" {
		for _, ns := range data {
			size += len(ns)
		}
	} else {
		size = len(data[namespace])
	}

	out := make([]config.Config, 0, size)
	if namespace == "" {
		for _, ns := range data {
			for _, value := range ns {
				out = append(out, value.(config.Config))
			}
		}
	} else {
		ns, exists := data[namespace]
		if !exists {
			return nil
		}
		for _, value := range ns {
			out = append(out, value.(config.Config))
		}
	}
	return out
}

func (cr *store) Delete(kind config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	data, ok := cr.data[kind]
	if !ok {
		return fmt.Errorf("unknown type %v", kind)
	}
	ns, exists := data[namespace]
	if !exists {
		return errNotFound
	}

	_, exists = ns[name]
	if !exists {
		return errNotFound
	}

	delete(ns, name)
	return nil
}

func (cr *store) Create(cfg config.Config) (string, error) {
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
	ns, exists := cr.data[kind][cfg.Namespace]
	if !exists {
		ns = map[string]any{}
		cr.data[kind][cfg.Namespace] = ns
	}

	_, exists = ns[cfg.Name]

	if !exists {
		tnow := time.Now()
		if cfg.ResourceVersion == "" {
			cfg.ResourceVersion = tnow.String()
		}
		// Set the creation timestamp, if not provided.
		if cfg.CreationTimestamp.IsZero() {
			cfg.CreationTimestamp = tnow
		}

		ns[cfg.Name] = cfg
		return cfg.ResourceVersion, nil
	}
	return "", errAlreadyExists
}

func (cr *store) Update(cfg config.Config) (string, error) {
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

	ns, exists := cr.data[kind][cfg.Namespace]
	if !exists {
		return "", errNotFound
	}

	existing, exists := ns[cfg.Name]
	if !exists {
		return "", errNotFound
	}
	if hasConflict(existing.(config.Config), cfg) {
		return "", errConflict
	}
	if cfg.Annotations != nil && cfg.Annotations[ResourceVersion] != "" {
		cfg.ResourceVersion = cfg.Annotations[ResourceVersion]
		delete(cfg.Annotations, ResourceVersion)
	} else {
		cfg.ResourceVersion = time.Now().String()
	}

	ns[cfg.Name] = cfg
	return cfg.ResourceVersion, nil
}

func (cr *store) UpdateStatus(cfg config.Config) (string, error) {
	return cr.Update(cfg)
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
