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

package api

import (
	"context"
	"flag"
	"strings"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/status"
)

type fakeExecutor struct {
	checkFunc  func() rpc.Status
	reportFunc func() rpc.Status
	quotaFunc  func() (*aspect.QuotaMethodResp, rpc.Status)
}

func (fe fakeExecutor) Check(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status {
	return fe.checkFunc()
}

func (fe fakeExecutor) Report(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status {
	return fe.reportFunc()
}

func (fe fakeExecutor) Quota(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	qma *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {
	return fe.quotaFunc()
}

func TestHandler(t *testing.T) {
	bag := attribute.GetMutableBag(nil)
	output := attribute.GetMutableBag(nil)

	checkReq := &mixerpb.CheckRequest{}
	checkResp := &mixerpb.CheckResponse{}
	reportReq := &mixerpb.ReportRequest{}
	reportResp := &mixerpb.ReportResponse{}
	quotaReq := &mixerpb.QuotaRequest{}
	quotaResp := &mixerpb.QuotaResponse{}

	cases := []struct {
		errStr string
		code   rpc.Code
	}{
		{"", rpc.OK},
		{"RESOLVER", rpc.INTERNAL},
		{"BADASPECT", rpc.INTERNAL},
	}

	for _, c := range cases {
		e := fakeExecutor{
			checkFunc: func() rpc.Status {
				if c.errStr != "" {
					return status.WithInternal(c.errStr)
				}
				return status.OK
			},
			reportFunc: func() rpc.Status {
				if c.errStr != "" {
					return status.WithInternal(c.errStr)
				}
				return status.OK
			},
			quotaFunc: func() (*aspect.QuotaMethodResp, rpc.Status) {
				if c.errStr != "" {
					return nil, status.WithInternal(c.errStr)
				}
				return nil, status.OK
			},
		}

		h := NewHandler(e).(*handlerState)

		h.Check(context.Background(), bag, output, checkReq, checkResp)
		h.Report(context.Background(), bag, output, reportReq, reportResp)
		h.Quota(context.Background(), bag, output, quotaReq, quotaResp)

		if checkResp.Result.Code != int32(c.code) || reportResp.Result.Code != int32(c.code) || quotaResp.Result.Code != int32(c.code) {
			t.Errorf("Expected %v for all responses, got %v, %v, %v", c.code, checkResp.Result.Code, reportResp.Result.Code, quotaResp.Result.Code)
		}

		if c.errStr != "" {
			if !strings.Contains(checkResp.Result.Message, c.errStr) ||
				!strings.Contains(reportResp.Result.Message, c.errStr) ||
				!strings.Contains(quotaResp.Result.Message, c.errStr) {
				t.Errorf("Expecting %s in error messages, got %s, %s, %s", c.errStr, checkResp.Result.Message, reportResp.Result.Message,
					quotaResp.Result.Message)
			}
		}
	}

	e := fakeExecutor{
		checkFunc: func() rpc.Status {
			return status.OK
		},
		reportFunc: func() rpc.Status {
			return status.OK
		},
		quotaFunc: func() (*aspect.QuotaMethodResp, rpc.Status) {
			return &aspect.QuotaMethodResp{Amount: 42}, status.OK
		},
	}

	h := NewHandler(e).(*handlerState)

	// Should succeed
	h.Quota(context.Background(), bag, output, quotaReq, quotaResp)

	if !status.IsOK(quotaResp.Result) {
		t.Errorf("Expected successful quota allocation, got %v", quotaResp.Result)
	}

	if quotaResp.Amount != 42 {
		t.Errorf("Expected 42, got %v", quotaResp.Amount)
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
