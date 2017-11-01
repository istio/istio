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

package prometheus

import (
	"fmt"
	"net/http"
	"testing"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func doesNothing(http.ResponseWriter, *http.Request) {}

func TestServer(t *testing.T) {
	testAddr := "127.0.0.1:9992"
	s := newServer(testAddr)

	if err := s.Start(test.NewEnv(t), http.HandlerFunc(doesNothing)); err != nil {
		t.Fatalf("Start() failed unexpectedly: %v", err)
	}

	testURL := fmt.Sprintf("http://%s%s", testAddr, metricsPath)
	// verify a response is returned from "/metrics"
	resp, err := http.Get(testURL)
	if err != nil {
		t.Fatalf("Failed to retrieve '%s' path: %v", metricsPath, err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("http.GET => %v, wanted '%v'", resp.StatusCode, http.StatusOK)
	}

	_ = resp.Body.Close()

	s2 := newServer(testAddr)
	if err := s2.Start(test.NewEnv(t), http.HandlerFunc(doesNothing)); err == nil {
		t.Fatal("Start() succeeded, expecting a failure")
	}

	if err := s.Close(); err != nil {
		t.Errorf("Failed to close server properly: %v", err)
	}

	if err := s2.Close(); err != nil {
		t.Errorf("Failed to close server properly: %v", err)
	}
}

func TestServerInst_Close(t *testing.T) {
	testAddr := "127.0.0.1:0"
	s := newServer(testAddr)
	env := test.NewEnv(t)

	if err := s.Start(env, http.HandlerFunc(doesNothing)); err != nil {
		t.Fatalf("Start() failed unexpectedly: %v", err)
	}

	if err := s.Start(env, http.HandlerFunc(doesNothing)); err != nil {
		t.Fatalf("Start() failed unexpectedly: %v", err)
	}

	if s.srv == nil {
		t.Fatalf("expected server to be non-nil")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Failed to close server properly: %v", err)
	}

	if s.srv == nil {
		t.Fatalf("expected server to be non-nil")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Failed to close server properly: %v", err)
	}

	if s.srv != nil {
		t.Fatalf("expected server to be nil: %v", s.srv)
	}
}
