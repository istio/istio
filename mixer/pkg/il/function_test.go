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
	"testing"
)

func TestNewFunctionTable(t *testing.T) {
	s := newStringTable()
	f := newFunctionTable(s)

	if f.strings != s {
		t.Fatal()
	}
	if len(f.functions) != 0 {
		t.Fatal()
	}
}

func TestNames(t *testing.T) {
	s := newStringTable()
	f := newFunctionTable(s)

	if len(f.Names()) != 0 {
		t.Fatalf("unknown name in Names: %s", f.Names()[0])
	}

	f.add(&Function{
		ID:         s.GetID("foo"),
		ReturnType: Void,
	})

	if len(f.Names()) != 1 {
		t.Fatal()
	}

	if f.Names()[0] != "foo" {
		t.Fatal()
	}
}

func TestGet(t *testing.T) {
	s := newStringTable()
	f := newFunctionTable(s)

	fn := f.Get("foo")
	if fn != nil {
		t.Fatal()
	}

	foo := &Function{
		ID:         s.GetID("foo"),
		ReturnType: Void,
	}
	f.add(foo)

	if len(f.Names()) != 1 {
		t.Fatal()
	}

	fn = f.Get("foo")
	if fn != foo {
		t.Fatal()
	}
}

func TestGetByID(t *testing.T) {
	s := newStringTable()
	f := newFunctionTable(s)

	fn := f.GetByID(0)
	if fn != nil {
		t.Fatal()
	}

	foo := &Function{
		ID:         s.GetID("foo"),
		ReturnType: Void,
	}
	f.add(foo)

	i := f.IDOf("foo")

	fn = f.GetByID(i)
	if fn != foo {
		t.Fatal()
	}
}

func TestIDOf(t *testing.T) {
	s := newStringTable()
	f := newFunctionTable(s)

	i := f.IDOf("foo")
	if i != 0 {
		t.Fatal()
	}

	foo := &Function{
		ID:         s.GetID("foo"),
		ReturnType: Void,
	}
	f.add(foo)

	i = f.IDOf("foo")
	if i == 0 {
		t.Fatal()
	}

	fn := f.GetByID(i)
	if fn != foo {
		t.Fatal()
	}
}
