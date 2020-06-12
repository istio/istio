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

package stsfetchtimeout

import (
	"testing"

	"github.com/onsi/gomega"

	testID "istio.io/istio/pkg/test/env"
	xdsService "istio.io/istio/security/pkg/stsservice/mock"
	stsTest "istio.io/istio/security/pkg/stsservice/test"
)

// TestTokenFetchTimeoutOne verifies when fetching federated token timeouts,
// Envoy fails to start.
func TestTokenFetchTimeoutOne(t *testing.T) {
	cb := xdsService.CreateXdsCallback(t)
	// Start all test servers
	setup := stsTest.SetupTest(t, cb, testID.STSTimeoutTest, false)

	// Get initial number of calls to auth server. They are not zero due to STS flow test
	// in the test setup, to make sure the servers are up and ready to serve.
	initialNumFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	initialNumAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()

	// Force the auth backend to hold the token request.
	setup.AuthServer.BlockFederatedTokenRequest(true)

	g := gomega.NewWithT(t)
	g.Expect(setup.ProxySetup.SetUp()).To(gomega.HaveOccurred())

	numFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	numAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()
	// Verify that there are retries if fetching federated token fetch timeouts.
	g.Expect(numFederatedTokenCall).Should(gomega.BeNumerically(">", initialNumFederatedTokenCall+1))
	// Access token fetch call does not happen if federated token is not available.
	g.Expect(numAccessTokenCall).To(gomega.Equal(initialNumAccessTokenCall))
	g.Expect(cb.NumStream()).To(gomega.Equal(0))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(0))
	setup.ProxySetup.SilentlyStopProxy(true)
	setup.TearDown()
}

// TestTokenFetchTimeoutTwo verifies when fetching access token timeouts,
// Envoy fails to start.
func TestTokenFetchTimeoutTwo(t *testing.T) {
	cb := xdsService.CreateXdsCallback(t)
	// Start all test servers
	setup := stsTest.SetupTest(t, cb, testID.STSTimeoutTest, false)

	// Get initial number of calls to auth server. They are not zero due to STS flow test
	// in the test setup, to make sure the servers are up and ready to serve.
	initialNumFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	initialNumAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()

	// Force the auth backend to hold the token request.
	setup.AuthServer.BlockAccessTokenRequest(true)

	g := gomega.NewWithT(t)
	g.Expect(setup.ProxySetup.SetUp()).To(gomega.HaveOccurred())

	numFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	numAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()
	// Verify that gRPC STS will retry when fetching token fails due to timeout.
	g.Expect(numFederatedTokenCall).Should(gomega.BeNumerically(">", initialNumFederatedTokenCall+1))
	// Verify that there are retries if fetching access token timeouts.
	g.Expect(numAccessTokenCall).To(gomega.BeNumerically(">", initialNumAccessTokenCall+1))
	g.Expect(cb.NumStream()).To(gomega.Equal(0))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(0))
	setup.ProxySetup.SilentlyStopProxy(true)
	setup.TearDown()
}
