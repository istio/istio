//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package converter

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/galley/pkg/kube/converter/legacy"
	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestGet(t *testing.T) {
	for name := range converters {
		// Should not panic
		_ = Get(name)
	}
}

func TestGet_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Should have panicked")
		}
	}()

	_ = Get("zzzzz")
}

func TestIdentity(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Struct", true)
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Struct")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	pb, err := identity(info, u)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	actual, ok := pb.(*types.Struct)
	if !ok {
		t.Fatalf("Unable to convert to struct: %v", pb)
	}

	expected := &types.Struct{
		Fields: map[string]*types.Value{
			"foo": {
				Kind: &types.Value_StringValue{
					StringValue: "bar",
				},
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestLegacyMixerResource(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Struct", true)
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Struct")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	pb, err := legacyMixerResource(info, u)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	actual, ok := pb.(*legacy.LegacyMixerResource)
	if !ok {
		t.Fatalf("Unable to convert to legacy: %v", pb)
	}

	expected := &legacy.LegacyMixerResource{
		Kind: "k1",
		Contents: &types.Struct{
			Fields: map[string]*types.Value{
				"foo": {
					Kind: &types.Value_StringValue{
						StringValue: "bar",
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestLegayMixerResource_Error(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Any", true)
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Any")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"spec": 23,
		},
	}

	_, err := legacyMixerResource(info, u)
	if err == nil {
		t.Fatalf("expected error not found")
	}
}
