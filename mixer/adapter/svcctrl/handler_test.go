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
	"errors"
	"reflect"
	"testing"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	at "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/mixer/template/quota"
)

type mockProcessor struct {
	checkResult *adapter.CheckResult
	quotaResult *adapter.QuotaResult
}

func (p *mockProcessor) ProcessCheck(ctx context.Context, instance *apikey.Instance) (adapter.CheckResult, error) {
	if p.checkResult == nil {
		return adapter.CheckResult{}, errors.New("injected error")
	}
	return *p.checkResult, nil
}

func (p *mockProcessor) ProcessQuota(ctx context.Context,
	instance *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	if p.quotaResult == nil {
		return adapter.QuotaResult{}, errors.New("injected error")
	}
	return *p.quotaResult, nil
}

func TestHandleApiKey(t *testing.T) {
	instance := apikey.Instance{
		ApiOperation: "/echo",
		ApiKey:       "test_key",
		Timestamp:    time.Now(),
	}

	mock := &mockProcessor{}
	h := getTesthandler(mock, t)

	mock.checkResult = &adapter.CheckResult{
		Status: status.OK,
	}

	result, err := h.HandleApiKey(context.Background(), &instance)
	if err != nil || !reflect.DeepEqual(*mock.checkResult, result) {
		t.Errorf(`expect check result %v, but get %v`, *mock.checkResult, result)
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
	h := getTesthandler(mock, t)
	mock.quotaResult = &adapter.QuotaResult{
		Status:        status.OK,
		ValidDuration: time.Minute,
		Amount:        10,
	}
	result, err := h.HandleQuota(context.Background(), &instance, adapter.QuotaArgs{})
	if err != nil || !reflect.DeepEqual(*mock.quotaResult, result) {
		t.Errorf(`expect quota result %v, but get %v`, *mock.checkResult, result)
	}
}

func getTesthandler(mock *mockProcessor, t *testing.T) *handler {
	return &handler{
		ctx: &handlerContext{
			env: at.NewEnv(t),
		},
		svcProc: &serviceProcessor{
			checkProcessor: mock,
			quotaProcessor: mock,
		},
	}
}
