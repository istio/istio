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

			// Apply the policy.
			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}
			jwtPolicies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, "testdata/jwt/simple-jwt-policy.yaml.tmpl"),
				file.AsStringOrFail(t, "testdata/jwt/jwt-with-paths.yaml.tmpl"),
				file.AsStringOrFail(t, "testdata/jwt/two-issuers.yaml.tmpl"))
			g.ApplyConfigOrFail(t, ns, jwtPolicies...)
			defer g.DeleteConfigOrFail(t, ns, jwtPolicies...)

			var a, b, c, d, e echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, util.EchoConfig("d", ns, false, nil, g, p)).
				With(&e, util.EchoConfig("e", ns, false, nil, g, p)).
				BuildOrFail(t)

			testCases := []authn.TestCase{
				{
					Name: "jwt-simple-valid-token",
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
					Name: "jwt-simple-expired-token",
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
					Name: "jwt-simple-no-token",
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
					Name: "jwt-excluded-paths-no-token[/health_check]",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							Path:     "/health_check",
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: true,
				},
				{
					Name: "jwt-excluded-paths-no-token[/guest-us]",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							Path:     "/guest-us",
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: true,
				},
				{
					Name: "jwt-excluded-paths-no-token[/index.html]",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							Path:     "/index.html",
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: false,
				},
				{
					Name: "jwt-excluded-paths-valid-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
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
					Name: "jwt-included-paths-no-token[/index.html]",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							Path:     "/index.html",
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: true,
				},
				{
					Name: "jwt-included-paths-no-token[/something-confidential]",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							Path:     "/something-confidential",
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: false,
				},
				{
					Name: "jwt-included-paths-valid-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
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
				{
					Name: "jwt-two-issuers-no-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: false,
				},
				{
					Name: "jwt-two-issuers-token2",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
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
					Name: "jwt-two-issuers-token1",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
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
					Name: "jwt-two-issuers-invalid-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
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
					Name: "jwt-two-issuers-token1[/testing-istio-jwt]",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
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
					Name: "jwt-two-issuers-token2[/testing-istio-jwt]",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
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
				{
					Name: "jwt-wrong-issuers",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
							Path:     "/wrong_issuer",
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + testIssuer1Token},
							},
						},
					},
					ExpectAuthenticated: false,
				},
			}

			for _, c := range testCases {
				t.Run(c.Name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, c.CheckAuthn,
						retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}
		})
}
