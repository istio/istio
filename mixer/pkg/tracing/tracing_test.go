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
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bytes"
)

var (
	testUser = "test"
	testPass = "pass"
)

func TestNewTracer(t *testing.T) {
	srv := &testServer{}
	zipkinServer := httptest.NewServer(handler(srv.receiveZipkin))
	zipkinAuthServer := httptest.NewServer(authHandler(srv.receiveZipkin))
	jaegerServer := httptest.NewServer(handler(srv.receiveJaeger))
	jaegerAuthServer := httptest.NewServer(authHandler(srv.receiveJaeger))
	defer zipkinServer.Close()
	defer zipkinAuthServer.Close()
	defer jaegerServer.Close()
	defer jaegerAuthServer.Close()

	zipkinOpt := WithZipkinCollector(zipkinServer.URL)
	zipkinAuthOpt := WithBasicAuthZipkinCollector(zipkinAuthServer.URL, testUser, testPass)
	zipkinBadAuthOpt := WithBasicAuthZipkinCollector(zipkinAuthServer.URL, "fail", "fail")

	jaegerOpt := WithJaegerHTTPCollector(jaegerServer.URL)
	jaegerAuthOpt := WithBasicAuthJaegerHTTPCollector(jaegerAuthServer.URL, testUser, testPass)
	jaegerBadAuthOpt := WithBasicAuthJaegerHTTPCollector(jaegerAuthServer.URL, "fail", "fail")

	var spanOut bytes.Buffer
	loggingOpt := withLogger(&testLogger{&spanOut})

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
		{"with jaeger auth", []Option{jaegerAuthOpt}, true, false, false},
		{"with jaeger auth (fail)", []Option{jaegerBadAuthOpt}, false, false, false},
		{"with zipkin", []Option{zipkinOpt}, false, true, false},
		{"with zipkin auth", []Option{zipkinAuthOpt}, false, true, false},
		{"with zipkin auth (fail)", []Option{zipkinBadAuthOpt}, false, false, false},
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

func authHandler(fn http.HandlerFunc) http.Handler {
	return &testHandler{basicAuth(fn)}
}

func basicAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
		if len(auth) != 2 || auth[0] != "Basic" {
			http.Error(w, "authz failed", http.StatusUnauthorized)
			return
		}
		creds, _ := base64.StdEncoding.DecodeString(auth[1])
		userPass := strings.SplitN(string(creds), ":", 2)
		if len(userPass) != 2 || !check(userPass[0], userPass[1]) {
			http.Error(w, "authz failed", http.StatusUnauthorized)
			return
		}
		fn(w, r)
	}
}

func check(user, pass string) bool {
	return user == testUser && pass == testPass
}

type testLogger struct {
	out io.Writer
}

func (t *testLogger) Error(msg string) {
	fmt.Fprintf(t.out, msg)
}

func (t *testLogger) Infof(msg string, args ...interface{}) {
	fmt.Fprintf(t.out, msg, args)
}
