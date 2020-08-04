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

package renewststoken

import (
	"testing"
	"time"

	"github.com/onsi/gomega"

	testID "istio.io/istio/pkg/test/env"
	xdsService "istio.io/istio/security/pkg/stsservice/mock"
	stsTest "istio.io/istio/security/pkg/stsservice/test"
)

// TestRenewToken verifies when proxy reconnect XDS server and sends token over
// the new stream, if the original token is expired, gRPC library will call
// STS server and returns new token to proxy.
func TestRenewToken(t *testing.T) {
	// Sets up callback that verifies token on new XDS stream.
	cb := xdsService.CreateXdsCallback(t)
	tokenLifeTimeInSec := 2
	numCloseStream := 2
	// Let the XDS streams last longer than token lifetime, so every new
	// stream should present a new token.
	cb.SetNumberOfStreamClose(numCloseStream, tokenLifeTimeInSec+1)
	// Start all test servers and proxy
	setup := stsTest.SetupTest(t, cb, testID.STSRenewTest, false)
	// Explicitly set token life time to a short duration.
	setup.AuthServer.SetTokenLifeTime(tokenLifeTimeInSec)
	// Explicitly set auth server to return different access token to each call.
	setup.AuthServer.EnableDynamicAccessToken(true)
	// Verify that initially XDS stream is not set up, stats are not incremented.
	g := gomega.NewWithT(t)
	g.Expect(cb.NumStream()).To(gomega.Equal(0))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(0))
	// Get initial number of calls to auth server. They are not zero due to STS flow test
	// in the test setup, to make sure the servers are up and ready to serve.
	initialNumFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	initialNumAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()
	setup.StartProxy(t)
	setup.ProxySetup.WaitEnvoyReady()
	// Verify that proxy re-connects XDS server after each stream close, and a
	// different token is received.
	gomega.SetDefaultEventuallyTimeout(10 * time.Second)
	g.Eventually(func() int { return cb.NumStream() }).Should(gomega.Equal(numCloseStream + 1)) // nolint:gocritic
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(numCloseStream + 1))
	// Verify every time proxy reconnects to XDS server, gRPC STS fetches a new token.
	g.Expect(setup.AuthServer.NumGetFederatedTokenCalls()).To(gomega.Equal(initialNumFederatedTokenCall + numCloseStream + 1))
	g.Expect(setup.AuthServer.NumGetAccessTokenCalls()).To(gomega.Equal(initialNumAccessTokenCall + numCloseStream + 1))
	setup.TearDown()
}
