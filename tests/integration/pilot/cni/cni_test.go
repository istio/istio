// +build integ
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
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/reachability"
)

var (
	ist  istio.Instance
	apps = &util.EchoDeployments{}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&ist, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
components:
  cni:
     enabled: true
     namespace: kube-system
`
		})).
		Setup(func(t resource.Context) error {
			return util.SetupApps(t, ist, apps, false)
		}).
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
		Run(func(t framework.TestContext) {
			cluster := t.Clusters().Default()
			_, err := kube2.WaitUntilPodsAreReady(kube2.NewSinglePodFetch(cluster, "kube-system", "k8s-app=istio-cni-node"))
			if err != nil {
				t.Fatal(err)
			}
			systemNM := istio.ClaimSystemNamespaceOrFail(t, t)
			testCases := []reachability.TestCase{
				{
					ConfigFile: "global-mtls-on.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude headless naked service, because it is no sidecar
						if apps.HeadlessNaked.Contains(src) || apps.HeadlessNaked.Contains(opts.Target) {
							return false
						}
						// Exclude calls to the headless TCP port.
						if apps.Headless.Contains(opts.Target) && opts.PortName == "tcp" {
							return false
						}
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						if apps.Naked.Contains(src) && apps.Naked.Contains(opts.Target) {
							// naked->naked should always succeed.
							return true
						}

						// If one of the two endpoints is naked, expect failure.
						return !apps.Naked.Contains(src) && !apps.Naked.Contains(opts.Target)
					},
					ExpectMTLS: func(src echo.Instance, opts echo.CallOptions) bool {
						if apps.IsNaked(src) || apps.IsNaked(opts.Target) {
							// If one of the two endpoints is naked, we don't send mTLS
							return false
						}
						if apps.IsHeadless(opts.Target) && opts.Target == src {
							// pod calling its own pod IP will not be intercepted
							return false
						}
						return true
					},
				},
			}
			reachability.Run(testCases, t, apps)
		})
}
