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

	pgogo "github.com/gogo/protobuf/proto"
	plang "github.com/golang/protobuf/proto"

	// Pull in gogo & golang Empty
	_ "github.com/gogo/protobuf/types"
	_ "github.com/golang/protobuf/ptypes/empty"
)

func TestSchema_All(t *testing.T) {
	// Test Schema.All in isolation, as the rest of the tests depend on it.
	s := Schema{
		byURL: make(map[string]Info),
	}

	foo := Info{TypeURL: TypeURL{"zoo.tar.com/foo"}, IsGogo: false}
	bar := Info{TypeURL: TypeURL{"zoo.tar.com/bar"}, IsGogo: false}
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

func TestRegister_Success(t *testing.T) {
	s := NewSchema()

	s.Register("type.googleapis.com/google.protobuf.Empty", false)
	if _, found := s.byURL["type.googleapis.com/google.protobuf.Empty"]; !found {
		t.Fatalf("Empty type should have been registered")
	}

	s = NewSchema()

	s.Register("type.googleapis.com/google.protobuf.Empty", true)
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

	s := NewSchema()
	s.Register("type.googleapis.com/google.protobuf.Empty", true)
	s.Register("type.googleapis.com/google.protobuf.Empty", true)
}

func TestRegister_UnknownProto_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	s := NewSchema()
	s.Register("type.googleapis.com/unknown", true)
}

func TestRegister_BadTypeURL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	s := NewSchema()
	s.Register("ftp://type.googleapis.com/google.protobuf.Empty", true)
}

func TestSchema_NewProtoInstance(t *testing.T) {
	for _, info := range Types.All() {
		p := info.NewProtoInstance()
		var name string
		if info.IsGogo {
			name = pgogo.MessageName(p)
		} else {
			name = plang.MessageName(p)
		}
		if name != info.TypeURL.messageName() {
			t.Fatalf("Name/TypeURL mismatch: TypeURL:%v, Name:%v", info.TypeURL, name)
		}
	}
}

func TestSchema_LookupByTypeURL(t *testing.T) {
	for _, info := range Types.All() {
		i, found := Types.Lookup(info.TypeURL.string)

		if !found {
			t.Fatalf("Expected info not found: %v", info)
		}

		if i != info {
			t.Fatalf("Lookup mismatch. Expected:%v, Actual:%v", info, i)
		}
	}
}

func TestSchema_TypeURLs(t *testing.T) {
	for _, url := range Types.TypeURLs() {
		_, found := Types.Lookup(url)

		if !found {
			t.Fatalf("Expected info not found: %v", url)
		}
	}
}
