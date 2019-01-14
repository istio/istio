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
	"sort"
	"strings"
	"testing"

	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestCollectionView_TypeURL(t *testing.T) {
	c := NewTable()
	v := NewTableView(emptyInfo.TypeURL, c, nil)

	if v.Type() != emptyInfo.TypeURL {
		t.Fatalf("unexpected type URL: %v", v.Type())
	}
}

func TestCollectionView_Get(t *testing.T) {
	c := NewTable()
	v := NewTableView(emptyInfo.TypeURL, c, nil)

	r1, err := resource.ToMcpResource(addRes1V1().Entry)
	if err != nil {
		t.Fatal(err)
	}

	r2, err := resource.ToMcpResource(addRes2V1().Entry)
	if err != nil {
		t.Fatal(err)
	}

	_ = c.Set(addRes1V1().Entry.ID, r1)
	_ = c.Set(addRes2V1().Entry.ID, r2)

	items := v.Get()
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

	if c.Generation() != v.Generation() {
		t.Fatalf("mismatch: got:%v, wanted:%v", v.Generation(), c.Generation())
	}
}

func TestCollectionView_ConversionError(t *testing.T) {
	c := NewTable()
	v := NewTableView(emptyInfo.TypeURL, c, nil)

	_ = c.Set(addRes1V1().Entry.ID, addRes2V1().Entry)

	items := v.Get()
	if len(items) != 0 {
		t.Fatalf("unexpected items: %v", items)
	}
}
