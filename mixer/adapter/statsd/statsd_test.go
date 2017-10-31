// Copyright 2017 Istio Authors
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

package statsd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
	"github.com/cactus/go-statsd-client/statsd/statsdtest"
	"github.com/golang/protobuf/proto"

	descriptor "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/adapter/statsd/config"
	"istio.io/mixer/pkg/adapter/test"
	"istio.io/mixer/template/metric"
)

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		conf      proto.Message
		errString string
	}{
		{&config.Params{}, ""},
		{&config.Params{Metrics: map[string]*config.Params_MetricInfo{"a": {NameTemplate: `{{.apiMethod}}-{{.responseCode}}`}}}, ""},
		{&config.Params{Metrics: map[string]*config.Params_MetricInfo{"badtemplate": {NameTemplate: `{{if 1}}`}}}, "metricNameTemplateStrings"},
		{&config.Params{FlushDuration: -1}, "flushDuration"},
		{&config.Params{SamplingRate: -1}, "samplingRate"},
		{&config.Params{FlushBytes: -1}, "flushBytes"},
	}
	for idx, c := range cases {
		errString := ""

		info := GetInfo()
		b := info.NewBuilder()
		b.SetAdapterConfig(c.conf)
		if err := b.Validate(); err != nil {
			errString = err.Error()
		}
		if !strings.Contains(errString, c.errString) {
			t.Errorf("[%d] b.ValidateConfig(c.conf) = '%s'; want errString containing '%s'", idx, errString, c.errString)
		}
	}
}

func TestNewMetricsAspect(t *testing.T) {
	conf := &config.Params{
		Address:       "localhost:8125",
		Prefix:        "",
		FlushDuration: 300 * time.Millisecond,
		FlushBytes:    -1,
		SamplingRate:  1.0,
		Metrics:       map[string]*config.Params_MetricInfo{"a": {NameTemplate: `{{(.apiMethod) "-" (.responseCode)}}`}},
	}
	info := GetInfo()
	b := info.NewBuilder()
	b.SetAdapterConfig(conf)
	env := test.NewEnv(t)
	if _, err := b.Build(context.Background(), env); err != nil {
		t.Errorf("b.NewMetrics(test.NewEnv(t), &config.Params{}) = %s, wanted no err", err)
	}

	logs := env.GetLogs()
	if len(logs) < 1 {
		t.Errorf("len(logs) = %d, wanted at least 1 item logged", len(logs))
	}
	present := false
	for _, l := range logs {
		present = present || strings.Contains(l, "FlushBytes")
	}
	if !present {
		t.Errorf("wanted NewMetricsAspect(env, conf, metrics) to log about 'FlushBytes', only got logs: %v", logs)
	}
}

func TestNewMetricsAspect_InvalidTemplate(t *testing.T) {
	name := "invalidTemplate"
	conf := &config.Params{
		Address:       "localhost:8125",
		Prefix:        "",
		FlushDuration: 300 * time.Millisecond,
		FlushBytes:    512,
		SamplingRate:  1.0,
		Metrics: map[string]*config.Params_MetricInfo{
			name:      {NameTemplate: `{{ .apiMethod "-" .responseCode }}`}, // fails at execute time, not template parsing time
			"missing": {NameTemplate: "foo"},
		},
	}
	metrics := map[string]*metric.Type{
		name: {Dimensions: map[string]descriptor.ValueType{"apiMethod": descriptor.STRING, "responseCode": descriptor.INT64}},
	}
	info := GetInfo()
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(conf)
	b.SetMetricTypes(metrics)
	env := test.NewEnv(t)
	if _, err := b.Build(context.Background(), env); err != nil {
		t.Errorf("NewMetricsAspect(test.NewEnv(t), conf, metrics) = _, %s, wanted no error", err)
	}

	logs := env.GetLogs()
	if len(logs) < 1 {
		t.Errorf("len(logs) = %d, wanted at least 1 item logged", len(logs))
	}
	present := false
	for _, l := range logs {
		present = present || strings.Contains(l, name)
	}
	if !present {
		t.Errorf("wanted NewMetricsAspect(env, conf, metrics) to log template error containing '%s', only got logs: %v", name, logs)
	}
}

func TestNewMetricsAspect_BadTemplate(t *testing.T) {
	conf := &config.Params{
		Address:       "localhost:8125",
		Prefix:        "",
		FlushDuration: 300 * time.Millisecond,
		FlushBytes:    512,
		SamplingRate:  1.0,
		Metrics:       map[string]*config.Params_MetricInfo{"badtemplate": {NameTemplate: `{{if 1}}`}},
	}
	metrics := map[string]*metric.Type{"badtemplate": {}}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewMetricsAspect(test.NewEnv(t), config, nil) didn't panic")
		}
	}()

	info := GetInfo()
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(conf)
	b.SetMetricTypes(metrics)
	if _, err := b.Build(context.Background(), test.NewEnv(t)); err != nil {
		t.Errorf("NewMetricsAspect(test.NewEnv(t), config, nil) = %v; wanted panic not err", err)
	}
	t.Fail()
}

func TestRecord(t *testing.T) {
	var templateMetricName = "methodCode"
	conf := &config.Params{
		Address:       "localhost:8125",
		Prefix:        "",
		FlushDuration: time.Duration(300) * time.Millisecond,
		FlushBytes:    512,
		SamplingRate:  1.0,
		Metrics: map[string]*config.Params_MetricInfo{
			templateMetricName: {NameTemplate: `{{.apiMethod}}-{{.responseCode}}`, Type: config.COUNTER},
			"counter":          {Type: config.COUNTER},
			"distribution":     {Type: config.DISTRIBUTION},
			"gauge":            {Type: config.GAUGE},
		},
	}
	metrics := map[string]*metric.Type{
		templateMetricName: {
			Value:      descriptor.INT64,
			Dimensions: map[string]descriptor.ValueType{"apiMethod": descriptor.STRING, "responseCode": descriptor.INT64},
		},
		"counter":      {},
		"distribution": {},
		"gauge":        {},
	}

	validGauge := metric.Instance{
		Name:       "gauge",
		Value:      int64(123),
		Dimensions: make(map[string]interface{}),
	}
	invalidGauge := validGauge
	invalidGauge.Value = "bar"

	validCounter := metric.Instance{
		Name:       "counter",
		Value:      int64(456),
		Dimensions: make(map[string]interface{}),
	}
	invalidCounter := validCounter
	invalidCounter.Value = 1.0

	requestDuration := &metric.Instance{
		Name:  "distribution",
		Value: 146 * time.Millisecond,
	}
	invalidDistribution := &metric.Instance{
		Name:  "distribution",
		Value: "not good",
	}
	int64Distribution := &metric.Instance{
		Name:  "distribution",
		Value: int64(3459),
	}

	templateMetric := metric.Instance{
		Name:       templateMetricName,
		Value:      int64(1),
		Dimensions: map[string]interface{}{"apiMethod": "methodName", "responseCode": 500},
	}
	expectedMetricName := "methodName-500"

	cases := []struct {
		vals      []*metric.Instance
		errString string
	}{
		{[]*metric.Instance{}, ""},
		{[]*metric.Instance{&validGauge}, ""},
		{[]*metric.Instance{&validCounter}, ""},
		{[]*metric.Instance{&templateMetric}, ""},
		{[]*metric.Instance{requestDuration}, ""},
		{[]*metric.Instance{int64Distribution}, ""},
		{[]*metric.Instance{&validCounter, &validGauge}, ""},
		{[]*metric.Instance{&validCounter, &validGauge, &templateMetric}, ""},
		{[]*metric.Instance{&invalidCounter}, "could not record"},
		{[]*metric.Instance{&invalidGauge}, "could not record"},
		{[]*metric.Instance{invalidDistribution}, "could not record"},
		{[]*metric.Instance{&validGauge, &invalidGauge}, "could not record"},
		{[]*metric.Instance{&templateMetric, &invalidCounter}, "could not record"},
	}
	for idx, c := range cases {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			info := GetInfo()
			b := info.NewBuilder().(*builder)
			b.SetAdapterConfig(conf)
			b.SetMetricTypes(metrics)

			m, err := b.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Fatalf("newBuilder().NewMetrics(test.NewEnv(t), conf) = _, %s; wanted no err", err)
			}

			rs := statsdtest.NewRecordingSender()
			cl, err := statsd.NewClientWithSender(rs, "")
			if err != nil {
				t.Errorf("statsd.NewClientWithSender(rs, \"\") = %s; wanted no err", err)
			}
			// We don't have an easy handle into setting the client, so we'll just reach in and update it
			asp := m.(*handler)
			asp.client = cl

			if err := asp.HandleMetric(context.Background(), c.vals); err != nil {
				if c.errString == "" {
					t.Errorf("m.Record(c.vals) = %s; wanted no err", err)
				}
				if !strings.Contains(err.Error(), c.errString) {
					t.Errorf("m.Record(c.vals) = %s; wanted err containing %s", err.Error(), c.errString)
				}
			}
			if err := m.Close(); err != nil {
				t.Errorf("m.Close() = %s; wanted no err", err)
			}
			if c.errString != "" {
				return
			}

			metrics := rs.GetSent()
			for _, val := range c.vals {
				name := val.Name
				if val.Name == templateMetricName {
					name = expectedMetricName
				}
				m := metrics.CollectNamed(name)
				if len(m) < 1 {
					t.Errorf("metrics.CollectNamed(%s) returned no stats, expected one.\nHave metrics: %v", name, metrics)
				}
			}
		})
	}
}
