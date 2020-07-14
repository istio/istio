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
	"errors"
	"html/template"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"

	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/metric"
)

type mockCloudWatchClient struct {
	cloudwatchiface.CloudWatchAPI
}

func (m *mockCloudWatchClient) PutMetricData(input *cloudwatch.PutMetricDataInput) (*cloudwatch.PutMetricDataOutput, error) {
	return &cloudwatch.PutMetricDataOutput{}, nil
}

type mockFailCloudWatchClient struct {
	cloudwatchiface.CloudWatchAPI
}

func (m *mockFailCloudWatchClient) PutMetricData(input *cloudwatch.PutMetricDataInput) (*cloudwatch.PutMetricDataOutput, error) {
	return nil, errors.New("put metric data failed")
}

func TestPutMetricData(t *testing.T) {
	cfg := &config.Params{
		Namespace: "istio-mixer-cloudwatch",
	}

	env := test.NewEnv(t)

	logEntryTemplates := make(map[string]*template.Template)
	logEntryTemplates["inst"], _ = template.New("inst").Parse(`{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}}`)

	cases := []struct {
		metricData        []*cloudwatch.MetricDatum
		expectedErrString string
		handler           adapter.Handler
	}{
		{
			[]*cloudwatch.MetricDatum{
				{MetricName: aws.String("testMetric"), Value: aws.Float64(1)},
			},
			"put metric data",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockFailCloudWatchClient{}, &mockLogsClient{}),
		},
		{
			[]*cloudwatch.MetricDatum{
				{MetricName: aws.String("testMetric"), Value: aws.Float64(1)},
			},
			"",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{}),
		},
	}

	for _, c := range cases {
		h, ok := c.handler.(*handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		err := h.putMetricData(c.metricData)
		if len(c.expectedErrString) > 0 {
			if err == nil {
				t.Errorf("Expected an error containing %s, got none", c.expectedErrString)

			} else if !strings.Contains(strings.ToLower(err.Error()), c.expectedErrString) {
				t.Errorf("Expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrString, err)
			}
		} else if err != nil {
			t.Errorf("Unexpected error: \"%v\"", err)
		}
	}
}

func generateCfgWithUnit(unit config.Params_MetricDatum_Unit) *config.Params {
	return generateCfgWithNameAndUnit("requestDuration", unit)
}

func generateCfgWithNameAndUnit(metricName string, unit config.Params_MetricDatum_Unit) *config.Params {
	return &config.Params{
		Namespace: "istio-mixer-cloudwatch",
		MetricInfo: map[string]*config.Params_MetricDatum{
			metricName: {
				Unit: unit,
			},
		},
	}
}

func TestDurationNumericValue(t *testing.T) {
	cases := []struct {
		config        *config.Params
		inst          *metric.Instance
		expectedValue float64
	}{
		{
			generateCfgWithUnit(config.None),
			&metric.Instance{
				Name:  "requestDuration",
				Value: 1 * time.Minute},
			60000,
		},
		{
			generateCfgWithUnit(config.Seconds),
			&metric.Instance{
				Name:  "requestDuration",
				Value: 1 * time.Minute},
			60,
		},
		{
			generateCfgWithUnit(config.Milliseconds),
			&metric.Instance{
				Name:  "requestDuration",
				Value: 1 * time.Minute},
			60000,
		},
		{
			generateCfgWithUnit(config.Microseconds),
			&metric.Instance{
				Name:  "requestDuration",
				Value: 1 * time.Minute},
			60000000,
		},
	}

	for _, c := range cases {
		v, ok := c.inst.Value.(time.Duration)
		if !ok {
			t.Error("Test case instance value is of the wrong type")
		}

		unit := c.config.GetMetricInfo()["requestDuration"].GetUnit()
		n := getDurationNumericValue(v, unit)

		if n != c.expectedValue {
			t.Errorf("Expected value %v, got %v", c.expectedValue, n)
		}

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
			generateCfgWithNameAndUnit("invalid", config.None),
			"can't parse string", 0},
		{&metric.Instance{Name: "requestcount", Value: "1"},
			generateCfgWithNameAndUnit("requestcount", config.Count),
			"", 1.0},
		{&metric.Instance{Name: "requestcount", Value: 1},
			generateCfgWithNameAndUnit("requestcount", config.Count),
			"", 1.0},
		{&metric.Instance{Name: "requestcount", Value: 1.0},
			generateCfgWithNameAndUnit("requestcount", config.Count),
			"", 1.0},
		{&metric.Instance{Name: "requestcount", Value: true},
			generateCfgWithNameAndUnit("requestcount", config.Count),
			"unsupported value type", 0},
		{&metric.Instance{Name: "requestduration", Value: 1 * time.Minute},
			generateCfgWithNameAndUnit("requestduration", config.Seconds), "", 60},
	}

	for _, c := range cases {
		v, err := getNumericValue(c.inst.Value, c.config.GetMetricInfo()[c.inst.Name].GetUnit())
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

func TestSendMetricsToCloudWatch(t *testing.T) {
	env := test.NewEnv(t)
	cfg := &config.Params{
		Namespace: "istio-mixer-cloudwatch",
	}
	logEntryTemplates := make(map[string]*template.Template)
	logEntryTemplates["inst"], _ = template.New("inst").Parse(`{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}}`)

	h := newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{})

	cases := []struct {
		metricData                  []*cloudwatch.MetricDatum
		expectedCloudWatchCallCount int
	}{
		{[]*cloudwatch.MetricDatum{}, 0},
		{generateTestMetricData(1), 1},
		{generateTestMetricData(21), 2},
	}

	for _, c := range cases {
		hdr, ok := h.(*handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		count, err := hdr.sendMetricsToCloudWatch(c.metricData)
		if err != nil {
			t.Errorf("Unexpected error: \"%v\"", err)
		}
		if count != c.expectedCloudWatchCallCount {
			t.Errorf("Expected %v calls but got %v", c.expectedCloudWatchCallCount, count)
		}
	}
}

func generateTestMetricData(count int) []*cloudwatch.MetricDatum {
	metricData := make([]*cloudwatch.MetricDatum, 0, count)

	for i := 0; i < count; i++ {
		metricData = append(metricData, &cloudwatch.MetricDatum{
			MetricName: aws.String("testMetric" + strconv.Itoa(i)),
			Value:      aws.Float64(1),
		})
	}

	return metricData
}

func TestGenerateMetricData(t *testing.T) {
	env := test.NewEnv(t)
	logEntryTemplates := make(map[string]*template.Template)
	logEntryTemplates["inst"], _ = template.New("inst").Parse(`{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}}`)
	cases := []struct {
		handler            adapter.Handler
		insts              []*metric.Instance
		expectedMetricData []*cloudwatch.MetricDatum
	}{
		// empty instances
		{
			newHandler(nil, nil, logEntryTemplates, env,
				generateCfgWithUnit(config.Count),
				&mockCloudWatchClient{}, &mockLogsClient{}),
			[]*metric.Instance{}, []*cloudwatch.MetricDatum{}},
		// timestamp value
		{
			newHandler(nil, nil, logEntryTemplates, env,
				generateCfgWithNameAndUnit("requestduration", config.Milliseconds),
				&mockCloudWatchClient{}, &mockLogsClient{}),
			[]*metric.Instance{
				{
					Value: 1 * time.Minute,
					Name:  "requestduration",
				},
			},
			[]*cloudwatch.MetricDatum{
				{
					MetricName: aws.String("requestduration"),
					Unit:       aws.String(config.Milliseconds.String()),
					Value:      aws.Float64(60000),
					Dimensions: []*cloudwatch.Dimension{},
				},
			},
		},
		// count value and dimensions
		{
			newHandler(nil, nil, logEntryTemplates, env,
				generateCfgWithNameAndUnit("requestcount", config.Count),
				&mockCloudWatchClient{}, &mockLogsClient{}),
			[]*metric.Instance{
				{
					Value: "1",
					Name:  "requestcount",
					Dimensions: map[string]interface{}{
						"arbitraryDimension": 50,
					},
				},
			},
			[]*cloudwatch.MetricDatum{
				{
					MetricName: aws.String("requestcount"),
					Unit:       aws.String(config.Count.String()),
					Value:      aws.Float64(1),
					Dimensions: []*cloudwatch.Dimension{
						{
							Name:  aws.String("arbitraryDimension"),
							Value: aws.String("50"),
						},
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

		if len(c.expectedMetricData) != len(md) {
			t.Errorf("Expected %v metric data items but got %v", len(c.expectedMetricData), len(md))
		}

		for i := 0; i < len(c.expectedMetricData); i++ {
			expectedMD := c.expectedMetricData[i]
			actualMD := md[i]

			if !reflect.DeepEqual(expectedMD, actualMD) {
				t.Errorf("Expected %v, actual %v", expectedMD, actualMD)
			}
		}
	}
}
