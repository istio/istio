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

package metric

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/genproto/googleapis/api/distribution"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/mixer/pkg/adapter"
)

const usec int32 = int32(1 * time.Microsecond)

// We can only send 1 point per timeseries per ~minute, so for each batch we group together all points for the same timeseries.
func merge(series []*monitoring.TimeSeries, logger adapter.Logger) []*monitoring.TimeSeries {
	grouped := groupBySeries(series)
	return mergeSeries(grouped, logger)
}

func groupBySeries(series []*monitoring.TimeSeries) map[uint64][]*monitoring.TimeSeries {
	bySeries := make(map[uint64][]*monitoring.TimeSeries)
	for _, ts := range series {
		for _, ts := range series {
			if ts.MetricKind == metricpb.MetricDescriptor_DELTA || ts.MetricKind == metricpb.MetricDescriptor_CUMULATIVE {
				// DELTA and CUMULATIVE metrics cannot have the same start and end time, so if they are the same we munge
				// the data by unit of least precision that Stackdriver stores (microsecond).
				if compareUSec(ts.Points[0].Interval.StartTime, ts.Points[0].Interval.EndTime) == 0 {
					ts.Points[0].Interval.EndTime = &timestamp.Timestamp{
						Seconds: ts.Points[0].Interval.EndTime.Seconds,
						Nanos:   ts.Points[0].Interval.EndTime.Nanos + usec,
					}
				}
				// Stackdriver doesn't allow DELTA custom metrics, but queries over CUMULATIVE data without overlapping time
				// intervals have the same semantics as DELTA metrics. So we change DELTAs to CUMULATIVE to get through the API front door.
				ts.MetricKind = metricpb.MetricDescriptor_CUMULATIVE
			}
		}
		k := toKey(ts.Metric, ts.Resource)
		bySeries[k] = append(bySeries[k], ts)
	}
	return bySeries
}

func mergeSeries(bySeries map[uint64][]*monitoring.TimeSeries, logger adapter.Logger) []*monitoring.TimeSeries {
	var err error
	out := make([]*monitoring.TimeSeries, 0, len(bySeries))
	for _, ts := range bySeries {
		current := ts[0]
		start := current.Points[0].Interval.StartTime
		end := current.Points[0].Interval.EndTime
		for i := 1; i < len(ts); i++ {
			if current, err = mergePoints(current, ts[i]); err != nil {
				logger.Warningf("failed to merge timeseries: %v", err)
				continue
			}
			if compareUSec(start, ts[i].Points[0].Interval.StartTime) > 0 {
				start = ts[i].Points[0].Interval.StartTime
			}
			if compareUSec(end, ts[i].Points[0].Interval.EndTime) < 0 {
				end = ts[i].Points[0].Interval.EndTime
			}
		}

		current.Points[0].Interval = &monitoring.TimeInterval{
			StartTime: start,
			EndTime:   end,
		}
		out = append(out, current)
	}
	return out
}

// Attempts to merge two timeseries; if they are not of the same type we return an error and a, unchanged, as the resulting timeseries.
// Given the way that stackdriver metrics work, and our grouping by timeseries, we should never see two timeseries merged with different value types.
func mergePoints(a, b *monitoring.TimeSeries) (*monitoring.TimeSeries, error) {
	var ok bool
	switch av := a.Points[0].Value.Value.(type) {
	case *monitoring.TypedValue_Int64Value:
		var bv *monitoring.TypedValue_Int64Value
		if bv, ok = b.Points[0].Value.Value.(*monitoring.TypedValue_Int64Value); !ok {
			return a, fmt.Errorf("can't merge two timeseries with different value types; a has int64 value, b does not: %#v", b.Points[0].Value)
		}
		a.Points[0].Value = &monitoring.TypedValue{&monitoring.TypedValue_Int64Value{av.Int64Value + bv.Int64Value}}
	case *monitoring.TypedValue_DoubleValue:
		var bv *monitoring.TypedValue_DoubleValue
		if bv, ok = b.Points[0].Value.Value.(*monitoring.TypedValue_DoubleValue); !ok {
			return a, fmt.Errorf("can't merge two timeseries with different value types; a has double value, b does not: %#v", b.Points[0].Value)
		}
		a.Points[0].Value = &monitoring.TypedValue{&monitoring.TypedValue_DoubleValue{av.DoubleValue + bv.DoubleValue}}
	case *monitoring.TypedValue_DistributionValue:
		var bv *monitoring.TypedValue_DistributionValue
		if bv, ok = b.Points[0].Value.Value.(*monitoring.TypedValue_DistributionValue); !ok {
			return a, fmt.Errorf("can't merge two timeseries with different value types; a is a distribution, b is not: %#v", b.Points[0].Value)
		}
		// TODO: in theory we should assert that the DistributionValue's options are identical before merging, but given
		// that we produce the input data and handle grouping things before calling merging, a test is the only place we'd
		// actually see values with different options.

		// For the API, we only need to get bucket_counts, count, and bucket_options correct. We know the bucket_options
		// are identical, so we just merge counts and carry on.
		buckets, err := mergeBuckets(av.DistributionValue.BucketCounts, bv.DistributionValue.BucketCounts)
		if err != nil {
			return a, fmt.Errorf("error merging distribution buckets: %v", err)
		}
		a.Points[0].Value = &monitoring.TypedValue{Value: &monitoring.TypedValue_DistributionValue{
			DistributionValue: &distribution.Distribution{
				Count:         av.DistributionValue.Count + bv.DistributionValue.Count,
				BucketOptions: av.DistributionValue.BucketOptions,
				BucketCounts:  buckets,
			}}}
	default:
		// illegal anyway, since we can't have DELTA/CUMULATIVE metrics on anything else
		return a, fmt.Errorf("invalid type for DELTA metric: %v", a.Points[0].Value)
	}
	return a, nil
}

func mergeBuckets(a, b []int64) ([]int64, error) {
	if len(a) != len(b) {
		return a, fmt.Errorf("can't merge bucket counts with different numbers of buckets: len(a) = %d, len(b) = %d", len(a), len(b))
	}
	for i := 0; i < len(a); i++ {
		a[i] += b[i]
	}
	return a, nil
}

// Compare returns < 0 if a < b, > 0 if a > b, and 0 if a == b.
// Note that Stackdriver stores times at microsecond precision, so our comparison is performed at that precision too.
func compareUSec(a, b *timestamp.Timestamp) int64 {
	if a.Seconds == b.Seconds {
		return int64(toUSec(a.Nanos) - toUSec(b.Nanos))
	}
	return a.Seconds - b.Seconds
}

func toUSec(nanos int32) int32 {
	return nanos / int32(time.Microsecond)
}

// We need to group TimeSeries based on the deep equality of their metric and monitored resource. Golang won't do this
// out of the box for us, so here we use a hasher to generate a key from the fields of both structs.
func toKey(metric *metricpb.Metric, mr *monitoredres.MonitoredResource) uint64 {
	// Per the package comments on hash, writing to a hash will never return an error, and we don't care how many bytes there were.
	buf := make([]byte, 8)
	hash := fnv.New64()
	binary.BigEndian.PutUint64(buf, stableMapHash(metric.Labels))
	_, _ = hash.Write(buf)
	binary.BigEndian.PutUint64(buf, stableMapHash(mr.Labels))
	_, _ = hash.Write(buf)
	_, _ = hash.Write([]byte(metric.Type))
	_, _ = hash.Write([]byte(mr.Type))
	return hash.Sum64()
}

// Returns a stable hash for the map regardless of iteration order by XORing the hash of the map's keys and values.
func stableMapHash(in map[string]string) uint64 {
	var h uint64
	for k, v := range in {
		hash := fnv.New64() // this is cheap, just copies and derefs a constant uint64
		_, _ = hash.Write([]byte(k))
		_, _ = hash.Write([]byte(v))
		h ^= hash.Sum64()
	}
	return h
}
