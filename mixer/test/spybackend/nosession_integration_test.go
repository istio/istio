// Copyright 2018 Istio Authors
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

package spybackend

import (
	"testing"

	"google.golang.org/grpc"

	"context"
	"fmt"

	"istio.io/api/mixer/adapter/model/v1beta1"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
)

// This test for now just validates the backend can be started and tested against. This is will be used to verify
// the OOP adapter work. As various features start lighting up, this test will grow.

func TestNoSessionBackend(t *testing.T) {
	adapter_integration.RunTest(
		t,
		nil,
		adapter_integration.Scenario{
			Setup: func() (interface{}, error) {
				args := defaultArgs()
				args.behavior.handleMetricResult = &v1beta1.ReportResult{}
				args.behavior.handleListEntryResult = &v1beta1.CheckResult{ValidUseCount: 31}
				args.behavior.handleQuotaResult = &v1beta1.QuotaResult{Quotas: map[string]v1beta1.QuotaResult_Result{"key1": {GrantedAmount: 32}}}

				var s server
				var err error
				if s, err = newNoSessionServer(args); err != nil {
					return nil, err
				}
				s.Run()
				return s, nil
			},
			Teardown: func(ctx interface{}) {
				_ = ctx.(server).Close()
			},
			GetState: func(ctx interface{}) (interface{}, error) {
				return nil, validateNoSessionBackend(ctx, t)
			},
			ParallelCalls: []adapter_integration.Call{},
			Configs:       []string{},
			Want: `{
              "AdapterState": null,
		      "Returns": []
            }`,
		},
	)
}

func validateNoSessionBackend(ctx interface{}, t *testing.T) error {
	s := ctx.(*noSessionServer)
	req := s.requests
	// Connect the client to Mixer
	conn, err := grpc.Dial(s.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Unable to connect to gRPC server: %v", err)
	}
	defer closeHelper(conn)

	return validateHandleCalls(
		metric.NewHandleMetricServiceClient(conn),
		listentry.NewHandleListEntryServiceClient(conn),
		quota.NewHandleQuotaServiceClient(conn),
		req)
}

func validateHandleCalls(metricClt metric.HandleMetricServiceClient,
	listentryClt listentry.HandleListEntryServiceClient, quotaClt quota.HandleQuotaServiceClient, req *requests) error {
	if _, err := metricClt.HandleMetric(context.Background(), &metric.HandleMetricRequest{}); err != nil {
		return err
	}
	if le, err := listentryClt.HandleListEntry(context.Background(), &listentry.HandleListEntryRequest{}); err != nil {
		return err
	} else if le.ValidUseCount != 31 {
		return fmt.Errorf("got listentry.ValidUseCount %v; want %v", le.ValidUseCount, 31)
	}
	if qr, err := quotaClt.HandleQuota(context.Background(), &quota.HandleQuotaRequest{}); err != nil {
		return err
	} else if qr.Quotas["key1"].GrantedAmount != 32 {
		return fmt.Errorf("got quota.GrantedAmount %v; want %v", qr.Quotas["key1"].GrantedAmount, 31)
	}

	if len(req.handleQuotaRequest) != 1 {
		return fmt.Errorf("got quota calls %d; want %d", len(req.handleQuotaRequest), 1)
	}
	if len(req.handleMetricRequest) != 1 {
		return fmt.Errorf("got metric calls %d; want %d", len(req.handleMetricRequest), 1)
	}
	if len(req.handleListEntryRequest) != 1 {
		return fmt.Errorf("got listentry calls %d; want %d", len(req.handleListEntryRequest), 1)
	}
	return nil
}
