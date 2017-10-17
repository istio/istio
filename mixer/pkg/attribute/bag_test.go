// Copyright 2016 Istio Authors
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

package attribute

import (
	"flag"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	mixerpb "istio.io/api/mixer/v1"
)

var (
	t9  = time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	t10 = time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)
	t42 = time.Date(2001, 1, 1, 1, 1, 1, 42, time.UTC)

	d1 = 42 * time.Second
	d2 = 34 * time.Second
)

func TestBag(t *testing.T) {
	sm1 := mixerpb.StringMap{Entries: map[int32]int32{-16: -16}}
	sm2 := mixerpb.StringMap{Entries: map[int32]int32{-17: -17}}
	m1 := map[string]string{"N16": "N16"}
	m3 := map[string]string{"N42": "FourtyTwo"}

	attrs := mixerpb.CompressedAttributes{
		Words:      []string{"N1", "N2", "N3", "N4", "N5", "N6", "N7", "N8", "N9", "N10", "N11", "N12", "N13", "N14", "N15", "N16", "N17"},
		Strings:    map[int32]int32{-1: -1, -2: -2},
		Int64S:     map[int32]int64{-3: 3, -4: 4},
		Doubles:    map[int32]float64{-5: 5.0, -6: 6.0},
		Bools:      map[int32]bool{-7: true, -8: false},
		Timestamps: map[int32]time.Time{-9: t9, -10: t10},
		Durations:  map[int32]time.Duration{-11: d1},
		Bytes:      map[int32][]uint8{-12: {12}, -13: {13}},
		StringMaps: map[int32]mixerpb.StringMap{-14: sm1, -15: sm2},
	}

	ab, err := GetBagFromProto(&attrs, nil)
	if err != nil {
		t.Errorf("Unable to start request: %v", err)
	}

	// override a bunch of values
	ab.Set("N2", "42")
	ab.Set("N4", int64(42))
	ab.Set("N6", float64(42.0))
	ab.Set("N8", true)
	ab.Set("N10", t42)
	ab.Set("N11", d2)
	ab.Set("N13", []byte{42})
	ab.Set("N15", m3)

	// make sure the overrides worked and didn't disturb non-overridden values
	results := []struct {
		name  string
		value interface{}
	}{
		{"N1", "N1"},
		{"N2", "42"},
		{"N3", int64(3)},
		{"N4", int64(42)},
		{"N5", 5.0},
		{"N6", 42.0},
		{"N7", true},
		{"N8", true},
		{"N9", t9},
		{"N10", t42},
		{"N11", d2},
		{"N12", []byte{12}},
		{"N13", []byte{42}},
		{"N14", m1},
		{"N15", m3},
	}

	for _, r := range results {
		t.Run(r.name, func(t *testing.T) {
			v, found := ab.Get(r.name)
			if !found {
				t.Error("Got false, expecting true")
			}

			if !reflect.DeepEqual(v, r.value) {
				t.Errorf("Got %v, expected %v", v, r.value)
			}
		})
	}

	if _, found := ab.Get("XYZ"); found {
		t.Error("XYZ was found")
	}

	// try another level of overrides just to make sure that path is OK
	child := GetMutableBag(ab)
	child.Set("N2", "31415692")
	r, found := ab.Get("N2")
	if !found || r.(string) != "42" {
		t.Error("N2 has wrong value")
	}
}

func TestMerge(t *testing.T) {
	mb := GetMutableBag(empty)

	c1 := GetMutableBag(mb)
	c2 := GetMutableBag(mb)

	c1.Set("STRING1", "A")
	c2.Set("STRING2", "B")

	if err := mb.Merge(c1, nil, c2); err != nil {
		t.Errorf("Got %v, expecting success", err)
	}

	if v, ok := mb.Get("STRING1"); !ok || v.(string) != "A" {
		t.Errorf("Got %v, expected A", v)
	}

	if v, ok := mb.Get("STRING2"); !ok || v.(string) != "B" {
		t.Errorf("Got %v, expected B", v)
	}
}

func TestMergeErrors(t *testing.T) {
	mb := GetMutableBag(empty)

	c1 := GetMutableBag(mb)
	c2 := GetMutableBag(mb)

	c1.Set("FOO", "X")
	c2.Set("FOO", "Y")

	if err := mb.Merge(c1, c2); err == nil {
		t.Error("Got success, expected failure")
	} else if !strings.Contains(err.Error(), "FOO") {
		t.Errorf("Expected error to contain the word FOO, got %s", err.Error())
	}
}

func TestEmpty(t *testing.T) {
	b := &emptyBag{}

	if names := b.Names(); len(names) > 0 {
		t.Errorf("Get len %d, expected 0", len(names))
	}

	if _, ok := b.Get("XYZ"); ok {
		t.Errorf("Got true, expected false")
	}

	b.Done()
}

func TestEmptyRoundTrip(t *testing.T) {
	attrs0 := mixerpb.CompressedAttributes{}
	attrs1 := mixerpb.CompressedAttributes{}
	mb := GetMutableBag(nil)
	mb.ToProto(&attrs1, nil, 0)

	if !reflect.DeepEqual(attrs0, attrs1) {
		t.Errorf("Expecting equal attributes, got a delta: original #%v, new #%v", attrs0, attrs1)
	}
}

func TestProtoBag(t *testing.T) {
	globalWordList := []string{"G0", "G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8", "G9"}
	messageWordList := []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9", "M10"}

	globalDict := make(map[string]int32)
	for k, v := range globalWordList {
		globalDict[v] = int32(k)
	}

	sm := mixerpb.StringMap{Entries: map[int32]int32{-6: -7}}

	attrs := mixerpb.CompressedAttributes{
		Words:      messageWordList,
		Strings:    map[int32]int32{4: 5},
		Int64S:     map[int32]int64{6: 42},
		Doubles:    map[int32]float64{7: 42.0},
		Bools:      map[int32]bool{-1: true},
		Timestamps: map[int32]time.Time{-2: t9},
		Durations:  map[int32]time.Duration{-3: d1},
		Bytes:      map[int32][]uint8{-4: {11}},
		StringMaps: map[int32]mixerpb.StringMap{-5: sm},
	}

	cases := []struct {
		name  string
		value interface{}
	}{
		{"G4", "G5"},
		{"G6", int64(42)},
		{"G7", 42.0},
		{"M1", true},
		{"M2", t9},
		{"M3", d1},
		{"M4", []byte{11}},
		{"M5", map[string]string{"M6": "M7"}},
	}

	for j := 0; j < 2; j++ {
		for i := 0; i < 2; i++ {
			var pb *MutableBag
			var err error

			if j == 0 {
				pb, err = GetBagFromProto(&attrs, globalWordList)
				if err != nil {
					t.Fatalf("GetBagFromProto failed with %v", err)
				}
			} else {
				b := NewProtoBag(&attrs, globalDict, globalWordList)
				pb = GetMutableBag(b)
			}

			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					v, ok := pb.Get(c.name)
					if !ok {
						t.Error("Got false, expected true")
					}

					if ok, _ := compareAttributeValues(v, c.value); !ok {
						t.Errorf("Got %v, expected %v", v, c.value)
					}
				})
			}

			// make sure all the expected names are there
			names := pb.Names()
			for _, cs := range cases {
				found := false
				for _, n := range names {
					if cs.name == n {
						found = true
						break
					}
				}

				if !found {
					t.Errorf("Could not find attribute name %s", cs.name)
				}
			}

			// try out round-tripping
			mb := GetMutableBag(pb)
			for _, n := range names {
				v, _ := pb.Get(n)
				mb.Set(n, v)
			}

			var a2 mixerpb.CompressedAttributes
			mb.ToProto(&a2, globalDict, len(globalDict))

			pb.Done()
		}
	}
}

func TestMessageDict(t *testing.T) {
	globalWordList := []string{"G0", "G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8", "G9"}

	globalDict := make(map[string]int32)
	for k, v := range globalWordList {
		globalDict[v] = int32(k)
	}

	b := GetMutableBag(nil)
	b.Set("M1", int64(1))
	b.Set("M2", int64(2))
	b.Set("M3", "M2")

	var attrs mixerpb.CompressedAttributes
	b.ToProto(&attrs, globalDict, len(globalDict))
	b2, _ := GetBagFromProto(&attrs, globalWordList)

	if !compareBags(b, b2) {
		t.Errorf("Got non-matching bags")
	}
}

func TestUpdateFromProto(t *testing.T) {
	globalDict := []string{"G0", "G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8", "G9"}
	messageDict := []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9", "M10"}

	revGlobalDict := make(map[string]int32)
	for k, v := range globalDict {
		revGlobalDict[v] = int32(k)
	}

	sm := mixerpb.StringMap{Entries: map[int32]int32{-6: -7}}

	attrs := mixerpb.CompressedAttributes{
		Words:      messageDict,
		Strings:    map[int32]int32{4: 5},
		Int64S:     map[int32]int64{6: 42},
		Doubles:    map[int32]float64{7: 42.0},
		Bools:      map[int32]bool{-1: true},
		Timestamps: map[int32]time.Time{-2: t9},
		Durations:  map[int32]time.Duration{-3: d1},
		Bytes:      map[int32][]uint8{-4: {11}},
		StringMaps: map[int32]mixerpb.StringMap{-5: sm},
	}

	b := GetMutableBag(nil)

	if err := b.UpdateBagFromProto(&attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	if err := b.UpdateBagFromProto(&attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	sm = mixerpb.StringMap{Entries: map[int32]int32{-7: -6}}
	attrs = mixerpb.CompressedAttributes{
		Words:      messageDict,
		Int64S:     map[int32]int64{6: 142},
		Doubles:    map[int32]float64{7: 142.0},
		StringMaps: map[int32]mixerpb.StringMap{-5: sm},
	}

	if err := b.UpdateBagFromProto(&attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	attrs = mixerpb.CompressedAttributes{
		Words:      messageDict,
		StringMaps: map[int32]mixerpb.StringMap{-1: sm},
	}

	if err := b.UpdateBagFromProto(&attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	refBag := GetMutableBag(nil)
	refBag.Set("M1", map[string]string{"M7": "M6"})
	refBag.Set("M2", t9)
	refBag.Set("M3", d1)
	refBag.Set("M4", []byte{11})
	refBag.Set("M5", map[string]string{"M7": "M6"})
	refBag.Set("G4", "G5")
	refBag.Set("G6", int64(142))
	refBag.Set("G7", 142.0)

	if !compareBags(b, refBag) {
		t.Error("Bags don't match")
	}
}

func TestCopyBag(t *testing.T) {
	refBag := GetMutableBag(nil)
	refBag.Set("M1", map[string]string{"M7": "M6"})
	refBag.Set("M2", t9)
	refBag.Set("M3", d1)
	refBag.Set("M4", []byte{11})
	refBag.Set("M5", map[string]string{"M7": "M6"})
	refBag.Set("G4", "G5")
	refBag.Set("G6", int64(142))
	refBag.Set("G7", 142.0)

	copy := CopyBag(refBag)

	if !compareBags(copy, refBag) {
		t.Error("Bags don't match")
	}
}

func TestProtoBag_Errors(t *testing.T) {
	globalWordList := []string{"G0", "G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8", "G9"}
	messageWordList := []string{"M0", "M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9"}

	attrs := mixerpb.CompressedAttributes{
		Words:   messageWordList,
		Strings: map[int32]int32{-24: 25},
	}

	pb, err := GetBagFromProto(&attrs, globalWordList)
	if err == nil {
		t.Error("GetBagFromProto succeeded, expected failure")
	}

	if pb != nil {
		t.Error("GetBagFromProto returned valid bag, expected nil")
	}
}

func TestUseAfterFree(t *testing.T) {
	b := GetMutableBag(nil)
	b.Done()

	if err := withPanic(func() { _, _ = b.Get("XYZ") }); err == nil {
		t.Error("Expected panic")
	}

	if err := withPanic(func() { _ = b.Names() }); err == nil {
		t.Error("Expected panic")
	}

	if err := withPanic(func() { b.Done() }); err == nil {
		t.Error("Expected panic")
	}
}

func withPanic(f func()) (ret interface{}) {
	defer func() {
		ret = recover()
	}()

	f()
	return ret
}

func compareBags(b1 Bag, b2 Bag) bool {
	b1Names := b1.Names()
	b2Names := b2.Names()

	if len(b1Names) != len(b2Names) {
		return false
	}

	for _, name := range b1Names {
		v1, _ := b1.Get(name)
		v2, _ := b2.Get(name)

		match, _ := compareAttributeValues(v1, v2)
		if !match {
			return false
		}
	}

	return true
}

func compareAttributeValues(v1, v2 interface{}) (bool, error) {
	var result bool
	switch t1 := v1.(type) {
	case string:
		t2, ok := v2.(string)
		result = ok && t1 == t2
	case int64:
		t2, ok := v2.(int64)
		result = ok && t1 == t2
	case float64:
		t2, ok := v2.(float64)
		result = ok && t1 == t2
	case bool:
		t2, ok := v2.(bool)
		result = ok && t1 == t2
	case time.Time:
		t2, ok := v2.(time.Time)
		result = ok && t1 == t2
	case time.Duration:
		t2, ok := v2.(time.Duration)
		result = ok && t1 == t2

	case []byte:
		t2, ok := v2.([]byte)
		if result = ok && len(t1) == len(t2); result {
			for i := 0; i < len(t1); i++ {
				if t1[i] != t2[i] {
					result = false
					break
				}
			}
		}

	case map[string]string:
		t2, ok := v2.(map[string]string)
		if result = ok && len(t1) == len(t2); result {
			for k, v := range t1 {
				if v != t2[k] {
					result = false
					break
				}
			}
		}

	default:
		return false, fmt.Errorf("unsupported attribute value type: %T", v1)
	}

	return result, nil
}

func TestBogusProto(t *testing.T) {
	globalWordList := []string{"G0", "G1"}
	globalDict := map[string]int32{globalWordList[0]: 0, globalWordList[1]: 1}
	messageWordList := []string{"N1", "N2"}

	sm1 := mixerpb.StringMap{Entries: map[int32]int32{-42: 0}}
	sm2 := mixerpb.StringMap{Entries: map[int32]int32{0: -42}}

	attrs := mixerpb.CompressedAttributes{
		Words:      messageWordList,
		Strings:    map[int32]int32{42: 1, 1: 42},
		StringMaps: map[int32]mixerpb.StringMap{-1: sm1, -2: sm2},
	}

	b := NewProtoBag(&attrs, globalDict, globalWordList)

	cases := []struct {
		name string
	}{
		{"Foo"},
		{"G0"},
		{"G1"},
		{"N1"},
		{"N2"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			v, ok := b.Get(c.name)
			if v != nil {
				t.Errorf("Expecting nil value, got #%v", v)
			}

			if ok {
				t.Error("Expecting false, got true")
			}
		})
	}
}

func TestMessageDictEdge(t *testing.T) {
	globalWordList := []string{"G0", "G1"}
	globalDict := map[string]int32{globalWordList[0]: 0, globalWordList[1]: 1}
	messageWordList := []string{"N1", "N2"}

	attrs := mixerpb.CompressedAttributes{
		Words:   messageWordList,
		Strings: map[int32]int32{0: -2},
	}

	b := NewProtoBag(&attrs, globalDict, globalWordList)
	v, ok := b.Get("G0")
	if !ok {
		t.Error("Expecting true, got false")
	}

	s, ok := v.(string)
	if !ok {
		t.Errorf("Expecting to get a string, got %v", v)
	}

	if s != "N2" {
		t.Errorf("Expecting to get N2, got %s", s)
	}
}

func TestDoubleStrings(t *testing.T) {
	// ensure that attribute ingestion handles having a word defined
	// in both the global and per-message word lists, preferring the
	// one defined in the per-message list.

	globalWordList := []string{"HELLO"}
	globalDict := map[string]int32{globalWordList[0]: 0}
	messageWordList := []string{"HELLO", "GOOD", "BAD"}

	attrs := mixerpb.CompressedAttributes{
		Words:   messageWordList,
		Strings: map[int32]int32{-1: -2},
	}

	for i := 0; i < 2; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var b Bag

			if i == 0 {
				b = NewProtoBag(&attrs, globalDict, globalWordList)
			} else {
				var err error
				b, err = GetBagFromProto(&attrs, globalWordList)
				if err != nil {
					t.Errorf("Got %v, expecting success", err)
					return
				}
			}

			if v, ok := b.Get("HELLO"); !ok {
				t.Error("Got false, expecting true")
			} else if s, ok := v.(string); !ok {
				t.Errorf("Got %v, expecting a string", v)
			} else if s != "GOOD" {
				t.Errorf("Got %s, expecting GOOD", s)
			}
		})
	}
}

func TestReferenceTracking(t *testing.T) {
	globalWordList := []string{"G0", "G1", "G2"}
	globalDict := map[string]int32{globalWordList[0]: 0, globalWordList[1]: 1, globalWordList[2]: 2}
	messageWordList := []string{"N1", "N2", "N3"}

	attrs := mixerpb.CompressedAttributes{
		Words: messageWordList,
		Strings: map[int32]int32{
			0:  0, // "G0":"G0"
			1:  0, // "G1":"G0"
			-1: 0, // "N1":"G0"
			-2: 0, // "N2":"G0"
		},
	}

	cases := []struct {
		name string
		cond mixerpb.ReferencedAttributes_Condition
	}{
		{"G0", mixerpb.EXACT},
		{"N1", mixerpb.EXACT},
		{"XX", mixerpb.ABSENCE},
		{"DUD", -1}, // not referenced, so shouldn't be a match
	}

	b := NewProtoBag(&attrs, globalDict, globalWordList)

	// reference some attributes
	_, _ = b.Get("G0") // from global word list
	_, _ = b.Get("N1") // from message word list
	_, _ = b.Get("XX") // not present

	ra := b.GetReferencedAttributes(globalDict, len(globalDict))
	if len(ra.AttributeMatches) != 3 {
		t.Errorf("Got %d matches, expected 3", len(ra.AttributeMatches))
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found := false
			for _, am := range ra.AttributeMatches {
				var name string
				index := am.Name
				if index >= 0 {
					name = globalWordList[index]
				} else {
					name = ra.Words[indexToSlot(index)]
				}

				if name == c.name {
					if c.cond != am.Condition {
						t.Errorf("Got condition %d, expected %d", am.Condition, c.cond)
					}
					found = true
					break
				}
			}

			if c.cond == -1 {
				if found {
					t.Errorf("Found match for %s, expecting not to", c.name)
				}
			} else if !found {
				t.Errorf("Did not find match for %s, expecting to find one", c.name)
			}
		})
	}

	b.ClearReferencedAttributes()

	ra = b.GetReferencedAttributes(globalDict, len(globalDict))
	if len(ra.AttributeMatches) != 0 {
		t.Errorf("Expecting no attributes matches, got %d", len(ra.AttributeMatches))
	}
}

func TestGlobalWordCount(t *testing.T) {
	// ensure that a component with a larger global word list can
	// produce an attribute message with a shorter word list to handle
	// back compat correctly

	longGlobalWordList := []string{"G0", "G1", "G2"}
	longGlobalDict := map[string]int32{longGlobalWordList[0]: 0, longGlobalWordList[1]: 1, longGlobalWordList[2]: 2}
	shortGlobalWordList := []string{"G0", "G1"}
	shortGlobalDict := map[string]int32{shortGlobalWordList[0]: 0, shortGlobalWordList[1]: 1}

	b := GetMutableBag(nil)
	for i := 0; i < len(longGlobalWordList); i++ {
		b.Set(longGlobalWordList[i], int64(i))
	}
	var output mixerpb.CompressedAttributes
	b.ToProto(&output, longGlobalDict, len(shortGlobalWordList)) // use the long word list, but short list's count

	for i := 0; i < 2; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// try decoding with a short word list
			var b2 Bag
			var err error

			if i == 0 {
				b2 = NewProtoBag(&output, shortGlobalDict, shortGlobalWordList)
			} else {
				b2, err = GetBagFromProto(&output, shortGlobalWordList)
			}

			if err != nil {
				t.Fatalf("Got '%v', expecting success", err)
			}

			for i := 0; i < len(longGlobalWordList); i++ {
				if v, ok := b2.Get(longGlobalWordList[i]); !ok {
					t.Errorf("Got nothing, expected expected attribute %s", longGlobalWordList[i])
				} else if int64(i) != v.(int64) {
					t.Errorf("Got %d, expected %d", v.(int), i)
				}
			}
		})
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
