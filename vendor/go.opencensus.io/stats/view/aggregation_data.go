// Copyright 2017, OpenCensus Authors
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
//

package view

import (
	"math"
)

// AggregationData represents an aggregated value from a collection.
// They are reported on the view data during exporting.
// Mosts users won't directly access aggregration data.
type AggregationData interface {
	isAggregationData() bool
	addSample(v float64)
	clone() AggregationData
	equal(other AggregationData) bool
}

const epsilon = 1e-9

// CountData is the aggregated data for the Count aggregation.
// A count aggregation processes data and counts the recordings.
//
// Most users won't directly access count data.
type CountData int64

func newCountData(v int64) *CountData {
	tmp := CountData(v)
	return &tmp
}

func (a *CountData) isAggregationData() bool { return true }

func (a *CountData) addSample(_ float64) {
	*a = *a + 1
}

func (a *CountData) clone() AggregationData {
	return newCountData(int64(*a))
}

func (a *CountData) equal(other AggregationData) bool {
	a2, ok := other.(*CountData)
	if !ok {
		return false
	}

	return int64(*a) == int64(*a2)
}

// SumData is the aggregated data for the Sum aggregation.
// A sum aggregation processes data and sums up the recordings.
//
// Most users won't directly access sum data.
type SumData float64

func newSumData(v float64) *SumData {
	tmp := SumData(v)
	return &tmp
}

func (a *SumData) isAggregationData() bool { return true }

func (a *SumData) addSample(f float64) {
	*a += SumData(f)
}

func (a *SumData) clone() AggregationData {
	return newSumData(float64(*a))
}

func (a *SumData) equal(other AggregationData) bool {
	a2, ok := other.(*SumData)
	if !ok {
		return false
	}
	return math.Pow(float64(*a)-float64(*a2), 2) < epsilon
}

// MeanData is the aggregated data for the Mean aggregation.
// A mean aggregation processes data and maintains the mean value.
//
// Most users won't directly access mean data.
type MeanData struct {
	Count int64   // number of data points aggregated
	Mean  float64 // mean of all data points
}

func newMeanData(mean float64, count int64) *MeanData {
	return &MeanData{
		Mean:  mean,
		Count: count,
	}
}

// Sum returns the sum of all samples collected.
func (a *MeanData) Sum() float64 { return a.Mean * float64(a.Count) }

func (a *MeanData) isAggregationData() bool { return true }

func (a *MeanData) addSample(f float64) {
	a.Count++
	if a.Count == 1 {
		a.Mean = f
		return
	}
	a.Mean = a.Mean + (f-a.Mean)/float64(a.Count)
}

func (a *MeanData) clone() AggregationData {
	return newMeanData(a.Mean, a.Count)
}

func (a *MeanData) equal(other AggregationData) bool {
	a2, ok := other.(*MeanData)
	if !ok {
		return false
	}
	return a.Count == a2.Count && math.Pow(a.Mean-a2.Mean, 2) < epsilon
}

// DistributionData is the aggregated data for the
// Distribution aggregation.
//
// Most users won't directly access distribution data.
type DistributionData struct {
	Count           int64     // number of data points aggregated
	Min             float64   // minimum value in the distribution
	Max             float64   // max value in the distribution
	Mean            float64   // mean of the distribution
	SumOfSquaredDev float64   // sum of the squared deviation from the mean
	CountPerBucket  []int64   // number of occurrences per bucket
	bounds          []float64 // histogram distribution of the values
}

func newDistributionData(bounds []float64) *DistributionData {
	return &DistributionData{
		CountPerBucket: make([]int64, len(bounds)+1),
		bounds:         bounds,
		Min:            math.MaxFloat64,
		Max:            math.SmallestNonzeroFloat64,
	}
}

// Sum returns the sum of all samples collected.
func (a *DistributionData) Sum() float64 { return a.Mean * float64(a.Count) }

func (a *DistributionData) variance() float64 {
	if a.Count <= 1 {
		return 0
	}
	return a.SumOfSquaredDev / float64(a.Count-1)
}

func (a *DistributionData) isAggregationData() bool { return true }

func (a *DistributionData) addSample(f float64) {
	if f < a.Min {
		a.Min = f
	}
	if f > a.Max {
		a.Max = f
	}
	a.Count++
	a.incrementBucketCount(f)

	if a.Count == 1 {
		a.Mean = f
		return
	}

	oldMean := a.Mean
	a.Mean = a.Mean + (f-a.Mean)/float64(a.Count)
	a.SumOfSquaredDev = a.SumOfSquaredDev + (f-oldMean)*(f-a.Mean)
}

func (a *DistributionData) incrementBucketCount(f float64) {
	if len(a.bounds) == 0 {
		a.CountPerBucket[0]++
		return
	}

	for i, b := range a.bounds {
		if f < b {
			a.CountPerBucket[i]++
			return
		}
	}
	a.CountPerBucket[len(a.bounds)]++
}

func (a *DistributionData) clone() AggregationData {
	counts := make([]int64, len(a.CountPerBucket))
	copy(counts, a.CountPerBucket)
	c := *a
	c.CountPerBucket = counts
	return &c
}

func (a *DistributionData) equal(other AggregationData) bool {
	a2, ok := other.(*DistributionData)
	if !ok {
		return false
	}
	if a2 == nil {
		return false
	}
	if len(a.CountPerBucket) != len(a2.CountPerBucket) {
		return false
	}
	for i := range a.CountPerBucket {
		if a.CountPerBucket[i] != a2.CountPerBucket[i] {
			return false
		}
	}
	return a.Count == a2.Count && a.Min == a2.Min && a.Max == a2.Max && math.Pow(a.Mean-a2.Mean, 2) < epsilon && math.Pow(a.variance()-a2.variance(), 2) < epsilon
}
