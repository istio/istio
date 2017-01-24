// Copyright 2017 Google Inc.
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
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/golang/protobuf/proto"

	"istio.io/manager/model"
)

// Mock values
const (
	Kind      = "MockConfig"
	Name      = "my-qualified-name"
	Namespace = "test"
)

// Mock values
var (
	Key = model.ConfigKey{
		Kind:      Kind,
		Name:      Name,
		Namespace: Namespace,
	}
	ConfigObject = MockConfig{
		Pairs: []*ConfigPair{
			{Key: "key", Value: "value"},
		},
	}
	Object = model.Config{
		ConfigKey: Key,
		Spec:      &ConfigObject,
		Status:    &MockConfigStatus{},
	}
	Mapping = model.KindMap{
		Kind: model.ProtoSchema{
			MessageName:       "mock.MockConfig",
			StatusMessageName: "mock.MockConfigStatus",
			Description:       "Sample config kind",
			Validate:          func(proto.Message) error { return nil },
		},
	}
)

// Registry is a fake registry
type Registry struct {
	store   map[model.ConfigKey]*model.Config
	mapping model.KindMap
}

// NewRegistry makes a fake registry
func NewRegistry() model.Registry {
	return &Registry{
		store:   make(map[model.ConfigKey]*model.Config),
		mapping: Mapping,
	}
}

// Get implementation
func (r *Registry) Get(key model.ConfigKey) (*model.Config, bool) {
	out, err := r.store[key]
	return out, err
}

// Put implementation
func (r *Registry) Put(obj *model.Config) error {
	if err := r.mapping.ValidateConfig(obj); err != nil {
		return err
	}
	r.store[obj.ConfigKey] = obj
	return nil
}

// Delete implementation
func (r *Registry) Delete(key model.ConfigKey) error {
	delete(r.store, key)
	return nil
}

// List implementation
func (r *Registry) List(kind string, ns string) ([]*model.Config, error) {
	var out = make([]*model.Config, 0)
	for _, v := range r.store {
		if v.Kind == kind && (ns == "" || v.Namespace == ns) {
			out = append(out, v)
		}
	}
	return out, nil
}

// Make creates a fake config
func Make(i int, namespace string) *model.Config {
	return &model.Config{
		ConfigKey: model.ConfigKey{
			Kind:      Kind,
			Name:      fmt.Sprintf("%s%d", Name, i),
			Namespace: namespace,
		},
		Spec: &MockConfig{
			Pairs: []*ConfigPair{
				{Key: "key", Value: strconv.Itoa(i)},
			},
		},
	}
}

// CheckMapInvariant validates operational invariants of a registry
func CheckMapInvariant(r model.Registry, t *testing.T, namespace string, n int) {
	// create configuration objects
	elts := make(map[int]*model.Config, 0)
	for i := 0; i < n; i++ {
		elts[i] = Make(i, namespace)
	}

	// put all elements
	for _, elt := range elts {
		if err := r.Put(elt); err != nil {
			t.Error(err)
		}
	}

	// check that elements are stored
	for _, elt := range elts {
		if v1, ok := r.Get(elt.ConfigKey); !ok || !reflect.DeepEqual(*v1, *elt) {
			t.Errorf("Wanted %v, got %v", elt, v1)
		}
	}

	// check for missing element
	if _, ok := r.Get(model.ConfigKey{
		Kind:      Kind,
		Name:      Name,
		Namespace: namespace,
	}); ok {
		t.Error("Unexpected configuration object found")
	}

	// list elements
	l, err := r.List(Kind, namespace)
	if err != nil {
		t.Error(err)
	}
	if len(l) != n {
		t.Errorf("Wanted %d element(s), got %d in %v", n, len(l), l)
	}

	// delete all elements
	for _, elt := range elts {
		if err = r.Delete(elt.ConfigKey); err != nil {
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
