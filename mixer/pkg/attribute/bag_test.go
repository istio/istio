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
	"reflect"
	"strconv"
	"testing"
	"time"

	ptypes "github.com/gogo/protobuf/types"

	mixerpb "istio.io/api/mixer/v1"
)

var (
	t9      = time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	t10     = time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)
	t42     = time.Date(2001, 1, 1, 1, 1, 1, 42, time.UTC)
	ts9, _  = ptypes.TimestampProto(t9)
	ts10, _ = ptypes.TimestampProto(t10)

	d1  = time.Duration(42) * time.Second
	d2  = time.Duration(34) * time.Second
	d3  = time.Duration(56) * time.Second
	td1 = ptypes.DurationProto(d1)
)

func TestBag(t *testing.T) {
	sm1 := &mixerpb.StringMap{Map: map[int32]string{16: "Sixteen"}}
	sm2 := &mixerpb.StringMap{Map: map[int32]string{17: "Seventeen"}}
	m1 := map[string]string{"N16": "Sixteen"}
	m3 := map[string]string{"N42": "FourtyTwo"}

	attrs := mixerpb.Attributes{
		Dictionary: dictionary{1: "N1", 2: "N2", 3: "N3", 4: "N4", 5: "N5", 6: "N6", 7: "N7", 8: "N8",
			9: "N9", 10: "N10", 11: "N11", 12: "N12", 13: "N13", 14: "N14", 15: "N15", 16: "N16", 17: "N17"},
		StringAttributes:    map[int32]string{1: "1", 2: "2"},
		Int64Attributes:     map[int32]int64{3: 3, 4: 4},
		DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
		BoolAttributes:      map[int32]bool{7: true, 8: false},
		TimestampAttributes: map[int32]*ptypes.Timestamp{9: ts9, 10: ts10},
		DurationAttributes:  map[int32]*ptypes.Duration{11: td1},
		BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
		StringMapAttributes: map[int32]*mixerpb.StringMap{14: sm1, 15: sm2},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	ab, err := at.StartRequest(&attrs)
	if err != nil {
		t.Errorf("Unable to start request: %v", err)
	}
	defer at.EndRequest()

	// override a bunch of values
	ab.SetString("N2", "42")
	ab.SetInt64("N4", 42)
	ab.SetFloat64("N6", 42.0)
	ab.SetBool("N8", true)
	ab.SetTime("N10", t42)
	ab.SetDuration("N11", d2)
	ab.SetBytes("N13", []byte{42})
	ab.SetStringMap("N15", m3)

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
			v, found := Value(ab, r.name)
			if !found {
				t.Error("Got false, expecting true")
			}

			if !reflect.DeepEqual(v, r.value) {
				t.Errorf("Got %v, expected %v", v, r.value)
			}
		})
	}

	if _, found := Value(ab, "XYZ"); found {
		t.Error("XYZ was found")
	}

	// try another level of overrides just to make sure that path is OK
	child := ab.Child()
	child.SetString("N2", "31415692")
	r, found := ab.String("N2")
	if !found || r != "42" {
		t.Error("N2 has wrong value")
	}
}

func TestContext(t *testing.T) {
	// simple bag
	b := getMutableBag(nil)
	b.SetInt64("42", 42)

	// make sure we can store and fetch the bag in a context
	ctx := NewContext(context.Background(), b)
	nb, found := FromContext(ctx)
	if !found {
		t.Error("Expecting to find bag, got nil")
	}

	r, found := nb.Int64("42")
	if !found || r != 42 {
		t.Error("Got different or altered bag return from FromContext")
	}

	// make sure FromContext handles cases where there is no bag attached
	nb, found = FromContext(context.Background())
	if found || nb != nil {
		t.Error("Expecting FromContext to fail cleanly")
	}
}

func TestBadTimestamp(t *testing.T) {
	// ensure we handle bogus on-the-wire timestamp values properly

	// a bogus timestamp value
	ts1 := &ptypes.Timestamp{Seconds: -1, Nanos: -1}

	attr := mixerpb.Attributes{
		Dictionary:          dictionary{1: "N1"},
		TimestampAttributes: map[int32]*ptypes.Timestamp{1: ts1},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	_, err := at.StartRequest(&attr)
	if err == nil {
		t.Error("Successfully updated attributes, expected an error")
	}
	defer at.EndRequest()
}

func TestBadDuration(t *testing.T) {
	// ensure we handle bogus on-the-wire duration values properly

	// a bogus duration value
	d1 := &ptypes.Duration{Seconds: 1, Nanos: -1}

	attr := mixerpb.Attributes{
		Dictionary:         dictionary{1: "N1"},
		DurationAttributes: map[int32]*ptypes.Duration{1: d1},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	_, err := at.StartRequest(&attr)
	if err == nil {
		t.Error("Successfully updated attributes, expected an error")
	}
	defer at.EndRequest()
}

func TestBadStringMapKey(t *testing.T) {
	// ensure we handle bogus on-the-wire string map key indices

	sm1 := &mixerpb.StringMap{Map: map[int32]string{16: "Sixteen"}}

	attr := mixerpb.Attributes{
		Dictionary:          dictionary{1: "N1"},
		StringMapAttributes: map[int32]*mixerpb.StringMap{1: sm1},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	_, err := at.StartRequest(&attr)
	if err == nil {
		t.Error("Successfully updated attributes, expected an error")
	}
	defer at.EndRequest()
}

type d map[string]interface{}
type ttable struct {
	inRoot  *mixerpb.Attributes
	inChild d
	out     d
}

func TestStringKeys(t *testing.T) {
	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:       map[int32]string{1: "root"},
					StringAttributes: map[int32]string{1: "r"},
				},
				d{},
				d{"root": "r"},
			},
			{
				&mixerpb.Attributes{},
				d{"one": "a", "two": "b"},
				d{"one": "a", "two": "b"},
			},
			{
				&mixerpb.Attributes{
					Dictionary:       map[int32]string{1: "root"},
					StringAttributes: map[int32]string{1: "r"},
				},
				d{"one": "a", "two": "b"},
				d{"root": "r", "one": "a", "two": "b"},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.(string)
			ab.SetString(k, vs)
		},
		func(b Bag) []string {
			return b.StringKeys()
		},
	)
}

func TestInt64Keys(t *testing.T) {
	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:      map[int32]string{1: "root"},
					Int64Attributes: map[int32]int64{1: 1},
				},
				d{},
				d{"root": int64(1)},
			},
			{
				&mixerpb.Attributes{},
				d{"one": int64(2), "two": int64(3)},
				d{"one": int64(2), "two": int64(3)},
			},
			{
				&mixerpb.Attributes{
					Dictionary:      map[int32]string{1: "root"},
					Int64Attributes: map[int32]int64{1: 1},
				},
				d{"one": int64(2), "two": int64(3)},
				d{"root": int64(1), "one": int64(2), "two": int64(3)},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.(int64)
			ab.SetInt64(k, vs)
		},
		func(b Bag) []string {
			return b.Int64Keys()
		},
	)
}

func TestFloat64Keys(t *testing.T) {
	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:       map[int32]string{1: "root"},
					DoubleAttributes: map[int32]float64{1: 1},
				},
				d{},
				d{"root": float64(1)},
			},
			{
				&mixerpb.Attributes{},
				d{"one": float64(2), "two": float64(3)},
				d{"one": float64(2), "two": float64(3)},
			},
			{
				&mixerpb.Attributes{
					Dictionary:       map[int32]string{1: "root"},
					DoubleAttributes: map[int32]float64{1: 1},
				},
				d{"one": float64(2), "two": float64(3)},
				d{"root": float64(1), "one": float64(2), "two": float64(3)},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.(float64)
			ab.SetFloat64(k, vs)
		},
		func(b Bag) []string {
			return b.Float64Keys()
		},
	)
}

func TestBoolKeys(t *testing.T) {
	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:     map[int32]string{1: "root"},
					BoolAttributes: map[int32]bool{1: true},
				},
				d{},
				d{"root": true},
			},
			{
				&mixerpb.Attributes{},
				d{"one": true, "two": false},
				d{"one": true, "two": false},
			},
			{
				&mixerpb.Attributes{
					Dictionary:     map[int32]string{1: "root"},
					BoolAttributes: map[int32]bool{1: false},
				},
				d{"one": true, "two": false},
				d{"root": false, "one": true, "two": false},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.(bool)
			ab.SetBool(k, vs)
		},
		func(b Bag) []string {
			return b.BoolKeys()
		},
	)
}

func TestTimeKeys(t *testing.T) {
	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:          map[int32]string{1: "root"},
					TimestampAttributes: map[int32]*ptypes.Timestamp{1: ts9},
				},
				d{},
				d{"root": t9},
			},
			{
				&mixerpb.Attributes{},
				d{"one": t10, "two": t42},
				d{"one": t10, "two": t42},
			},
			{
				&mixerpb.Attributes{
					Dictionary:          map[int32]string{1: "root"},
					TimestampAttributes: map[int32]*ptypes.Timestamp{1: ts9},
				},
				d{"one": t10, "two": t42},
				d{"root": t9, "one": t10, "two": t42},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.(time.Time)
			ab.SetTime(k, vs)
		},
		func(b Bag) []string {
			return b.TimeKeys()
		},
	)
}

func TestDurationKeys(t *testing.T) {
	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:         map[int32]string{1: "root"},
					DurationAttributes: map[int32]*ptypes.Duration{1: td1},
				},
				d{},
				d{"root": d1},
			},
			{
				&mixerpb.Attributes{},
				d{"one": d2, "two": d3},
				d{"one": d2, "two": d3},
			},
			{
				&mixerpb.Attributes{
					Dictionary:         map[int32]string{1: "root"},
					DurationAttributes: map[int32]*ptypes.Duration{1: td1},
				},
				d{"one": d2, "two": d3},
				d{"root": d1, "one": d2, "two": d3},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.(time.Duration)
			ab.SetDuration(k, vs)
		},
		func(b Bag) []string {
			return b.DurationKeys()
		},
	)
}

func TestByteKeys(t *testing.T) {
	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:      map[int32]string{1: "root"},
					BytesAttributes: map[int32][]byte{1: {11}},
				},
				d{},
				d{"root": []byte{11}},
			},
			{
				&mixerpb.Attributes{},
				d{"one": []byte{12}, "two": []byte{13}},
				d{"one": []byte{12}, "two": []byte{13}},
			},
			{
				&mixerpb.Attributes{
					Dictionary:      map[int32]string{1: "root"},
					BytesAttributes: map[int32][]byte{1: {1}},
				},
				d{"one": []byte{2}, "two": []byte{3}},
				d{"root": []byte{1}, "one": []byte{2}, "two": []byte{3}},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.([]byte)
			ab.SetBytes(k, vs)
		},
		func(b Bag) []string {
			return b.BytesKeys()
		},
	)
}

func TestStringMapKeys(t *testing.T) {
	sm1 := &mixerpb.StringMap{Map: map[int32]string{2: "One"}}
	m1 := map[string]string{"key1": "One"}
	m2 := map[string]string{"key2": "Two"}
	m3 := map[string]string{"key3": "Three"}

	testKeys(t,
		[]ttable{
			{
				&mixerpb.Attributes{},
				d{},
				d{},
			},
			{
				&mixerpb.Attributes{
					Dictionary:          map[int32]string{1: "root", 2: "key1"},
					StringMapAttributes: map[int32]*mixerpb.StringMap{1: sm1},
				},
				d{},
				d{"root": m1},
			},
			{
				&mixerpb.Attributes{},
				d{"one": m1, "two": m2},
				d{"one": m1, "two": m2},
			},
			{
				&mixerpb.Attributes{
					Dictionary:          map[int32]string{1: "root", 2: "key1"},
					StringMapAttributes: map[int32]*mixerpb.StringMap{1: sm1},
				},
				d{"one": m2, "two": m3},
				d{"root": m1, "one": m2, "two": m3},
			},
		},
		func(ab MutableBag, k string, v interface{}) {
			vs := v.(map[string]string)
			ab.SetStringMap(k, vs)
		},
		func(b Bag) []string {
			return b.StringMapKeys()
		},
	)
}

func testKeys(t *testing.T, cases []ttable, setVal func(MutableBag, string, interface{}), keyFn func(Bag) []string) {
	for i, tc := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			at := NewManager().NewTracker()
			root, _ := at.StartRequest(tc.inRoot)
			ab := root.Child()
			for k, v := range tc.inChild {
				setVal(ab, k, v)
			}

			keys := keyFn(ab)
			// verify everything that was returned was expected
			for _, key := range keys {
				if _, found := tc.out[key]; !found {
					t.Errorf("keyFn = [..., %s, ...], wanted (key, val) in set: %v", key, tc.out)
				}
			}
			// and that everything that was expected was returned
			for key := range tc.out {
				if !contains(keys, key) {
					t.Errorf("keyFn = %v, wanted '%s' in set", keys, key)
				}
			}

			at.Done()
			root.Done()
			ab.Done()
		})
	}
}

func contains(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
