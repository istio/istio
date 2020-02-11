// Copyright 2019 Istio Authors
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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment"
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

			rctx := reachability.CreateContext(ctx, g, p)
			systemNM := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)

			testCases := []reachability.TestCase{
				{
					ConfigFile:          "global-mtls-on.yaml",
					Namespace:           systemNM,
					RequiredEnvironment: environment.Kube,
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
					ConfigFile:          "global-mtls-permissive.yaml",
					Namespace:           systemNM,
					RequiredEnvironment: environment.Kube,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the naked app.
						return opts.Target != rctx.Naked
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					ConfigFile: "global-mtls-off.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					ConfigFile:          "single-port-mtls-on.yaml",
					Namespace:           rctx.Namespace,
					RequiredEnvironment: environment.Kube,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Include all tests that target app B, which has the single-port config.
						return opts.Target == rctx.B
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return opts.PortName != "http"
					},
				},
				{
					ConfigFile:          "beta-mtls-on.yaml",
					Namespace:           systemNM,
					RequiredEnvironment: environment.Kube,
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
					ConfigFile:          "beta-mtls-permissive.yaml",
					Namespace:           systemNM,
					RequiredEnvironment: environment.Kube,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the naked app.
						return opts.Target != rctx.Naked
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					ConfigFile:          "beta-mtls-off.yaml",
					Namespace:           systemNM,
					RequiredEnvironment: environment.Kube,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					ConfigFile:          "beta-per-port-mtls.yaml",
					Namespace:           rctx.Namespace,
					RequiredEnvironment: environment.Kube,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Include all tests that target app B, which has the single-port config.
						return opts.Target == rctx.B
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return opts.PortName != "http"
					},
				},
				{
					ConfigFile:          "mix-mtls-api.yaml",
					Namespace:           systemNM,
					RequiredEnvironment: environment.Kube,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
			}
			rctx.Run(testCases)
		})
}
