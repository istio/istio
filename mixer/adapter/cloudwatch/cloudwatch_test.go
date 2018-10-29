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

package cloudwatch

import (
	"strings"
	"testing"

	istio_policy_v1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/template/metric"
)

func TestValidate(t *testing.T) {
	b := &builder{}

	cases := []struct {
		cfg            *config.Params
		metricTypes    map[string]*metric.Type
		expectedErrors string
	}{
		// config missing namespace
		{
			&config.Params{},
			map[string]*metric.Type{
				"metric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			"namespace",
		},
		// length of instance and handler metrics does not match
		{
			&config.Params{
				Namespace: "namespace",
			},
			map[string]*metric.Type{
				"metric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			"metricInfo",
		},
		// instance and handler metrics do not match
		{
			&config.Params{
				Namespace: "namespace",
				MetricInfo: map[string]*config.Params_MetricDatum{
					"metric": {},
				},
			},
			map[string]*metric.Type{
				"newmetric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			"metricInfo",
		},
		// validate duration metric has a duration unit
		{
			&config.Params{
				Namespace: "namespace",
				MetricInfo: map[string]*config.Params_MetricDatum{
					"duration": {
						Unit: config.Count,
					},
				},
			},
			map[string]*metric.Type{
				"duration": {
					Value: istio_policy_v1beta1.DURATION,
				},
			},
			"duration",
		},
		// validate that value can be handled by the cloudwatch handler
		{
			&config.Params{
				Namespace: "namespace",
				MetricInfo: map[string]*config.Params_MetricDatum{
					"dns": {
						Unit: config.Count,
					},
				},
			},
			map[string]*metric.Type{
				"dns": {
					Value: istio_policy_v1beta1.DNS_NAME,
				},
			},
			"value type",
		},
		// validate dimension cloudwatch_limits
		{
			&config.Params{
				Namespace: "namespace",
				MetricInfo: map[string]*config.Params_MetricDatum{
					"dns": {
						Unit: config.Count,
					},
				},
			},
			map[string]*metric.Type{
				"dns": {
					Value: istio_policy_v1beta1.STRING,
					Dimensions: map[string]istio_policy_v1beta1.ValueType{
						"d1":  istio_policy_v1beta1.STRING,
						"d2":  istio_policy_v1beta1.STRING,
						"d3":  istio_policy_v1beta1.STRING,
						"d4":  istio_policy_v1beta1.STRING,
						"d5":  istio_policy_v1beta1.STRING,
						"d6":  istio_policy_v1beta1.STRING,
						"d7":  istio_policy_v1beta1.STRING,
						"d8":  istio_policy_v1beta1.STRING,
						"d9":  istio_policy_v1beta1.STRING,
						"d10": istio_policy_v1beta1.STRING,
						"d11": istio_policy_v1beta1.STRING,
					},
				},
			},
			"dimensions",
		},
	}

	for _, c := range cases {
		b.SetMetricTypes(c.metricTypes)
		b.SetAdapterConfig(c.cfg)

		errs := b.Validate()

		if !strings.Contains(errs.Error(), c.expectedErrors) {
			t.Errorf("Expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrors, errs.Error())
		}
	}
}
