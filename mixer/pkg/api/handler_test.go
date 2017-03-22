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
	"errors"
	"flag"
	"fmt"
	"strings"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/status"
)

type fakeresolver struct {
	ret []*cpb.Combined
	err error
}

func (f *fakeresolver) Resolve(bag attribute.Bag, aspectSet config.AspectSet) ([]*cpb.Combined, error) {
	return f.ret, f.err
}

type fakeExecutor struct {
	body func() aspect.Output
}

// Execute takes a set of configurations and Executes all of them.
func (f *fakeExecutor) Execute(ctx context.Context, cfgs []*cpb.Combined, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	ma aspect.APIMethodArgs) aspect.Output {
	return f.body()
}

func TestAspectManagerErrorsPropagated(t *testing.T) {
	f := &fakeExecutor{func() aspect.Output {
		return aspect.Output{Status: status.WithError(errors.New("expected"))}
	}}
	h := NewHandler(f, map[aspect.APIMethod]config.AspectSet{}).(*handlerState)
	h.ConfigChange(&fakeresolver{[]*cpb.Combined{nil, nil}, nil})

	o := h.execute(context.Background(), attribute.GetMutableBag(nil), attribute.GetMutableBag(nil), aspect.CheckMethod, nil)
	if o.Status.Code != int32(rpc.INTERNAL) {
		t.Errorf("execute(..., invalidConfig, ...) returned %v, wanted status with code %v", o.Status, rpc.INTERNAL)
	}
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
		resolver    *fakeresolver
		resolverErr string
		executorErr string
		resultErr   string
		code        rpc.Code
	}{
		{nil, "", "", "", rpc.INTERNAL},
		{&fakeresolver{[]*cpb.Combined{nil, nil}, nil}, "RESOLVER", "", "RESOLVER", rpc.INTERNAL},
		{&fakeresolver{[]*cpb.Combined{nil, nil}, nil}, "", "BADASPECT", "BADASPECT", rpc.INTERNAL},
	}

	for _, c := range cases {
		e := &fakeExecutor{}
		if c.executorErr != "" {
			e = &fakeExecutor{func() aspect.Output {
				return aspect.Output{Status: status.WithInternal(c.executorErr)}
			}}
		}

		h := NewHandler(e, map[aspect.APIMethod]config.AspectSet{}).(*handlerState)

		if c.resolver != nil {
			r := c.resolver
			if c.resolverErr != "" {
				r.err = fmt.Errorf(c.resolverErr)
			}
			h.ConfigChange(r)
		}

		h.Check(context.Background(), bag, output, checkReq, checkResp)
		h.Report(context.Background(), bag, output, reportReq, reportResp)
		h.Quota(context.Background(), bag, output, quotaReq, quotaResp)

		if checkResp.Result.Code != int32(c.code) || reportResp.Result.Code != int32(c.code) || quotaResp.Result.Code != int32(c.code) {
			t.Errorf("Expected %v for all responses, got %v, %v, %v", c.code, checkResp.Result.Code, reportResp.Result.Code, quotaResp.Result.Code)
		}

		if c.resultErr != "" {
			if !strings.Contains(checkResp.Result.Message, c.resultErr) ||
				!strings.Contains(reportResp.Result.Message, c.resultErr) ||
				!strings.Contains(quotaResp.Result.Message, c.resultErr) {
				t.Errorf("Expecting %s in error messages, got %s, %s, %s", c.resultErr, checkResp.Result.Message, reportResp.Result.Message,
					quotaResp.Result.Message)
			}
		}
	}

	f := &fakeExecutor{func() aspect.Output {
		return aspect.Output{Status: status.OK, Response: &aspect.QuotaMethodResp{Amount: 42}}
	}}
	r := &fakeresolver{[]*cpb.Combined{nil, nil}, nil}
	h := NewHandler(f, map[aspect.APIMethod]config.AspectSet{}).(*handlerState)
	h.ConfigChange(r)

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
