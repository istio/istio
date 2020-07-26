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

package attribute

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	mixerpb "istio.io/api/mixer/v1"
	attr "istio.io/pkg/attribute"
	"istio.io/pkg/log"
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
	m1 := attr.WrapStringMap(map[string]string{"N16": "N16"})
	m3 := attr.WrapStringMap(map[string]string{"N42": "FourtyTwo"})

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

			if !attr.Equal(v, r.value) {
				t.Errorf("Got %v, expected %v for %s", v, r.value, r.name)
			}
		})
	}

	if _, found := ab.Get("XYZ"); found {
		t.Error("XYZ was found")
	}

	// try another level of overrides just to make sure that path is OK
	child := attr.GetMutableBag(ab)
	child.Set("N2", "31415692")
	r, found := ab.Get("N2")
	if !found || r.(string) != "42" {
		t.Error("N2 has wrong value")
	}

	_ = child.String()
	child.Done()
}

func TestEmptyRoundTrip(t *testing.T) {
	attrs0 := mixerpb.CompressedAttributes{}
	attrs1 := mixerpb.CompressedAttributes{}
	mb := attr.GetMutableBag(nil)
	ToProto(mb, &attrs1, nil, 0)

	if !reflect.DeepEqual(attrs0, attrs1) {
		t.Errorf("Expecting equal attributes, got a delta: original #%v, new #%v", attrs0, attrs1)
	}
}

func mutableBagFromProtoForTesing() *attr.MutableBag {
	b := GetProtoBag(protoAttrsForTesting())
	return attr.GetMutableBag(b)
}

func protoAttrsForTesting() (*mixerpb.CompressedAttributes, map[string]int32, []string) {
	globalWordList := []string{"G0", "G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8", "G9", "BADKEY"}
	messageWordList := []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9", "M10"}

	globalDict := make(map[string]int32)
	for k, v := range globalWordList {
		globalDict[v] = int32(k)
	}

	sm := mixerpb.StringMap{Entries: map[int32]int32{-6: -7}}

	attrs := mixerpb.CompressedAttributes{
		Words:      messageWordList,
		Strings:    map[int32]int32{4: 5, 3: 2, 2: 6, 5: 4},
		Int64S:     map[int32]int64{6: 42},
		Doubles:    map[int32]float64{7: 42.0},
		Bools:      map[int32]bool{-1: true},
		Timestamps: map[int32]time.Time{-2: t9},
		Durations:  map[int32]time.Duration{-3: d1},
		Bytes:      map[int32][]uint8{-4: {11}},
		StringMaps: map[int32]mixerpb.StringMap{-5: sm},
	}

	return &attrs, globalDict, globalWordList
}

func TestProtoBag(t *testing.T) {

	attrs, globalDict, globalWordList := protoAttrsForTesting()

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
		{"M5", attr.WrapStringMap(map[string]string{"M6": "M7"})},
	}

	for j := 0; j < 2; j++ {
		for i := 0; i < 2; i++ {
			var pb *attr.MutableBag
			var err error

			if j == 0 {
				pb, err = GetBagFromProto(attrs, globalWordList)
				if err != nil {
					t.Fatalf("GetBagFromProto failed with %v", err)
				}
			} else {
				b := GetProtoBag(attrs, globalDict, globalWordList)
				pb = attr.GetMutableBag(b)
			}

			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					v, ok := pb.Get(c.name)
					if !ok {
						t.Error("Got false, expected true")
					}

					if !attr.Equal(v, c.value) {
						t.Errorf("Got %v, expected %v", v, c.value)
					}
				})
			}

			// make sure all the expected names are there
			names := pb.Names()
			for _, cs := range cases {
				found := false
				for _, n := range names {
					if !pb.Contains(n) {
						t.Errorf("expected bag to contain %s", n)
					}
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
			mb := attr.GetMutableBag(pb)
			for _, n := range names {
				v, _ := pb.Get(n)
				mb.Set(n, v)
			}

			var a2 mixerpb.CompressedAttributes
			ToProto(mb, &a2, globalDict, len(globalDict))

			_ = pb.String()
			pb.Done()
		}
	}
}

func TestProtoBag_Contains(t *testing.T) {
	mb := mutableBagFromProtoForTesing()

	if mb.Contains("THIS_KEY_IS_NOT_IN_DICT") {
		t.Errorf("Found unexpected key")
	}

	if mb.Contains("BADKEY") {
		t.Errorf("Found unexpected key")
	}

}

func TestMessageDict(t *testing.T) {
	globalWordList := []string{"G0", "G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8", "G9"}

	globalDict := make(map[string]int32)
	for k, v := range globalWordList {
		globalDict[v] = int32(k)
	}

	b := attr.GetMutableBag(nil)
	b.Set("M1", int64(1))
	b.Set("M2", int64(2))
	b.Set("M3", "M2")

	var attrs mixerpb.CompressedAttributes
	ToProto(b, &attrs, globalDict, len(globalDict))
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

	b := attr.GetMutableBag(nil)

	if err := UpdateBagFromProto(b, &attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	if err := UpdateBagFromProto(b, &attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	sm = mixerpb.StringMap{Entries: map[int32]int32{-7: -6}}
	attrs = mixerpb.CompressedAttributes{
		Words:      messageDict,
		Int64S:     map[int32]int64{6: 142},
		Doubles:    map[int32]float64{7: 142.0},
		StringMaps: map[int32]mixerpb.StringMap{-5: sm},
	}

	if err := UpdateBagFromProto(b, &attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	attrs = mixerpb.CompressedAttributes{
		Words:      messageDict,
		StringMaps: map[int32]mixerpb.StringMap{-1: sm},
	}

	if err := UpdateBagFromProto(b, &attrs, globalDict); err != nil {
		t.Errorf("Got %v, expected success", err)
	}

	refBag := attr.GetMutableBag(nil)
	refBag.Set("M1", attr.WrapStringMap(map[string]string{"M7": "M6"}))
	refBag.Set("M2", t9)
	refBag.Set("M3", d1)
	refBag.Set("M4", []byte{11})
	refBag.Set("M5", attr.WrapStringMap(map[string]string{"M7": "M6"}))
	refBag.Set("G4", "G5")
	refBag.Set("G6", int64(142))
	refBag.Set("G7", 142.0)

	if !compareBags(b, refBag) {
		t.Error("Bags don't match")
	}
}

func TestUpdateFromProtoWithDupes(t *testing.T) {
	globalDict := []string{"G0", "G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8", "G9"}
	messageDict := []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9", "M10"}

	b := attr.GetMutableBag(nil)

	cases := []struct {
		attrs mixerpb.CompressedAttributes
	}{
		{mixerpb.CompressedAttributes{
			Words:   messageDict,
			Strings: map[int32]int32{4: 5},
			Int64S:  map[int32]int64{4: 5},
		}},

		{mixerpb.CompressedAttributes{
			Words:   messageDict,
			Strings: map[int32]int32{4: 5},
			Doubles: map[int32]float64{4: 5.0},
		}},

		{mixerpb.CompressedAttributes{
			Words:   messageDict,
			Strings: map[int32]int32{4: 5},
			Bools:   map[int32]bool{4: false},
		}},

		{mixerpb.CompressedAttributes{
			Words:     messageDict,
			Strings:   map[int32]int32{4: 5},
			Durations: map[int32]time.Duration{4: time.Second},
		}},

		{mixerpb.CompressedAttributes{
			Words:      messageDict,
			Strings:    map[int32]int32{4: 5},
			Timestamps: map[int32]time.Time{4: {}},
		}},

		{mixerpb.CompressedAttributes{
			Words:   messageDict,
			Strings: map[int32]int32{4: 5},
			Bytes:   map[int32][]byte{4: nil},
		}},

		{mixerpb.CompressedAttributes{
			Words:      messageDict,
			Strings:    map[int32]int32{4: 5},
			StringMaps: map[int32]mixerpb.StringMap{4: {}},
		}},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {

			if err := UpdateBagFromProto(b, &c.attrs, globalDict); err == nil {
				t.Errorf("Got success, expected error")
			}
		})
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

func compareBags(b1 attr.Bag, b2 attr.Bag) bool {
	b1Names := b1.Names()
	b2Names := b2.Names()

	if len(b1Names) != len(b2Names) {
		return false
	}

	for _, name := range b1Names {
		v1, _ := b1.Get(name)
		v2, _ := b2.Get(name)

		if !attr.Equal(v1, v2) {
			return false
		}
	}

	return true
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

	b := GetProtoBag(&attrs, globalDict, globalWordList)

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

	b.Done()
}

func TestMessageDictEdge(t *testing.T) {
	globalWordList := []string{"G0", "G1"}
	globalDict := map[string]int32{globalWordList[0]: 0, globalWordList[1]: 1}
	messageWordList := []string{"N1", "N2"}

	attrs := mixerpb.CompressedAttributes{
		Words:   messageWordList,
		Strings: map[int32]int32{0: -2},
	}

	b := GetProtoBag(&attrs, globalDict, globalWordList)
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

	b.Done()
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
			var b attr.Bag

			if i == 0 {
				b = GetProtoBag(&attrs, globalDict, globalWordList)
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

			b.Done()
		})
	}
}

func TestReferenceTracking(t *testing.T) {
	globalWordList := []string{"G0", "G1", "G2", "G3"}
	globalDict := map[string]int32{globalWordList[0]: 0, globalWordList[1]: 1, globalWordList[2]: 2, globalWordList[3]: 3}
	messageWordList := []string{"N1", "N2", "N3"}

	attrs := mixerpb.CompressedAttributes{
		Words: messageWordList,
		Strings: map[int32]int32{
			0:  0, // "G0":"G0"
			1:  0, // "G1":"G0"
			-1: 0, // "N1":"G0"
			-2: 0, // "N2":"G0"
		},
		StringMaps: map[int32]mixerpb.StringMap{
			2: {Entries: map[int32]int32{3: 0}},
		},
	}

	cases := []struct {
		name   string
		cond   mixerpb.ReferencedAttributes_Condition
		mapKey string
	}{
		{"G0", mixerpb.EXACT, ""},
		{"N1", mixerpb.EXACT, ""},
		{"XX", mixerpb.ABSENCE, ""},
		{"G2", mixerpb.EXACT, "G3"},
		{"G2", mixerpb.ABSENCE, "YY"},
		{"DUD", -1, ""}, // not referenced, so shouldn't be a match
	}

	b := GetProtoBag(&attrs, globalDict, globalWordList)

	// reference some attributes
	_, _ = b.Get("G0")  // from global word list
	_, _ = b.Get("N1")  // from message word list
	_, _ = b.Get("XX")  // not present
	x, _ := b.Get("G2") // the string map

	sm := x.(attr.StringMap)
	_, _ = sm.Get("G3") // present
	_, _ = sm.Get("YY") // not present

	ra := b.GetReferencedAttributes(globalDict, len(globalDict))
	if len(ra.AttributeMatches) != 5 {
		t.Errorf("Got %d matches, expected 5", len(ra.AttributeMatches))
	}

	for _, c := range cases {
		t.Run(c.name+"-"+c.mapKey, func(t *testing.T) {
			found := false
			for _, am := range ra.AttributeMatches {
				var name string
				index := am.Name
				if index >= 0 {
					name = globalWordList[index]
				} else {
					name = ra.Words[indexToSlot(index)]
				}

				var mapKey string
				index = am.MapKey
				if index >= 0 {
					if index != 0 || c.mapKey != "" {
						mapKey = globalWordList[index]
					}
				} else {
					mapKey = ra.Words[indexToSlot(index)]
				}

				if name == c.name && mapKey == c.mapKey {
					if c.cond != am.Condition {
						t.Errorf("Got condition %d for mapkey %s, expected %d", am.Condition, mapKey, c.cond)
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

	snap := b.Snapshot()

	b.Clear()

	ra = b.GetReferencedAttributes(globalDict, len(globalDict))
	if len(ra.AttributeMatches) != 0 {
		t.Errorf("Expecting no attributes matches, got %d", len(ra.AttributeMatches))
	}

	b.Restore(snap)

	ra = b.GetReferencedAttributes(globalDict, len(globalDict))
	if len(ra.AttributeMatches) != 5 {
		t.Errorf("Expecting 5 attributes matches, got %d", len(ra.AttributeMatches))
	}

	b.Done()
}

func TestGlobalWordCount(t *testing.T) {
	// ensure that a component with a larger global word list can
	// produce an attribute message with a shorter word list to handle
	// back compat correctly

	longGlobalWordList := []string{"G0", "G1", "G2"}
	longGlobalDict := map[string]int32{longGlobalWordList[0]: 0, longGlobalWordList[1]: 1, longGlobalWordList[2]: 2}
	shortGlobalWordList := []string{"G0", "G1"}
	shortGlobalDict := map[string]int32{shortGlobalWordList[0]: 0, shortGlobalWordList[1]: 1}

	b := attr.GetMutableBag(nil)
	for i := 0; i < len(longGlobalWordList); i++ {
		b.Set(longGlobalWordList[i], int64(i))
	}
	var output mixerpb.CompressedAttributes
	ToProto(b, &output, longGlobalDict, len(shortGlobalWordList)) // use the long word list, but short list's count

	for i := 0; i < 2; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// try decoding with a short word list
			var b2 attr.Bag
			var err error

			if i == 0 {
				b2 = GetProtoBag(&output, shortGlobalDict, shortGlobalWordList)
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

func TestToProtoForTesting(t *testing.T) {
	m := map[string]interface{}{
		"A": 1.0,
		"B": 2.0,
		"C": int64(3),
	}

	ca := GetProtoForTesting(m)
	b, err := GetBagFromProto(ca, nil)
	if err != nil {
		t.Errorf("Expecting success, got %v", err)
	}

	if v, found := b.Get("A"); !found {
		t.Errorf("Didn't find A")
	} else if v.(float64) != 1.0 {
		t.Errorf("Got %v, expecting 1.0", v)
	}

	if v, found := b.Get("B"); !found {
		t.Errorf("Didn't find B")
	} else if v.(float64) != 2.0 {
		t.Errorf("Got %v, expecting 2.0", v)
	}

	if v, found := b.Get("C"); !found {
		t.Errorf("Didn't find C")
	} else if v.(int64) != 3 {
		t.Errorf("Got %v, expecting 3", v)
	}
}

func TestGlobalList(t *testing.T) {
	l := GlobalList()

	// check that there's a known string in there...
	for _, s := range l {
		if s == "destination.service" {
			return
		}
	}

	t.Error("Did not find destination.service")
}

func TestParentRoundTrip(t *testing.T) {
	parent := attr.GetMutableBag(nil)
	child := attr.GetMutableBag(parent)

	parent.Set("parent", true)
	child.Set("child", true)

	var pb mixerpb.CompressedAttributes
	ToProto(child, &pb, nil, 0)

	mb, err := GetBagFromProto(&pb, nil)
	if err != nil {
		t.Errorf("failed to get bag from protobuf: %v", err)
	}
	names := mb.Names()

	if len(names) != len(child.Names()) {
		t.Errorf("missing attributes after round-trip. Got %v, expected %v", names, child.Names())
	}

	for _, k := range names {
		pre, _ := child.Get(k)
		post, _ := mb.Get(k)
		if !reflect.DeepEqual(post, pre) {
			t.Errorf("Got %v, expected %v for attribute %s", post, pre, k)
		}
	}
}

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func getLargeBag() *attr.MutableBag {
	m := map[string]interface{}{}

	for _, l := range alphabet {
		m[string(l)] = string(l) + "-val"
	}

	return attr.GetMutableBagForTesting(m)
}

// Original        100000	     20527 ns/op	    9624 B/op	      12 allocs/op
// with Contains() 1000000	      1997 ns/op	       0 B/op	       0 allocs/op
func Benchmark_MutableBagMerge(b *testing.B) {

	mergeTo := mutableBagFromProtoForTesing()
	mergeFrom := getLargeBag()

	// bag.merge will only update an element if one is not present.
	// bag.Merge(bag) should be a no

	b.Run("LargeMerge", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			mergeTo.Merge(mergeFrom)
		}
	})

}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	o := log.DefaultOptions()
	o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
	o.SetOutputLevel(scope.Name(), log.DebugLevel)
	_ = log.Configure(o)
}
