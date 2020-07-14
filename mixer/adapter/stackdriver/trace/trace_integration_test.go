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

package trace

import (
	"context"
	"testing"
	"time"

	md "cloud.google.com/go/compute/metadata"
	"go.opencensus.io/trace"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/tracespan"
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
  sourceName: source.name | ""
  sourceIp: source.ip | ip("0.0.0.0")
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
---
apiVersion: "config.istio.io/v1alpha2"
kind: stackdriver
metadata:
  name: handler
  namespace: istio-system
spec:
  # Must be supplied for the stackdriver adapter to work
  project_id: example-project-id
  # One of the following must be set; the preferred method is "appCredentials"", which corresponds to
  # Google Application Default Credentials. See:
  #    https://developers.google.com/identity/protocols/application-default-credentials
  # If none is provided we default to app credentials.
  # appCredentials:
  # apiKey:
  serviceAccountPath: ./testdata/my-test-account-creds.json
  trace:
    sampleProbability: 1.0
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: stackdriver
  namespace: istio-system
spec:
  match: "true" # If omitted match is true.
  actions:
  - handler: handler.stackdriver
    instances:
    - default.tracespan
---
`
)

func TestReport(t *testing.T) {
	getExporterPrev := getExporterFunc
	defer func() {
		getExporterFunc = getExporterPrev
	}()
	var te testExporter
	getExporterFunc = func(_ context.Context, env adapter.Env, params *config.Params) (trace.Exporter, error) {
		return &te, nil
	}
	end, err := time.Parse("Mon Jan 2 15:04:05 -0700 MST 2006", "Mon Jan 2 15:04:05 -0700 MST 2006")
	if err != nil {
		t.Fatal(err)
	}
	start := end.Add(-100 * time.Millisecond)
	adapter_integration.RunTest(
		t,
		getInfo,
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
						"request.path":             "/foo/bar",
						"request.host":             "example.istio.com",
						"request.useragent":        "xxx",
						"request.size":             int64(100),
						"request.total_size":       int64(128),
						"response.size":            int64(500),
						"response.total_size":      int64(512),
						"source.name":              "srcsvc",
						"destination.service.name": "destsvc",
						"source.labels":            map[string]string{"version": "v1"},
						"source.ip":                []uint8{10, 0, 0, 1},
						"api.protocol":             "http",
						"request.method":           "POST",
						"response.code":            int64(200),
					},
				},
			},

			GetState: func(ctx interface{}) (interface{}, error) {
				return te.exported, nil
			},

			Configs: []string{
				traceSpanConfig,
			},

			Want: `{
        "AdapterState": [
         {
          "Annotations": null,
          "Attributes": {
           "http.host": "example.istio.com",
           "http.method": "POST",
           "http.path": "/foo/bar",
           "http.status_code": 200,
           "http.user_agent": "xxx",
           "principal": "unknown",
           "source.version": "v1"
          },
          "ChildSpanCount": 0,
          "Code": 0,
          "DroppedAnnotationCount": 0,
          "DroppedAttributeCount": 0,
          "DroppedLinkCount": 0,
          "DroppedMessageEventCount": 0,
          "EndTime": "2006-01-02T22:04:05Z",
          "HasRemoteParent": true,
          "Links": null,
          "Message": "OK",
          "MessageEvents": [
           {
            "CompressedByteSize": 128,
            "EventType": 2,
            "MessageID": 0,
            "Time": "2006-01-02T22:04:04.9Z",
            "UncompressedByteSize": 128
           },
           {
            "CompressedByteSize": 512,
            "EventType": 1,
            "MessageID": 0,
            "Time": "2006-01-02T22:04:05Z",
            "UncompressedByteSize": 512
           }
          ],
          "Name": "/foo/bar",
          "ParentSpanID": [
           0,
           32,
           0,
           0,
           0,
           0,
           0,
           1
          ],
          "SpanID": [
           162,
           251,
           74,
           29,
           26,
           150,
           211,
           18
          ],
          "SpanKind": 1,
          "StartTime": "2006-01-02T22:04:04.9Z",
          "TraceID": [
           70,
           58,
           195,
           92,
           159,
           100,
           19,
           173,
           72,
           72,
           90,
           57,
           83,
           187,
           97,
           36
          ],
          "TraceOptions": 1,
          "Tracestate": null
         }
        ],
        "Returns": [
         {
          "Check": {
           "Status": {},
           "ValidDuration": 0,
           "ValidUseCount": 0,
           "RouteDirective": null
          },
          "Quota": null,
          "Error": null
         }
        ]
      }`,
		},
	)
}

func getInfo() adapter.Info {
	clusterNameFn := func() (string, error) {
		cn, err := md.InstanceAttributeValue("cluster-name")
		if err != nil {
			return "", err
		}
		return cn, nil
	}
	mg := helper.NewMetadataGenerator(md.OnGCE, md.ProjectID, md.Zone, clusterNameFn)
	return adapter.Info{
		Name:        "stackdriver",
		Impl:        "istio.io/istio/mixer/adapte/stackdriver",
		Description: "[testing] Stackdriver Trace",
		SupportedTemplates: []string{
			tracespan.TemplateName,
		},
		DefaultConfig: &config.Params{},
		NewBuilder: func() adapter.HandlerBuilder {
			return NewBuilder(mg)
		}}
}

type testExporter struct {
	exported []*trace.SpanData
	flushes  int
}

func (te *testExporter) ExportSpan(sd *trace.SpanData) {
	te.exported = append(te.exported, sd)
}

func (te *testExporter) Flush() {
	te.flushes++
}
