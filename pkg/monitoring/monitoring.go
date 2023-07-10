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
	"math"
	"net/http"
	"sync"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/metric"
	"go.opencensus.io/metric/metricdata"
	"go.opencensus.io/metric/metricproducer"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"istio.io/istio/pkg/log"
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

	// DerivedMetrics can be used to supply values that dynamically derive from internal
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
	Label struct {
		k tag.Key
	}

	// A LabelValue represents a Label with a specific value. It is used to record
	// values for a Metric.
	LabelValue struct {
		kv tag.Mutator
	}

	options struct {
		unit     Unit
		labels   []Label
		useInt64 bool
	}

	derivedOptions struct {
		labelKeys []string
		valueFn   func() float64
	}

	// RecordHook has a callback function which a measure is recorded.
	RecordHook interface {
		OnRecord(name string, tags LabelSet, value float64)
	}
)

type LabelSet struct {
	m *tag.Map
}

func newLabelSet(tags []tag.Mutator) LabelSet {
	originalCtx, err := tag.New(context.Background(), tags...)
	if err != nil {
		log.Warn("fail to initialize original tag context")
		return LabelSet{}
	}
	return LabelSet{tag.FromContext(originalCtx)}
}

func (ls LabelSet) Value(k Label) (string, bool) {
	return ls.m.Value(k.k)
}

// RegisterPrometheusExporter registers the provided prometheus Registry as an exporter.
// The registry will be populated with metrics.
// Generally this is called once per binary near the start of the application.
// The returned http.Handler can be used to serve metrics
func RegisterPrometheusExporter(reg prometheus.Registerer, gatherer prometheus.Gatherer) (http.Handler, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	exporter, err := ocprom.NewExporter(ocprom.Options{
		Registerer: reg,
		Gatherer:   gatherer,
	})
	if err != nil {
		return nil, fmt.Errorf("could not set up prometheus exporter: %v", err)
	}
	view.RegisterExporter(exporter)
	return exporter, nil
}

// Decrement implements Metric
func (dm *disabledMetric) Decrement() {}

// Increment implements Metric
func (dm *disabledMetric) Increment() {}

// Name implements Metric
func (dm *disabledMetric) Name() string {
	return dm.name
}

// Record implements Metric
func (dm *disabledMetric) Record(value float64) {}

// RecordInt implements Metric
func (dm *disabledMetric) RecordInt(value int64) {}

// Register implements Metric
func (dm *disabledMetric) Register() error {
	return nil
}

// With implements Metric
func (dm *disabledMetric) With(labelValues ...LabelValue) Metric {
	return dm
}

var _ Metric = &disabledMetric{}

var (
	recordHooks     map[string]RecordHook
	recordHookMutex sync.RWMutex

	derivedRegistry = metric.NewRegistry()
)

func init() {
	recordHooks = make(map[string]RecordHook)
	// ensures exporters can see any derived metrics
	metricproducer.GlobalManager().AddProducer(derivedRegistry)
}

// RegisterRecordHook adds a RecordHook for a given measure.
func RegisterRecordHook(name string, h RecordHook) {
	recordHookMutex.Lock()
	defer recordHookMutex.Unlock()
	recordHooks[name] = h
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

// WithInt64Values provides configuration options for a new Metric, indicating that
// recorded values will be saved as int64 values. Any float64 values recorded will
// converted to int64s via math.Floor-based conversion.
func WithInt64Values() Options {
	return func(opts *options) {
		opts.useInt64 = true
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
	return LabelValue{tag.Upsert(l.k, value)}
}

// MustCreateLabel will attempt to create a new Label. If
// creation fails, then this method will panic.
func MustCreateLabel(key string) Label {
	k, err := tag.NewKey(key)
	if err != nil {
		panic(fmt.Errorf("could not create label %q: %v", key, err))
	}
	return Label{k}
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

// NewDerivedGauge creates a new Metric with an aggregation type of LastValue that generates the value
// dynamically according to the provided function. This can be used for values based on querying some
// state within a system (when event-driven recording is not appropriate).
func NewDerivedGauge(name, description string, opts ...DerivedOptions) DerivedMetric {
	options := createDerivedOptions(opts...)
	m, err := derivedRegistry.AddFloat64DerivedGauge(name,
		metric.WithDescription(description),
		metric.WithLabelKeys(options.labelKeys...),
		metric.WithUnit(metricdata.UnitDimensionless)) // TODO: allow unit in options
	if err != nil {
		log.Warnf("failed to add metric %q: %v", name, err)
	}
	derived := &derivedFloat64Metric{
		base: m,
		name: name,
	}
	if options.valueFn != nil {
		derived.ValueFrom(options.valueFn)
	}
	return derived
}

// NewDistribution creates a new Metric with an aggregation type of Distribution. This means that the
// data collected by the Metric will be collected and exported as a histogram, with the specified bounds.
func NewDistribution(name, description string, bounds []float64, opts ...Options) Metric {
	return newMetric(name, description, view.Distribution(bounds...), opts...)
}

func newMetric(name, description string, aggregation *view.Aggregation, opts ...Options) Metric {
	o := createOptions(opts...)
	if o.useInt64 {
		return newInt64Metric(name, description, aggregation, o)
	}
	return newFloat64Metric(name, description, aggregation, o)
}

type derivedFloat64Metric struct {
	base *metric.Float64DerivedGauge

	name string
}

func (d *derivedFloat64Metric) Name() string {
	return d.name
}

// no-op
func (d *derivedFloat64Metric) Register() error {
	return nil
}

func (d *derivedFloat64Metric) ValueFrom(valueFn func() float64, labelValues ...string) {
	if len(labelValues) == 0 {
		if err := d.base.UpsertEntry(valueFn); err != nil {
			log.Errorf("failed to add value for derived metric %q: %v", d.name, err)
		}
		return
	}
	lv := make([]metricdata.LabelValue, 0, len(labelValues))
	for _, l := range labelValues {
		lv = append(lv, metricdata.NewLabelValue(l))
	}
	if err := d.base.UpsertEntry(valueFn, lv...); err != nil {
		log.Errorf("failed to add value for derived metric %q: %v", d.name, err)
	}
}

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

func newFloat64Metric(name, description string, aggregation *view.Aggregation, opts *options) *float64Metric {
	measure := stats.Float64(name, description, string(opts.unit))
	tagKeys := make([]tag.Key, 0, len(opts.labels))
	for _, l := range opts.labels {
		tagKeys = append(tagKeys, l.k)
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
	recordHookMutex.RLock()
	if rh, ok := recordHooks[f.Name()]; ok {
		rh.OnRecord(f.Name(), newLabelSet(f.tags), value)
	}
	recordHookMutex.RUnlock()
	m := f.M(value)
	stats.Record(f.ctx, m) //nolint:errcheck
}

func (f *float64Metric) recordMeasurements(m []stats.Measurement) {
	recordHookMutex.RLock()
	if rh, ok := recordHooks[f.Name()]; ok {
		for _, mv := range m {
			rh.OnRecord(f.Name(), newLabelSet(f.tags), mv.Value())
		}
	}
	recordHookMutex.RUnlock()
	stats.Record(f.ctx, m...)
}

func (f *float64Metric) RecordInt(value int64) {
	f.Record(float64(value))
}

func (f *float64Metric) With(labelValues ...LabelValue) Metric {
	t := make([]tag.Mutator, len(f.tags), len(f.tags)+len(labelValues))
	copy(t, f.tags)
	for _, tagValue := range labelValues {
		t = append(t, tagValue.kv)
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

type int64Metric struct {
	*stats.Int64Measure

	// tags stores all tags for the metrics
	tags []tag.Mutator
	// ctx is a precomputed context holding tags, as an optimization
	ctx  context.Context
	view *view.View

	// incrementMeasure is a precomputed +1 measurement to avoid extra allocations in Increment()
	incrementMeasure []stats.Measurement
	// decrementMeasure is a precomputed -1 measurement to avoid extra allocations in Decrement()
	decrementMeasure []stats.Measurement
}

func newInt64Metric(name, description string, aggregation *view.Aggregation, opts *options) *int64Metric {
	measure := stats.Int64(name, description, string(opts.unit))
	tagKeys := make([]tag.Key, 0, len(opts.labels))
	for _, l := range opts.labels {
		tagKeys = append(tagKeys, l.k)
	}
	ctx, _ := tag.New(context.Background()) //nolint:errcheck
	return &int64Metric{
		Int64Measure:     measure,
		tags:             make([]tag.Mutator, 0),
		ctx:              ctx,
		view:             &view.View{Measure: measure, TagKeys: tagKeys, Aggregation: aggregation},
		incrementMeasure: []stats.Measurement{measure.M(1)},
		decrementMeasure: []stats.Measurement{measure.M(-1)},
	}
}

func (i *int64Metric) Increment() {
	i.recordMeasurements(i.incrementMeasure)
}

func (i *int64Metric) Decrement() {
	i.recordMeasurements(i.decrementMeasure)
}

func (i *int64Metric) Name() string {
	return i.Int64Measure.Name()
}

func (i *int64Metric) Record(value float64) {
	i.RecordInt(int64(math.Floor(value)))
}

func (i *int64Metric) recordMeasurements(m []stats.Measurement) {
	stats.Record(i.ctx, m...) //nolint:errcheck
}

func (i *int64Metric) RecordInt(value int64) {
	stats.Record(i.ctx, i.M(value)) //nolint:errcheck
}

func (i *int64Metric) With(labelValues ...LabelValue) Metric {
	t := make([]tag.Mutator, len(i.tags), len(i.tags)+len(labelValues))
	copy(t, i.tags)
	for _, tagValue := range labelValues {
		t = append(t, tagValue.kv)
	}
	ctx, _ := tag.New(context.Background(), t...) //nolint:errcheck
	return &int64Metric{
		Int64Measure:     i.Int64Measure,
		tags:             t,
		ctx:              ctx,
		view:             i.view,
		incrementMeasure: i.incrementMeasure,
		decrementMeasure: i.decrementMeasure,
	}
}

func (i *int64Metric) Register() error {
	return view.Register(i.view)
}
