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

	ptypes "github.com/gogo/protobuf/types"

	mixerpb "istio.io/api/mixer/v1"
)

func BenchmarkTracker(b *testing.B) {
	t9 := time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	t10 := time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)
	ts9, _ := ptypes.TimestampProto(t9)
	ts10, _ := ptypes.TimestampProto(t10)

	d := time.Duration(42) * time.Second
	ds := ptypes.DurationProto(d)

	sm := &mixerpb.StringMap{Map: map[int32]string{14: "14"}}

	attrs := []mixerpb.Attributes{
		{
			Dictionary: dictionary{1: "N1", 2: "N2", 3: "N3", 4: "N4", 5: "N5", 6: "N6", 7: "N7", 8: "N8",
				9: "N9", 10: "N10", 11: "N11", 12: "N12", 13: "N13", 14: "N14"},
			StringAttributes:    map[int32]string{1: "1", 2: "2"},
			Int64Attributes:     map[int32]int64{3: 3, 4: 4},
			DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
			BoolAttributes:      map[int32]bool{7: true, 8: false},
			TimestampAttributes: map[int32]*ptypes.Timestamp{9: ts9, 10: ts10},
			DurationAttributes:  map[int32]*ptypes.Duration{11: ds},
			BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
			StringMapAttributes: map[int32]*mixerpb.StringMap{14: sm},
		},

		{},

		{
			StringAttributes:    map[int32]string{1: "1", 2: "2"},
			Int64Attributes:     map[int32]int64{3: 3, 4: 4},
			DoubleAttributes:    map[int32]float64{5: 5.0, 6: 6.0},
			BoolAttributes:      map[int32]bool{7: true, 8: false},
			TimestampAttributes: map[int32]*ptypes.Timestamp{9: ts9, 10: ts10},
			DurationAttributes:  map[int32]*ptypes.Duration{11: ds},
			BytesAttributes:     map[int32][]uint8{12: {12}, 13: {13}},
			StringMapAttributes: map[int32]*mixerpb.StringMap{14: sm},
		},
	}

	am := NewManager()

	// Note that we don't call the Tracker.Done method such that we
	// get fresh instances every time through instead of one from the
	// recycling pool
	for i := 0; i < b.N; i++ {
		t := am.NewTracker()

		for _, a := range attrs {
			b, _ := t.StartRequest(&a)

			_, _ = b.String("a")
			_, _ = b.Int64("a")
			_, _ = b.Float64("a")
			_, _ = b.Bool("a")
			_, _ = b.Time("a")
			_, _ = b.Duration("a")
			_, _ = b.Bytes("a")
			_, _ = b.StringMap("a")

			t.EndRequest()
		}
	}
}
