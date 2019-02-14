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
	"math"
	"reflect"
	"testing"

	"google.golang.org/api/googleapi"
	sc "google.golang.org/api/servicecontrol/v1"
)

var option = distValueBuilderOption{
	8,
	10,
	1,
}

const tolerance = 1e-5

func meanValue(values []float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func squareDeviationSum(values []float64) float64 {
	if len(values) <= 1 {
		return 0.0
	}

	mean := meanValue(values)
	result := 0.0
	for _, value := range values {
		result += math.Pow(value-mean, 2.0)
	}
	return result
}

func TestNewDistValueBuilder(t *testing.T) {
	b, err := newDistValueBuilder(option)

	if err != nil {
		t.Fatalf(`newDistValueBuilder() failed with %v`, err)
	}

	expected := sc.Distribution{
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
	}
	if !reflect.DeepEqual(expected, *b.dist) {
		t.Errorf(`expected distribution value %v, but get %v`, expected, *b.dist)
	}
}

func TestBuildDistribution(t *testing.T) {
	b, err := newDistValueBuilder(option)
	if err != nil {
		t.Fatalf(`newDistValueBuilder() failed with %v`, err)
	}

	testValues := []float64{10.0, 11.0, 100.0}
	for _, value := range testValues {
		b.addSample(value)
	}

	dist := b.build()
	if dist.Count != int64(len(testValues)) {
		t.Errorf(`expect dist.Count == %v', but get: %v`, len(testValues), dist.Count)
	}
	if math.Abs(dist.Minimum-10.0) > tolerance {
		t.Errorf(`expect dist.Minimum == 10.0, but get: %v`, dist.Minimum)
	}
	if math.Abs(dist.Maximum-100.0) > tolerance {
		t.Errorf(`expect dist.Maximum == 100.0, but get: %v`, dist.Maximum)
	}
	expectedMean := meanValue(testValues)
	if math.Abs(dist.Mean-expectedMean) > tolerance {
		t.Errorf(`expect dist.Mean == %v, but get: %v`, expectedMean, dist.Mean)
	}
	expectedSumOfSquaredDeviation := squareDeviationSum(testValues)
	if math.Abs(dist.SumOfSquaredDeviation-expectedSumOfSquaredDeviation) > tolerance {
		t.Errorf(`expect dist.SumOfSquaredDeviation == %v, but get: %v`,
			expectedSumOfSquaredDeviation, dist.SumOfSquaredDeviation)
	}
	expectedBucketCount := googleapi.Int64s{0, 0, 2, 1, 0, 0, 0, 0, 0, 0}
	if !reflect.DeepEqual(expectedBucketCount, dist.BucketCounts) {
		t.Errorf(`incorrect dist.BucketCounts, expect: %v, get: %v`,
			expectedBucketCount, dist.BucketCounts)
	}
}
