// Copyright Istio Authors
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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/prometheus/config/config.proto -x "-n prometheus -t metric -d example"

// Package prometheus publishes metric values collected by Mixer for
// ingestion by prometheus.
package prometheus

import (
	"context"
	"crypto/sha1"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/adapter/prometheus/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
	"istio.io/pkg/cache"
	"istio.io/pkg/pool"
)

type (
	// cinfo is a collector, its kind and the sha
	// of config that produced the collector.
	// sha is used to confirm a cache hit.
	cinfo struct {
		c            prometheus.Collector
		sha          [sha1.Size]byte
		kind         config.Params_MetricInfo_Kind
		sortedLabels []string
	}

	builder struct {
		// maps instance_name to collector.
		metrics  map[string]*cinfo
		registry *prometheus.Registry
		srv      Server
		cfg      *config.Params
	}

	handler struct {
		srv     Server
		metrics map[string]*cinfo

		labelsCache cache.ExpiringCache
	}

	cacheEntry struct {
		vec    prometheus.Collector
		labels prometheus.Labels
	}
)

var (
	charReplacer = strings.NewReplacer("/", "_", ".", "_", " ", "_", "-", "")

	_ metric.HandlerBuilder = &builder{}
	_ metric.Handler        = &handler{}
)

const (
	defaultNS = "istio"
)

// GetInfoWithAddr returns the Info associated with this adapter.
func GetInfoWithAddr(addr string) (adapter.Info, Server) {
	singletonBuilder := &builder{
		srv: newServer(addr),
	}
	singletonBuilder.clearState()
	info := metadata.GetInfo("prometheus")
	info.NewBuilder = func() adapter.HandlerBuilder { return singletonBuilder }
	return info, singletonBuilder.srv
}

// GetInfo returns the Info associated with this adapter.
func GetInfo() adapter.Info {
	// prometheus uses a singleton http port, so we make the
	// builder itself a singleton, when defaultAddr become configurable
	// srv will be a map[string]server
	ii, _ := GetInfoWithAddr(defaultAddr)
	return ii
}

func (b *builder) clearState() {
	b.registry = prometheus.NewPedanticRegistry()
	b.metrics = make(map[string]*cinfo)
}

func (b *builder) SetMetricTypes(map[string]*metric.Type) {}
func (b *builder) SetAdapterConfig(cfg adapter.Config)    { b.cfg = cfg.(*config.Params) }
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if b.cfg.MetricsExpirationPolicy != nil {
		if b.cfg.MetricsExpirationPolicy.MetricsExpiryDuration <= 0 {
			ce = ce.Appendf("metricsExpiryDuration",
				"metricsExpiryDuration %v is invalid, must be > 0", b.cfg.MetricsExpirationPolicy.MetricsExpiryDuration)
		}
	}
	return
}

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

	env.Logger().Debugf("%d new metrics defined", len(newMetrics))

	var err error
	for _, m := range newMetrics {
		ns := defaultNS
		if len(m.Namespace) > 0 {
			ns = safeName(m.Namespace)
		}
		mname := m.InstanceName
		if len(m.Name) != 0 {
			mname = m.Name
		}
		ci := &cinfo{kind: m.Kind, sha: computeSha(m, env.Logger())}
		ci.sortedLabels = make([]string, len(m.LabelNames))
		copy(ci.sortedLabels, m.LabelNames)
		sort.Strings(ci.sortedLabels)

		switch m.Kind {
		case config.GAUGE:
			// TODO: make prometheus use the keys of metric.Type.Dimensions as the label names and remove from config.
			ci.c, err = registerOrGet(b.registry, newGaugeVec(ns, mname, m.Description, m.LabelNames))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			b.metrics[m.InstanceName] = ci
		case config.COUNTER:
			ci.c, err = registerOrGet(b.registry, newCounterVec(ns, mname, m.Description, m.LabelNames))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			b.metrics[m.InstanceName] = ci
		case config.DISTRIBUTION:
			ci.c, err = registerOrGet(b.registry, newHistogramVec(ns, mname, m.Description, m.LabelNames, m.Buckets))
			if err != nil {
				metricErr = multierror.Append(metricErr, fmt.Errorf("could not register metric: %v", err))
				continue
			}
			b.metrics[m.InstanceName] = ci
		default:
			metricErr = multierror.Append(metricErr, fmt.Errorf("unknown metric kind (%d); could not register metric %v", m.Kind, m))
		}
	}

	// We want best-effort on metrics generation. It is important to log the failures, however,
	// to help capture any breakages that may be hidden.
	opts := promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      &promLogger{logger: env.Logger()},
	}

	if err = b.srv.Start(env, promhttp.HandlerFor(b.registry, opts)); err != nil {
		return nil, err
	}

	var expiryCache cache.ExpiringCache
	if cfg.MetricsExpirationPolicy != nil {
		checkDuration := cfg.MetricsExpirationPolicy.ExpiryCheckIntervalDuration
		if checkDuration == 0 {
			checkDuration = cfg.MetricsExpirationPolicy.MetricsExpiryDuration / 2
		}
		expiryCache = cache.NewTTLWithCallback(
			cfg.MetricsExpirationPolicy.MetricsExpiryDuration,
			checkDuration,
			deleteOldMetrics)
	}

	return &handler{b.srv, b.metrics, expiryCache}, metricErr.ErrorOrNil()
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
			pl := promLabels(val.Dimensions)
			if h.labelsCache != nil {
				h.labelsCache.Set(key(val.Name, "gauge", pl, ci.sortedLabels), &cacheEntry{vec, pl})
			}
			vec.With(pl).Set(amt)
		case config.COUNTER:
			vec := collector.(*prometheus.CounterVec)
			amt, err := promValue(val.Value)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Name, err))
				continue
			}
			pl := promLabels(val.Dimensions)
			if h.labelsCache != nil {
				h.labelsCache.Set(key(val.Name, "counter", pl, ci.sortedLabels), &cacheEntry{vec, pl})
			}
			vec.With(pl).Add(amt)
		case config.DISTRIBUTION:
			vec := collector.(*prometheus.HistogramVec)
			amt, err := promValue(val.Value)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("could not get value for metric %s: %v", val.Name, err))
				continue
			}
			pl := promLabels(val.Dimensions)
			if h.labelsCache != nil {
				h.labelsCache.Set(key(val.Name, "distribution", pl, ci.sortedLabels), &cacheEntry{vec, pl})
			}
			vec.With(pl).Observe(amt)
		}
	}

	return result.ErrorOrNil()
}

func key(name, kind string, labels prometheus.Labels, sortedLabelKeys []string) uint64 {
	buf := pool.GetBuffer()
	buf.WriteString(name + ":" + kind)
	for _, k := range sortedLabelKeys {
		buf.WriteString(k + "=" + labels[k] + ";") // nolint: gas
	}
	h := fnv.New64()
	_, _ = buf.WriteTo(h)
	pool.PutBuffer(buf)
	return h.Sum64()
}

func deleteOldMetrics(_, value interface{}) {
	if entry, ok := value.(*cacheEntry); ok {
		switch v := entry.vec.(type) {
		case *prometheus.CounterVec:
			v.Delete(entry.labels)
		case *prometheus.GaugeVec:
			v.Delete(entry.labels)
		case *prometheus.HistogramVec:
			v.Delete(entry.labels)
		}
	}
}

func (h *handler) Close() error { return h.srv.Close() }

func newCounterVec(namespace, name, desc string, labels []string) *prometheus.CounterVec {
	if desc == "" {
		desc = name
	}
	c := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      safeName(name),
			Help:      desc,
		},
		labelNames(labels),
	)
	return c
}

func newGaugeVec(namespace, name, desc string, labels []string) *prometheus.GaugeVec {
	if desc == "" {
		desc = name
	}
	c := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      safeName(name),
			Help:      desc,
		},
		labelNames(labels),
	)
	return c
}

func newHistogramVec(namespace, name, desc string, labels []string, bucketDef *config.Params_MetricInfo_BucketsDefinition) *prometheus.HistogramVec {
	if desc == "" {
		desc = name
	}
	c := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      safeName(name),
			Help:      desc,
			Buckets:   buckets(bucketDef),
		},
		labelNames(labels),
	)
	return c
}

func buckets(def *config.Params_MetricInfo_BucketsDefinition) []float64 {
	switch def.GetDefinition().(type) {
	case *config.Params_MetricInfo_BucketsDefinition_ExplicitBuckets:
		b := def.GetExplicitBuckets()
		return b.Bounds
	case *config.Params_MetricInfo_BucketsDefinition_LinearBuckets:
		lb := def.GetLinearBuckets()
		return prometheus.LinearBuckets(lb.Offset, lb.Width, int(lb.NumFiniteBuckets+1))
	case *config.Params_MetricInfo_BucketsDefinition_ExponentialBuckets:
		eb := def.GetExponentialBuckets()
		return prometheus.ExponentialBuckets(eb.Scale, eb.GrowthFactor, int(eb.NumFiniteBuckets+1))
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
		labels[i] = adapter.Stringify(label)
	}
	return labels
}

func computeSha(m proto.Marshaler, log adapter.Logger) [sha1.Size]byte {
	ba, err := m.Marshal()
	if err != nil {
		log.Warningf("Unable to encode %v", err)
	}
	return sha1.Sum(ba)
}

type promLogger struct {
	logger adapter.Logger
}

func (pl *promLogger) Println(v ...interface{}) {
	_ = pl.logger.Errorf("Prometheus handler error: %s", fmt.Sprintln(v...)) // nolint: gas
}
