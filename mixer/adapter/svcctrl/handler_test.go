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

	"errors"
	"reflect"

	"istio.io/istio/mixer/pkg/adapter"
	at "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/apikey"
)

type mockCheckProcessor struct {
	result *adapter.CheckResult
}

func (p *mockCheckProcessor) ProcessCheck(ctx context.Context, instance *apikey.Instance) (adapter.CheckResult, error) {
	if p.result == nil {
		return adapter.CheckResult{}, errors.New("injected error")
	}
	return *p.result, nil
}

func TestHandleApiKey(t *testing.T) {
	instance := apikey.Instance{
		ApiOperation: "/echo",
		ApiKey:       "test_key",
		Timestamp:    time.Now(),
	}

	mock := &mockCheckProcessor{}
	h := handler{
		ctx: &handlerContext{
			env: at.NewEnv(t),
		},
		svcProc: &serviceProcessor{
			checkProcessor: mock,
		},
	}

	mock.result = &adapter.CheckResult{
		Status: status.OK,
	}

	result, err := h.HandleApiKey(context.Background(), &instance)
	if err != nil || !reflect.DeepEqual(*mock.result, result) {
		t.Errorf(`expect check result %v, but get %v`, *mock.result, result)
	}
}
