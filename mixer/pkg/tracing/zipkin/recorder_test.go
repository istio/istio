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
	"bytes"
	"testing"
	"time"

	"github.com/golang/glog"
	zt "github.com/openzipkin/zipkin-go-opentracing"
	"github.com/openzipkin/zipkin-go-opentracing/types"
)

var parentID = uint64(1)
var span = zt.RawSpan{
	Context: zt.SpanContext{
		TraceID:      types.TraceID{High: 0, Low: 1},
		SpanID:       2,
		Sampled:      false,
		ParentSpanID: &parentID,
	},
	Operation: "test span",
	Start:     time.Now(),
}

func TestLoggingRecorder(t *testing.T) {
	recorder := LoggingRecorder()
	recorder.RecordSpan(span)

	glog.Flush()
	if glog.Stats.Info.Lines() == 0 {
		t.Errorf("LoggingRecorder didn't write span %v to info", span)
	}
}

func TestIORecorder(t *testing.T) {
	buf := new(bytes.Buffer)
	recorder := IORecorder(buf)
	recorder.RecordSpan(span)

	// The IORecorder uses fmt.Fprintln, so we always have a newline at the end.
	expected := spanToString(span) + "\n"
	if buf.String() != expected {
		t.Errorf("Expected output string '%s' for span %v, actual: '%s'", expected, span, buf.String())
	}
}
