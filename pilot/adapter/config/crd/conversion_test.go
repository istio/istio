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

package crd

import (
	"reflect"
	"testing"

	"istio.io/pilot/model"
	"istio.io/pilot/test/mock"
)

var (
	camelKabobs = []struct{ in, out string }{
		{"ExampleNameX", "example-name-x"},
		{"Example1", "example1"},
		{"ExampleXY", "example-x-y"},
	}
)

func TestCamelKabob(t *testing.T) {
	for _, tt := range camelKabobs {
		s := CamelCaseToKabobCase(tt.in)
		if s != tt.out {
			t.Errorf("CamelCaseToKabobCase(%q) => %q, want %q", tt.in, s, tt.out)
		}
		u := KabobCaseToCamelCase(tt.out)
		if u != tt.in {
			t.Errorf("kabobToCamel(%q) => %q, want %q", tt.out, u, tt.in)
		}
	}
}

func TestConvert(t *testing.T) {
	if _, err := ConvertConfig(model.RouteRule, model.Config{}); err == nil {
		t.Errorf("expected error for converting empty config")
	}
	if _, err := ConvertObject(model.RouteRule, &IstioKind{Spec: map[string]interface{}{"x": 1}}, "local"); err == nil {
		t.Errorf("expected error for converting empty object")
	}
	config := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            model.RouteRule.Type,
			Name:            "test",
			Namespace:       "default",
			Domain:          "cluster",
			ResourceVersion: "1234",
			Labels:          map[string]string{"label": "value"},
			Annotations:     map[string]string{"annotation": "value"},
		},
		Spec: mock.ExampleRouteRule,
	}

	obj, err := ConvertConfig(model.RouteRule, config)
	if err != nil {
		t.Errorf("ConvertConfig() => unexpected error %v", err)
	}
	got, err := ConvertObject(model.RouteRule, obj, "cluster")
	if err != nil {
		t.Errorf("ConvertObject() => unexpected error %v", err)
	}
	if !reflect.DeepEqual(&config, got) {
		t.Errorf("ConvertObject(ConvertConfig(%#v)) => got %#v", config, got)
	}
}

func TestParseInputs(t *testing.T) {
	if varr, _, err := ParseInputs(""); len(varr) > 0 || err != nil {
		t.Errorf(`ParseInput("") => got %v, %v, want nil, nil`, varr, err)
	}
	if _, _, err := ParseInputs("a"); err == nil {
		t.Error(`ParseInput("a") => got no error`)
	}
	if _, others, err := ParseInputs("kind: Pod"); err != nil || len(others) != 1 {
		t.Errorf(`ParseInput("kind: Pod") => got %v, %v`, others, err)
	}
	if varr, others, err := ParseInputs("---\n"); err != nil || len(varr) != 0 || len(others) != 0 {
		t.Errorf(`ParseInput("---") => got %v, %v, %v`, varr, others, err)
	}
	if _, _, err := ParseInputs("kind: RouteRule\nspec:\n  destination: x"); err == nil {
		t.Error("ParseInput(bad spec) => got no error")
	}
	if _, _, err := ParseInputs("kind: RouteRule\nspec:\n  destination:\n    service:"); err == nil {
		t.Error("ParseInput(invalid spec) => got no error")
	}

	varr, _, err := ParseInputs("kind: RouteRule\nspec:\n  destination:\n    service: x")
	if err != nil || len(varr) == 0 {
		t.Errorf("ParseInputs(correct input) => got %v, %v", varr, err)
	}
}
