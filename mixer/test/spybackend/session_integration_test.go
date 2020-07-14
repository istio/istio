// Copyright Istio Authors
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
	"context"
	"fmt"
	"io"
	"testing"

	"google.golang.org/grpc"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
)

// This test for now just validates the backend can be started and tested against. This is will be used to verify
// the OOP adapter work. As various features start lighting up, this test will grow.

const (
	sessionID = "1234"
)

func TestSessionBackend(t *testing.T) {
	adapter_integration.RunTest(
		t,
		nil,
		adapter_integration.Scenario{
			Setup: func() (interface{}, error) {
				args := DefaultArgs()
				args.Behavior.ValidateResponse = &v1beta1.ValidateResponse{Status: &rpc.Status{Code: 0}}
				args.Behavior.CreateSessionResponse = &v1beta1.CreateSessionResponse{Status: &rpc.Status{Code: 0}, SessionId: "1234"}
				args.Behavior.CloseSessionResponse = &v1beta1.CloseSessionResponse{Status: &rpc.Status{Code: 0}}
				args.Behavior.HandleMetricResult = &v1beta1.ReportResult{}
				args.Behavior.HandleListEntryResult = &v1beta1.CheckResult{ValidUseCount: 31}
				args.Behavior.HandleQuotaResult = &v1beta1.QuotaResult{Quotas: map[string]v1beta1.QuotaResult_Result{"key1": {GrantedAmount: 32}}}

				var s Server
				var err error
				if s, err = newSessionServer(args); err != nil {
					return nil, err
				}
				s.Run()
				return s, nil
			},
			Teardown: func(ctx interface{}) {
				_ = ctx.(Server).Close()
			},
			GetState: func(ctx interface{}) (interface{}, error) {
				return nil, validateSessionBackend(ctx, t)
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

func closeHelper(c io.Closer) {
	_ = c.Close()
}

func validateSessionBackend(ctx interface{}, t *testing.T) error {
	s := ctx.(*sessionServer)
	// Connect the client to Mixer
	conn, err := grpc.Dial(s.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Unable to connect to gRPC server: %v", err)
	}
	client := v1beta1.NewInfrastructureBackendClient(conn)
	defer closeHelper(conn)

	if createSessionResponse, err := client.CreateSession(context.Background(), &v1beta1.CreateSessionRequest{}); err != nil {
		return err
	} else if createSessionResponse.SessionId != sessionID {
		return fmt.Errorf("got SessionId %s; want %s", createSessionResponse.SessionId, sessionID)
	}

	if createSessionResponse, err := client.CloseSession(context.Background(), &v1beta1.CloseSessionRequest{sessionID}); err != nil {
		return err
	} else if createSessionResponse.Status.Code != int32(rpc.OK) {
		return fmt.Errorf("got CloseSession code %v; want %v", createSessionResponse.Status.Code, rpc.OK)
	}

	if validateResponse, err := client.Validate(context.Background(), &v1beta1.ValidateRequest{}); err != nil {
		return err
	} else if validateResponse.Status.Code != int32(rpc.OK) {
		return fmt.Errorf("got Validate code %v; want %v", validateResponse.Status.Code, rpc.OK)
	}

	return validateHandleCalls(
		metric.NewHandleMetricServiceClient(conn),
		listentry.NewHandleListEntryServiceClient(conn),
		quota.NewHandleQuotaServiceClient(conn),
		s.Requests)
}

func validateHandleCalls(metricClt metric.HandleMetricServiceClient,
	listentryClt listentry.HandleListEntryServiceClient, quotaClt quota.HandleQuotaServiceClient, req *Requests) error {
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

	if len(req.HandleQuotaRequest) != 1 {
		return fmt.Errorf("got quota calls %d; want %d", len(req.HandleQuotaRequest), 1)
	}
	if len(req.HandleMetricRequest) != 1 {
		return fmt.Errorf("got metric calls %d; want %d", len(req.HandleMetricRequest), 1)
	}
	if len(req.HandleListEntryRequest) != 1 {
		return fmt.Errorf("got listentry calls %d; want %d", len(req.HandleListEntryRequest), 1)
	}
	return nil
}
