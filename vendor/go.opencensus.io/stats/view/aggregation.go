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

//go:generate stringer -type AggType

// AggType represents the type of aggregation function used on a View.
type AggType int

const (
	AggTypeNone         AggType = iota // no aggregation; reserved for future use.
	AggTypeCount                       // the count aggregation, see Count.
	AggTypeSum                         // the sum aggregation, see Sum.
	AggTypeMean                        // the mean aggregation, see Mean.
	AggTypeDistribution                // the distribution aggregation, see Distribution.
)

// Aggregation represents a data aggregation method. Use one of the functions:
// Count, Sum, Mean, or Distribution to construct an Aggregation.
type Aggregation struct {
	Type    AggType   // Type is the AggType of this Aggregation.
	Buckets []float64 // Buckets are the bucket endpoints if this Aggregation represents a distribution, see Distribution.

	newData func() AggregationData
}

var (
	aggCount = &Aggregation{
		Type: AggTypeCount,
		newData: func() AggregationData {
			return newCountData(0)
		},
	}
	aggSum = &Aggregation{
		Type: AggTypeSum,
		newData: func() AggregationData {
			return newSumData(0)
		},
	}
	aggMean = &Aggregation{
		Type: AggTypeMean,
		newData: func() AggregationData {
			return newMeanData(0, 0)
		},
	}
)

// Count indicates that data collected and aggregated
// with this method will be turned into a count value.
// For example, total number of accepted requests can be
// aggregated by using Count.
func Count() *Aggregation {
	return aggCount
}

// Sum indicates that data collected and aggregated
// with this method will be summed up.
// For example, accumulated request bytes can be aggregated by using
// Sum.
func Sum() *Aggregation {
	return aggSum
}

// Mean indicates that collect and aggregate data and maintain
// the mean value.
// For example, average latency in milliseconds can be aggregated by using
// Mean, although in most cases it is preferable to use a Distribution.
func Mean() *Aggregation {
	return aggMean
}

// Distribution indicates that the desired aggregation is
// a histogram distribution.
//
// An distribution aggregation may contain a histogram of the values in the
// population. The bucket boundaries for that histogram are described
// by the bounds. This defines len(bounds)+1 buckets.
//
// If len(bounds) >= 2 then the boundaries for bucket index i are:
//
//     [-infinity, bounds[i]) for i = 0
//     [bounds[i-1], bounds[i]) for 0 < i < length
//     [bounds[i-1], +infinity) for i = length
//
// If len(bounds) is 0 then there is no histogram associated with the
// distribution. There will be a single bucket with boundaries
// (-infinity, +infinity).
//
// If len(bounds) is 1 then there is no finite buckets, and that single
// element is the common boundary of the overflow and underflow buckets.
func Distribution(bounds ...float64) *Aggregation {
	return &Aggregation{
		Type:    AggTypeDistribution,
		Buckets: bounds,
		newData: func() AggregationData {
			return newDistributionData(bounds)
		},
	}
}
