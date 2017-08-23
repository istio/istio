// Copyright 2017 Istio Authors.
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

// Package prometheus publishes metric values collected by Mixer for
// ingestion by prometheus.
package prometheus

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"istio.io/mixer/adapter/prometheus2/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/template/metric"
)

type (
	newServerFn func(string) server

	builder struct {
		newServer newServerFn
		registry  *prometheus.Registry
	}

	handler struct {
		srv     server
		metrics map[string]prometheus.Collector
		kinds   map[string]config.Params_MetricInfo_Kind
	}
)

var (
	charReplacer = strings.NewReplacer("/", "_", ".", "_", " ", "_")

	_ metric.HandlerBuilder = &builder{}
	_ metric.Handler        = &handler{}
)

// GetBuilderInfo returns the BuilderInfo associated with this adapter implementation.
func GetBuilderInfo() adapter.BuilderInfo {
	return adapter.BuilderInfo{
		Name:        "prometheus",
		Description: "Publishes prometheus metrics",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return &builder{newServer, prometheus.NewPedanticRegistry()} },
		DefaultConfig:        &config.Params{},
		ValidateConfig:       func(msg adapter.Config) *adapter.ConfigErrors { return nil },
	}
}

func (b *builder) ConfigureMetricHandler(map[string]*metric.Type) error { return nil }

func (b *builder) Build(c adapter.Config, env adapter.Env) (adapter.Handler, error) {
	srv := b.newServer(defaultAddr)
	if err := srv.Start(env, promhttp.HandlerFor(b.registry, promhttp.HandlerOpts{})); err != nil {
		return nil, err
	}

	cfg := c.(*config.Params)
	var metricErr *multierror.Error
	metricsMap := make(map[string]prometheus.Collector, len(cfg.Metrics))
	kinds := make(map[string]config.Params_MetricInfo_Kind, len(cfg.Metrics))
	for _, m := range cfg.Metrics {
		switch m.Kind {
		case config.GAUGE:
			c, err := registerOrGet(b.registry, newGaugeVec(m.Name, m.Description, m.LabelNames))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			metricsMap[m.Name] = c
			kinds[m.Name] = config.GAUGE
		case config.COUNTER:
			c, err := registerOrGet(b.registry, newCounterVec(m.Name, m.Description, m.LabelNames))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			metricsMap[m.Name] = c
			kinds[m.Name] = config.COUNTER
		case config.DISTRIBUTION:
			c, err := registerOrGet(b.registry, newHistogramVec(m.Name, m.Description, m.LabelNames, m.Buckets))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			metricsMap[m.Name] = c
			kinds[m.Name] = config.DISTRIBUTION
		default:
			metricErr = multierror.Append(metricErr, fmt.Errorf("unknown metric kind (%d); could not register metric", m.Kind))
		}
	}
	return &handler{srv, metricsMap, kinds}, metricErr.ErrorOrNil()
}

func (h *handler) HandleMetric(_ context.Context, vals []*metric.Instance) error {
	var result *multierror.Error

	for _, val := range vals {
		collector, found := h.metrics[val.Name]
		if !found {
			result = multierror.Append(result, fmt.Errorf("could not find metric info from adapter config for %s", val.Name))
			continue
		}
		kind, found := h.kinds[val.Name]
		if !found {
			result = multierror.Append(result, fmt.Errorf("could not find kind for metric %s", val.Name))
			continue
		}
		switch kind {
		case config.GAUGE:
			vec := collector.(*prometheus.GaugeVec)
			amt, err := promValue(val.Value)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Name, err))
				continue
			}
			vec.With(promLabels(val.Dimensions)).Set(amt)
		case config.COUNTER:
			vec := collector.(*prometheus.CounterVec)
			amt, err := promValue(val.Value)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Name, err))
				continue
			}
			vec.With(promLabels(val.Dimensions)).Add(amt)
		case config.DISTRIBUTION:
			vec := collector.(*prometheus.HistogramVec)
			amt, err := promValue(val.Value)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Name, err))
				continue
			}
			vec.With(promLabels(val.Dimensions)).Observe(amt)
		}
	}

	return result.ErrorOrNil()
}

func (h *handler) Close() error { return h.srv.Close() }

func newCounterVec(name, desc string, labels []string) *prometheus.CounterVec {
	if desc == "" {
		desc = name
	}
	c := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: safeName(name),
			Help: desc,
		},
		labelNames(labels),
	)
	return c
}

func newGaugeVec(name, desc string, labels []string) *prometheus.GaugeVec {
	if desc == "" {
		desc = name
	}
	c := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: safeName(name),
			Help: desc,
		},
		labelNames(labels),
	)
	return c
}

func newHistogramVec(name, desc string, labels []string, bucketDef adapter.BucketDefinition) *prometheus.HistogramVec {
	if desc == "" {
		desc = name
	}
	c := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    safeName(name),
			Help:    desc,
			Buckets: buckets(bucketDef),
		},
		labelNames(labels),
	)
	return c
}

func buckets(def adapter.BucketDefinition) []float64 {
	switch def.(type) {
	case *adapter.ExplicitBuckets:
		b := def.(*adapter.ExplicitBuckets)
		return b.Bounds
	case *adapter.LinearBuckets:
		lb := def.(*adapter.LinearBuckets)
		return prometheus.LinearBuckets(lb.Offset, lb.Width, int(lb.Count+1))
	case *adapter.ExponentialBuckets:
		eb := def.(*adapter.ExponentialBuckets)
		return prometheus.ExponentialBuckets(eb.Scale, eb.GrowthFactor, int(eb.Count+1))
	default:
		return prometheus.DefBuckets
	}
}

func labelNames(m []string) []string {
	out := make([]string, len(m))
	for i, s := range m {
		out[i] = safeName(s)
	}
	return out
}

// borrowed from prometheus.RegisterOrGet. However, that method is
// targeted for removal soon(tm). So, we duplicate that functionality here
// to maintain it long-term, as we have a use case for the convenience.
func registerOrGet(registry *prometheus.Registry, c prometheus.Collector) (prometheus.Collector, error) {
	if err := registry.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			return are.ExistingCollector, nil
		}
		return nil, err
	}
	return c, nil
}

func safeName(n string) string {
	s := strings.TrimPrefix(n, "/")
	return charReplacer.Replace(s)
}

func promValue(val interface{}) (float64, error) {
	switch i := val.(type) {
	case float64:
		return i, nil
	case int64:
		return float64(i), nil
	case time.Duration:
		// TODO: what is the right thing here?
		// use seconds for now, as we get fractional values...
		return i.Seconds(), nil
	case string:
		f, err := strconv.ParseFloat(i, 64)
		if err != nil {
			return math.NaN(), err
		}
		return f, err
	default:
		return math.NaN(), fmt.Errorf("could not extract numeric value for %v", val)
	}
}

func promLabels(l map[string]interface{}) prometheus.Labels {
	labels := make(prometheus.Labels, len(l))
	for i, label := range l {
		labels[i] = fmt.Sprintf("%v", label)
	}
	return labels
}
