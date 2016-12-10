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

package model

import "testing"

var (
	validKeys = []ConfigKey{
		{Name: "example-config-name"},
		{Name: "x", Version: "id"},
		{Name: "x", Version: "v1"},
		{Name: "x", Kind: "ExampleKind"},
	}
	invalidKeys = []ConfigKey{
		{Name: "exampleConfigName"},
		{Name: "x", Version: "config-id"},
		{Name: "x", Version: "Version"},
		{Name: "x", Kind: "example-kind"},
		{Name: "x", Version: "default"},
	}
)

func TestConfigValidation(t *testing.T) {
	for _, valid := range validKeys {
		if err := valid.Validate(); err != nil {
			t.Errorf("Valid config failed validation: %#v", valid)
		}
	}
	for _, invalid := range invalidKeys {
		if err := invalid.Validate(); err == nil {
			t.Errorf("Inalid config passed validation: %#v", invalid)
		}
	}
}
