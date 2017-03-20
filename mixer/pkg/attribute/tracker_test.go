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
			b, _ := t.ApplyRequestAttributes(&a)

			_, _ = b.Get("a")
		}
	}
}

func TestTracker_ApplyRequestAttributes(t *testing.T) {
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
	_, err := tracker.ApplyRequestAttributes(&attr1)
	if err != nil {
		t.Errorf("Expecting success, got %v", err)
	}

	oldDict := tracker.currentRequestDictionary
	oldBag := CopyBag(tracker.requestContexts[0])

	_, err = tracker.ApplyRequestAttributes(&attr2)
	if err == nil {
		t.Error("Expecting failure, got success")
	}

	// make sure nothing has changed due to the second failed attribute update
	if !compareDictionaries(oldDict, tracker.currentRequestDictionary) {
		t.Error("Expected dictionaries to be consistent, they're different")
	}

	if !compareBags(oldBag, tracker.requestContexts[0]) {
		t.Error("Expecting bags to be consistent, they're different")
	}

	cp := CopyBag(oldBag)
	if !compareBags(oldBag, cp) {
		t.Error("Expecting copied bag to match original")
	}
}

func compareBags(b1 Bag, b2 Bag) bool {
	if len(b1.Names()) != len(b2.Names()) {
		return false
	}

	for _, k := range b1.Names() {
		v1, ok1 := b1.Get(k)
		v2, ok2 := b2.Get(k)

		if ok1 != ok2 {
			return false
		}

		switch t1 := v1.(type) {
		case string:
			t2, ok := v2.(string)
			if !ok || t1 != t2 {
				return false
			}
		case int64:
			t2, ok := v2.(int64)
			if !ok || t1 != t2 {
				return false
			}
		case float64:
			t2, ok := v2.(float64)
			if !ok || t1 != t2 {
				return false
			}
		case bool:
			t2, ok := v2.(bool)
			if !ok || t1 != t2 {
				return false
			}
		case time.Time:
			t2, ok := v2.(time.Time)
			if !ok || t1 != t2 {
				return false
			}
		case time.Duration:
			t2, ok := v2.(time.Duration)
			if !ok || t1 != t2 {
				return false
			}
		case []byte:
			t2, ok := v2.([]byte)
			if !ok {
				return false
			}

			if len(t1) != len(t2) {
				return false
			}

			for i := 0; i < len(t1); i++ {
				if t1[i] != t2[i] {
					return false
				}
			}
		case map[string]string:
			t2, ok := v2.(map[string]string)
			if !ok {
				return false
			}

			if len(t1) != len(t2) {
				return false
			}

			for k, v := range t1 {
				if v != t2[k] {
					return false
				}
			}
		}
	}

	return true
}
