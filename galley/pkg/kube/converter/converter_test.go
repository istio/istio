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
	"time"

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

const fakeCreateTime = "2009-02-04T21:00:57-08:00"

func TestIdentity(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Struct")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"creationTimestamp": fakeCreateTime,
			},
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := "key"

	outkey, createTime, pb, err := identity(info, key, u)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if key != outkey {
		t.Fatalf("Keys mismatch. Wanted=%s, Got=%s", key, outkey)
	}

	if createTime.Format(time.RFC3339) != fakeCreateTime {
		t.Fatalf("createTime mismatch: got %q want %q",
			createTime.Format(time.RFC3339), fakeCreateTime)
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

func TestIdentity_Error(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Empty")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := "key"

	_, _, _, err := identity(info, key, u)
	if err == nil {
		t.Fatal("Expected error not found")
	}
}

func TestLegacyMixerResource(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Struct")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"metadata": map[string]interface{}{
				"creationTimestamp": fakeCreateTime,
			},
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := "key"

	outkey, createTime, pb, err := legacyMixerResource(info, key, u)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedKey := "k1/" + key
	if outkey != expectedKey {
		t.Fatalf("Keys mismatch. Wanted=%s, Got=%s", expectedKey, outkey)
	}

	if createTime.Format(time.RFC3339) != fakeCreateTime {
		t.Fatalf("createTime mismatch: got %q want %q",
			createTime.Format(time.RFC3339), fakeCreateTime)
	}

	actual, ok := pb.(*legacy.LegacyMixerResource)
	if !ok {
		t.Fatalf("Unable to convert to legacy: %v", pb)
	}

	expected := &legacy.LegacyMixerResource{
		Name: "key",
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
	b.Register("type.googleapis.com/google.protobuf.Any")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Any")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"spec": 23,
		},
	}

	key := "key"

	_, _, _, err := legacyMixerResource(info, key, u)
	if err == nil {
		t.Fatalf("expected error not found")
	}
}
