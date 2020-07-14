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

package prometheus

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prom2json"

	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	hconfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: h1
  namespace: istio-system
spec:
  adapter: prometheus-nosession
  connection:
    address: "%s"
  params:
    metrics:
    - name: request_count
      instance_name: i1metric.instance.istio-system
      kind: COUNTER
      label_names:
      - destination_service
      - response_code
---
`
	iconfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i1metric
  namespace: istio-system
spec:
  template: metric
  params:
    value: request.size
    dimensions:
      destination_service: "\"myservice\""
      response_code: "200"
---
`

	rconfig = `
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

	prometheusMetricsURLTemplate = "http://localhost:%d/metrics"
)

func TestReport(t *testing.T) {
	adptCfgBytes, err := ioutil.ReadFile("prometheus-nosession.yaml")
	if err != nil {
		t.Fatalf("could not read file: %v", err)
	}

	adapter_integration.RunTest(
		t,
		nil,
		adapter_integration.Scenario{
			Setup: func() (ctx interface{}, err error) {
				pServer, err := NewNoSessionServer(0, 0)
				if err != nil {
					return nil, err
				}
				pServer.Run()
				return pServer, nil
			},
			Teardown: func(ctx interface{}) {
				s := ctx.(Server)
				s.Close()
			},
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": int64(555)},
				},
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": int64(445)},
				},
			},

			GetState: func(ctx interface{}) (interface{}, error) {
				s := ctx.(Server)
				mfChan := make(chan *dto.MetricFamily, 1)
				transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
				go prom2json.FetchMetricFamilies(fmt.Sprintf(prometheusMetricsURLTemplate, s.PromPort()), mfChan, transport)
				result := []prom2json.Family{}
				for mf := range mfChan {
					result = append(result, *prom2json.NewFamily(mf))
				}
				return result, nil
			},
			GetConfig: func(ctx interface{}) ([]string, error) {
				s := ctx.(Server)
				return []string{
					// CRs for built-in templates are automatically added by the integration test framework.
					string(adptCfgBytes),
					fmt.Sprintf(hconfig, s.Addr()),
					iconfig,
					rconfig,
				}, nil
			},
			Want: `
            {
             "AdapterState": [
              {
               "help": "request_count",
               "metrics": [
                {
                 "labels": {
                  "destination_service": "myservice",
                  "response_code": "200"
                 },
                 "value": "1000"
                }
               ],
               "name": "istio_request_count",
               "type": "COUNTER"
              }
             ],
             "Returns": [
              {
               "Check": {
                "Status": {},
                "ValidDuration": 0,
                "ValidUseCount": 0
               },
               "Error": null
              },
              {
               "Check": {
                "Status": {},
                "ValidDuration": 0,
                "ValidUseCount": 0
               },
               "Error": null
              }
             ]
             }`,
		},
	)
}
