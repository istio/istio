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

package successtest

import (
	"fmt"
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pkg/test/env"
	xdsService "istio.io/istio/security/pkg/stsservice/mock"
	stsTest "istio.io/istio/security/pkg/stsservice/test"
)

// TestProxySTS verifies that XDS server receives token correctly.
// Here is the flow being tested:
// 1. Proxy loads bootstrap config, and its gRPC STS client sends request to a STS
// server for token.
// 2. STS server has a token manager, which makes API calls to auth server for
// token. STS server returns token to gRPC STS client.
// 3. Once gRPC STS receives token, proxy sets up XDS stream with XDS server.
// 4. XDS server verifies the token is correct and pushes LDS to proxy.
// To verify that the dynamic listener is loaded, the test sends http request to
// that dynamic listener.
func TestProxySTS(t *testing.T) {
	expectedToken := "expected access token"
	// Sets up callback that verifies token on new XDS stream.
	cb := xdsService.CreateXdsCallback(t)
	cb.SetExpectedToken(expectedToken)
	// Start all test servers and proxy
	setup := stsTest.SetupTest(t, cb, env.STSTest, false)
	// Verify that initially XDS stream is not set up, stats do not update initial stats
	g := gomega.NewWithT(t)
	g.Expect(cb.NumStream()).To(gomega.Equal(0))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(0))
	setup.StartProxy(t)
	// Verify that token is received
	g.Expect(cb.NumStream()).To(gomega.Equal(1))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(1))
	// Verify that LDS push is done and dynamic listener works properly, this is
	// to make sure XDS stream is working properly
	setup.ProxySetup.WaitEnvoyReady()
	// Issues a GET echo request with 0 size body to the dynamic listener
	if _, _, err := env.HTTPGet(fmt.Sprintf("http://localhost:%d/echo", setup.ProxyListenerPort)); err != nil {
		t.Errorf("Failed in request: %v", err)
	}
	setup.TearDown()
}
