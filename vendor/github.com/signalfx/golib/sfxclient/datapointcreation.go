package sfxclient

import (
	"time"

	"github.com/signalfx/golib/datapoint"
	"sync/atomic"
)

// Gauge creates a SignalFx gauge for integer values.
func Gauge(metricName string, dimensions map[string]string, val int64) *datapoint.Datapoint {
	return datapoint.New(metricName, dimensions, datapoint.NewIntValue(val), datapoint.Gauge, time.Time{})
}

// GaugeF creates a SignalFx gauge for floating point values.
func GaugeF(metricName string, dimensions map[string]string, val float64) *datapoint.Datapoint {
	return datapoint.New(metricName, dimensions, datapoint.NewFloatValue(val), datapoint.Gauge, time.Time{})
}

// Cumulative creates a SignalFx cumulative counter for integer values.
func Cumulative(metricName string, dimensions map[string]string, val int64) *datapoint.Datapoint {
	return datapoint.New(metricName, dimensions, datapoint.NewIntValue(val), datapoint.Counter, time.Time{})
}

// CumulativeF creates a SignalFx cumulative counter for float values.
func CumulativeF(metricName string, dimensions map[string]string, val float64) *datapoint.Datapoint {
	return datapoint.New(metricName, dimensions, datapoint.NewFloatValue(val), datapoint.Counter, time.Time{})
}

// CumulativeP creates a SignalFx cumulative counter for integer values from a pointer that is loaded atomically.
func CumulativeP(metricName string, dimensions map[string]string, val *int64) *datapoint.Datapoint {
	return datapoint.New(metricName, dimensions, datapoint.NewIntValue(atomic.LoadInt64(val)), datapoint.Counter, time.Time{})
}

// Counter creates a SignalFx counter for integer values, incrementing by a set value.  Generally,
// it is preferable to use Cumulative Counters when possible.
func Counter(metricName string, dimensions map[string]string, val int64) *datapoint.Datapoint {
	return datapoint.New(metricName, dimensions, datapoint.NewIntValue(val), datapoint.Count, time.Time{})
}
