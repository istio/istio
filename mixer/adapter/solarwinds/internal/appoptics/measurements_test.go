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

	test2 "istio.io/istio/mixer/pkg/adapter/test"
)

func TestCreate(t *testing.T) {
	t.Run("All good", func(t *testing.T) {
		env := test2.NewEnv(t)
		logger := env.Logger()
		logger.Infof("Starting %s - test run. . .\n", t.Name())
		defer logger.Infof("Finishing %s - test run. . .\n", t.Name())

		var count int64

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&count, 1)
		}))
		defer ts.Close()

		c := NewClient("some string", logger)
		c.baseURL, _ = url.Parse(ts.URL)

		resp, err := c.MeasurementsService().Create([]*Measurement{
			{}, {}, {},
		})
		if err != nil {
			t.Errorf("There was an error: %v", err)
		}
		if resp == nil || resp.StatusCode != http.StatusOK {
			t.Error("Unexpected response")
		}

		if count != 1 {
			t.Errorf("Received requests don't match expected.")
		}
	})
}
