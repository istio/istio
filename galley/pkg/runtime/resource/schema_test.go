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
	// Test Schema.All in isolation, as the rest of the tests depend on it.
	s := Schema{
		byURL: make(map[string]Info),
	}

	foo := Info{TypeURL: TypeURL{"zoo.tar.com/foo"}}
	bar := Info{TypeURL: TypeURL{"zoo.tar.com/bar"}}
	s.byURL[foo.TypeURL.String()] = foo
	s.byURL[bar.TypeURL.String()] = bar

	infos := s.All()
	sort.Slice(infos, func(i, j int) bool {
		return strings.Compare(infos[i].TypeURL.String(), infos[j].TypeURL.String()) < 0
	})

	expected := []Info{
		{
			TypeURL: TypeURL{"zoo.tar.com/bar"},
		},
		{
			TypeURL: TypeURL{"zoo.tar.com/foo"},
		},
	}

	if !reflect.DeepEqual(expected, infos) {
		t.Fatalf("Mismatch.\nExpected:\n%v\nActual:\n%v\n", expected, infos)
	}
}

func TestSchemaBuilder_Register_Success(t *testing.T) {
	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	if _, found := s.byURL["type.googleapis.com/google.protobuf.Empty"]; !found {
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
	b.Register("type.googleapis.com/google.protobuf.Empty")
	b.Register("type.googleapis.com/google.protobuf.Empty")
}

func TestRegister_UnknownProto_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/unknown")
}

func TestRegister_BadTypeURL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	b := NewSchemaBuilder()
	b.Register("ftp://type.googleapis.com/google.protobuf.Empty")
}

func TestSchema_Lookup(t *testing.T) {
	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	_, ok := s.Lookup("type.googleapis.com/google.protobuf.Empty")
	if !ok {
		t.Fatal("Should have found the info")
	}

	_, ok = s.Lookup("type.googleapis.com/Foo")
	if ok {
		t.Fatal("Shouldn't have found the info")
	}

	if _, found := s.byURL["type.googleapis.com/google.protobuf.Empty"]; !found {
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
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	i := s.Get("type.googleapis.com/google.protobuf.Empty")
	if i.TypeURL.String() != "type.googleapis.com/google.protobuf.Empty" {
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
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	_ = s.Get("type.googleapis.com/foo")
}

func TestSchema_TypeURLs(t *testing.T) {
	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	b.Register("type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	actual := s.TypeURLs()
	sort.Strings(actual)

	expected := []string{
		"type.googleapis.com/google.protobuf.Empty",
		"type.googleapis.com/google.protobuf.Struct",
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Mismatch\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

//
//func TestSchema_NewProtoInstance(t *testing.T) {
//	for _, info := range Types.All() {
//		p := info.NewProtoInstance()
//		name := plang.MessageName(p)
//		if name != info.TypeURL.MessageName() {
//			t.Fatalf("Name/TypeURL mismatch: TypeURL:%v, Name:%v", info.TypeURL, name)
//		}
//	}
//}
//
//func TestSchema_LookupByTypeURL(t *testing.T) {
//	for _, info := range Types.All() {
//		i, found := Types.Lookup(info.TypeURL.string)
//
//		if !found {
//			t.Fatalf("Expected info not found: %v", info)
//		}
//
//		if i != info {
//			t.Fatalf("Lookup mismatch. Expected:%v, Actual:%v", info, i)
//		}
//	}
//}
//
//func TestSchema_TypeURLs(t *testing.T) {
//	for _, url := range Types.TypeURLs() {
//		_, found := Types.Lookup(url)
//
//		if !found {
//			t.Fatalf("Expected info not found: %v", url)
//		}
//	}
//}
