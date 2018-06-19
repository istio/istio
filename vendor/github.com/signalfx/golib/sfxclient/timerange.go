package sfxclient

import (
	"github.com/signalfx/golib/datapoint"
	"sync/atomic"
	"time"
)

// TimeCounter counts durations exactly above/below a value.  NsBarrier is expected to be some
// amount of nanoseconds that the barrier exists at
type TimeCounter struct {
	NsBarrier int64
	Above     int64
	Below     int64
}

// Add includes a datapoint in the source, incrementing above or below according to barrier
func (t *TimeCounter) Add(dur time.Duration) {
	if dur.Nanoseconds() <= atomic.LoadInt64(&t.NsBarrier) {
		atomic.AddInt64(&t.Below, 1)
		return
	}
	atomic.AddInt64(&t.Above, 1)
}

// Collector returns a datapoint collector for this time counter that uses the given metric name.
func (t *TimeCounter) Collector(metric string) Collector {
	return CollectorFunc(func() []*datapoint.Datapoint {
		dur := time.Duration(atomic.LoadInt64(&t.NsBarrier)).String()
		return []*datapoint.Datapoint{
			CumulativeP(metric, map[string]string{"range": "above", "time": dur}, &t.Above),
			CumulativeP(metric, map[string]string{"range": "below", "time": dur}, &t.Below),
		}
	})
}
