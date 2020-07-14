//  Copyright Istio Authors
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
	"time"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
)

var (
	fakeCreateTime      = time.Date(2018, time.January, 1, 2, 3, 4, 5, time.UTC)
	fakeLabels          = map[string]string{"lk1": "lv1"}
	fakeAnnotations     = map[string]string{"ak1": "av1"}
	fakeCreateTimeProto *types.Timestamp
)

func init() {
	var err error
	fakeCreateTimeProto, err = types.TimestampProto(fakeCreateTime)
	if err != nil {
		panic(err)
	}
}

func TestInMemoryBuilder(t *testing.T) {
	b := NewInMemoryBuilder()
	sn := b.Build()

	if len(sn.resources) != 0 {
		t.Fatal("Resources should have been empty")
	}

	if len(sn.versions) != 0 {
		t.Fatal("Versions should have been empty")
	}
}

func TestInMemoryBuilder_Set(t *testing.T) {
	b := NewInMemoryBuilder()

	items := []*mcp.Resource{{Body: &types.Any{}, Metadata: &mcp.Metadata{Name: "foo"}}}
	b.Set("collection", "v1", items)
	sn := b.Build()

	if sn.Version("collection") != "v1" {
		t.Fatalf("Unexpected version: %v", sn.Version("type"))
	}

	actual := sn.Resources("collection")
	if !reflect.DeepEqual(items, sn.Resources("collection")) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, items)
	}
}

func TestInMemoryBuilder_SetEntry_Add(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})

	sn := b.Build()

	expected := []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:        "foo",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},

			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any", Value: []byte{}},
		},
	}
	actual := sn.Resources("collection")
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_SetEntry_Update(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})

	sn := b.Build()

	expected := []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:        "foo",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},
			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any", Value: []byte{}},
		},
	}
	actual := sn.Resources("collection")
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_SetEntry_Marshal_Error(t *testing.T) {
	b := NewInMemoryBuilder()

	err := b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, nil)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestInMemoryBuilder_DeleteEntry_EntryNotFound(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	b.DeleteEntry("collection", "bar")
	sn := b.Build()

	expected := []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:        "foo",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},
			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any", Value: []byte{}},
		},
	}
	actual := sn.Resources("collection")
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_DeleteEntry_TypeNotFound(t *testing.T) {
	b := NewInMemoryBuilder()

	b.DeleteEntry("collection", "bar")
	sn := b.Build()

	if len(sn.resources) != 0 {
		t.Fatal("Resources should have been empty")
	}

	if len(sn.versions) != 0 {
		t.Fatal("Versions should have been empty")
	}
}

func TestInMemoryBuilder_DeleteEntry_Single(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	b.DeleteEntry("collection", "foo")
	sn := b.Build()

	if len(sn.resources) != 0 {
		t.Fatal("Resources should have been empty")
	}

	if len(sn.versions) != 0 {
		t.Fatal("Versions should have been empty")
	}
}

func TestInMemoryBuilder_DeleteEntry_Multiple(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	_ = b.SetEntry("collection", "bar", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	b.DeleteEntry("collection", "foo")
	sn := b.Build()

	expected := []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:        "bar",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},
			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any", Value: []byte{}},
		},
	}
	actual := sn.Resources("collection")
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestInMemoryBuilder_SetVersion(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	b.SetVersion("collection", "v1")
	sn := b.Build()

	if sn.Version("collection") != "v1" {
		t.Fatalf("Unexpected version: %s", sn.Version("type"))
	}
}

func TestInMemory_Clone(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	_ = b.SetEntry("collection", "bar", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	b.SetVersion("collection", "v1")
	sn := b.Build()

	sn2 := sn.Clone()

	expected := []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:        "bar",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},
			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any"},
		},
		{
			Metadata: &mcp.Metadata{
				Name:        "foo",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},
			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any"},
		},
	}

	actual := sn2.Resources("collection")

	sort.Slice(actual, func(i, j int) bool {
		return strings.Compare(
			actual[i].Metadata.Name,
			actual[j].Metadata.Name) < 0
	})

	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}

	if sn2.Version("collection") != "v1" {
		t.Fatalf("Unexpected version: %s", sn2.Version("type"))
	}
}

func TestInMemory_Builder(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	_ = b.SetEntry("collection", "bar", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	b.SetVersion("collection", "v1")
	sn := b.Build()

	b = sn.Builder()

	sn2 := b.Build()

	expected := []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:        "bar",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},
			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any"},
		},
		{
			Metadata: &mcp.Metadata{
				Name:        "foo",
				Version:     "v0",
				CreateTime:  fakeCreateTimeProto,
				Labels:      fakeLabels,
				Annotations: fakeAnnotations,
			},
			Body: &types.Any{TypeUrl: "type.googleapis.com/google.protobuf.Any"},
		},
	}

	actual := sn2.Resources("collection")

	sort.Slice(actual, func(i, j int) bool {
		return strings.Compare(
			actual[i].Metadata.Name,
			actual[j].Metadata.Name) < 0
	})

	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}

	if sn2.Version("collection") != "v1" {
		t.Fatalf("Unexpected version: %s", sn2.Version("type"))
	}
}

func TestInMemory_String(t *testing.T) {
	b := NewInMemoryBuilder()

	_ = b.SetEntry("collection", "foo", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	_ = b.SetEntry("collection", "bar", "v0", fakeCreateTime, fakeLabels, fakeAnnotations, &types.Any{})
	b.SetVersion("collection", "v1")
	sn := b.Build()

	// Shouldn't crash
	_ = sn.String()
}
