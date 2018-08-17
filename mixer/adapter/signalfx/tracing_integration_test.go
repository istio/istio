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

package signalfx

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/signalfx/golib/trace"

	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	tracingConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: signalfx
metadata:
  name: handler2
  namespace: istio-system
spec:
  access_token: abcdef
  ingest_url: %s
  enable_metrics: false
  tracing_buffer_size: 1000
  tracing_sample_probability: 1.0
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r2
  namespace: istio-system
spec:
  actions:
  - handler: handler2.signalfx
    instances:
    - signalfx.tracespan
---
apiVersion: "config.istio.io/v1alpha2"
kind: tracespan
metadata:
  name: signalfx
  namespace: istio-system
spec:
  traceId: request.headers["x-b3-traceid"] | ""
  spanId: request.headers["x-b3-spanid"] | ""
  parentSpanId: request.headers["x-b3-parentspanid"] | ""
  spanName: request.path | "/"
  startTime: request.time
  endTime: response.time
  httpStatusCode: response.code | 0
  clientSpan: context.reporter.kind == "outbound"
  spanTags:
    destination.ip: destination.ip | ip("0.0.0.0")
    destination.name: destination.name | "unknown"
    source.ip: source.ip | ip("0.0.0.0")
    source.name: source.name | "unknown"
`
)

func TestReportTraces(t *testing.T) {
	fakeIngest := &fakeSfxIngest{
		Spans: make(chan *trace.Span, 3),
	}
	fakeIngest.Server = httptest.NewServer(fakeIngest)

	end := time.Unix(1000, 0)
	start := end.Add(-100 * time.Millisecond)

	adapter_integration.RunTest(
		t,
		GetInfo,
		adapter_integration.Scenario{
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
					Attrs: map[string]interface{}{
						"request.time":          start,
						"response.time":         end,
						"context.reporter.kind": "outbound",
						"request.headers": map[string]string{
							"x-b3-traceid": "463ac35c9f6413ad48485a3953bb6124",
							"x-b3-spanid":  "a2fb4a1d1a96d312",
						},
						"request.path":        "/foo/bar",
						"request.host":        "example.istio.com",
						"request.useragent":   "xxx",
						"request.size":        int64(128),
						"response.size":       int64(512),
						"source.service":      "srcsvc",
						"destination.service": "destsvc",
						"destination.name":    "destsvc",
						"destination.ip":      []byte(net.ParseIP("10.0.0.2")),
						"source.labels":       map[string]string{"version": "v1"},
						"source.ip":           []byte(net.ParseIP("10.0.0.1")),
						"source.name":         "srcsvc",
						"api.protocol":        "http",
						"request.method":      "POST",
						"response.code":       int64(200),
					},
				},
				{
					CallKind: adapter_integration.REPORT,
					Attrs: map[string]interface{}{
						"request.time":          start.Add(5 * time.Millisecond),
						"response.time":         end.Add(10 * time.Millisecond),
						"context.reporter.kind": "inbound",
						"request.headers": map[string]string{
							"x-b3-traceid":      "463ac35c9f6413ad48485a3953bb6124",
							"x-b3-spanid":       "b3a9b83bb2b3098f",
							"x-b3-parentspanid": "a2fb4a1d1a96d312",
						},
						"request.path":        "/bar/baz",
						"request.host":        "example.istio.com",
						"request.useragent":   "xxx",
						"request.size":        int64(128),
						"response.size":       int64(512),
						"source.service":      "srcsvc",
						"destination.service": "destsvc",
						"destination.ip":      []byte(net.ParseIP("10.0.0.3")),
						"source.labels":       map[string]string{"version": "v1"},
						"source.ip":           []byte(net.ParseIP("10.0.0.2")),
						"api.protocol":        "http",
						"request.method":      "POST",
						"response.code":       int64(200),
					},
				},
				{
					CallKind: adapter_integration.REPORT,
					Attrs: map[string]interface{}{
						"request.time":          start.Add(6 * time.Millisecond),
						"response.time":         end.Add(11 * time.Millisecond),
						"context.reporter.kind": "outbound",
						"request.headers": map[string]string{
							"x-b3-traceid":      "463ac35c9f6413ad48485a3953bb6124",
							"x-b3-spanid":       "abcdef0123456789",
							"x-b3-parentspanid": "a2fb4a1d1a96d312",
						},
						"request.path":        "/bar/baz?q=whatever",
						"request.host":        "example.istio.com",
						"request.useragent":   "xxx",
						"request.size":        int64(128),
						"response.size":       int64(512),
						"source.service":      "srcsvc",
						"destination.service": "destsvc",
						"destination.ip":      []byte(net.ParseIP("10.0.0.3")),
						"source.ip":           []byte(net.ParseIP("10.0.0.2")),
						"api.protocol":        "http",
						"request.method":      "POST",
						"response.code":       int64(500),
					},
				},
			},

			GetState: func(_ interface{}) (interface{}, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				var spans []*trace.Span
				for {
					select {
					case <-ctx.Done():
						cancel()
						return spans, nil
					case sp := <-fakeIngest.Spans:
						spans = append(spans, sp)
						sort.Slice(spans, func(i, j int) bool {
							return *spans[i].Timestamp < *spans[j].Timestamp
						})
						if len(spans) >= 3 {
							cancel()
							return spans, nil
						}
					}
				}
			},

			Configs: []string{
				fmt.Sprintf(tracingConfig, fakeIngest.URL),
			},

			Want: `
            {
             "AdapterState": [
                  {
                   "annotations": null,
                   "debug": null,
                   "duration": 100000,
                   "id": "a2fb4a1d1a96d312",
                   "kind": "CLIENT",
                   "localEndpoint": {
                    "ipv4": "10.0.0.1",
                    "ipv6": null,
                    "port": null,
                    "serviceName": "srcsvc"
                   },
                   "name": "/foo/bar",
                   "parentId": null,
                   "remoteEndpoint": {
                    "ipv4": "10.0.0.2",
                    "ipv6": null,
                    "port": null,
                    "serviceName": "destsvc"
                   },
                   "shared": null,
                   "tags": {
                    "destination.ip": "10.0.0.2",
                    "destination.name": "destsvc",
                    "httpStatusCode": "200",
                    "source.ip": "10.0.0.1",
                    "source.name": "srcsvc"
                   },
                   "timestamp": 999900000,
                   "traceId": "463ac35c9f6413ad48485a3953bb6124"
                  },
                  {
                   "annotations": null,
                   "debug": null,
                   "duration": 105000,
                   "id": "b3a9b83bb2b3098f",
                   "kind": "SERVER",
                   "localEndpoint": {
                    "ipv4": "10.0.0.2",
                    "ipv6": null,
                    "port": null,
                    "serviceName": "unknown"
                   },
                   "name": "/bar/baz",
                   "parentId": "a2fb4a1d1a96d312",
                   "remoteEndpoint": {
                    "ipv4": "10.0.0.3",
                    "ipv6": null,
                    "port": null,
                    "serviceName": "unknown"
                   },
                   "shared": null,
                   "tags": {
                    "destination.ip": "10.0.0.3",
                    "destination.name": "unknown",
                    "httpStatusCode": "200",
                    "source.ip": "10.0.0.2",
                    "source.name": "unknown"
                   },
                   "timestamp": 999905000,
                   "traceId": "463ac35c9f6413ad48485a3953bb6124"
                  },
                  {
                   "annotations": null,
                   "debug": null,
                   "duration": 105000,
                   "id": "abcdef0123456789",
                   "kind": "CLIENT",
                   "localEndpoint": {
                    "ipv4": "10.0.0.2",
                    "ipv6": null,
                    "port": null,
                    "serviceName": "unknown"
                   },
                   "name": "/bar/baz",
                   "parentId": "a2fb4a1d1a96d312",
                   "remoteEndpoint": {
                    "ipv4": "10.0.0.3",
                    "ipv6": null,
                    "port": null,
                    "serviceName": "unknown"
                   },
                   "shared": null,
                   "tags": {
                    "error": "server error",
                    "destination.ip": "10.0.0.3",
                    "destination.name": "unknown",
                    "httpStatusCode": "500",
                    "q": "whatever",
                    "source.ip": "10.0.0.2",
                    "source.name": "unknown"
                   },
                   "timestamp": 999906000,
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
               "Error": null,
               "Quota": null
              },
              {
               "Check": {
                "Status": {},
                "ValidDuration": 0,
                "ValidUseCount": 0
               },
               "Error": null,
               "Quota": null
              },
              {
               "Check": {
                "Status": {},
                "ValidDuration": 0,
                "ValidUseCount": 0
               },
               "Error": null,
               "Quota": null
              }
             ]
             }`,
		},
	)
}
