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
// WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
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
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs/cloudwatchlogsiface"

	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
)

type mockLogsClient struct {
	cloudwatchlogsiface.CloudWatchLogsAPI
	resp          *cloudwatchlogs.DescribeLogStreamsOutput
	describeError error
}

func (m *mockLogsClient) PutLogEvents(input *cloudwatchlogs.PutLogEventsInput) (*cloudwatchlogs.PutLogEventsOutput, error) {
	return &cloudwatchlogs.PutLogEventsOutput{}, nil
}

func (m *mockLogsClient) DescribeLogStreams(input *cloudwatchlogs.DescribeLogStreamsInput) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
	return m.resp, m.describeError
}

type failLogsClient struct {
	cloudwatchlogsiface.CloudWatchLogsAPI
	resp          *cloudwatchlogs.DescribeLogStreamsOutput
	describeError error
}

func (m *failLogsClient) PutLogEvents(input *cloudwatchlogs.PutLogEventsInput) (*cloudwatchlogs.PutLogEventsOutput, error) {
	return nil, errors.New("put logentry data failed")
}

func (m *failLogsClient) DescribeLogStreams(input *cloudwatchlogs.DescribeLogStreamsInput) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
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

	logEntryTemplates := make(map[string]*template.Template)
	logEntryTemplates["accesslog"], _ = template.New("accesslog").Parse(`{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}}`)

	cases := []struct {
		name              string
		logEntryData      []*cloudwatchlogs.InputLogEvent
		expectedErrString string
		handler           adapter.Handler
		nextSequenceToken string
	}{
		{
			"testValidPutLogEntryData",
			[]*cloudwatchlogs.InputLogEvent{
				{Message: aws.String("testMessage2"), Timestamp: aws.Int64(1)},
			},
			"",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{}),
			"49579643037721145729486712515534281748446571659784111634",
		},
		{
			"testEmptyLogEntryData",
			[]*cloudwatchlogs.InputLogEvent{},
			"put logentry data",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &failLogsClient{}),
			"49579643037721145729486712515534281748446571659784111634",
		},
		{
			"testFailLogsClient",
			[]*cloudwatchlogs.InputLogEvent{
				{Message: aws.String("testMessage2"), Timestamp: aws.Int64(1)},
			},
			"put logentry data",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &failLogsClient{}),
			"49579643037721145729486712515534281748446571659784111634",
		},
		{
			"testNoSequenceToken",
			[]*cloudwatchlogs.InputLogEvent{
				{Message: aws.String("testMessage2"), Timestamp: aws.Int64(1)},
			},
			"put logentry data",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &failLogsClient{}),
			"",
		},
	}

	for _, c := range cases {
		h, ok := c.handler.(*handler)
		if !ok {
			t.Errorf("test case: %s has the wrong type of handler.", c.name)
		}

		err := h.putLogEntryData(c.logEntryData, c.nextSequenceToken)
		if len(c.expectedErrString) > 0 {
			if err == nil {
				t.Errorf("putLogEntryData() for test case: %s did not produce expected error: %s", c.name, c.expectedErrString)

			} else if !strings.Contains(strings.ToLower(err.Error()), c.expectedErrString) {
				t.Errorf("putLogEntryData() for test case: %s returned error message %s, wanted %s", c.name, err, c.expectedErrString)
			}
		} else if err != nil {
			t.Errorf("putLogEntryData() for test case: %s generated unexpected error: %v", c.name, err)
		}
	}
}

func TestSendLogEntriesToCloudWatch(t *testing.T) {
	env := test.NewEnv(t)
	cfg := &config.Params{
		LogGroupName:  "TestLogGroup",
		LogStreamName: "TestLogStream",
	}
	logEntryTemplates := make(map[string]*template.Template)
	logEntryTemplates["accesslog"], _ = template.New("accesslog").Parse(`{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}}`)

	h := newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{resp: generateLogStreamOutput(), describeError: nil})

	cases := []struct {
		name                        string
		logEntryData                []*cloudwatchlogs.InputLogEvent
		expectedCloudWatchCallCount int
		Handler                     adapter.Handler
	}{
		{"testNilLogEntryData", []*cloudwatchlogs.InputLogEvent{}, 0, h},
		{"testMockLogsClient", generateTestLogEntryData(1), 1, h},
		{"testBatchCountLimit", generateTestLogEntryData(10001), 2, h},
		{
			"testLogstreamDescribeFailure",
			generateTestLogEntryData(0),
			0,
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{
				resp: generateLogStreamOutput(), describeError: errors.New("describe logstream failed"),
			}),
		},
		{
			"testNilOutputStream",
			generateTestLogEntryData(0),
			0,
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{resp: generateNilLogStreamOutput(), describeError: nil}),
		},
		{
			"testFailLogsClient",
			generateTestLogEntryData(1),
			0,
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &failLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
		},
	}

	for _, c := range cases {
		h, ok := c.Handler.(*handler)
		if !ok {
			t.Errorf("test case: %s has the wrong type of handler.", c.name)
		}

		count, _ := h.sendLogEntriesToCloudWatch(c.logEntryData)
		if count != c.expectedCloudWatchCallCount {
			t.Errorf("sendLogEntriesToCloudWatch() for test case: %s resulted in %d calls; wanted %d", c.name, count, c.expectedCloudWatchCallCount)
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
	tmpl := `{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}}`
	cfg := &config.Params{
		LogGroupName:  "testLogGroup",
		LogStreamName: "testLogStream",
		Logs: map[string]*config.Params_LogInfo{
			"accesslog": {
				PayloadTemplate: tmpl,
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
	logEntryTemplates := make(map[string]*template.Template)
	logEntryTemplates["accesslog"], _ = template.New("accesslog").Parse(tmpl)

	emptyTemplateMap := make(map[string]*template.Template)
	emptyTemplateMap["accesslog"], _ = template.New("accesslog").Parse("")

	cases := []struct {
		name                 string
		handler              adapter.Handler
		insts                []*logentry.Instance
		expectedLogEntryData []*cloudwatchlogs.InputLogEvent
	}{
		// empty instances
		{
			"testEmptyInstance",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
			[]*logentry.Instance{},
			[]*cloudwatchlogs.InputLogEvent{},
		},
		// non empty instances
		{
			"testNonEmptyInstance",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
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
			"testMultipleVariables",
			newHandler(nil, nil, logEntryTemplates, env, cfg, &mockCloudWatchClient{}, &mockLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
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
			"testEmptyTemplate",
			newHandler(nil, nil, emptyTemplateMap, env, emptyPayloadCfg, &mockCloudWatchClient{}, &mockLogsClient{resp: generateLogStreamOutput(), describeError: nil}),
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
			t.Errorf("test case: %s has the wrong type of handler.", c.name)
		}

		ld := h.generateLogEntryData(c.insts)

		if len(c.expectedLogEntryData) != len(ld) {
			t.Errorf("generateLogEntryData() for test case: %s generated %d items; wanted %d", c.name, len(ld), len(c.expectedLogEntryData))
		}

		for i := 0; i < len(c.expectedLogEntryData); i++ {
			expectedLD := c.expectedLogEntryData[i]
			actualLD := ld[i]

			if !reflect.DeepEqual(expectedLD, actualLD) {
				t.Errorf("generateLogEntryData() for test case: %s generated %v; wanted %v", c.name, actualLD, expectedLD)
			}
		}
	}
}
