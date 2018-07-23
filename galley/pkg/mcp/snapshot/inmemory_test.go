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

package snapshot

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/runtime/resource"

	mcp "istio.io/api/config/mcp/v1alpha1"
)

var (
	typeURL = resource.MustTypeURL("type.googleapis.com/type")
)

func TestInMemoryBuilder(t *testing.T) {
	b := NewInMemoryBuilder()
	sn := b.Build()

	if len(sn.envelopes) != 0 {
		t.Fatal("Envelopes should have been empty")
	}

	if len(sn.versions) != 0 {
		t.Fatal("Versions should have been empty")
	}
}

func TestInMemoryBuilder_Set(t *testing.T) {
	b := NewInMemoryBuilder()

	items := []*mcp.Envelope{{Resource: &types.Any{}, Metadata: &mcp.Metadata{Name: "foo"}}}
	b.Set(typeURL, "v1", items)
	sn := b.Build()

	if sn.Version(typeURL) != "v1" {
		t.Fatalf("Unexpected version: %v", sn.Version(typeURL))
	}

	actual := sn.Resources(typeURL)
	if !reflect.DeepEqual(items, sn.Resources(typeURL)) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, items)
	}
}

func TestInMemoryBuilder_SetEntry_Add(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})

	sn := b.Build()

	expected := []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{Name: "foo"},
			Resource: &types.Any{TypeUrl: typeURL.String(), Value: []byte{}},
		},
	}
	actual := sn.Resources(typeURL)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_SetEntry_Update(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	_ = b.SetEntry(typeURL, "foo", &types.Any{})

	sn := b.Build()

	expected := []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{Name: "foo"},
			Resource: &types.Any{TypeUrl: typeURL.String(), Value: []byte{}},
		},
	}
	actual := sn.Resources(typeURL)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_SetEntry_Marshal_Error(t *testing.T) {
	b := NewInMemoryBuilder()

	err := b.SetEntry(typeURL, "foo", nil)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestInMemoryBuilder_DeleteEntry_EntryNotFound(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	b.DeleteEntry(typeURL, "bar")
	sn := b.Build()

	expected := []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{Name: "foo"},
			Resource: &types.Any{TypeUrl: typeURL.String(), Value: []byte{}},
		},
	}
	actual := sn.Resources(typeURL)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_DeleteEntry_TypeNotFound(t *testing.T) {
	b := NewInMemoryBuilder()

	b.DeleteEntry(typeURL, "bar")
	sn := b.Build()

	if len(sn.envelopes) != 0 {
		t.Fatal("Envelopes should have been empty")
	}

	if len(sn.versions) != 0 {
		t.Fatal("Versions should have been empty")
	}
}

func TestInMemoryBuilder_DeleteEntry_Single(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	b.DeleteEntry(typeURL, "foo")
	sn := b.Build()

	if len(sn.envelopes) != 0 {
		t.Fatal("Envelopes should have been empty")
	}

	if len(sn.versions) != 0 {
		t.Fatal("Versions should have been empty")
	}
}

func TestInMemoryBuilder_DeleteEntry_Multiple(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	_ = b.SetEntry(typeURL, "bar", &types.Any{})
	b.DeleteEntry(typeURL, "foo")
	sn := b.Build()

	expected := []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{Name: "bar"},
			Resource: &types.Any{TypeUrl: typeURL.String(), Value: []byte{}},
		},
	}
	actual := sn.Resources(typeURL)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_SetVersion(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	b.SetVersion(typeURL, "v1")
	sn := b.Build()

	if sn.Version(typeURL) != "v1" {
		t.Fatalf("Unexpected version: %s", sn.Version(typeURL))
	}
}

func TestInMemory_Clone(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	_ = b.SetEntry(typeURL, "bar", &types.Any{})
	b.SetVersion(typeURL, "v1")
	sn := b.Build()

	sn2 := sn.Clone()

	expected := []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{Name: "bar"},
			Resource: &types.Any{TypeUrl: typeURL.String()},
		},
		{
			Metadata: &mcp.Metadata{Name: "foo"},
			Resource: &types.Any{TypeUrl: typeURL.String()},
		},
	}

	actual := sn2.Resources(typeURL)

	sort.Slice(actual, func(i, j int) bool {
		return strings.Compare(
			actual[i].Metadata.Name,
			actual[j].Metadata.Name) < 0
	})

	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}

	if sn2.Version(typeURL) != "v1" {
		t.Fatalf("Unexpected version: %s", sn2.Version(typeURL))
	}
}

func TestInMemory_Builder(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	_ = b.SetEntry(typeURL, "bar", &types.Any{})
	b.SetVersion(typeURL, "v1")
	sn := b.Build()

	b = sn.Builder()

	sn2 := b.Build()

	expected := []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{Name: "bar"},
			Resource: &types.Any{TypeUrl: typeURL.String()},
		},
		{
			Metadata: &mcp.Metadata{Name: "foo"},
			Resource: &types.Any{TypeUrl: typeURL.String()},
		},
	}

	actual := sn2.Resources(typeURL)

	sort.Slice(actual, func(i, j int) bool {
		return strings.Compare(
			actual[i].Metadata.Name,
			actual[j].Metadata.Name) < 0
	})

	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}

	if sn2.Version(typeURL) != "v1" {
		t.Fatalf("Unexpected version: %s", sn2.Version(typeURL))
	}
}

func TestInMemory_String(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry(typeURL, "foo", &types.Any{})
	_ = b.SetEntry(typeURL, "bar", &types.Any{})
	b.SetVersion(typeURL, "v1")
	sn := b.Build()

	// Shouldn't crash
	_ = sn.String()
}
