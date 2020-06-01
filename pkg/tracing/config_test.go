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

package tracing

import (
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	ot "github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go/transport/zipkin"

	"istio.io/pkg/log"
)

func TestConfigure(t *testing.T) {
	srv := &testServer{}
	zipkinServer := httptest.NewServer(handler(srv.receiveZipkin))
	jaegerServer := httptest.NewServer(handler(srv.receiveJaeger))
	defer zipkinServer.Close()
	defer jaegerServer.Close()

	cases := []struct {
		name       string
		opts       Options
		wantJaeger bool
		wantZipkin bool
		wantLog    bool
	}{
		{"no options", Options{}, false, false, false},
		{"with logging", Options{LogTraceSpans: true, SamplingRate: 1.0}, false, false, true},
		{"with logging and no sampling", Options{LogTraceSpans: true}, false, false, false},
		{"with jaeger", Options{JaegerURL: jaegerServer.URL, SamplingRate: 1.0}, true, false, false},
		{"with zipkin", Options{ZipkinURL: zipkinServer.URL, SamplingRate: 1.0}, false, true, false},
		{"with jaeger and logging", Options{JaegerURL: jaegerServer.URL, SamplingRate: 1.0}, true, false, false},
		{"with zipkin and logging", Options{ZipkinURL: zipkinServer.URL, LogTraceSpans: true, SamplingRate: 1.0}, false, true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			lines, err := captureStdout(func() {

				err := log.Configure(log.DefaultOptions())
				if err != nil {
					t.Fatalf("Unable to configure logging: %v", err)
				}

				closer, err := Configure("test-service", &c.opts)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				tracer := ot.GlobalTracer()

				span := tracer.StartSpan("test-operation")
				span.Finish()

				// force a flush of spans to endpoints
				_ = closer.Close()

				// force the log to flush
				_ = log.Sync()
			})

			if err != nil {
				t.Fatalf("Unable to capture standard output: %v", err)
			}

			if c.wantJaeger != srv.receivedJaeger {
				t.Errorf("Jaeger trace received state unexpected: got %t, want %t", srv.receivedJaeger, c.wantJaeger)
			}

			if c.wantZipkin != srv.receivedZipkin {
				t.Errorf("Zipkin trace received state unexpected: got %t, want %t", srv.receivedZipkin, c.wantZipkin)
			}

			if c.wantLog && len(lines) != 2 {
				t.Errorf("Trace data not sent to logger")
			}
		})

		srv.reset()
	}
}

func TestZipkinError(t *testing.T) {
	o := DefaultOptions()
	o.ZipkinURL = "foobar"

	_, err := configure("tracing-test", o, func(url string, options ...zipkin.HTTPOption) (*zipkin.HTTPTransport, error) {
		return nil, errors.New("BAD")
	})

	if err == nil {
		t.Error("Expecting failure, got success")
	}
}

func TestSpanLogger(t *testing.T) {
	sl := spanLogger{}

	lines, err := captureStdout(func() {
		err := log.Configure(log.DefaultOptions())
		if err != nil {
			t.Fatalf("Unable to configure logging: %v", err)
		}

		sl.Error("ERROR")
		sl.Infof("INFO")

		log.Sync() // nolint: errcheck
	})

	if err != nil {
		t.Fatalf("Unable to capture stdout: %v", err)
	}

	if len(lines) != 3 {
		t.Errorf("Expecting 3 lines, got %#v", lines)
	}

	sl.Close()
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

// Runs the given function while capturing everything sent to stdout
func captureStdout(f func()) ([]string, error) {
	tf, err := ioutil.TempFile("", "tracing_test")
	if err != nil {
		return nil, err
	}

	old := os.Stdout
	os.Stdout = tf

	f()

	os.Stdout = old
	path := tf.Name()
	_ = tf.Sync()
	_ = tf.Close()

	content, err := ioutil.ReadFile(path)
	_ = os.Remove(path)

	if err != nil {
		return nil, err
	}

	return strings.Split(string(content), "\n"), nil
}

func TestConfigWithBadOptions(t *testing.T) {
	o := DefaultOptions()
	o.JaegerURL = "https://foo"
	o.ZipkinURL = "https://bar"

	if _, err := Configure("foo", o); err == nil {
		t.Error("Expecting failure, got success")
	}
}
