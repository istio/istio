// Copyright 2019 Istio Authors
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

package monitoring

import (
	"context"
	"fmt"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

type (
	// A Metric collects numerical observations.
	Metric interface {
		// Increment records a value of 1 for the current measure. For Sums,
		// this is equivalent to adding 1 to the current value. For Gauges,
		// this is equivalent to setting the value to 1. For Distributions,
		// this is equivalent to making an observation of value 1.
		Increment()

		// Decrement records a value of -1 for the current measure. For Sums,
		// this is equivalent to subtracting -1 to the current value. For Gauges,
		// this is equivalent to setting the value to -1. For Distributions,
		// this is equivalent to making an observation of value -1.
		Decrement()

		// Name returns the name value of a Metric.
		Name() string

		// Record makes an observation of the provided value for the given measure.
		Record(value float64)

		// With creates a new Metric, with the LabelValues provided. This allows creating
		// a set of pre-dimensioned data for recording purposes. This is primarily used
		// for documentation and convenience. Metrics created with this method do not need
		// to be registered (they share the registration of their parent Metric).
		With(labelValues ...LabelValue) Metric

		// Register configures the Metric for export. It MUST be called before collection
		// of values for the Metric. An error will be returned if registration fails.
		Register() error
	}

	// Options encode changes to the options passed to a Metric at creation time.
	Options func(*options)

	// A Label provides a named dimension for a Metric.
	Label tag.Key

	// A LabelValue represents a Label with a specific value. It is used to record
	// values for a Metric.
	LabelValue tag.Mutator

	options struct {
		unit   Unit
		labels []Label
	}
)

// WithLabels provides configuration options for a new Metric, providing the expected
// dimensions for data collection for that Metric.
func WithLabels(labels ...Label) Options {
	return func(opts *options) {
		opts.labels = labels
	}
}

// WithUnit provides configuration options for a new Metric, providing unit of measure
// information for a new Metric.
func WithUnit(unit Unit) Options {
	return func(opts *options) {
		opts.unit = unit
	}
}

// Value creates a new LabelValue for the Label.
func (l Label) Value(value string) LabelValue {
	return tag.Upsert(tag.Key(l), value)
}

// MustCreateLabel will attempt to create a new Label. If
// creation fails, then this method will panic.
func MustCreateLabel(key string) Label {
	k, err := tag.NewKey(key)
	if err != nil {
		panic(fmt.Errorf("could not create label %q: %v", key, err))
	}
	return Label(k)
}

// MustRegister is a helper function that will ensure that the provided Metrics are
// registered. If a metric fails to register, this method will panic.
func MustRegister(metrics ...Metric) {
	for _, m := range metrics {
		if err := m.Register(); err != nil {
			panic(err)
		}
	}
}

// NewSum creates a new Metric with an aggregation type of Sum (the values will be cumulative).
// That means that data collected by the new Metric will be summed before export.
func NewSum(name, description string, opts ...Options) Metric {
	return newMetric(name, description, view.Sum(), opts...)
}

// NewGauge creates a new Metric with an aggregation type of LastValue. That means that data collected
// by the new Metric will export only the last recorded value.
func NewGauge(name, description string, opts ...Options) Metric {
	return newMetric(name, description, view.LastValue(), opts...)
}

// NewDistribution creates a new Metric with an aggregration type of Distribution. This means that the
// data collected by the Metric will be collected and exported as a histogram, with the specified bounds.
func NewDistribution(name, description string, bounds []float64, opts ...Options) Metric {
	return newMetric(name, description, view.Distribution(bounds...), opts...)
}

func newMetric(name, description string, aggregation *view.Aggregation, opts ...Options) Metric {
	return newFloat64Metric(name, description, aggregation, opts...)
}

type float64Metric struct {
	*stats.Float64Measure

	tags []tag.Mutator
	view *view.View
}

func createOptions(opts ...Options) *options {
	o := &options{unit: None, labels: make([]Label, 0)}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func newFloat64Metric(name, description string, aggregation *view.Aggregation, opts ...Options) *float64Metric {
	o := createOptions(opts...)
	measure := stats.Float64(name, description, string(o.unit))
	tagKeys := make([]tag.Key, 0, len(o.labels))
	for _, l := range o.labels {
		tagKeys = append(tagKeys, tag.Key(l))
	}
	return &float64Metric{
		measure,
		make([]tag.Mutator, 0),
		&view.View{Measure: measure, TagKeys: tagKeys, Aggregation: aggregation},
	}
}

func (f *float64Metric) Increment() {
	f.Record(1)
}

func (f *float64Metric) Decrement() {
	f.Record(-1)
}

func (f *float64Metric) Name() string {
	return f.Float64Measure.Name()
}

func (f *float64Metric) Record(value float64) {
	stats.RecordWithTags(context.Background(), f.tags, f.M(value)) //nolint:errcheck
}

func (f *float64Metric) With(labelValues ...LabelValue) Metric {
	t := make([]tag.Mutator, len(f.tags))
	copy(t, f.tags)
	for _, tagValue := range labelValues {
		t = append(t, tag.Mutator(tagValue))
	}
	return &float64Metric{f.Float64Measure, t, f.view}
}

func (f *float64Metric) Register() error {
	return view.Register(f.view)
}
