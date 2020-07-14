// Copyright Istio Authors
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

package contextgraph

import (
	"sort"
	"testing"
)

func TestEntityCache(t *testing.T) {
	e1 := entity{
		containerFullName: "container",
		typeName:          "type",
		fullName:          "container/e1",
		location:          "location",
		shortNames:        [4]string{"e1", "", "", ""},
	}
	e2 := e1
	e2.fullName = "container/e2"
	e2.shortNames[0] = "e2"

	ec := newEntityCache(mockLogger{})

	for epoch, test := range []struct {
		entitiesToAssert  []entity
		checkResults      []bool
		entitiesFromFlush []entity
	}{
		{[]entity{e1, e2}, []bool{true, true}, nil},
		// dupe is suppressed
		{[]entity{e1}, []bool{false}, nil},
		// all entities are flushed
		{nil, nil, []entity{e1, e2}},
		// part of last flush, should be delayed until next flush
		{[]entity{e1}, []bool{false}, nil},
		// part of last flush, should be delayed until flush
		{[]entity{e2}, []bool{false}, []entity{e1, e2}},
		// part of last flush, should be delayed until flush
		{[]entity{e1}, []bool{false}, []entity{e1}},
		// not part of last flush, should be sent immediately
		{[]entity{e2}, []bool{true}, []entity{}},
		// nothing in this flush
		{nil, nil, []entity{}},
	} {
		t.Logf("Epoch %d", epoch)
		for i, e := range test.entitiesToAssert {
			if ec.AssertAndCheck(e, epoch) != test.checkResults[i] {
				t.Errorf("AssertAndCheck(%#v, %d) = %v, want %v", e, epoch, !test.checkResults[i], test.checkResults[i])
			}
		}
		if test.entitiesFromFlush != nil {
			result := ec.Flush(epoch)
			sort.Slice(result, func(i, j int) bool { return result[i].fullName < result[j].fullName })
			if got, want := len(result), len(test.entitiesFromFlush); got != want {
				t.Errorf("Flush(%d) returned %d entities, want %d", epoch, got, want)
			}
			for i, e := range test.entitiesFromFlush {
				if i >= len(result) {
					break
				}
				if e != result[i] {
					t.Errorf("Flush(%d)[%d] = %#v, want %#v", epoch, i, result[i], e)
				}
			}
		}
	}
}

func TestEdgeCache(t *testing.T) {
	e11 := edge{
		sourceFullName:      "source1",
		destinationFullName: "dest1",
		typeName:            "type",
	}
	e12 := e11
	e12.destinationFullName = "dest2"
	e21 := e11
	e21.sourceFullName = "source2"

	ec := newEdgeCache(mockLogger{})

	for epoch, test := range []struct {
		entitiesToInvalidate []string
		edgesToAssert        []edge
		checkResults         []bool
		edgesFromFlush       []edge
	}{
		{nil, []edge{e11, e12}, []bool{true, true}, nil},
		// dupe is suppressed
		{nil, []edge{e11}, []bool{false}, nil},
		// all entities are flushed
		{nil, nil, nil, []edge{e11, e12}},
		// part of last flush, should be delayed until next flush
		{nil, []edge{e11, e21}, []bool{false, true}, nil},
		// part of last flush, should be delayed until flush
		{nil, []edge{e12}, []bool{false}, []edge{e11, e12, e21}},
		// invalidated, should be sent immediately
		{[]string{"source1"}, []edge{e12}, []bool{true}, []edge{}},
		// not part of last flush, should be sent immediately
		{nil, []edge{e21}, []bool{true}, []edge{}},
		// nothing in this flush
		{nil, nil, nil, []edge{}},
	} {
		t.Logf("Epoch %d", epoch)
		for _, n := range test.entitiesToInvalidate {
			ec.Invalidate(n)
		}
		for i, e := range test.edgesToAssert {
			if ec.AssertAndCheck(e, epoch) != test.checkResults[i] {
				t.Errorf("AssertAndCheck(%#v, %d) = %v, want %v", e, epoch, !test.checkResults[i], test.checkResults[i])
			}
		}
		if test.edgesFromFlush != nil {
			result := ec.Flush(epoch)
			sort.Slice(result, func(i, j int) bool {
				sfn1, sfn2 := result[i].sourceFullName, result[j].sourceFullName
				return sfn1 < sfn2 || (sfn1 == sfn2 && result[i].destinationFullName < result[j].destinationFullName)
			})
			if got, want := len(result), len(test.edgesFromFlush); got != want {
				t.Errorf("Flush(%d) returned %d entities, want %d", epoch, got, want)
			}
			for i, e := range test.edgesFromFlush {
				if i >= len(result) {
					break
				}
				if e != result[i] {
					t.Errorf("Flush(%d)[%d] = %#v, want %#v", epoch, i, result[i], e)
				}
			}
		}
	}
}
