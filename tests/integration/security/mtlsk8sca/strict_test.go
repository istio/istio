//go:build integ
// +build integ

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

package mtlsk8sca

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/tests/integration/security/util/reachability"
)

// When k8s CA issues the Pilot certificate, this test verifies
// reachability under different authN scenario when automtls enabled
// - app A to app B using mTLS.
// In each test, the steps are:
// - Configure authn policy.
// - Wait for config propagation.
// - Send HTTP/gRPC requests between apps.
func TestMtlsStrictK8sCA(t *testing.T) {
	framework.NewTest(t).
		Features("security.control-plane.k8s-certs.k8sca").
		Run(func(t framework.TestContext) {
			systemNM := istio.ClaimSystemNamespaceOrFail(t, t)

			testCases := []reachability.TestCase{
				{
					ConfigFile: "global-mtls-on-no-dr.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the headless service.
						// Auto mtls does not apply to headless service, because for headless service
						// the cluster discovery type is ORIGINAL_DST, and it will not apply upstream tls setting
						return !opts.To.Config().IsHeadless()
					},
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						// When mTLS is in STRICT mode, DR's TLS settings are default to mTLS so the result would
						// be the same as having global DR rule.
						if opts.To.Config().IsNaked() {
							// calls to naked should always succeed.
							return true
						}

						// If source is naked, and destination is not, expect failure.
						return !(from.Config().IsNaked() && !opts.To.Config().IsNaked())
					},
					ExpectMTLS: func(from echo.Instance, opts echo.CallOptions) bool {
						if from.Config().IsNaked() || opts.To.Config().IsNaked() {
							// If one of the two endpoints is naked, we don't send mTLS
							return false
						}
						if opts.To.Config().IsHeadless() && opts.To == from {
							// pod calling its own pod IP will not be intercepted
							return false
						}
						return true
					},
				},
				{
					ConfigFile: "global-plaintext.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the headless TCP port.
						if apps.Headless.ContainsTarget(opts.To) && opts.Port.Name == "tcp" {
							return false
						}

						return true
					},
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						// When mTLS is disabled, all traffic should work.
						return true
					},
					ExpectDestinations: func(from echo.Instance, to echo.Target) echo.Instances {
						// Without TLS we can't perform SNI routing required for multi-network
						return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to.Instances())
					},
					ExpectMTLS: func(src echo.Instance, opts echo.CallOptions) bool {
						return false
					},
				},
			}
			reachability.Run(testCases, t)
		})
}
