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

func TestAttributeManager(t *testing.T) {
	type getStringCase struct {
		name    string
		result  string
		present bool
	}

	type getInt64Case struct {
		name    string
		result  int64
		present bool
	}

	type getFloat64Case struct {
		name    string
		result  float64
		present bool
	}

	type getBoolCase struct {
		name    string
		result  bool
		present bool
	}

	type getTimeCase struct {
		name    string
		result  time.Time
		present bool
	}

	type getDurationCase struct {
		name    string
		result  time.Duration
		present bool
	}

	type getBytesCase struct {
		name    string
		result  []uint8
		present bool
	}

	type getStringMapCase struct {
		name    string
		result  map[string]string
		present bool
	}

	sm := mixerpb.StringMap{Map: map[int32]string{9: "Nine"}}
	m := map[string]string{"name9": "Nine"}

	cases := []struct {
		attrs        mixerpb.Attributes
		result       bool
		getString    []getStringCase
		getInt64     []getInt64Case
		getFloat64   []getFloat64Case
		getBool      []getBoolCase
		getTime      []getTimeCase
		getDuration  []getDurationCase
		getBytes     []getBytesCase
		getStringMap []getStringMapCase
	}{
		// 0: make sure reset works against a fresh state
		{
			attrs: mixerpb.Attributes{
				ResetContext: true,
			},
			result: true,
		},

		// 1: basic case to try out adding one of everything
		{
			attrs: mixerpb.Attributes{
				Dictionary:          dictionary{1: "name1", 2: "name2", 3: "name3", 4: "name4", 5: "name5", 6: "name6", 7: "name7", 8: "name8", 9: "name9"},
				StringAttributes:    map[int32]string{1: "1"},
				Int64Attributes:     map[int32]int64{2: 2},
				DoubleAttributes:    map[int32]float64{3: 3.0},
				BoolAttributes:      map[int32]bool{4: true},
				TimestampAttributes: map[int32]time.Time{5: time.Date(1970, time.January, 1, 0, 0, 5, 5, time.UTC)},
				DurationAttributes:  map[int32]time.Duration{7: time.Duration(42) * time.Second},
				BytesAttributes:     map[int32][]uint8{6: {6}},
				StringMapAttributes: map[int32]mixerpb.StringMap{8: sm},
				ResetContext:        false,
				AttributeContext:    0,
				DeletedAttributes:   nil,
			},
			result: true,
			getString: []getStringCase{
				{"name1", "1", true},
				{"name2", "", false},
				{"name42", "", false},
			},

			getInt64: []getInt64Case{
				{"name2", 2, true},
				{"name1", 0, false},
				{"name42", 0, false},
			},

			getFloat64: []getFloat64Case{
				{"name3", 3.0, true},
				{"name1", 0.0, false},
				{"name42", 0.0, false},
			},

			getBool: []getBoolCase{
				{"name4", true, true},
				{"name1", false, false},
				{"name42", false, false},
			},

			getTime: []getTimeCase{
				{"name5", time.Date(1970, time.January, 1, 0, 0, 5, 5, time.UTC), true},
				{"name1", time.Time{}, false},
				{"name42", time.Time{}, false},
			},

			getDuration: []getDurationCase{
				{"name7", time.Second * 42, true},
				{"name1", time.Duration(0), false},
				{"name42", time.Duration(0), false},
			},

			getBytes: []getBytesCase{
				{"name6", []byte{6}, true},
				{"name1", nil, false},
				{"name42", nil, false},
			},

			getStringMap: []getStringMapCase{
				{"name8", m, true},
				{"name1", nil, false},
				{"name42", nil, false},
			},
		},

		// 2: now switch dictionaries and make sure we can still find things
		{
			attrs: mixerpb.Attributes{
				Dictionary: dictionary{11: "name1", 22: "name2", 33: "name3", 44: "name4", 55: "name5", 66: "name6", 77: "name7", 88: "name8", 99: "name9"},
			},
			result: true,
			getString: []getStringCase{
				{"name1", "1", true},
				{"name2", "", false},
				{"name42", "", false},
			},

			getInt64: []getInt64Case{
				{"name2", 2, true},
				{"name1", 0, false},
				{"name42", 0, false},
			},

			getFloat64: []getFloat64Case{
				{"name3", 3.0, true},
				{"name1", 0.0, false},
				{"name42", 0.0, false},
			},

			getBool: []getBoolCase{
				{"name4", true, true},
				{"name1", false, false},
				{"name42", false, false},
			},

			getTime: []getTimeCase{
				{"name5", time.Date(1970, time.January, 1, 0, 0, 5, 5, time.UTC), true},
				{"name1", time.Time{}, false},
				{"name42", time.Time{}, false},
			},

			getDuration: []getDurationCase{
				{"name7", time.Second * 42, true},
				{"name1", time.Duration(0), false},
				{"name42", time.Duration(0), false},
			},

			getBytes: []getBytesCase{
				{"name6", []byte{6}, true},
				{"name1", nil, false},
				{"name42", nil, false},
			},

			getStringMap: []getStringMapCase{
				{"name8", m, true},
				{"name1", nil, false},
				{"name42", nil, false},
			},
		},

		// 3: now delete everything and make sure it's all gone
		{
			attrs: mixerpb.Attributes{
				DeletedAttributes: []int32{11, 22, 33, 44, 55, 66, 77, 88},
			},
			result:       true,
			getString:    []getStringCase{{"name1", "", false}},
			getInt64:     []getInt64Case{{"name2", 0, false}},
			getFloat64:   []getFloat64Case{{"name3", 0.0, false}},
			getBool:      []getBoolCase{{"name4", false, false}},
			getTime:      []getTimeCase{{"name5", time.Time{}, false}},
			getDuration:  []getDurationCase{{"name7", time.Duration(0), false}},
			getBytes:     []getBytesCase{{"name6", []byte{}, false}},
			getStringMap: []getStringMapCase{{"name8", map[string]string{}, false}},
		},

		// 4: add stuff back in
		{
			attrs: mixerpb.Attributes{
				Dictionary:          dictionary{1: "name1", 2: "name2", 3: "name3", 4: "name4", 5: "name5", 6: "name6", 7: "name7", 8: "name8", 9: "name9"},
				StringAttributes:    map[int32]string{1: "1"},
				Int64Attributes:     map[int32]int64{2: 2},
				DoubleAttributes:    map[int32]float64{3: 3.0},
				BoolAttributes:      map[int32]bool{4: true},
				TimestampAttributes: map[int32]time.Time{5: time.Date(0, 0, 0, 0, 0, 5, 5, time.UTC)},
				DurationAttributes:  map[int32]time.Duration{7: time.Duration(42) * time.Second},
				BytesAttributes:     map[int32][]uint8{6: {6}},
				StringMapAttributes: map[int32]mixerpb.StringMap{8: sm},
				ResetContext:        false,
				AttributeContext:    0,
				DeletedAttributes:   nil,
			},
			result: true,
		},

		// 5: make sure reset works
		{
			attrs: mixerpb.Attributes{
				ResetContext: true,
			},
			result:       true,
			getString:    []getStringCase{{"name1", "", false}},
			getInt64:     []getInt64Case{{"name2", 0, false}},
			getFloat64:   []getFloat64Case{{"name3", 0.0, false}},
			getBool:      []getBoolCase{{"name4", false, false}},
			getTime:      []getTimeCase{{"name5", time.Time{}, false}},
			getDuration:  []getDurationCase{{"name7", time.Duration(0), false}},
			getBytes:     []getBytesCase{{"name6", []byte{}, false}},
			getStringMap: []getStringMapCase{{"name8", map[string]string{}, false}},
		},

		// 6: make sure reset works against a reset state
		{
			attrs: mixerpb.Attributes{
				ResetContext: true,
			},
			result: true,
		},

		// 7: try out bad dictionary index for strings
		{
			attrs:  mixerpb.Attributes{StringAttributes: map[int32]string{42: "1"}},
			result: false,
		},

		// 8: try out bad dictionary index for int64
		{
			attrs:  mixerpb.Attributes{Int64Attributes: map[int32]int64{42: 0}},
			result: false,
		},

		// 9: try out bad dictionary index for float64
		{
			attrs:  mixerpb.Attributes{DoubleAttributes: map[int32]float64{42: 0.0}},
			result: false,
		},

		// 10: try out bad dictionary index for bool
		{
			attrs:  mixerpb.Attributes{BoolAttributes: map[int32]bool{42: false}},
			result: false,
		},

		// 11: try out bad dictionary index for timestamp
		{
			attrs:  mixerpb.Attributes{TimestampAttributes: map[int32]time.Time{42: {}}},
			result: false,
		},

		// 12: try out bad dictionary index for duration
		{
			attrs:  mixerpb.Attributes{DurationAttributes: map[int32]time.Duration{42: time.Duration(0)}},
			result: false,
		},

		// 13: try out bad dictionary index for bytes
		{
			attrs:  mixerpb.Attributes{BytesAttributes: map[int32][]uint8{42: {}}},
			result: false,
		},

		// 14: try out bad dictionary index for string map
		{
			attrs:  mixerpb.Attributes{StringMapAttributes: map[int32]mixerpb.StringMap{42: {}}},
			result: false,
		},

		// 15: try to delete attributes that don't exist
		{
			attrs:  mixerpb.Attributes{DeletedAttributes: []int32{111, 222, 333}},
			result: true,
		},
	}

	am := NewManager()
	at := am.NewTracker()
	defer at.Done()

	for i, c := range cases {
		ab, err := at.ApplyAttributes(&c.attrs)
		if (err == nil) != c.result {
			if c.result {
				t.Errorf("Expected StartRequest to succeed but it returned %v for test case %d", err, i)
			} else {
				t.Errorf("Expected StartRequest to fail but it succeeded for test case %d", i)
			}
		}

		for j, g := range c.getString {
			result, present := ab.String(g.name)
			if result != g.result {
				t.Errorf("Expecting result='%v', got result='%v' for string test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for string test case %v:%v", g.present, present, i, j)
			}
		}

		for j, g := range c.getInt64 {
			result, present := ab.Int64(g.name)
			if result != g.result {
				t.Errorf("Expecting result='%v', got result='%v' for int64 test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for int64 test case %v:%v", g.present, present, i, j)
			}
		}

		for j, g := range c.getFloat64 {
			result, present := ab.Float64(g.name)
			if result != g.result {
				t.Errorf("Expecting result='%v', got result='%v' for float64 test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for float64 test case %v:%v", g.present, present, i, j)
			}
		}

		for j, g := range c.getBool {
			result, present := ab.Bool(g.name)
			if result != g.result {
				t.Errorf("Expecting result='%v', got result='%v' for bool test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for bool test case %v:%v", g.present, present, i, j)
			}
		}

		for j, g := range c.getTime {
			result, present := ab.Time(g.name)
			if result != g.result {
				t.Errorf("Expecting result='%v', got result='%v' for time test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for time test case %v:%v", g.present, present, i, j)
			}
		}

		for j, g := range c.getDuration {
			result, present := ab.Duration(g.name)
			if result != g.result {
				t.Errorf("Expecting result='%v', got result='%v' for duration test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for duration test case %v:%v", g.present, present, i, j)
			}
		}

		for j, g := range c.getBytes {
			result, present := ab.Bytes(g.name)

			same := len(result) == len(g.result)
			if same {
				for i := range result {
					if result[i] != g.result[i] {
						same = false
						break
					}
				}
			}

			if !same {
				t.Errorf("Expecting result='%v', got result='%v' for bytes test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for bytes test case %v:%v", g.present, present, i, j)
			}
		}

		for j, g := range c.getStringMap {
			result, present := ab.StringMap(g.name)

			same := len(result) == len(g.result)
			if same {
				for i := range result {
					if result[i] != g.result[i] {
						same = false
						break
					}
				}
			}

			if !same {
				t.Errorf("Expecting result='%v', got result='%v' for string map test case %v:%v", g.result, result, i, j)
			}

			if present != g.present {
				t.Errorf("Expecting present=%v, got present=%v for string map test case %v:%v", g.present, present, i, j)
			}
		}
	}
}
