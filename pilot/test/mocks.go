// Copyright 2016 Google Inc.
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

package test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"

	"istio.io/manager/model"
)

const (
	MockKind = "MockConfig"
	MockName = "my-qualified-name"
)

var (
	MockKey = model.ConfigKey{
		Kind: MockKind,
		Name: MockName,
	}
	MockConfigObject = MockConfig{
		Pairs: []*ConfigPair{
			&ConfigPair{Key: "key", Value: "value"},
		},
	}
	MockObject = model.Config{
		ConfigKey: MockKey,
		Content:   &MockConfigObject,
	}
	MockMapping = model.KindMap{
		MockKind: model.ProtoValidator{
			MessageName: "test.MockConfig",
			Description: "Sample config kind",
			Validate:    func(proto.Message) error { return nil },
		},
	}
)

type MockRegistry struct {
	store   map[model.ConfigKey]*model.Config
	mapping model.KindMap
}

type MockGenerator struct{}

type MockConfigConsumer struct {
	Generator model.Generator
}

func NewMockRegistry() model.Registry {
	return &MockRegistry{
		store:   make(map[model.ConfigKey]*model.Config),
		mapping: MockMapping,
	}
}

func (r *MockRegistry) Get(key model.ConfigKey) (*model.Config, bool) {
	out, err := r.store[key]
	return out, err
}

func (r *MockRegistry) Put(obj model.Config) error {
	if err := r.mapping.ValidateConfig(obj); err != nil {
		return err
	}
	r.store[obj.ConfigKey] = &obj
	return nil
}

func (r *MockRegistry) Delete(key model.ConfigKey) {
	delete(r.store, key)
}

func (r *MockRegistry) List(kind string) []*model.Config {
	var out = make([]*model.Config, 0)
	for _, v := range r.store {
		if v.Kind == kind {
			out = append(out, v)
		}
	}
	return out
}

func CheckMapInvariant(r model.Registry, t *testing.T) {
	if err := r.Put(MockObject); err != nil {
		t.Error(err)
	}
	if v1, ok := r.Get(MockKey); !ok || !reflect.DeepEqual(*v1, MockObject) {
		t.Errorf("Wanted %v, got %v", MockObject, *v1)
	}
	if _, ok := r.Get(model.ConfigKey{
		Kind:    MockKind,
		Name:    MockName,
		Version: "test",
	}); ok {
		t.Errorf("Wanted missing element")
	}
	l := r.List(MockKind)
	if len(l) != 1 {
		t.Errorf("Wanted 1 element, got %d in %v", len(l), l)
	}
	if !reflect.DeepEqual(*l[0], MockObject) {
		t.Errorf("Wanted %v, got %v", MockObject, l[0])
	}
	r.Delete(MockKey)
	l = r.List(MockKind)
	if len(l) != 0 {
		t.Errorf("Wanted 0 elements, got %d in %v", len(l), l)
	}
}

func (generator *MockGenerator) Render(reg model.Registry) ([]*model.ConfigOutput, error) {
	var buffer bytes.Buffer
	var keys []*model.ConfigKey
	for _, config := range reg.List(MockKind) {
		keys = append(keys, &config.ConfigKey)
		for _, pair := range config.Content.(*MockConfig).Pairs {
			buffer.WriteString(pair.Key)
			buffer.WriteString(": ")
			buffer.WriteString(pair.Value)
			buffer.WriteString("\n")
		}
	}
	return []*model.ConfigOutput{&model.ConfigOutput{
		Sources: keys,
		Content: buffer.Bytes(),
	}}, nil
}

func (consumer *MockConfigConsumer) Name() string {
	return "MockConfigConsumer"
}

func (consumer *MockConfigConsumer) Generators() []model.Generator {
	return []model.Generator{consumer.Generator}
}

func (consumer *MockConfigConsumer) Distribute([]*model.ConfigOutput) error {
	return nil
}
