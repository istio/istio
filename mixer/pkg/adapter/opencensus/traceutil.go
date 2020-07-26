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
	"go.opencensus.io/plugin/ochttp/propagation/b3"
	"go.opencensus.io/trace"
)

// ExtractParentContext returns an OpenCensus SpanContext that corresponds to
// the parent of the given trace and parent span id that are in the hex encoded
// string that comes from tracespan instances.  The second return value will be
// false if the trace id is invalid.
func ExtractParentContext(traceID, parentSpanID string) (trace.SpanContext, bool) {
	var (
		parentContext trace.SpanContext
		ok            bool
	)
	if parentContext.TraceID, ok = b3.ParseTraceID(traceID); !ok {
		return trace.SpanContext{}, false
	}
	parentContext.SpanID, _ = b3.ParseSpanID(parentSpanID)
	return parentContext, true
}

// ExtractSpanContext returns an OpenCensus SpanContext that corresponds to the
// the given span id and parent span context.  The second return value will be
// false if the span id is invalid.
func ExtractSpanContext(spanID string, parent trace.SpanContext) (trace.SpanContext, bool) {
	var (
		spanContext trace.SpanContext
		ok          bool
	)
	spanContext.TraceID = parent.TraceID
	if spanContext.SpanID, ok = b3.ParseSpanID(spanID); !ok {
		return trace.SpanContext{}, false
	}
	return spanContext, true
}
