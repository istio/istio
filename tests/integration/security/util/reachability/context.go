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

package reachability

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/scheck"
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

	// Indicates whether the test should expect a successful response.
	ExpectSuccess func(src echo.Instance, opts echo.CallOptions) bool

	// Allows filtering the destinations we expect to reach (optional).
	ExpectDestinations func(src echo.Instance, dest echo.Instances) echo.Instances

	// Indicates whether the test should expect a MTLS response.
	ExpectMTLS func(src echo.Instance, opts echo.CallOptions) bool

	// Indicates whether a test should be run in the multicluster environment.
	// This is a temporary flag during the converting tests into multicluster supported.
	// TODO: Remove this flag when all tests support multicluster
	SkippedForMulticluster bool
}

// Run runs the given reachability test cases with the context.
func Run(testCases []TestCase, t framework.TestContext, apps *util.EchoDeployments) {
	callOptions := []echo.CallOptions{
		{
			Port: echo.Port{
				Name: "http",
			},
			Scheme: scheme.HTTP,
		},
		{
			Port: echo.Port{
				Name: "http",
			},
			Scheme: scheme.WebSocket,
		},
		{
			Port: echo.Port{
				Name: "tcp",
			},
			Scheme: scheme.TCP,
		},
		{
			Port: echo.Port{
				Name: "grpc",
			},
			Scheme: scheme.GRPC,
		},
		{
			Port: echo.Port{
				Name: "https",
			},
			Scheme: scheme.HTTPS,
		},
	}

	for _, c := range testCases {
		// Create a copy to avoid races, as tests are run in parallel
		c := c
		if c.SkippedForMulticluster && t.Clusters().IsMulticluster() {
			continue
		}
		testName := strings.TrimSuffix(c.ConfigFile, filepath.Ext(c.ConfigFile))
		t.NewSubTest(testName).Run(func(t framework.TestContext) {
			// Apply the policy.
			cfg := t.ConfigIstio().File(filepath.Join("./testdata", c.ConfigFile))
			retry.UntilSuccessOrFail(t, func() error {
				t.Logf("[%s] [%v] Apply config %s", testName, time.Now(), c.ConfigFile)
				// TODO(https://github.com/istio/istio/issues/20460) We shouldn't need a retry loop
				return cfg.Apply(c.Namespace.Name(), resource.Wait)
			})
			for _, clients := range []echo.Instances{apps.A, apps.B.Match(echo.Namespace(apps.Namespace1.Name())), apps.Headless, apps.Naked, apps.HeadlessNaked} {
				for _, client := range clients {
					client := client
					t.NewSubTest(fmt.Sprintf("%s in %s",
						client.Config().Service, client.Config().Cluster.StableName())).Run(func(t framework.TestContext) {
						destinationSets := []echo.Instances{
							apps.A,
							apps.B,
							// only hit same network headless services
							apps.Headless.Match(echo.InNetwork(client.Config().Cluster.NetworkName())),
							// only hit same cluster multiversion services
							apps.Multiversion.Match(echo.InCluster(client.Config().Cluster)),
							// only hit same cluster naked services
							apps.Naked.Match(echo.InCluster(client.Config().Cluster)),
							apps.VM,
						}

						for _, destinations := range destinationSets {
							destinations := destinations
							if c.ExpectDestinations != nil {
								destinations = c.ExpectDestinations(client, destinations)
							}
							destClusters := destinations.Clusters()
							if len(destClusters) == 0 {
								continue
							}
							// grabbing the 0th assumes all echos in destinations have the same service name
							destination := destinations[0]
							if isNakedToVM(apps, client, destination) {
								// No need to waste time on these tests which will time out on connection instead of fail-fast
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

								// Set the target on the call options.
								opts.To = dest
								opts.Count = callCount

								// TODO(https://github.com/istio/istio/issues/37629) go back to converge
								opts.Retry.Options = []retry.Option{retry.Converge(1)}

								expectSuccess := c.ExpectSuccess(src, opts)
								expectMTLS := c.ExpectMTLS(src, opts)
								var tpe string
								if expectSuccess {
									tpe = "positive"
									opts.Check = check.And(
										check.OK(),
										scheck.ReachedClusters(destinations, &opts))
									if expectMTLS {
										opts.Check = check.And(opts.Check,
											check.MTLSForHTTP())
									}
								} else {
									tpe = "negative"
									opts.Check = scheck.NotOK()
								}

								include := c.Include
								if include == nil {
									include = func(_ echo.Instance, _ echo.CallOptions) bool { return true }
								}
								if include(src, opts) {
									subTestName := fmt.Sprintf("%s to %s:%s%s %s",
										opts.Scheme,
										dest.Config().Service,
										opts.Port.Name,
										opts.HTTP.Path,
										tpe)

									t.NewSubTest(subTestName).
										RunParallel(func(t framework.TestContext) {
											// TODO: fix Multiversion related test in multicluster
											if t.Clusters().IsMulticluster() && apps.Multiversion.Contains(destination) {
												t.Skip("https://github.com/istio/istio/issues/37307")
											}
											if (apps.IsNaked(client)) && len(destClusters) > 1 {
												// TODO use echotest to generate the cases that would work for multi-network + naked
												t.Skip("https://github.com/istio/istio/issues/37307")
											}

											src.CallOrFail(t, opts)
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

// Exclude calls from naked->VM since naked has no Envoy
// However, no endpoint exists for VM in k8s, so calls from naked->VM will fail, regardless of mTLS
func isNakedToVM(apps *util.EchoDeployments, src, dst echo.Instance) bool {
	return apps.IsNaked(src) && apps.IsVM(dst)
}
