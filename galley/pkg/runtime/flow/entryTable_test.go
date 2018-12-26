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

package flow

import (
	"reflect"
	"testing"

	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestEntryTable_CountEmpty(t *testing.T) {
	c := NewEntryTable()

	if c.Count() != 0 {
		t.Fatalf("Unexpected table size: %d", c.Count())
	}
}

func TestEntryTable_CountOne(t *testing.T) {
	c := NewEntryTable()

	changed := c.Set(addRes1V1().Entry)
	if !changed {
		t.Fatal("expected table to change")
	}

	if c.Count() != 1 {
		t.Fatalf("Unexpected table size: %d", c.Count())
	}
}

func TestEntryTable_SetTwice(t *testing.T) {
	c := NewEntryTable()

	g := c.Generation()

	changed := c.Set(addRes1V1().Entry)
	if !changed {
		t.Fatal("expected table to change")
	}

	if g == c.Generation() {
		t.Fatalf("The generation should have changed")
	}
	g = c.Generation()

	changed = c.Set(addRes1V1().Entry)
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

func TestEntryTable_SetNewVersion(t *testing.T) {
	c := NewEntryTable()

	g := c.Generation()

	changed := c.Set(addRes1V1().Entry)
	if !changed {
		t.Fatal("expected table to change")
	}

	if g == c.Generation() {
		t.Fatalf("The generation should have changed")
	}
	g = c.Generation()

	changed = c.Set(updateRes1V2().Entry)
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

func TestEntryTable_NamesEmpty(t *testing.T) {
	c := NewEntryTable()

	if len(c.Names()) != 0 {
		t.Fatalf("unexpected name found: %v", c.Names())
	}
}

func TestEntryTable_NamesOneEntry(t *testing.T) {
	c := NewEntryTable()

	_ = c.Set(addRes1V1().Entry)

	if len(c.Names()) != 1 {
		t.Fatalf("unexpected names: %v", c.Names())
	}

	expected := "ns1/res1"
	if c.Names()[0].String() != expected {
		t.Fatalf("mismatch: got:%s, wanted:%v", c.Names()[0].String(), expected)
	}
}

func TestEntryTable_NamesTwoEntries(t *testing.T) {
	c := NewEntryTable()

	_ = c.Set(addRes1V1().Entry)
	_ = c.Set(addRes2V1().Entry)

	if len(c.Names()) != 2 {
		t.Fatalf("unexpected names: %v", c.Names())
	}
}

func TestEntryTable_Item(t *testing.T) {
	c := NewEntryTable()

	_ = c.Set(addRes1V1().Entry)

	item := c.Item(addRes1V1().Entry.ID.FullName)
	if !reflect.DeepEqual(item, addRes1V1().Entry) {
		t.Fatalf("mismatch: got:%s, wanted:%v", item, addRes1V1().Entry)
	}
}

func TestEntryTable_Remove(t *testing.T) {
	c := NewEntryTable()

	_ = c.Set(addRes1V1().Entry)

	removed := c.Remove(addRes1V1().Entry.ID.FullName)
	if !removed {
		t.Fatal("Should have removed")
	}

	removed = c.Remove(addRes1V1().Entry.ID.FullName)
	if removed {
		t.Fatal("Should not have removed")
	}
}

func TestEntryTable_ForEachItem(t *testing.T) {
	c := NewEntryTable()

	_ = c.Set(addRes1V1().Entry)
	_ = c.Set(addRes2V1().Entry)

	count := 0
	c.ForEachItem(func(i resource.Entry) {
		count++
	})

	if count != 2 {
		t.Fatalf("unexpected count: %v", count)
	}
}

func TestNewEntryAccumulator_BogusEvent(t *testing.T) {
	c := NewEntryTable()

	changed := c.Handle(bogusEvent())
	if changed {
		t.Fatal("Expected no change due to failure")
	}
}

func TestNewEntryAccumulator_DoubleAdd(t *testing.T) {
	c := NewEntryTable()

	changed := c.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change after add event")
	}

	changed = c.Handle(addRes1V1())
	if changed {
		t.Fatal("Expected no change after second add")
	}
}

func TestNewEntryAccumulator_AddRemove(t *testing.T) {
	c := NewEntryTable()

	changed := c.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change after add event")
	}

	changed = c.Handle(delete1())
	if !changed {
		t.Fatal("Expected a change after add event")
	}
}

func TestNewEntryAccumulator_Remove(t *testing.T) {
	c := NewEntryTable()

	changed := c.Handle(delete1())
	if changed {
		t.Fatal("Expected no change after delete")
	}
}
