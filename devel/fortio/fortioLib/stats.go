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

package fortio

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
)

// Counter is class to record values and calculate stats (count,average,min,max,stddev)
type Counter struct {
	Count        int64
	Min          float64
	Max          float64
	Sum          float64
	SumOfSquares float64
}

// Record records a data point
func (c *Counter) Record(v float64) {
	c.Count++
	if c.Count == 1 {
		c.Min = v
		c.Max = v
	} else if v < c.Min {
		c.Min = v
	} else if v > c.Max {
		c.Max = v
	}
	c.Sum += v
	c.SumOfSquares += (v * v)
}

// Avg returns the average.
func (c *Counter) Avg() float64 {
	return c.Sum / float64(c.Count)
}

// StdDev returns the standard deviation.
func (c *Counter) StdDev() float64 {
	fC := float64(c.Count)
	sigma := (c.SumOfSquares - c.Sum*c.Sum/fC) / fC
	return math.Sqrt(sigma)
}

// FPrint prints stats
func (c *Counter) FPrint(out io.Writer, msg string) {
	fmt.Fprintf(out, "%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g\n", // nolint(errorcheck)
		msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum)
}

// Log outputs the stats to the logger
func (c *Counter) Log(msg string) {
	log.Printf("%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g",
		msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum)
}

// Histogram - written in go with inspiration from https://github.com/facebook/wdt/blob/master/util/Stats.h

var histogramBuckets = []int32{
	1, 2, 3, 4, 5, 6,
	7, 8, 9, 10, 11, // initially increment buckets by 1, my amp goes to 11 !
	12, 14, 16, 18, 20, // then by 2
	25, 30, 35, 40, 45, 50, // then by 5
	60, 70, 80, 90, 100, // then by 10
	120, 140, 160, 180, 200, // line3 *10
	250, 300, 350, 400, 450, 500, // line4 *10
	600, 700, 800, 900, 1000, // line5 *10
	2000, 3000, 4000, 5000, 7500, 10000, // another order of magnitude coarsly covered
	20000, 30000, 40000, 50000, 75000, 100000, // ditto, the end
}
var numBuckets = len(histogramBuckets)
var firstValue = float64(histogramBuckets[0])
var lastValue = float64(histogramBuckets[numBuckets-1])

// Histogram extends Counter and adds an histogram.
type Histogram struct {
	Counter
	Offset  float64 // offset applied to data before fitting into buckets
	Divider float64 // divider applied to data before fitting into buckets

	Scale float64
	hdata []int32 // n+1 buckets (for last one)

	MoreStuff string
}

//NewHistogram creates a new histogram (sets up the buckets)
func NewHistogram(Offset float64, Divider float64) *Histogram {
	h := new(Histogram)
	h.Offset = Offset
	h.Divider = Divider
	h.hdata = make([]int32, numBuckets+1)
	return h
}

var val2Bucket []int

// Tradeoff memory for speed (though that also kills the cache so...)
// this creates an array of 100k (max value) entries
// TODO: consider using an interval search for the last N big buckets
func init() {
	lastValue := int32(lastValue)
	val2Bucket = make([]int, lastValue)
	idx := 0
	for i := int32(0); i < lastValue; i++ {
		if i >= histogramBuckets[idx] {
			idx++
		}
		val2Bucket[i] = idx
	}
	if idx != numBuckets-1 {
		log.Fatalf("Bug in creating histogram buckets idx %d vs numbuckets %d (last val %d)",
			idx, numBuckets, lastValue)
	}

}

// Record records a data point
func (h *Histogram) Record(v float64) {
	h.Counter.Record(v)
	// Scaled value to bucketize:
	scaledVal := (v - h.Offset) / h.Divider
	idx := 0
	if scaledVal >= lastValue {
		idx = numBuckets
	} else if scaledVal >= firstValue {
		idx = val2Bucket[int(scaledVal)]
	} // else it's <  and idx 0
	h.hdata[idx]++
}

// CalcPercentile returns the value for an input percentile
// e.g. for 90. as input returns an estimate of the original value threshold
// where 90.0% of the data is below said threshold
func (h *Histogram) CalcPercentile(percentile float64) float64 {
	if percentile >= 100 {
		return h.Max
	}
	if percentile <= 0 {
		return h.Min
	}
	// Initial value of prev should in theory be offset_
	// but if the data is wrong (smaller than offset - eg 'negative') that
	// yields to strangeness (see one bucket test)
	prev := float64(0)
	var total int64
	ctrTotal := float64(h.Count)
	var prevPerc float64
	var perc float64
	found := false
	cur := h.Offset
	// last bucket is virtual/special - we'll use max if we reach it
	// we also use max if the bucket is past the max for better accuracy
	// and the property that target = 100 will always return max
	// (+/- rouding issues) and value close to 100 (99.9...) will be close to max
	// if the data is not sampled in several buckets
	for i := 0; i < numBuckets; i++ {
		cur = float64(histogramBuckets[i])*h.Divider + h.Offset
		total += int64(h.hdata[i])
		perc = 100. * float64(total) / ctrTotal
		if cur > h.Max {
			break
		}
		if perc >= percentile {
			found = true
			break
		}
		prevPerc = perc
		prev = cur
	}
	if !found {
		// covers the > ctrMax case
		cur = h.Max
		perc = 100. // can't be removed
	}
	// Improve accuracy near p0 too
	if prev < h.Min {
		prev = h.Min
	}
	return (prev + (percentile-prevPerc)*(cur-prev)/(perc-prevPerc))
}

// FPrint dumps the histogram (and counter) to the provided writer.
// Also calculates the percentile.
func (h *Histogram) FPrint(out io.Writer, msg string, percentile float64) {
	multiplier := h.Divider

	// calculate the last bucket index
	lastIdx := -1
	for i := numBuckets; i >= 0; i-- {
		if h.hdata[i] > 0 {
			lastIdx = i
			break
		}
	}
	if lastIdx == -1 {
		fmt.Fprintf(out, "%s : no data\n", msg) // nolint: gas
		return
	}

	// the base counter part:
	h.Counter.FPrint(out, msg)
	fmt.Fprintln(out, "# range, mid point, percentile, count") // nolint: gas
	// previous bucket value:
	prev := histogramBuckets[0]
	var total int64
	ctrTotal := float64(h.Count)
	// we can combine this loop and the calcPercentile() one but it's
	// easier to read/maintain/test when separated and it's only 2 pass on
	// very little data

	// output the data of each bucket of the histogram
	for i := 0; i <= lastIdx; i++ {
		if h.hdata[i] == 0 {
			// skip the bucket but update prev which is needed for next iter
			if i < numBuckets {
				prev = histogramBuckets[i]
			}
			continue
		}

		total += int64(h.hdata[i])
		// data in each row is separated by comma (",")
		if i > 0 {
			fmt.Fprintf(out, ">= %.6g ", multiplier*float64(prev)+h.Offset) // nolint: gas
		}
		perc := 100. * float64(total) / ctrTotal
		if i < numBuckets {
			cur := histogramBuckets[i]
			fmt.Fprintf(out, "< %.6g ", multiplier*float64(cur)+h.Offset) // nolint: gas
			midpt := multiplier*float64(prev+cur)/2. + h.Offset
			fmt.Fprintf(out, ", %.6g ", midpt) // nolint: gas
			prev = cur
		} else {
			fmt.Fprintf(out, ", %.6g ", multiplier*float64(prev)+h.Offset) // nolint: gas
		}
		fmt.Fprintf(out, ", %.2f, %d\n", perc, h.hdata[i]) // nolint: gas
	}

	// print the information of target percentiles
	fmt.Fprintf(out, "# target %g%% %.6g\n", percentile, h.CalcPercentile(percentile)) // nolint: gas
}

// Log Logs the histogram to the counter.
func (h *Histogram) Log(msg string, percentile float64) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	h.FPrint(w, msg, percentile)
	w.Flush() // nolint: gas,errcheck
	log.Println(string(b.Bytes()))
}
