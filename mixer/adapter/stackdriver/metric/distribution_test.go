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
	"testing"

	"google.golang.org/genproto/googleapis/api/distribution"
)

func TestIndex(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		opts *distribution.Distribution_BucketOptions
		out  int
	}{
		{"linear-underflow", 0, linear(10, 1, 3), 0},            // 5 buckets: under, 10-11, 11-12, 12-13, over
		{"linear-first", 10, linear(10, 1, 3), 1},               // 5 buckets: under, 10-11, 11-12, 12-13, over
		{"linear-second", 11, linear(10, 1, 3), 2},              // 5 buckets: under, 10-11, 11-12, 12-13, over
		{"linear-third", 12, linear(10, 1, 3), 3},               // 5 buckets: under, 10-11, 11-12, 12-13, over
		{"linear-overflow val == max", 13, linear(10, 1, 3), 4}, // 5 buckets: under, 10-11, 11-12, 12-13, over
		{"linear-overflow", 17, linear(10, 1, 1), 2},            // 3 buckets total: under, 10-11, over

		{"exp-underflow", 0, exp(10, 2, 1), 0},            // 3 buckets: under, 10-20, over
		{"exp-first", 10, exp(10, 10, 3), 1},              // 5 buckets: under, 10-100, 100-1000, 1000-10000, over
		{"exp-second", 100, exp(10, 10, 3), 2},            // 5 buckets: under, 10-100, 100-1000, 1000-10000, over
		{"exp-third", 1000, exp(10, 10, 3), 3},            // 5 buckets: under, 10-100, 100-1000, 1000-10000, over
		{"exp-overflow val == max", 20, exp(10, 2, 1), 2}, // 3 buckets: under, 10-20, over
		{"exp-overflow", 200, exp(10, 2, 1), 2},           // 3 buckets: under, 10-20, over

		{"explicit-underflow", 0, explicit([]float64{1, 2}), 0},           // 3 buckets: under, 1-2, 2-over
		{"explicit", 1, explicit([]float64{1, 2}), 1},                     // 3 buckets: under, 1-2, 2-over
		{"explicit-overflow val == max", 2, explicit([]float64{1, 2}), 2}, // 3 buckets: under, 1-2, 2-over
		{"explicit-overflow", 3, explicit([]float64{1, 2}), 2},            // 3 buckets: under, 1-2, 2-over
		{"explicit 1 bucket-under", 9, explicit([]float64{10}), 0},
		{"explicit 1 bucket-exact", 10, explicit([]float64{10}), 1},
		{"explicit 1 bucket-over", 11, explicit([]float64{10}), 1},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if actual := index(tt.in, tt.opts); actual != tt.out {
				t.Fatalf("index(%f, %v) = %d, wanted %d", tt.in, tt.opts, actual, tt.out)
			}
		})
	}
}

func linear(offset, width float64, buckets int32) *distribution.Distribution_BucketOptions {
	return &distribution.Distribution_BucketOptions{Options: &distribution.Distribution_BucketOptions_LinearBuckets{
		LinearBuckets: &distribution.Distribution_BucketOptions_Linear{
			Offset:           offset,
			Width:            width,
			NumFiniteBuckets: buckets,
		}}}
}

func exp(scale, gf float64, buckets int32) *distribution.Distribution_BucketOptions {
	return &distribution.Distribution_BucketOptions{Options: &distribution.Distribution_BucketOptions_ExponentialBuckets{
		ExponentialBuckets: &distribution.Distribution_BucketOptions_Exponential{
			Scale:            scale,
			GrowthFactor:     gf,
			NumFiniteBuckets: buckets,
		}}}
}

func explicit(bounds []float64) *distribution.Distribution_BucketOptions {
	return &distribution.Distribution_BucketOptions{Options: &distribution.Distribution_BucketOptions_ExplicitBuckets{
		ExplicitBuckets: &distribution.Distribution_BucketOptions_Explicit{
			Bounds: bounds,
		}}}
}
