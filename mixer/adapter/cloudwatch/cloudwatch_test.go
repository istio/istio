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
// WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudwatch

import (
	"reflect"
	"strings"
	"testing"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/metric"
)

func TestValidate(t *testing.T) {
	info := GetInfo()
	b := info.NewBuilder()

	cases := []struct {
		cfg            *config.Params
		expectedErrors string
	}{
		{
			&config.Params{
				Namespace: "istio-mixer-cloudwatch",
				MetricInfo: map[string]*config.Params_MetricDatum{
					"requestCount": {
						Unit:              "Count",
						StorageResolution: 10,
					},
				},
			},
			"Storage resolution should be either 1 or 60",
		},
		{
			&config.Params{
				Namespace: "istio-mixer-cloudwatch",
				MetricInfo: map[string]*config.Params_MetricDatum{
					"requestCount": {
						StorageResolution: 1,
					},
				},
			},
			"Cloudwatch metric unit must not be null",
		},
	}

	for _, c := range cases {
		b.SetAdapterConfig(c.cfg)
		errs := b.Validate()
		if !strings.Contains(errs.Error(), c.expectedErrors) {
			t.Errorf("Expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrors, errs.Error())
		}
	}

}

func TestHandleMetric(t *testing.T) {
	env := test.NewEnv(t)
	types := map[string]*metric.Type{
		"validType": {
			Value: v1beta1.STRING,
		},
	}
	cfg := &config.Params{
		MetricInfo: map[string]*config.Params_MetricDatum{
			"validType": {},
		},
	}

	cases := []struct {
		handler       adapter.Handler
		insts         []*metric.Instance
		expectedInsts []*metric.Instance
	}{
		{
			NewHandler(nil, env, nil, &mockCloudWatchClient{}),
			[]*metric.Instance{{Name: "nonExistentType", Value: "1"}},
			[]*metric.Instance{},
		},
		{
			NewHandler(types, env, nil, &mockCloudWatchClient{}),
			[]*metric.Instance{{Name: "validType", Value: "1"}},
			[]*metric.Instance{},
		},
		{
			NewHandler(types, env, cfg, &mockCloudWatchClient{}),
			[]*metric.Instance{{Name: "validType", Value: "1"}},
			[]*metric.Instance{{Name: "validType", Value: "1"}},
		},
	}

	for _, c := range cases {
		h, ok := c.handler.(*Handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		actualInsts := getValidMetrics(h, c.insts)

		if len(c.expectedInsts) != len(actualInsts) {
			t.Errorf("Expected %v instances but got %v", len(c.expectedInsts), len(actualInsts))
		}

		for i := 0; i < len(actualInsts); i++ {
			expectedInst := c.expectedInsts[i]
			actualInst := actualInsts[i]

			if !reflect.DeepEqual(expectedInst, actualInst) {
				t.Errorf("Expected %v, actual %v", expectedInst, actualInst)
			}
		}
	}
}
