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
	"context"
	"flag"
	"reflect"
	"strings"
	"testing"
	"time"

	mixerpb "istio.io/api/mixer/v1"
)

var (
	t9  = time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	t10 = time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)
	t42 = time.Date(2001, 1, 1, 1, 1, 1, 42, time.UTC)

	d1 = time.Duration(42) * time.Second
	d2 = time.Duration(34) * time.Second
)

func TestBag(t *testing.T) {
	sm1 := mixerpb.StringMap{Map: map[int32]string{16: "Sixteen"}}
	sm2 := mixerpb.StringMap{Map: map[int32]string{17: "Seventeen"}}
	m1 := map[string]string{"N16": "Sixteen"}
	m3 := map[string]string{"N42": "FourtyTwo"}

	attrs := mixerpb.Attributes{
		Dictionary: dictionary{1: "N1", 2: "N2", 3: "N3", 4: "N4", 5: "N5", 6: "N6", 7: "N7", 8: "N8",
			9: "N9", 10: "N10", 11: "N11", 12: "N12", 13: "N13", 14: "N14", 15: "N15", 16: "N16", 17: "N17"},
		StringAttributes:    map[int32]string{1: "1", 2: "2"},
		Int64Attributes:     map[int32]int64{3: 3, 4: 4},
		DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
		BoolAttributes:      map[int32]bool{7: true, 8: false},
		TimestampAttributes: map[int32]time.Time{9: t9, 10: t10},
		DurationAttributes:  map[int32]time.Duration{11: d1},
		BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
		StringMapAttributes: map[int32]mixerpb.StringMap{14: sm1, 15: sm2},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	ab, err := at.ApplyAttributes(&attrs)
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
		{"N1", "1"},
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
	child := ab.Child()
	child.Set("N2", "31415692")
	r, found := ab.Get("N2")
	if !found || r.(string) != "42" {
		t.Error("N2 has wrong value")
	}
}

func TestStringMapEdgeCase(t *testing.T) {
	// ensure coverage for some obscure logging paths

	d := dictionary{1: "N1", 2: "N2"}
	rb := getMutableBag(nil)
	attrs := &mixerpb.Attributes{}

	// empty to non-empty
	sm1 := mixerpb.StringMap{Map: map[int32]string{2: "Two"}}
	attrs.StringMapAttributes = map[int32]mixerpb.StringMap{1: sm1}
	_ = rb.update(d, attrs)

	// non-empty to non-empty
	sm1 = mixerpb.StringMap{Map: map[int32]string{}}
	attrs.StringMapAttributes = map[int32]mixerpb.StringMap{1: sm1, 2: sm1}
	_ = rb.update(d, attrs)

	// non-empty to empty
	attrs.DeletedAttributes = []int32{1}
	attrs.StringMapAttributes = map[int32]mixerpb.StringMap{}
	_ = rb.update(d, attrs)
}

func TestContext(t *testing.T) {
	// simple bag
	b := getMutableBag(nil)
	b.Set("42", int64(42))

	// make sure we can store and fetch the bag in a context
	ctx := NewContext(context.Background(), b)
	nb, found := FromContext(ctx)
	if !found {
		t.Error("Expecting to find bag, got nil")
	}

	r, found := nb.Get("42")
	if !found || r.(int64) != 42 {
		t.Error("Got different or altered bag return from FromContext")
	}

	// make sure FromContext handles cases where there is no bag attached
	nb, found = FromContext(context.Background())
	if found || nb != nil {
		t.Error("Expecting FromContext to fail cleanly")
	}
}

func TestBadStringMapKey(t *testing.T) {
	// ensure we handle bogus on-the-wire string map key indices

	sm1 := mixerpb.StringMap{Map: map[int32]string{16: "Sixteen"}}

	attr := mixerpb.Attributes{
		Dictionary:          dictionary{1: "N1"},
		StringMapAttributes: map[int32]mixerpb.StringMap{1: sm1},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	_, err := at.ApplyAttributes(&attr)
	if err == nil {
		t.Error("Successfully updated attributes, expected an error")
	}
}

func TestMerge(t *testing.T) {
	mb := getMutableBag(empty)

	c1 := mb.Child()
	c2 := mb.Child()

	c1.Set("STRING1", "A")
	c2.Set("STRING2", "B")

	if err := mb.Merge([]*MutableBag{c1, c2}); err != nil {
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
	mb := getMutableBag(empty)

	c1 := mb.Child()
	c2 := mb.Child()

	c1.Set("FOO", "X")
	c2.Set("FOO", "Y")

	if err := mb.Merge([]*MutableBag{c1, c2}); err == nil {
		t.Error("Got success, expected failure")
	} else if !strings.Contains(err.Error(), "FOO") {
		t.Errorf("Expected error to contain the word FOO, got %s", err.Error())
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
