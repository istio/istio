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

	"istio.io/pkg/ledger"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

var (
	errNotFound      = errors.New("item not found")
	errAlreadyExists = errors.New("item already exists")
)

const ledgerLogf = "error tracking pilot config memory versions for distribution: %v"

// Make creates an in-memory config store from a config schemas
func Make(schemas collection.Schemas) model.ConfigStore {
	return MakeWithLedger(schemas, ledger.Make(time.Minute), false)
}

// Make creates an in-memory config store from a config schemas, with validation disabled
func MakeWithoutValidation(schemas collection.Schemas) model.ConfigStore {
	return MakeWithLedger(schemas, ledger.Make(time.Minute), true)
}

func MakeWithLedger(schemas collection.Schemas, configLedger ledger.Ledger, skipValidation bool) model.ConfigStore {
	out := store{
		schemas:        schemas,
		data:           make(map[resource.GroupVersionKind]map[string]*sync.Map),
		ledger:         configLedger,
		skipValidation: skipValidation,
	}
	for _, s := range schemas.All() {
		out.data[s.Resource().GroupVersionKind()] = make(map[string]*sync.Map)
	}
	return &out
}

type store struct {
	schemas        collection.Schemas
	data           map[resource.GroupVersionKind]map[string]*sync.Map
	ledger         ledger.Ledger
	skipValidation bool
	mutex          sync.RWMutex
}

func (cr *store) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return cr.ledger.GetPreviousValue(version, key)
}

func (cr *store) GetLedger() ledger.Ledger {
	return cr.ledger
}

func (cr *store) SetLedger(l ledger.Ledger) error {
	cr.ledger = l
	return nil
}

func (cr *store) Schemas() collection.Schemas {
	return cr.schemas
}

func (cr *store) Version() string {
	return cr.ledger.RootHash()
}

func (cr *store) Get(kind resource.GroupVersionKind, name, namespace string) *model.Config {
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

	out, exists := ns.Load(name)
	if !exists {
		return nil
	}
	config := out.(model.Config)

	return &config
}

func (cr *store) List(kind resource.GroupVersionKind, namespace string) ([]model.Config, error) {
	cr.mutex.RLock()
	defer cr.mutex.RUnlock()
	data, exists := cr.data[kind]
	if !exists {
		return nil, nil
	}
	out := make([]model.Config, 0, len(cr.data[kind]))
	if namespace == "" {
		for _, ns := range data {
			ns.Range(func(key, value interface{}) bool {
				out = append(out, value.(model.Config))
				return true
			})
		}
	} else {
		ns, exists := data[namespace]
		if !exists {
			return nil, nil
		}
		ns.Range(func(key, value interface{}) bool {
			out = append(out, value.(model.Config))
			return true
		})
	}
	return out, nil
}

func (cr *store) Delete(kind resource.GroupVersionKind, name, namespace string) error {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	data, ok := cr.data[kind]
	if !ok {
		return errors.New("unknown type")
	}
	ns, exists := data[namespace]
	if !exists {
		return errNotFound
	}

	_, exists = ns.Load(name)
	if !exists {
		return errNotFound
	}

	err := cr.ledger.Delete(model.Key(kind.Kind, name, namespace))
	if err != nil {
		log.Warnf(ledgerLogf, err)
	}
	ns.Delete(name)
	return nil
}

func (cr *store) Create(config model.Config) (string, error) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	kind := config.GroupVersionKind
	s, ok := cr.schemas.FindByGroupVersionKind(kind)
	if !ok {
		return "", fmt.Errorf("unknown type %v", kind)
	}
	if !cr.skipValidation {
		if err := s.Resource().ValidateProto(config.Name, config.Namespace, config.Spec); err != nil {
			return "", err
		}
	}
	ns, exists := cr.data[kind][config.Namespace]
	if !exists {
		ns = new(sync.Map)
		cr.data[kind][config.Namespace] = ns
	}

	_, exists = ns.Load(config.Name)

	if !exists {
		tnow := time.Now()
		config.ResourceVersion = tnow.String()

		// Set the creation timestamp, if not provided.
		if config.CreationTimestamp.IsZero() {
			config.CreationTimestamp = tnow
		}

		_, err := cr.ledger.Put(model.Key(kind.Kind, config.Namespace, config.Name), config.ResourceVersion)
		if err != nil {
			log.Warnf(ledgerLogf, err)
		}
		ns.Store(config.Name, config)
		return config.ResourceVersion, nil
	}
	return "", errAlreadyExists
}

func (cr *store) Update(config model.Config) (string, error) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	kind := config.GroupVersionKind
	s, ok := cr.schemas.FindByGroupVersionKind(kind)
	if !ok {
		return "", errors.New("unknown type")
	}
	if err := s.Resource().ValidateProto(config.Name, config.Namespace, config.Spec); err != nil {
		return "", err
	}

	ns, exists := cr.data[kind][config.Namespace]
	if !exists {
		return "", errNotFound
	}

	_, exists = ns.Load(config.Name)
	if !exists {
		return "", errNotFound
	}

	rev := time.Now().String()
	config.ResourceVersion = rev
	_, err := cr.ledger.Put(model.Key(kind.Kind, config.Namespace, config.Name), config.ResourceVersion)
	if err != nil {
		log.Warnf(ledgerLogf, err)
	}
	ns.Store(config.Name, config)
	return rev, nil
}
