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
	"errors"
	"reflect"
	"testing"
	"time"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/adapter/servicecontrol/template/servicecontrolreport"
	"istio.io/istio/mixer/pkg/adapter"
	at "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/mixer/template/quota"
)

type mockProcessor struct {
	checkResult *adapter.CheckResult
	reportError error
	quotaResult *adapter.QuotaResult
}

func (p *mockProcessor) ProcessCheck(ctx context.Context, instance *apikey.Instance) (adapter.CheckResult, error) {
	if p.checkResult == nil {
		return adapter.CheckResult{}, errors.New("injected error")
	}
	return *p.checkResult, nil
}

func (p *mockProcessor) ProcessReport(ctx context.Context, instances []*servicecontrolreport.Instance) error {
	return p.reportError
}

func (p *mockProcessor) ProcessQuota(ctx context.Context,
	instance *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	if p.quotaResult == nil {
		return adapter.QuotaResult{}, errors.New("injected error")
	}
	return *p.quotaResult, nil
}

func (p *mockProcessor) Close() error {
	return nil
}

func TestHandleApiKey(t *testing.T) {
	instance := apikey.Instance{
		Api:          "test_service",
		ApiOperation: "/echo",
		ApiKey:       "test_key",
		Timestamp:    time.Now(),
	}

	mock := &mockProcessor{}
	h := getTestHandler(mock, t)
	mock.checkResult = &adapter.CheckResult{
		Status: status.OK,
	}

	result, err := h.HandleApiKey(context.Background(), &instance)
	if err != nil || !reflect.DeepEqual(*mock.checkResult, result) {
		t.Errorf(`expect check result %v, but get %v`, *mock.checkResult, result)
	}
}

func TestHandleReport(t *testing.T) {
	now := time.Now()
	instances := []*servicecontrolreport.Instance{
		{
			ApiVersion:      "v1",
			ApiOperation:    "echo.foo.bar",
			ApiProtocol:     "gRPC",
			ApiService:      "test_service",
			ApiKey:          "test_key",
			RequestTime:     now,
			RequestMethod:   "POST",
			RequestPath:     "/blah",
			RequestBytes:    10,
			ResponseTime:    now,
			ResponseCode:    200,
			ResponseBytes:   100,
			ResponseLatency: time.Duration(1) * time.Second,
		},
	}
	mock := &mockProcessor{}
	h := getTestHandler(mock, t)
	err := h.HandleServicecontrolReport(context.Background(), instances)
	if err != nil {
		t.Errorf(`expect success but failed with %v`, err)
	}
}

func TestHandleQuota(t *testing.T) {
	instance := quota.Instance{
		Name: "ratelimit.quota.istio-system",
		Dimensions: map[string]interface{}{
			"api_key":       "api-key",
			"api_operation": "echo",
		},
	}

	mock := &mockProcessor{}
	h := getTestHandler(mock, t)
	mock.quotaResult = &adapter.QuotaResult{
		Status:        status.OK,
		ValidDuration: time.Minute,
		Amount:        10,
	}
	_, _ = h.HandleQuota(context.Background(), &instance, adapter.QuotaArgs{})

	/*
		if err != nil || !reflect.DeepEqual(*mock.quotaResult, result) {
			t.Errorf(`expect quota result %v, but get %v`, *mock.checkResult, result)
		}
	*/
}

func TestUnknownService(t *testing.T) {
	h := getTestHandler(&mockProcessor{}, t)
	delete(h.ctx.serviceConfigIndex, "test_service")
	delete(h.svcProcMap, "test_service")
	_, err := h.getServiceProcessor("test_service")
	if err == nil {
		t.Errorf(`expect non-nil error`)
	}
}

func TestNewHandler(t *testing.T) {
	ctx := &handlerContext{
		env: at.NewEnv(t),
		config: &config.Params{
			ServiceConfigs: []*config.GcpServiceSetting{
				{
					MeshServiceName:   "test_service",
					GoogleServiceName: "echo.googleapi.com",
				},
			},
		},
		serviceConfigIndex: map[string]*config.GcpServiceSetting{
			"test_service": {
				MeshServiceName:   "test_service",
				GoogleServiceName: "echo.googleapi.com",
			},
		},
	}

	h, err := newHandler(ctx)
	if err != nil {
		t.Fatal(`fail to create handler`)
	}
	if !reflect.DeepEqual(*ctx, *h.ctx) {
		t.Errorf(`handler not initialized with handleContext`)
	}

	if h.svcProcMap == nil {
		t.Errorf(`handler.svcProcMap not initialized`)
	}
}

func getTestHandler(mock *mockProcessor, t *testing.T) *handler {
	return &handler{
		ctx: &handlerContext{
			env: at.NewEnv(t),
			serviceConfigIndex: map[string]*config.GcpServiceSetting{
				"test_service": {
					MeshServiceName:   "test_service",
					GoogleServiceName: "echo.googleapi.com",
				},
			},
		},
		svcProcMap: map[string]*serviceProcessor{
			"test_service": {
				checkProcessor:  mock,
				reportProcessor: mock,
				quotaProcessor:  mock,
			},
		},
	}
}
