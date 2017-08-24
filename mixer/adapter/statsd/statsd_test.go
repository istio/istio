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
	"strings"
	"testing"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
	"github.com/cactus/go-statsd-client/statsd/statsdtest"
	"github.com/gogo/protobuf/proto"

	"istio.io/mixer/adapter/statsd/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

func TestInvariants(t *testing.T) {
	test.AdapterInvariants(Register, t)
}

func TestNewBuilder(t *testing.T) {
	b := newBuilder()
	if err := b.Close(); err != nil {
		t.Errorf("b.Close() = %s, expected no err", err)
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		conf      proto.Message
		errString string
	}{
		{&config.Params{}, ""},
		{&config.Params{MetricNameTemplateStrings: map[string]string{"a": `{{.apiMethod}}-{{.responseCode}}`}}, ""},
		{&config.Params{MetricNameTemplateStrings: map[string]string{"badtemplate": `{{if 1}}`}}, "metricNameTemplateStrings"},
		{&config.Params{FlushDuration: -1}, "flushDuration"},
		{&config.Params{SamplingRate: -1}, "samplingRate"},
		{&config.Params{FlushBytes: -1}, "flushBytes"},
	}
	for idx, c := range cases {
		b := &builder{}
		errString := ""
		if err := b.ValidateConfig(c.conf); err != nil {
			errString = err.Error()
		}
		if !strings.Contains(errString, c.errString) {
			t.Errorf("[%d] b.ValidateConfig(c.conf) = '%s'; want errString containing '%s'", idx, errString, c.errString)
		}
	}
}

func TestNewMetricsAspect(t *testing.T) {
	conf := &config.Params{
		Address:                   "localhost:8125",
		Prefix:                    "",
		FlushDuration:             300 * time.Millisecond,
		FlushBytes:                -1,
		SamplingRate:              1.0,
		MetricNameTemplateStrings: map[string]string{"a": `{{(.apiMethod) "-" (.responseCode)}}`},
	}
	env := test.NewEnv(t)
	if _, err := newBuilder().NewMetricsAspect(env, conf, nil); err != nil {
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
		t.Errorf("wanted NewMetricsAspect(env, conf, metrics) to log about '%s', only got logs: %v", name, logs)
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
		MetricNameTemplateStrings: map[string]string{
			name:      `{{ .apiMethod "-" .responseCode }}`, // fails at execute time, not template parsing time
			"missing": "foo",
		},
	}
	metrics := []adapter.MetricDefinition{
		{
			Name:   name,
			Labels: map[string]adapter.LabelType{"apiMethod": 1, "responseCode": 2}, // we don't care about the kind
		},
	}
	env := test.NewEnv(t)
	if _, err := newBuilder().NewMetricsAspect(env, conf, makeMetricMap(metrics)); err != nil {
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
		Address:                   "localhost:8125",
		Prefix:                    "",
		FlushDuration:             300 * time.Millisecond,
		FlushBytes:                512,
		SamplingRate:              1.0,
		MetricNameTemplateStrings: map[string]string{"badtemplate": `{{if 1}}`},
	}
	metrics := []adapter.MetricDefinition{
		{Name: "badtemplate"},
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewMetricsAspect(test.NewEnv(t), config, nil) didn't panic")
		}
	}()
	if _, err := newBuilder().NewMetricsAspect(test.NewEnv(t), conf, makeMetricMap(metrics)); err != nil {
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
		MetricNameTemplateStrings: map[string]string{
			templateMetricName: `{{.apiMethod}}-{{.responseCode}}`,
		},
	}
	metrics := []adapter.MetricDefinition{
		{
			Name:   templateMetricName,
			Labels: map[string]adapter.LabelType{"apiMethod": 1, "responseCode": 2}, // we don't care about the kind
		},
	}

	d := &adapter.MetricDefinition{
		Name: "foo",
		Kind: adapter.Gauge,
	}

	hist := &adapter.MetricDefinition{
		Name: "request_duration",
		Kind: adapter.Distribution,
	}

	validGauge := adapter.Value{
		Definition:  d,
		Labels:      make(map[string]interface{}),
		StartTime:   time.Now(),
		EndTime:     time.Now(),
		MetricValue: int64(123),
	}
	invalidGauge := validGauge
	invalidGauge.MetricValue = "bar"

	d = &adapter.MetricDefinition{
		Name: "bar",
		Kind: adapter.Counter,
	}

	validCounter := adapter.Value{
		Definition:  d,
		Labels:      make(map[string]interface{}),
		StartTime:   time.Now(),
		EndTime:     time.Now(),
		MetricValue: int64(123),
	}
	invalidCounter := validCounter
	invalidCounter.MetricValue = 1.0

	requestDuration := adapter.Value{
		Definition:  hist,
		MetricValue: 146 * time.Millisecond,
	}
	invalidDistribution := adapter.Value{
		Definition:  hist,
		MetricValue: "not good",
	}
	int64Distribution := adapter.Value{
		Definition:  hist,
		MetricValue: int64(3459),
	}

	methodCodeMetric := validCounter
	methodCodeMetric.Definition = &metrics[0] // this needs to match the name in conf.MetricNameTemplateStrings
	methodCodeMetric.Labels["apiMethod"] = "methodName"
	methodCodeMetric.Labels["responseCode"] = "500"
	expectedMetricName := methodCodeMetric.Labels["apiMethod"].(string) + "-" + methodCodeMetric.Labels["responseCode"].(string)

	cases := []struct {
		vals      []adapter.Value
		errString string
	}{
		{[]adapter.Value{}, ""},
		{[]adapter.Value{validGauge}, ""},
		{[]adapter.Value{validCounter}, ""},
		{[]adapter.Value{methodCodeMetric}, ""},
		{[]adapter.Value{requestDuration}, ""},
		{[]adapter.Value{int64Distribution}, ""},
		{[]adapter.Value{validCounter, validGauge}, ""},
		{[]adapter.Value{validCounter, validGauge, methodCodeMetric}, ""},
		{[]adapter.Value{invalidCounter}, "could not record"},
		{[]adapter.Value{invalidGauge}, "could not record"},
		{[]adapter.Value{invalidDistribution}, "could not record"},
		{[]adapter.Value{validGauge, invalidGauge}, "could not record"},
		{[]adapter.Value{methodCodeMetric, invalidCounter}, "could not record"},
	}
	for idx, c := range cases {
		b := newBuilder()
		rs := statsdtest.NewRecordingSender()
		cl, err := statsd.NewClientWithSender(rs, "")
		if err != nil {
			t.Errorf("statsd.NewClientWithSender(rs, \"\") = %s; wanted no err", err)
		}
		m, err := b.NewMetricsAspect(test.NewEnv(t), conf, makeMetricMap(metrics))
		if err != nil {
			t.Errorf("[%d] newBuilder().NewMetrics(test.NewEnv(t), conf) = _, %s; wanted no err", idx, err)
			continue
		}
		// We don't have an easy handle into setting the client, so we'll just reach in and update it
		asp := m.(*aspect)
		asp.client = cl

		if err := m.Record(c.vals); err != nil {
			if c.errString == "" {
				t.Errorf("[%d] m.Record(c.vals) = %s; wanted no err", idx, err)
			}
			if !strings.Contains(err.Error(), c.errString) {
				t.Errorf("[%d] m.Record(c.vals) = %s; wanted err containing %s", idx, err.Error(), c.errString)
			}
		}
		if err := m.Close(); err != nil {
			t.Errorf("[%d] m.Close() = %s; wanted no err", idx, err)
		}
		if c.errString != "" {
			continue
		}

		metrics := rs.GetSent()
		for _, val := range c.vals {
			name := val.Definition.Name
			if val.Definition.Name == templateMetricName {
				name = expectedMetricName
			}
			m := metrics.CollectNamed(name)
			if len(m) < 1 {
				t.Errorf("[%d] metrics.CollectNamed(%s) returned no stats, expected one.\nHave metrics: %v", idx, name, metrics)
			}
		}
	}
}

func makeMetricMap(metrics []adapter.MetricDefinition) map[string]*adapter.MetricDefinition {
	m := make(map[string]*adapter.MetricDefinition, len(metrics))
	for _, metric := range metrics {
		m[metric.Name] = &metric
	}

	return m
}
