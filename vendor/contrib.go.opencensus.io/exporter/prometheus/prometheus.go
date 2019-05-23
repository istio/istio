// Copyright 2017, OpenCensus Authors
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

// Package prometheus contains a Prometheus exporter that supports exporting
// OpenCensus views as Prometheus metrics.
package prometheus // import "contrib.go.opencensus.io/exporter/prometheus"

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opencensus.io/metric/metricdata"
	"go.opencensus.io/metric/metricexport"
	"go.opencensus.io/stats/view"
)

// Exporter exports stats to Prometheus, users need
// to register the exporter as an http.Handler to be
// able to export.
type Exporter struct {
	opts    Options
	g       prometheus.Gatherer
	c       *collector
	handler http.Handler
}

// Options contains options for configuring the exporter.
type Options struct {
	Namespace   string
	Registry    *prometheus.Registry
	OnError     func(err error)
	ConstLabels prometheus.Labels // ConstLabels will be set as labels on all views.
}

// NewExporter returns an exporter that exports stats to Prometheus.
func NewExporter(o Options) (*Exporter, error) {
	if o.Registry == nil {
		o.Registry = prometheus.NewRegistry()
	}
	collector := newCollector(o, o.Registry)
	e := &Exporter{
		opts:    o,
		g:       o.Registry,
		c:       collector,
		handler: promhttp.HandlerFor(o.Registry, promhttp.HandlerOpts{}),
	}
	collector.ensureRegisteredOnce()

	return e, nil
}

var _ http.Handler = (*Exporter)(nil)

// ensureRegisteredOnce invokes reg.Register on the collector itself
// exactly once to ensure that we don't get errors such as
//  cannot register the collector: descriptor Desc{fqName: *}
//  already exists with the same fully-qualified name and const label values
// which is documented by Prometheus at
//  https://github.com/prometheus/client_golang/blob/fcc130e101e76c5d303513d0e28f4b6d732845c7/prometheus/registry.go#L89-L101
func (c *collector) ensureRegisteredOnce() {
	c.registerOnce.Do(func() {
		if err := c.reg.Register(c); err != nil {
			c.opts.onError(fmt.Errorf("cannot register the collector: %v", err))
		}
	})

}

func (o *Options) onError(err error) {
	if o.OnError != nil {
		o.OnError(err)
	} else {
		log.Printf("Failed to export to Prometheus: %v", err)
	}
}

// ExportView exports to the Prometheus if view data has one or more rows.
// Each OpenCensus AggregationData will be converted to
// corresponding Prometheus Metric: SumData will be converted
// to Untyped Metric, CountData will be a Counter Metric,
// DistributionData will be a Histogram Metric.
// Deprecated in lieu of metricexport.Reader interface.
func (e *Exporter) ExportView(vd *view.Data) {
}

// ServeHTTP serves the Prometheus endpoint.
func (e *Exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.handler.ServeHTTP(w, r)
}

// collector implements prometheus.Collector
type collector struct {
	opts Options
	mu   sync.Mutex // mu guards all the fields.

	registerOnce sync.Once

	// reg helps collector register views dynamically.
	reg *prometheus.Registry

	// reader reads metrics from all registered producers.
	reader *metricexport.Reader
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	de := &descExporter{c: c, descCh: ch}
	c.reader.ReadAndExport(de)
}

// Collect fetches the statistics from OpenCensus
// and delivers them as Prometheus Metrics.
// Collect is invoked every time a prometheus.Gatherer is run
// for example when the HTTP endpoint is invoked by Prometheus.
func (c *collector) Collect(ch chan<- prometheus.Metric) {
	me := &metricExporter{c: c, metricCh: ch}
	c.reader.ReadAndExport(me)
}

func newCollector(opts Options, registrar *prometheus.Registry) *collector {
	return &collector{
		reg:    registrar,
		opts:   opts,
		reader: metricexport.NewReader()}
}

func (c *collector) toDesc(metric *metricdata.Metric) *prometheus.Desc {
	return prometheus.NewDesc(
		metricName(c.opts.Namespace, metric),
		metric.Descriptor.Description,
		toPromLabels(metric.Descriptor.LabelKeys),
		c.opts.ConstLabels)
}

type metricExporter struct {
	c        *collector
	metricCh chan<- prometheus.Metric
}

// ExportMetrics exports to the Prometheus.
// Each OpenCensus Metric will be converted to
// corresponding Prometheus Metric:
// TypeCumulativeInt64 and TypeCumulativeFloat64 will be a Counter Metric,
// TypeCumulativeDistribution will be a Histogram Metric.
// TypeGaugeFloat64 and TypeGaugeInt64 will be a Gauge Metric
func (me *metricExporter) ExportMetrics(ctx context.Context, metrics []*metricdata.Metric) error {
	for _, metric := range metrics {
		desc := me.c.toDesc(metric)
		for _, ts := range metric.TimeSeries {
			tvs := toLabelValues(ts.LabelValues)
			for _, point := range ts.Points {
				metric, err := toPromMetric(desc, metric, point, tvs)
				if err != nil {
					me.c.opts.onError(err)
				} else if metric != nil {
					me.metricCh <- metric
				}
			}
		}
	}
	return nil
}

type descExporter struct {
	c      *collector
	descCh chan<- *prometheus.Desc
}

// ExportMetrics exports descriptor to the Prometheus.
// It is invoked when request to scrape descriptors is received.
func (me *descExporter) ExportMetrics(ctx context.Context, metrics []*metricdata.Metric) error {
	for _, metric := range metrics {
		desc := me.c.toDesc(metric)
		me.descCh <- desc
	}
	return nil
}

func toPromLabels(mls []metricdata.LabelKey) (labels []string) {
	for _, ml := range mls {
		labels = append(labels, sanitize(ml.Key))
	}
	return labels
}

func metricName(namespace string, m *metricdata.Metric) string {
	var name string
	if namespace != "" {
		name = namespace + "_"
	}
	return name + sanitize(m.Descriptor.Name)
}

func toPromMetric(
	desc *prometheus.Desc,
	metric *metricdata.Metric,
	point metricdata.Point,
	labelValues []string) (prometheus.Metric, error) {
	switch metric.Descriptor.Type {
	case metricdata.TypeCumulativeFloat64, metricdata.TypeCumulativeInt64:
		pv, err := toPromValue(point)
		if err != nil {
			return nil, err
		}
		return prometheus.NewConstMetric(desc, prometheus.CounterValue, pv, labelValues...)

	case metricdata.TypeGaugeFloat64, metricdata.TypeGaugeInt64:
		pv, err := toPromValue(point)
		if err != nil {
			return nil, err
		}
		return prometheus.NewConstMetric(desc, prometheus.GaugeValue, pv, labelValues...)

	case metricdata.TypeCumulativeDistribution:
		switch v := point.Value.(type) {
		case *metricdata.Distribution:
			points := make(map[float64]uint64)
			// Histograms are cumulative in Prometheus.
			// Get cumulative bucket counts.
			cumCount := uint64(0)
			for i, b := range v.BucketOptions.Bounds {
				cumCount += uint64(v.Buckets[i].Count)
				points[b] = cumCount
			}
			return prometheus.NewConstHistogram(desc, uint64(v.Count), v.Sum, points, labelValues...)
		default:
			return nil, typeMismatchError(point)
		}
	case metricdata.TypeSummary:
		// TODO: [rghetia] add support for TypeSummary.
		return nil, nil
	default:
		return nil, fmt.Errorf("aggregation %T is not yet supported", metric.Descriptor.Type)
	}
}

func toLabelValues(labelValues []metricdata.LabelValue) (values []string) {
	for _, lv := range labelValues {
		if lv.Present {
			values = append(values, lv.Value)
		} else {
			values = append(values, "")
		}
	}
	return values
}

func typeMismatchError(point metricdata.Point) error {
	return fmt.Errorf("point type %T does not match metric type", point)

}

func toPromValue(point metricdata.Point) (float64, error) {
	switch v := point.Value.(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	default:
		return 0.0, typeMismatchError(point)
	}
}
