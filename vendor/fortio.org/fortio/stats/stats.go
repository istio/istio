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

package stats // import "fortio.org/fortio/stats"

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"fortio.org/fortio/log"
)

// Counter is a type whose instances record values
// and calculate stats (count,average,min,max,stddev).
type Counter struct {
	Count        int64
	Min          float64
	Max          float64
	Sum          float64
	sumOfSquares float64
}

// Record records a data point.
func (c *Counter) Record(v float64) {
	c.RecordN(v, 1)
}

// RecordN efficiently records the same value N times
func (c *Counter) RecordN(v float64, n int) {
	isFirst := (c.Count == 0)
	c.Count += int64(n)
	if isFirst {
		c.Min = v
		c.Max = v
	} else if v < c.Min {
		c.Min = v
	} else if v > c.Max {
		c.Max = v
	}
	s := v * float64(n)
	c.Sum += s
	c.sumOfSquares += (s * s)
}

// Avg returns the average.
func (c *Counter) Avg() float64 {
	return c.Sum / float64(c.Count)
}

// StdDev returns the standard deviation.
func (c *Counter) StdDev() float64 {
	fC := float64(c.Count)
	sigma := (c.sumOfSquares - c.Sum*c.Sum/fC) / fC
	// should never happen but it does
	if sigma < 0 {
		log.Warnf("Unexpected negative sigma for %+v: %g", c, sigma)
		return 0
	}
	return math.Sqrt(sigma)
}

// Print prints stats.
func (c *Counter) Print(out io.Writer, msg string) {
	_, _ = fmt.Fprintf(out, "%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g\n", // nolint(errorcheck)
		msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum)
}

// Log outputs the stats to the logger.
func (c *Counter) Log(msg string) {
	log.Infof("%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g",
		msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum)
}

// Reset clears the counter to reset it to original 'no data' state.
func (c *Counter) Reset() {
	var empty Counter
	*c = empty
}

// Transfer merges the data from src into this Counter and clears src.
func (c *Counter) Transfer(src *Counter) {
	if src.Count == 0 {
		return // nothing to do
	}
	if c.Count == 0 {
		*c = *src // copy everything at once
		src.Reset()
		return
	}
	c.Count += src.Count
	if src.Min < c.Min {
		c.Min = src.Min
	}
	if src.Max > c.Max {
		c.Max = src.Max
	}
	c.Sum += src.Sum
	c.sumOfSquares += src.sumOfSquares
	src.Reset()
}

// Histogram - written in go with inspiration from https://github.com/facebook/wdt/blob/master/util/Stats.h

// The intervals are ]prev,current] so for "90" (previous is 80) the values in that bucket are >80 and <=90
// that way a cumulative % up to that bucket means X% of the data <= 90 (or 100-X% > 90), works well for max too
// There are 2 special buckets - the first one is from min to and including 0,
// one after the last for value > last and up to max
var (
	histogramBucketValues = []int32{
		0, 1, 2, 3, 4, 5, 6,
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
	numValues  = len(histogramBucketValues)
	numBuckets = numValues + 1 // 1 special first bucket is <= 0; and 1 extra last bucket is > 100000
	firstValue = float64(histogramBucketValues[0])
	lastValue  = float64(histogramBucketValues[numValues-1])
	val2Bucket []int // ends at 1000. Remaining values will not be received in constant time.

	maxArrayValue      = int32(1000) // Last value looked up as O(1) array, the rest is linear search
	maxArrayValueIndex = -1          // Index of maxArrayValue
)

// Histogram extends Counter and adds an histogram.
// Must be created using NewHistogram or anotherHistogram.Clone()
// and not directly.
type Histogram struct {
	Counter
	Offset  float64 // offset applied to data before fitting into buckets
	Divider float64 // divider applied to data before fitting into buckets
	// Don't access directly (outside of this package):
	Hdata []int32 // numValues buckets (one more than values, for last one)
}

// For export of the data:

// Interval is a range from start to end.
// Interval are left closed, open right expect the last one which includes Max.
// ie [Start, End[ with the next one being [PrevEnd, NextEnd[.
type Interval struct {
	Start float64
	End   float64
}

// Bucket is the data for 1 bucket: an Interval and the occurrence Count for
// that interval.
type Bucket struct {
	Interval
	Percent float64 // Cumulative percentile
	Count   int64   // How many in this bucket
}

// Percentile value for the percentile
type Percentile struct {
	Percentile float64 // For this Percentile
	Value      float64 // value at that Percentile
}

// HistogramData is the exported Histogram data, a sorted list of intervals
// covering [Min, Max]. Pure data, so Counter for instance is flattened
type HistogramData struct {
	Count       int64
	Min         float64
	Max         float64
	Sum         float64
	Avg         float64
	StdDev      float64
	Data        []Bucket
	Percentiles []Percentile
}

// NewHistogram creates a new histogram (sets up the buckets).
// Divider value can not be zero, otherwise returns zero
func NewHistogram(Offset float64, Divider float64) *Histogram {
	h := new(Histogram)
	h.Offset = Offset
	if Divider == 0 {
		return nil
	}
	h.Divider = Divider
	h.Hdata = make([]int32, numBuckets)
	return h
}

// Val2Bucket values are kept in two different structure
// val2Bucket allows you reach between 0 and 1000 in constant time
func init() {
	val2Bucket = make([]int, maxArrayValue)
	maxArrayValueIndex = -1
	for i, value := range histogramBucketValues {
		if value == maxArrayValue {
			maxArrayValueIndex = i
			break
		}
	}
	if maxArrayValueIndex == -1 {
		log.Fatalf("Bug boundary maxArrayValue=%d not found in bucket list %v", maxArrayValue, histogramBucketValues)
	}
	idx := 0
	for i := int32(0); i < maxArrayValue; i++ {
		if i >= histogramBucketValues[idx] {
			idx++
		}
		val2Bucket[i] = idx
	}
	// coding bug detection (aka impossible if it works once) until 1000
	if idx != maxArrayValueIndex {
		log.Fatalf("Bug in creating histogram index idx %d vs index %d up to %d", idx, int(maxArrayValue), maxArrayValue)
	}
}

// lookUpIdx looks for scaledValue's index in histogramBucketValues
// TODO: change linear time to O(log(N)) with binary search
func lookUpIdx(scaledValue int) int {
	scaledValue32 := int32(scaledValue)
	if scaledValue32 < maxArrayValue { //constant
		return val2Bucket[scaledValue]
	}
	for i := maxArrayValueIndex; i < numValues; i++ {
		if histogramBucketValues[i] > scaledValue32 {
			return i
		}
	}
	log.Fatalf("never reached/bug")
	return 0
}

// Record records a data point.
func (h *Histogram) Record(v float64) {
	h.RecordN(v, 1)
}

// RecordN efficiently records a data point N times.
func (h *Histogram) RecordN(v float64, n int) {
	h.Counter.RecordN(v, n)
	h.record(v, n)
}

// Records v value to count times
func (h *Histogram) record(v float64, count int) {
	// Scaled value to bucketize - we subtract epsilon because the interval
	// is open to the left ] start, end ] so when exactly on start it has
	// to fall on the previous bucket. TODO add boundary tests
	scaledVal := (v-h.Offset)/h.Divider - 0.0001
	var idx int
	if scaledVal <= firstValue {
		idx = 0
	} else if scaledVal > lastValue {
		idx = numBuckets - 1 // last bucket is for > last value
	} else {
		// else we look it up
		idx = lookUpIdx(int(scaledVal))
	}
	h.Hdata[idx] += int32(count)
}

// CalcPercentile returns the value for an input percentile
// e.g. for 90. as input returns an estimate of the original value threshold
// where 90.0% of the data is below said threshold.
// with 3 data points 10, 20, 30; p0-p33.33 == 10, p 66.666 = 20, p100 = 30
// p33.333 - p66.666 = linear between 10 and 20; so p50 = 15
// TODO: consider spreading the count of the bucket evenly from start to end
// so the % grows by at least to 1/N on start of range, and for last range
// when start == end we should get to that % faster
func (e *HistogramData) CalcPercentile(percentile float64) float64 {
	if len(e.Data) == 0 {
		log.Errf("Unexpected call to CalcPercentile(%g) with no data", percentile)
		return 0
	}
	if percentile >= 100 {
		return e.Max
	}
	// We assume Min is at least a single point so at least covers 1/Count %
	pp := 100. / float64(e.Count) // previous percentile
	if percentile <= pp {
		return e.Min
	}
	for _, cur := range e.Data {
		if percentile <= cur.Percent {
			return cur.Start + (percentile-pp)/(cur.Percent-pp)*(cur.End-cur.Start)
		}
		pp = cur.Percent
	}
	return e.Max // not reached
}

// Export translate the internal representation of the histogram data in
// an externally usable one. Calculates the request Percentiles.
func (h *Histogram) Export() *HistogramData {
	var res HistogramData
	res.Count = h.Counter.Count
	res.Min = h.Counter.Min
	res.Max = h.Counter.Max
	res.Sum = h.Counter.Sum
	res.Avg = h.Counter.Avg()
	res.StdDev = h.Counter.StdDev()
	multiplier := h.Divider
	offset := h.Offset
	// calculate the last bucket index
	lastIdx := -1
	for i := numBuckets - 1; i >= 0; i-- {
		if h.Hdata[i] > 0 {
			lastIdx = i
			break
		}
	}
	if lastIdx == -1 {
		return &res
	}

	// previous bucket value:
	prev := histogramBucketValues[0]
	var total int64
	ctrTotal := float64(h.Count)
	// export the data of each bucket of the histogram
	for i := 0; i <= lastIdx; i++ {
		if h.Hdata[i] == 0 {
			// empty bucket: skip it but update prev which is needed for next iter
			if i < numValues {
				prev = histogramBucketValues[i]
			}
			continue
		}
		var b Bucket
		total += int64(h.Hdata[i])
		if len(res.Data) == 0 {
			// First entry, start is min
			b.Start = h.Min
		} else {
			b.Start = multiplier*float64(prev) + offset
		}
		b.Percent = 100. * float64(total) / ctrTotal
		if i < numValues {
			cur := histogramBucketValues[i]
			b.End = multiplier*float64(cur) + offset
			prev = cur
		} else {
			// Last Entry
			b.Start = multiplier*float64(prev) + offset
			b.End = h.Max
		}
		b.Count = int64(h.Hdata[i])
		res.Data = append(res.Data, b)
	}
	res.Data[len(res.Data)-1].End = h.Max
	return &res
}

// CalcPercentiles calculates the requested percentile and add them to the
// HistogramData. Potential TODO: sort or assume sorting and calculate all
// the percentiles in 1 pass (greater and greater values).
func (e *HistogramData) CalcPercentiles(percentiles []float64) *HistogramData {
	if e.Count == 0 {
		return e
	}
	for _, p := range percentiles {
		e.Percentiles = append(e.Percentiles, Percentile{p, e.CalcPercentile(p)})
	}
	return e
}

// Print dumps the histogram (and counter) to the provided writer.
// Also calculates the percentile.
func (e *HistogramData) Print(out io.Writer, msg string) {
	if len(e.Data) == 0 {
		_, _ = fmt.Fprintf(out, "%s : no data\n", msg) // nolint: gas
		return
	}
	// the base counter part:
	_, _ = fmt.Fprintf(out, "%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g\n",
		msg, e.Count, e.Avg, e.StdDev, e.Min, e.Max, e.Sum)
	_, _ = fmt.Fprintln(out, "# range, mid point, percentile, count")
	sep := ">="
	for i, b := range e.Data {
		if i > 0 {
			sep = ">" // last interval is inclusive (of max value)
		}
		_, _ = fmt.Fprintf(out, "%s %.6g <= %.6g , %.6g , %.2f, %d\n", sep, b.Start, b.End, (b.Start+b.End)/2., b.Percent, b.Count)
	}

	// print the information of target percentiles
	for _, p := range e.Percentiles {
		_, _ = fmt.Fprintf(out, "# target %g%% %.6g\n", p.Percentile, p.Value) // nolint: gas
	}
}

// Print dumps the histogram (and counter) to the provided writer.
// Also calculates the percentiles. Use Export() once and Print if you
// are going to need the Export results too.
func (h *Histogram) Print(out io.Writer, msg string, percentiles []float64) {
	h.Export().CalcPercentiles(percentiles).Print(out, msg)
}

// Log Logs the histogram to the counter.
func (h *Histogram) Log(msg string, percentiles []float64) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	h.Print(w, msg, percentiles)
	w.Flush() // nolint: gas,errcheck
	log.Infof("%s", b.Bytes())
}

// Reset clears the data. Reset it to NewHistogram state.
func (h *Histogram) Reset() {
	h.Counter.Reset()
	// Leave Offset and Divider alone
	for i := 0; i < len(h.Hdata); i++ {
		h.Hdata[i] = 0
	}
}

// Clone returns a copy of the histogram.
func (h *Histogram) Clone() *Histogram {
	copy := NewHistogram(h.Offset, h.Divider)
	copy.CopyFrom(h)
	return copy
}

// CopyFrom sets the content of this object to a copy of the src.
func (h *Histogram) CopyFrom(src *Histogram) {
	h.Counter = src.Counter
	h.copyHDataFrom(src)
}

// copyHDataFrom appends histogram data values to this object from the src.
// Src histogram data values will be appended according to this object's
// offset and divider
func (h *Histogram) copyHDataFrom(src *Histogram) {
	if h.Divider == src.Divider && h.Offset == src.Offset {
		for i := 0; i < len(h.Hdata); i++ {
			h.Hdata[i] += src.Hdata[i]
		}
		return
	}

	hData := src.Export()
	for _, data := range hData.Data {
		h.record((data.Start+data.End)/2, int(data.Count))
	}
}

// Merge two different histogram with different scale parameters
// Lowest offset and highest divider value will be selected on new Histogram as scale parameters
func Merge(h1 *Histogram, h2 *Histogram) *Histogram {
	divider := h1.Divider
	offset := h1.Offset
	if h2.Divider > h1.Divider {
		divider = h2.Divider
	}
	if h2.Offset < h1.Offset {
		offset = h2.Offset
	}
	newH := NewHistogram(offset, divider)
	newH.Transfer(h1)
	newH.Transfer(h2)
	return newH
}

// Transfer merges the data from src into this Histogram and clears src.
func (h *Histogram) Transfer(src *Histogram) {
	if src.Count == 0 {
		return
	}
	if h.Count == 0 {
		h.CopyFrom(src)
		src.Reset()
		return
	}
	h.copyHDataFrom(src)
	h.Counter.Transfer(&src.Counter)
	src.Reset()
}

// ParsePercentiles extracts the percentiles from string (flag).
func ParsePercentiles(percentiles string) ([]float64, error) {
	percs := strings.Split(percentiles, ",") // will make a size 1 array for empty input!
	res := make([]float64, 0, len(percs))
	for _, pStr := range percs {
		pStr = strings.TrimSpace(pStr)
		if len(pStr) == 0 {
			continue
		}
		p, err := strconv.ParseFloat(pStr, 64)
		if err != nil {
			return res, err
		}
		res = append(res, p)
	}
	if len(res) == 0 {
		return res, errors.New("list can't be empty")
	}
	log.LogVf("Will use %v for percentiles", res)
	return res, nil
}

// RoundToDigits rounds the input to digits number of digits after decimal point.
// Note this incorrectly rounds the last digit of negative numbers.
func RoundToDigits(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Floor(v*p+0.5) / p
}

// Round rounds to 4 digits after the decimal point.
func Round(v float64) float64 {
	return RoundToDigits(v, 4)
}
