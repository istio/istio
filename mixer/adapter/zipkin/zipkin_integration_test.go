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

package zipkin

import (
	"testing"
	"time"

	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/reporter"

	"istio.io/istio/mixer/adapter/zipkin/config"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	traceSpanConfig = `
---
apiVersion: "config.istio.io/v1alpha2"
kind: tracespan
metadata:
  name: default
  namespace: istio-system
spec:
  traceId: request.headers["x-b3-traceid"] | ""
  spanId: request.headers["x-b3-spanid"] | ""
  parentSpanId: request.headers["x-b3-parentspanid"] | ""
  spanName: request.path | "/"
  startTime: request.time
  endTime: response.time
  httpStatusCode: response.code | 0
  sourceName: source.workload.name | ""
  sourceIp: source.ip | ip("0.0.0.0")
  destinationName: destination.workload.name | ""
  destinationIp: destination.ip | ip("0.0.0.0")
  requestSize: request.size | 0
  requestTotalSize: request.total_size | 0
  responseSize: response.size | 0
  responseTotalSize: response.total_size | 0
  apiProtocol: api.protocol | ""
  spanTags:
    http.host: request.host | ""
    http.method: request.method | ""
    http.path: request.path | ""
    http.user_agent: request.useragent | ""
    principal: request.auth.principal | "unknown"
    source.version: source.labels["version"] | "unknown"
    destination.version: destination.labels["version"] | "unknown"
---
apiVersion: "config.istio.io/v1alpha2"
kind: zipkin
metadata:
  name: handler
  namespace: istio-system
spec:
  sampleProbability: 1.0
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: zipkin
  namespace: istio-system
spec:
  match: "true" # If omitted match is true.
  actions:
  - handler: handler.zipkin
    instances:
    - default.tracespan
---
`
)

type fakeReporter []model.SpanModel

func (r *fakeReporter) Send(span model.SpanModel) {
	*r = append(*r, span)
}

func (r *fakeReporter) Close() error {
	*r = nil
	return nil
}

func TestReport(t *testing.T) {
	getExporterPrev := getReporterFunc
	defer func() {
		getReporterFunc = getExporterPrev
	}()
	var r fakeReporter
	getReporterFunc = func(params *config.Params) reporter.Reporter {
		return &r
	}
	end, err := time.Parse("Mon Jan 2 15:04:05 -0700 MST 2006", "Mon Jan 2 15:04:05 -0700 MST 2006")
	if err != nil {
		t.Fatal(err)
	}
	start := end.Add(-100 * time.Millisecond)
	adapter_integration.RunTest(
		t,
		GetInfo,
		adapter_integration.Scenario{
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
					Attrs: map[string]interface{}{
						"request.time":  start,
						"response.time": end,
						"request.headers": map[string]string{
							"x-b3-traceid":      "463ac35c9f6413ad48485a3953bb6124",
							"x-b3-spanid":       "a2fb4a1d1a96d312",
							"x-b3-parentspanid": "0020000000000001",
						},
						"request.path":              "/foo/bar",
						"request.host":              "example.istio.com",
						"request.useragent":         "xxx",
						"request.size":              int64(100),
						"request.total_size":        int64(128),
						"response.size":             int64(500),
						"response.total_size":       int64(512),
						"source.workload.name":      "srcsvc",
						"destination.workload.name": "destsvc",
						"source.labels":             map[string]string{"version": "v1"},
						"api.protocol":              "http",
						"request.method":            "POST",
						"response.code":             int64(200),
					},
				},
			},

			GetState: func(ctx interface{}) (interface{}, error) {
				return r, nil
			},

			Configs: []string{
				traceSpanConfig,
			},

			Want: `{
  "AdapterState": [
    {
      "annotations": [
        {
          "timestamp": 1136239444900000,
          "value": "Received 128 bytes"
        },
        {
          "timestamp": 1136239445000000,
          "value": "Sent 512 bytes"
        }
      ],
      "duration": 100000,
      "id": "a2fb4a1d1a96d312",
      "kind": "SERVER",
      "localEndpoint": {
        "ipv4": "0.0.0.0",
        "serviceName": "destsvc"
      },
      "name": "/foo/bar",
      "parentId": "0020000000000001",
      "tags": {
        "destination.version": "unknown",
        "http.host": "example.istio.com",
        "http.method": "POST",
        "http.path": "/foo/bar",
        "http.status_code": "200",
        "http.user_agent": "xxx",
        "opencensus.status_description": "OK",
        "principal": "unknown",
        "source.version": "v1"
      },
      "timestamp": 1136239444900000,
      "traceId": "463ac35c9f6413ad48485a3953bb6124"
    }
  ],
  "Returns": [
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
