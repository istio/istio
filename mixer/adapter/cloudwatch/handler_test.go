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
	"errors"
	"net"
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

	cases := []struct {
		metricData        []*cloudwatch.MetricDatum
		expectedErrString string
		handler           adapter.Handler
	}{
		{
			[]*cloudwatch.MetricDatum{},
			"metricdata",
			NewHandler(nil, env, cfg, &mockCloudWatchClient{}),
		},
		{
			[]*cloudwatch.MetricDatum{
				{MetricName: aws.String("testMetric"), Value: aws.Float64(1)},
			},
			"namespace",
			NewHandler(nil, env, &config.Params{}, &mockCloudWatchClient{}),
		},
		{
			[]*cloudwatch.MetricDatum{
				{MetricName: aws.String("testMetric"), Value: aws.Float64(1)},
			},
			"put metric data",
			NewHandler(nil, env, cfg, &mockFailCloudWatchClient{}),
		},
		{
			[]*cloudwatch.MetricDatum{
				{MetricName: aws.String("testMetric"), Value: aws.Float64(1)},
			},
			"",
			NewHandler(nil, env, cfg, &mockCloudWatchClient{}),
		},
	}

	for _, c := range cases {
		h, ok := c.handler.(*Handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		err := putMetricData(h, c.metricData)
		if len(c.expectedErrString) > 0 {
			if err == nil {
				t.Errorf("Expected an error containing %s, got none", c.expectedErrString)
			}

			if !strings.Contains(strings.ToLower(err.Error()), c.expectedErrString) {
				t.Errorf("Expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrString, err)
			}
		} else if err != nil {
			t.Errorf("Unexpected error: \"%v\"", err)
		}
	}
}

func TestGetValue(t *testing.T) {
	env := test.NewEnv(t)
	h := NewHandler(nil, env, nil, &mockCloudWatchClient{})

	cases := []struct {
		inst              *metric.Instance
		expectedErrString string
		expectedValue     float64
	}{
		{&metric.Instance{Value: "invalidValue"}, "can't parse string", 0},
		{&metric.Instance{Value: "1"}, "", 1.0},
		{&metric.Instance{Value: 1}, "", 1.0},
		{&metric.Instance{Value: 1.0}, "", 1.0},
		{&metric.Instance{Value: true}, "unsupported value type", 0},
	}

	for _, c := range cases {
		h, ok := h.(*Handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		v, err := getNumericValue(h, c.inst)
		if len(c.expectedErrString) > 0 && err == nil {
			t.Errorf("Unexpected error: \"%v\"", err)
		}

		if len(c.expectedErrString) > 0 {
			if !strings.Contains(strings.ToLower(err.Error()), c.expectedErrString) {
				t.Errorf("Expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrString, err)
			}

		} else {
			if v != c.expectedValue {
				t.Errorf("Expected value %v, got %v", c.expectedValue, v)
			}
		}
	}
}

func TestGetDimensionValue(t *testing.T) {
	env := test.NewEnv(t)
	h := NewHandler(nil, env, nil, &mockCloudWatchClient{})

	type invalidType struct{}

	cases := []struct {
		value             interface{}
		expectedErrString string
		expectedValue     string
	}{
		{"value", "", "value"},
		{int64(1), "", "1"},
		{1.1, "", "1.1E+00"},
		{true, "", "true"},
		{time.Second, "", "1s"},
		{net.IPv4(0, 0, 0, 0), "", "0.0.0.0"},
		{invalidType{}, "unsupported type", ""},
	}

	for _, c := range cases {
		h, ok := h.(*Handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		v, err := getDimensionValue(h, c.value)
		if len(c.expectedErrString) > 0 && err == nil {
			t.Errorf("Unexpected error: \"%v\"", err)
		}

		if len(c.expectedErrString) > 0 {
			if !strings.Contains(strings.ToLower(err.Error()), c.expectedErrString) {
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

	h := NewHandler(nil, env, cfg, &mockCloudWatchClient{})

	cases := []struct {
		metricData                  []*cloudwatch.MetricDatum
		expectedCloudWatchCallCount int
	}{
		{[]*cloudwatch.MetricDatum{}, 0},
		{generateTestMetricData(1), 1},
		{generateTestMetricData(21), 2},
	}

	for _, c := range cases {
		h, ok := h.(*Handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		count, err := sendMetricsToCloudWatch(h, c.metricData)
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
	cfg := &config.Params{
		Namespace: "istio-mixer-cloudwatch",
		MetricInfo: map[string]*config.Params_MetricDatum{
			"requestCount": {
				Unit:              "Count",
				StorageResolution: 1,
			},
			"requestDuration": {
				Unit: "Seconds",
			},
		},
	}
	timestamp := time.Now()
	type invalidType struct{}

	h := NewHandler(nil, env, cfg, &mockCloudWatchClient{})

	cases := []struct {
		insts              []*metric.Instance
		expectedMetricData []*cloudwatch.MetricDatum
	}{
		// empty instances
		{[]*metric.Instance{}, []*cloudwatch.MetricDatum{}},
		// no storage resolution specified
		{[]*metric.Instance{
			{
				Name:  "requestDuration",
				Value: "1"}},
			[]*cloudwatch.MetricDatum{
				{
					MetricName: aws.String("requestDuration"),
					Unit:       aws.String("Seconds"),
					Value:      aws.Float64(1),
					Dimensions: []*cloudwatch.Dimension{},
				},
			}},
		// illegal value
		{[]*metric.Instance{
			{
				Name:  "requestCount",
				Value: "invalidValue"}},
			[]*cloudwatch.MetricDatum{}},
		{
			[]*metric.Instance{
				{
					Value: "1",
					Name:  "requestCount",
				},
			},
			[]*cloudwatch.MetricDatum{
				{
					MetricName:        aws.String("requestCount"),
					Unit:              aws.String("Count"),
					StorageResolution: aws.Int64(1),
					Value:             aws.Float64(1),
					Dimensions:        []*cloudwatch.Dimension{},
				},
			},
		},
		{
			[]*metric.Instance{
				{
					Value: "1",
					Name:  "requestCount",
					Dimensions: map[string]interface{}{
						"time": timestamp,
					},
				},
			},
			[]*cloudwatch.MetricDatum{
				{
					MetricName:        aws.String("requestCount"),
					Unit:              aws.String("Count"),
					StorageResolution: aws.Int64(1),
					Value:             aws.Float64(1),
					Timestamp:         aws.Time(timestamp),
					Dimensions:        []*cloudwatch.Dimension{},
				},
			},
		},
		{
			[]*metric.Instance{
				{
					Value: "1",
					Name:  "requestCount",
					Dimensions: map[string]interface{}{
						"time":               timestamp,
						"arbitraryDimension": 50,
					},
				},
			},
			[]*cloudwatch.MetricDatum{
				{
					MetricName:        aws.String("requestCount"),
					Unit:              aws.String("Count"),
					StorageResolution: aws.Int64(1),
					Value:             aws.Float64(1),
					Timestamp:         aws.Time(timestamp),
					Dimensions: []*cloudwatch.Dimension{
						{
							Name:  aws.String("arbitraryDimension"),
							Value: aws.String("50"),
						},
					},
				},
			},
		},
		{
			[]*metric.Instance{
				{
					Value: "1",
					Name:  "requestCount",
					Dimensions: map[string]interface{}{
						"time":            timestamp,
						"unsupportedType": invalidType{},
					},
				},
			},
			[]*cloudwatch.MetricDatum{
				{
					MetricName:        aws.String("requestCount"),
					Unit:              aws.String("Count"),
					StorageResolution: aws.Int64(1),
					Value:             aws.Float64(1),
					Timestamp:         aws.Time(timestamp),
					Dimensions:        []*cloudwatch.Dimension{},
				},
			},
		},
	}

	for _, c := range cases {
		h, ok := h.(*Handler)
		if !ok {
			t.Error("Test case has the wrong type of handler.")
		}

		md := generateMetricData(h, c.insts)

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
