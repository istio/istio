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
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/security/util/reachability"
)

// This test verifies reachability under different mTLS configuration.
func TestReachability(t *testing.T) {
	framework.NewTest(t).
		// Multiversion app is unable to created locally, where we need two deployments with different labels.
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			rctx := reachability.CreateContext(ctx, g, p)
			systemNM := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)
			fmt.Println("incfly debug printing sysnamespace ", systemNM, scheme.HTTP)

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
					// Multiversion app v1 enbles mTLS using workload selector policy, v2 is PERMISSIVE.
					// We use VirtualService and DestinationRule to ensure request hit the v1 and v2 subset.
					// TODO: add DestinationRule specified case which does not work all the time.
					ConfigFile:          "beta-mtls-workload-automtls.yaml",
					Namespace:           rctx.Namespace,
					RequiredEnvironment: environment.Kube,
					CallOpts: []echo.CallOptions{
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/v1",
						},
						{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/v2-default",
						},
					},
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Focused on multiversion app. We exclude naked app since routing on naked app client
						// can't be enforced.
						return src == rctx.A && opts.Target == rctx.MultiVersion
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
			}
			rctx.Run(testCases)
		})
}
