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

	"github.com/golang/protobuf/proto"

	"istio.io/pilot/model"
)

// Make creates an in-memory config store from a config descriptor
func Make(descriptor model.ConfigDescriptor) model.ConfigStore {
	out := store{
		descriptor: descriptor,
		data:       make(map[string]map[string]proto.Message),
		revs:       make(map[string]map[string]string),
	}
	for _, typ := range descriptor.Types() {
		out.data[typ] = make(map[string]proto.Message)
		out.revs[typ] = make(map[string]string)
	}
	return &out
}

type store struct {
	descriptor model.ConfigDescriptor
	data       map[string]map[string]proto.Message
	revs       map[string]map[string]string
}

func (cr *store) ConfigDescriptor() model.ConfigDescriptor {
	return cr.descriptor
}

func (cr *store) Get(typ, key string) (proto.Message, bool, string) {
	_, ok := cr.data[typ]
	if !ok {
		return nil, false, ""
	}
	val, exists := cr.data[typ][key]
	return val, exists, cr.revs[typ][key]
}

func (cr *store) List(typ string) ([]model.Config, error) {
	_, ok := cr.data[typ]
	if !ok {
		return nil, nil
	}
	out := make([]model.Config, 0, len(cr.data[typ]))
	for key, elt := range cr.data[typ] {
		out = append(out, model.Config{
			Type:     typ,
			Key:      key,
			Revision: cr.revs[typ][key],
			Content:  elt,
		})
	}
	return out, nil
}

func (cr *store) Delete(typ, key string) error {
	_, ok := cr.data[typ]
	if !ok {
		return errors.New("unknown type")
	}
	if _, exists := cr.data[typ][key]; exists {
		delete(cr.data[typ], key)
		delete(cr.revs[typ], key)
		return nil
	}
	return &model.ItemNotFoundError{Key: key}
}

func (cr *store) Post(config proto.Message) (string, error) {
	schema, ok := cr.descriptor.GetByMessageName(proto.MessageName(config))
	if !ok {
		return "", errors.New("unknown type")
	}
	if err := schema.Validate(config); err != nil {
		return "", err
	}
	typ := schema.Type
	key := schema.Key(config)
	_, exists := cr.data[typ][key]
	if !exists {
		rev := time.Now().String()
		cr.revs[typ][key] = rev
		cr.data[typ][key] = config
		return rev, nil
	}
	return "", &model.ItemAlreadyExistsError{Key: key}
}

func (cr *store) Put(config proto.Message, oldRevision string) (string, error) {
	schema, ok := cr.descriptor.GetByMessageName(proto.MessageName(config))
	if !ok {
		return "", errors.New("unknown type")
	}
	if err := schema.Validate(config); err != nil {
		return "", err
	}
	typ := schema.Type
	key := schema.Key(config)
	_, exists := cr.data[typ][key]
	if !exists {
		return "", &model.ItemNotFoundError{Key: key}
	}
	if oldRevision != cr.revs[typ][key] {
		return "", errors.New("old revision")
	}

	rev := time.Now().String()
	cr.revs[typ][key] = rev
	cr.data[typ][key] = config
	return rev, nil
}
