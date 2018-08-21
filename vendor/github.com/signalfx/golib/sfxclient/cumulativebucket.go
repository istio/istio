package sfxclient

import (
	"math"
	"sync/atomic"

	"github.com/signalfx/golib/datapoint"
)

type atomicFloat struct {
	bits uint64
}

func (a *atomicFloat) Get() float64 {
	return math.Float64frombits(atomic.LoadUint64(&a.bits))
}

func (a *atomicFloat) Add(f float64) {
	for {
		oldValue := atomic.LoadUint64(&a.bits)
		newValue := math.Float64bits(math.Float64frombits(oldValue) + f)
		if atomic.CompareAndSwapUint64(&a.bits, oldValue, newValue) {
			break
		}
	}
}

// Result is a cumulated result of items that can be added to a CumulativeBucket at once
type Result struct {
	Count        int64
	Sum          int64
	SumOfSquares float64
}

// Add a single number to the bucket.  This does not use atomic operations and is not thread safe,
// but adding a finished Result into a CumulativeBucket is thread safe.
func (r *Result) Add(val int64) {
	r.Count++
	r.Sum += val
	// Sum of squares tends to roll over pretty easily
	r.SumOfSquares += float64(val) * float64(val)
}

// A CumulativeBucket tracks groups of values, reporting the count/sum/sum of squares
// as a cumulative counter.
type CumulativeBucket struct {
	MetricName string
	Dimensions map[string]string

	count        int64
	sum          int64
	sumOfSquares atomicFloat
}

var _ Collector = &CumulativeBucket{}

// Add an item to the bucket, later reporting the result in the next report cycle.
func (b *CumulativeBucket) Add(val int64) {
	r := &Result{}
	r.Add(val)
	b.MultiAdd(r)
}

// MultiAdd many items into the bucket at once using a Result.  This can be more efficient as it
// involves only a constant number of atomic operations.
func (b *CumulativeBucket) MultiAdd(res *Result) {
	if res.Count == 0 {
		return
	}
	atomic.AddInt64(&b.count, res.Count)
	atomic.AddInt64(&b.sum, res.Sum)
	b.sumOfSquares.Add(res.SumOfSquares)
}

// Datapoints returns the count/sum/sumsquare datapoints, or nil if there is no set metric name
func (b *CumulativeBucket) Datapoints() []*datapoint.Datapoint {
	if b.MetricName == "" {
		return []*datapoint.Datapoint{}
	}
	return []*datapoint.Datapoint{
		CumulativeP(b.MetricName+".count", b.Dimensions, &b.count),
		CumulativeP(b.MetricName+".sum", b.Dimensions, &b.sum),
		CumulativeF(b.MetricName+".sumsquare", b.Dimensions, b.sumOfSquares.Get()),
	}
}
