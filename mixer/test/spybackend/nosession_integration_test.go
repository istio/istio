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
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
)

// This test for now just validates the backend can be started and tested against. This is will be used to verify
// the OOP adapter work. As various features start lighting up, this test will grow.

// TODO add quota config test..

const (
	h1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: myh1
  namespace: istio-system
spec:
  adapter: spybackend-nosession
---
`
	i1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: myi1
  namespace: istio-system
spec:
  template: metric
  param:
    value: request.size | 123
    dimensions:
      destination_service: "\"myservice\""
      response_code: "200"
---
`

	r1H1I1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: myr1
  namespace: istio-system
spec:
  actions:
  - handler: myh1.istio-system
    instances:
    - myi1
---
`

	h2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: myh2
  namespace: istio-system
spec:
  adapter: spybackend-nosession
---
`

	i2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: myi2
  namespace: istio-system
spec:
  template: metric
  param:
    value: request.size | 456
    dimensions:
      destination_service: "\"myservice2\""
      response_code: "400"
---
`

	r2H2I2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: myr2
  namespace: istio-system
spec:
  actions:
  - handler: myh2.istio-system
    instances:
    - myi2
---
`
	i3 = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: myi3
  namespace: istio-system
spec:
  template: listentry
  param:
    value: source.name | ""
---
`

	r3H1I3 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: myr3
  namespace: istio-system
spec:
  actions:
  - handler: myh1.istio-system
    instances:
    - myi3
---
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
				args.Behavior.HandleQuotaResult = &v1beta1.QuotaResult{Quotas: map[string]v1beta1.QuotaResult_Result{"key1": {GrantedAmount: 32}}}

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
				// TODO validate if the data has received the backend.
				return nil, validateNoSessionBackend(ctx, t)
			},
			ParallelCalls: []adapter_integration.Call{
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
			},
			Configs: []string{
				// CRs for built-in templates are automatically added by the integration test framework.
				string(adptCfgBytes),
				h1,
				i1,
				r1H1I1,
				h2,
				i2,
				r2H2I2,
				i3,
				r3H1I3,
			},
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
		  }
		 ]
		}
`,
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
