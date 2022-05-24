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

package install

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/pkg/test/util/assert"
)

func TestServer(t *testing.T) {
	router := http.NewServeMux()
	isReady := initRouter(router)

	assert.Equal(t, isReady.Load(), false)

	server := httptest.NewServer(router)
	defer server.Close()

	makeReq(t, server.URL, constants.LivenessEndpoint, http.StatusOK)
	makeReq(t, server.URL, constants.ReadinessEndpoint, http.StatusServiceUnavailable)

	SetReady(isReady)
	assert.Equal(t, isReady.Load(), true)

	makeReq(t, server.URL, constants.LivenessEndpoint, http.StatusOK)
	makeReq(t, server.URL, constants.ReadinessEndpoint, http.StatusOK)

	SetNotReady(isReady)
	assert.Equal(t, isReady.Load(), false)

	makeReq(t, server.URL, constants.LivenessEndpoint, http.StatusOK)
	makeReq(t, server.URL, constants.ReadinessEndpoint, http.StatusServiceUnavailable)
}

func makeReq(t *testing.T, url, endpoint string, expectedStatusCode int) {
	t.Helper()
	res, err := http.Get(url + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != expectedStatusCode {
		t.Fatalf("expected status code from %s: %d, got: %d", endpoint, expectedStatusCode, res.StatusCode)
	}
}
