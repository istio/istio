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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
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
		Features("security.reachability").
		Run(func(ctx framework.TestContext) {
			systemNM := istio.ClaimSystemNamespaceOrFail(ctx, ctx)
			// mtlsOnExpect defines our expectations for when mTLS is expected when its enabled
			mtlsOnExpect := func(src echo.Instance, opts echo.CallOptions) bool {
				if apps.IsNaked(src) || apps.IsNaked(opts.Target) {
					// If one of the two endpoints is naked, we don't send mTLS
					return false
				}
				if apps.IsHeadless(opts.Target) && opts.Target == src {
					// pod calling its own pod IP will not be intercepted
					return false
				}
				return true
			}
			Always := func(src echo.Instance, opts echo.CallOptions) bool {
				return true
			}
			Never := func(src echo.Instance, opts echo.CallOptions) bool {
				return false
			}
			testCases := []reachability.TestCase{
				{
					ConfigFile: "beta-mtls-on.yaml",
					Namespace:  systemNM,
					Include:    Always,
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						if apps.IsNaked(src) && apps.IsNaked(opts.Target) {
							// naked->naked should always succeed.
							return true
						}
						// If one of the two endpoints is naked, expect failure.
						return !apps.IsNaked(src) && !apps.IsNaked(opts.Target)
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "beta-mtls-permissive.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to naked since we are applying ISTIO_MUTUAL
						return !apps.IsNaked(opts.Target)
					},
					ExpectSuccess: Always,
					ExpectMTLS:    mtlsOnExpect,
				},
				{
					ConfigFile:             "beta-mtls-off.yaml",
					Namespace:              systemNM,
					Include:                Always,
					ExpectSuccess:          Always,
					ExpectMTLS:             Never,
					SkippedForMulticluster: true,
				},
				{
					ConfigFile:             "plaintext-to-permissive.yaml",
					Namespace:              systemNM,
					Include:                Always,
					ExpectSuccess:          Always,
					ExpectMTLS:             Never,
					SkippedForMulticluster: true,
				},
				{
					ConfigFile: "beta-per-port-mtls.yaml",
					Namespace:  apps.Namespace1,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Include all tests that target app B, which has the single-port config.
						return apps.B.Contains(opts.Target)
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return opts.PortName != "http"
					},
					ExpectMTLS:             Never,
					SkippedForMulticluster: true,
				},
				{
					ConfigFile: "beta-mtls-automtls.yaml",
					Namespace:  apps.Namespace1,
					Include:    Always,
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// autoMtls doesn't work for client that doesn't have proxy, unless target doesn't
						// have proxy neither.
						if apps.IsNaked(src) {
							return apps.IsNaked(opts.Target)
						}
						return true
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "beta-mtls-partial-automtls.yaml",
					Namespace:  apps.Namespace1,
					Include:    Always,
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// autoMtls doesn't work for client that doesn't have proxy, unless target doesn't
						// have proxy or have mTLS disabled
						if apps.IsNaked(src) {
							return apps.IsNaked(opts.Target) || (apps.B.Contains(opts.Target) && opts.PortName != "http")
						}
						// PeerAuthentication disable mTLS for workload app:b, except http port. Thus, autoMTLS
						// will fail on all ports on b, except http port.
						return !apps.B.Contains(opts.Target) || opts.PortName == "http"
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "no-peer-authn.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to naked since we are applying ISTIO_MUTUAL
						return !apps.IsNaked(opts.Target)
					},
					ExpectSuccess: Always, // No PeerAuthN should default to a PERMISSIVE.
					ExpectMTLS:    mtlsOnExpect,
				},
				{
					ConfigFile:             "global-plaintext.yaml",
					Namespace:              systemNM,
					Include:                Always,
					ExpectSuccess:          Always,
					ExpectMTLS:             Never,
					SkippedForMulticluster: true,
				},
				{
					ConfigFile: "automtls-passthrough.yaml",
					Namespace:  systemNM,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// VM passthrough doesn't work. We will send traffic to the ClusterIP of
						// the VM service, which will have 0 Endpoints. If we generated
						// EndpointSlice's for VMs this might work.
						return !apps.IsVM(opts.Target)
					},
					ExpectSuccess: Always,
					ExpectMTLS: func(src echo.Instance, opts echo.CallOptions) bool {
						if opts.Target.Config().Service == apps.Multiversion[0].Config().Service {
							// For mixed targets, we downgrade to plaintext.
							// TODO(https://github.com/istio/istio/issues/27376) enable mixed deployments
							return false
						}
						return mtlsOnExpect(src, opts)
					},
					// Since we are doing passthrough, only single cluster is relevant here, as we
					// are bypassing any Istio cluster load balancing
					SkippedForMulticluster: true,
				},
				// --------start of auto mtls partial test cases ---------------
				// The follow three consecutive test together ensures the auto mtls works as intended
				// for sidecar migration scenario.
				{
					ConfigFile: "automtls-partial-sidecar-dr-no-tls.yaml",
					Namespace:  apps.Namespace1,
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
						return apps.A.Contains(src) && apps.Multiversion.Contains(opts.Target)
					},
					ExpectSuccess: Always,
					ExpectMTLS: func(src echo.Instance, opts echo.CallOptions) bool {
						return opts.Path == "/vistio"
					},
				},
				{
					ConfigFile: "automtls-partial-sidecar-dr-disable.yaml",
					Namespace:  apps.Namespace1,
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
						return apps.A.Contains(src) && apps.Multiversion.Contains(opts.Target)
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// Only the request to legacy one succeeds as we disable mtls explicitly.
						return opts.Path == "/vlegacy"
					},
					ExpectMTLS: Never,
				},
				{
					ConfigFile: "automtls-partial-sidecar-dr-mutual.yaml",
					Namespace:  apps.Namespace1,
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
						return apps.A.Contains(src) && apps.Multiversion.Contains(opts.Target)
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						// Only the request to vistio one succeeds as we enable mtls explicitly.
						return opts.Path == "/vistio"
					},
					ExpectMTLS: func(src echo.Instance, opts echo.CallOptions) bool {
						return opts.Path == "/vistio"
					},
				},
				// ----- end of automtls partial test suites -----
			}
			reachability.Run(testCases, ctx, apps)
		})
}
