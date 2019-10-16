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

package security

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/authn"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	authHeaderKey = "Authorization"
)

func TestAuthnJwt(t *testing.T) {
	testIssuer1Token := jwt.TokenIssuer1
	testIssuer2Token := jwt.TokenIssuer2

	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "authn-jwt",
				Inject: true,
			})

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			testCases := []struct {
				configFile string
				subTests   []authn.TestCase
			}{
				{
					configFile: "simple-jwt-policy.yaml.tmpl",
					subTests: []authn.TestCase{
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer1Token},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + jwt.TokenExpired},
									},
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: false,
						},
					},
				},
				{
					configFile: "wrong-issuer.yaml.tmpl",
					subTests: []authn.TestCase{
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer1Token},
									},
								},
							},
							ExpectAuthenticated: false,
						},
					},
				},
				{
					// Test jwt with paths with trigger rules with one issuer.
					configFile: "jwt-with-paths.yaml.tmpl",
					subTests: []authn.TestCase{
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/health_check",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/guest-us",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/index.html",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/index.html",
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer1Token},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   c,
									Path:     "/index.html",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   c,
									Path:     "/something-confidential",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   c,
									Path:     "/something-confidential",
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer1Token},
									},
								},
							},
							ExpectAuthenticated: true,
						},
					},
				},
				{
					// Test jwt with paths with trigger rules with two issuers.
					configFile: "two-issuers.yaml.tmpl",
					subTests: []authn.TestCase{
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer2Token},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer1Token},
									},
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Path:     "/testing-istio-jwt",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + jwt.TokenInvalid},
									},
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Path:     "/testing-istio-jwt",
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer1Token},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Path:     "/testing-istio-jwt",
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testIssuer2Token},
									},
								},
							},
							ExpectAuthenticated: false,
						},
					},
				},
			}

			for _, c := range testCases {
				testName := strings.TrimSuffix(c.configFile, filepath.Ext(c.configFile))
				t.Run(testName, func(t *testing.T) {

					// Apply the policy.
					namespaceTmpl := map[string]string{
						"Namespace": ns.Name(),
					}
					deploymentYAML := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
						file.AsStringOrFail(t, filepath.Join("testdata", c.configFile)))
					g.ApplyConfigOrFail(t, ns, deploymentYAML...)
					defer g.DeleteConfigOrFail(t, ns, deploymentYAML...)

					// Give some time for the policy propagate.
					time.Sleep(60 * time.Second)
					for _, subTest := range c.subTests {
						subTestName := fmt.Sprintf("%s->%s:%s",
							subTest.Request.From.Config().Service,
							subTest.Request.Options.Target.Config().Service,
							subTest.Request.Options.PortName)
						t.Run(subTestName, func(t *testing.T) {
							retry.UntilSuccessOrFail(t, subTest.CheckAuthn, retry.Delay(time.Second), retry.Timeout(10*time.Second))
						})
					}
				})
			}
		})
}
