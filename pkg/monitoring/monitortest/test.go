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
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opencensus.io/metric/metricdata"
	"go.opencensus.io/metric/metricexport"
	"go.opencensus.io/stats/view"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

type testExporter struct {
	sync.Mutex

	t       test.Failer
	rows    map[string][]*view.Row
	metrics map[string]*metricdata.Metric
}

func (t *testExporter) ExportView(d *view.Data) {
	t.Lock()
	defer t.Unlock()
	for _, tk := range d.View.TagKeys {
		if len(tk.Name()) < 1 {
			t.t.Fatalf("got invalid tag: %v", tk)
		}
	}
	t.rows[d.View.Name] = append(t.rows[d.View.Name], d.Rows...)
}

func (t *testExporter) ExportMetrics(ctx context.Context, data []*metricdata.Metric) error {
	t.metrics = map[string]*metricdata.Metric{}
	for _, m := range data {
		t.metrics[m.Descriptor.Name] = m
	}
	return nil
}

type MetricsTest struct {
	t   test.Failer
	exp *testExporter
}

func New(t test.Failer) *MetricsTest {
	exp := &testExporter{
		t:    t,
		rows: make(map[string][]*view.Row),
	}
	mt := &MetricsTest{t: t, exp: exp}
	view.RegisterExporter(exp)
	t.Cleanup(func() {
		view.UnregisterExporter(exp)
		mt.resetAll()
	})
	view.SetReportingPeriod(time.Millisecond)
	return mt
}

func (m *MetricsTest) resetAll() {
	m.t.Helper()
	m.exp.Lock()
	defer m.exp.Unlock()
	names := sets.New[string](maps.Keys(m.exp.rows)...)
	names.InsertAll(maps.Keys(m.exp.metrics)...)
	for name := range names {
		v := view.Find(name)
		if v == nil {
			continue
		}
		view.Unregister(v)
		if err := view.Register(v); err != nil {
			m.t.Fatal(err)
		}
	}
}

type Compare func(any) bool

func Exactly(v float64) func(any) bool {
	return func(f any) bool {
		return v == toFloat(f)
	}
}

func Distribution(count int64, sum float64) func(any) bool {
	return func(f any) bool {
		d := f.(*metricdata.Distribution)
		return d.Count == count && d.Sum == sum
	}
}

func AtLeast(v float64) func(any) bool {
	return func(f any) bool {
		return toFloat(f) >= v
	}
}

func (m *MetricsTest) Assert(name string, tags map[string]string, val Compare, opts ...retry.Option) {
	m.t.Helper()
	opt := []retry.Option{retry.Timeout(time.Second * 5), retry.Message("metric not found")}
	opt = append(opt, opts...)
	err := retry.UntilSuccess(func() error {
		reader := metricexport.NewReader()
		reader.ReadAndExport(m.exp)
		m.exp.Lock()
		defer m.exp.Unlock()
		ts, f := m.exp.metrics[name]
		if !f || len(ts.TimeSeries) == 0 {
			return fmt.Errorf("metric %v not found", name)
		}
		for _, r := range ts.TimeSeries {
			want := maps.Clone(tags)
			for i, t := range r.LabelValues {
				k := ts.Descriptor.LabelKeys[i].Key
				if want[k] == t.Value {
					delete(want, k)
				} else {
					m.t.Logf("skip metric: want %v=%v, got %v=%v", k, want[k], k, t.Value)
				}
			}
			if len(want) > 0 {
				// Not a match
				m.t.Logf("missing labels: %+v", want)
				continue
			}
			if val(r.Points[0].Value) {
				return nil
			}
			m.t.Logf("got unexpected val %v", display(r.Points[0].Value))

		}
		return fmt.Errorf("no matching rows found")
	}, opt...)
	if err != nil {
		m.Dump()
		m.t.Fatal(err)
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
	case *metricdata.Distribution:
		return v.Sum
	}
}

func (m *MetricsTest) Dump() {
	m.t.Helper()
	m.exp.Lock()
	defer m.exp.Unlock()
	for name, met := range m.exp.metrics {
		m.t.Logf("metric %v: %v rows", name, len(met.TimeSeries))
		for _, row := range met.TimeSeries {
			kv := []string{}
			for i, v := range row.LabelValues {
				k := met.Descriptor.LabelKeys[i]
				kv = append(kv, k.Key+"="+v.Value)
			}
			tags := strings.Join(kv, ",")
			m.t.Logf(" %v{%v} %v", name, tags, display(row.Points[0].Value))
		}
	}
}

func display(r interface{}) string {
	switch v := r.(type) {
	default:
		return fmt.Sprintf("%v", v)
	case *metricdata.Distribution:
		return fmt.Sprintf("distribution{count=%v,sum=%v}", v.Count, v.Sum)
	}
}
