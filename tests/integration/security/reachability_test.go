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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/match"
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
		Run(func(t framework.TestContext) {
			systemNM := istio.ClaimSystemNamespaceOrFail(t, t)
			// mtlsOnExpect defines our expectations for when mTLS is expected when its enabled
			mtlsOnExpect := func(from echo.Instance, opts echo.CallOptions) bool {
				if apps.IsNaked(from) || apps.IsNaked(opts.To) {
					// If one of the two endpoints is naked, we don't send mTLS
					return false
				}
				if apps.IsHeadless(opts.To) && opts.To.Instances().Contains(from) {
					// pod calling its own pod IP will not be intercepted
					return false
				}
				return true
			}
			Always := func(echo.Instance, echo.CallOptions) bool {
				return true
			}
			Never := func(echo.Instance, echo.CallOptions) bool {
				return false
			}
			SameNetwork := func(from echo.Instance, to echo.Target) echo.Instances {
				return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to.Instances())
			}
			testCases := []reachability.TestCase{
				{
					ConfigFile: "beta-mtls-on.yaml",
					Namespace:  systemNM,
					Include:    Always,
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						if apps.IsNaked(from) && apps.IsNaked(opts.To) {
							// naked->naked should always succeed.
							return true
						}
						// If one of the two endpoints is naked, expect failure.
						return !apps.IsNaked(from) && !apps.IsNaked(opts.To)
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "beta-mtls-permissive.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to naked since we are applying ISTIO_MUTUAL
						return !apps.IsNaked(opts.To)
					},
					ExpectSuccess: Always,
					ExpectMTLS:    mtlsOnExpect,
				},
				{
					ConfigFile:    "beta-mtls-off.yaml",
					Namespace:     systemNM,
					Include:       Always,
					ExpectSuccess: Always,
					ExpectMTLS:    Never,
					// Without TLS we can't perform SNI routing required for multi-network
					ExpectDestinations: SameNetwork,
				},
				{
					ConfigFile: "beta-mtls-automtls-workload.yaml",
					Namespace:  apps.Namespace1,
					Include: func(from echo.Instance, opts echo.CallOptions) bool {
						// including the workloads where testing is possible ; eg. mtls is disabled on workload B for port 8090(http),
						// hence not including it otherwise failure will occur in multi-cluster scenarios
						return (apps.B.Contains(from) || apps.IsNaked(from)) &&
							(apps.A.ContainsTarget(opts.To) || apps.B.ContainsTarget(opts.To)) && !(apps.B.ContainsTarget(opts.To) && opts.Port.Name == "http")
					},
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						// Sidecar injected client always succeed.
						if apps.B.Contains(from) {
							return true
						}
						// For naked app as client, only requests targeted to mTLS disabled endpoints succeed:
						// A are disabled by workload selector for entire service.
						// B port 8090 http port are disabled.
						return apps.A.ContainsTarget(opts.To) || (apps.B.ContainsTarget(opts.To) && opts.Port.Name == "http")
					},
					// Only when the source is B and the destination does not disable mTLS.
					ExpectMTLS: func(from echo.Instance, opts echo.CallOptions) bool {
						return apps.B.Contains(from) && opts.Port.Name != "http" && !apps.A.ContainsTarget(opts.To)
					},
					ExpectDestinations: func(from echo.Instance, to echo.Target) echo.Instances {
						if apps.A.ContainsTarget(to) {
							return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to.Instances())
						}
						return to.Instances()
					},
				},
				{
					ConfigFile:    "plaintext-to-permissive.yaml",
					Namespace:     systemNM,
					Include:       Always,
					ExpectSuccess: Always,
					ExpectMTLS:    Never,
					// Since we are only sending plaintext and Without TLS
					// we can't perform SNI routing required for multi-network
					ExpectDestinations: SameNetwork,
				},
				{
					ConfigFile: "beta-per-port-mtls.yaml",
					Namespace:  apps.Namespace1,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Include all tests that target app B, which has the single-port config.
						return apps.B.ContainsTarget(opts.To)
					},
					ExpectSuccess: func(_ echo.Instance, opts echo.CallOptions) bool {
						return opts.Port.Name != "http"
					},
					ExpectMTLS: Never,
					// Since we are only sending plaintext and Without TLS
					// we can't perform SNI routing required for multi-network
					ExpectDestinations: SameNetwork,
				},
				{
					ConfigFile: "beta-mtls-automtls.yaml",
					Namespace:  apps.Namespace1,
					Include:    Always,
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						// autoMtls doesn't work for client that doesn't have proxy, unless target doesn't
						// have proxy neither.
						if apps.IsNaked(from) {
							return apps.IsNaked(opts.To)
						}
						return true
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "no-peer-authn.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to naked since we are applying ISTIO_MUTUAL
						return !apps.IsNaked(opts.To)
					},
					ExpectSuccess: Always, // No PeerAuthN should default to a PERMISSIVE.
					ExpectMTLS:    mtlsOnExpect,
				},
				{
					ConfigFile: "global-plaintext.yaml",
					Namespace:  systemNM,
					ExpectDestinations: func(from echo.Instance, to echo.Target) echo.Instances {
						// Without TLS we can't perform SNI routing required for multi-network
						return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to.Instances())
					},
					ExpectSuccess: Always,
					ExpectMTLS:    Never,
				},
				{
					ConfigFile: "automtls-passthrough.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// VM passthrough doesn't work. We will send traffic to the ClusterIP of
						// the VM service, which will have 0 Endpoints. If we generated
						// EndpointSlice's for VMs this might work.
						return !apps.IsVM(opts.To)
					},
					ExpectSuccess: Always,
					ExpectMTLS: func(from echo.Instance, opts echo.CallOptions) bool {
						if opts.To.Config().Service == apps.Multiversion[0].Config().Service {
							// For mixed targets, we downgrade to plaintext.
							// TODO(https://github.com/istio/istio/issues/27376) enable mixed deployments
							return false
						}
						return mtlsOnExpect(from, opts)
					},

					ExpectDestinations: func(from echo.Instance, to echo.Target) echo.Instances {
						// Since we are doing passthrough, only single cluster is relevant here, as we
						// are bypassing any Istio cluster load balancing
						return match.Cluster(from.Config().Cluster).GetMatches(to.Instances())
					},
				},
				// --------start of auto mtls partial test cases ---------------
				// The follow three consecutive test together ensures the auto mtls works as intended
				// for sidecar migration scenario.
				{
					ConfigFile: "automtls-partial-sidecar-dr-no-tls.yaml",
					Namespace:  apps.Namespace1,
					CallOpts: []echo.CallOptions{
						{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/vistio",
							},
						},
						{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/vlegacy",
							},
						},
					},
					Include: func(from echo.Instance, opts echo.CallOptions) bool {
						// We only need one pair.
						return apps.A.Contains(from) && apps.Multiversion.ContainsTarget(opts.To)
					},
					ExpectSuccess: Always,
					ExpectMTLS: func(_ echo.Instance, opts echo.CallOptions) bool {
						return opts.HTTP.Path == "/vistio"
					},
				},
				{
					ConfigFile: "automtls-partial-sidecar-dr-disable.yaml",
					Namespace:  apps.Namespace1,
					CallOpts: []echo.CallOptions{
						{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/vistio",
							},
						},
						{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/vlegacy",
							},
						},
					},
					Include: func(from echo.Instance, opts echo.CallOptions) bool {
						// We only need one pair.
						return apps.A.Contains(from) && apps.Multiversion.ContainsTarget(opts.To)
					},
					ExpectSuccess: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Only the request to legacy one succeeds as we disable mtls explicitly.
						return opts.HTTP.Path == "/vlegacy"
					},
					ExpectMTLS: Never,
				},
				{
					ConfigFile: "automtls-partial-sidecar-dr-mutual.yaml",
					Namespace:  apps.Namespace1,
					CallOpts: []echo.CallOptions{
						{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/vistio",
							},
						},
						{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/vlegacy",
							},
						},
					},
					Include: func(from echo.Instance, opts echo.CallOptions) bool {
						// We only need one pair.
						return apps.A.Contains(from) && apps.Multiversion.ContainsTarget(opts.To)
					},
					ExpectSuccess: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Only the request to vistio one succeeds as we enable mtls explicitly.
						return opts.HTTP.Path == "/vistio"
					},
					ExpectMTLS: func(_ echo.Instance, opts echo.CallOptions) bool {
						return opts.HTTP.Path == "/vistio"
					},
				},
				// ----- end of automtls partial test suites -----
			}
			reachability.Run(testCases, t, apps)
		})
}
