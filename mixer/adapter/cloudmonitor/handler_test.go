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
// WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudmonitor

import (
	"strings"
	"testing"
	"istio.io/istio/mixer/adapter/cloudmonitor/config"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/pkg/adapter"
	"reflect"
	"istio.io/istio/mixer/pkg/adapter/test"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cms"
	"encoding/json"
)

type mockCloudMonitorClient struct {
	cms.Client
}


type mockFailCloudMonitorClient struct {
	cms.Client
}



func TestGenerateMetricData(t *testing.T) {
	env := test.NewEnv(t)
	cases := []struct {
		handler            adapter.Handler
		insts              []*metric.Instance
		expectedMetricData []*CustomMetricRequest
	}{
		// empty instances
		{
			newHandler(nil, env,
				generateCfgWithName("requestcount"),
				nil),
			[]*metric.Instance{}, []*CustomMetricRequest{}},
		// count value and dimensions
		{
			newHandler(nil, env,
				generateCfgWithName("requestcount"),
				nil),
			[]*metric.Instance{
				{
					Value: "1",
					Name:  "requestcount",
					Dimensions: map[string]interface{}{
						"arbitraryDimension": 50.0,
					},
				},
			},
			[]*CustomMetricRequest{
				{
					GroupId: 1,
					MetricName: "requestcount",
					Dimensions: map[string]interface{}{
						"arbitraryDimension": 50.0,
					},
					Type: 0,
					Period: 60,
					Values:     map[string]interface{}{
						"value": 1.0,
					},

				},
			},
		},
	}

	for _, c := range cases {
		h, ok := c.handler.(*handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		md := h.generateMetricData(c.insts)
		var metricList []CustomMetricRequest
		json.Unmarshal([]byte(md.MetricList), &metricList)


		if len(c.expectedMetricData) != len(metricList) {
			t.Errorf("Expected %v metric data items but got %v", len(c.expectedMetricData), len(metricList))
		}

		for i := 0; i < len(c.expectedMetricData); i++ {
			expectedMD := c.expectedMetricData[i]
			actualMD := metricList[i]

			if !reflect.DeepEqual(expectedMD.Dimensions, actualMD.Dimensions) {
				t.Errorf("Expected %v, actual %v", expectedMD.Dimensions, actualMD.Dimensions)
			}
			if !reflect.DeepEqual(expectedMD.Values, actualMD.Values) {
				t.Errorf("Expected %v, actual %v", expectedMD.Values, actualMD.Values)
			}
		}
	}
}

func generateCfgWithName(metricName string) *config.Params {
	return &config.Params{
		MetricInfo: map[string]*config.Params_MetricList{
			metricName: {

			},
		},
	}
}

func TestGetNumericValue(t *testing.T) {
	cases := []struct {
		inst              *metric.Instance
		config            *config.Params
		expectedErrString string
		expectedValue     float64
	}{
		{&metric.Instance{Name: "invalid", Value: "invalidValue"},
			generateCfgWithName("invalid"),
			"can't parse string", 0},
		{&metric.Instance{Name: "requestcount", Value: "1"},
			generateCfgWithName("requestcount"),
			"", 1.0},
		{&metric.Instance{Name: "requestcount", Value: 1},
			generateCfgWithName("requestcount"),
			"", 1.0},
		{&metric.Instance{Name: "requestcount", Value: 1.0},
			generateCfgWithName("requestcount"),
			"", 1.0},
		{&metric.Instance{Name: "requestcount", Value: true},
			generateCfgWithName("requestcount"),
			"unsupported value type", 0},
	}

	for _, c := range cases {
		v, err := getNumericValue(c.inst.Value)
		if len(c.expectedErrString) == 0 && err != nil {
			t.Errorf("Unexpected error: \"%v\"", err)
		}

		if len(c.expectedErrString) > 0 {
			if err == nil {
				t.Errorf("Expected an error containing \"%s\", but no error was thrown", c.expectedErrString)

			} else if !strings.Contains(strings.ToLower(err.Error()), c.expectedErrString) {
				t.Errorf("Expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrString, err)
			}

		} else {
			if v != c.expectedValue {
				t.Errorf("Expected value %v, got %v", c.expectedValue, v)
			}
		}
	}
}

