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
	"net/http"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prom2json"

	"istio.io/istio/mixer/pkg/adapter"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	prometheusMetricsURLTemplate = "http://localhost:%d/metrics"
	requestCountToPromCfg        = `
apiVersion: "config.istio.io/v1alpha2"
kind: prometheus
metadata:
  name: handler
  namespace: istio-system
spec:
  metrics:
  - name: request_count
    instance_name: requestcount.metric.istio-system
    kind: COUNTER
    label_names:
    - destination_service
    - response_code
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  actions:
  - handler: handler.prometheus
    instances:
    - requestcount.metric
---
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: requestcount
  namespace: istio-system
spec:
  value: "1"
  dimensions:
    destination_service: "\"myservice\""
    response_code: "200"
`
)

func TestReport(t *testing.T) {
	info, pServer := GetInfoWithAddr(":0")
	defer pServer.Close()

	adapter_integration.RunTest(
		t,
		func() adapter.Info {
			return info
		},
		adapter_integration.Scenario{
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
				},
				{
					CallKind: adapter_integration.REPORT,
				},
			},

			GetState: func(ctx interface{}) (interface{}, error) {
				mfChan := make(chan *dto.MetricFamily, 1)
				transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
				go prom2json.FetchMetricFamilies(fmt.Sprintf(prometheusMetricsURLTemplate, pServer.Port()), mfChan, transport)
				result := []prom2json.Family{}
				for mf := range mfChan {
					result = append(result, *prom2json.NewFamily(mf))
				}
				return result, nil
			},

			Configs: []string{
				requestCountToPromCfg,
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
                 "value": "2"
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
