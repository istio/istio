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

package opencensus

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.opencensus.io/trace"

	"istio.io/istio/mixer/template/tracespan"
)

func TestHandleTraceSpan(t *testing.T) {
	end := time.Now()
	start := end.Add(-10 * time.Millisecond)
	tests := []struct {
		name                   string
		vals                   []*tracespan.Instance
		wantSpans              []*trace.SpanData
		sampler                trace.Sampler
		wantFlushes            int
		wantName, wantEndpoint string
	}{
		{
			name: "minimal",
			vals: []*tracespan.Instance{
				{
					TraceId:      "463ac35c9f6413ad48485a3953bb6124",
					SpanId:       "a2fb4a1d1a96d312",
					ParentSpanId: "0020000000000001",
					Name:         "tracespan.test",
					SpanName:     "/io.opencensus.Service.Method",
					StartTime:    start,
					EndTime:      end,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					ClientSpan: false,
				},
			},
			wantSpans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 0x1,
					},
					ParentSpanID: trace.SpanID{0x0, 0x20, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1},
					SpanKind:     trace.SpanKindServer,
					Name:         "/io.opencensus.Service.Method",
					StartTime:    start,
					EndTime:      end,
					Attributes: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					Annotations: nil,
					MessageEvents: []trace.MessageEvent{
						{
							Time:      start,
							EventType: trace.MessageEventTypeRecv,
						},
						{
							Time:      end,
							EventType: trace.MessageEventTypeSent,
						},
					},
					Status:          trace.Status{Code: 0, Message: ""},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler:     trace.ProbabilitySampler(1.0),
			wantFlushes: 1,
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
					StartTime:    start,
					EndTime:      end,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					HttpStatusCode: 500,
				},
			},
			wantSpans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 0x1,
					},
					ParentSpanID: trace.SpanID{0x0, 0x20, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1},
					SpanKind:     trace.SpanKindServer,
					Name:         "/io.opencensus.Service.Method",
					StartTime:    start,
					EndTime:      end,
					Attributes: map[string]interface{}{
						"http.method":      "GET",
						"http.host":        "opencensus.io",
						"http.status_code": int64(500),
					},
					Annotations: nil,
					MessageEvents: []trace.MessageEvent{
						{
							Time:      start,
							EventType: trace.MessageEventTypeRecv,
						},
						{
							Time:      end,
							EventType: trace.MessageEventTypeSent,
						},
					},
					Status:          trace.Status{Code: 2, Message: `"UNKNOWN"`},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler:     trace.ProbabilitySampler(1.0),
			wantFlushes: 1,
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
					StartTime:    start,
					EndTime:      end,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					HttpStatusCode: 200,
				},
			},
			wantSpans:   []*trace.SpanData(nil),
			sampler:     trace.ProbabilitySampler(1.0),
			wantFlushes: 0,
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
					StartTime:    start,
					EndTime:      end,
					SpanTags: map[string]interface{}{
						"http.method": "GET",
						"http.host":   "opencensus.io",
					},
					HttpStatusCode: 200,
				},
			},
			wantSpans:   nil,
			sampler:     nil,
			wantFlushes: 0,
		},
		{
			name: "no parent - server span",
			vals: []*tracespan.Instance{
				{
					Name:              "tracespan.test",
					SpanName:          "/com.exmaple.Service.Method",
					StartTime:         start,
					EndTime:           end,
					SourceName:        "exampleclient.default",
					SourceIp:          net.ParseIP("10.0.0.1"),
					DestinationName:   "exampleserver.default",
					DestinationIp:     net.ParseIP("10.0.0.2"),
					RequestSize:       1024,
					RequestTotalSize:  1124,
					ResponseSize:      2048,
					ResponseTotalSize: 2200,
					HttpStatusCode:    200,
					SpanTags:          map[string]interface{}{},
					TraceId:           "463ac35c9f6413ad48485a3953bb6124",
					SpanId:            "a2fb4a1d1a96d312",
					ParentSpanId:      "",
					ApiProtocol:       "grpc",
					ClientSpan:        false,
				},
			},
			wantName:     "exampleserver.default",
			wantEndpoint: "10.0.0.2:0",
			wantSpans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 1,
					},
					ParentSpanID: trace.SpanID{},
					SpanKind:     trace.SpanKindServer,
					Name:         "/com.exmaple.Service.Method",
					StartTime:    start,
					EndTime:      end,
					Attributes: map[string]interface{}{
						"http.status_code": int64(200),
					},
					Annotations: nil,
					MessageEvents: []trace.MessageEvent{
						{
							Time:                 start,
							EventType:            trace.MessageEventTypeRecv,
							UncompressedByteSize: 1124,
							CompressedByteSize:   1124,
						},
						{
							Time:                 end,
							EventType:            trace.MessageEventTypeSent,
							UncompressedByteSize: 2200,
							CompressedByteSize:   2200,
						},
					},
					Status: trace.Status{
						Message: `"OK"`,
					},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler:     trace.AlwaysSample(),
			wantFlushes: 1,
		},
		{
			name: "client span",
			vals: []*tracespan.Instance{
				{
					Name:              "tracespan.test",
					SpanName:          "/com.exmaple.Service.Method",
					StartTime:         start,
					EndTime:           end,
					SourceName:        "exampleclient.default",
					SourceIp:          net.ParseIP("10.0.0.1"),
					DestinationName:   "exampleserver.default",
					DestinationIp:     net.ParseIP("10.0.0.2"),
					RequestSize:       1024,
					ResponseSize:      2048,
					HttpStatusCode:    200,
					SpanTags:          map[string]interface{}{},
					TraceId:           "463ac35c9f6413ad48485a3953bb6124",
					SpanId:            "a2fb4a1d1a96d312",
					ParentSpanId:      "b2fb4a1d1a96d312",
					ApiProtocol:       "grpc",
					ClientSpan:        true,
					RequestTotalSize:  1124,
					ResponseTotalSize: 2200,
				},
			},
			wantName:     "exampleclient.default",
			wantEndpoint: "10.0.0.1:0",
			wantSpans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 1,
					},
					ParentSpanID: trace.SpanID{0xb2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
					SpanKind:     trace.SpanKindClient,
					Name:         "/com.exmaple.Service.Method",
					StartTime:    start,
					EndTime:      end,
					Attributes: map[string]interface{}{
						"http.status_code": int64(200),
					},
					Annotations: nil,
					MessageEvents: []trace.MessageEvent{
						{
							Time:                 start,
							EventType:            trace.MessageEventTypeSent,
							UncompressedByteSize: 1124,
							CompressedByteSize:   1124,
						},
						{
							Time:                 end,
							EventType:            trace.MessageEventTypeRecv,
							UncompressedByteSize: 2200,
							CompressedByteSize:   2200,
						},
					},
					Status: trace.Status{
						Message: `"OK"`,
					},
					Links:           nil,
					HasRemoteParent: false,
				},
			},
			sampler:     trace.AlwaysSample(),
			wantFlushes: 1,
		},
		{
			name: "error span",
			vals: []*tracespan.Instance{
				{
					Name:              "tracespan.test",
					SpanName:          "/a/b/c",
					StartTime:         start,
					EndTime:           end,
					SourceName:        "exampleclient.default",
					SourceIp:          net.ParseIP("10.0.0.1"),
					DestinationName:   "exampleserver.default",
					DestinationIp:     net.ParseIP("10.0.0.2"),
					RequestSize:       1024,
					ResponseSize:      2048,
					HttpStatusCode:    500,
					SpanTags:          map[string]interface{}{},
					TraceId:           "463ac35c9f6413ad48485a3953bb6124",
					SpanId:            "a2fb4a1d1a96d312",
					ParentSpanId:      "b2fb4a1d1a96d312",
					ApiProtocol:       "http",
					ClientSpan:        true,
					RequestTotalSize:  1124,
					ResponseTotalSize: 2200,
				},
			},
			wantSpans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 1,
					},
					ParentSpanID: trace.SpanID{0xb2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
					SpanKind:     trace.SpanKindClient,
					Name:         "/a/b/c",
					StartTime:    start,
					EndTime:      end,
					Attributes: map[string]interface{}{
						"http.status_code": int64(500),
					},
					Annotations: nil,
					MessageEvents: []trace.MessageEvent{
						{
							Time:                 start,
							EventType:            trace.MessageEventTypeSent,
							UncompressedByteSize: 1124,
							CompressedByteSize:   1124,
						},
						{
							Time:                 end,
							EventType:            trace.MessageEventTypeRecv,
							UncompressedByteSize: 2200,
							CompressedByteSize:   2200,
						},
					},
					Status: trace.Status{
						Message: `"UNKNOWN"`,
						Code:    2,
					},
					Links:           nil,
					HasRemoteParent: false,
				},
			},
			sampler:     trace.AlwaysSample(),
			wantFlushes: 1,
		},
		{
			name: "client span rewrite id",
			vals: []*tracespan.Instance{
				{
					TraceId:             "463ac35c9f6413ad48485a3953bb6124",
					SpanId:              "a2fb4a1d1a96d312",
					Name:                "tracespan.test",
					SpanName:            "/io.opencensus.Service.Method",
					StartTime:           start,
					EndTime:             end,
					ClientSpan:          true,
					RewriteClientSpanId: true,
				},
			},
			wantSpans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0x9d, 0x91, 0x64, 0xde, 0xd2, 0x86, 0x11, 0xb9},
						TraceOptions: 0x1,
					},
					SpanKind:    trace.SpanKindClient,
					Name:        "/io.opencensus.Service.Method",
					StartTime:   start,
					EndTime:     end,
					Attributes:  map[string]interface{}{},
					Annotations: nil,
					MessageEvents: []trace.MessageEvent{
						{
							Time:                 start,
							EventType:            trace.MessageEventTypeSent,
							UncompressedByteSize: 0,
							CompressedByteSize:   0,
						},
						{
							Time:                 end,
							EventType:            trace.MessageEventTypeRecv,
							UncompressedByteSize: 0,
							CompressedByteSize:   0,
						},
					},
					Status:          trace.Status{Code: 0, Message: ""},
					Links:           nil,
					HasRemoteParent: false,
				},
			},
			sampler:     trace.ProbabilitySampler(1.0),
			wantFlushes: 1,
		},
		{
			name: "server span rewrite id",
			vals: []*tracespan.Instance{
				{
					TraceId:             "463ac35c9f6413ad48485a3953bb6124",
					SpanId:              "a2fb4a1d1a96d312",
					Name:                "tracespan.test",
					SpanName:            "/io.opencensus.Service.Method",
					StartTime:           start,
					EndTime:             end,
					RewriteClientSpanId: true,
				},
			},
			wantSpans: []*trace.SpanData{
				{
					SpanContext: trace.SpanContext{
						TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
						SpanID:       trace.SpanID{0xa2, 0xfb, 0x4a, 0x1d, 0x1a, 0x96, 0xd3, 0x12},
						TraceOptions: 0x1,
					},
					ParentSpanID: trace.SpanID{0x9d, 0x91, 0x64, 0xde, 0xd2, 0x86, 0x11, 0xb9},
					SpanKind:     trace.SpanKindServer,
					Name:         "/io.opencensus.Service.Method",
					StartTime:    start,
					EndTime:      end,
					Attributes:   map[string]interface{}{},
					Annotations:  nil,
					MessageEvents: []trace.MessageEvent{
						{
							Time:                 start,
							EventType:            trace.MessageEventTypeRecv,
							UncompressedByteSize: 0,
							CompressedByteSize:   0,
						},
						{
							Time:                 end,
							EventType:            trace.MessageEventTypeSent,
							UncompressedByteSize: 0,
							CompressedByteSize:   0,
						},
					},
					Status:          trace.Status{Code: 0, Message: ""},
					Links:           nil,
					HasRemoteParent: true,
				},
			},
			sampler:     trace.ProbabilitySampler(1.0),
			wantFlushes: 1,
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			exporter := &testExporter{}
			h := NewTraceHandler(
				func(name, endpoint string) trace.Exporter {
					if tt.wantName != "" && tt.wantName != name {
						t.Errorf("name = %q; want %q", name, tt.wantName)
					}
					if tt.wantEndpoint != "" && tt.wantEndpoint != endpoint {
						t.Errorf("endpoint = %q; want %q", endpoint, tt.wantEndpoint)
					}
					return exporter
				},
				tt.sampler)
			ctx := context.Background()
			err := h.HandleTraceSpan(ctx, tt.vals)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(exporter.exported, tt.wantSpans); diff != "" {
				t.Errorf("Exported spans differ, -got +want: %s\n", diff)
			}
			if got, want := exporter.flushes, tt.wantFlushes; got != want {
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

func TestExtractMessageEvents(t *testing.T) {
	end := time.Now()
	start := end.Add(-10 * time.Millisecond)
	tests := []struct {
		name string
		inst *tracespan.Instance
		want []trace.MessageEvent
	}{
		{
			name: "no sizes",
			inst: &tracespan.Instance{RequestTotalSize: 0, ResponseTotalSize: 0},
			want: []trace.MessageEvent{
				{EventType: trace.MessageEventTypeRecv, CompressedByteSize: 0, UncompressedByteSize: 0, Time: start},
				{EventType: trace.MessageEventTypeSent, CompressedByteSize: 0, UncompressedByteSize: 0, Time: end},
			},
		},
		{
			name: "only response size",
			inst: &tracespan.Instance{RequestTotalSize: 0, ResponseTotalSize: 1024},
			want: []trace.MessageEvent{
				{EventType: trace.MessageEventTypeRecv, CompressedByteSize: 0, UncompressedByteSize: 0, Time: start},
				{EventType: trace.MessageEventTypeSent, CompressedByteSize: 1024, UncompressedByteSize: 1024, Time: end},
			},
		},
		{
			name: "only request size",
			inst: &tracespan.Instance{RequestTotalSize: 1024, ResponseTotalSize: 0},
			want: []trace.MessageEvent{
				{EventType: trace.MessageEventTypeRecv, CompressedByteSize: 1024, UncompressedByteSize: 1024, Time: start},
				{EventType: trace.MessageEventTypeSent, CompressedByteSize: 0, UncompressedByteSize: 0, Time: end},
			},
		},
		{
			name: "sent and received",
			inst: &tracespan.Instance{RequestTotalSize: 1024, ResponseTotalSize: 2048},
			want: []trace.MessageEvent{
				{EventType: trace.MessageEventTypeRecv, CompressedByteSize: 1024, UncompressedByteSize: 1024, Time: start},
				{EventType: trace.MessageEventTypeSent, CompressedByteSize: 2048, UncompressedByteSize: 2048, Time: end},
			},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			val := *tt.inst
			val.StartTime = start
			val.EndTime = end
			got := extractMessageEvents(&val)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("extractMessageEvents(tt.inst) -got +want: %s", diff)
			}
		})
	}
}
