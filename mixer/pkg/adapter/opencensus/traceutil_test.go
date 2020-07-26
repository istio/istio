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

package opencensus

import (
	"fmt"
	"testing"

	"go.opencensus.io/trace"
)

func TestExtractParentContext(t *testing.T) {
	tests := []struct {
		name          string
		traceID       string
		parentSpanID  string
		parentContext trace.SpanContext
		ok            bool
	}{
		{
			name:         "missing TraceID",
			traceID:      "",
			parentSpanID: "0020000000000001",
			ok:           false,
		},
		{
			name:         "ParentSpanID not present (root span)",
			traceID:      "463ac35c9f6413ad48485a3953bb6124",
			parentSpanID: "",
			ok:           true,
			parentContext: trace.SpanContext{
				TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
				SpanID:       trace.SpanID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
				TraceOptions: 0x0,
			},
		},
		{
			name:         "ParentSpanID present",
			traceID:      "463ac35c9f6413ad48485a3953bb6124",
			parentSpanID: "0020000000000001",
			ok:           true,
			parentContext: trace.SpanContext{
				TraceID:      trace.TraceID{0x46, 0x3a, 0xc3, 0x5c, 0x9f, 0x64, 0x13, 0xad, 0x48, 0x48, 0x5a, 0x39, 0x53, 0xbb, 0x61, 0x24},
				SpanID:       trace.SpanID{0x0, 0x20, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1},
				TraceOptions: 0x0,
			},
		},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			gotParentContext, gotOK := ExtractParentContext(tt.traceID, tt.parentSpanID)
			if gotOK != tt.ok {
				t.Errorf("got ok = %v; want %v", gotOK, tt.ok)
				return
			}
			if gotParentContext != tt.parentContext {
				t.Errorf("got parentContext = %#v; want %#v", gotParentContext, tt.parentContext)
			}
		})
	}
}
