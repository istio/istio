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

package monitortest

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil/promlint"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/attribute"

	"istio.io/istio/pkg/lazy"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

type MetricsTest struct {
	t      test.Failer
	reg    prometheus.Gatherer
	deltas map[metricKey]float64
}

type metricKey struct {
	name  string
	attrs attribute.Set
}

var reg = lazy.New(func() (prometheus.Gatherer, error) {
	// TODO: do not use a global and/or add a way to reset (https://github.com/open-telemetry/opentelemetry-go/issues/4291)
	reg := prometheus.NewRegistry()
	_, err := monitoring.RegisterPrometheusExporter(reg, reg)
	if err != nil {
		return nil, err
	}
	return reg, nil
})

func TestRegistry(t test.Failer) prometheus.Gatherer {
	r, err := reg.Get()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func New(t test.Failer) *MetricsTest {
	r := TestRegistry(t)
	mt := &MetricsTest{t: t, reg: r, deltas: computeDeltas(t, r)}
	return mt
}

func computeDeltas(t test.Failer, reg prometheus.Gatherer) map[metricKey]float64 {
	res := map[metricKey]float64{}
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range metrics {
		for _, row := range metric.Metric {
			if row.Counter == nil {
				continue
			}
			key := toMetricKey(row, metric)
			res[key] = *row.Counter.Value
		}
	}
	return res
}

func toMetricKey(row *dto.Metric, metric *dto.MetricFamily) metricKey {
	kvs := []attribute.KeyValue{}
	for _, lv := range row.Label {
		kvs = append(kvs, attribute.String(*lv.Name, *lv.Value))
	}
	key := metricKey{
		name:  *metric.Name,
		attrs: attribute.NewSet(kvs...),
	}
	return key
}

type Compare func(any) error

func DoesNotExist(any) error {
	// special case logic in the Assert
	return nil
}

// Check if two floats are equal with some room for errors (for example, rounding errors)
// For rounding errors, setting `eps` to 1e-7 is a good default
func AlmostEquals(v float64, eps float64) func(any) error {
	return func(f any) error {
		if math.Abs(v-toFloat(f)) > eps {
			return fmt.Errorf("%v and %v and not within %v", v, toFloat(f), eps)
		}
		return nil
	}
}

func Exactly(v float64) func(any) error {
	return func(f any) error {
		if v != toFloat(f) {
			return fmt.Errorf("want %v, got %v", v, toFloat(f))
		}
		return nil
	}
}

func LessThan(v float64) func(any) error {
	return func(f any) error {
		if v <= toFloat(f) {
			return fmt.Errorf("want <= %v (got %v)", v, toFloat(f))
		}
		return nil
	}
}

func Distribution(count uint64, sum float64) func(any) error {
	return func(f any) error {
		d := f.(*dto.Histogram)
		if *d.SampleCount != count {
			return fmt.Errorf("want %v samples, got %v", count, *d.SampleCount)
		}
		if *d.SampleSum != sum {
			return fmt.Errorf("want %v sum, got %v", count, *d.SampleSum)
		}
		return nil
	}
}

// Buckets asserts a distribution has the number of buckets
func Buckets(count int) func(any) error {
	return func(f any) error {
		d := f.(*dto.Histogram)
		if len(d.Bucket) != count {
			return fmt.Errorf("want %v buckets, got %v", count, len(d.Bucket))
		}
		return nil
	}
}

func AtLeast(want float64) func(any) error {
	return func(got any) error {
		if want > toFloat(got) {
			return fmt.Errorf("want %v <= %v (got %v)", want, toFloat(got), want)
		}
		return nil
	}
}

func (m *MetricsTest) Assert(name string, tags map[string]string, compare Compare, opts ...retry.Option) {
	m.t.Helper()
	opt := []retry.Option{retry.Timeout(time.Second * 5), retry.Message("metric not found")}
	opt = append(opt, opts...)
	err := retry.UntilSuccess(func() error {
		res, err := m.reg.Gather()
		if err != nil {
			return err
		}
		if fmt.Sprintf("%p", compare) == fmt.Sprintf("%p", DoesNotExist) {
			for _, metric := range res {
				if *metric.Name == name {
					return fmt.Errorf("metric was found when it should not have been")
				}
			}
			return nil
		}
		for _, metric := range res {
			if *metric.Name != name {
				continue
			}
			for _, row := range metric.Metric {
				want := maps.Clone(tags)
				for _, lv := range row.Label {
					k, v := *lv.Name, *lv.Value
					if want[k] == v {
						delete(want, k)
					} else {
						m.t.Logf("skip metric: want %v=%v, got %v=%v", k, want[k], k, v)
					}
				}
				if len(want) > 0 {
					// Not a match
					m.t.Logf("skip metric: missing labels: %+v", want)
					continue
				}
				var v any
				if row.Counter != nil {
					cv := *row.Counter.Value
					key := toMetricKey(row, metric)
					if delta, f := m.deltas[key]; f {
						cv -= delta
					}
					v = cv
				} else if row.Gauge != nil {
					v = *row.Gauge.Value
				} else if row.Histogram != nil {
					v = row.Histogram
				}
				err := compare(v)
				if err != nil {
					return fmt.Errorf("got unexpected val %v: %v", v, err)
				}
				return nil
			}
		}
		return fmt.Errorf("no matching rows found")
	}, opt...)
	if err != nil {
		m.t.Logf("Metric %v/%v not matched (%v); Dumping known metrics:", name, tags, err)
		m.Dump()
		m.t.Fatal(err)
	}

	// Run through linter. For now this is warning, maybe allow opt-in to strict
	res, err := m.reg.Gather()
	if err != nil {
		m.t.Fatal(err)
	}
	problems, err := promlint.NewWithMetricFamilies(res).Lint()
	if err != nil {
		m.t.Fatal(err)
	}
	if len(problems) > 0 {
		m.t.Logf("WARNING: Prometheus linter issue: %v", problems)
	}
}

func toFloat(r interface{}) float64 {
	switch v := r.(type) {
	default:
		panic(fmt.Sprintf("unknown type %T", r))
	case int64:
		return float64(v)
	case float64:
		return v
	}
}

// Metrics returns the full list of known metrics. Usually Assert should be used
func (m *MetricsTest) Metrics() []Metric {
	m.t.Helper()
	res, err := m.reg.Gather()
	if err != nil {
		m.t.Fatal(err)
	}
	metrics := []Metric{}
	for _, metric := range res {
		if len(metric.Metric) == 0 {
			m.t.Logf("%v: no rows", *metric.Name)
		}
		for _, row := range metric.Metric {
			m := Metric{Name: *metric.Name, Labels: map[string]string{}, Value: display(row)}
			for _, kv := range row.Label {
				k, v := *kv.Name, *kv.Value
				m.Labels[k] = v
			}
			metrics = append(metrics, m)
		}
	}
	return metrics
}

type Metric struct {
	Name   string
	Labels map[string]string
	Value  string
}

func (m *MetricsTest) Dump() {
	m.t.Helper()
	res, err := m.reg.Gather()
	if err != nil {
		m.t.Fatal(err)
	}
	for _, metric := range res {
		if len(metric.Metric) == 0 {
			m.t.Logf("%v: no rows", *metric.Name)
		}
		for _, row := range metric.Metric {
			kvs := []string{}
			for _, kv := range row.Label {
				k, v := *kv.Name, *kv.Value
				kvs = append(kvs, k+"="+v)
			}
			tags := strings.Join(kvs, ",")
			m.t.Logf(" %v{%v} %v", *metric.Name, tags, display(row))
		}
	}
}

func display(row *dto.Metric) string {
	if row.Counter != nil {
		return fmt.Sprint(*row.Counter.Value)
	} else if row.Gauge != nil {
		return fmt.Sprint(*row.Gauge.Value)
	} else if row.Histogram != nil {
		return fmt.Sprintf("histogram{count=%v,sum=%v}", *row.Histogram.SampleCount, *row.Histogram.SampleSum)
	} else if row.Summary != nil {
		return fmt.Sprintf("summary{count=%v,sum=%v}", *row.Summary.SampleCount, *row.Summary.SampleSum)
	}
	return "?"
}
