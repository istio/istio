package sfxclient

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/signalfx/gohistogram"
	"github.com/signalfx/golib/datapoint"
	"github.com/signalfx/golib/timekeeper"
)

// DefaultQuantiles are the default set of percentiles RollingBucket should collect
var DefaultQuantiles = []float64{.25, .5, .9, .99}

// DefaultBucketWidth is the default width that a RollingBucket should flush histogram values
var DefaultBucketWidth = time.Second * 20

// DefaultHistogramSize is the default number of windows RollingBucket uses for created histograms
var DefaultHistogramSize = 80

// DefaultMaxBufferSize is the default number of past bucket Quantile values RollingBucket saves until a Datapoints() call
var DefaultMaxBufferSize = 100

// RollingBucket keeps histogram style metrics over a BucketWidth window of time.  It allows users
// to collect and report percentile metrics like like median or p99, as well as min/max/sum/count
// and sum of square from a set of points.
type RollingBucket struct {
	// MetricName is the metric name used when the RollingBucket is reported to SignalFx
	MetricName string
	// Dimensions are the dimensions used when the RollingBucket is reported to SignalFx
	Dimensions map[string]string
	// Quantiles are an array of values [0 - 1.0] that are the histogram quantiles reported to
	// SignalFx during a Datapoints() call.  For example, [.5] would only report the median.
	Quantiles []float64
	// BucketWidth is how long in time a bucket accumulates values before a flush is forced
	BucketWidth time.Duration
	// Hist is an efficient tracker of numeric values for a histogram
	Hist *gohistogram.NumericHistogram
	// MaxFlushBufferSize is the maximum size of a window to keep for the RollingBucket before
	// quantiles are dropped.  It is ideally close to len(quantiles) * 3 + 15
	MaxFlushBufferSize int
	// Timer is used to track time.Now() during default value add calls
	Timer timekeeper.TimeKeeper

	// Inclusive
	bucketStartTime time.Time
	// Exclusive
	bucketEndTime time.Time

	count        int64
	sum          float64
	sumOfSquares float64
	min          float64
	max          float64

	pointsToFlush []*datapoint.Datapoint
	mu            sync.Mutex
}

var _ Collector = &RollingBucket{}

// NewRollingBucket creates a new RollingBucket using default values for Quantiles, BucketWidth,
// and the histogram tracker.
func NewRollingBucket(metricName string, dimensions map[string]string) *RollingBucket {
	return &RollingBucket{
		MetricName:         metricName,
		Dimensions:         dimensions,
		Quantiles:          DefaultQuantiles,
		BucketWidth:        DefaultBucketWidth,
		Hist:               gohistogram.NewHistogram(DefaultHistogramSize),
		MaxFlushBufferSize: DefaultMaxBufferSize,
		Timer:              &timekeeper.RealTime{},
	}
}

func (r *RollingBucket) flushPoints() []*datapoint.Datapoint {
	if r.Hist.Count() > 0 {
		pointsToFlush := make([]*datapoint.Datapoint, 0, 2+len(r.Quantiles))
		pointsToFlush = append(pointsToFlush,
			GaugeF(r.MetricName+".min", r.Dimensions, r.min),
			GaugeF(r.MetricName+".max", r.Dimensions, r.max),
		)
		for _, q := range r.Quantiles {
			pointsToFlush = append(pointsToFlush, GaugeF(r.MetricName+".p"+percentToString(q), r.Dimensions, r.Hist.Quantile(q)))
		}
		for _, dp := range pointsToFlush {
			dp.Timestamp = r.bucketEndTime
		}
		r.Hist.Reset()
		return pointsToFlush
	}
	return nil
}

// We want numbers that look like the following:
/*
"p90"
"p50"
"p999"
"p75"
"p333"
*/
func percentToString(f float64) string {
	fInt := int64(f * 1000)
	if fInt == 0 {
		return "0"
	}
	if fInt == 1000 {
		return "100"
	}
	if fInt < 0 || fInt > 1000 {
		return "INVALID"
	}
	r := strconv.FormatInt(fInt, 10)
	r = strings.TrimRight(r, "0")
	for len(r) < 2 {
		r = r + "0"
	}
	return r
}

// Datapoints returns basic bucket stats every time and will only the first time called for each window
// return that window's points.  For efficiency sake, Datapoints() will only return histogram window
// values once.  Because of this, it is suggested to always forward datapoints returned by this call
// to SignalFx.
func (r *RollingBucket) Datapoints() []*datapoint.Datapoint {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateTime(r.Timer.Now())
	ret := make([]*datapoint.Datapoint, 0, 3+len(r.Quantiles))
	ret = append(ret,
		// Note: No need for CumulativeP because I'm in a mutex
		Cumulative(r.MetricName+".count", r.Dimensions, r.count),
		CumulativeF(r.MetricName+".sum", r.Dimensions, r.sum),
		CumulativeF(r.MetricName+".sumsquare", r.Dimensions, r.sumOfSquares),
	)
	if r.pointsToFlush == nil {
		return ret
	}

	ret = append(ret, r.pointsToFlush...)
	r.pointsToFlush = nil
	return ret
}

func (r *RollingBucket) updateTime(t time.Time) {
	// Note: The tail of the bucket is exclusive
	if !t.Before(r.bucketEndTime) {
		r.pointsToFlush = append(r.pointsToFlush, r.flushPoints()...)
		if len(r.pointsToFlush) > r.MaxFlushBufferSize {
			r.pointsToFlush = r.pointsToFlush[:r.MaxFlushBufferSize]
		}
		r.bucketStartTime = r.bucketEndTime
		r.bucketEndTime = r.bucketEndTime.Add(r.BucketWidth)
		if !t.Before(r.bucketEndTime) {
			r.bucketStartTime = t
			r.bucketEndTime = r.bucketStartTime.Add(r.BucketWidth)
		}
	}
}

// Add a value to the rolling bucket histogram.  If the current time is already calculated, it may
// be more efficient to call AddAt in order to save another time.Time() call.
func (r *RollingBucket) Add(v float64) {
	r.AddAt(v, r.Timer.Now())
}

// AddAt is like Add but also takes a time to pretend the value comes at.
func (r *RollingBucket) AddAt(v float64, t time.Time) {
	r.mu.Lock()
	r.updateTime(t)

	if r.Hist.Count() == 0 {
		r.min = v
		r.max = v
	} else {
		r.min = math.Min(r.min, v)
		r.max = math.Max(r.max, v)
	}

	r.count++
	r.sum += v
	r.sumOfSquares += v * v
	r.Hist.Add(v)

	r.mu.Unlock()
}
