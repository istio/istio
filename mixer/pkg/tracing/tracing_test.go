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

package tracing

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	jaeger "github.com/uber/jaeger-client-go"
)

var (
	testUser = "test"
	testPass = "pass"
)

func TestNewTracer(t *testing.T) {
	srv := &testServer{}
	zipkinServer := httptest.NewServer(handler(srv.receiveZipkin))
	jaegerServer := httptest.NewServer(handler(srv.receiveJaeger))
	defer zipkinServer.Close()
	defer jaegerServer.Close()
	zipkinOpt := WithZipkinCollector(zipkinServer.URL)
	jaegerOpt := WithJaegerHTTPCollector(jaegerServer.URL)

	var spanOut bytes.Buffer
	loggingOpt := withReporter(&testReporter{&spanOut})

	cases := []struct {
		name       string
		opts       []Option
		wantJaeger bool
		wantZipkin bool
		wantLog    bool
	}{
		{"no options", nil, false, false, false},
		{"with logging", []Option{loggingOpt}, false, false, true},
		{"with jaeger", []Option{jaegerOpt}, true, false, false},
		{"with zipkin", []Option{zipkinOpt}, false, true, false},
		{"with jaeger and zipkin", []Option{jaegerOpt, zipkinOpt}, true, true, false},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			tracer, closer, err := NewTracer("test-service", v.opts...)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			span := tracer.StartSpan("test-operation")
			span.Finish()

			// force a flush of spans to endpoints
			closer.Close()

			if v.wantJaeger != srv.receivedJaeger {
				t.Errorf("Jaeger trace received state unexpected: got %t, want %t", srv.receivedJaeger, v.wantJaeger)
			}

			if v.wantZipkin != srv.receivedZipkin {
				t.Errorf("Zipkin trace received state unexpected: got %t, want %t", srv.receivedZipkin, v.wantZipkin)
			}
			if v.wantLog && len(spanOut.Bytes()) == 0 {
				t.Errorf("Trace data not sent to logger")
			}
		})
		srv.reset()
		spanOut.Reset()
	}
}

type testServer struct {
	receivedJaeger bool
	receivedZipkin bool
}

func (t *testServer) reset() {
	t.receivedJaeger = false
	t.receivedZipkin = false
}

func (t *testServer) receiveJaeger(resp http.ResponseWriter, req *http.Request) {
	t.receivedJaeger = true
}

func (t *testServer) receiveZipkin(resp http.ResponseWriter, req *http.Request) {
	t.receivedZipkin = true
}

type testHandler struct {
	fn http.HandlerFunc
}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.fn(w, r)
}

func handler(fn http.HandlerFunc) http.Handler {
	return &testHandler{fn}
}

type testReporter struct {
	out io.Writer
}

func (t testReporter) Report(span *jaeger.Span) {
	fmt.Fprintf(t.out, "Reporting span: %v, %v", span.OperationName(), span.String())
}

func (testReporter) Close() {}

func (t testReporter) Error(msg string) {
	fmt.Fprintf(t.out, msg)
}

func (t testReporter) Infof(msg string, args ...interface{}) {
	fmt.Fprintf(t.out, msg, args)
}
