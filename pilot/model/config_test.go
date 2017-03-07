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

package model

import "testing"

var (
	validKeys = []Key{
		{Kind: "my-config", Name: "example-config-name", Namespace: "default"},
		{Kind: "my-config", Name: "x", Namespace: "default"},
		{Kind: "some-kind", Name: "x", Namespace: "default"},
	}
	invalidKeys = []Key{
		{Kind: "my-config", Name: "exampleConfigName", Namespace: "default"},
		{Name: "x"},
		{Kind: "my-config", Name: "x"},
		{Kind: "ExampleKind", Name: "x", Namespace: "default"},
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

func TestConfigKinds(t *testing.T) {
	err := IstioConfig.Validate()
	if err != nil {
		t.Errorf(err.Error())
	}
}
