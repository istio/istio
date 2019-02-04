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

package resource

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	// Pull in gogo well-known types
	_ "github.com/gogo/protobuf/types"
)

func TestSchema_All(t *testing.T) {
	// Test schema.All in isolation, as the rest of the tests depend on it.
	s := Schema{
		byCollection: make(map[string]Info),
	}

	foo := Info{Collection: Collection{"zoo/tar/com/foo"}}
	bar := Info{Collection: Collection{"zoo/tar/com/bar"}}
	s.byCollection[foo.Collection.String()] = foo
	s.byCollection[bar.Collection.String()] = bar

	infos := s.All()
	sort.Slice(infos, func(i, j int) bool {
		return strings.Compare(infos[i].Collection.String(), infos[j].Collection.String()) < 0
	})

	expected := []Info{
		{
			Collection: bar.Collection,
		},
		{
			Collection: foo.Collection,
		},
	}

	if !reflect.DeepEqual(expected, infos) {
		t.Fatalf("Mismatch.\nExpected:\n%v\nActual:\n%v\n", expected, infos)
	}
}

func TestSchemaBuilder_Register_Success(t *testing.T) {
	b := NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	if _, found := s.byCollection["foo"]; !found {
		t.Fatalf("Empty type should have been registered")
	}
}

func TestRegister_DoubleRegistrationPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	b := NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Empty")
	b.Register("foo", "type.googleapis.com/google.protobuf.Empty")
}

func TestRegister_UnknownProto_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	b := NewSchemaBuilder()
	b.Register("unknown", "type.googleapis.com/unknown")
}

func TestRegister_BadCollection(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	b := NewSchemaBuilder()
	b.Register("badCollection", "ftp://type.googleapis.com/google.protobuf.Empty")
}

func TestSchema_Lookup(t *testing.T) {
	b := NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	_, ok := s.Lookup("foo")
	if !ok {
		t.Fatal("Should have found the info")
	}

	_, ok = s.Lookup("bar")
	if ok {
		t.Fatal("Shouldn't have found the info")
	}

	if _, found := s.byCollection["foo"]; !found {
		t.Fatalf("Empty type should have been registered")
	}
}

func TestSchema_Get_Success(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("should not have panicked: %v", r)
		}
	}()

	b := NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	i := s.Get("foo")
	if i.Collection.String() != "foo" {
		t.Fatalf("Unexpected info: %v", i)
	}
}

func TestSchema_Get_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	b := NewSchemaBuilder()
	b.Register("panic", "type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	_ = s.Get("type.googleapis.com/foo")
}

func TestSchema_Collections(t *testing.T) {
	b := NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Empty")
	b.Register("bar", "type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	actual := s.Collections()
	sort.Strings(actual)

	expected := []string{
		"bar",
		"foo",
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Mismatch\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}
