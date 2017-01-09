// Copyright 2016 Google Inc.
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
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	ts "github.com/golang/protobuf/ptypes/timestamp"

	mixerpb "istio.io/api/mixer/api/v1"
)

func TestBag(t *testing.T) {
	t9 := time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	t10 := time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)
	t42 := time.Date(2001, 1, 1, 1, 1, 1, 42, time.UTC)
	ts9, _ := ptypes.TimestampProto(t9)
	ts10, _ := ptypes.TimestampProto(t10)

	attrs := mixerpb.Attributes{
		Dictionary:          dictionary{1: "N1", 2: "N2", 3: "N3", 4: "N4", 5: "N5", 6: "N6", 7: "N7", 8: "N8", 9: "N9", 10: "N10", 11: "N11", 12: "N12"},
		StringAttributes:    map[int32]string{1: "1", 2: "2"},
		Int64Attributes:     map[int32]int64{3: 3, 4: 4},
		DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
		BoolAttributes:      map[int32]bool{7: true, 8: false},
		TimestampAttributes: map[int32]*ts.Timestamp{9: ts9, 10: ts10},
		BytesAttributes:     map[int32][]uint8{11: []byte{11}, 12: []byte{12}},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	ab, err := at.StartRequest(&attrs)
	if err != nil {
		t.Errorf("Unable to update attrs: %v", err)
	}
	defer at.EndRequest()

	// override a bunch of values
	ab.SetString("N2", "42")
	ab.SetInt64("N4", 42)
	ab.SetFloat64("N6", 42.0)
	ab.SetBool("N8", true)
	ab.SetTime("N10", t42)
	ab.SetBytes("N12", []byte{42})

	// make sure the overrides worked and didn't disturb non-overridden values

	// strings
	{
		var r string
		var found bool

		if r, found = ab.String("N1"); !found {
			t.Error("N1 not found")
		}
		if r != "1" {
			t.Error("N1 has wrong value")
		}

		if r, found = ab.String("N2"); !found {
			t.Error("N2 not found")
		}
		if r != "42" {
			t.Error("N2 has wrong value")
		}

		if _, found = ab.String("XYZ"); found {
			t.Error("XYZ was found")
		}
	}

	// int64
	{
		var r int64
		var found bool

		if r, found = ab.Int64("N3"); !found {
			t.Error("N3 not found")
		}
		if r != 3 {
			t.Error("N3 has wrong value")
		}

		if r, found = ab.Int64("N4"); !found {
			t.Error("N4 not found")
		}
		if r != 42 {
			t.Error("N4 has wrong value")
		}

		if _, found = ab.Int64("XYZ"); found {
			t.Error("XYZ was found")
		}
	}

	// float64
	{
		var r float64
		var found bool

		if r, found = ab.Float64("N5"); !found {
			t.Error("N5 not found")
		}
		if r != 5.0 {
			t.Error("N5 has wrong value")
		}

		if r, found = ab.Float64("N6"); !found {
			t.Error("N6 not found")
		}
		if r != 42 {
			t.Error("N6 has wrong value")
		}

		if _, found = ab.Float64("XYZ"); found {
			t.Error("XYZ was found")
		}
	}

	// bool
	{
		var r bool
		var found bool

		if r, found = ab.Bool("N7"); !found {
			t.Error("N7 not found")
		}
		if !r {
			t.Error("N7 has wrong value")
		}

		if r, found = ab.Bool("N8"); !found {
			t.Error("N8 not found")
		}
		if !r {
			t.Error("N8 has wrong value")
		}

		if _, found = ab.Bool("XYZ"); found {
			t.Error("XYZ was found")
		}
	}

	// Time
	{
		var r time.Time
		var found bool

		if r, found = ab.Time("N9"); !found {
			t.Error("N9 not found")
		}
		if r != t9 {
			t.Error("N9 has wrong value")
		}

		if r, found = ab.Time("N10"); !found {
			t.Error("N10 not found")
		}
		if r != t42 {
			t.Error("N10 has wrong value")
		}

		if _, found = ab.Time("XYZ"); found {
			t.Error("XYZ was found")
		}
	}

	// []uint8
	{
		var r []uint8
		var found bool

		if r, found = ab.Bytes("N11"); !found {
			t.Error("N11 not found")
		}
		if r[0] != 11 {
			t.Error("N11 has wrong value")
		}

		if r, found = ab.Bytes("N12"); !found {
			t.Error("N12 not found")
		}
		if r[0] != 42 {
			t.Error("N12 has wrong value")
		}

		if _, found = ab.Bytes("XYZ"); found {
			t.Error("XYZ was found")
		}
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
	ts1 := &ts.Timestamp{Seconds: -1, Nanos: -1}

	attrs := mixerpb.Attributes{
		Dictionary:          dictionary{1: "N1"},
		TimestampAttributes: map[int32]*ts.Timestamp{1: ts1},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	_, err := at.StartRequest(&attrs)
	if err == nil {
		t.Error("Successfully updated attributes, expected an error")
	}
	defer at.EndRequest()
}
