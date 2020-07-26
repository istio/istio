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

package metric

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/genproto/googleapis/api/distribution"
	"google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"
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
	m3 = &metric.Metric{
		Type:   "Star Trek",
		Labels: map[string]string{"series": "Star Trek: Deep Space Nine", "captain": "Benjamin Sisko"},
	}
	mr3 = &monitoredres.MonitoredResource{
		Type:   "Star Trek",
		Labels: map[string]string{"ship": "USS Defiant (NX-74205)"},
	}
)

func makeTS(m *metric.Metric, mr *monitoredres.MonitoredResource, seconds int64, micros int32) *monitoring.TimeSeries {
	return makeTSFull(m, mr, seconds, micros, 0, metric.MetricDescriptor_DELTA)
}

func makeTSFull(m *metric.Metric, mr *monitoredres.MonitoredResource, seconds int64, micros int32, value int64,
	kind metric.MetricDescriptor_MetricKind) *monitoring.TimeSeries {
	return &monitoring.TimeSeries{
		Metric:     m,
		Resource:   mr,
		MetricKind: kind,
		Points: []*monitoring.Point{{
			Value: &monitoring.TypedValue{Value: &monitoring.TypedValue_Int64Value{Int64Value: value}},
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

func TestMergePoints(t *testing.T) {
	// under, 10-12, 12-14, 14-16, over
	linearOpts := linear(10, 2, 3)
	linear := distFactory(linearOpts)

	// under, 2-4, 4-8, over
	expOpts := exp(2, 2, 2)
	exp := distFactory(expOpts)

	// under, 10-20, 20-30, 30-40, over
	explicitOpts := explicit([]float64{10, 20, 30, 40})
	explicit := distFactory(explicitOpts)

	tests := []struct {
		name string
		a    *monitoring.TypedValue
		b    *monitoring.TypedValue
		out  *monitoring.TypedValue
		err  string
	}{
		{"happy i64",
			&monitoring.TypedValue{Value: &monitoring.TypedValue_Int64Value{Int64Value: 47}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_Int64Value{Int64Value: 33}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_Int64Value{Int64Value: 80}},
			""},
		{"happy double",
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DoubleValue{DoubleValue: 2.4}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DoubleValue{DoubleValue: 8.6}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DoubleValue{DoubleValue: 11}},
			""},
		{"sad i64",
			&monitoring.TypedValue{Value: &monitoring.TypedValue_Int64Value{Int64Value: 47}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DoubleValue{DoubleValue: 8.6}},
			nil,
			"can't merge two timeseries with different value types"},
		{"sad double",
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DoubleValue{DoubleValue: 8.6}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_Int64Value{Int64Value: 47}},
			nil,
			"can't merge two timeseries with different value types"},
		{"sad distribution",
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DistributionValue{}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_Int64Value{Int64Value: 47}},
			nil,
			"can't merge two timeseries with different value types"},
		{"invalid",
			&monitoring.TypedValue{Value: &monitoring.TypedValue_StringValue{}},
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DoubleValue{DoubleValue: 8.6}},
			nil,
			"invalid type for DELTA metric"},
		{"linear-happy",
			linear(11),
			linear(13),
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DistributionValue{
				DistributionValue: &distribution.Distribution{
					Count:         2,
					BucketOptions: linearOpts,
					BucketCounts:  []int64{0, 1, 1, 0, 0},
				}}},
			""},
		{"linear-sad",
			linear(11),
			exp(13),
			nil,
			"can't merge bucket counts with different numbers of buckets"},
		{"exp-happy",
			exp(3),
			exp(6),
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DistributionValue{
				DistributionValue: &distribution.Distribution{
					Count:         2,
					BucketOptions: expOpts,
					BucketCounts:  []int64{0, 1, 1, 0},
				}}},
			""},
		{"exp-sad",
			linear(1),
			exp(6),
			nil,
			"can't merge bucket counts with different numbers of buckets"},
		{"explicit-happy",
			explicit(39),
			explicit(40),
			&monitoring.TypedValue{Value: &monitoring.TypedValue_DistributionValue{
				DistributionValue: &distribution.Distribution{
					Count:         2,
					BucketOptions: explicitOpts,
					BucketCounts:  []int64{0, 0, 0, 1, 1},
				}}},
			""},
		{"explicit-sad",
			exp(83),
			explicit(17),
			nil,
			"can't merge bucket counts with different numbers of buckets"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			a := makeTS(m1, mr1, 1, 1)
			a.Points[0].Value = tt.a
			b := makeTS(m1, mr1, 2, 2)
			b.Points[0].Value = tt.b

			out, err := mergePoints(a, b)
			wantInterval := &monitoring.TimeInterval{
				StartTime: &timestamp.Timestamp{Seconds: 1, Nanos: 1000},
				EndTime:   &timestamp.Timestamp{Seconds: 2, Nanos: 3000},
			}
			if err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("merge(%v, %v) = '%s', wanted no err", a, b, err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			} else if !reflect.DeepEqual(out.Points[0].Value, tt.out) {
				t.Fatalf("merge(%v, %v) = %v, wanted value %v", a, b, out, tt.out)
			} else if !reflect.DeepEqual(out.Points[0].Interval, wantInterval) {
				t.Fatalf("merge(%v, %v) = %v, want interval %v", a, b, out, wantInterval)
			}
		})
	}
}

func distFactory(opts *distribution.Distribution_BucketOptions) func(float64) *monitoring.TypedValue {
	return func(val float64) *monitoring.TypedValue {
		buckets := makeBuckets(opts)
		buckets[index(val, opts)] = 1
		return &monitoring.TypedValue{Value: &monitoring.TypedValue_DistributionValue{
			DistributionValue: &distribution.Distribution{
				Count:         1,
				BucketOptions: opts,
				BucketCounts:  buckets,
			}}}
	}
}
