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
	"sort"
	"strings"
	"testing"

)

func TestStoredProjection_TypeURL(t *testing.T) {
	p := NewStoredProjection(emptyInfo.TypeURL)

	if p.Type() != emptyInfo.TypeURL {
		t.Fatalf("unexpected type URL: %v", p.Type())
	}
}

func TestStoredProjection_Get(t *testing.T) {
	p := NewStoredProjection(emptyInfo.TypeURL)

	fl := &fakeListener{}
	p.SetProjectionListener(fl)

	p.Set(addRes1V1().Entry.ID.FullName, toMcpResourceOrPanic(addRes1V1().Entry))
	p.Set(addRes2V1().Entry.ID.FullName, toMcpResourceOrPanic(addRes2V1().Entry))

	fl.changed = false

	if fl.changed {
		t.Fatal("Calling get should not have caused notification")
	}

	items := p.Get()
	if len(items) != 2 {
		t.Fatalf("unexpected items: %v", items)
	}

	sort.Slice(items, func(i, j int) bool {
		li := items[i]
		lj := items[j]
		return strings.Compare(li.Metadata.Name, lj.Metadata.Name) < 0
	})

	expected := "ns1/res1"
	if items[0].Metadata.Name != expected {
		t.Fatalf("mismatch: got:%v, wanted:%v", items[0].Metadata.Name, expected)
	}

	expected = "ns1/res2"
	if items[1].Metadata.Name != expected {
		t.Fatalf("mismatch: got:%v, wanted:%v", items[1].Metadata.Name, expected)
	}
}

func TestStoredProjection_Set(t *testing.T) {
	p := NewStoredProjection(emptyInfo.TypeURL)

	fl := &fakeListener{}
	p.SetProjectionListener(fl)

	g := p.Generation()
	p.Set(addRes1V1().Entry.ID.FullName, toMcpResourceOrPanic(addRes1V1().Entry))

	if !fl.changed {
		t.Fatal("Calling set should have caused notification")
	}
	if g == p.Generation() {
		t.Fatal("The generation should have changed")
	}

	fl.changed = false
	g = p.Generation()
	p.Set(addRes1V1().Entry.ID.FullName, toMcpResourceOrPanic(res1V1()))
	if fl.changed {
		t.Fatal("Second call without version change should not have caused notification")
	}
	if g != p.Generation() {
		t.Fatal("The generation should not have changed")
	}

	if len(p.entries) != 1 {
		t.Fatalf("There should have been 1 item: %v", len(p.entries))
	}
}

func TestStoredProjection_Remove(t *testing.T) {
	p := NewStoredProjection(emptyInfo.TypeURL)

	fl := &fakeListener{}
	p.SetProjectionListener(fl)

	p.Set(addRes1V1().Entry.ID.FullName, toMcpResourceOrPanic(res1V1()))

	fl.changed = false
	g := p.Generation()
	p.Remove(res1V1().ID.FullName)
	if !fl.changed {
		t.Fatal("Calling remove should have caused notification")
	}
	if g == p.Generation() {
		t.Fatal("The generation should have changed")
	}

	fl.changed = false
	g = p.Generation()
	p.Remove(res1V1().ID.FullName)
	if fl.changed {
		t.Fatal("Second call should not have caused notification")
	}
	if g != p.Generation() {
		t.Fatal("The generation should not have changed")
	}

	if len(p.entries) != 0 {
		t.Fatalf("There should have been 0 items: %v", len(p.entries))
	}
}

type fakeListener struct {
	changed bool
}

var _ ProjectionListener = &fakeListener{}

func (f *fakeListener) ProjectionChanged(p Projection) {
	f.changed = true
}
