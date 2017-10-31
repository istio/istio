// Copyright 2017 Istio Authors.
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
	"fmt"
	"io"

	"github.com/golang/glog"
	zt "github.com/openzipkin/zipkin-go-opentracing"
)

type loggingRecorder struct{}

// LoggingRecorder returns a SpanRecorder which logs writes its spans to glog.
func LoggingRecorder() zt.SpanRecorder {
	return loggingRecorder{}
}

// RecordSpan writes span to glog.Info.
//
// TODO: allow a user to specify trace log level.
func (l loggingRecorder) RecordSpan(span zt.RawSpan) {
	glog.Info(spanToString(span))
}

type ioRecorder struct {
	sink io.Writer
}

// IORecorder returns a SpanRecorder which writes its spans to the provided io.Writer.
func IORecorder(w io.Writer) zt.SpanRecorder {
	return ioRecorder{w}
}

// RecordSpan writes span to stdout.
func (s ioRecorder) RecordSpan(span zt.RawSpan) {
	/* #nosec */
	_, _ = fmt.Fprintln(s.sink, spanToString(span))
}

func spanToString(span zt.RawSpan) string {
	return fmt.Sprintf("%v %s %v trace: %v; span: %d; parent: %d; tags: %v; logs: %v",
		span.Start, span.Operation, span.Duration, span.Context.TraceID, span.Context.SpanID, span.Context.ParentSpanID, span.Tags, span.Logs)
}
