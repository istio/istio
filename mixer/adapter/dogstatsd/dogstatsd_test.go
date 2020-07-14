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

package dogstatsd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/golang/protobuf/proto"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/dogstatsd/config"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/metric"
)

func TestValidateConfig(t *testing.T) {
	invalidCounter := config.Params_MetricInfo{
		Name: "unsupported",
		Type: config.COUNTER,
	}
	invalidGauge := config.Params_MetricInfo{
		Name: "unsupported",
		Type: config.GAUGE,
	}
	invalidHistogram := config.Params_MetricInfo{
		Name: "unsupported",
		Type: config.DISTRIBUTION,
	}
	invalidType := config.Params_MetricInfo{
		Name: "unsupported",
		Type: config.UNKNOWN_TYPE,
	}
	nonexistantMetric := config.Params_MetricInfo{
		Name: "nonexistant",
		Type: config.UNKNOWN_TYPE,
	}
	mtypes := map[string]*metric.Type{
		"unsupported": {Value: descriptor.VALUE_TYPE_UNSPECIFIED},
	}

	cases := []struct {
		conf      proto.Message
		errString string
	}{
		{&config.Params{}, "metricName"},
		{&config.Params{SampleRate: -1}, "sampleRate"},
		{&config.Params{SampleRate: 2}, "sampleRate"},
		{&config.Params{BufferLength: -1}, "bufferLength"},
		{&config.Params{Metrics: map[string]*config.Params_MetricInfo{"unsupported": &invalidCounter}}, "metricType"},
		{&config.Params{Metrics: map[string]*config.Params_MetricInfo{"unsupported": &invalidGauge}}, "metricType"},
		{&config.Params{Metrics: map[string]*config.Params_MetricInfo{"unsupported": &invalidHistogram}}, "metricType"},
		{&config.Params{Metrics: map[string]*config.Params_MetricInfo{"unsupported": &invalidType}}, "metricType"},
		{&config.Params{Metrics: map[string]*config.Params_MetricInfo{"nonexistant": &nonexistantMetric}}, "metricType"},
	}

	for idx, c := range cases {
		errString := ""

		info := GetInfo()
		b := info.NewBuilder().(*builder)
		b.SetAdapterConfig(c.conf)
		b.SetMetricTypes(mtypes)
		if err := b.Validate(); err != nil {
			errString = err.Error()
		}
		if !strings.Contains(errString, c.errString) {
			t.Errorf("[%d] b.ValidateConfig(c.conf) = '%s'; want errString containing '%s'", idx, errString, c.errString)
		}
	}
}

func TestBuild(t *testing.T) {
	conf := &config.Params{
		Address:      "localhost:8125",
		Prefix:       "",
		BufferLength: 0,
		GlobalTags:   nil,
		Metrics:      map[string]*config.Params_MetricInfo{},
		SampleRate:   1.0,
	}
	info := GetInfo()
	b := info.NewBuilder()
	b.SetAdapterConfig(conf)
	env := test.NewEnv(t)
	if _, err := b.Build(context.Background(), env); err != nil {
		t.Errorf("b.NewMetrics(test.NewEnv(t), conf) = %s, wanted no err", err)
	}
	logs := env.GetLogs()
	if len(logs) > 0 {
		t.Errorf("b.NewMetrics(test.NewEnv(t), conf) wanted at least 1 item logged")
	}
}

func TestBadStatsdClient(t *testing.T) {
	cases := []struct {
		conf      proto.Message
		errString string
	}{
		{&config.Params{Address: "localhost:9999999"}, ""},
		{&config.Params{Address: "nothostport"}, ""},
	}
	for idx, c := range cases {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			info := GetInfo()
			b := info.NewBuilder()
			b.SetAdapterConfig(c.conf)
			env := test.NewEnv(t)
			if _, err := b.Build(context.Background(), env); err == nil {
				t.Errorf("b.NewMetrics(test.NewEnv(t), conf) = %s, wanted an err, got none", err)
			}
			logs := env.GetLogs()
			if len(logs) < 1 {
				t.Errorf("b.NewMetrics(test.NewEnv(t), conf) wanted at least 1 item logged")
			}
		})
	}
}

type statsdTestWriter struct {
	*bytes.Buffer
}

func (statsdTestWriter) SetWriteTimeout(time.Duration) error {
	return nil
}
func (s statsdTestWriter) Close() error {
	s.Reset()
	return nil
}

func TestRecord(t *testing.T) {
	conf := &config.Params{
		Address:      "localhost:8125",
		Prefix:       "testing.",
		BufferLength: 1,
		GlobalTags:   map[string]string{"global": "tag"},
		SampleRate:   1.0,
		Metrics: map[string]*config.Params_MetricInfo{
			"counter":      {Name: "counter", Type: config.COUNTER, Tags: map[string]string{"tag": "value"}},
			"distribution": {Name: "distribution", Type: config.DISTRIBUTION},
			"gauge":        {Name: "gauge", Type: config.GAUGE},
			"unknown":      {Name: "unknown", Type: config.UNKNOWN_TYPE},
			"dimension":    {Name: "dimension", Type: config.COUNTER},
		},
	}

	metrics := map[string]*metric.Type{
		"counter":      {},
		"distribution": {},
		"gauge":        {},
		"unknown":      {},
	}
	validGauge := metric.Instance{
		Name:       "gauge",
		Value:      int64(123),
		Dimensions: make(map[string]interface{}),
	}

	validCounter := metric.Instance{
		Name:       "counter",
		Value:      int64(456),
		Dimensions: make(map[string]interface{}),
	}
	requestDuration := &metric.Instance{
		Name:  "distribution",
		Value: 146 * time.Millisecond,
	}
	int64Distribution := &metric.Instance{
		Name:  "distribution",
		Value: int64(3459),
	}
	unknownMetric := metric.Instance{
		Name: "unknown",
	}
	dimensionMetric := &metric.Instance{
		Name:       "dimension",
		Value:      int64(1),
		Dimensions: make(map[string]interface{}),
	}
	dimensionMetric.Dimensions["response"] = int64(200)

	cases := []struct {
		vals     []*metric.Instance
		expected string
	}{
		{[]*metric.Instance{&validGauge}, "testing.gauge:123.000000|g|#global:tag"},
		{[]*metric.Instance{&validCounter}, "testing.counter:456|c|#global:tag,tag:value"},
		{[]*metric.Instance{requestDuration}, "testing.distribution:146.000000|ms|#global:tag"},
		{[]*metric.Instance{int64Distribution}, "testing.distribution:3459.000000|h|#global:tag"},
		{[]*metric.Instance{&unknownMetric}, "unknown metric type"},
		{[]*metric.Instance{dimensionMetric}, "testing.dimension:1|c|#global:tag,response:200"},
	}
	for idx, c := range cases {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			info := GetInfo()
			b := info.NewBuilder().(*builder)
			b.SetAdapterConfig(conf)
			b.SetMetricTypes(metrics)

			h, err := b.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Fatalf("newBuilder().NewMetrics(test.NewEnv(t), conf) = _, %s; wanted no err", err)
			}

			var buf []byte
			w := statsdTestWriter{bytes.NewBuffer(buf)}
			client, _ := statsd.NewWithWriter(w)
			client.Namespace = b.adapterConfig.Prefix
			client.Tags = flattenTags(b.adapterConfig.GlobalTags)
			asp := h.(*handler)
			// Inject the default client with the one we've built for testing
			asp.client = client

			err = asp.HandleMetric(context.Background(), c.vals)
			if err != nil {
				if !strings.Contains(err.Error(), c.expected) {
					t.Errorf("[%d] m.Record(c.vals) Received error: %s didn't match the expected error: %s", idx, err, c.expected)
				}
			} else {
				// confirm the metric in our buffer matches what we expect
				if w.String() != c.expected {
					t.Errorf("[%d] w.String() Received metric: %s didn't match the expected metric: %s", idx, w.String(), c.expected)
				}
			}
			if err = h.Close(); err != nil {
				t.Errorf("[%d] h.Close() Unable to close metrics handler: %s", idx, err)
			}
		})
	}
}
