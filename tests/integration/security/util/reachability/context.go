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

package reachability

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
	"istio.io/pkg/log"
)

// TestCase represents reachability test cases.
type TestCase struct {
	// ConfigFile is the name of the yaml contains the authentication policy and destination rule CRs
	// that are needed for the test setup.
	// The file is expected in the tests/integration/security/reachability/testdata folder.
	ConfigFile string
	Namespace  namespace.Instance

	// CallOpts specified the call options for destination service. If not specified, use the default
	// framework provided ones.
	CallOpts []echo.CallOptions

	// Indicates whether a test should be created for the given configuration.
	Include func(src echo.Instance, opts echo.CallOptions) bool

	// Handler called when the given test is being run.
	OnRun func(ctx framework.TestContext, src echo.Instance, opts echo.CallOptions)

	// Indicates whether the test should expect a successful response.
	ExpectSuccess func(src echo.Instance, opts echo.CallOptions) bool

	// Indicates whether a test should be run in the multicluster environment.
	// This is a temporary flag during the converting tests into multicluster supported.
	// TODO: Remove this flag when all tests support multicluster
	SkippedForMulticluster bool
}

// Run runs the given reachability test cases with the context.
func Run(testCases []TestCase, ctx framework.TestContext, apps *util.EchoDeployments) {
	callOptions := []echo.CallOptions{
		{
			PortName: "http",
			Scheme:   scheme.HTTP,
		},
		{
			PortName: "http",
			Scheme:   scheme.WebSocket,
		},
		{
			PortName: "tcp",
			Scheme:   scheme.TCP,
		},
		{
			PortName: "grpc",
			Scheme:   scheme.GRPC,
		},
	}

	ik := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
	for _, c := range testCases {
		// Create a copy to avoid races, as tests are run in parallel
		c := c
		if c.SkippedForMulticluster && ctx.Clusters().IsMulticluster() {
			continue
		}
		testName := strings.TrimSuffix(c.ConfigFile, filepath.Ext(c.ConfigFile))
		test := ctx.NewSubTest(testName)
		test.Run(func(ctx framework.TestContext) {
			// Apply the policy.
			policyYAML := file.AsStringOrFail(ctx, filepath.Join("./testdata", c.ConfigFile))
			retry.UntilSuccessOrFail(ctx, func() error {
				ctx.Logf("[%s] [%v] Apply config %s", testName, time.Now(), c.ConfigFile)
				// TODO(https://github.com/istio/istio/issues/20460) We shouldn't need a retry loop
				return ctx.Config().ApplyYAML(c.Namespace.Name(), policyYAML)
			})
			ctx.Logf("[%s] [%v] Wait for config propagate to endpoints...", testName, time.Now())
			t0 := time.Now()
			if err := ik.WaitForConfigs(c.Namespace.Name(), policyYAML); err != nil {
				// Continue anyways, so we can assess the effectiveness of using `istioctl wait`
				ctx.Logf("warning: failed to wait for config: %v", err)
				// Get proxy status for additional debugging
				s, _, _ := ik.Invoke([]string{"ps"})
				ctx.Logf("proxy status: %v", s)
			}
			// TODO(https://github.com/istio/istio/issues/25945) introducing istioctl wait in favor of a 10s sleep lead to flakes
			// to work around this, we will temporarily make sure we are always sleeping at least 10s, even if istioctl wait is faster.
			// This allows us to debug istioctl wait, while still ensuring tests are stable
			sleep := time.Second*10 - time.Since(t0)
			ctx.Logf("[%s] [%v] Wait for additional %v config propagate to endpoints...", testName, time.Now(), sleep)
			time.Sleep(sleep)
			ctx.Logf("[%s] [%v] Finish waiting. Continue testing.", testName, time.Now())
			ctx.Cleanup(func() {
				if err := retry.UntilSuccess(func() error {
					return ctx.Config().DeleteYAML(c.Namespace.Name(), policyYAML)
				}); err != nil {
					log.Errorf("failed to delete configuration: %v", err)
				}
			})
			for _, clients := range []echo.Instances{apps.A, apps.B, apps.Headless, apps.Naked, apps.HeadlessNaked} {
				for _, client := range clients {
					ctx.NewSubTest(fmt.Sprintf("%s in %s",
						client.Config().Service, client.Config().Cluster.Name())).Run(func(ctx framework.TestContext) {
						destinationSets := []echo.Instances{
							apps.A,
							apps.B,
							// only hit same cluster headless services
							apps.Headless.Match(echo.InCluster(client.Config().Cluster)),
							// only hit same cluster multiversion services
							apps.Multiversion.Match(echo.InCluster(client.Config().Cluster)),
							// only hit same cluster naked services
							apps.Naked.Match(echo.InCluster(client.Config().Cluster)),
							// only hit same cluster vm services
							apps.VM.Match(echo.InCluster(client.Config().Cluster)),
							// only hit same cluster headless services
							apps.HeadlessNaked.Match(echo.InCluster(client.Config().Cluster)),
						}

						for _, destinations := range destinationSets {
							client := client
							destinations := destinations
							destClusters := destinations.Clusters()
							if len(destClusters) == 0 {
								continue
							}
							// grabbing the 0th assumes all echos in destinations have the same service name
							destination := destinations[0]
							// TODO: fix Multiversion related test in multicluster
							if ctx.Clusters().IsMulticluster() && apps.Multiversion.Contains(destination) {
								continue
							}
							if (apps.IsHeadless(client) || apps.IsHeadless(destination) || apps.IsNaked(client)) && len(destClusters) > 1 {
								// TODO(landow) fix DNS issues with multicluster/VMs/headless
								continue
							}
							callCount := 1
							if len(destClusters) > 1 {
								// so we can validate all clusters are hit
								callCount = util.CallsPerCluster * len(destClusters)
							}

							copts := &callOptions
							// If test case specified service call options, use that instead.
							if c.CallOpts != nil {
								copts = &c.CallOpts
							}
							for _, opts := range *copts {
								// Copy the loop variables so they won't change for the subtests.
								src := client
								dest := destination
								opts := opts
								onPreRun := c.OnRun

								// Set the target on the call options.
								opts.Target = dest
								opts.Count = callCount

								if c.Include(src, opts) {
									expectSuccess := c.ExpectSuccess(src, opts)

									subTestName := fmt.Sprintf("with scheme %s to %s:%s%s",
										opts.Scheme,
										dest.Config().Service,
										opts.PortName,
										opts.Path)

									ctx.NewSubTest(subTestName).
										RunParallel(func(ctx framework.TestContext) {
											if onPreRun != nil {
												onPreRun(ctx, src, opts)
											}

											checker := connection.Checker{
												From:          src,
												DestClusters:  destClusters,
												Options:       opts,
												ExpectSuccess: expectSuccess,
											}
											checker.CheckOrFail(ctx)
										})
								}
							}
						}
					})
				}
			}
		})
	}
}
