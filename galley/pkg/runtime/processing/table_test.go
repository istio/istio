//  Copyright 2019 Istio Authors
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

package processing


import (
	"reflect"
	"testing"
)

func TestCollection_CountEmpty(t *testing.T) {
	c := NewTable()

	if c.Count() != 0 {
		t.Fatalf("Unexpected table size: %d", c.Count())
	}
}

func TestCollection_CountOne(t *testing.T) {
	c := NewTable()

	changed := c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)
	if !changed {
		t.Fatal("expected table to change")
	}

	if c.Count() != 1 {
		t.Fatalf("Unexpected table size: %d", c.Count())
	}
}

func TestCollection_SetTwice(t *testing.T) {
	c := NewTable()

	g := c.Generation()

	changed := c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)
	if !changed {
		t.Fatal("expected table to change")
	}

	if g == c.Generation() {
		t.Fatalf("The generation should have changed")
	}
	g = c.Generation()

	changed = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)
	if changed {
		t.Fatal("expected table to not change")
	}

	if g != c.Generation() {
		t.Fatalf("The generation should not have changed")
	}

	if c.Count() != 1 {
		t.Fatalf("Unexpected table size: %d", c.Count())
	}
}

func TestCollection_SetNewVersion(t *testing.T) {
	c := NewTable()

	g := c.Generation()

	changed := c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)
	if !changed {
		t.Fatal("expected table to change")
	}

	if g == c.Generation() {
		t.Fatalf("The generation should have changed")
	}
	g = c.Generation()

	changed = c.Set(updateRes1V2().Entry.ID, updateRes1V2().Entry.Item)
	if !changed {
		t.Fatal("expected table to change")
	}

	if g == c.Generation() {
		t.Fatalf("The generation should have changed")
	}

	if c.Count() != 1 {
		t.Fatalf("Unexpected table size: %d", c.Count())
	}
}

func TestCollection_NamesEmpty(t *testing.T) {
	c := NewTable()

	if len(c.Names()) != 0 {
		t.Fatalf("unexpected name found: %v", c.Names())
	}
}

func TestCollection_NamesOneEntry(t *testing.T) {
	c := NewTable()

	_ = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)

	if len(c.Names()) != 1 {
		t.Fatalf("unexpected names: %v", c.Names())
	}

	expected := "ns1/res1"
	if c.Names()[0].String() != expected {
		t.Fatalf("mismatch: got:%s, wanted:%v", c.Names()[0].String(), expected)
	}
}

func TestCollection_NamesTwoEntries(t *testing.T) {
	c := NewTable()

	_ = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)
	_ = c.Set(addRes2V1().Entry.ID, addRes2V1().Entry.Item)

	if len(c.Names()) != 2 {
		t.Fatalf("unexpected names: %v", c.Names())
	}
}

func TestCollection_Item(t *testing.T) {
	c := NewTable()

	_ = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)

	item := c.Item(addRes1V1().Entry.ID.FullName)
	if !reflect.DeepEqual(item, addRes1V1().Entry.Item) {
		t.Fatalf("mismatch: got:%s, wanted:%v", item, addRes1V1().Entry.Item)
	}
}

func TestCollection_Remove(t *testing.T) {
	c := NewTable()

	_ = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)

	removed := c.Remove(addRes1V1().Entry.ID.FullName)
	if !removed {
		t.Fatal("Should have removed")
	}

	removed = c.Remove(addRes1V1().Entry.ID.FullName)
	if removed {
		t.Fatal("Should not have removed")
	}
}

func TestCollection_Version(t *testing.T) {
	c := NewTable()

	_ = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)

	v := c.Version(addRes1V1().Entry.ID.FullName)
	if !reflect.DeepEqual(v, addRes1V1().Entry.ID.Version) {
		t.Fatalf("mismatch: got:%s, wanted:%v", v, addRes1V1().Entry.ID.Version)
	}
}

func TestCollection_GetTwoEntries(t *testing.T) {
	c := NewTable()

	_ = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)
	_ = c.Set(addRes2V1().Entry.ID, addRes2V1().Entry.Item)

	items := c.Get()
	if len(items) != 2 {
		t.Fatalf("unexpected items: %v", items)
	}
}

func TestCollection_ForEachItem(t *testing.T) {
	c := NewTable()

	_ = c.Set(addRes1V1().Entry.ID, addRes1V1().Entry.Item)
	_ = c.Set(addRes2V1().Entry.ID, addRes2V1().Entry.Item)

	count := 0
	c.ForEachItem(func(i interface{}) {
		count++
	})

	if count != 2 {
		t.Fatalf("unexpected count: %v", count)
	}
}

