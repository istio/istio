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
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	api "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/aggregation"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

var meter = func() api.Meter {
	return otel.GetMeterProvider().Meter("istio")
}

// RegisterPrometheusExporter sets the global metrics handler to the provided Prometheus registerer and gatherer.
// Returned is an HTTP handler that can be used to read metrics from.
func RegisterPrometheusExporter(reg prometheus.Registerer, gatherer prometheus.Gatherer) (http.Handler, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	promOpts := []otelprom.Option{
		otelprom.WithoutScopeInfo(),
		otelprom.WithoutTargetInfo(),
		otelprom.WithoutUnits(),
		otelprom.WithRegisterer(reg),
		otelprom.WithoutCounterSuffixes(),
	}

	prom, err := otelprom.New(promOpts...)
	if err != nil {
		return nil, err
	}

	opts := []metric.Option{metric.WithReader(prom)}
	opts = append(opts, knownMetrics.toHistogramViews()...)
	mp := metric.NewMeterProvider(opts...)
	otel.SetMeterProvider(mp)
	handler := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
	return handler, nil
}

// A Metric collects numerical observations.
type Metric interface {
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

	// RecordInt makes an observation of the provided value for the measure.
	RecordInt(value int64)

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
type DerivedMetric interface {
	// Name returns the name value of a DerivedMetric.
	Name() string

	// Register handles any required setup to ensure metric export.
	Register() error

	// ValueFrom is used to update the derived value with the provided
	// function and the associated label values. If the metric is unlabeled,
	// ValueFrom may be called without any labelValues. Otherwise, the labelValues
	// supplied MUST match the label keys supplied at creation time both in number
	// and in order.
	ValueFrom(valueFn func() float64, labelValues ...LabelValue) DerivedMetric
}

// CreateLabel will attempt to create a new Label.
func CreateLabel(key string) Label {
	return Label{attribute.Key(key)}
}

// A Label provides a named dimension for a Metric.
type Label struct {
	key attribute.Key
}

// Value creates a new LabelValue for the Label.
func (l Label) Value(value string) LabelValue {
	return LabelValue{l.key.String(value)}
}

// A LabelValue represents a Label with a specific value. It is used to record
// values for a Metric.
type LabelValue struct {
	keyValue attribute.KeyValue
}

func (l LabelValue) Key() Label {
	return Label{l.keyValue.Key}
}

func (l LabelValue) Value() string {
	return l.keyValue.Value.AsString()
}

// RecordHook has a callback function which a measure is recorded.
type RecordHook interface {
	OnRecord(name string, tags []LabelValue, value float64)
}

var (
	recordHooks     = map[string]RecordHook{}
	recordHookMutex sync.RWMutex
)

// RegisterRecordHook adds a RecordHook for a given measure.
func RegisterRecordHook(name string, h RecordHook) {
	recordHookMutex.Lock()
	defer recordHookMutex.Unlock()
	recordHooks[name] = h
}

// NewSum creates a new Sum Metric (the values will be cumulative).
// That means that data collected by the new Metric will be summed before export.
func NewSum(name, description string, opts ...Options) Metric {
	knownMetrics.register(MetricDefinition{
		Name:        name,
		Type:        "Sum",
		Description: description,
	})
	o, dm := createOptions(name, description, opts...)
	if dm != nil {
		return dm
	}
	return newCounter(o)
}

// NewGauge creates a new Gauge Metric. That means that data collected by the new
// Metric will export only the last recorded value.
func NewGauge(name, description string, opts ...Options) Metric {
	knownMetrics.register(MetricDefinition{
		Name:        name,
		Type:        "LastValue",
		Description: description,
	})
	o, dm := createOptions(name, description, opts...)
	if dm != nil {
		return dm
	}
	return newGauge(o)
}

// NewDerivedGauge creates a new Gauge Metric. That means that data collected by the new
// Metric will export only the last recorded value.
// Unlike NewGauge, the DerivedGauge accepts functions which are called to get the current value.
func NewDerivedGauge(name, description string) DerivedMetric {
	knownMetrics.register(MetricDefinition{
		Name:        name,
		Type:        "LastValue",
		Description: description,
	})
	return newDerivedGauge(name, description)
}

// NewDistribution creates a new Metric with an aggregation type of Distribution. This means that the
// data collected by the Metric will be collected and exported as a histogram, with the specified bounds.
func NewDistribution(name, description string, bounds []float64, opts ...Options) Metric {
	knownMetrics.register(MetricDefinition{
		Name:        name,
		Type:        "Distribution",
		Description: description,
		Bounds:      bounds,
	})
	o, dm := createOptions(name, description, opts...)
	if dm != nil {
		return dm
	}
	return newDistribution(o)
}

// MetricDefinition records a metric's metadata.
// This is used to work around two limitations of OpenTelemetry:
//   - (https://github.com/open-telemetry/opentelemetry-go/issues/4003) Histogram buckets cannot be defined per instrument.
//     instead, we record all metric definitions and add them as Views at registration time.
//   - Support pkg/collateral, which wants to query all metrics. This cannot use a simple Collect() call, as this ignores any unused metrics.
type MetricDefinition struct {
	Name        string
	Type        string
	Description string
	Bounds      []float64
}

// metrics stores known metrics
type metrics struct {
	started bool
	mu      sync.Mutex
	known   map[string]MetricDefinition
}

// knownMetrics is a global that stores all registered metrics
var knownMetrics = metrics{
	known: map[string]MetricDefinition{},
}

// ExportMetricDefinitions reports all currently registered metric definitions.
func ExportMetricDefinitions() []MetricDefinition {
	knownMetrics.mu.Lock()
	defer knownMetrics.mu.Unlock()
	return slices.SortFunc(maps.Values(knownMetrics.known), func(a, b MetricDefinition) bool {
		return a.Name < b.Name
	})
}

// register records a newly defined metric. Only valid before an exporter is set.
func (d *metrics) register(def MetricDefinition) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		log.Fatalf("Attempting to initialize metric %q after metrics have started", def.Name)
	}
	d.known[def.Name] = def
}

// toHistogramViews works around https://github.com/open-telemetry/opentelemetry-go/issues/4003; in the future we can define
// this when we create the histogram.
func (d *metrics) toHistogramViews() []metric.Option {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.started = true
	opts := []metric.Option{}
	for name, def := range d.known {
		if def.Bounds == nil {
			continue
		}
		// for each histogram metric (i.e. those with bounds), set up a view explicitly defining those buckets.
		v := metric.WithView(metric.NewView(
			metric.Instrument{Name: name},
			metric.Stream{Aggregation: aggregation.ExplicitBucketHistogram{
				Boundaries: def.Bounds,
			}},
		))
		opts = append(opts, v)
	}
	return opts
}
