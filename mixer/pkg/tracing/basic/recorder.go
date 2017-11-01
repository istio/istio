// Copyright 2017 Istio Authors
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
//
// Copyright (c) 2017, gRPC Ecosystem
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// * Redistributions of source code must retain the above copyright notice, this
// list of conditions and the following disclaimer.
//
// * Redistributions in binary form must reproduce the above copyright notice,
// this list of conditions and the following disclaimer in the documentation
// and/or other materials provided with the distribution.
//
// * Neither the name of grpc-opentracing nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//
// TODO: Vet with lawyers what we need to do when we depend on BSD-3 licensed codebases.

package basic

import (
	"fmt"
	"io"

	"github.com/golang/glog"
	bt "github.com/opentracing/basictracer-go"
)

type loggingRecorder struct{}

// LoggingRecorder returns a SpanRecorder which logs writes its spans to glog.
func LoggingRecorder() bt.SpanRecorder {
	return loggingRecorder{}
}

// RecordSpan writes span to glog.Info.
//
// TODO: allow a user to specify trace log level.
func (l loggingRecorder) RecordSpan(span bt.RawSpan) {
	glog.Info(spanToString(span))
}

type ioRecorder struct {
	sink io.Writer
}

// IORecorder returns a SpanRecorder which writes its spans to the provided io.Writer.
func IORecorder(w io.Writer) bt.SpanRecorder {
	return ioRecorder{w}
}

// RecordSpan writes span to stdout.
func (s ioRecorder) RecordSpan(span bt.RawSpan) {
	/* #nosec */
	_, _ = fmt.Fprintln(s.sink, spanToString(span))
}

func spanToString(span bt.RawSpan) string {
	return fmt.Sprintf("%v %s %v trace: %d; span: %d; parent: %d; tags: %v; logs: %v",
		span.Start, span.Operation, span.Duration, span.Context.TraceID, span.Context.SpanID, span.ParentSpanID, span.Tags, span.Logs)
}
