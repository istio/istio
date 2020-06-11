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

package servercachedshortlivedststoken

import (
	"testing"

	"github.com/onsi/gomega"

	testID "istio.io/istio/pkg/test/env"
	xdsService "istio.io/istio/security/pkg/stsservice/mock"
	stsTest "istio.io/istio/security/pkg/stsservice/test"
)

// TestServerShortLivedCachedToken verifies when proxy restarts and reconnects XDS server,
// proxy calls STS server to fetch token. If the original token is going to expire,
// STS server fetches a new token for the proxy.
func TestServerShortLivedCachedToken(t *testing.T) {
	// Sets up callback that verifies token on new XDS stream.
	cb := xdsService.CreateXdsCallback(t)
	// Start all test servers and proxy
	setup := stsTest.SetupTest(t, cb, testID.STSShortLivedCacheTest, true)
	// Explicitly set token life time to a short duration, which is below the grace
	// period (5 minutes) of using cached token. Cached token is not in use.
	setup.ClearTokenCache()
	setup.AuthServer.SetTokenLifeTime(300)
	// Explicitly set auth server to return different access token to each call.
	setup.AuthServer.EnableDynamicAccessToken(true)
	// Verify that initially XDS stream is not set up, stats are not incremented.
	g := gomega.NewWithT(t)
	g.Expect(cb.NumStream()).To(gomega.Equal(0))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(0))
	// Get initial number of calls to auth server. There is a warm-up phase where
	// STS request is sent by HTTP client to make sure components are up and running.
	// By doing that the token is cached at the STS server.
	initialNumFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	initialNumAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()
	// Starting proxy will send a STS request to the STS server. Because cached
	// token is deleted, the STS server fetches a new token.
	setup.StartProxy(t)
	setup.ProxySetup.WaitEnvoyReady()
	setup.ProxySetup.ReStartEnvoy()
	// Restarting proxy will send another STS request to the STS server. Because
	// cached token is within grace period, the STS server fetches a new token.
	g.Expect(cb.NumStream()).To(gomega.Equal(2))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(2))
	g.Expect(setup.AuthServer.NumGetFederatedTokenCalls()).To(gomega.Equal(initialNumFederatedTokenCall + 2))
	g.Expect(setup.AuthServer.NumGetAccessTokenCalls()).To(gomega.Equal(initialNumAccessTokenCall + 2))
	setup.TearDown()
}
