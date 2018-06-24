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

	"github.com/gogo/protobuf/proto"
)

func TestSchema_All(t *testing.T) {
	// Test Schema.All in isolation, as the rest of the tests depend on it.
	s := Schema{
		byName: make(map[MessageName]Info),
		byURL:  make(map[string]Info),
	}

	s.Register("bar")
	s.Register("foo")

	infos := s.All()
	sort.Slice(infos, func(i, j int) bool {
		return strings.Compare(string(infos[i].MessageName), string(infos[j].MessageName)) < 0
	})

	expected := []Info{
		{
			MessageName: MessageName("bar"),
			TypeURL:     BaseTypeURL + "/bar",
		},
		{
			MessageName: MessageName("foo"),
			TypeURL:     BaseTypeURL + "/foo",
		},
	}

	if !reflect.DeepEqual(expected, infos) {
		t.Fatalf("Mismatch.\nExpected:\n%v\nActual:\n%v\n", expected, infos)
	}
}

func TestSchema_NewProtoInstance(t *testing.T) {
	for _, info := range Types.All() {
		p := info.NewProtoInstance()
		name := proto.MessageName(p)
		if name != string(info.MessageName) {
			t.Fatalf("Name/MessageName mismatch: MessageName:%v, Name:%v", info.MessageName, name)
		}
	}
}

func TestSchema_LookupByKind(t *testing.T) {
	for _, info := range Types.All() {
		i, found := Types.LookupByMessageName(info.MessageName)

		if !found {
			t.Fatalf("Expected info not found: %v", info)
		}

		if i != info {
			t.Fatalf("Lookup mismatch. Expected:%v, Actual:%v", info, i)
		}
	}
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
