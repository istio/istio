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
	"context"
	"fmt"
	"io/ioutil"
	"testing"

	"google.golang.org/grpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	istio_mixer_v1 "istio.io/api/mixer/v1"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
)

const (
	h1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: h1
  namespace: istio-system
spec:
  adapter: spybackend-nosession
  connection:
    address: "%s"
---
`
	i1Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i1metric
  namespace: istio-system
spec:
  template: metric
  params:
    value: request.size | 123
    dimensions:
      destination_service: "\"myservice\""
      response_code: "200"
---
`

	r1H1I1Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  actions:
  - handler: h1.istio-system
    instances:
    - i1metric
---
`

	h2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: h2
  namespace: istio-system
spec:
  adapter: spybackend-nosession
  connection:
    address: "%s"
---
`

	i2Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i2metric
  namespace: istio-system
spec:
  template: metric
  params:
    value: request.size | 456
    dimensions:
      destination_service: "\"myservice2\""
      response_code: "400"
---
`

	r2H2I2Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r2
  namespace: istio-system
spec:
  actions:
  - handler: h2.istio-system
    instances:
    - i2metric
---
`
	i3List = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i3list
  namespace: istio-system
spec:
  template: listentry
  params:
    value: source.name | ""
---
`

	r3H1I3List = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r3
  namespace: istio-system
spec:
  actions:
  - handler: h1.istio-system
    instances:
    - i3list
---
`

	i4Quota = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: requestQuota
  namespace: istio-system
spec:
  template: quota
  params:
    dimensions:
      source: source.labels["app"] | source.service | "unknown"
      sourceVersion: source.labels["version"] | "unknown"
      destination: destination.labels["app"] | destination.service | "unknown"
      destinationVersion: destination.labels["version"] | "unknown"
---
`

	r4h1i4Quota = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r4
  namespace: istio-system
spec:
  actions:
  - handler: h1
    instances:
    - requestQuota
`

	r6MatchIfReqIDH1i4Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r5
  namespace: istio-system
spec:
  match: request.id | "unknown" != "unknown"
  actions:
  - handler: h1
    instances:
    - i1metric
`
)

func TestNoSessionBackend(t *testing.T) {
	adptCfgBytes, err := ioutil.ReadFile("nosession.yaml")
	if err != nil {
		t.Fatalf("cannot open file: %v", err)
	}
	adapter_integration.RunTest(
		t,
		nil,
		adapter_integration.Scenario{
			Setup: func() (interface{}, error) {
				args := DefaultArgs()
				args.Behavior.HandleMetricResult = &v1beta1.ReportResult{}
				args.Behavior.HandleListEntryResult = &v1beta1.CheckResult{ValidUseCount: 31}
				args.Behavior.HandleQuotaResult = &v1beta1.QuotaResult{
					Quotas: map[string]v1beta1.QuotaResult_Result{"requestQuota": {GrantedAmount: 32}}}

				var s Server
				var err error
				if s, err = NewNoSessionServer(args); err != nil {
					return nil, err
				}
				s.Run()
				return s, nil
			},
			Teardown: func(ctx interface{}) {
				_ = ctx.(Server).Close()
			},
			GetState: func(ctx interface{}) (interface{}, error) {
				// TODO return the state of data received by the spy adapter; deterministic order.
				//return nil, validateNoSessionBackend(ctx, t)
				return nil, nil
			},
			SingleThreaded: true,
			ParallelCalls: []adapter_integration.Call{
				// 3 report calls; varying request.size attribute and no attributes call too.
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": 666},
				},
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": 888},
				},
				{
					CallKind: adapter_integration.REPORT,
				},

				// 3 check calls; varying source.name attribute and no attributes call too.,
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"source.name": "foobar"},
				},
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"source.name": "bazbaz"},
				},
				{
					CallKind: adapter_integration.CHECK,
				},
				// one call with quota args
				{
					CallKind: adapter_integration.CHECK,
					Quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
						"requestQuota": {
							Amount:     30,
							BestEffort: true,
						},
					},
				},
				// one report request with request.id to match r4 rule
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.id": "somereqid"},
				},
			},
			GetConfig: func(ctx interface{}) ([]string, error) {
				s := ctx.(Server)
				return []string{
					// CRs for built-in templates are automatically added by the integration test framework.
					string(adptCfgBytes),
					fmt.Sprintf(h1, s.Addr().String()),
					i1Metric,
					r1H1I1Metric,
					fmt.Sprintf(h2, s.Addr().String()),
					i2Metric,
					r2H2I2Metric,
					i3List,
					r3H1I3List,
					i4Quota,
					r4h1i4Quota,
					r6MatchIfReqIDH1i4Metric,
				}, nil
			},
			//VerifyResult: verifyResult,
			Want: `
		{
		 "AdapterState": null,
		 "Returns": [
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 0
		   },
		   "Quota": null,
		   "Error": null
		  },
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 0
		   },
		   "Quota": null,
		   "Error": null
		  },
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 0
		   },
		   "Quota": null,
		   "Error": null
		  },
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 31
		   },
		   "Quota": null,
		   "Error": null
		  },
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 31
		   },
		   "Quota": null,
		   "Error": null
		  },
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 31
		   },
		   "Quota": null,
		   "Error": null
		  },
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 0
		   },
		   "Quota": {
		    "requestQuota": {
		     "Status": {},
		     "ValidDuration": 0,
		     "Amount": 0
		    }
		   },
		   "Error": null
		  },
		  {
		   "Check": {
		    "Status": {},
		    "ValidDuration": 0,
		    "ValidUseCount": 0
		   },
		   "Quota": null,
		   "Error": null
		  }
		 ]
		}`,
		},
	)
}

func validateNoSessionBackend(ctx interface{}, t *testing.T) error {
	s := ctx.(*NoSessionServer)
	req := s.Requests
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
