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

package externalca

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/security/util/scheck"
)

// TestReachability verifies:
// (a) Different workloads after getting their certificates signed by the K8s CA are successfully able to communicate with each other
func TestReachability(t *testing.T) {
	framework.NewTest(t).
		Features("security.externalca.reachability").
		Run(func(t framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, t)
			namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)

			from := apps.A
			to := apps.B
			fromAndTo := from.Append(to)

			echotest.New(t, fromAndTo).
				WithDefaultFilters(1, 1).
				FromMatch(match.ServiceName(from.NamespacedName())).
				ToMatch(match.ServiceName(to.NamespacedName())).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					// Verify mTLS works between a and b
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: "http",
						},
					}
					opts.Check = check.And(check.OK(), scheck.ReachedClusters(t.AllClusters(), &opts))

					from.CallOrFail(t, opts)
				})
		})
}
