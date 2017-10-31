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

package prometheus

import (
	"context"
	"errors"
	"net/http"
	"reflect"
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

var _ server = &testServer{}

func (t testServer) Start(adapter.Env, http.Handler) error {
	if t.errOnStart {
		return errors.New("could not start server")
	}
	return nil
}

func (testServer) Close() error { return nil }

func newBuilder(s server) *builder {
	return &builder{
		srv:      s,
		registry: prometheus.NewPedanticRegistry(),
		metrics:  make(map[string]*cinfo),
	}
}

var (
	gaugeNoLabels = &config.Params_MetricInfo{
		InstanceName: "/funky::gauge",
		Description:  "funky all the time",
		Kind:         config.GAUGE,
		LabelNames:   []string{},
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

	counter = &config.Params_MetricInfo{
		InstanceName: "special_counter",
		Description:  "count all the special tests",
		Kind:         config.COUNTER,
		LabelNames:   []string{"bool", "string", "email"},
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

	counterVal = &metric.Instance{
		Name: counter.InstanceName,
		Dimensions: map[string]interface{}{
			"bool":   true,
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

func TestBuild(t *testing.T) {
	f := newBuilder(&testServer{})

	tests := []struct {
		name    string
		metrics []*config.Params_MetricInfo
	}{
		{"No Metrics", []*config.Params_MetricInfo{}},
		{"One Gauge", []*config.Params_MetricInfo{gaugeNoLabels}},
		{"One Counter", []*config.Params_MetricInfo{counterNoLabels}},
		{"Distribution", []*config.Params_MetricInfo{histogramNoLabels}},
		{"Multiple Metrics", []*config.Params_MetricInfo{counterNoLabels, gaugeNoLabels, histogramNoLabels}},
		{"With Labels", []*config.Params_MetricInfo{counter, histogram}},
		{"No Descriptions", []*config.Params_MetricInfo{counterNoLabelsNoDesc, gaugeNoLabelsNoDesc, histogramNoLabelsNoDesc}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(makeConfig(v.metrics...))
			if _, err := f.Build(context.Background(), test.NewEnv(t)); err != nil {
				t.Errorf("NewMetricsAspect() => unexpected error: %v", err)
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

func TestFactory_BuildServerFail(t *testing.T) {
	f := newBuilder(&testServer{errOnStart: true})
	f.SetAdapterConfig(makeConfig())
	if _, err := f.Build(context.Background(), test.NewEnv(t)); err == nil {
		t.Error("NewMetricsAspect() => expected error on server startup")
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

func TestFactory_Build_MetricDefinitionConflicts(t *testing.T) {
	f := newBuilder(&testServer{})

	gaugeWithLabels := &config.Params_MetricInfo{
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

func TestProm_Record(t *testing.T) {
	duration, _ := time.ParseDuration("386ms")

	f := newBuilder(&testServer{})
	tests := []struct {
		name    string
		metrics []*config.Params_MetricInfo
		values  []*metric.Instance
	}{
		/*	{"Increment Counter", []*config.Params_MetricInfo{counter}, []*metric.Instance{counterVal}},
			{"Histogram Observation", []*config.Params_MetricInfo{histogram}, []*metric.Instance{histogramVal}},
			{"Change Gauge", []*config.Params_MetricInfo{gaugeNoLabels}, []*metric.Instance{gaugeVal}},*/
		{"Counter and Gauge",
			[]*config.Params_MetricInfo{counterNoLabels, gaugeNoLabels},
			[]*metric.Instance{gaugeVal, newCounterVal(counterNoLabels.InstanceName, float64(16))}},
		{"Int64", []*config.Params_MetricInfo{gaugeNoLabels}, []*metric.Instance{newGaugeVal(gaugeVal.Name, int64(8))}},
		{"Duration", []*config.Params_MetricInfo{gaugeNoLabels}, []*metric.Instance{newGaugeVal(gaugeVal.Name, duration)}},
		{"String", []*config.Params_MetricInfo{gaugeNoLabels}, []*metric.Instance{newGaugeVal(gaugeVal.Name, "8.243543")}},
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
			if err != nil {
				t.Errorf("Record() => unexpected error: %v", err)
			}
			// Check tautological recording of entries.
			pr := aspect.(*handler)
			for _, adapterVal := range v.values {
				ci, ok := pr.metrics[adapterVal.Name]
				if !ok {
					t.Errorf("Record() could not find metric with name %s:", adapterVal.Name)
					continue
				}
				c := ci.c
				m := new(dto.Metric)
				switch c.(type) {
				case *prometheus.CounterVec:
					if err := c.(*prometheus.CounterVec).With(promLabels(adapterVal.Dimensions)).Write(m); err != nil {
						t.Errorf("Error writing metric value to proto: %v", err)
						continue
					}
				case *prometheus.GaugeVec:
					if err := c.(*prometheus.GaugeVec).With(promLabels(adapterVal.Dimensions)).Write(m); err != nil {
						t.Errorf("Error writing metric value to proto: %v", err)
						continue
					}
				case *prometheus.HistogramVec:
					if err := c.(*prometheus.HistogramVec).With(promLabels(adapterVal.Dimensions)).Write(m); err != nil {
						t.Errorf("Error writing metric value to proto: %v", err)
						continue
					}
				}

				got := metricValue(m)
				want, err := promValue(adapterVal.Value)
				if err != nil {
					t.Errorf("Record(%s) could not get desired value: %v", adapterVal.Name, err)
				}
				if got != want {
					t.Errorf("Record(%s) => %f, want %f", adapterVal.Name, got, want)
				}
			}
		})
	}
}

func TestProm_RecordFailures(t *testing.T) {
	f := newBuilder(&testServer{})
	tests := []struct {
		name    string
		metrics []*config.Params_MetricInfo
		values  []*metric.Instance
	}{
		{"Not Found", []*config.Params_MetricInfo{counterNoLabels}, []*metric.Instance{newGaugeVal(gaugeVal.Name, true)}},
		{"Bool", []*config.Params_MetricInfo{gaugeNoLabels}, []*metric.Instance{newGaugeVal(gaugeVal.Name, true)}},
		{"Text String (Gauge)", []*config.Params_MetricInfo{gaugeNoLabels}, []*metric.Instance{newGaugeVal(gaugeVal.Name, "not a value")}},
		{"Text String (Counter)", []*config.Params_MetricInfo{counterNoLabels}, []*metric.Instance{newCounterVal(counterVal.Name, "not a value")}},
		{"Text String (Histogram)", []*config.Params_MetricInfo{histogramNoLabels}, []*metric.Instance{newHistogramVal(histogramVal.Name, "not a value")}},
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
				t.Error("Record() - expected error, got none")
			}
		})
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
