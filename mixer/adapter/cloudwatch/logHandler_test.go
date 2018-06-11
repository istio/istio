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

package cloudwatch

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs/cloudwatchlogsiface"
	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
)

type mockCloudWatchLogsClient struct {
	cloudwatchlogsiface.CloudWatchLogsAPI
	resp          *cloudwatchlogs.DescribeLogStreamsOutput
	describeError error
}

func (m *mockCloudWatchLogsClient) PutLogEvents(input *cloudwatchlogs.PutLogEventsInput) (*cloudwatchlogs.PutLogEventsOutput, error) {
	return &cloudwatchlogs.PutLogEventsOutput{}, nil
}

func (m *mockCloudWatchLogsClient) DescribeLogStreams(input *cloudwatchlogs.DescribeLogStreamsInput) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
	return m.resp, m.describeError
}

type mockFailCloudWatchLogsClient struct {
	cloudwatchlogsiface.CloudWatchLogsAPI
	resp          *cloudwatchlogs.DescribeLogStreamsOutput
	describeError error
}

func (m *mockFailCloudWatchLogsClient) PutLogEvents(input *cloudwatchlogs.PutLogEventsInput) (*cloudwatchlogs.PutLogEventsOutput, error) {
	return nil, errors.New("put logentry data failed")
}

func (m *mockFailCloudWatchLogsClient) DescribeLogStreams(input *cloudwatchlogs.DescribeLogStreamsInput) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
	return m.resp, m.describeError
}

func generateLogStreamOutput() *cloudwatchlogs.DescribeLogStreamsOutput {
	logstreams := make([]*cloudwatchlogs.LogStream, 0, 1)

	stream := cloudwatchlogs.LogStream{
		LogStreamName:       aws.String("TestLogStream"),
		UploadSequenceToken: aws.String("49579643037721145729486712515534281748446571659784111634"),
	}

	logstreams = append(logstreams, &stream)

	return &cloudwatchlogs.DescribeLogStreamsOutput{
		LogStreams: logstreams,
	}
}

func generateEmptyLogStreamOutput() *cloudwatchlogs.DescribeLogStreamsOutput {
	logstreams := make([]*cloudwatchlogs.LogStream, 0, 0)
	return &cloudwatchlogs.DescribeLogStreamsOutput{
		LogStreams: logstreams,
	}
}

func generateNilLogStreamOutput() *cloudwatchlogs.DescribeLogStreamsOutput {
	return &cloudwatchlogs.DescribeLogStreamsOutput{
		LogStreams: nil,
	}
}

func TestPutLogEntryData(t *testing.T) {
	cfg := &config.Params{
		LogGroupName:  "TestLogGroup",
		LogStreamName: "TestLogStream",
	}

	env := test.NewEnv(t)

	cases := []struct {
		logEntryData      []*cloudwatchlogs.InputLogEvent
		expectedErrString string
		handler           adapter.Handler
		nextSequenceToken string
	}{
		{
			[]*cloudwatchlogs.InputLogEvent{
				{Message: aws.String("testMessage2"), Timestamp: aws.Int64(1)},
			},
			"",
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{}),
			"49579643037721145729486712515534281748446571659784111634",
		},
		{
			[]*cloudwatchlogs.InputLogEvent{},
			"put logentry data",
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockFailCloudWatchLogsClient{}),
			"49579643037721145729486712515534281748446571659784111634",
		},
		{
			[]*cloudwatchlogs.InputLogEvent{
				{Message: aws.String("testMessage2"), Timestamp: aws.Int64(1)},
			},
			"put logentry data",
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockFailCloudWatchLogsClient{}),
			"49579643037721145729486712515534281748446571659784111634",
		},
		{
			[]*cloudwatchlogs.InputLogEvent{
				{Message: aws.String("testMessage2"), Timestamp: aws.Int64(1)},
			},
			"put logentry data",
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockFailCloudWatchLogsClient{}),
			"",
		},
	}

	for _, c := range cases {
		h, ok := c.handler.(*handler)
		if !ok {
			t.Error("test case has the wrong type of handler.")
		}

		err := h.putLogEntryData(c.logEntryData, c.nextSequenceToken)
		if len(c.expectedErrString) > 0 {
			if err == nil {
				t.Errorf("expected an error containing %s, got none", c.expectedErrString)

			} else if !strings.Contains(strings.ToLower(err.Error()), c.expectedErrString) {
				t.Errorf("expected an error containing \"%s\", actual error: \"%v\"", c.expectedErrString, err)
			}
		} else if err != nil {
			t.Errorf("unexpected error: \"%v\"", err)
		}
	}
}

func TestSendLogEntriesToCloudWatch(t *testing.T) {
	env := test.NewEnv(t)
	cfg := &config.Params{
		LogGroupName:  "TestLogGroup",
		LogStreamName: "TestLogStream",
	}

	h := newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{resp: generateLogStreamOutput(), describeError: nil})

	cases := []struct {
		logEntryData                []*cloudwatchlogs.InputLogEvent
		expectedCloudWatchCallCount int
		Handler                     adapter.Handler
	}{
		{[]*cloudwatchlogs.InputLogEvent{}, 0, h},
		{generateTestLogEntryData(1), 1, h},
		{generateTestLogEntryData(10001), 2, h},
		{
			generateTestLogEntryData(0),
			0,
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{resp: generateLogStreamOutput(), describeError: errors.New("describe logstream failed")}),
		},
		{
			generateTestLogEntryData(0),
			0,
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{resp: generateNilLogStreamOutput(), describeError: nil}),
		},
		{
			generateTestLogEntryData(1),
			0,
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockFailCloudWatchLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
		},
	}

	for _, c := range cases {
		h, ok := c.Handler.(*handler)
		if !ok {
			t.Error("test case has the wrong type of handler.")
		}

		count, _ := h.sendLogEntriesToCloudWatch(c.logEntryData)
		if count != c.expectedCloudWatchCallCount {
			t.Errorf("expected %v calls but got %v", c.expectedCloudWatchCallCount, count)
		}
	}
}

func generateTestLogEntryData(count int) []*cloudwatchlogs.InputLogEvent {
	logEntryData := make([]*cloudwatchlogs.InputLogEvent, 0, count)

	for i := 0; i < count; i++ {
		logEntryData = append(logEntryData, &cloudwatchlogs.InputLogEvent{
			Message:   aws.String("testMessage" + strconv.Itoa(i)),
			Timestamp: aws.Int64(1),
		})
	}

	return logEntryData
}

func TestGenerateLogEntryData(t *testing.T) {
	timestp := time.Now()
	env := test.NewEnv(t)
	template := `{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}}`
	cfg := &config.Params{
		LogGroupName:  "testLogGroup",
		LogStreamName: "testLogStream",
		Logs: map[string]*config.Params_LogInfo{
			"accesslog": {
				PayloadTemplate: template,
			},
		},
	}
	emptyPayloadCfg := &config.Params{
		LogGroupName:  "testLogGroup",
		LogStreamName: "testLogStream",
		Logs: map[string]*config.Params_LogInfo{
			"accesslog": {
				PayloadTemplate: "",
			},
		},
	}

	cases := []struct {
		handler              adapter.Handler
		insts                []*logentry.Instance
		expectedLogEntryData []*cloudwatchlogs.InputLogEvent
	}{
		// empty instances
		{
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
			[]*logentry.Instance{},
			[]*cloudwatchlogs.InputLogEvent{},
		},
		// non empty instances
		{
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
			[]*logentry.Instance{
				{
					Timestamp: timestp,
					Name:      "accesslog",
					Severity:  "Default",
					Variables: map[string]interface{}{
						"sourceUser": "abc",
					},
				},
			},
			[]*cloudwatchlogs.InputLogEvent{
				{
					Message:   aws.String("- - abc"),
					Timestamp: aws.Int64(timestp.UnixNano() / int64(time.Millisecond)),
				},
			},
		},
		{
			newHandler(nil, nil, env, cfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
			[]*logentry.Instance{
				{
					Timestamp: timestp,
					Name:      "accesslog",
					Severity:  "Default",
					Variables: map[string]interface{}{
						"sourceUser": "abc",
						"sourceIp":   "10.0.0.0",
					},
				},
			},
			[]*cloudwatchlogs.InputLogEvent{
				{
					Message:   aws.String("10.0.0.0 - abc"),
					Timestamp: aws.Int64(timestp.UnixNano() / int64(time.Millisecond)),
				},
			},
		},
		// payload template not provided explicitly
		{
			newHandler(nil, nil, env, emptyPayloadCfg, &mockCloudWatchClient{}, &mockCloudWatchLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
			[]*logentry.Instance{
				{
					Timestamp: timestp,
					Name:      "accesslog",
					Severity:  "Default",
					Variables: map[string]interface{}{
						"sourceUser": "abc",
						"sourceIp":   "10.0.0.0",
					},
				},
			},
			[]*cloudwatchlogs.InputLogEvent{
				{
					Message:   aws.String(""),
					Timestamp: aws.Int64(timestp.UnixNano() / int64(time.Millisecond)),
				},
			},
		},
	}

	for _, c := range cases {
		h, ok := c.handler.(*handler)
		if !ok {
			t.Error("test case has the wrong type of handler.")
		}

		ld := h.generateLogEntryData(c.insts)

		if len(c.expectedLogEntryData) != len(ld) {
			t.Errorf("expected %v logentry data items but got %v", len(c.expectedLogEntryData), len(ld))
		}

		for i := 0; i < len(c.expectedLogEntryData); i++ {
			expectedLD := c.expectedLogEntryData[i]
			actualLD := ld[i]

			if !reflect.DeepEqual(expectedLD, actualLD) {
				t.Errorf("expected %v, actual %v", expectedLD, actualLD)
			}
		}
	}
}
