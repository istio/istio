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
	"time"

	"istio.io/istio/pilot/model"
)

var (
	errNotFound      = errors.New("item not found")
	errAlreadyExists = errors.New("item already exists")
)

// Make creates an in-memory config store from a config descriptor
func Make(descriptor model.ConfigDescriptor) model.ConfigStore {
	out := store{
		descriptor: descriptor,
		data:       make(map[string]map[string]map[string]model.Config),
	}
	for _, typ := range descriptor.Types() {
		out.data[typ] = make(map[string]map[string]model.Config)
	}
	return &out
}

type store struct {
	descriptor model.ConfigDescriptor
	data       map[string]map[string]map[string]model.Config
}

func (cr *store) ConfigDescriptor() model.ConfigDescriptor {
	return cr.descriptor
}

func (cr *store) Get(typ, name, namespace string) (*model.Config, bool) {
	_, ok := cr.data[typ]
	if !ok {
		return nil, false
	}

	ns, exists := cr.data[typ][namespace]
	if !exists {
		return nil, false
	}

	out, exists := ns[name]
	if !exists {
		return nil, false
	}

	return &out, true
}

func (cr *store) List(typ, namespace string) ([]model.Config, error) {
	data, exists := cr.data[typ]
	if !exists {
		return nil, nil
	}
	out := make([]model.Config, 0, len(cr.data[typ]))
	if namespace == "" {
		for _, ns := range data {
			for _, elt := range ns {
				out = append(out, elt)
			}
		}
	} else {
		for _, elt := range data[namespace] {
			out = append(out, elt)
		}
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

	_, exists = ns[name]
	if !exists {
		return errNotFound
	}

	delete(ns, name)
	return nil
}

func (cr *store) Create(config model.Config) (string, error) {
	typ := config.Type
	schema, ok := cr.descriptor.GetByType(typ)
	if !ok {
		return "", errors.New("unknown type")
	}
	if err := schema.Validate(config.Spec); err != nil {
		return "", err
	}
	ns, exists := cr.data[typ][config.Namespace]
	if !exists {
		ns = make(map[string]model.Config)
		cr.data[typ][config.Namespace] = ns
	}

	_, exists = ns[config.Name]

	if !exists {
		config.ResourceVersion = time.Now().String()
		ns[config.Name] = config
		return config.ResourceVersion, nil
	}
	return "", errAlreadyExists
}

func (cr *store) Update(config model.Config) (string, error) {
	typ := config.Type
	schema, ok := cr.descriptor.GetByType(typ)
	if !ok {
		return "", errors.New("unknown type")
	}
	if err := schema.Validate(config.Spec); err != nil {
		return "", err
	}

	ns, exists := cr.data[typ][config.Namespace]
	if !exists {
		return "", errNotFound
	}

	oldConfig, exists := ns[config.Name]
	if !exists {
		return "", errNotFound
	}

	if config.ResourceVersion != oldConfig.ResourceVersion {
		return "", errors.New("old revision")
	}

	rev := time.Now().String()
	config.ResourceVersion = rev
	ns[config.Name] = config
	return rev, nil
}
