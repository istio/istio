package datapoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/signalfx/golib/errors"
)

// Documentation taken from http://metrics20.org/spec/

// MetricType define how to display the Value.  It's more metadata of the series than data about the
// series itself.  See target_type of http://metrics20.org/spec/
type MetricType int

const (
	// Gauge is values at each point in time
	Gauge MetricType = iota
	// Count is a number per a given interval (such as a statsd flushInterval); not very useful
	Count
	// Enum is an added type: Values aren't important relative to each other but are just important as distinct
	//             items in a set.  Usually used when Value is type "string"
	Enum
	// Counter is a number that keeps increasing over time (but might wrap/reset at some points)
	// (no statsd counterpart), i.e. a gauge with the added notion of "i usually want to derive this"
	Counter
	// Rate is a number per second
	Rate
	// Timestamp value represents a unix timestamp
	Timestamp
)

// A Datapoint is the metric that is saved.  Designed around http://metrics20.org/spec/
type Datapoint struct {
	// What is being measured.  We think metric, rather than "unit" of metrics20, should be the
	// required identity of a datapoint and the "unit" should be a property of the Value itself
	Metric string `json:"metric"`
	// Dimensions of what is being measured.  They are intrinsic.  Contributes to the identity of
	// the metric. If this changes, we get a new metric identifier
	Dimensions map[string]string `json:"dimensions"`
	// Meta is information that's not particularly important to the datapoint, but may be important
	// to the pipeline that uses the datapoint.  They are extrinsic.  It provides additional
	// information about the metric. changes in this set doesn't change the metric identity
	Meta map[interface{}]interface{} `json:"-"`
	// Value of the datapoint
	Value Value `json:"value"`
	// The type of the datapoint series
	MetricType MetricType `json:"metric_type"`
	// The unix time of the datapoint
	Timestamp time.Time `json:"timestamp"`
}

type jsonDatapoint struct {
	Metric     string            `json:"metric"`
	Dimensions map[string]string `json:"dimensions"`
	Value      interface{}       `json:"value"`
	MetricType MetricType        `json:"metric_type"`
	Timestamp  time.Time         `json:"timestamp"`
}

// UnmarshalJSON decodes JSON into a datapoint, creating the correct Value interface types for the
// type of JSON value that was encoded
func (dp *Datapoint) UnmarshalJSON(b []byte) error {
	var m jsonDatapoint
	dec := json.NewDecoder(bytes.NewBuffer(b))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return errors.Annotatef(err, "JSON decoding of byte array %v failed", b)
	}
	switch t := m.Value.(type) {
	case string:
		dp.Value = NewStringValue(t)
	case json.Number:
		if num, e := t.Int64(); e == nil {
			dp.Value = NewIntValue(num)
		} else if num, e := t.Float64(); e == nil {
			dp.Value = NewFloatValue(num)
		}
	}
	dp.Metric = m.Metric
	dp.Dimensions = m.Dimensions
	dp.MetricType = m.MetricType
	dp.Timestamp = m.Timestamp
	return nil
}

func (dp *Datapoint) String() string {
	return fmt.Sprintf("DP[%s\t%s\t%s\t%d\t%s]", dp.Metric, dp.Dimensions, dp.Value, dp.MetricType, dp.Timestamp.String())
}

type metadata int

const (
	tsProperty metadata = iota
)

//SetProperty sets a property to be used when the time series associated with the datapoint is created
func (dp *Datapoint) SetProperty(key string, value interface{}) {
	if dp.Meta[tsProperty] == nil {
		dp.Meta[tsProperty] = make(map[string]interface{}, 1)
	}
	dp.GetProperties()[key] = value
}

//RemoveProperty removes a property from the map of properties to be used when the time series associated with the datapoint is created
func (dp *Datapoint) RemoveProperty(key string) {
	if dp.Meta[tsProperty] != nil {
		delete(dp.GetProperties(), key)
		if len(dp.GetProperties()) == 0 {
			delete(dp.Meta, tsProperty)
		}
	}
}

//GetProperties gets the map of properties to set when creating the time series associated with the datapoint. nil if no properties are set.
func (dp *Datapoint) GetProperties() map[string]interface{} {
	m, ok := dp.Meta[tsProperty].(map[string]interface{})
	if !ok {
		return nil
	}
	return m
}

// New creates a new datapoint with empty meta data
func New(metric string, dimensions map[string]string, value Value, metricType MetricType, timestamp time.Time) *Datapoint {
	return NewWithMeta(metric, dimensions, map[interface{}]interface{}{}, value, metricType, timestamp)
}

// NewWithMeta creates a new datapoint with passed metadata
func NewWithMeta(metric string, dimensions map[string]string, meta map[interface{}]interface{}, value Value, metricType MetricType, timestamp time.Time) *Datapoint {
	return &Datapoint{
		Metric:     metric,
		Dimensions: dimensions,
		Meta:       meta,
		Value:      value,
		MetricType: metricType,
		Timestamp:  timestamp,
	}
}

// AddMaps adds two maps of dimensions and returns a new map of dimensions that is a + b.  Note that
// b takes precedent.  Works with nil or empty a/b maps.  Does not modify either map, but may return
// a or b if the other is empty.
func AddMaps(a, b map[string]string) map[string]string {
	if len(a) == 0 {
		if len(b) == 0 {
			return map[string]string{}
		}
		return b
	}
	if len(b) == 0 {
		return a
	}
	r := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		r[k] = v
	}
	for k, v := range b {
		r[k] = v
	}
	return r
}
