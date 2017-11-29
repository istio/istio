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
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package svcctrl

import (
	"reflect"
	"testing"
	"time"

	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/svcctrl/template/svcctrlreport"
)

func getTestReportBuilder() *reportBuilder {
	requestTime, _ := time.Parse(time.RFC3339Nano, "2017-10-21T17:09:05.000Z")
	responseTime, _ := time.Parse(time.RFC3339Nano, "2017-10-21T17:09:05.100Z")

	return &reportBuilder{
		supportedMetrics: []metricDef{
			{
				name:           "test_request_count",
				valueGenerator: generateRequestCount,
				labels: []string{
					"/consumer_id",
					"/credential_id",
					"/error_type",
					"/protocol",
					"/response_code",
					"/response_code_class",
					"/status_code",
				},
			},
			{
				name:           "test_request_error_count",
				valueGenerator: generateErrorCount,
				labels:         []string{},
			},
			{
				name:           "test_request_size",
				valueGenerator: generateRequestSize,
				labels:         []string{},
			},
			{
				name:           "test_backend_latency",
				valueGenerator: generateBackendLatencies,
				labels:         []string{},
			},
		},
		instance: &svcctrlreport.Instance{
			ApiVersion:      "v1.0",
			ApiOperation:    "echo",
			ApiProtocol:     "REST",
			ApiService:      "echo.test.com",
			ApiKey:          "test_key",
			RequestTime:     requestTime,
			RequestMethod:   "POST",
			RequestPath:     "echo.test.com/echo",
			RequestBytes:    10,
			ResponseTime:    responseTime,
			ResponseCode:    200,
			ResponseBytes:   1024,
			ResponseLatency: 1 * time.Microsecond,
		},
		resolver: &mockConsumerProjectIDResolver{
			consumerProjectID: "test_consumer_project",
		},
	}
}

func TestBuildLogEntry(t *testing.T) {
	rb := getTestReportBuilder()
	op := &sc.Operation{}
	rb.addLogEntry(op)

	if len(op.LogEntries) != 1 {
		t.Errorf(`len(op.LogEntries) != 1`)
	}

	expected :=
		`{
			"api_name":"echo.test.com",
			"api_operation":"echo",
			"api_key":"test_key",
			"http_method":"POST",
			"request_size_in_bytes":10,
			"http_response_code":200,
			"timestamp":"2017-10-21T17:09:05Z",
			"location":"global",
			"log_message":"Method:echo"
		}`

	actual := string(op.LogEntries[0].StructPayload)
	if !compareJSON(expected, actual) {
		t.Errorf("expect payload %v, but get %v", expected, actual)
	}
}

func TestBuildMetricValue(t *testing.T) {
	rb := getTestReportBuilder()
	op := &sc.Operation{}
	rb.addMetricValues(op)
	expected :=
		`{
			"labels":{
				"cloud.googleapis.com/location":"global",
				"serviceruntime.googleapis.com/api_method":"echo",
				"serviceruntime.googleapis.com/api_version":"v1.0",
				"serviceruntime.googleapis.com/consumer_project":"test_consumer_project",
				"/consumer_id":"api_key:test_key",
				"/credential_id":"apiKey:test_key",
				"/protocol":"REST",
				"/response_code":"200",
				"/response_code_class":"2xx",
				"/status_code":"0"
			},
			"metricValueSets":[
				{
					"metricName":"test_request_count",
					"metricValues":[
						{
							"endTime":"2017-10-21T17:09:05.1Z",
							"int64Value":"1",
							"startTime":"2017-10-21T17:09:05Z"
						}
					]
				},
				{
					"metricName":"test_request_size",
					"metricValues":[
						{
						   "distributionValue":{
							  "bucketCounts":[
									"0","0","1","0","0","0","0","0","0","0"
							  ],
							  "count":"1",
							  "exponentialBuckets":{
								 "growthFactor":10,
								 "numFiniteBuckets":8,
								 "scale":1
							  },
							  "maximum":10,
							  "mean":10,
							  "minimum":10
						   },
						   "endTime":"2017-10-21T17:09:05.1Z",
						   "startTime":"2017-10-21T17:09:05Z"
						}
					]
				},
				{
					"metricName":"test_backend_latency",
					"metricValues":[
						{
							"distributionValue":{
							"bucketCounts":[
								"0","1","0","0","0","0","0","0","0","0",
								"0","0","0","0","0","0","0","0","0","0",
								"0","0","0","0","0","0","0","0","0","0",
								"0"],
							"count":"1",
							"exponentialBuckets":{
								"growthFactor":2,
								"numFiniteBuckets":29,
								"scale":0.000001
								},
								"maximum":0.000001,
								"mean":0.000001,
								"minimum":0.000001
							},
							"endTime":"2017-10-21T17:09:05.1Z",
							"startTime":"2017-10-21T17:09:05Z"
							}
					]
				}
			]
		}`
	actual, _ := op.MarshalJSON()
	if !compareJSON(expected, string(actual)) {
		t.Errorf(`expect Operation: %v but get %v`, expected, string(actual))
	}
}

func TestGenerateConsumerID(t *testing.T) {
	rb := getTestReportBuilder()
	id, ok := generateConsumerID(rb.instance)
	if !ok || id != "api_key:test_key" {
		t.Errorf(`unexpected consumer ID: (%v, %v)`, id, ok)
	}
	rb.instance.ApiKey = ""
	id, ok = generateConsumerID(rb.instance)
	if ok || id != "" {
		t.Errorf(`unexpected consumer ID: (%v, %v)`, id, ok)
	}
}

func TestGenerateCredentialID(t *testing.T) {
	rb := getTestReportBuilder()
	id, ok := generateCredentialID(rb.instance)
	if !ok || id != "apiKey:test_key" {
		t.Errorf(`unexpected credential ID: (%v, %v)`, id, ok)
	}
	rb.instance.ApiKey = ""
	id, ok = generateCredentialID(rb.instance)
	if ok || id != "" {
		t.Errorf(`unexpected credential ID: (%v, %v)`, id, ok)
	}
}

func TestGenerateErrorType(t *testing.T) {
	rb := getTestReportBuilder()
	errType, ok := generateErrorType(rb.instance)
	if ok || errType != "" {
		t.Error(`expect no error, but an error type is returned`)
	}
	rb.instance.ResponseCode = 500
	errType, ok = generateErrorType(rb.instance)
	if !ok || errType != "5xx" {
		t.Errorf(`expect error type: 5xx, but get (%v, %v)`, errType, ok)
	}
}

func TestGenerateProtocol(t *testing.T) {
	rb := getTestReportBuilder()
	protocol, ok := generateProtocol(rb.instance)
	if !ok || protocol != "REST" {
		t.Errorf(`expect REST, but get (%v, %v)`, protocol, ok)
	}
}

func TestGenerateResponseCodeClass(t *testing.T) {
	rb := getTestReportBuilder()
	class, ok := generateResponseCodeClass(rb.instance)
	if !ok || class != "2xx" {
		t.Errorf(`expect 2xx class, but get (%v, %v)`, class, ok)
	}
}

func TestGenerateLogSeverity(t *testing.T) {
	severity := generateLogSeverity(399)
	if severity != endPointsLogSeverityInfo {
		t.Errorf(`unexpected serverity %v`, severity)
	}
	severity = generateLogSeverity(400)
	if severity != endPointsLogSeverityError {
		t.Errorf(`unexpected serverity %v`, severity)
	}
}

func TestGenerateLogMessage(t *testing.T) {
	rb := getTestReportBuilder()
	msg := generateLogMessage(rb.instance)
	if msg != endPointsMessage+rb.instance.ApiOperation {
		t.Errorf(`unexpected log message: %v`, msg)
	}
	rb.instance.ApiOperation = ""
	msg = generateLogMessage(rb.instance)
	if msg != "" {
		t.Errorf(`unexpected log message: %v`, msg)
	}
}

func TestGenerateLogErrorCause(t *testing.T) {
	rb := getTestReportBuilder()
	if generateLogErrorCause(rb.instance) != "" {
		t.Error(`expect no error cause when HTTP code = 200`)
	}
	rb.instance.ResponseCode = 403
	if generateLogErrorCause(rb.instance) != endPointsLogErrorCauseAuth {
		t.Error(`expect to get auth error when HTTP code = 403`)
	}
	rb.instance.ResponseCode = 500
	if generateLogErrorCause(rb.instance) != endPointsLogErrorCauseApplication {
		t.Error(`expect to get application error when HTTP code = 500`)
	}
}

func TestNewReportBuilder(t *testing.T) {
	instance := new(svcctrlreport.Instance)
	metrics := []metricDef{
		{
			name:           "test_metric",
			valueGenerator: generateRequestCount,
			labels: []string{
				"/status_code",
			},
		},
	}
	resolver := new(mockConsumerProjectIDResolver)
	builder := newReportBuilder(instance, metrics, resolver)
	expectedBuilder := reportBuilder{
		metrics,
		instance,
		resolver,
	}
	if builder == nil || !reflect.DeepEqual(expectedBuilder, *builder) {
		t.Errorf(`expect to get %v, but get %v`, expectedBuilder, *builder)
	}
}
