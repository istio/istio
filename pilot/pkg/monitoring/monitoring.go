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
	// Metric collects numerical observations. These observations are aggregated and exported
	// to views.
	Metric interface {
		// Increment records a value of 1 for the current measure. For Sums,
		// this is equivalent to adding 1 to the current value.
		Increment()

		// Name returns the name value of a Metric
		Name() string

		// Record makes an observation of the provided value for the given measure.
		Record(value float64)

		// With creates a new Metric, with the TagValues provided. This allows creating
		// a set of pre-dimensioned data for recording and export purposes.
		With(tagValues ...TagValue) Metric

		// View returns the OpenCensus view to which this Metric is being exported.
		View() *view.View
	}

	// A Tag provides a named dimension for a Metric (also known as a label).
	Tag tag.Key

	// A TagValue represents a Tag with a specific value. It is used to record
	// values for a Metric.
	TagValue tag.Mutator
)

// Value creates a new TagValue for the Tag.
func (t Tag) Value(value string) TagValue {
	return tag.Upsert(tag.Key(t), value)
}

// MustCreateTag will attempt to create a new Tag. If
// creation fails, then this method will panic.
func MustCreateTag(key string) Tag {
	k, err := tag.NewKey(key)
	if err != nil {
		panic(fmt.Errorf("could not create tag key %q: %v", key, err))
	}
	return Tag(k)
}

// MustRegisterViews is a helper function that will ensure that the views created
// for the provided Metrics are registered. If the view for a metric fails to register,
// this method will panic.
func MustRegisterViews(metrics ...Metric) {
	for _, s := range metrics {
		if err := view.Register(s.View()); err != nil {
			panic(err)
		}
	}
}

// NewSum creates a new Metric with an aggregation type of Sum. That means that data collected
// by the new Metric will be summed before export.
func NewSum(name, description string, tags ...Tag) Metric {
	return newMetric(name, description, view.Sum(), tags...)
}

// NewGauge creates a new Metric with an aggregation type of LastValue. That means that data collected
// by the new Metric will export only the last recorded value.
func NewGauge(name, description string, tags ...Tag) Metric {
	return newMetric(name, description, view.LastValue(), tags...)
}

// NewDistribution creates a new Metric with an aggregration type of Distribution. This means that the
// data collected by the Metric will be collected and exported as a histogram, with the specified bounds.
func NewDistribution(name, description string, bounds []float64, tags ...Tag) Metric {
	return newMetric(name, description, view.Distribution(bounds...), tags...)
}

func newMetric(name, description string, aggregation *view.Aggregation, tags ...Tag) Metric {
	return newFloat64Metric(name, description, aggregation, tags...)
}

type float64Metric struct {
	*stats.Float64Measure

	tags []tag.Mutator
	view *view.View
}

func newFloat64Metric(name, description string, aggregation *view.Aggregation, tags ...Tag) *float64Metric {
	measure := stats.Float64(name, description, stats.UnitDimensionless)
	tagKeys := make([]tag.Key, 0, len(tags))
	for _, t := range tags {
		tagKeys = append(tagKeys, tag.Key(t))
	}
	return &float64Metric{
		measure,
		make([]tag.Mutator, 0, len(tags)),
		&view.View{Measure: measure, TagKeys: tagKeys, Aggregation: aggregation},
	}
}

func (f *float64Metric) Increment() {
	f.Record(1)
}

func (f *float64Metric) Name() string {
	return f.Float64Measure.Name()
}

func (f *float64Metric) Record(value float64) {
	stats.RecordWithTags(context.Background(), f.tags, f.M(value)) //nolint:errcheck
}

func (f *float64Metric) With(tagValues ...TagValue) Metric {
	t := make([]tag.Mutator, 0, len(f.tags)+len(tagValues))
	copy(t, f.tags)
	for _, tagValue := range tagValues {
		t = append(t, tag.Mutator(tagValue))
	}
	return &float64Metric{f.Float64Measure, t, f.view}
}

func (f *float64Metric) View() *view.View {
	return f.view
}
