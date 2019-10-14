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

// Package memory provides an in-memory volatile config store implementation
package memory

import (
	"errors"
	"sync"
	"time"

	"istio.io/pkg/log"

	"istio.io/pkg/ledger"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema"
)

var (
	errNotFound      = errors.New("item not found")
	errAlreadyExists = errors.New("item already exists")
)

const ledgerLogf = "error tracking pilot config memory versions for distribution: %v"

// Make creates an in-memory config store from a config descriptor
func Make(descriptor schema.Set) model.ConfigStore {
	return MakeWithLedger(descriptor, ledger.Make(time.Minute))
}

func MakeWithLedger(descriptor schema.Set, configLedger ledger.Ledger) model.ConfigStore {
	out := store{
		descriptor: descriptor,
		data:       make(map[string]map[string]*sync.Map),
		ledger:     configLedger,
	}
	for _, typ := range descriptor.Types() {
		out.data[typ] = make(map[string]*sync.Map)
	}
	return &out
}

type store struct {
	descriptor schema.Set
	data       map[string]map[string]*sync.Map
	ledger     ledger.Ledger
}

func (cr *store) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return cr.ledger.GetPreviousValue(version, key)
}

func (cr *store) ConfigDescriptor() schema.Set {
	return cr.descriptor
}

func (cr *store) Version() string {
	return cr.ledger.RootHash()
}

func (cr *store) Get(typ, name, namespace string) *model.Config {
	_, ok := cr.data[typ]
	if !ok {
		return nil
	}

	ns, exists := cr.data[typ][namespace]
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

func (cr *store) List(typ, namespace string) ([]model.Config, error) {
	data, exists := cr.data[typ]
	if !exists {
		return nil, nil
	}
	out := make([]model.Config, 0, len(cr.data[typ]))
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

func (cr *store) Delete(typ, name, namespace string) error {
	data, ok := cr.data[typ]
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

	err := cr.ledger.Delete(model.Key(typ, name, namespace))
	if err != nil {
		log.Warnf(ledgerLogf, err)
	}
	ns.Delete(name)
	return nil
}

func (cr *store) Create(config model.Config) (string, error) {
	typ := config.Type
	s, ok := cr.descriptor.GetByType(typ)
	if !ok {
		return "", errors.New("unknown type")
	}
	if err := s.Validate(config.Name, config.Namespace, config.Spec); err != nil {
		return "", err
	}
	ns, exists := cr.data[typ][config.Namespace]
	if !exists {
		ns = new(sync.Map)
		cr.data[typ][config.Namespace] = ns
	}

	_, exists = ns.Load(config.Name)

	if !exists {
		tnow := time.Now()
		config.ResourceVersion = tnow.String()

		// Set the creation timestamp, if not provided.
		if config.CreationTimestamp.IsZero() {
			config.CreationTimestamp = tnow
		}

		_, err := cr.ledger.Put(model.Key(typ, config.Namespace, config.Name), config.Version)
		if err != nil {
			log.Warnf(ledgerLogf, err)
		}
		ns.Store(config.Name, config)
		return config.ResourceVersion, nil
	}
	return "", errAlreadyExists
}

func (cr *store) Update(config model.Config) (string, error) {
	typ := config.Type
	s, ok := cr.descriptor.GetByType(typ)
	if !ok {
		return "", errors.New("unknown type")
	}
	if err := s.Validate(config.Name, config.Namespace, config.Spec); err != nil {
		return "", err
	}

	ns, exists := cr.data[typ][config.Namespace]
	if !exists {
		return "", errNotFound
	}

	oldConfig, exists := ns.Load(config.Name)
	if !exists {
		return "", errNotFound
	}

	if config.ResourceVersion != oldConfig.(model.Config).ResourceVersion {
		return "", errors.New("old revision")
	}

	rev := time.Now().String()
	config.ResourceVersion = rev
	_, err := cr.ledger.Put(model.Key(typ, config.Namespace, config.Name), config.Version)
	if err != nil {
		log.Warnf(ledgerLogf, err)
	}
	ns.Store(config.Name, config)
	return rev, nil
}
