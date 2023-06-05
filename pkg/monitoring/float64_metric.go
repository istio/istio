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

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var _ Metric = &float64Metric{}

type float64Metric struct {
	*stats.Float64Measure

	// tags stores all tags for the metrics
	tags []tag.Mutator
	// ctx is a precomputed context holding tags, as an optimization
	ctx  context.Context
	view *view.View

	incrementMeasure []stats.Measurement
	decrementMeasure []stats.Measurement
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

// NewDistribution creates a new Metric with an aggregation type of Distribution. This means that the
// data collected by the Metric will be collected and exported as a histogram, with the specified bounds.
func NewDistribution(name, description string, bounds []float64, opts ...Options) Metric {
	return newMetric(name, description, view.Distribution(bounds...), opts...)
}

func newMetric(name, description string, aggregation *view.Aggregation, opts ...Options) Metric {
	o := createOptions(opts...)
	return newFloat64Metric(name, description, aggregation, o)
}

func newFloat64Metric(name, description string, aggregation *view.Aggregation, opts *options) *float64Metric {
	measure := stats.Float64(name, description, string(opts.unit))
	tagKeys := make([]tag.Key, 0, len(opts.labels))
	for _, l := range opts.labels {
		tagKeys = append(tagKeys, tag.Key(l))
	}
	ctx, _ := tag.New(context.Background()) //nolint:errcheck
	return &float64Metric{
		Float64Measure:   measure,
		tags:             make([]tag.Mutator, 0),
		ctx:              ctx,
		view:             &view.View{Measure: measure, TagKeys: tagKeys, Aggregation: aggregation},
		incrementMeasure: []stats.Measurement{measure.M(1)},
		decrementMeasure: []stats.Measurement{measure.M(-1)},
	}
}

func (f *float64Metric) Increment() {
	f.recordMeasurements(f.incrementMeasure)
}

func (f *float64Metric) Decrement() {
	f.recordMeasurements(f.decrementMeasure)
}

func (f *float64Metric) Name() string {
	return f.Float64Measure.Name()
}

func (f *float64Metric) Record(value float64) {
	m := f.M(value)
	stats.Record(f.ctx, m) //nolint:errcheck
}

func (f *float64Metric) recordMeasurements(m []stats.Measurement) {
	stats.Record(f.ctx, m...)
}

func (f *float64Metric) With(labelValues ...LabelValue) Metric {
	t := make([]tag.Mutator, len(f.tags), len(f.tags)+len(labelValues))
	copy(t, f.tags)
	for _, tagValue := range labelValues {
		t = append(t, tag.Mutator(tagValue))
	}
	ctx, _ := tag.New(context.Background(), t...) //nolint:errcheck
	return &float64Metric{
		Float64Measure:   f.Float64Measure,
		tags:             t,
		ctx:              ctx,
		view:             f.view,
		incrementMeasure: f.incrementMeasure,
		decrementMeasure: f.decrementMeasure,
	}
}

func (f *float64Metric) Register() error {
	return view.Register(f.view)
}
