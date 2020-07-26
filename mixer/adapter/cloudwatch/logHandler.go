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
	"html/template"
	"net"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/mixer/template/logentry"
	"istio.io/pkg/pool"
)

const (
	// Cloudwatchlogs enforced limit on number of logentries per request
	// https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_PutLogEvents.html
	batchcount = 10000

	// A default template for log entries will be used if operator doesn't supply one
	defaultTemplate = `{{or (.source_ip) "-"}} - {{or (.source_user) "-"}} ` +
		`[{{or (.timestamp.Format "2006-01-02T15:04:05Z07:00") "-"}}] "{{or (.method) "-"}} {{or (.url) "-"}} ` +
		`{{or (.protocol) "-"}}" {{or (.response_code) "-"}} {{or (.response_size) "-"}}`
)

func (h *handler) generateLogEntryData(insts []*logentry.Instance) []*cloudwatchlogs.InputLogEvent {
	logentryData := make([]*cloudwatchlogs.InputLogEvent, 0, len(insts))

	for _, inst := range insts {
		// cloudwatchlogs accepts unix timestamp in milliseconds
		timestamp := inst.Timestamp.UnixNano() / int64(time.Millisecond)

		message, err := getMessageFromVariables(h, inst)
		if err != nil {
			_ = h.env.Logger().Errorf("failed to get message for instance: %s. %v", inst.Name, err)
			continue
		}

		logEvent := cloudwatchlogs.InputLogEvent{
			Message:   aws.String(message),
			Timestamp: aws.Int64(timestamp),
		}

		logentryData = append(logentryData, &logEvent)
	}
	return logentryData
}

func getMessageFromVariables(h *handler, inst *logentry.Instance) (string, error) {
	var tmpl *template.Template
	var found bool
	if tmpl, found = h.logEntryTemplates[inst.Name]; !found {
		return "", h.env.Logger().Errorf("failed to evaluate template for log instance: %s, skipping", inst)
	}

	buf := pool.GetBuffer()

	data := inst.Variables
	data["timestamp"] = inst.Timestamp
	data["severity"] = inst.Severity

	ipval, ok := inst.Variables["source_ip"].([]byte)
	if ok {
		inst.Variables["source_ip"] = net.IP(ipval).String()
	}

	if err := tmpl.Execute(buf, data); err != nil {
		pool.PutBuffer(buf)
		return "", h.env.Logger().Errorf("failed to execute template for log '%s': %v", inst.Name, err)
	}

	payload := buf.String()
	pool.PutBuffer(buf)
	return payload, nil
}

func (h *handler) sendLogEntriesToCloudWatch(logentryData []*cloudwatchlogs.InputLogEvent) (int, error) {
	cloudWatchCallCount := 0
	var multiError *multierror.Error

	nextSequenceToken, err := getNextSequenceToken(h)
	if err != nil {
		return 0, h.env.Logger().Errorf("logentry upload failed as next upload sequence token could not be retrieved: %v", err)
	}

	for i := 0; i < len(logentryData); i += batchcount {
		size := i + batchcount
		if len(logentryData) < size {
			size = len(logentryData)
		}

		if uploadErr := h.putLogEntryData(logentryData[i:size], nextSequenceToken); uploadErr == nil {
			cloudWatchCallCount++
		} else {
			multiError = multierror.Append(multiError, uploadErr)
		}
	}
	return cloudWatchCallCount, multiError.ErrorOrNil()
}

func (h *handler) putLogEntryData(logentryData []*cloudwatchlogs.InputLogEvent, nextSequenceToken string) error {

	input := cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String(h.cfg.GetLogGroupName()),
		LogStreamName: aws.String(h.cfg.GetLogStreamName()),
		LogEvents:     logentryData,
	}

	if len(nextSequenceToken) > 0 {
		input.SetSequenceToken(nextSequenceToken)
	}

	_, err := h.cloudwatchlogs.PutLogEvents(&input)
	if err != nil {
		return h.env.Logger().Errorf("could not put logentry data into cloudwatchlogs: %v. %v", input, err)
	}
	return nil
}

func getNextSequenceToken(h *handler) (string, error) {
	input := cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName:        aws.String(h.cfg.LogGroupName),
		LogStreamNamePrefix: aws.String(h.cfg.LogStreamName),
	}

	output, err := h.cloudwatchlogs.DescribeLogStreams(&input)
	if err != nil {
		return "", h.env.Logger().Errorf("could not retrieve Log Stream info from cloudwatch: %v. %v", input, err)
	}

	for _, logStream := range output.LogStreams {
		if strings.Compare(*logStream.LogStreamName, h.cfg.LogStreamName) == 0 {
			if logStream.UploadSequenceToken != nil {
				return *logStream.UploadSequenceToken, nil
			}
		}
	}
	return "", nil
}
