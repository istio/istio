//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package cni

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/integration/security/util/reachability"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(nil, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
components:
  cni:
     enabled: true
     namespace: kube-system
`
		})).
		Run()
}

// This test verifies reachability under different authN scenario:
// - app A to app B using mTLS.
// In each test, the steps are:
// - Configure authn policy.
// - Wait for config propagation.
// - Send HTTP/gRPC requests between apps.
func TestCNIReachability(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			kenv := ctx.Environment().(*kube.Environment)
			cluster := kenv.KubeClusters[0]
			_, err := kube2.WaitUntilPodsAreReady(kube2.NewSinglePodFetch(cluster, "kube-system", "k8s-app=istio-cni-node"))
			if err != nil {
				ctx.Fatal(err)
			}
			rctx := reachability.CreateContext(ctx, pilot.NewOrFail(t, ctx, pilot.Config{}), false)
			systemNM := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)

			testCases := []reachability.TestCase{
				{
					ConfigFile: "global-mtls-on.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude headless naked service, because it is no sidecar
						if src == rctx.HeadlessNaked || opts.Target == rctx.HeadlessNaked {
							return false
						}
						// Exclude calls to the headless TCP port.
						if opts.Target == rctx.Headless && opts.PortName == "tcp" {
							return false
						}
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
			}
			rctx.Run(testCases)
		})
}
