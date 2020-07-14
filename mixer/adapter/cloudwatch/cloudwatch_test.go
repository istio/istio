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

package cloudwatch

import (
	"context"
	"strings"
	"testing"

	istio_policy_v1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
)

func TestBasic(t *testing.T) {
	info := GetInfo()

	if !containsTemplate(info.SupportedTemplates, logentry.TemplateName, metric.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	cfg := info.DefaultConfig
	b := info.NewBuilder()

	params := cfg.(*config.Params)
	params.Namespace = "default"
	params.LogGroupName = "group"
	params.LogStreamName = "stream"
	params.Logs = make(map[string]*config.Params_LogInfo)
	params.Logs["empty"] = &config.Params_LogInfo{PayloadTemplate: "   "}
	params.Logs["other"] = &config.Params_LogInfo{PayloadTemplate: `{{or (.source_ip) "-"}}`}

	b.SetAdapterConfig(cfg)

	if err := b.Validate(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if err = handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

func containsTemplate(s []string, template ...string) bool {
	found := 0
	for _, a := range s {
		for _, t := range template {
			if t == a {
				found++
			}
		}
	}
	return found == len(template)
}

func TestValidate(t *testing.T) {
	b := &builder{}

	cases := []struct {
		cfg            *config.Params
		metricTypes    map[string]*metric.Type
		logentryTypes  map[string]*logentry.Type
		expectedErrors string
	}{
		// config missing namespace
		{
			&config.Params{
				LogGroupName:  "logGroupName",
				LogStreamName: "logStreamName",
			},
			map[string]*metric.Type{
				"metric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
				},
			},
			"namespace",
		},
		// config missing logGroupName
		{
			&config.Params{
				Namespace:     "namespace",
				LogStreamName: "logStreamName",
			},
			map[string]*metric.Type{
				"metric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
				},
			},
			"log_group_name",
		},
		// config missing logStreamName
		{
			&config.Params{
				Namespace:    "namespace",
				LogGroupName: "logGroupName",
			},
			map[string]*metric.Type{
				"metric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
				},
			},
			"log_stream_name",
		},
		// length of instance and handler metrics does not match
		{
			&config.Params{
				Namespace:     "namespace",
				LogGroupName:  "logGroupName",
				LogStreamName: "logStreamName",
			},
			map[string]*metric.Type{
				"metric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
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
				LogGroupName:  "logGroupName",
				LogStreamName: "logStreamName",
			},
			map[string]*metric.Type{
				"newmetric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
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
				LogGroupName:  "logGroupName",
				LogStreamName: "logStreamName",
			},
			map[string]*metric.Type{
				"duration": {
					Value: istio_policy_v1beta1.DURATION,
				},
			},
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
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
				LogGroupName:  "logGroupName",
				LogStreamName: "logStreamName",
			},
			map[string]*metric.Type{
				"dns": {
					Value: istio_policy_v1beta1.DNS_NAME,
				},
			},
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
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
				LogGroupName:  "logGroupName",
				LogStreamName: "logStreamName",
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
			map[string]*logentry.Type{
				"logentry": {
					Variables: map[string]istio_policy_v1beta1.ValueType{
						"sourceUser": istio_policy_v1beta1.STRING,
					},
				},
			},
			"dimensions",
		},
		// config missing variables
		{
			&config.Params{
				LogGroupName:  "logGroupName",
				LogStreamName: "logStreamName",
			},
			map[string]*metric.Type{
				"metric": {
					Value: istio_policy_v1beta1.STRING,
				},
			},
			map[string]*logentry.Type{
				"logentry": {},
			},
			"namespace",
		},
	}

	for _, c := range cases {
		b.SetMetricTypes(c.metricTypes)
		b.SetLogEntryTypes(c.logentryTypes)
		b.SetAdapterConfig(c.cfg)

		errs := b.Validate()

		if !strings.Contains(errs.Error(), c.expectedErrors) {
			t.Errorf("Expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrors, errs.Error())
		}
	}
}
