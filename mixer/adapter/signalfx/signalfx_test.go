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

	"istio.io/istio/mixer/adapter/signalfx/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

func TestGetInfo(t *testing.T) {
	i := GetInfo()
	if i.Name != "signalfx" {
		t.Fatalf("GetInfo().Name=%s; want %s", i.Name, "signalfx")
	}

	if !reflect.DeepEqual(i.SupportedTemplates, []string{metric.TemplateName}) {
		t.Fatalf("GetInfo().SupportedTemplates=%v; want %v", i.SupportedTemplates, []string{metric.TemplateName})
	}
}

var (
	metrics = map[string]*metric.Type{
		"bytes_sent":  {},
		"error_count": {},
	}
)

func TestValidation(t *testing.T) {
	f := GetInfo().NewBuilder().(*builder)

	tests := []struct {
		name          string
		conf          config.Params
		expectedError *adapter.ConfigError
	}{
		{
			"Valid Config",
			config.Params{
				AccessToken: "abcd",
				Metrics: []*config.Params_MetricConfig{
					{
						Name: "bytes_sent",
						Type: config.COUNTER,
					},
				},
			}, nil},
		{"No access token", config.Params{}, &adapter.ConfigError{"access_token", nil}},
		{"No metrics", config.Params{}, &adapter.ConfigError{"metrics", nil}},
		{
			"Unknown metric",
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
			"Unknown type",
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
			f.SetMetricTypes(metrics)
			err := f.Validate()

			if v.expectedError == nil {
				if err != nil {
					t.Fatalf("Validate() should not have produced an error")
				}
				return
			}

			errFound := false
			for _, ce := range err.Multi.WrappedErrors() {
				if ce.(adapter.ConfigError).Field == v.expectedError.Field {
					if v.expectedError.Underlying == nil || ce.(adapter.ConfigError).Underlying == v.expectedError.Underlying {
						errFound = true
					}
				}
			}

			if !errFound {
				t.Fatalf("Validate() did not produce error %v", v.expectedError)
			}
		})
	}
}
