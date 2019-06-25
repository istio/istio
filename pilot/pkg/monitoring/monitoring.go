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
	// to OpenCensus views.
	Metric interface {

		// Increment records a value of 1 for the current measure. For view.Count and view.Sum
		// aggregations, this is equivalent to adding 1 to the current value.
		Increment()

		// Name returns the name value of a Metric
		Name() string

		// Record makes an observation of the provided value for the given measure.
		Record(value float64)

		// WithTags creates a new Metric, with the tag mutations provided. This allows creating
		// a set of pre-dimensioned data for recording and export purposes.
		WithTags(tags ...tag.Mutator) Metric

		// View returns the OpenCensus view structure to which this Metric is being exported.
		View() *view.View
	}

	// MetricOpts provides the basic options for a Metric.
	MetricOpts struct {

		// Name is the canonical name of the metric.
		Name string

		// Description provides a summary of the value being tracked by the Metric. It is used
		// to generate documentation of the metric.
		Description string
	}
)

// MustCreateTagKey will attempt to create a new OpenCensus tag key. If
// creation fails, then this method will panic.
func MustCreateTagKey(key string) tag.Key {
	k, err := tag.NewKey(key)
	if err != nil {
		panic(fmt.Errorf("could not create tag key %q: %v", key, err))
	}
	return k
}

// MustRegisterViews is a helper function that will ensure that the OpenCensus views created
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
func NewSum(opts MetricOpts, tagKeys ...tag.Key) Metric {
	return newMetric(opts, view.Sum(), tagKeys...)
}

// NewGauge creates a new Metric with an aggregation type of LastValue. That means that data collected
// by the new Metric will export only the last recorded value.
func NewGauge(opts MetricOpts, tagKeys ...tag.Key) Metric {
	return newMetric(opts, view.LastValue(), tagKeys...)
}

// NewDistribution creates a new Metric with an aggregration type of Distribution. This means that the
// data collected by the Metric will be collected and exported as a histogram, with the specified bounds.
func NewDistribution(opts MetricOpts, bounds []float64, tagKeys ...tag.Key) Metric {
	return newMetric(opts, view.Distribution(bounds...), tagKeys...)
}

func newMetric(opts MetricOpts, aggregation *view.Aggregation, tagKeys ...tag.Key) Metric {
	return newFloat64Metric(opts, aggregation, tagKeys...)
}

type float64Metric struct {
	*stats.Float64Measure

	tags []tag.Mutator
	view *view.View
}

func newFloat64Metric(opts MetricOpts, aggregation *view.Aggregation, tagKeys ...tag.Key) *float64Metric {
	measure := stats.Float64(opts.Name, opts.Description, stats.UnitDimensionless)
	return &float64Metric{
		measure,
		make([]tag.Mutator, 0, len(tagKeys)),
		&view.View{Measure: measure, TagKeys: tagKeys, Aggregation: aggregation},
	}
}

func (f *float64Metric) Increment() {
	f.Record(1)
}

func (f *float64Metric) Name() string {
	return f.Name()
}

func (f *float64Metric) Record(value float64) {
	stats.RecordWithTags(context.Background(), f.tags, f.M(value)) //nolint:errcheck
}

func (f *float64Metric) WithTags(tags ...tag.Mutator) Metric {
	return &float64Metric{f.Float64Measure, append(f.tags, tags...), f.view}
}

func (f *float64Metric) View() *view.View {
	return f.view
}
