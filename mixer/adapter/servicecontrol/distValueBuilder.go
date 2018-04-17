// Copyright 2017 Istio Authors
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

package servicecontrol

import (
	"errors"
	"math"

	sc "google.golang.org/api/servicecontrol/v1"
)

type (
	// Option defines how the exponential distribution value bucket is constructed. For detail,
	// see https://godoc.org/google.golang.org/api/servicecontrol/v1#ExponentialBuckets and
	// the reference implementation in
	// https://github.com/cloudendpoints/esp/blob/master/src/api_manager/service_control/proto.cc
	distValueBuilderOption struct {
		buckets int64
		growth  float64
		scale   float64
	}

	// A builder that generates distribute value based on given option.
	distValueBuilder struct {
		option distValueBuilderOption
		dist   *sc.Distribution
	}
)

var (
	// We use the same parameter as in Google ESP implementation.
	// Option for time-based bucket
	timeOption = distValueBuilderOption{
		29,
		2.0,
		1e-6,
	}
	// Option for size-based bucket
	sizeOption = distValueBuilderOption{
		8,
		10.0,
		1,
	}
)

// addSample adds a sample to distribution value.
func (b *distValueBuilder) addSample(value float64) {
	b.updateCommonStatistics(value)
	b.UpdateExponentialBucketCount(value)
}

// updateCommonStatistics updates common statistics such as mean, min/max when a sample is added.
func (b *distValueBuilder) updateCommonStatistics(value float64) {
	dist := b.dist
	if dist.Count == 0 {
		dist.Minimum = value
		dist.Maximum = value
		dist.Mean = value
		dist.SumOfSquaredDeviation = 0.0
	} else {
		dist.Minimum = math.Min(dist.Minimum, value)
		dist.Maximum = math.Max(dist.Maximum, value)
		// We use a well known online variance method to calculate square sum deviation.
		// https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance
		delta := value - dist.Mean
		dist.Mean += delta / (float64(dist.Count) + 1.0)
		delta2 := value - dist.Mean
		dist.SumOfSquaredDeviation += delta * delta2
	}
	dist.Count++
}

// UpdateExponentialBucketCount updates sample account in an exponential bucket distribution value.
func (b *distValueBuilder) UpdateExponentialBucketCount(value float64) {
	dist := b.dist
	var idx int64
	if value >= dist.ExponentialBuckets.Scale {
		idx = 1 +
			int64(math.Log(value/dist.ExponentialBuckets.Scale)/
				math.Log(dist.ExponentialBuckets.GrowthFactor))
		if idx > dist.ExponentialBuckets.NumFiniteBuckets+1 {
			idx = dist.ExponentialBuckets.NumFiniteBuckets + 1
		}
	}
	dist.BucketCounts[idx]++
}

// build builds a distribution value from a builder.
func (b *distValueBuilder) build() *sc.Distribution {
	return b.dist
}

func newDistValueBuilder(option distValueBuilderOption) (distValueBuilder, error) {
	if option.buckets <= 0 || option.growth <= 1.0 || option.scale <= 0 {
		return distValueBuilder{}, errors.New("invalid distValueBuilderOption")
	}
	return distValueBuilder{
		option,
		&sc.Distribution{
			// Add 2, because we need to take care -inf, and +inf
			BucketCounts: make([]int64, option.buckets+2),
			Count:        0,
			ExponentialBuckets: &sc.ExponentialBuckets{
				GrowthFactor:     option.growth,
				NumFiniteBuckets: option.buckets,
				Scale:            option.scale,
			},
			Maximum:               0.0,
			Mean:                  0.0,
			Minimum:               0.0,
			SumOfSquaredDeviation: 0.0,
		},
	}, nil
}
