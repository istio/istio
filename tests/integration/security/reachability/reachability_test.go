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

package reachability

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/tests/integration/security/util/connection"
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
			systemNM := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)
			ns := namespace.NewOrFail(ctx, ctx, "reachability", true)

			var a, b, headless, naked echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, config("a", ns, false, nil)).
				With(&b, config("b", ns, false, nil)).
				With(&headless, config("headless", ns, true, nil)).
				With(&naked, config("naked", ns, false, echo.NewAnnotations().
					SetBool(echo.SidecarInject, false))).
				BuildOrFail(ctx)

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
					Scheme:   scheme.HTTP,
				},
				{
					PortName: "grpc",
					Scheme:   scheme.GRPC,
				},
			}

			testCases := []struct {
				configFile string
				namespace  namespace.Instance

				requiredEnvironment environment.Name

				// Indicates whether a test should be created for the given configuration.
				include func(src echo.Instance, opts echo.CallOptions) bool

				// Handler called when the given test is being run.
				onRun func(ctx framework.TestContext, src echo.Instance, opts echo.CallOptions)

				// Indicates whether the test should expect a successful response.
				expectSuccess func(src echo.Instance, opts echo.CallOptions) bool
			}{
				{
					configFile:          "global-mtls-on.yaml",
					namespace:           systemNM,
					requiredEnvironment: environment.Kube,
					include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the headless TCP port.
						if opts.Target == headless && opts.PortName == "tcp" {
							return false
						}

						// Exclude headless->headless
						return src != headless || opts.Target != headless
					},
					expectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						if src == naked && opts.Target == naked {
							// naked->naked should always succeed.
							return true
						}

						// If one of the two endpoints is naked, expect failure.
						return src != naked && opts.Target != naked
					},
				},
				{
					configFile:          "global-mtls-permissive.yaml",
					namespace:           systemNM,
					requiredEnvironment: environment.Kube,
					include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the naked app.
						if opts.Target == naked {
							return false
						}

						// Exclude calls to the headless TCP port.
						if opts.Target == headless && opts.PortName == "tcp" {
							return false
						}
						return true
					},
					expectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					configFile: "global-mtls-off.yaml",
					namespace:  systemNM,
					include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the headless TCP port.
						if opts.Target == headless && opts.PortName == "tcp" {
							return false
						}
						return true
					},
					onRun: func(ctx framework.TestContext, src echo.Instance, opts echo.CallOptions) {
						// The native implementation has some limitations that we need to account for.
						ctx.Environment().Case(environment.Native, func() {
							if src == naked && opts.Target == naked {
								// naked->naked should always work.
								return
							}

							switch opts.Scheme {
							case scheme.WebSocket, scheme.GRPC:
								// TODO(https://github.com/istio/istio/issues/13754)
								ctx.Skipf("https://github.com/istio/istio/issues/13754")
							}
						})
					},
					expectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return true
					},
				},
				{
					configFile:          "single-port-mtls-on.yaml",
					namespace:           ns,
					requiredEnvironment: environment.Kube,
					include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Include all tests that target app B, which has the single-port config.
						return opts.Target == b
					},
					expectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						return opts.PortName != "http"
					},
				},
			}

			for _, c := range testCases {
				testName := strings.TrimSuffix(c.configFile, filepath.Ext(c.configFile))
				test := ctx.NewSubTest(testName)

				if c.requiredEnvironment != "" {
					test.RequiresEnvironment(c.requiredEnvironment)
				}

				test.Run(func(ctx framework.TestContext) {
					// Apply the policy.
					policyYAML := file.AsStringOrFail(ctx, filepath.Join("testdata", c.configFile))
					g.ApplyConfigOrFail(ctx, c.namespace, policyYAML)
					ctx.WhenDone(func() error {
						return g.DeleteConfig(ctx, c.namespace.Name(), policyYAML)
					})

					// Give some time for the policy propagate.
					// TODO: query pilot or app to know instead of sleep.
					time.Sleep(10 * time.Second)

					for _, src := range []echo.Instance{a, b, headless, naked} {
						for _, dest := range []echo.Instance{a, b, headless, naked} {
							for _, opts := range callOptions {
								// Copy the loop variables so they won't change for the subtests.
								src := src
								dest := dest
								opts := opts
								onPreRun := c.onRun

								// Set the target on the call options.
								opts.Target = dest

								if c.include(src, opts) {
									expectSuccess := c.expectSuccess(src, opts)

									subTestName := fmt.Sprintf("%s->%s://%s:%s",
										src.Config().Service,
										opts.Scheme,
										dest.Config().Service,
										opts.PortName)

									ctx.NewSubTest(subTestName).
										RunParallel(func(ctx framework.TestContext) {
											if onPreRun != nil {
												onPreRun(ctx, src, opts)
											}

											checker := connection.Checker{
												From:          src,
												Options:       opts,
												ExpectSuccess: expectSuccess,
											}
											checker.CheckOrFail(ctx)
										})
								}
							}
						}
					}
				})
			}
		})
}

func config(name string, ns namespace.Instance, headless bool, annos echo.Annotations) echo.Config {
	return echo.Config{
		Service:        name,
		Namespace:      ns,
		ServiceAccount: true,
		Headless:       headless,
		Annotations:    annos,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: model.ProtocolHTTP,
			},
			{
				Name:     "tcp",
				Protocol: model.ProtocolTCP,
			},
			{
				Name:     "grpc",
				Protocol: model.ProtocolGRPC,
			},
		},
		Galley: g,
		Pilot:  p,
	}
}
