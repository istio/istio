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

package bootstrap_test

import (
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"
	"istio.io/istio/tests/util"
)

// TestListServices verifies that the mock services are available on the Pilot discovery service
func TestListServices(t *testing.T) {
	s := util.EnsureTestServer()
	// We are not interested in teardown - this test server will be used for all tests.
	helloService := mock.MakeService("hello.default.svc.cluster.local", "10.1.0.0")
	worldService := mock.MakeService("world.default.svc.cluster.local", "10.2.0.0")

	s.MemoryServiceDiscovery.AddService(helloService.Hostname, helloService)
	s.MemoryServiceDiscovery.AddService(worldService.Hostname, worldService)
	url := util.MockPilotURL + "/v1/registration"
	resp, err := http.Get(url)
	if err != nil {
		t.Error(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Response had unexpected status: %d", resp.StatusCode)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Failed reading response body")
	}

	bodyString := string(bodyBytes)

	// Verify that the hello and world mock services are available.
	if !strings.Contains(bodyString, "hello.default.svc.cluster.local") {
		t.Errorf("Response missing hello service")
	}
	if !strings.Contains(bodyString, "world.default.svc.cluster.local") {
		t.Errorf("Response missing world service")
	}
}
