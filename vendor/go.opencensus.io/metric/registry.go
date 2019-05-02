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
	"sync"
	"time"

	"go.opencensus.io/metric/metricdata"
)

// Registry creates and manages a set of gauges.
// External synchronization is required if you want to add gauges to the same
// registry from multiple goroutines.
type Registry struct {
	gauges sync.Map
}

type gaugeType int

const (
	gaugeInt64 gaugeType = iota
	gaugeFloat64
	derivedGaugeInt64
	derivedGaugeFloat64
)

//TODO: [rghetia] add constant labels.
type metricOptions struct {
	unit      metricdata.Unit
	labelkeys []string
	desc      string
}

// Options apply changes to metricOptions.
type Options func(*metricOptions)

// WithDescription applies provided description.
func WithDescription(desc string) Options {
	return func(mo *metricOptions) {
		mo.desc = desc
	}
}

// WithUnit applies provided unit.
func WithUnit(unit metricdata.Unit) Options {
	return func(mo *metricOptions) {
		mo.unit = unit
	}
}

// WithLabelKeys applies provided label.
func WithLabelKeys(labelKeys ...string) Options {
	return func(mo *metricOptions) {
		mo.labelkeys = labelKeys
	}
}

// NewRegistry initializes a new Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// AddFloat64Gauge creates and adds a new float64-valued gauge to this registry.
func (r *Registry) AddFloat64Gauge(name string, mos ...Options) (*Float64Gauge, error) {
	f := &Float64Gauge{
		g: gauge{
			gType: gaugeFloat64,
		},
	}
	_, err := r.initGauge(&f.g, name, mos...)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// AddInt64Gauge creates and adds a new int64-valued gauge to this registry.
func (r *Registry) AddInt64Gauge(name string, mos ...Options) (*Int64Gauge, error) {
	i := &Int64Gauge{
		g: gauge{
			gType: gaugeInt64,
		},
	}
	_, err := r.initGauge(&i.g, name, mos...)
	if err != nil {
		return nil, err
	}
	return i, nil
}

// AddInt64DerivedGauge creates and adds a new derived int64-valued gauge to this registry.
// A derived gauge is convenient form of gauge where the object associated with the gauge
// provides its value by implementing func() int64.
func (r *Registry) AddInt64DerivedGauge(name string, mos ...Options) (*Int64DerivedGauge, error) {
	i := &Int64DerivedGauge{
		g: gauge{
			gType: derivedGaugeInt64,
		},
	}
	_, err := r.initGauge(&i.g, name, mos...)
	if err != nil {
		return nil, err
	}
	return i, nil
}

// AddFloat64DerivedGauge creates and adds a new derived float64-valued gauge to this registry.
// A derived gauge is convenient form of gauge where the object associated with the gauge
// provides its value by implementing func() float64.
func (r *Registry) AddFloat64DerivedGauge(name string, mos ...Options) (*Float64DerivedGauge, error) {
	f := &Float64DerivedGauge{
		g: gauge{
			gType: derivedGaugeFloat64,
		},
	}
	_, err := r.initGauge(&f.g, name, mos...)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func gTypeToMetricType(g *gauge) metricdata.Type {
	switch g.gType {
	case derivedGaugeFloat64:
		return metricdata.TypeGaugeFloat64
	case derivedGaugeInt64:
		return metricdata.TypeGaugeInt64
	case gaugeFloat64:
		return metricdata.TypeGaugeFloat64
	case gaugeInt64:
		return metricdata.TypeGaugeInt64
	default:
		panic("unsupported gauge type")
	}
}

func createMetricOption(mos ...Options) *metricOptions {
	o := &metricOptions{}
	for _, mo := range mos {
		mo(o)
	}
	return o
}

func (r *Registry) initGauge(g *gauge, name string, mos ...Options) (*gauge, error) {
	val, ok := r.gauges.Load(name)
	if ok {
		existing := val.(*gauge)
		if existing.gType != g.gType {
			return nil, errGaugeExistsWithDiffType
		}
	}
	g.start = time.Now()
	o := createMetricOption(mos...)
	g.keys = o.labelkeys
	g.desc = metricdata.Descriptor{
		Name:        name,
		Description: o.desc,
		Unit:        o.unit,
		LabelKeys:   o.labelkeys,
		Type:        gTypeToMetricType(g),
	}
	r.gauges.Store(name, g)
	return g, nil
}

// Read reads all gauges in this registry and returns their values as metrics.
func (r *Registry) Read() []*metricdata.Metric {
	ms := []*metricdata.Metric{}
	r.gauges.Range(func(k, v interface{}) bool {
		g := v.(*gauge)
		ms = append(ms, g.read())
		return true
	})
	return ms
}
