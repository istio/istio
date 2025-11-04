//go:build integ

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
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
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
	Include func(from echo.Instance, opts echo.CallOptions) bool

	// Indicates whether the test should expect a successful response.
	ExpectSuccess func(from echo.Instance, opts echo.CallOptions) bool

	// Allows filtering the destinations we expect to reach (optional).
	ExpectDestinations func(from echo.Instance, to echo.Target) echo.Instances

	// Indicates whether the test should expect a MTLS response.
	ExpectMTLS func(from echo.Instance, opts echo.CallOptions) bool

	// Indicates whether a test should be run in the multicluster environment.
	// This is a temporary flag during the converting tests into multicluster supported.
	// TODO: Remove this flag when all tests support multicluster
	SkippedForMulticluster bool
}

var (
	A             echo.Instances
	B             echo.Instances
	C             echo.Instances
	D             echo.Instances
	E             echo.Instances
	Multiversion  echo.Instances
	VM            echo.Instances
	External      echo.Instances
	Naked         echo.Instances
	Headless      echo.Instances
	HeadlessNaked echo.Instances
)

const (
	ASvc             = "a"
	BSvc             = "b"
	CSvc             = "c"
	DSvc             = "d"
	ESvc             = "e"
	MultiversionSvc  = "multiversion"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	NakedSvc         = "naked"
	HeadlessNakedSvc = "headless-naked"
	ExternalSvc      = "external"
)

// Run runs the given reachability test cases with the context.
func Run(testCases []TestCase, t framework.TestContext) {
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
		testName := strings.TrimSuffix(c.ConfigFile, filepath.Ext(c.ConfigFile))
		t.NewSubTest(testName).Run(func(t framework.TestContext) {
			// Apply the policy.
			cfg := t.ConfigIstio().File(c.Namespace.Name(), filepath.Join("./testdata", c.ConfigFile))
			retry.UntilSuccessOrFail(t, func() error {
				t.Logf("[%s] [%v] Apply config %s", testName, time.Now(), c.ConfigFile)
				// TODO(https://github.com/istio/istio/issues/20460) We shouldn't need a retry loop
				return cfg.Apply(apply.Wait)
			})
			for _, clients := range []echo.Instances{A, B, Headless, Naked, HeadlessNaked} {
				for _, from := range clients {
					t.NewSubTest(fmt.Sprintf("%s in %s",
						from.Config().Service, from.Config().Cluster.StableName())).Run(func(t framework.TestContext) {
						destinationSets := []echo.Instances{
							A,
							B,
							// only hit same network headless services
							match.Network(from.Config().Cluster.NetworkName()).GetMatches(Headless),
							// only hit same cluster multiversion services
							match.Cluster(from.Config().Cluster).GetMatches(Multiversion),
							// only hit same cluster naked services
							match.Cluster(from.Config().Cluster).GetMatches(Naked),
							VM,
						}

						for _, to := range destinationSets {
							if c.ExpectDestinations != nil {
								to = c.ExpectDestinations(from, to)
							}
							toClusters := to.Clusters()
							if len(toClusters) == 0 {
								continue
							}
							// grabbing the 0th assumes all echos in destinations have the same service name
							if isNakedToVM(from, to) {
								// No need to waste time on these tests which will time out on connection instead of fail-fast
								continue
							}

							copts := &callOptions
							// If test case specified service call options, use that instead.
							if c.CallOpts != nil {
								copts = &c.CallOpts
							}
							for _, opts := range *copts {
								// Set the target on the call options.
								opts.To = to
								if len(toClusters) == 1 {
									opts.Count = 1
								}

								// TODO(https://github.com/istio/istio/issues/37629) go back to converge
								opts.Retry.Options = []retry.Option{retry.Converge(1)}
								// TODO(https://github.com/istio/istio/issues/37629) go back to 5s
								opts.Timeout = time.Second * 10

								expectSuccess := c.ExpectSuccess(from, opts)
								expectMTLS := c.ExpectMTLS(from, opts)
								var tpe string
								if expectSuccess {
									tpe = "positive"
									opts.Check = check.And(
										check.OK(),
										check.ReachedTargetClusters(t))
									if expectMTLS {
										opts.Check = check.And(opts.Check,
											check.MTLSForHTTP())
									}
								} else {
									tpe = "negative"
									opts.Check = check.NotOK()
								}
								include := c.Include
								if include == nil {
									include = func(_ echo.Instance, _ echo.CallOptions) bool { return true }
								}
								if include(from, opts) {
									subTestName := fmt.Sprintf("%s to %s:%s%s %s",
										opts.Scheme,
										to.Config().Service,
										opts.Port.Name,
										opts.HTTP.Path,
										tpe)

									t.NewSubTest(subTestName).
										Run(func(t framework.TestContext) {
											if (from.Config().IsNaked()) && len(toClusters) > 1 {
												// TODO use echotest to generate the cases that would work for multi-network + naked
												t.Skip("https://github.com/istio/istio/issues/37307")
											}

											from.CallOrFail(t, opts)
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
func isNakedToVM(from echo.Instance, to echo.Target) bool {
	return from.Config().IsNaked() && to.Config().IsVM()
}

func CreateCustomInstances(apps *deployment.SingleNamespaceView) error {
	for index, namespacedName := range apps.EchoNamespace.All.NamespacedNames() {
		switch {
		case namespacedName.Name == ASvc:
			A = apps.EchoNamespace.All[index]
		case namespacedName.Name == BSvc:
			B = apps.EchoNamespace.All[index]
		case namespacedName.Name == CSvc:
			C = apps.EchoNamespace.All[index]
		case namespacedName.Name == DSvc:
			D = apps.EchoNamespace.All[index]
		case namespacedName.Name == ESvc:
			E = apps.EchoNamespace.All[index]
		case namespacedName.Name == HeadlessSvc:
			Headless = apps.EchoNamespace.All[index]
		case namespacedName.Name == HeadlessNakedSvc:
			HeadlessNaked = apps.EchoNamespace.All[index]
		case namespacedName.Name == ExternalSvc:
			External = apps.EchoNamespace.All[index]
		case namespacedName.Name == NakedSvc:
			Naked = apps.EchoNamespace.All[index]
		case namespacedName.Name == VMSvc:
			VM = apps.EchoNamespace.All[index]
		case namespacedName.Name == MultiversionSvc:
			Multiversion = apps.EchoNamespace.All[index]
		}
	}
	return nil
}
