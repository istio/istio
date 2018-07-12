// Copyright 2018 Istio Authors
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

package signalfx

import (
	"reflect"
	"testing"
	"time"

	"github.com/signalfx/golib/errors"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/signalfx/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/tracespan"
)

func TestGetInfo(t *testing.T) {
	i := GetInfo()
	if i.Name != "signalfx" {
		t.Fatalf("GetInfo().Name=%s; want %s", i.Name, "signalfx")
	}

	want := []string{metric.TemplateName, tracespan.TemplateName}

	if !reflect.DeepEqual(i.SupportedTemplates, want) {
		t.Fatalf("GetInfo().SupportedTemplates=%v; want %v", i.SupportedTemplates, want)
	}
}

var (
	metrics = map[string]*metric.Type{
		"bytes_sent": {
			Value: v1beta1.INT64,
		},
		"error_count": {
			Value: v1beta1.INT64,
		},
	}

	badMetrics = map[string]*metric.Type{
		"service_status": {
			Value: v1beta1.STRING,
		},
		"error_count": {
			Value: v1beta1.INT64,
		},
	}
)

func TestValidation(t *testing.T) {
	info := GetInfo()
	f := info.NewBuilder().(*builder)

	tests := []struct {
		name          string
		metricTypes   map[string]*metric.Type
		conf          config.Params
		expectedError *adapter.ConfigError
	}{
		{
			"Valid Config",
			metrics,
			config.Params{
				AccessToken:       "abcd",
				DatapointInterval: 5 * time.Second,
				TracingBufferSize: 1000,
				Metrics: []*config.Params_MetricConfig{
					{
						Name: "bytes_sent",
						Type: config.COUNTER,
					},
					{
						Name: "error_count",
						Type: config.COUNTER,
					},
				},
			}, nil},
		{"No access token", metrics, config.Params{}, &adapter.ConfigError{"access_token", nil}},
		{
			"Malformed ingest URI",
			metrics,
			config.Params{
				IngestUrl: "a;//asdf$%^:abb",
			},
			&adapter.ConfigError{"ingest_url", nil}},
		{"No metrics", metrics, config.Params{}, &adapter.ConfigError{"metrics", nil}},
		{
			"Unknown Istio metric",
			metrics,
			config.Params{
				AccessToken: "abcd",
				Metrics: []*config.Params_MetricConfig{
					{
						Name: "unknown",
						Type: config.COUNTER,
					},
				},
			}, &adapter.ConfigError{"metrics[0].name", nil}},
		{
			"Omitted Istio metric",
			metrics,
			config.Params{
				AccessToken: "abcd",
				Metrics: []*config.Params_MetricConfig{
					{
						Name: "error_count",
						Type: config.COUNTER,
					},
				},
			}, &adapter.ConfigError{"metrics", nil}},
		{
			"Non-numeric Istio metric value",
			badMetrics,
			config.Params{
				AccessToken: "abcd",
				Metrics: []*config.Params_MetricConfig{
					{
						Name: "service_status",
						Type: config.COUNTER,
					},
				},
			}, &adapter.ConfigError{"metrics[0]", errors.New("istio metric's value should be numeric but is STRING")}},
		{
			"Unknown SignalFx type",
			metrics,
			config.Params{
				AccessToken: "abcd",
				Metrics: []*config.Params_MetricConfig{
					{
						Name: "bytes_sent",
						Type: config.NONE,
					},
				},
			}, &adapter.ConfigError{"metrics[0].type", nil}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f.SetAdapterConfig(&v.conf)
			f.SetMetricTypes(v.metricTypes)
			err := f.Validate()

			if v.expectedError == nil {
				if err != nil {
					t.Fatalf("Validate() should not have produced this error: %s", err)
				}
				return
			}

			errFound := false
			for _, ce := range err.Multi.WrappedErrors() {
				if ce.(adapter.ConfigError).Field == v.expectedError.Field {
					if v.expectedError.Underlying == nil ||
						ce.(adapter.ConfigError).Underlying.Error() == v.expectedError.Underlying.Error() {
						errFound = true
					}
				}
			}

			if !errFound {
				t.Fatalf("Validate() did not produce error %v\nbut did produce: %v", v.expectedError, err)
			}
		})
	}
}
