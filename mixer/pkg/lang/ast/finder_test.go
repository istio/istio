// Copyright 2018 Istio Authors
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

package ast

import (
	"fmt"
	"testing"

	configpb "istio.io/api/policy/v1beta1"
	descriptorpb "istio.io/api/policy/v1beta1"
)

func TestFinder(t *testing.T) {

	var finder AttributeDescriptorFinder = finder{
		attributes: map[string]*configpb.AttributeManifest_AttributeInfo{
			"foo": {
				ValueType: descriptorpb.DOUBLE,
			},
			"baz": {
				ValueType: descriptorpb.INT64,
			},
		},
	}

	foo := finder.GetAttribute("foo")
	if foo == nil || foo.ValueType != descriptorpb.DOUBLE {
		t.Fail()
	}

	bar := finder.GetAttribute("bar")
	if bar != nil {
		t.Fail()
	}

	expected := `Attributes:
  baz: INT64
  foo: DOUBLE
`
	s := fmt.Sprintf("%v", finder)
	if s != expected {
		t.Log(s)
		t.Log("!=")
		t.Logf(expected)
		t.Fatal("finder.String() mismatch")
	}
}

func TestChainedFinder(t *testing.T) {
	finder := NewFinder(map[string]*configpb.AttributeManifest_AttributeInfo{
		"foo": {
			ValueType: descriptorpb.DOUBLE,
		},
		"bar": {
			ValueType: descriptorpb.DOUBLE,
		},
	})

	child := NewChainedFinder(finder, map[string]*configpb.AttributeManifest_AttributeInfo{
		"bar": {
			ValueType: descriptorpb.INT64,
		},
	})

	foo := child.GetAttribute("foo")
	if foo == nil || foo.ValueType != descriptorpb.DOUBLE {
		t.Errorf("unexpected attribute info %v", foo)
	}

	bar := child.GetAttribute("bar")
	if bar == nil || bar.ValueType != descriptorpb.INT64 {
		t.Errorf("unexpected attribute info %v", bar)
	}
}
