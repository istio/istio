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
		byName: make(map[string]Info),
		byURL:  make(map[string]Info),
	}

	foo := Info{MessageName: MessageName{"foo"}, IsGogo: false, TypeURL: "zoo.tar.com/foo"}
	bar := Info{MessageName: MessageName{"bar"}, IsGogo: false, TypeURL: "zoo.tar.com/bar"}
	s.byName[foo.MessageName.string] = foo
	s.byName[bar.MessageName.string] = bar
	s.byURL[foo.TypeURL] = foo
	s.byURL[bar.TypeURL] = bar

	infos := s.All()
	sort.Slice(infos, func(i, j int) bool {
		return strings.Compare(infos[i].MessageName.String(), infos[j].MessageName.String()) < 0
	})

	expected := []Info{
		{
			MessageName: MessageName{"bar"},
			TypeURL:     "zoo.tar.com/bar",
		},
		{
			MessageName: MessageName{"foo"},
			TypeURL:     "zoo.tar.com/foo",
		},
	}

	if !reflect.DeepEqual(expected, infos) {
		t.Fatalf("Mismatch.\nExpected:\n%v\nActual:\n%v\n", expected, infos)
	}
}

func TestRegister_Success(t *testing.T) {
	s := NewSchema()

	s.Register("google.protobuf.Empty", false)
	if _, found := s.byName["google.protobuf.Empty"]; !found {
		t.Fatalf("Empty type should have been registered")
	}

	s = NewSchema()

	s.Register("google.protobuf.Empty", true)
	if _, found := s.byName["google.protobuf.Empty"]; !found {
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
	s.Register("google.protobuf.Empty", true)
	s.Register("google.protobuf.Empty", true)
}

func TestRegister_UnknownProto_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should have panicked")
		}
	}()

	s := NewSchema()
	s.Register("unknown", true)
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
		if name != info.MessageName.String() {
			t.Fatalf("Name/MessageName mismatch: MessageName:%v, Name:%v", info.MessageName, name)
		}
	}
}

func TestSchema_Info(t *testing.T) {
	s := NewSchema()
	s.Register("google.protobuf.Empty", true)
	i, _ := s.LookupByMessageName("google.protobuf.Empty")

	info := s.Info(i.MessageName)
	if info.MessageName.String() != "google.protobuf.Empty" {
		t.Fatalf("Unexpected info found: %v", info)
	}
}

func TestSchema_Info_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Expected panic not found")
		}
	}()

	s := NewSchema()
	s.Info(MessageName{"unknown"})
}

func TestSchema_LookupByTypeURL(t *testing.T) {
	for _, info := range Types.All() {
		i, found := Types.LookupByTypeURL(info.TypeURL)

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
		_, found := Types.LookupByTypeURL(url)

		if !found {
			t.Fatalf("Expected info not found: %v", url)
		}
	}
}
