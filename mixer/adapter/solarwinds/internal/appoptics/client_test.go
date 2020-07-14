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

package appoptics

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func TestNewRequest(t *testing.T) {

	t.Run("All good", func(t *testing.T) {
		env := test.NewEnv(t)
		logger := env.Logger()
		logger.Infof("Starting %s - test run. . .\n", t.Name())
		defer logger.Infof("Finishing %s - test run. . .\n", t.Name())

		c := NewClient("some string", logger)
		req, err := c.NewRequest("POST", "measurements", &Measurement{})
		if req == nil || err != nil {
			t.Errorf("There was an error creating request")
		}
	})

	t.Run("Request error", func(t *testing.T) {
		env := test.NewEnv(t)
		logger := env.Logger()
		logger.Infof("Starting %s - test run. . .\n", t.Name())
		defer logger.Infof("Finishing %s - test run. . .\n", t.Name())

		c := NewClient("some string", logger)
		req, err := c.NewRequest("POST", "measurements", make(chan int))
		if req != nil || err == nil {
			t.Errorf("There should have been an error creating request")
		}
	})
}

func TestDoAllGood(t *testing.T) {
	env := test.NewEnv(t)
	logger := env.Logger()
	logger.Infof("Starting %s - test run. . .\n", t.Name())
	defer logger.Infof("Finishing %s - test run. . .\n", t.Name())

	var count int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
	}))
	defer ts.Close()

	c := NewClient("some string", logger)
	c.baseURL, _ = url.Parse(ts.URL)
	req, err := c.NewRequest("POST", "measurements", &Measurement{})
	if req == nil || err != nil {
		t.Errorf("There was an error creating request")
	}
	resp, err := c.Do(req, nil)
	if err != nil {
		t.Errorf("There was an error: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Error("Unexpected response")
	}

	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("Received requests don't match expected.")
	}
}

func TestDoRemoteError(t *testing.T) {
	env := test.NewEnv(t)
	logger := env.Logger()
	logger.Infof("Starting %s - test run. . .\n", t.Name())
	defer logger.Infof("Finishing %s - test run. . .\n", t.Name())

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "I don't like your message.", http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := NewClient("some string", logger)
	c.baseURL, _ = url.Parse(ts.URL)
	req, err := c.NewRequest("POST", "measurements", &Measurement{})
	if req == nil || err != nil {
		t.Errorf("There was an error creating request")
	}
	resp, err := c.Do(req, nil)
	if err == nil {
		t.Errorf("An error is expected")
	}
	if resp != nil && resp.StatusCode == http.StatusOK {
		t.Error("Unexpected response")
	}
}
