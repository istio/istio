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
	"fmt"

	"go.opencensus.io/metric"
	"go.opencensus.io/metric/metricproducer"
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

	// DerivedMetric can be used to supply values that dynamically derive from internal
	// state, but are not updated based on any specific event. Their value will be calculated
	// based on a value func that executes when the metrics are exported.
	//
	// At the moment, only a Gauge type is supported.
	DerivedMetric interface {
		// Name returns the name value of a DerivedMetric.
		Name() string

		// Register handles any required setup to ensure metric export.
		Register() error

		// ValueFrom is used to update the derived value with the provided
		// function and the associated label values. If the metric is unlabeled,
		// ValueFrom may be called without any labelValues. Otherwise, the labelValues
		// supplied MUST match the label keys supplied at creation time both in number
		// and in order.
		ValueFrom(valueFn func() float64, labelValues ...string)
	}

	disabledMetric struct {
		name string
	}

	// Options encode changes to the options passed to a Metric at creation time.
	Options func(*options)

	// DerivedOptions encode changes to the options passed to a DerivedMetric at creation time.
	DerivedOptions func(*derivedOptions)

	// A Label provides a named dimension for a Metric.
	Label tag.Key

	// A LabelValue represents a Label with a specific value. It is used to record
	// values for a Metric.
	LabelValue tag.Mutator

	options struct {
		unit   Unit
		labels []Label
	}

	derivedOptions struct {
		labelKeys []string
		valueFn   func() float64
	}
)

var derivedRegistry = metric.NewRegistry()

func init() {
	// ensures exporters can see any derived metrics
	metricproducer.GlobalManager().AddProducer(derivedRegistry)
}

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

// WithLabelKeys is used to configure the label keys used by a DerivedMetric. This
// option is mutually exclusive with the derived option `WithValueFrom` and will be ignored
// if that option is provided.
func WithLabelKeys(keys ...string) DerivedOptions {
	return func(opts *derivedOptions) {
		opts.labelKeys = keys
	}
}

// WithValueFrom is used to configure the derivation of a DerivedMetric. This option
// is mutually exclusive with the derived option `WithLabelKeys`. It acts as syntactic sugar
// that elides the need to create a DerivedMetric (with no labels) and then call `ValueFrom`.
func WithValueFrom(valueFn func() float64) DerivedOptions {
	return func(opts *derivedOptions) {
		opts.valueFn = valueFn
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

// RegisterIf is a helper function that will ensure that the provided
// Metric is registered if enabled function returns true.
// If a metric fails to register, this method will panic.
// It returns the registered metric or no-op metric based on enabled function.
// NOTE: It is important to use the returned Metric if RegisterIf is used.
func RegisterIf(metric Metric, enabled func() bool) Metric {
	if enabled() {
		if err := metric.Register(); err != nil {
			panic(err)
		}
		return metric
	}
	return &disabledMetric{name: metric.Name()}
}

func createOptions(opts ...Options) *options {
	o := &options{unit: None, labels: make([]Label, 0)}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func createDerivedOptions(opts ...DerivedOptions) *derivedOptions {
	o := &derivedOptions{labelKeys: make([]string, 0)}
	for _, opt := range opts {
		opt(o)
	}
	// if a valueFn is supplied, then no label values can be supplied.
	// to prevent issues, drop the label keys
	if o.valueFn != nil {
		o.labelKeys = []string{}
	}
	return o
}
