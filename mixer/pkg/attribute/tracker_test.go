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
	"testing"
	"time"

	mixerpb "istio.io/api/mixer/v1"
)

func BenchmarkTracker(b *testing.B) {
	t9 := time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	t10 := time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)

	d := time.Duration(42) * time.Second

	sm := mixerpb.StringMap{Map: map[int32]string{14: "14"}}

	attrs := []mixerpb.Attributes{
		{
			Dictionary: dictionary{1: "N1", 2: "N2", 3: "N3", 4: "N4", 5: "N5", 6: "N6", 7: "N7", 8: "N8",
				9: "N9", 10: "N10", 11: "N11", 12: "N12", 13: "N13", 14: "N14"},
			StringAttributes:    map[int32]string{1: "1", 2: "2"},
			Int64Attributes:     map[int32]int64{3: 3, 4: 4},
			DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
			BoolAttributes:      map[int32]bool{7: true, 8: false},
			TimestampAttributes: map[int32]time.Time{9: t9, 10: t10},
			DurationAttributes:  map[int32]time.Duration{11: d},
			BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
			StringMapAttributes: map[int32]mixerpb.StringMap{14: sm},
		},

		{},

		{
			StringAttributes:    map[int32]string{1: "1", 2: "2"},
			Int64Attributes:     map[int32]int64{3: 3, 4: 4},
			DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
			BoolAttributes:      map[int32]bool{7: true, 8: false},
			TimestampAttributes: map[int32]time.Time{9: t9, 10: t10},
			DurationAttributes:  map[int32]time.Duration{11: d},
			BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
			StringMapAttributes: map[int32]mixerpb.StringMap{14: sm},
		},
	}

	am := NewManager()

	// Note that we don't call the Tracker.Done method such that we
	// get fresh instances every time through instead of one from the
	// recycling pool
	for i := 0; i < b.N; i++ {
		t := am.NewTracker()

		for _, a := range attrs {
			b, _ := t.ApplyAttributes(&a)

			_, _ = b.String("a")
			_, _ = b.Int64("a")
			_, _ = b.Float64("a")
			_, _ = b.Bool("a")
			_, _ = b.Time("a")
			_, _ = b.Duration("a")
			_, _ = b.Bytes("a")
			_, _ = b.StringMap("a")
		}
	}
}

func TestTracker_ApplyAttributes(t *testing.T) {
	t9 := time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	t10 := time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)
	d := time.Duration(42) * time.Second
	sm := mixerpb.StringMap{Map: map[int32]string{14: "14"}}

	attr1 := mixerpb.Attributes{
		Dictionary: dictionary{1: "N1", 2: "N2", 3: "N3", 4: "N4", 5: "N5", 6: "N6", 7: "N7", 8: "N8",
			9: "N9", 10: "N10", 11: "N11", 12: "N12", 13: "N13", 14: "N14"},
		StringAttributes:    map[int32]string{1: "1", 2: "2"},
		Int64Attributes:     map[int32]int64{3: 3, 4: 4},
		DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
		BoolAttributes:      map[int32]bool{7: true, 8: false},
		TimestampAttributes: map[int32]time.Time{9: t9, 10: t10},
		DurationAttributes:  map[int32]time.Duration{11: d},
		BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
		StringMapAttributes: map[int32]mixerpb.StringMap{14: sm},
	}

	attr2 := mixerpb.Attributes{
		Dictionary: dictionary{1: "X1", 2: "X2", 3: "X3", 4: "X4", 5: "X5", 6: "X6", 7: "X7", 8: "X8",
			9: "X9", 10: "X10", 11: "X11", 12: "X12", 13: "X13", 14: "X14"},
		StringAttributes:    map[int32]string{1: "1", 2: "2"},
		Int64Attributes:     map[int32]int64{3: 3, 4: 4},
		DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
		BoolAttributes:      map[int32]bool{7: true, 8: false},
		TimestampAttributes: map[int32]time.Time{9: t9, 10: t10},
		DurationAttributes:  map[int32]time.Duration{11: d},
		BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
		StringMapAttributes: map[int32]mixerpb.StringMap{14: sm, 15: sm},
	}

	tracker := NewManager().NewTracker().(*tracker)
	_, err := tracker.ApplyAttributes(&attr1)
	if err != nil {
		t.Errorf("Expecting success, got %v", err)
	}

	oldDict := tracker.currentDictionary
	oldBag := copyBag(tracker.contexts[0])

	_, err = tracker.ApplyAttributes(&attr2)
	if err == nil {
		t.Error("Expecting failure, got success")
	}

	// make sure nothing has changed due to the second failed attribute update
	if !compareDictionaries(oldDict, tracker.currentDictionary) {
		t.Error("Expected dictionaries to be consistent, they're different")
	}

	if !compareBags(oldBag, tracker.contexts[0]) {
		t.Error("Expecting bags to be consistent, they're different")
	}

	copy := copyBag(oldBag)
	if !compareBags(oldBag, copy) {
		t.Error("Expecting copied bag to match original")
	}
}

func compareBags(b1 Bag, b2 Bag) bool {
	if len(b1.StringKeys()) != len(b2.StringKeys()) {
		return false
	}

	for _, k := range b1.StringKeys() {
		v1, _ := b1.String(k)
		v2, _ := b2.String(k)
		if v1 != v2 {
			return false
		}
	}

	if len(b1.Int64Keys()) != len(b2.Int64Keys()) {
		return false
	}

	for _, k := range b1.Int64Keys() {
		v1, _ := b1.Int64(k)
		v2, _ := b2.Int64(k)
		if v1 != v2 {
			return false
		}
	}

	if len(b1.Float64Keys()) != len(b2.Float64Keys()) {
		return false
	}

	for _, k := range b1.Float64Keys() {
		v1, _ := b1.Float64(k)
		v2, _ := b2.Float64(k)
		if v1 != v2 {
			return false
		}
	}

	if len(b1.BoolKeys()) != len(b2.BoolKeys()) {
		return false
	}

	for _, k := range b1.BoolKeys() {
		v1, _ := b1.Bool(k)
		v2, _ := b2.Bool(k)
		if v1 != v2 {
			return false
		}
	}

	if len(b1.TimeKeys()) != len(b2.TimeKeys()) {
		return false
	}

	for _, k := range b1.TimeKeys() {
		v1, _ := b1.Time(k)
		v2, _ := b2.Time(k)
		if v1 != v2 {
			return false
		}
	}

	if len(b1.DurationKeys()) != len(b2.DurationKeys()) {
		return false
	}

	for _, k := range b1.DurationKeys() {
		v1, _ := b1.Duration(k)
		v2, _ := b2.Duration(k)
		if v1 != v2 {
			return false
		}
	}

	if len(b1.BytesKeys()) != len(b2.BytesKeys()) {
		return false
	}

	for _, k := range b1.BytesKeys() {
		v1, _ := b1.Bytes(k)
		v2, _ := b2.Bytes(k)

		if len(v1) != len(v2) {
			return false
		}

		for i := 0; i < len(v1); i++ {
			if v1[i] != v2[i] {
				return false
			}
		}
	}

	if len(b1.StringMapKeys()) != len(b2.StringMapKeys()) {
		return false
	}

	for _, k := range b1.StringMapKeys() {
		v1, _ := b1.StringMap(k)
		v2, _ := b2.StringMap(k)

		if len(v1) != len(v2) {
			return false
		}

		for k, v := range v1 {
			if v != v2[k] {
				return false
			}
		}
	}

	return true
}
