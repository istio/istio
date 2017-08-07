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
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"istio.io/mixer/adapter/prometheus/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	factory struct {
		adapter.DefaultBuilder

		srv      server
		registry *prometheus.Registry
		once     sync.Once
	}

	prom struct {
		metrics map[string]prometheus.Collector
	}
)

var (
	name = "prometheus"
	desc = "Publishes prometheus metrics"
	conf = &config.Params{}

	charReplacer = strings.NewReplacer("/", "_", ".", "_", " ", "_")
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterMetricsBuilder(newFactory(newServer(defaultAddr)))
}

func newFactory(srv server) *factory {
	return &factory{adapter.NewDefaultBuilder(name, desc, conf), srv, prometheus.NewPedanticRegistry(), sync.Once{}}
}

func (f *factory) Close() error {
	return f.srv.Close()
}

// NewMetricsAspect provides an implementation for adapter.MetricsBuilder.
func (f *factory) NewMetricsAspect(env adapter.Env, _ adapter.Config, metrics map[string]*adapter.MetricDefinition) (adapter.MetricsAspect, error) {
	var serverErr error
	f.once.Do(func() {
		serverErr = f.srv.Start(env, promhttp.HandlerFor(f.registry, promhttp.HandlerOpts{}))
	})
	if serverErr != nil {
		return nil, fmt.Errorf("could not start prometheus server: %v", serverErr)
	}

	var metricErr *multierror.Error

	metricsMap := make(map[string]prometheus.Collector, len(metrics))
	for _, m := range metrics {
		switch m.Kind {
		case adapter.Gauge:
			c, err := registerOrGet(f.registry, newGaugeVec(m.Name, m.Description, m.Labels))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			metricsMap[m.Name] = c
		case adapter.Counter:
			c, err := registerOrGet(f.registry, newCounterVec(m.Name, m.Description, m.Labels))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			metricsMap[m.Name] = c
		case adapter.Distribution:
			c, err := registerOrGet(f.registry, newHistogramVec(m.Name, m.Description, m.Labels, m.Buckets))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			metricsMap[m.Name] = c
		default:
			metricErr = multierror.Append(metricErr, fmt.Errorf("unknown metric kind (%d); could not register metric", m.Kind))
		}
	}

	return &prom{metricsMap}, metricErr.ErrorOrNil()
}

func (p *prom) Record(vals []adapter.Value) error {
	var result *multierror.Error

	for _, val := range vals {
		collector, found := p.metrics[val.Definition.Name]
		if !found {
			result = multierror.Append(result, fmt.Errorf("could not find metric description for %s", val.Definition.Name))
			continue
		}
		switch val.Definition.Kind {
		case adapter.Gauge:
			vec := collector.(*prometheus.GaugeVec)
			amt, err := promValue(val)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Definition.Name, err))
				continue
			}
			vec.With(promLabels(val.Labels)).Set(amt)
		case adapter.Counter:
			vec := collector.(*prometheus.CounterVec)
			amt, err := promValue(val)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Definition.Name, err))
				continue
			}
			vec.With(promLabels(val.Labels)).Add(amt)
		case adapter.Distribution:
			vec := collector.(*prometheus.HistogramVec)
			amt, err := promValue(val)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Definition.Name, err))
				continue
			}
			vec.With(promLabels(val.Labels)).Observe(amt)
		}
	}

	return result.ErrorOrNil()
}

func (*prom) Close() error { return nil }

func newCounterVec(name, desc string, labels map[string]adapter.LabelType) *prometheus.CounterVec {
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

func newGaugeVec(name, desc string, labels map[string]adapter.LabelType) *prometheus.GaugeVec {
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

func newHistogramVec(name, desc string, labels map[string]adapter.LabelType, bucketDef adapter.BucketDefinition) *prometheus.HistogramVec {
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

func labelNames(m map[string]adapter.LabelType) []string {
	i := 0
	keys := make([]string, len(m))
	for k := range m {
		keys[i] = safeName(k)
		i++
	}
	return keys
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

func promValue(val adapter.Value) (float64, error) {
	switch i := val.MetricValue.(type) {
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
		return math.NaN(), fmt.Errorf("could not extract numeric value for metric %s", val.Definition.Name)
	}
}

func promLabels(l map[string]interface{}) prometheus.Labels {
	labels := make(prometheus.Labels, len(l))
	for i, label := range l {
		labels[i] = fmt.Sprintf("%v", label)
	}
	return labels
}
