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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/security/util/reachability"
)

// This test verifies reachability under different authN scenario:
// - app A to app B using mTLS.
// - app A to app B using mTLS-permissive.
// - app A to app B without using mTLS.
// In each test, the steps are:
// - Configure authn policy.
// - Wait for config propagation.
// - Send HTTP/gRPC requests between apps.
func TestReachability(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {

			rctx := reachability.CreateContext(ctx, p)
			systemNM := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)

			testCases := []reachability.TestCase{
				{
					ConfigFile: "beta-mtls-on.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						if src == rctx.Naked && opts.Target == rctx.Naked {
							// naked->naked should always succeed.
							return true
						}

						// If one of the two endpoints is naked, expect failure.
						return src != rctx.Naked && opts.Target != rctx.Naked
					},
				},
				{
					ConfigFile: "beta-mtls-permissive.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the naked app.
						return opts.Target != rctx.Naked
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					ConfigFile: "beta-mtls-off.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					ConfigFile: "beta-per-port-mtls.yaml",
					Namespace:  rctx.Namespace,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Include all tests that target app B, which has the single-port config.
						return opts.Target == rctx.B
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return opts.PortName != "http"
					},
				},
				{
					ConfigFile: "beta-mtls-automtls.yaml",
					Namespace:  rctx.Namespace,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// autoMtls doesn't work for client that doesn't have proxy, unless target doesn't
						// have proxy neither.
						if src == rctx.Naked {
							return opts.Target == rctx.Naked
						}
						return true
					},
				},
				{
					ConfigFile: "beta-mtls-partial-automtls.yaml",
					Namespace:  rctx.Namespace,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// autoMtls doesn't work for client that doesn't have proxy, unless target doesn't
						// have proxy or have mTLS disabled
						if src == rctx.Naked {
							return opts.Target == rctx.Naked || (opts.Target == rctx.B && opts.PortName != "http")

						}
						// PeerAuthentication disable mTLS for workload app:b, except http port. Thus, autoMTLS
						// will fail on all ports on b, except http port.
						return opts.Target != rctx.B || opts.PortName == "http"
					},
				},
				{
					ConfigFile: "global-plaintext.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the headless TCP port.
						if opts.Target == rctx.Headless && opts.PortName == "tcp" {
							return false
						}

						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// When mTLS is disabled, all traffic should work.
						return true
					},
				},
				// --------start of auto mtls partial test cases ---------------
				// The follow three consecutive test together ensures the auto mtls works as intended
				// for sidecar migration scenario.
				{
					ConfigFile: "automtls-partial-sidecar-dr-no-tls.yaml",
					Namespace:  rctx.Namespace,
					CallOpts: []echo.CallOptions{
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/vistio",
						},
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/vlegacy",
						},
					},
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// We only need one pair.
						return src == rctx.A && opts.Target == rctx.Multiversion
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					ConfigFile: "automtls-partial-sidecar-dr-disable.yaml",
					Namespace:  rctx.Namespace,
					CallOpts: []echo.CallOptions{
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/vistio",
						},
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/vlegacy",
						},
					},
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// We only need one pair.
						return src == rctx.A && opts.Target == rctx.Multiversion
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// Only the request to legacy one succeeds as we disable mtls explicitly.
						return opts.Path == "/vlegacy"
					},
				},
				{
					ConfigFile: "automtls-partial-sidecar-dr-mutual.yaml",
					Namespace:  rctx.Namespace,
					CallOpts: []echo.CallOptions{
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/vistio",
						},
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/vlegacy",
						},
					},
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// We only need one pair.
						return src == rctx.A && opts.Target == rctx.Multiversion
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// Only the request to vistio one succeeds as we enable mtls explicitly.
						return opts.Path == "/vistio"
					},
				},
				// ----- end of automtls partial test suites -----
			}
			rctx.Run(testCases)
		})
}
