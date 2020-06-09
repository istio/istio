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

package prometheus

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"istio.io/istio/mixer/adapter/prometheus/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/metric"
)

type testServer struct {
	errOnStart bool
}

type metricInfos []*config.Params_MetricInfo
type metricInsts []*metric.Instance

var _ Server = &testServer{}

func (t testServer) Start(adapter.Env, http.Handler) error {
	if t.errOnStart {
		return errors.New("could not start server")
	}
	return nil
}

func (testServer) Port() int    { return 0 }
func (testServer) Close() error { return nil }

func newBuilder(s Server) *builder {
	return &builder{
		srv:      s,
		registry: prometheus.NewPedanticRegistry(),
		metrics:  make(map[string]*cinfo),
	}
}

var (
	gaugeNoLabels = &config.Params_MetricInfo{
		Namespace:    "not-istio",
		InstanceName: "/funky::gauge",
		Description:  "funky all the time",
		Kind:         config.GAUGE,
		LabelNames:   []string{},
	}

	noInstanceName = &config.Params_MetricInfo{
		Name:        "newInstance",
		Description: "funky all the time",
		Kind:        config.GAUGE,
		LabelNames:  []string{},
	}

	histogramNoLabels = &config.Params_MetricInfo{
		InstanceName: "happy_histogram",
		Description:  "fun with buckets",
		Kind:         config.DISTRIBUTION,
		LabelNames:   []string{},
		Buckets: &config.Params_MetricInfo_BucketsDefinition{
			&config.Params_MetricInfo_BucketsDefinition_ExplicitBuckets{
				&config.Params_MetricInfo_BucketsDefinition_Explicit{Bounds: []float64{0.5434}}}},
	}

	counterNoLabels = &config.Params_MetricInfo{
		InstanceName: "the.counter",
		Description:  "count all the tests",
		Kind:         config.COUNTER,
		LabelNames:   []string{},
	}

	gaugeNoLabelsNoDesc = &config.Params_MetricInfo{
		InstanceName: "/funky::gauge.nodesc",
		Kind:         config.GAUGE,
		LabelNames:   []string{},
	}

	counterNoLabelsNoDesc = &config.Params_MetricInfo{
		InstanceName: "the.counter.nodesc",
		Kind:         config.COUNTER,
		LabelNames:   []string{},
	}

	histogramNoLabelsNoDesc = &config.Params_MetricInfo{
		InstanceName: "happy_histogram_the_elder",
		Kind:         config.DISTRIBUTION,
		LabelNames:   []string{},
		Buckets: &config.Params_MetricInfo_BucketsDefinition{
			&config.Params_MetricInfo_BucketsDefinition_LinearBuckets{
				&config.Params_MetricInfo_BucketsDefinition_Linear{NumFiniteBuckets: 5, Offset: 45, Width: 12}}},
	}

	gaugeWithLabels = &config.Params_MetricInfo{
		InstanceName: "labeled_gauge",
		Description:  "super power strength gauge",
		Kind:         config.GAUGE,
		LabelNames:   []string{"bool", "string", "email"},
	}

	counter = &config.Params_MetricInfo{
		InstanceName: "special_counter",
		Description:  "count all the special tests",
		Kind:         config.COUNTER,
		LabelNames:   []string{"bool", "string", "email"},
	}

	counterModified = &config.Params_MetricInfo{
		InstanceName: "special_counter",
		Description:  "count all the special tests",
		Kind:         config.COUNTER,
		LabelNames:   []string{}, // removed labels from "counter"
	}

	histogram = &config.Params_MetricInfo{
		InstanceName: "happy_histogram_the_younger",
		Description:  "fun with buckets",
		Kind:         config.DISTRIBUTION,
		Buckets: &config.Params_MetricInfo_BucketsDefinition{
			&config.Params_MetricInfo_BucketsDefinition_ExponentialBuckets{
				&config.Params_MetricInfo_BucketsDefinition_Exponential{Scale: .14, GrowthFactor: 2, NumFiniteBuckets: 198}},
		},
		LabelNames: []string{"bool", "string", "email"},
	}

	unknown = &config.Params_MetricInfo{
		InstanceName: "unknown",
		Description:  "unknown",
		Kind:         config.UNSPECIFIED,
		LabelNames:   []string{},
	}

	gaugeWithLabelsVal = &metric.Instance{
		Name: gaugeWithLabels.InstanceName,
		Dimensions: map[string]interface{}{
			"bool":   true,
			"string": "testing",
			"email":  "test@istio.io",
		},
		Value: float64(45),
	}

	counterVal = &metric.Instance{
		Name: counter.InstanceName,
		Dimensions: map[string]interface{}{
			"bool":   true,
			"string": "testing",
			"email":  "test@istio.io",
		},
		Value: float64(45),
	}

	counterVal2 = &metric.Instance{
		Name: counter.InstanceName,
		Dimensions: map[string]interface{}{
			"bool":   false,
			"string": "testing",
			"email":  "test@istio.io",
		},
		Value: float64(45),
	}

	histogramVal = &metric.Instance{
		Name: histogram.InstanceName,
		Dimensions: map[string]interface{}{
			"bool":   true,
			"string": "testing",
			"email":  "test@istio.io",
		},
		Value: float64(234.23),
	}

	gaugeVal = newGaugeVal(gaugeNoLabels.InstanceName, int64(993))
)

func TestGetInfo(t *testing.T) {
	i := GetInfo()
	if i.Name != "prometheus" {
		t.Fatalf("GetInfo().Name=%s; want %s", i.Name, "prometheus")
	}

	if !reflect.DeepEqual(i.SupportedTemplates, []string{metric.TemplateName}) {
		t.Fatalf("GetInfo().SupportedTemplates=%v; want %v", i.SupportedTemplates, []string{metric.TemplateName})
	}
}

func TestBuild(t *testing.T) {
	f := newBuilder(&testServer{})

	tests := []struct {
		name    string
		metrics metricInfos
	}{
		{"No Metrics", metricInfos{}},
		{"One Gauge", metricInfos{gaugeNoLabels}},
		{"No Instance Name", metricInfos{noInstanceName}},
		{"One Counter", metricInfos{counterNoLabels}},
		{"Distribution", metricInfos{histogramNoLabels}},
		{"Multiple Metrics", metricInfos{counterNoLabels, gaugeNoLabels, histogramNoLabels}},
		{"With Labels", metricInfos{counter, histogram}},
		{"counter With Labels modified", metricInfos{counterModified}},
		{"No Descriptions", metricInfos{counterNoLabelsNoDesc, gaugeNoLabelsNoDesc, histogramNoLabelsNoDesc}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfig(v.metrics...))
			f.SetMetricTypes(nil)
			_ = f.Validate()
			if _, err := f.Build(context.Background(), test.NewEnv(t)); err != nil {
				t.Errorf("Build() => unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_withExpirationPolicy(t *testing.T) {
	f := newBuilder(&testServer{})

	tests := []struct {
		name    string
		policy  *config.Params_MetricsExpirationPolicy
		wantErr bool
	}{
		{"No Policy", nil, false},
		{"Good Policy", expPolicy(10*time.Minute, 1*time.Second), false},
		{"Missing Expiration", &config.Params_MetricsExpirationPolicy{ExpiryCheckIntervalDuration: 1 * time.Minute}, true},
		{"Missing Interval", &config.Params_MetricsExpirationPolicy{MetricsExpiryDuration: 1 * time.Minute}, false},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfigWithExpirationPolicy(v.policy))
			f.SetMetricTypes(nil)
			err := f.Validate()
			if err == nil && v.wantErr {
				t.Error("Validate() should have produced an error")
			} else if err != nil && !v.wantErr {
				t.Errorf("Validate() => unexpected error: %v", err)
			}
		})
	}
}

func TestBucket(t *testing.T) {
	tests := []struct {
		name      string
		bucketDef *config.Params_MetricInfo_BucketsDefinition
		want      []float64
	}{
		{
			name: "linear bucket",
			bucketDef: &config.Params_MetricInfo_BucketsDefinition{
				Definition: &config.Params_MetricInfo_BucketsDefinition_LinearBuckets{
					LinearBuckets: &config.Params_MetricInfo_BucketsDefinition_Linear{
						Offset: 4, Width: 1, NumFiniteBuckets: 1,
					},
				},
			},
			want: []float64{4, 5},
		},
		{
			name: "explicit bucket",
			bucketDef: &config.Params_MetricInfo_BucketsDefinition{
				Definition: &config.Params_MetricInfo_BucketsDefinition_ExplicitBuckets{
					ExplicitBuckets: &config.Params_MetricInfo_BucketsDefinition_Explicit{
						Bounds: []float64{6, 7},
					},
				},
			},
			want: []float64{6, 7},
		},
		{
			name: "exponential bucket",
			bucketDef: &config.Params_MetricInfo_BucketsDefinition{
				Definition: &config.Params_MetricInfo_BucketsDefinition_ExponentialBuckets{
					ExponentialBuckets: &config.Params_MetricInfo_BucketsDefinition_Exponential{
						GrowthFactor: 3, NumFiniteBuckets: 3, Scale: 4,
					},
				},
			},
			want: []float64{4, 12, 36, 108},
		},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			if got := buckets(v.bucketDef); !reflect.DeepEqual(got, v.want) {
				t.Errorf("bucket() => %v; want %v", got, v.want)
			}
		})
	}
}

func TestBuild_ServerFail(t *testing.T) {
	f := newBuilder(&testServer{errOnStart: true})
	f.SetAdapterConfig(makeConfig())
	if _, err := f.Build(context.Background(), test.NewEnv(t)); err == nil {
		t.Error("Build() => expected error on server startup")
	}
}

func TestRegisterOrGet(t *testing.T) {
	f := newBuilder(&testServer{})
	gv := newGaugeVec("test", "g1", "d1", []string{})
	if _, err := registerOrGet(f.registry, gv); err != nil {
		t.Fatalf("registerOrGet #1 returned error '%v'; want nil", err)
	}
	if _, err := registerOrGet(f.registry, gv); err != nil {
		t.Fatalf("registerOrGet #2 returned error '%v'; want nil", err)
	}
}

func TestBuild_MetricDefinitionErrors(t *testing.T) {
	f := newBuilder(&testServer{})
	tests := []struct {
		name    string
		metrics []*config.Params_MetricInfo
	}{
		{"Unknown MetricKind", []*config.Params_MetricInfo{unknown}},
	}
	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfig(v.metrics...))
			if _, err := f.Build(context.Background(), test.NewEnv(t)); err == nil {
				t.Errorf("Expected error for Build(%#v)", v.metrics)
			}
		})
	}
}

func TestBuild_MetricDefinitionConflicts(t *testing.T) {
	f := newBuilder(&testServer{})

	gaugeWithLabels := &config.Params_MetricInfo{
		Namespace:    "not-istio",
		InstanceName: "/funky::gauge",
		Description:  "funky all the time",
		Kind:         config.GAUGE,
		LabelNames:   []string{"test"},
	}

	altCounter := &config.Params_MetricInfo{
		InstanceName: "special_counter",
		Description:  "count all the special tests",
		Kind:         config.COUNTER,
		LabelNames:   []string{"email"},
	}

	altHistogram := &config.Params_MetricInfo{
		InstanceName: "happy_histogram",
		Description:  "fun with buckets",
		Kind:         config.DISTRIBUTION,
		LabelNames:   []string{"test"},
	}

	tests := []struct {
		name    string
		metrics []*config.Params_MetricInfo
	}{
		{"Gauge Definition Conflicts", []*config.Params_MetricInfo{gaugeNoLabels, gaugeWithLabels}},
		{"Counter Definition Conflicts", []*config.Params_MetricInfo{counter, altCounter}},
		{"Histogram Definition Conflicts", []*config.Params_MetricInfo{histogramNoLabels, altHistogram}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfig(v.metrics...))
			_, err := f.Build(context.Background(), test.NewEnv(t))
			if err == nil {
				t.Error("Build() => expected error during metrics registration")
			}
		})
	}
}

func TestProm_Close(t *testing.T) {
	f := newBuilder(&testServer{})
	f.SetAdapterConfig(&config.Params{})
	prom, _ := f.Build(context.Background(), test.NewEnv(t))
	if err := prom.Close(); err != nil {
		t.Errorf("Close() should not have returned an error: %v", err)
	}
}

func TestProm_HandleMetrics(t *testing.T) {
	duration, _ := time.ParseDuration("386ms")

	f := newBuilder(&testServer{})
	tests := []struct {
		name    string
		metrics metricInfos
		values  metricInsts
	}{
		{"Counter and Gauge", metricInfos{counterNoLabels, gaugeNoLabels}, metricInsts{gaugeVal, newCounterVal(counterNoLabels.InstanceName, float64(16))}},
		{"Int64", metricInfos{gaugeNoLabels}, metricInsts{newGaugeVal(gaugeVal.Name, int64(8))}},
		{"Int64WithLabels", metricInfos{counter}, metricInsts{counterVal}},
		{"Duration", metricInfos{gaugeNoLabels}, metricInsts{newGaugeVal(gaugeVal.Name, duration)}},
		{"String", metricInfos{gaugeNoLabels}, metricInsts{newGaugeVal(gaugeVal.Name, "8.243543")}},
		{"histogram int64", metricInfos{histogramNoLabels}, metricInsts{newHistogramVal(histogramNoLabels.InstanceName, int64(8))}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfig(v.metrics...))
			a, err := f.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Errorf("Build() => unexpected error: %v", err)
			}
			hndlr := a.(metric.Handler)

			err = hndlr.HandleMetric(context.Background(), v.values)
			if err != nil {
				t.Errorf("HandleMetric() => unexpected error: %v", err)
			}
			// Check tautological recording of entries.
			pr := hndlr.(*handler)
			for _, adapterVal := range v.values {
				ci, ok := pr.metrics[adapterVal.Name]
				if !ok {
					t.Errorf("HandleMetric() could not find metric with name %s:", adapterVal.Name)
					continue
				}
				m := new(dto.Metric)
				switch c := ci.c.(type) {
				case *prometheus.CounterVec:
					if err := c.With(promLabels(adapterVal.Dimensions)).Write(m); err != nil {
						t.Errorf("Error writing metric value to proto: %v", err)
						continue
					}
				case *prometheus.GaugeVec:
					if err := c.With(promLabels(adapterVal.Dimensions)).Write(m); err != nil {
						t.Errorf("Error writing metric value to proto: %v", err)
						continue
					}
				case *prometheus.HistogramVec:
					if err := c.With(promLabels(adapterVal.Dimensions)).(prometheus.Metric).Write(m); err != nil {
						t.Errorf("Error writing metric value to proto: %v", err)
						continue
					}
				}

				got := metricValue(m)
				want, err := promValue(adapterVal.Value)
				if err != nil {
					t.Errorf("HandleMetric(%s) could not get desired value: %v", adapterVal.Name, err)
				}
				if got != want {
					t.Errorf("HandleMetric(%s) => %f, want %f", adapterVal.Name, got, want)
				}
			}
		})
	}
}

func TestProm_HandleMetricFailures(t *testing.T) {
	f := newBuilder(&testServer{})
	tests := []struct {
		name    string
		metrics metricInfos
		values  metricInsts
	}{
		{"Not Found", metricInfos{counterNoLabels}, metricInsts{newGaugeVal(gaugeVal.Name, true)}},
		{"Bool", metricInfos{gaugeNoLabels}, metricInsts{newGaugeVal(gaugeVal.Name, true)}},
		{"Text String (Gauge)", metricInfos{gaugeNoLabels}, metricInsts{newGaugeVal(gaugeVal.Name, "not a value")}},
		{"Text String (Counter)", metricInfos{counterNoLabels}, metricInsts{newCounterVal(counterNoLabels.InstanceName, "not a value")}},
		{"Text String (Histogram)", metricInfos{histogram}, metricInsts{newHistogramVal(histogramVal.Name, "not a value")}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfig(v.metrics...))
			a, err := f.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Errorf("Build() => unexpected error: %v", err)
			}
			aspect := a.(metric.Handler)
			err = aspect.HandleMetric(context.Background(), v.values)
			if err == nil {
				t.Error("HandleMetric() - expected error, got none")
			}
		})
	}
}

type marshaler struct{}

func (m marshaler) Marshal() ([]byte, error) {
	return nil, errors.New("error")
}

func TestComputeSha(t *testing.T) {
	// just ensure we don't panic
	_ = computeSha(marshaler{}, test.NewEnv(t).Logger())
}

func TestMetricExpiration(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/17182")
	testPolicy := expPolicy(50*time.Millisecond, 10*time.Millisecond)
	altPolicy := expPolicy(50*time.Millisecond, 0)

	cases := []struct {
		name             string
		metrics          metricInfos
		valuesToExpire   metricInsts
		valuesToRefresh  metricInsts
		expirationPolicy *config.Params_MetricsExpirationPolicy
		checkWaitTime    time.Duration
	}{
		{"No expiration", metricInfos{counter}, metricInsts{counterVal}, metricInsts{}, nil, 0},
		{"Single metric expiration (counter)", metricInfos{counter}, metricInsts{counterVal}, metricInsts{}, testPolicy, 200 * time.Millisecond},
		{"Single metric expiration (counter, alt policy)", metricInfos{counter}, metricInsts{counterVal}, metricInsts{}, altPolicy, 200 * time.Millisecond},
		{"Single metric expiration (gauge)", metricInfos{gaugeWithLabels}, metricInsts{gaugeWithLabelsVal}, metricInsts{}, testPolicy, 200 * time.Millisecond},
		{"Single metric expiration (histogram)", metricInfos{histogram}, metricInsts{histogramVal}, metricInsts{}, testPolicy, 200 * time.Millisecond},
		{"Preserve non-stale metrics", metricInfos{counter}, metricInsts{counterVal}, metricInsts{counterVal2}, testPolicy, 200 * time.Millisecond},
	}

	f := newBuilder(&testServer{})
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfigWithExpirationPolicy(v.expirationPolicy, v.metrics...))
			a, err := f.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Errorf("Build() => unexpected error: %v", err)
			}
			h := a.(metric.Handler)

			if err := h.HandleMetric(context.Background(), v.valuesToExpire); err != nil {
				t.Errorf("HandleMetric() => unexpected error: %v", err)
			}

			time.Sleep(v.checkWaitTime)

			inst := a.(*handler)
			if v.expirationPolicy != nil {

				h.HandleMetric(context.Background(), v.valuesToRefresh) // add fresh instances
				inst.labelsCache.EvictExpired()                         // ensure eviction
				inst.labelsCache.RemoveAll()                            // prevent further evictions

				if int(inst.labelsCache.Stats().Evictions) != len(v.valuesToExpire) {
					t.Errorf("Bad number of evictions: got %d, wanted %d", inst.labelsCache.Stats().Evictions, len(v.valuesToExpire))
				}

				totalMetrics := 0

				var wg sync.WaitGroup
				for _, m := range v.metrics {
					ch := make(chan prometheus.Metric)
					go func() {
						for {
							elem, more := <-ch
							if !more {
								wg.Done()
								return
							}
							t.Logf("collector (%s): %+v", m.Name, elem)
							totalMetrics++
						}
					}()
					wg.Add(1)
					inst.metrics[m.InstanceName].c.Collect(ch)
					close(ch)
				}

				wg.Wait()
				if totalMetrics != len(v.valuesToRefresh) {
					t.Errorf("Bad number of metrics remaining: got %d, want %d", totalMetrics, len(v.valuesToRefresh))
				}
			}
		})
	}
}

func BenchmarkKey(b *testing.B) {
	pl := prometheus.Labels(map[string]string{
		"a":      "14.5",
		"c":      "hello",
		"f":      "cannonball",
		"b":      "kubernetes",
		"g":      "istio",
		"d":      "system",
		"magic":  "15ms",
		"lots":   "tons",
		"of":     "0.0001",
		"fun":    "isn't it?",
		"test":   "production",
		"joy":    "sadness",
		"allocs": "zero"})

	keys := make([]string, 0, len(pl))
	// ensure stable order
	for k := range pl {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key("testMetric", "gauge", pl, keys)
	}
}

func metricValue(m *dto.Metric) float64 {
	if c := m.GetCounter(); c != nil {
		return *c.Value
	}
	if c := m.GetGauge(); c != nil {
		return *c.Value
	}
	if c := m.GetHistogram(); c != nil {
		return *c.SampleSum
	}
	if c := m.GetUntyped(); c != nil {
		return *c.Value
	}
	return -1
}

func newGaugeVal(name string, val interface{}) *metric.Instance {
	return &metric.Instance{
		Name:       name,
		Value:      val,
		Dimensions: map[string]interface{}{},
	}
}

func newCounterVal(name string, val interface{}) *metric.Instance {
	return &metric.Instance{
		Name:       name,
		Value:      val,
		Dimensions: map[string]interface{}{},
	}
}

func newHistogramVal(name string, val interface{}) *metric.Instance {
	return &metric.Instance{
		Name:       name,
		Value:      val,
		Dimensions: map[string]interface{}{},
	}
}

func makeConfig(metrics ...*config.Params_MetricInfo) *config.Params {
	return &config.Params{Metrics: metrics}
}

func makeConfigWithExpirationPolicy(policy *config.Params_MetricsExpirationPolicy, metrics ...*config.Params_MetricInfo) *config.Params {
	return &config.Params{Metrics: metrics, MetricsExpirationPolicy: policy}
}

func expPolicy(expiry, check time.Duration) *config.Params_MetricsExpirationPolicy {
	return &config.Params_MetricsExpirationPolicy{expiry, check}
}

func TestPromLogger_Println(t *testing.T) {
	testEnvLogger := test.NewEnv(t)
	underTest := &promLogger{logger: testEnvLogger}

	underTest.Println("Istio Service Mesh", 94.5, false)

	logs := testEnvLogger.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("Println() did not produce correct number of logs; got %d, want 1", len(logs))
	}

	line := logs[0]
	want := "Prometheus handler error: Istio Service Mesh 94.5 false\n"
	if !strings.EqualFold(line, want) {
		t.Fatalf("Println() did not produce expected logs; got '%s', want '%s'", line, want)
	}

}
