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
	"fmt"

	"google.golang.org/genproto/googleapis/api/distribution"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"

	descriptor "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/adapter/stackdriver/config"
)

func toDist(val interface{}, i info) (*monitoringpb.TypedValue, error) {
	var fval float64
	switch i.vtype {
	case descriptor.INT64:
		if ival, ok := val.(int64); ok {
			fval = float64(ival)
		} else {
			return nil, fmt.Errorf("expected int64 value, got: %#v", val)
		}
	case descriptor.DOUBLE:
		if dval, ok := val.(float64); ok {
			fval = dval
		} else {
			return nil, fmt.Errorf("expected float64 value, got: %#v", val)
		}
	}

	opts := toBucketOpts(i.minfo.Buckets)
	buckets := makeBuckets(opts)
	buckets[index(fval, opts)] = 1
	return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DistributionValue{
		DistributionValue: &distribution.Distribution{
			Count:         1,
			BucketOptions: opts,
			BucketCounts:  buckets,
		}}}, nil
}

func toBucketOpts(definition *config.Params_MetricInfo_BucketsDefinition) *distribution.Distribution_BucketOptions {
	switch t := definition.Definition.(type) {
	case *config.Params_MetricInfo_BucketsDefinition_LinearBuckets:
		return &distribution.Distribution_BucketOptions{Options: &distribution.Distribution_BucketOptions_LinearBuckets{
			LinearBuckets: &distribution.Distribution_BucketOptions_Linear{
				Offset:           t.LinearBuckets.Offset,
				NumFiniteBuckets: t.LinearBuckets.NumFiniteBuckets,
				Width:            t.LinearBuckets.Width,
			}}}
	case *config.Params_MetricInfo_BucketsDefinition_ExponentialBuckets:
		return &distribution.Distribution_BucketOptions{Options: &distribution.Distribution_BucketOptions_ExponentialBuckets{
			ExponentialBuckets: &distribution.Distribution_BucketOptions_Exponential{
				NumFiniteBuckets: t.ExponentialBuckets.NumFiniteBuckets,
				GrowthFactor:     t.ExponentialBuckets.GrowthFactor,
				Scale:            t.ExponentialBuckets.Scale,
			}}}
	case *config.Params_MetricInfo_BucketsDefinition_ExplicitBuckets:
		return &distribution.Distribution_BucketOptions{Options: &distribution.Distribution_BucketOptions_ExplicitBuckets{
			ExplicitBuckets: &distribution.Distribution_BucketOptions_Explicit{
				Bounds: t.ExplicitBuckets.Bounds,
			}}}
	default:
		return &distribution.Distribution_BucketOptions{}
	}
}

func makeBuckets(opts *distribution.Distribution_BucketOptions) []int64 {
	switch t := opts.Options.(type) {
	case *distribution.Distribution_BucketOptions_LinearBuckets:
		// + 2, overflow and underflow
		return make([]int64, t.LinearBuckets.NumFiniteBuckets+2)
	case *distribution.Distribution_BucketOptions_ExponentialBuckets:
		// + 2, overflow and underflow
		return make([]int64, t.ExponentialBuckets.NumFiniteBuckets+2)
	case *distribution.Distribution_BucketOptions_ExplicitBuckets:
		// + 1, len(bounds) already includes an underflow, we need 1 more for overflow
		return make([]int64, len(t.ExplicitBuckets.Bounds)+1)
	default:
		return []int64{}
	}
}

// Given a value and a description of the buckets, computes the index of the bucket to insert in to.
func index(val float64, opts *distribution.Distribution_BucketOptions) int {
	switch t := opts.Options.(type) {
	case *distribution.Distribution_BucketOptions_LinearBuckets:
		//    Upper bound (0 <= i < N-1):     offset + (width * i).
		//    Lower bound (1 <= i < N):       offset + (width * (i - 1)).
		lower := t.LinearBuckets.Offset
		if val < lower {
			return 0
		}

		width := t.LinearBuckets.Width
		upper := t.LinearBuckets.Offset + width
		N := int(t.LinearBuckets.NumFiniteBuckets) + 2 // + 2, overflow and underflow
		for i := 1; i < N; i++ {
			if lower <= val && val < upper {
				return i
			}
			lower += width
			upper += width
		}
		return N - 1 // the overflow bucket
	case *distribution.Distribution_BucketOptions_ExponentialBuckets:
		//    Upper bound (0 <= i < N-1):     scale * (growth_factor ^ i).
		//    Lower bound (1 <= i < N):       scale * (growth_factor ^ (i - 1)).
		lower := t.ExponentialBuckets.Scale
		if val < lower {
			return 0
		}

		gf := t.ExponentialBuckets.GrowthFactor
		upper := lower * gf
		N := int(t.ExponentialBuckets.NumFiniteBuckets) + 2 // + 2, overflow and underflow
		for i := 1; i < N; i++ {
			if lower <= val && val < upper {
				return i
			}
			lower *= gf
			upper *= gf
		}
		return N - 1 // the overflow bucket
	case *distribution.Distribution_BucketOptions_ExplicitBuckets:
		//    Upper bound (0 <= i < N-1):     bounds[i]
		//    Lower bound (1 <= i < N);       bounds[i - 1]
		bounds := t.ExplicitBuckets.Bounds
		if len(bounds) > 0 && val < bounds[0] {
			return 0
		} else if len(bounds) == 1 {
			// Per comment on the bucketing options:
			//     If `bounds` has only one element, then there are no finite buckets, and that single element is the common
			//     boundary of the overflow and underflow buckets.
			// if len(bounds) is exactly 1 then we already checked to see if val < bounds[0], so it must be greater, so
			// we return the index of the overflow bucket.
			return 1
		}
		// We're not in the case where's there's only under-/over-flow buckets, so we'll do the normal thing
		N := len(t.ExplicitBuckets.Bounds)
		for i := 1; i < N; i++ {
			if bounds[i-1] <= val && val < bounds[i] {
				return i
			}
		}
		return N // the overflow bucket
	default:
		return 0
	}
}
