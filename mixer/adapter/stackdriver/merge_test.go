// Copyright 2017 Istio Authors.
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

package stackdriver

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	"google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/mixer/pkg/adapter/test"
)

var (
	m1 = &metric.Metric{
		Type:   "Star Trek",
		Labels: map[string]string{"series": "Star Trek", "captain": "James T. Kirk"},
	}
	mr1 = &monitoredres.MonitoredResource{
		Type:   "Star Trek",
		Labels: map[string]string{"ship": "USS Enterprise (NX-01)"},
	}
	m2 = &metric.Metric{
		Type:   "Star Trek",
		Labels: map[string]string{"series": "Star Trek: The Next Generation", "captain": "Jean-Luc Picard"},
	}
	mr2 = &monitoredres.MonitoredResource{
		Type:   "Star Trek",
		Labels: map[string]string{"ship": "USS Enterprise (NCC-1701-D)"},
	}
)

// shorthand to save us some chars in test cases
type ts []*monitoring.TimeSeries

func makeTS(m *metric.Metric, mr *monitoredres.MonitoredResource, seconds int64, micros int32) *monitoring.TimeSeries {
	return makeTSFull(m, mr, seconds, micros, 0, metric.MetricDescriptor_DELTA)
}

func makeTSDelta(m *metric.Metric, mr *monitoredres.MonitoredResource, seconds int64, micros int32, val int64) *monitoring.TimeSeries {
	return makeTSFull(m, mr, seconds, micros, val, metric.MetricDescriptor_DELTA)
}

func makeTSCumulative(m *metric.Metric, mr *monitoredres.MonitoredResource, seconds int64, micros int32, val int64) *monitoring.TimeSeries {
	return makeTSFull(m, mr, seconds, micros, val, metric.MetricDescriptor_CUMULATIVE)
}

func makeTSFull(m *metric.Metric, mr *monitoredres.MonitoredResource, seconds int64, micros int32, value int64,
	kind metric.MetricDescriptor_MetricKind) *monitoring.TimeSeries {
	return &monitoring.TimeSeries{
		Metric:     m,
		Resource:   mr,
		MetricKind: kind,
		Points: []*monitoring.Point{{
			Value: &monitoring.TypedValue{&monitoring.TypedValue_Int64Value{value}},
			Interval: &monitoring.TimeInterval{
				StartTime: &timestamp.Timestamp{Seconds: seconds, Nanos: micros * usec},
				EndTime:   &timestamp.Timestamp{Seconds: seconds, Nanos: (micros * usec) + usec},
			},
		}},
	}
}

func TestToUSec(t *testing.T) {
	now := time.Now()
	pbnow, _ := ptypes.TimestampProto(now)
	if int32(now.Nanosecond()/int(time.Microsecond)) != toUSec(pbnow.Nanos) {
		t.Fatalf("toUSec(%d) = %d, expected it to be equal to %v / time.Microsecond", pbnow.Nanos, toUSec(pbnow.Nanos), now.Nanosecond())
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name  string
		in    ts
		start *timestamp.Timestamp
		end   *timestamp.Timestamp
		val   *monitoring.TypedValue
	}{
		{"one",
			ts{makeTSDelta(m1, mr1, 1, 5, 456)},
			&timestamp.Timestamp{Seconds: 1, Nanos: 5000},
			&timestamp.Timestamp{Seconds: 1, Nanos: 6000},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{456}}},
		{"dupe",
			ts{makeTSDelta(m1, mr1, 1, 5, 1), makeTSDelta(m1, mr1, 1, 5, 1)},
			&timestamp.Timestamp{Seconds: 1, Nanos: 5000},
			&timestamp.Timestamp{Seconds: 1, Nanos: 6000},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{2}}},
		{"out of order",
			ts{makeTSDelta(m1, mr1, 2, 5, 1), makeTSDelta(m1, mr1, 1, 5, 1)},
			&timestamp.Timestamp{Seconds: 1, Nanos: 5000},
			&timestamp.Timestamp{Seconds: 2, Nanos: 6000},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{2}}},
		{"reversed",
			ts{makeTSDelta(m1, mr1, 4, 1, 1), makeTSDelta(m1, mr1, 3, 1, 1), makeTSDelta(m1, mr1, 2, 1, 1), makeTSDelta(m1, mr1, 1, 1, 1)},
			&timestamp.Timestamp{Seconds: 1, Nanos: 1000},
			&timestamp.Timestamp{Seconds: 4, Nanos: 2000},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{4}}},
		{"out of order, overlapping times",
			ts{makeTSDelta(m1, mr1, 1, 1, 1), makeTSDelta(m1, mr1, 1, 1, 2), makeTSDelta(m1, mr1, 1, 3, 3), makeTSDelta(m1, mr1, 1, 2, 4)},
			&timestamp.Timestamp{Seconds: 1, Nanos: 1000},
			&timestamp.Timestamp{Seconds: 1, Nanos: 4000},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{10}}},
		{"larger time spans",
			ts{makeTSDelta(m1, mr1, 1, 1, 7), makeTSDelta(m1, mr1, 1, 1, 3), makeTSDelta(m1, mr1, 1, 7, 4896), makeTSDelta(m1, mr1, 1, 5, 9485367)},
			&timestamp.Timestamp{Seconds: 1, Nanos: 1000},
			&timestamp.Timestamp{Seconds: 1, Nanos: 8000},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{9490273}}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			out := merge(tt.in, test.NewEnv(t))
			if len(out) != 1 {
				t.Fatalf("Wanted 1 output value, got: %v", out)
			} else if len(out[0].Points) != 1 {
				t.Fatalf("There should only ever be a single point, got: %v", out[0].Points)
			}

			if !reflect.DeepEqual(out[0].Points[0].Interval.StartTime, tt.start) {
				t.Errorf("Got start time %v wanted %v; out: %v", out[0].Points[0].Interval.StartTime, tt.start, out[0].Points[0])
			}
			if !reflect.DeepEqual(out[0].Points[0].Interval.EndTime, tt.end) {
				t.Errorf("Got end time %v wanted %v; out: %v", out[0].Points[0].Interval.EndTime, tt.end, out[0].Points[0])
			}
			if !reflect.DeepEqual(out[0].Points[0].Value, tt.val) {
				t.Errorf("Got value %v wanted %v; out: %v", out[0].Points[0].Value, tt.val, out[0].Points[0])
			}
		})
	}
}

func TestMerge_Errors(t *testing.T) {
	env := test.NewEnv(t)
	a := makeTSDelta(m1, mr1, 1, 1, 1)
	b := makeTSDelta(m1, mr1, 1, 1, 1)
	b.Points[0].Value = &monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{4.7}}
	_ = merge([]*monitoring.TimeSeries{a, b}, env)
	if len(env.GetLogs()) < 1 {
		t.Fatalf("Expected bad data to be logged about, got no log entries")
	} else if !strings.Contains(env.GetLogs()[0], "failed to merge timeseries") {
		t.Fatalf("Expected log entry for failed merge, got entry: %v", env.GetLogs()[0])
	}

	c := makeTSDelta(m1, mr1, 1, 1, 1)
	c.Points[0].Interval.EndTime = c.Points[0].Interval.StartTime
	out := merge([]*monitoring.TimeSeries{c}, env)
	if reflect.DeepEqual(out[0].Points[0].Interval.EndTime, out[0].Points[0].Interval.StartTime) {
		t.Fatalf("After coalescing, DELTA metrics must not have the same start and end time, but we do: %v", out[0])
	}
}

func TestToKey(t *testing.T) {
	hashes := [4]uint64{
		toKey(m1, mr1),
		toKey(m2, mr1),
		toKey(m1, mr2),
		toKey(m2, mr2),
	}
	for idx, hash := range hashes {
		for i := idx + 1; i < len(hashes); i++ {
			if hashes[i] == hash {
				t.Errorf("hash[%d] = hash[%d] = %v; none should be equivalent", i, idx, hash)
			}
		}
	}

	// Verify that our hashes don't depend on address by creating duplicates and verifying they collide:
	k1 := toKey(
		&metric.Metric{Type: "m1", Labels: map[string]string{"foo": "bar"}},
		&monitoredres.MonitoredResource{Type: "m1", Labels: map[string]string{"foo": "bar"}})
	k2 := toKey(
		&metric.Metric{Type: "m1", Labels: map[string]string{"foo": "bar"}},
		&monitoredres.MonitoredResource{Type: "m1", Labels: map[string]string{"foo": "bar"}})
	if k1 != k2 {
		t.Fatalf("Expected toKey on duplicates to have the same hash, got %d and %d instead", k1, k2)
	}
}

func TestGroupBySeries(t *testing.T) {
	m1mr1 := toKey(m1, mr1)
	m1mr2 := toKey(m1, mr2)
	m2mr1 := toKey(m2, mr1)
	m2mr2 := toKey(m2, mr2)

	tests := []struct {
		name string
		in   ts
		out  map[uint64]ts
	}{
		{"empty",
			ts{},
			map[uint64]ts{}},
		{"singleton",
			ts{makeTSCumulative(m1, mr1, 1, 1, 0)},
			map[uint64]ts{m1mr1: {makeTSCumulative(m1, mr1, 1, 1, 0)}}},
		{"multiple points",
			ts{makeTS(m1, mr1, 1, 1), makeTS(m1, mr1, 2, 2)},
			map[uint64]ts{m1mr1: {makeTSCumulative(m1, mr1, 1, 1, 0), makeTSCumulative(m1, mr1, 2, 2, 0)}}},
		{"two metrics",
			ts{makeTS(m1, mr1, 1, 1), makeTS(m2, mr1, 1, 1)},
			map[uint64]ts{
				m1mr1: {makeTSCumulative(m1, mr1, 1, 1, 0)},
				m2mr1: {makeTSCumulative(m2, mr1, 1, 1, 0)}}},
		{"two MRs",
			ts{makeTS(m1, mr2, 1, 1), makeTS(m1, mr1, 1, 1)},
			map[uint64]ts{
				m1mr2: {makeTSCumulative(m1, mr2, 1, 1, 0)},
				m1mr1: {makeTSCumulative(m1, mr1, 1, 1, 0)}}},
		{"disjoint",
			ts{makeTS(m1, mr1, 1, 1), makeTS(m2, mr2, 1, 1)},
			map[uint64]ts{
				m1mr1: {makeTSCumulative(m1, mr1, 1, 1, 0)},
				m2mr2: {makeTSCumulative(m2, mr2, 1, 1, 0)}}},
		{"disjoint, multiple points",
			ts{makeTS(m1, mr1, 1, 1), makeTS(m2, mr2, 1, 1), makeTS(m1, mr1, 2, 2), makeTS(m2, mr2, 2, 2)},
			map[uint64]ts{
				m1mr1: {makeTSCumulative(m1, mr1, 1, 1, 0), makeTSCumulative(m1, mr1, 2, 2, 0)},
				m2mr2: {makeTSCumulative(m2, mr2, 1, 1, 0), makeTSCumulative(m2, mr2, 2, 2, 0)}}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			out := groupBySeries(tt.in)
			if len(out) != len(tt.out) {
				t.Fatalf("Expected %d groups, got %d: %v", len(tt.out), len(out), out)
			}
			for key, vals := range tt.out {
				if len(out[key]) != len(vals) {
					t.Fatalf("Expected key %d in map to have %d entries, got: %v", key, len(vals), out)
				}
				for i := 0; i < len(vals); i++ {
					if !reflect.DeepEqual(vals[i], out[key][i]) {
						t.Errorf("Expected entries in group %d to match %v, got: %v", key, vals, out[key])
					}
				}
			}
		})
	}
}

func TestMergeSeries(t *testing.T) {
	merged := makeTSCumulative(m1, mr1, 1, 1, 15)
	merged.Points[0].Interval = &monitoring.TimeInterval{
		StartTime: merged.Points[0].Interval.StartTime,
		EndTime:   &timestamp.Timestamp{Seconds: 2, Nanos: 3 * usec},
	}

	tests := []struct {
		name string
		in   map[uint64][]*monitoring.TimeSeries
		out  []*monitoring.TimeSeries
	}{
		{"empty",
			map[uint64][]*monitoring.TimeSeries{},
			[]*monitoring.TimeSeries{}},
		{"singleton",
			map[uint64][]*monitoring.TimeSeries{0: {makeTSCumulative(m1, mr1, 1, 1, 0)}},
			[]*monitoring.TimeSeries{makeTSCumulative(m1, mr1, 1, 1, 0)}},
		{"multiple points",
			map[uint64][]*monitoring.TimeSeries{65425478798: {makeTSCumulative(m1, mr1, 1, 1, 7), makeTSCumulative(m1, mr1, 2, 2, 8)}},
			[]*monitoring.TimeSeries{merged}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			out := mergeSeries(tt.in, test.NewEnv(t))
			if len(out) != len(tt.out) {
				t.Fatalf("Expected %d groups, got %d: %v", len(tt.out), len(out), out)
			}
			for i := 0; i < len(tt.out); i++ {
				if !reflect.DeepEqual(out[i], tt.out[i]) {
					t.Errorf("Expected out[%d] = %v, got %v; %v", i, tt.out[i], out[i], out)
				}
			}
		})
	}
}

func TestMergePoints(t *testing.T) {
	tests := []struct {
		name string
		a    *monitoring.TypedValue
		b    *monitoring.TypedValue
		out  *monitoring.TypedValue
		err  string
	}{
		{"happy i64",
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{47}},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{33}},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{80}},
			""},
		{"happy double",
			&monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{2.4}},
			&monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{8.6}},
			&monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{11}},
			""},
		{"happy distribution",
			&monitoring.TypedValue{&monitoring.TypedValue_DistributionValue{}},
			&monitoring.TypedValue{&monitoring.TypedValue_DistributionValue{}},
			&monitoring.TypedValue{&monitoring.TypedValue_DistributionValue{}},
			"not implemented"},
		{"sad i64",
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{47}},
			&monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{8.6}},
			nil,
			"can't merge two timeseries with different value types"},
		{"sad double",
			&monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{8.6}},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{47}},
			nil,
			"can't merge two timeseries with different value types"},
		{"sad distribution",
			&monitoring.TypedValue{&monitoring.TypedValue_DistributionValue{}},
			&monitoring.TypedValue{&monitoring.TypedValue_Int64Value{47}},
			nil,
			"can't merge two timeseries with different value types"},
		{"invalid",
			&monitoring.TypedValue{&monitoring.TypedValue_StringValue{}},
			&monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{8.6}},
			nil,
			"invalid type for DELTA metric"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			a := makeTS(m1, mr1, 1, 1)
			a.Points[0].Value = tt.a
			b := makeTS(m1, mr1, 1, 1)
			b.Points[0].Value = tt.b

			out, err := mergePoints(a, b)
			if err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("merge(%v, %v) = '%s', wanted no err", a, b, err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			} else if !reflect.DeepEqual(out.Points[0].Value, tt.out) {
				t.Fatalf("merge(%v, %v) = %v, wanted value %v", a, b, out, tt.out)
			}
		})
	}
}
