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
	"context"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/svcctrl/config"
	"istio.io/istio/mixer/adapter/svcctrl/template/svcctrlreport"
	at "istio.io/istio/mixer/pkg/adapter/test"
)

type reportTest struct {
	inst      *svcctrlreport.Instance
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
		inst: &svcctrlreport.Instance{
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
			client:   mockClient,
			resolver: &mockConsumerProjectIDResolver{},
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
	test.processor.ProcessReport(context.Background(), []*svcctrlreport.Instance{test.inst})
	<-test.env.GetDoneChan()
	if test.client.reportRequest == nil {
		t.Error("report request failed")
	}
}
