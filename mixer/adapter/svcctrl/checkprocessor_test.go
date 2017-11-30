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
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	pbtypes "github.com/gogo/protobuf/types"
	rpc "github.com/googleapis/googleapis/google/rpc"
	"google.golang.org/api/googleapi"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/svcctrl/config"
	"istio.io/istio/mixer/pkg/adapter"
	at "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/apikey"
)

const meshServiceName = "test_service"
const gcpServiceName = "test_service.cloud.goog"
const gcpConsumerProjectNumber = 12345

type checkProcessorTest struct {
	testConfig config.Params
	mockClient *mockSvcctrlClient
	checkProc  *checkImpl
}

func checkProcessorTestSetup(t *testing.T) *checkProcessorTest {
	test := &checkProcessorTest{
		testConfig: config.Params{
			RuntimeConfig: &config.RuntimeConfig{
				CheckCacheSize: 10,
				CheckResultExpiration: &pbtypes.Duration{
					Seconds: 300,
				},
			},
			ServiceConfigs: []*config.GcpServiceSetting{
				{
					MeshServiceName:   meshServiceName,
					GoogleServiceName: gcpServiceName,
				},
			},
		},
		mockClient: &mockSvcctrlClient{},
	}

	ctx, err := initializeHandlerContext(at.NewEnv(t), &test.testConfig, test.mockClient)
	if err != nil {
		t.Fatalf(`fail to initialize handleContext %v`, err)
	}

	checkProc, err := newCheckProcessor(meshServiceName, ctx)
	if err != nil {
		t.Fatalf(`fail to create test checkProcessor %v`, err)
	}

	test.checkProc = checkProc
	return test
}

func TestProcessCheck(t *testing.T) {
	test := checkProcessorTestSetup(t)
	response := &sc.CheckResponse{
		ServerResponse: googleapi.ServerResponse{
			HTTPStatusCode: 200,
		},
		CheckInfo: &sc.CheckInfo{
			ConsumerInfo: &sc.ConsumerInfo{
				ProjectNumber: gcpConsumerProjectNumber,
			},
		},
	}

	expectedResult := &adapter.CheckResult{
		Status:        status.OK,
		ValidDuration: test.checkProc.checkResultExpiration,
		ValidUseCount: math.MaxInt32,
	}

	testProcessCheck(test, response, expectedResult, t)
	// Call again with nil response to test check cache.
	testProcessCheck(test, nil, expectedResult, t)
}

func TestProcessCheckFailedHTTPCode(t *testing.T) {
	test := checkProcessorTestSetup(t)
	response := &sc.CheckResponse{
		ServerResponse: googleapi.ServerResponse{
			HTTPStatusCode: 403,
		},
		CheckInfo: &sc.CheckInfo{
			ConsumerInfo: &sc.ConsumerInfo{
				ProjectNumber: gcpConsumerProjectNumber,
			},
		},
	}

	expectedResult := &adapter.CheckResult{
		Status:        status.New(rpc.PERMISSION_DENIED),
		ValidDuration: test.checkProc.checkResultExpiration,
		ValidUseCount: math.MaxInt32,
	}

	testProcessCheck(test, response, expectedResult, t)
}

func TestProcessCheckWithError(t *testing.T) {
	test := checkProcessorTestSetup(t)
	response := &sc.CheckResponse{
		ServerResponse: googleapi.ServerResponse{
			HTTPStatusCode: 200,
		},
		CheckErrors: []*sc.CheckError{
			{
				Code:   "PERMISSION_DENIED",
				Detail: "check failed",
			},
		},
	}

	expectedResult := &adapter.CheckResult{
		Status:        status.WithPermissionDenied("PERMISSION_DENIED: check failed"),
		ValidDuration: test.checkProc.checkResultExpiration,
		ValidUseCount: math.MaxInt32,
	}

	testProcessCheck(test, response, expectedResult, t)
}

func TestResolveConsumerProjectID(t *testing.T) {
	test := checkProcessorTestSetup(t)
	test.mockClient.setCheckResponse(&sc.CheckResponse{
		ServerResponse: googleapi.ServerResponse{
			HTTPStatusCode: 200,
		},
		CheckInfo: &sc.CheckInfo{
			ConsumerInfo: &sc.ConsumerInfo{
				ProjectNumber: gcpConsumerProjectNumber,
			},
		},
	})
	{
		id, err := test.checkProc.ResolveConsumerProjectID(apiKeyPrefix+"test_key", "/echo")
		if err != nil {
			t.Fatalf(`ResolveConsumerProjectID(...) failed with error %v`, err)
		}
		if id != fmt.Sprintf("project_number:%d", gcpConsumerProjectNumber) {
			t.Errorf(`unexpected consumer project ID:%v`, id)
		}
	}
	{
		// Repeat the same test but set injected response to nil. Without check cache, the following
		// test would fail.
		test.mockClient.checkResponse = nil
		id, err := test.checkProc.ResolveConsumerProjectID(apiKeyPrefix+"test_key", "/echo")
		test.mockClient.setCheckResponse(nil)
		if err != nil {
			t.Fatalf(`ResolveConsumerProjectID(...) failed with error %v`, err)
		}
		if id != fmt.Sprintf("project_number:%d", gcpConsumerProjectNumber) {
			t.Errorf(`unexpected consumer project ID:%v`, id)
		}
	}
}

func testProcessCheck(test *checkProcessorTest, injectedResponse *sc.CheckResponse,
	expectedResult *adapter.CheckResult, t *testing.T) {

	instance := apikey.Instance{
		ApiOperation: "/echo",
		ApiKey:       "test_key",
		Timestamp:    time.Now(),
	}

	test.mockClient.setCheckResponse(injectedResponse)
	result, _ := test.checkProc.ProcessCheck(context.Background(), &instance)
	if !reflect.DeepEqual(*expectedResult, result) {
		t.Errorf(`expect to get result %v, but get %v`, *expectedResult, result)
	}
}
