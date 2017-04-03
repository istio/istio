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

package mock

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/golang/protobuf/proto"

	"istio.io/manager/model"
)

// Mock values
const (
	Kind      = "mock-config"
	Name      = "my-qualified-name"
	Namespace = "test"
)

// Mock values
var (
	Key = model.Key{
		Kind:      Kind,
		Name:      Name,
		Namespace: Namespace,
	}
	ConfigObject = &MockConfig{
		Pairs: []*ConfigPair{
			{Key: "key", Value: "value"},
		},
	}
	Mapping = model.KindMap{
		Kind: model.ProtoSchema{
			MessageName: "mock.MockConfig",
			Validate:    func(proto.Message) error { return nil },
		},
	}
)

// MakeRegistry creates a mock config registry
func MakeRegistry() *model.IstioRegistry {
	return &model.IstioRegistry{
		ConfigRegistry: &ConfigRegistry{
			data: make(map[model.Key]proto.Message),
		}}
}

// ConfigRegistry is a mock config registry
type ConfigRegistry struct {
	data map[model.Key]proto.Message
}

// Get implements config registry method
func (cr *ConfigRegistry) Get(key model.Key) (proto.Message, bool) {
	val, ok := cr.data[key]
	return val, ok
}

// Delete implements config registry method
func (cr *ConfigRegistry) Delete(key model.Key) error {
	if _, ok := cr.data[key]; ok {
		delete(cr.data, key)
		return nil
	}
	return errors.New("item is missing")
}

// Post implements config registry method
func (cr *ConfigRegistry) Post(key model.Key, v proto.Message) error {
	_, ok := cr.data[key]
	if !ok {
		cr.data[key] = v
		return nil
	}
	return errors.New("item already exists")
}

// Put implements config registry method
func (cr *ConfigRegistry) Put(key model.Key, v proto.Message) error {
	_, ok := cr.data[key]
	if !ok {
		return errors.New("item is missing")
	}
	cr.data[key] = v
	return nil
}

// List implements config registry method
func (cr *ConfigRegistry) List(kind string, namespace string) (map[model.Key]proto.Message, error) {
	out := make(map[model.Key]proto.Message)
	for k, v := range cr.data {
		if k.Kind == kind && (namespace == "" || k.Namespace == namespace) {
			out[k] = v
		}
	}
	return out, nil
}

// Make creates a fake config
func Make(i int) *MockConfig {
	return &MockConfig{
		Pairs: []*ConfigPair{
			{Key: "key", Value: strconv.Itoa(i)},
		},
	}
}

// CheckMapInvariant validates operational invariants of a config registry
func CheckMapInvariant(r model.ConfigRegistry, t *testing.T, namespace string, n int) {
	// create configuration objects
	keys := make(map[int]model.Key)
	elts := make(map[int]*MockConfig)
	for i := 0; i < n; i++ {
		keys[i] = model.Key{
			Kind:      Kind,
			Name:      fmt.Sprintf("%s%d", Name, i),
			Namespace: namespace,
		}
		elts[i] = Make(i)
	}

	// post all elements
	for i, elt := range elts {
		if err := r.Post(keys[i], elt); err != nil {
			t.Error(err)
		}
	}

	// check that elements are stored
	for i, elt := range elts {
		if v1, ok := r.Get(keys[i]); !ok || !reflect.DeepEqual(v1, elt) {
			t.Errorf("Wanted %v, got %v", elt, v1)
		}
	}

	// check for missing element
	if _, ok := r.Get(model.Key{
		Kind:      Kind,
		Name:      Name,
		Namespace: namespace,
	}); ok {
		t.Error("Unexpected configuration object found")
	}

	// list elements
	l, err := r.List(Kind, namespace)
	if err != nil {
		t.Errorf("List error %#v, %v", l, err)
	}
	if len(l) != n {
		t.Errorf("Wanted %d element(s), got %d in %v", n, len(l), l)
	}

	// update all elements
	for i := 0; i < n; i++ {
		elts[i].Pairs[0].Value += "(updated)"
		if err = r.Put(keys[i], elts[i]); err != nil {
			t.Error(err)
		}
	}

	// check that elements are stored
	for i, elt := range elts {
		if v1, ok := r.Get(keys[i]); !ok || !reflect.DeepEqual(v1, elt) {
			t.Errorf("Wanted %v, got %v", elt, v1)
		}
	}

	// delete all elements
	for i := range elts {
		if err = r.Delete(keys[i]); err != nil {
			t.Error(err)
		}
	}

	l, err = r.List(Kind, namespace)
	if err != nil {
		t.Error(err)
	}
	if len(l) != 0 {
		t.Errorf("Wanted 0 element(s), got %d in %v", len(l), l)
	}
}
