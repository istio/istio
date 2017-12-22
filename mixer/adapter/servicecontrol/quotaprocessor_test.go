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

	pbtypes "github.com/gogo/protobuf/types"
	sc "google.golang.org/api/servicecontrol/v1"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/pkg/adapter"
	at "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/quota"
)

type quotaTest struct {
	processor *quotaImpl
	client    *mockSvcctrlClient
	instance  quota.Instance
}

type quotaTestCase struct {
	args            adapter.QuotaArgs
	expectedRequest string
	response        *sc.AllocateQuotaResponse
	expectedResult  adapter.QuotaResult
}

func setupQuotaTest(t *testing.T) *quotaTest {
	svcCfg := &config.GcpServiceSetting{
		Quotas: []*config.Quota{
			{
				"ratelimit.quota.istio-system",
				"read-requests",
				&pbtypes.Duration{
					Seconds: 60,
				},
			},
		},
	}

	mockClient := &mockSvcctrlClient{}
	ctx := &handlerContext{
		env: at.NewEnv(t),
		serviceConfigIndex: map[string]*config.GcpServiceSetting{
			svcCfg.MeshServiceName: svcCfg,
		},
		client: mockClient,
	}

	proc, err := newQuotaProcessor(svcCfg.MeshServiceName, ctx)
	if err != nil {
		t.Fatalf("fail to create test quota processor: %v", err)
	}

	return &quotaTest{
		proc,
		mockClient,
		quota.Instance{
			Name: "ratelimit.quota.istio-system",
			Dimensions: map[string]interface{}{
				"api_key":       "api-key",
				"api_operation": "echo",
			},
		},
	}
}

func TestProcessQuota(t *testing.T) {
	testCases := []*quotaTestCase{
		{
			args: adapter.QuotaArgs{
				DeduplicationID: "-1-",
				QuotaAmount:     25,
				BestEffort:      true,
			},
			expectedRequest: `
				{
				  "consumerId":"api_key:api-key",
				  "methodName":"echo",
				  "quotaMetrics":[
					 {
						"metricName":"read-requests",
						"metricValues":[
						   {
							  "int64Value":"25"
						   }
						]
					 }
				  ],
				  "quotaMode":"BEST_EFFORT"
				}`,
			response: &sc.AllocateQuotaResponse{
				OperationId: "cc56c90f-155f-4907-9461-64ed764fcbc1",
				QuotaMetrics: []*sc.MetricValueSet{
					{
						MetricName: "serviceruntime.googleapis.com/api/consumer/quota_used_count",
						MetricValues: []*sc.MetricValue{
							{
								Int64Value: getInt64Address(10),
								Labels: map[string]string{
									"/quota_name": "read-requests",
								},
							},
						},
					},
				},
			},
			expectedResult: createQuotaResult(status.OK, time.Duration(60)*time.Second, 10),
		},
		{
			args: adapter.QuotaArgs{
				DeduplicationID: "-2-",
				QuotaAmount:     15,
			},
			expectedRequest: `
				{
				  "consumerId":"api_key:api-key",
				  "methodName":"echo",
				  "quotaMetrics":[
					 {
						"metricName":"read-requests",
						"metricValues":[
						   {
							  "int64Value":"15"
						   }
						]
					 }
				  ],
				  "quotaMode":"NORMAL"
				}`,
			response: &sc.AllocateQuotaResponse{
				OperationId: "cc56c90f-155f-4907-9461-64ed764fcbc1",
				QuotaMetrics: []*sc.MetricValueSet{
					{
						MetricName: "serviceruntime.googleapis.com/api/consumer/quota_used_count",
						MetricValues: []*sc.MetricValue{
							{
								Int64Value: getInt64Address(15),
								Labels: map[string]string{
									"/quota_name": "read-requests",
								},
							},
						},
					},
				},
			},
			expectedResult: createQuotaResult(status.OK, time.Duration(60)*time.Second, 15),
		},
		{
			args: adapter.QuotaArgs{
				DeduplicationID: "-2-",
				QuotaAmount:     15,
			},
			expectedRequest: `
				{
				  "consumerId":"api_key:api-key",
				  "methodName":"echo",
				  "quotaMetrics":[
					 {
						"metricName":"read-requests",
						"metricValues":[
						   {
							  "int64Value":"15"
						   }
						]
					 }
				  ],
				  "quotaMode":"NORMAL"
				}`,
			response: &sc.AllocateQuotaResponse{
				OperationId: "cc56c90f-155f-4907-9461-64ed764fcbc1",
				AllocateErrors: []*sc.QuotaError{
					{
						Code:        "RESOURCE_EXHAUSTED",
						Description: "out of quota",
					},
				},
			},
			expectedResult: createQuotaResult(status.WithMessage(rpc.RESOURCE_EXHAUSTED, "out of quota"),
				time.Duration(60)*time.Second, 15),
		},
	}

	for _, testCase := range testCases {
		quotaTestImpl(testCase, t)
	}
}

func TestInvalidInstance(t *testing.T) {
	test := setupQuotaTest(t)
	delete(test.instance.Dimensions, "api_key")
	result, err := test.processor.ProcessQuota(context.Background(), &test.instance, adapter.QuotaArgs{
		DeduplicationID: "-1-",
		BestEffort:      false,
		QuotaAmount:     10,
	})
	if err == nil {
		t.Errorf(`expect error but get nil`)
	}
	if result.Status.Code != int32(rpc.INVALID_ARGUMENT) {
		t.Errorf(`expect invalid argument error, but get %v`, result)
	}
}

func quotaTestImpl(testCase *quotaTestCase, t *testing.T) {
	test := setupQuotaTest(t)
	test.client.setQuotaAllocateRespone(testCase.response)
	result, err := test.processor.ProcessQuota(context.Background(), &test.instance, testCase.args)
	if test.client.allocateQuotaRequest != nil && err != nil {
		t.Errorf(`unexpected quota allocate error %v`, err)
	}

	test.client.allocateQuotaRequest.AllocateOperation.OperationId = ""
	allocOp, err := test.client.allocateQuotaRequest.AllocateOperation.MarshalJSON()
	if err != nil {
		t.Errorf("fail to marshal JSON %v", err)
	}

	if !compareJSON(string(allocOp), testCase.expectedRequest) {
		t.Errorf("expect request: %v, but get: %v", testCase.expectedRequest, string(allocOp))
	}

	if err != nil && testCase.response != nil {
		t.Errorf("unexpected error: %v, injected response:%v", err, *testCase.response)
	}

	if err != nil && !reflect.DeepEqual(testCase.expectedResult, result) {
		t.Errorf("expecte checkResult: %v, but get %v", testCase.expectedRequest, result)
	}
}
