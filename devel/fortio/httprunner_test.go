// Copyright 2017 Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Adapted from istio/proxy/test/backend/echo with error handling and
// concurrency fixes and making it as low overhead as possible
// (no std output by default)

package fortio

import (
	"fmt"
	"net/http"
	"testing"
)

func TestHTTPRunner(t *testing.T) {
	SetLogLevel(Info)
	http.HandleFunc("/foo/", EchoHandler)
	port := DynamicHTTPServer(false)
	baseURL := fmt.Sprintf("http://localhost:%d/", port)

	opts := HTTPRunnerOptions{
		RunnerOptions: RunnerOptions{
			QPS: 100,
		},
		URL: baseURL,
	}
	_, err := RunHTTPTest(&opts)
	if err == nil {
		t.Error("Expecting an error but didn't get it when not using full url")
	}
	opts.URL = baseURL + "foo/bar"
	res, err := RunHTTPTest(&opts)
	if err != nil {
		t.Error(err)
		return
	}
	totalReq := res.DurationHistogram.Count
	httpOk := res.RetCodes[http.StatusOK]
	if totalReq != httpOk {
		t.Errorf("Mismatch between requests %d and ok %v", totalReq, res.RetCodes)
	}
}

func TestHTTPRunnerBadServer(t *testing.T) {
	SetLogLevel(Info)
	// Using http to an https server (or the current 'close all' dummy https server)
	// should fail:
	port := DynamicHTTPServer(true)
	baseURL := fmt.Sprintf("http://localhost:%d/", port)

	opts := HTTPRunnerOptions{
		RunnerOptions: RunnerOptions{
			QPS: 10,
		},
		URL: baseURL,
	}
	_, err := RunHTTPTest(&opts)
	if err == nil {
		t.Fatal("Expecting an error but didn't get it when connecting to bad server")
	}
	Infof("Got expected error from mismatch/bad server: %v", err)
}
