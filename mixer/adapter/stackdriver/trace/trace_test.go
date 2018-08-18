// Copyright 2018 the Istio Authors.
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
	"fmt"
	"reflect"
	"testing"
	"time"

	"go.opencensus.io/trace"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/tracespan"
)

var dummyShouldFill = func() bool { return true }
var dummyMetadataFn = func() (string, error) { return "", nil }

func TestHandleTraceSpan(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		vals    []*tracespan.Instance
		spans   []*trace.SpanData
		sampler trace.Sampler
		flushes int
	}{
		{
			name: "default",
			vals: []*tracespan.Instance{
				{
					TraceId:      "463ac35c9f6413ad48485a3953bb6124",
					SpanId:       "a2fb4a1d1a96d312",
					ParentSpanId: "0020000000000001",
					Name:         "tracespan.test",
					SpanName:     "/io.opencensus.Service.Method",
					StartTime:    now.Add(-10 * time.Millisecond),
					EndTime:      now,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
				},
			},
			spans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 0x1,
					},
					ParentSpanID: trace.SpanID{0x0, 0x20, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1},
					SpanKind:     trace.SpanKindServer,
					Name:         "/io.opencensus.Service.Method",
					StartTime:    now.Add(-10 * time.Millisecond),
					EndTime:      now,
					Attributes: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					Annotations:     nil,
					MessageEvents:   nil,
					Status:          trace.Status{Code: 0, Message: ""},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler: trace.ProbabilitySampler(1.0),
			flushes: 1,
		},
		{
			name: "error",
			vals: []*tracespan.Instance{
				{
					TraceId:      "463ac35c9f6413ad48485a3953bb6124",
					SpanId:       "a2fb4a1d1a96d312",
					ParentSpanId: "0020000000000001",
					Name:         "tracespan.test",
					SpanName:     "/io.opencensus.Service.Method",
					StartTime:    now.Add(-10 * time.Millisecond),
					EndTime:      now,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					HttpStatusCode: 500,
				},
			},
			spans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 0x1,
					},
					ParentSpanID: trace.SpanID{0x0, 0x20, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1},
					SpanKind:     trace.SpanKindServer,
					Name:         "/io.opencensus.Service.Method",
					StartTime:    now.Add(-10 * time.Millisecond),
					EndTime:      now,
					Attributes: map[string]interface{}{
						"http.method":      "GET",
						"http.host":        "opencensus.io",
						"http.status_code": int64(500),
					},
					Annotations:     nil,
					MessageEvents:   nil,
					Status:          trace.Status{Code: 2, Message: `"UNKNOWN"`},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler: trace.ProbabilitySampler(1.0),
			flushes: 1,
		},
		{
			name: "no tracing",
			vals: []*tracespan.Instance{
				{
					TraceId:      "",
					SpanId:       "",
					ParentSpanId: "",
					Name:         "tracespan.test",
					SpanName:     "/io.opencensus.Service.Method",
					StartTime:    now.Add(-10 * time.Millisecond),
					EndTime:      now,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					HttpStatusCode: 200,
				},
			},
			spans:   []*trace.SpanData(nil),
			sampler: trace.ProbabilitySampler(1.0),
			flushes: 0,
		},
		{
			name: "tracing disabled",
			vals: []*tracespan.Instance{
				{
					TraceId:      "463ac35c9f6413ad48485a3953bb6124",
					SpanId:       "a2fb4a1d1a96d312",
					ParentSpanId: "0020000000000001",
					Name:         "tracespan.test",
					SpanName:     "/io.opencensus.Service.Method",
					StartTime:    now.Add(-10 * time.Millisecond),
					EndTime:      now,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					HttpStatusCode: 200,
				},
			},
			spans:   nil,
			sampler: nil,
			flushes: 0,
		},
		{
			name: "client span",
			vals: []*tracespan.Instance{
				{
					TraceId:    "463ac35c9f6413ad48485a3953bb6124",
					SpanId:     "a2fb4a1d1a96d312",
					Name:       "tracespan.test",
					SpanName:   "/io.opencensus.Service.Method",
					StartTime:  now.Add(-10 * time.Millisecond),
					EndTime:    now,
					ClientSpan: true,
				},
			},
			spans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 0x1,
					},
					SpanKind:        trace.SpanKindClient,
					Name:            "/io.opencensus.Service.Method",
					StartTime:       now.Add(-10 * time.Millisecond),
					EndTime:         now,
					Attributes:      map[string]interface{}{},
					Annotations:     nil,
					MessageEvents:   nil,
					Status:          trace.Status{Code: 0, Message: ""},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler: trace.ProbabilitySampler(1.0),
			flushes: 1,
		},
		{
			name: "client span rewrite id",
			vals: []*tracespan.Instance{
				{
					TraceId:             "463ac35c9f6413ad48485a3953bb6124",
					SpanId:              "a2fb4a1d1a96d312",
					Name:                "tracespan.test",
					SpanName:            "/io.opencensus.Service.Method",
					StartTime:           now.Add(-10 * time.Millisecond),
					EndTime:             now,
					ClientSpan:          true,
					RewriteClientSpanId: true,
				},
			},
			spans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0x9d, 0x91, 0x64, 0xde, 0xd2, 0x86, 0x11, 0xb9},
						TraceOptions: 0x1,
					},
					SpanKind:        trace.SpanKindClient,
					Name:            "/io.opencensus.Service.Method",
					StartTime:       now.Add(-10 * time.Millisecond),
					EndTime:         now,
					Attributes:      map[string]interface{}{},
					Annotations:     nil,
					MessageEvents:   nil,
					Status:          trace.Status{Code: 0, Message: ""},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler: trace.ProbabilitySampler(1.0),
			flushes: 1,
		},
		{
			name: "server span rewrite id",
			vals: []*tracespan.Instance{
				{
					TraceId:             "463ac35c9f6413ad48485a3953bb6124",
					SpanId:              "a2fb4a1d1a96d312",
					Name:                "tracespan.test",
					SpanName:            "/io.opencensus.Service.Method",
					StartTime:           now.Add(-10 * time.Millisecond),
					EndTime:             now,
					RewriteClientSpanId: true,
				},
			},
			spans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 0x1,
					},
					ParentSpanID:    trace.SpanID{0x9d, 0x91, 0x64, 0xde, 0xd2, 0x86, 0x11, 0xb9},
					SpanKind:        trace.SpanKindServer,
					Name:            "/io.opencensus.Service.Method",
					StartTime:       now.Add(-10 * time.Millisecond),
					EndTime:         now,
					Attributes:      map[string]interface{}{},
					Annotations:     nil,
					MessageEvents:   nil,
					Status:          trace.Status{Code: 0, Message: ""},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler: trace.ProbabilitySampler(1.0),
			flushes: 1,
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			h := handler{sampler: tt.sampler}
			exporter := &testExporter{}
			h.te = exporter
			ctx := context.Background()
			err := h.HandleTraceSpan(ctx, tt.vals)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := exporter.exported, tt.spans; !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v\nwant: %+v\n", got, want)
				if len(got) >= 1 && len(want) >= 1 {
					t.Errorf(" got[0]: %#v", got[0])
					t.Errorf("want[0]: %#v", want[0])
				}
			}
			if got, want := exporter.flushes, tt.flushes; got != want {
				t.Errorf("exporter.flushes = %d; want %d", got, want)
			}
		})
	}
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

func TestBuildSpanData(t *testing.T) {
	now := time.Now()
	traceSpan := &tracespan.Instance{
		Name:      "tracespan.test",
		SpanName:  "/io.opencensus.Service.Method",
		StartTime: now.Add(-10 * time.Millisecond),
		EndTime:   now,
		SpanTags: map[string]interface{}{
			"http.method": "GET",
			"http.host":   "opencensus.io",
			"http.path":   "/io.opencensus.Service.Method",
		},
		HttpStatusCode: 200,
	}

	parentContext := trace.SpanContext{
		TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
		SpanID:       trace.SpanID{0x0, 0x20, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1},
		TraceOptions: 0x0,
	}
	spanContext := trace.SpanContext{
		TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
		SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
		TraceOptions: 0x0,
	}

	spanData := buildSpanData(traceSpan, parentContext, spanContext)

	if got, want := spanData.Name, "/io.opencensus.Service.Method"; got != want {
		t.Errorf("spanData.Name = %q; want %q", got, want)
	}
	if got, want := spanData.Attributes["http.method"], "GET"; got != want {
		t.Errorf("spanData.Attributes[http.method] = %q; want %q", got, want)
	}
	if got, want := spanData.Attributes["http.status_code"], int64(200); got != want {
		t.Errorf("spanData.Attributes[http.status_cpde] = %v; want %v", got, want)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		params  *config.Params_Trace
		wantErr bool
	}{
		{"zero", &config.Params_Trace{SampleProbability: 0}, false},
		{"10percent", &config.Params_Trace{SampleProbability: 0.1}, false},
		{"negative", &config.Params_Trace{SampleProbability: -1}, true},
		{"too big", &config.Params_Trace{SampleProbability: 100}, true},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			var (
				b builder
			)
			b.SetAdapterConfig(&config.Params{Trace: tt.params})
			err := b.Validate()
			if !tt.wantErr && err != nil {
				t.Errorf("wantErr = false, got %s", err)
			}
			if tt.wantErr && err == nil {
				t.Errorf("wantErr, got nil")
			}
		})
	}
}

func TestProjectID(t *testing.T) {
	getExporterFunc = func(_ context.Context, _ adapter.Env, params *config.Params) (trace.Exporter, error) {
		if params.ProjectId != "pid" {
			return nil, fmt.Errorf("wanted pid got %v", params.ProjectId)
		}
		return nil, nil
	}

	tests := []struct {
		name string
		cfg  *config.Params
		pid  func() (string, error)
	}{
		{
			"empty project id",
			&config.Params{
				ProjectId: "",
			},
			func() (string, error) { return "pid", nil },
		},
		{
			"filled project id",
			&config.Params{
				ProjectId: "pid",
			},
			func() (string, error) { return "meta-pid", nil },
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			mg := helper.NewMetadataGenerator(dummyShouldFill, tt.pid, dummyMetadataFn, dummyMetadataFn)
			b := &builder{mg: mg}
			b.SetAdapterConfig(tt.cfg)
			_, err := b.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Errorf("Project id is not expected: %v", err)
			}
		})
	}
}
