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
	"crypto/sha1"
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
	pkgHndlr "istio.io/mixer/pkg/handler"
	"istio.io/mixer/template/metric"
)

type (
	// cinfo is a collector, its kind and the sha
	// of config that produced the collector.
	// sha is used to confirm a cache hit.
	cinfo struct {
		c    prometheus.Collector
		sha  [sha1.Size]byte
		kind config.Params_MetricInfo_Kind
	}

	builder struct {
		// maps instance_name to collector.
		metrics  map[string]*cinfo
		registry *prometheus.Registry
		srv      server
		cfg      *config.Params
	}

	handler struct {
		srv     server
		metrics map[string]*cinfo
	}
)

var (
	charReplacer = strings.NewReplacer("/", "_", ".", "_", " ", "_", "-", "")

	_ metric.HandlerBuilder = &obuilder{}
	_ metric.Handler        = &handler{}
)

// GetInfo returns the BuilderInfo associated with this adapter.
func GetInfo() pkgHndlr.Info {
	// prometheus uses a singleton http port, so we make the
	// builder itself a singleton, when defaultAddr become configurable
	// srv will be a map[string]server
	singletonBuilder := &builder{
		srv: newServer(defaultAddr),
	}
	singletonBuilder.clearState()
	return pkgHndlr.Info{
		Name:        "prometheus",
		Impl:        "istio.io/mixer/adapter/prometheus",
		Description: "Publishes prometheus metrics",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder: func() adapter.Builder2 { return singletonBuilder },
		// to be deleted
		DefaultConfig:  &config.Params{},
		ValidateConfig: func(msg adapter.Config) *adapter.ConfigErrors { return nil },
		CreateHandlerBuilder: func() adapter.HandlerBuilder {
			return &obuilder{b: singletonBuilder}
		},
	}
}

func (b *builder) clearState() {
	b.registry = prometheus.NewPedanticRegistry()
	b.metrics = make(map[string]*cinfo)
}

func (b *builder) SetMetricTypes(map[string]*metric.Type) {}
func (b *builder) SetAdapterConfig(cfg adapter.Config)    { b.cfg = cfg.(*config.Params) }
func (b *builder) Validate() *adapter.ConfigErrors        { return nil }
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {

	cfg := b.cfg
	var metricErr *multierror.Error

	// newMetrics collects new metric configuration
	newMetrics := make([]*config.Params_MetricInfo, 0, len(cfg.Metrics))

	// Check for metric redefinition.
	// If a metric is redefined clear the metric registry and metric map.
	// prometheus client panics on metric redefinition.
	// Addition and removal of metrics is ok.
	var cl *cinfo
	for _, m := range cfg.Metrics {
		// metric is not found in the current metric table
		// should be added.
		if cl = b.metrics[m.InstanceName]; cl == nil {
			newMetrics = append(newMetrics, m)
			continue
		}

		// metric collector found and sha matches
		// safe to reuse the existing collector.
		if cl.sha == computeSha(m, env.Logger()) {
			continue
		}

		// sha does not match.
		env.Logger().Warningf("Metric %s redefined. Reloading adapter.", m.Name)
		b.clearState()
		// consider all configured metrics to be "new".
		newMetrics = cfg.Metrics
		break
	}

	if env.Logger().VerbosityLevel(4) {
		env.Logger().Infof("%d new metrics defined", len(newMetrics))
	}

	var err error
	for _, m := range newMetrics {
		mname := m.InstanceName
		if len(m.Name) != 0 {
			mname = m.Name
		}
		ci := &cinfo{kind: m.Kind, sha: computeSha(m, env.Logger())}
		switch m.Kind {
		case config.GAUGE:
			ci.c, err = registerOrGet(b.registry, newGaugeVec(mname, m.Description, m.LabelNames))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			b.metrics[m.InstanceName] = ci
		case config.COUNTER:
			ci.c, err = registerOrGet(b.registry, newCounterVec(mname, m.Description, m.LabelNames))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			b.metrics[m.InstanceName] = ci
		case config.DISTRIBUTION:
			ci.c, err = registerOrGet(b.registry, newHistogramVec(mname, m.Description, m.LabelNames, m.Buckets))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			b.metrics[m.InstanceName] = ci
		default:
			metricErr = multierror.Append(metricErr, fmt.Errorf("unknown metric kind (%d); could not register metric %v", m.Kind, m))
		}
	}

	if err := b.srv.Start(env, promhttp.HandlerFor(b.registry, promhttp.HandlerOpts{})); err != nil {
		return nil, err
	}

	return &handler{b.srv, b.metrics}, metricErr.ErrorOrNil()
}

func (h *handler) HandleMetric(_ context.Context, vals []*metric.Instance) error {
	var result *multierror.Error

	for _, val := range vals {
		ci := h.metrics[val.Name]
		if ci == nil {
			result = multierror.Append(result, fmt.Errorf("could not find metric info from adapter config for %s", val.Name))
			continue
		}
		collector := ci.c
		switch ci.kind {
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

func computeSha(m *config.Params_MetricInfo, log adapter.Logger) [sha1.Size]byte {
	ba, err := m.Marshal()
	if err != nil {
		log.Warningf("Unable to encode %v", err)
	}
	return sha1.Sum(ba)
}

type obuilder struct {
	b *builder
}

func (o *obuilder) Build(cfg adapter.Config, env adapter.Env) (adapter.Handler, error) {
	o.b.SetAdapterConfig(cfg)
	return o.b.Build(context.Background(), env)
}

// ConfigureMetricHandler is to be deleted
func (o *obuilder) ConfigureMetricHandler(types map[string]*metric.Type) error {
	o.b.SetMetricTypes(types)
	return nil
}
