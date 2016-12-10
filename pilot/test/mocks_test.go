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
	"testing"

	"istio.io/manager/model"
)

var configTests = []model.Config{
	model.Config{ConfigKey: MockKey},
	model.Config{ConfigKey: MockKey, Content: "x"},
	model.Config{ConfigKey: model.ConfigKey{Name: "BLAH"}},
	model.Config{
		ConfigKey: model.ConfigKey{Name: MockName},
		Content:   &MockConfigObject,
	},
	model.Config{
		ConfigKey: model.ConfigKey{Name: MockName, Kind: "Mock"},
		Content:   &MockConfigObject,
	},
}

func TestMockRegistry(t *testing.T) {
	r := NewMockRegistry()
	if err := MockMapping.Validate(); err != nil {
		t.Error(err)
	}
	CheckMapInvariant(r, t)
	for _, config := range configTests {
		if err := r.Put(config); err == nil {
			t.Errorf("t.Put(%#v) succeeded for invalid config", config)
		}
	}
}

func TestKindMap(t *testing.T) {
	if MockMapping.Validate() != nil {
		t.Fail()
	}
}

func TestGenerator(t *testing.T) {
	r := NewMockRegistry()
	r.Put(MockObject)
	var g model.Generator = &MockGenerator{}
	out, err := g.Render(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatal(out)
	}
	data := string(out[0].Content)
	const expected = "key: value\n"
	if data != expected {
		t.Errorf("Wanted %s, got %s", expected, data)
	}
	if len(out[0].Sources) != 1 {
		t.Fatal(out[0].Sources)
	}
	if *out[0].Sources[0] != MockKey {
		t.Errorf("Wanted %v, got %v", MockKey, out[0].Sources)
	}
}
