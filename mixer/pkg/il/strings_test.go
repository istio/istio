// Copyright 2017 Istio Authors
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

package il

import (
	"fmt"
	"testing"
)

func TestNewStringTable(t *testing.T) {
	s := newStringTable()

	if s.nextID == 0 {
		t.Fatal()
	}

	if len(s.stringToID) != 1 {
		t.Fatal()
	}

	if s.idToString[0] != nullString {
		t.Fatal()
	}
}

func TestGetID(t *testing.T) {
	s := newStringTable()

	i := s.GetID("foo")
	if s.nextID != i+1 {
		t.Fatal()
	}

	if s.idToString[i] != "foo" {
		t.Fatal()
	}

	if s.stringToID["foo"] != i {
		t.Fatal()
	}

	i2 := s.GetID("foo")
	if i != i2 {
		t.Fatal()
	}
}

func TestTryGetID(t *testing.T) {
	s := newStringTable()

	n := s.nextID
	i := s.TryGetID("foo")
	if i != 0 {
		t.Fatal()
	}

	if s.nextID != n {
		t.Fatal()
	}

	i = s.GetID("foo")
	if i != s.TryGetID("foo") {
		t.Fatal()
	}
}

func TestGetString(t *testing.T) {
	s := newStringTable()

	if s.GetString(0) != nullString {
		t.Fatal()
	}

	if s.GetString(1) != "" {
		t.Fatal()
	}

	i := s.GetID("foo")
	if s.GetString(i) != "foo" {
		t.Fatal()
	}
}

func TestExpansion(t *testing.T) {
	s := newStringTable()

	strMap := make(map[uint32]string)

	for i := 1; i < allocSize*10+1; i++ {
		str := fmt.Sprintf("str-%d", i)
		id := s.GetID(str)
		strMap[id] = str
	}

	for id, str := range strMap {

		actualStr := s.GetString(id)
		if actualStr != str {
			t.Fatal()
		}

		actualID := s.GetID(str)
		if actualID != id {
			t.Fatal()
		}
	}
}
