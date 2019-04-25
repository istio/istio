// Copyright 2018, OpenCensus Authors
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

package metric

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"go.opencensus.io/internal/tagencoding"
	"go.opencensus.io/metric/metricdata"
)

// gauge represents a quantity that can go up an down, for example queue depth
// or number of outstanding requests.
//
// gauge maintains a value for each combination of of label values passed to
// the Set or Add methods.
//
// gauge should not be used directly, use Float64Gauge or Int64Gauge.
type gauge struct {
	vals  sync.Map
	desc  metricdata.Descriptor
	start time.Time
	keys  []string
	gType gaugeType
}

type gaugeEntry interface {
	read(t time.Time) metricdata.Point
}

// Read returns the current values of the gauge as a metric for export.
func (g *gauge) read() *metricdata.Metric {
	now := time.Now()
	m := &metricdata.Metric{
		Descriptor: g.desc,
	}
	g.vals.Range(func(k, v interface{}) bool {
		entry := v.(gaugeEntry)
		key := k.(string)
		labelVals := g.labelValues(key)
		m.TimeSeries = append(m.TimeSeries, &metricdata.TimeSeries{
			StartTime:   now, // Gauge value is instantaneous.
			LabelValues: labelVals,
			Points: []metricdata.Point{
				entry.read(now),
			},
		})
		return true
	})
	return m
}

func (g *gauge) mapKey(labelVals []metricdata.LabelValue) string {
	vb := &tagencoding.Values{}
	for _, v := range labelVals {
		b := make([]byte, 1, len(v.Value)+1)
		if v.Present {
			b[0] = 1
			b = append(b, []byte(v.Value)...)
		}
		vb.WriteValue(b)
	}
	return string(vb.Bytes())
}

func (g *gauge) labelValues(s string) []metricdata.LabelValue {
	vals := make([]metricdata.LabelValue, 0, len(g.keys))
	vb := &tagencoding.Values{Buffer: []byte(s)}
	for range g.keys {
		v := vb.ReadValue()
		if v[0] == 0 {
			vals = append(vals, metricdata.LabelValue{})
		} else {
			vals = append(vals, metricdata.NewLabelValue(string(v[1:])))
		}
	}
	return vals
}

func (g *gauge) entryForValues(labelVals []metricdata.LabelValue, newEntry func() gaugeEntry) (interface{}, error) {
	if len(labelVals) != len(g.keys) {
		return nil, errKeyValueMismatch
	}
	mapKey := g.mapKey(labelVals)
	if entry, ok := g.vals.Load(mapKey); ok {
		return entry, nil
	}
	entry, _ := g.vals.LoadOrStore(mapKey, newEntry())
	return entry, nil
}

func (g *gauge) upsertEntry(labelVals []metricdata.LabelValue, newEntry func() gaugeEntry) error {
	if len(labelVals) != len(g.keys) {
		return errKeyValueMismatch
	}
	mapKey := g.mapKey(labelVals)
	g.vals.Delete(mapKey)
	g.vals.Store(mapKey, newEntry())
	return nil
}

// Float64Gauge represents a float64 value that can go up and down.
//
// Float64Gauge maintains a float64 value for each combination of of label values
// passed to the Set or Add methods.
type Float64Gauge struct {
	g gauge
}

// Float64Entry represents a single value of the gauge corresponding to a set
// of label values.
type Float64Entry struct {
	val uint64 // needs to be uint64 for atomic access, interpret with math.Float64frombits
}

func (e *Float64Entry) read(t time.Time) metricdata.Point {
	v := math.Float64frombits(atomic.LoadUint64(&e.val))
	if v < 0 {
		v = 0
	}
	return metricdata.NewFloat64Point(t, v)
}

// GetEntry returns a gauge entry where each key for this gauge has the value
// given.
//
// The number of label values supplied must be exactly the same as the number
// of keys supplied when this gauge was created.
func (g *Float64Gauge) GetEntry(labelVals ...metricdata.LabelValue) (*Float64Entry, error) {
	entry, err := g.g.entryForValues(labelVals, func() gaugeEntry {
		return &Float64Entry{}
	})
	if err != nil {
		return nil, err
	}
	return entry.(*Float64Entry), nil
}

// Set sets the gauge entry value to val.
func (e *Float64Entry) Set(val float64) {
	atomic.StoreUint64(&e.val, math.Float64bits(val))
}

// Add increments the gauge entry value by val.
func (e *Float64Entry) Add(val float64) {
	var swapped bool
	for !swapped {
		oldVal := atomic.LoadUint64(&e.val)
		newVal := math.Float64bits(math.Float64frombits(oldVal) + val)
		swapped = atomic.CompareAndSwapUint64(&e.val, oldVal, newVal)
	}
}

// Int64Gauge represents a int64 gauge value that can go up and down.
//
// Int64Gauge maintains an int64 value for each combination of label values passed to the
// Set or Add methods.
type Int64Gauge struct {
	g gauge
}

// Int64GaugeEntry represents a single value of the gauge corresponding to a set
// of label values.
type Int64GaugeEntry struct {
	val int64
}

func (e *Int64GaugeEntry) read(t time.Time) metricdata.Point {
	v := atomic.LoadInt64(&e.val)
	if v < 0 {
		v = 0.0
	}
	return metricdata.NewInt64Point(t, v)
}

// GetEntry returns a gauge entry where each key for this gauge has the value
// given.
//
// The number of label values supplied must be exactly the same as the number
// of keys supplied when this gauge was created.
func (g *Int64Gauge) GetEntry(labelVals ...metricdata.LabelValue) (*Int64GaugeEntry, error) {
	entry, err := g.g.entryForValues(labelVals, func() gaugeEntry {
		return &Int64GaugeEntry{}
	})
	if err != nil {
		return nil, err
	}
	return entry.(*Int64GaugeEntry), nil
}

// Set sets the value of the gauge entry to the provided value.
func (e *Int64GaugeEntry) Set(val int64) {
	atomic.StoreInt64(&e.val, val)
}

// Add increments the current gauge entry value by val, which may be negative.
func (e *Int64GaugeEntry) Add(val int64) {
	atomic.AddInt64(&e.val, val)
}

// Int64DerivedGauge represents int64 gauge value that is derived from an object.
//
// Int64DerivedGauge maintains objects for each combination of label values.
// These objects implement Int64DerivedGaugeInterface to read instantaneous value
// representing the object.
type Int64DerivedGauge struct {
	g gauge
}

type int64DerivedGaugeEntry struct {
	fn func() int64
}

func (e *int64DerivedGaugeEntry) read(t time.Time) metricdata.Point {
	return metricdata.NewInt64Point(t, e.fn())
}

// UpsertEntry inserts or updates a derived gauge entry for the given set of label values.
// The object for which this gauge entry is inserted or updated, must implement func() int64
//
// It returns an error if
// 1. The number of label values supplied are not the same as the number
// of keys supplied when this gauge was created.
// 2. fn func() int64 is nil.
func (g *Int64DerivedGauge) UpsertEntry(fn func() int64, labelVals ...metricdata.LabelValue) error {
	if fn == nil {
		return errInvalidParam
	}
	return g.g.upsertEntry(labelVals, func() gaugeEntry {
		return &int64DerivedGaugeEntry{fn}
	})
}

// Float64DerivedGauge represents float64 gauge value that is derived from an object.
//
// Float64DerivedGauge maintains objects for each combination of label values.
// These objects implement Float64DerivedGaugeInterface to read instantaneous value
// representing the object.
type Float64DerivedGauge struct {
	g gauge
}

type float64DerivedGaugeEntry struct {
	fn func() float64
}

func (e *float64DerivedGaugeEntry) read(t time.Time) metricdata.Point {
	return metricdata.NewFloat64Point(t, e.fn())
}

// UpsertEntry inserts or updates a derived gauge entry for the given set of label values.
// The object for which this gauge entry is inserted or updated, must implement func() float64
//
// It returns an error if
// 1. The number of label values supplied are not the same as the number
// of keys supplied when this gauge was created.
// 2. fn func() float64 is nil.
func (g *Float64DerivedGauge) UpsertEntry(fn func() float64, labelVals ...metricdata.LabelValue) error {
	if fn == nil {
		return errInvalidParam
	}
	return g.g.upsertEntry(labelVals, func() gaugeEntry {
		return &float64DerivedGaugeEntry{fn}
	})
}
