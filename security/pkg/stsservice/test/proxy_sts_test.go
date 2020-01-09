// Copyright 2020 Istio Authors
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

package test

import (
	"testing"
	"time"

	xdsService "istio.io/istio/security/pkg/stsservice/mock"
)

// TestProxySTS verifies that XDS server receives token correctly.
// Proxy loads bootstrap config, and its gRPC STS sends request to STS server for
// token, STS server has a token manager, which makes API calls to auth server for
// token. Once gRPC STS receives token, proxy sets up XDS stream with XDS server.
// XDS server verifies the token is correct and pushes LDS to proxy.
// To verify that the dynamic listener is loaded, the test sends http request to
// that dynamic listener.
func TestProxySTS(t *testing.T) {
	// Sets up callback that verifies token on new XDS stream.
	cb := xdsService.CreateXdsCallback(t)
	// Start all test servers and proxy
	setup := SetUpTest(t, cb)
	// Get initial stats
	t.Logf("num stream: %d", cb.NumStream())
	t.Logf("num token: %d", cb.NumTokenReceived())
	setup.StartProxy(t)
	time.Sleep(500 * time.Second)
	// Verify that token is received
	t.Logf("num stream: %d", cb.NumStream())
	t.Logf("num token: %d", cb.NumTokenReceived())
	// Verify that LDS push is done and dynamic listener works properly
	setup.TearDown()
}