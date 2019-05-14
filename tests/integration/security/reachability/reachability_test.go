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
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
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
			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			ns := namespace.NewOrFail(t, ctx, "reachability", true)

			ports := []echo.Port{
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
			}

			var a, b, headless, naked echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, echo.Config{
					Service:        "a",
					Namespace:      ns,
					ServiceAccount: true,
					Ports:          ports,
					Galley:         g,
					Pilot:          p,
				}).
				With(&b, echo.Config{
					Service:        "b",
					Namespace:      ns,
					Ports:          ports,
					ServiceAccount: true,
					Galley:         g,
					Pilot:          p,
				}).
				With(&headless, echo.Config{
					Service:        "headless",
					Namespace:      ns,
					Ports:          ports,
					ServiceAccount: true,
					Headless:       true,
					Galley:         g,
					Pilot:          p,
				}).
				With(&naked, echo.Config{
					Service:   "naked",
					Namespace: ns,
					Ports:     ports,
					Annotations: echo.NewAnnotations().
						SetBool(echo.SidecarInject, false),
					Galley: g,
					Pilot:  p,
				}).
				BuildOrFail(t)

			testCases := []struct {
				configFile string
				namespace  namespace.Instance
				subTests   []connection.Checker
			}{
				{
					configFile: "global-mtls-on.yaml",
					namespace:  systemNS,
					subTests: []connection.Checker{
						{
							From: a,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
						{
							From: a,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "grpc",
								Scheme:   scheme.GRPC,
							},
							ExpectSuccess: true,
						},
						{
							From: a,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "tcp",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
						{
							From: a,
							Options: echo.CallOptions{
								Target:   headless,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
						{
							From: a,
							Options: echo.CallOptions{
								Target:   headless,
								PortName: "grpc",
								Scheme:   scheme.GRPC,
							},
							ExpectSuccess: true,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: false,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "grpc",
								Scheme:   scheme.GRPC,
							},
							ExpectSuccess: false,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "tcp",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: false,
						},
					},
				},
				{
					configFile: "global-mtls-permissive.yaml",
					namespace:  systemNS,
					subTests: []connection.Checker{
						{
							From: a,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "grpc",
								Scheme:   scheme.GRPC,
							},
							ExpectSuccess: true,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "tcp",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
					},
				},
				{
					configFile: "global-mtls-off.yaml",
					namespace:  systemNS,
					subTests: []connection.Checker{
						{
							From: a,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
					},
				},
				{
					configFile: "single-port-mtls-on.yaml",
					namespace:  ns,
					subTests: []connection.Checker{
						{
							From: a,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: false,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "http",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: false,
						},
						{
							From: a,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "tcp",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
						{
							From: naked,
							Options: echo.CallOptions{
								Target:   b,
								PortName: "tcp",
								Scheme:   scheme.HTTP,
							},
							ExpectSuccess: true,
						},
					},
				},
			}

			for _, c := range testCases {
				testName := strings.TrimSuffix(c.configFile, filepath.Ext(c.configFile))
				t.Run(testName, func(t *testing.T) {

					// Apply the policy.
					deploymentYAML := file.AsStringOrFail(t, filepath.Join("testdata", c.configFile))
					g.ApplyConfigOrFail(t, c.namespace, deploymentYAML)
					defer g.DeleteConfigOrFail(t, c.namespace, deploymentYAML)

					// Give some time for the policy propagate.
					// TODO: query pilot or app to know instead of sleep.
					time.Sleep(time.Second)

					for _, subTest := range c.subTests {
						subTestName := fmt.Sprintf("%s->%s:%s",
							subTest.From.Config().Service,
							subTest.Options.Target.Config().Service,
							subTest.Options.PortName)
						t.Run(subTestName, func(t *testing.T) {
							retry.UntilSuccessOrFail(t, subTest.Check, retry.Delay(time.Second), retry.Timeout(10*time.Second))
						})
					}
				})
			}
		})
}
