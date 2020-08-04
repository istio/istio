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

package failureststokenfetch

import (
	"errors"
	"testing"

	"github.com/onsi/gomega"

	testID "istio.io/istio/pkg/test/env"
	xdsService "istio.io/istio/security/pkg/stsservice/mock"
	stsTest "istio.io/istio/security/pkg/stsservice/test"
)

// TestTokenFetchFailureOne verifies when auth backend fails to generate
// federated token, Envoy fails to start.
func TestTokenFetchFailureOne(t *testing.T) {
	cb := xdsService.CreateXdsCallback(t)
	// Start all test servers
	setup := stsTest.SetupTest(t, cb, testID.STSFailureTest, false)

	// Get initial number of calls to auth server. They are not zero due to STS flow test
	// in the test setup, to make sure the servers are up and ready to serve.
	initialNumFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	initialNumAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()

	// Force the auth backend to return error response to federated token fetch request.
	setup.AuthServer.SetGenFedTokenError(errors.New("fake token generation error"))

	// Verify that auth backend gets token exchange calls, and envoy fails to start.
	g := gomega.NewWithT(t)
	g.Expect(setup.ProxySetup.SetUp()).To(gomega.HaveOccurred())
	numFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	numAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()
	g.Expect(numFederatedTokenCall).Should(gomega.BeNumerically(">", initialNumFederatedTokenCall))
	g.Expect(numAccessTokenCall).To(gomega.Equal(initialNumAccessTokenCall))
	g.Expect(cb.NumStream()).To(gomega.Equal(0))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(0))
	setup.ProxySetup.SilentlyStopProxy(true)
	setup.TearDown()
}

// TestTokenFetchFailureTwo verifies when auth backend fails to generate
// access token, Envoy fails to start.
func TestTokenFetchFailureTwo(t *testing.T) {
	cb := xdsService.CreateXdsCallback(t)
	// Start all test servers
	setup := stsTest.SetupTest(t, cb, testID.STSFailureTest, false)

	// Get initial number of calls to auth server. They are not zero due to STS flow test
	// in the test setup, to make sure the servers are up and ready to serve.
	initialNumFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	initialNumAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()

	// Force the auth backend to return error response to access token fetch request.
	setup.AuthServer.SetGenAcsTokenError(errors.New("fake token generation error"))

	// Verify that auth backend gets token exchange calls, and envoy fails to start.
	g := gomega.NewWithT(t)
	g.Expect(setup.ProxySetup.SetUp()).To(gomega.HaveOccurred())
	numFederatedTokenCall := setup.AuthServer.NumGetFederatedTokenCalls()
	numAccessTokenCall := setup.AuthServer.NumGetAccessTokenCalls()
	g.Expect(numFederatedTokenCall).Should(gomega.BeNumerically(">", initialNumFederatedTokenCall))
	g.Expect(numAccessTokenCall).Should(gomega.BeNumerically(">", initialNumAccessTokenCall))
	g.Expect(cb.NumStream()).To(gomega.Equal(0))
	g.Expect(cb.NumTokenReceived()).To(gomega.Equal(0))
	setup.ProxySetup.SilentlyStopProxy(true)
	setup.TearDown()
}
