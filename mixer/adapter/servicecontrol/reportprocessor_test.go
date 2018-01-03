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

package servicecontrol

import (
	"context"
	"reflect"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/adapter/servicecontrol/template/servicecontrolreport"
	at "istio.io/istio/mixer/pkg/adapter/test"
)

type reportTest struct {
	inst      *servicecontrolreport.Instance
	env       *at.Env
	client    *mockSvcctrlClient
	processor *reportImpl
}

func setupReportTest(t *testing.T) *reportTest {
	requestTime, err := time.Parse(time.RFC3339, "2017-11-01T17:00:00Z")
	if err != nil {
		t.Fatalf("fail to initialize test")
	}
	responseTime, err := time.Parse(time.RFC3339, "2017-11-01T17:00:01Z")
	if err != nil {
		t.Fatalf("fail to initialize test")
	}

	mockClient := &mockSvcctrlClient{}
	mockEnv := at.NewEnv(t)
	test := &reportTest{
		inst: &servicecontrolreport.Instance{
			ApiVersion:      "v1",
			ApiOperation:    "echo.foo.bar",
			ApiProtocol:     "gRPC",
			ApiService:      "echo.googleapi.com",
			ApiKey:          "test_key",
			RequestTime:     requestTime,
			RequestMethod:   "POST",
			RequestPath:     "/blah",
			RequestBytes:    10,
			ResponseTime:    responseTime,
			ResponseCode:    200,
			ResponseBytes:   100,
			ResponseLatency: time.Duration(1) * time.Second,
		},
		env:    mockEnv,
		client: mockClient,
		processor: &reportImpl{
			env: mockEnv,
			serviceConfig: &config.GcpServiceSetting{
				MeshServiceName:   "echo",
				GoogleServiceName: "echo.googleapi.com",
			},
			client: mockClient,
			resolver: &mockConsumerProjectIDResolver{
				consumerProjectID: "project_number:123",
			},
		},
	}
	return test
}

func TestReport(t *testing.T) {
	test := setupReportTest(t)
	test.client.setReportResponse(&sc.ReportResponse{
		ServerResponse: googleapi.ServerResponse{
			HTTPStatusCode: 200,
		},
	})
	test.processor.ProcessReport(context.Background(), []*servicecontrolreport.Instance{test.inst})
	<-test.env.GetDoneChan()
	if test.client.reportRequest == nil {
		t.Error("report request failed")
	}
	expectedReport := `
		{
			"operations":[
				{
					"consumerId":"api_key:test_key",
				 	"endTime":"2017-11-01T17:00:01Z",
					"labels":{
						"/credential_id":"apiKey:test_key",
						"/protocol":"gRPC",
						"/response_code":"200",
						"/response_code_class":"2xx",
						"/status_code":"0",
						"cloud.googleapis.com/location":"global",
						"serviceruntime.googleapis.com/api_method":"echo.foo.bar",
						"serviceruntime.googleapis.com/api_version":"v1",
						"serviceruntime.googleapis.com/consumer_project": "project_number:123"
					},
					"logEntries":[
						{
							"name":"endpoints_log",
							"severity":"INFO",
							"structPayload":{
								"api_name":"echo.googleapi.com",
								"api_operation":"echo.foo.bar",
								"api_key":"test_key",
								"http_method":"POST",
								"request_size_in_bytes":10,
								"http_response_code":200,
								"request_latency_in_ms":1000,
								"timestamp":"2017-11-01T17:00:00Z",
								"location":"global",
								"log_message":"Method:echo.foo.bar"
							},
							"timestamp":"2017-11-01T17:00:00Z"
						}
					],
					"metricValueSets":[
						{
							"metricName":"serviceruntime.googleapis.com/api/producer/request_count",
							"metricValues":[
								{
								"endTime":"2017-11-01T17:00:01Z",
								"int64Value":"1",
								"startTime":"2017-11-01T17:00:00Z"
								}
							]
						},
						{
							"metricName":"serviceruntime.googleapis.com/api/producer/backend_latencies",
							"metricValues":[
								{
									"distributionValue":{
										"bucketCounts":[
											"0","0","0","0","0","0","0","0","0","0",
											"0","0","0","0","0","0","0","0","0","0",
											"1","0","0","0","0","0","0","0","0","0",
											"0"
										 ],
										"count":"1",
										"exponentialBuckets":{
											"growthFactor":2,
											"numFiniteBuckets":29,
											"scale":0.000001
										},
										"maximum":1,
										"mean":1,
										"minimum":1
									},
									"endTime":"2017-11-01T17:00:01Z",
									"startTime":"2017-11-01T17:00:00Z"
								}
							]
						},
						{
							"metricName": "serviceruntime.googleapis.com/api/producer/request_sizes",
							"metricValues": [
								{
									"distributionValue": {
										"bucketCounts": [
											"0","0","1","0","0","0","0","0","0","0"
										],
										"count": "1",
										"exponentialBuckets": {
											 "growthFactor": 10,
											 "numFiniteBuckets": 8,
											 "scale": 1
										},
										"maximum": 10,
										"mean": 10,
										"minimum": 10
									},
									"endTime": "2017-11-01T17:00:01Z",
									"startTime": "2017-11-01T17:00:00Z"
								}
							]
						},
						{
							"metricName":"serviceruntime.googleapis.com/api/producer/by_consumer/request_count",
							"metricValues":[
								{
									"endTime":"2017-11-01T17:00:01Z",
									"int64Value":"1",
									"startTime":"2017-11-01T17:00:00Z"
								}
							]
						},
						{
							"metricName":"serviceruntime.googleapis.com/api/consumer/request_count",
							"metricValues":[
								{
									"endTime":"2017-11-01T17:00:01Z",
									"int64Value":"1",
									"startTime":"2017-11-01T17:00:00Z"
								}
							]
						},
						{
							"metricName":"serviceruntime.googleapis.com/api/consumer/backend_latencies",
							"metricValues":[
								{
									"distributionValue":{
									"bucketCounts":[
										"0","0","0","0","0","0","0","0","0","0",
										"0","0","0","0","0","0","0","0","0","0",
										"1","0","0","0","0","0","0","0","0","0",
										"0"
									 ],
									"count":"1",
									"exponentialBuckets":{
									   "growthFactor":2,
									   "numFiniteBuckets":29,
									   "scale":0.000001
									},
									"maximum":1,
									"mean":1,
									"minimum":1
									},
									"endTime":"2017-11-01T17:00:01Z",
									"startTime":"2017-11-01T17:00:00Z"
								}
							]
						}
					],
					"operationName":"echo.foo.bar",
					"startTime":"2017-11-01T17:00:00Z"
				}
			]
		}`
	actualRequest := test.client.reportRequest
	if actualRequest.Operations == nil || len(actualRequest.Operations) != 1 {
		t.Error(`operation is not found or more than one operations exist in request`)
	}
	// Clear UUID in operation ID before verification
	actualRequest.Operations[0].OperationId = ""
	actualRequestJSON, _ := actualRequest.MarshalJSON()

	if !compareJSON(expectedReport, string(actualRequestJSON)) {
		t.Errorf(`expect report actualRequest: %v, but get %v`, expectedReport, string(actualRequestJSON))
	}
}

func TestNewReportProcessor(t *testing.T) {
	ctx := &handlerContext{
		env:    at.NewEnv(t),
		client: new(mockSvcctrlClient),
		serviceConfigIndex: map[string]*config.GcpServiceSetting{
			"echo": {
				MeshServiceName:   "echo",
				GoogleServiceName: "echo.googleapi.com",
			},
		},
	}
	resolver := new(mockConsumerProjectIDResolver)
	processor, err := newReportProcessor("echo", ctx, resolver)
	if err != nil {
		t.Fatalf(`fail to create reportProcessor %v`, err)
	}

	expectedProcessor := &reportImpl{
		env:           ctx.env,
		serviceConfig: ctx.serviceConfigIndex["echo"],
		client:        ctx.client,
		resolver:      resolver,
	}
	if !reflect.DeepEqual(expectedProcessor, processor) {
		t.Errorf(`expect %v but get %v`, *expectedProcessor, *processor)
	}
}
