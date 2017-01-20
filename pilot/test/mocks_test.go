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

package test

import (
	"testing"

	"istio.io/manager/model"
)

var invalidConfigs = []model.Config{
	{ConfigKey: MockKey},
	{ConfigKey: MockKey, Spec: "x"},
	{ConfigKey: model.ConfigKey{Name: "BLAH"}},
	{
		ConfigKey: model.ConfigKey{Name: MockName},
		Spec:      &MockConfigObject,
	},
	{
		ConfigKey: model.ConfigKey{Name: MockName, Kind: "Mock"},
		Spec:      &MockConfigObject,
	},
}

func TestMockRegistry(t *testing.T) {
	r := NewMockRegistry()
	if err := MockMapping.Validate(); err != nil {
		t.Error(err)
	}
	CheckMapInvariant(r, t, MockNamespace, 10)
	for _, config := range invalidConfigs {
		if err := r.Put(&config); err == nil {
			t.Errorf("t.Put(%#v) succeeded for invalid config", config)
		}
	}
}

func TestKindMap(t *testing.T) {
	if MockMapping.Validate() != nil {
		t.Fail()
	}
}
